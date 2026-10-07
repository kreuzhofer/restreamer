package relay

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

type OutputStatus struct {
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	State    string `json:"state"`
	Attempts uint64 `json:"attempts"`
	Bytes    uint64 `json:"media_bytes_sent"`
}

type output struct {
	config config.Target
	log    *slog.Logger
	mu     sync.Mutex
	status OutputStatus
}

func (o *output) state(state string)     { o.mu.Lock(); o.status.State = state; o.mu.Unlock() }
func (o *output) snapshot() OutputStatus { o.mu.Lock(); defer o.mu.Unlock(); return o.status }

func (o *output) run(ctx context.Context, h *hub) {
	defer o.state("idle")
	delay := time.Second
	for ctx.Err() == nil {
		o.state("connecting")
		o.mu.Lock()
		o.status.Attempts++
		o.mu.Unlock()
		started := time.Now()
		err := o.attempt(ctx, h)
		if ctx.Err() != nil {
			return
		}
		o.state("retrying")
		// Never log raw network/protocol errors: a peer may echo a stream key.
		o.log.Warn("output disconnected; reconnecting", "target", o.config.Name, "reason", reason(err))
		if time.Since(started) > 30*time.Second {
			delay = time.Second
		}
		timer := time.NewTimer(delay + time.Duration(rand.Int64N(int64(delay/4))))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, 30*time.Second)
	}
}

var errQueue = errors.New("output queue overflow")
var errConnect = errors.New("connection or publish handshake failed")

func reason(err error) string {
	if errors.Is(err, errQueue) {
		return "queue_overflow"
	}
	if errors.Is(err, errConnect) {
		return "connect_failed"
	}
	return "connection_closed_or_write_failed"
}

func (o *output) attempt(ctx context.Context, h *hub) error {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	c, stream, err := rtmp.Dial(dialCtx, o.config.URL, o.config.StreamKey)
	cancel()
	if err != nil {
		return errConnect
	}
	defer c.Net.Close()
	s := h.subscribe()
	defer h.unsubscribe(s)
	o.state("waiting_for_keyframe")

	// The reader handles destination pings and acknowledgements for the entire
	// session. Conn serializes these writes with media writes.
	readDone := make(chan struct{})
	var readErr error
	go func() {
		defer close(readDone)
		for {
			m, err := c.Read()
			if err == nil {
				err = rtmp.CheckControl(m)
			}
			if err != nil {
				readErr = err
				return
			}
		}
	}()
	watchDone := make(chan struct{})
	stopWatch := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
		case <-s.failed:
		case <-stopWatch:
			return
		}
		c.Net.Close() // Interrupt an in-flight write immediately on overflow/shutdown.
	}()
	defer func() { close(stopWatch); <-watchDone; c.Net.Close(); <-readDone }()
	var base time.Duration
	first := true
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.failed:
			return errQueue
		case <-readDone:
			return readErr
		case m := <-s.packets:
			h.consumed(s, m)
			if first {
				base = m.Timestamp
				first = false
			}
			// Some publishers timestamp later metadata/sequence headers at zero.
			// Keep those; skip only media that predates the starting keyframe.
			isHeader := m.Type == rtmp.Data || (len(m.Body) > 1 && m.Body[1] == 0)
			if m.Timestamp < base && !isHeader {
				continue
			}
			copy := *m
			copy.Timestamp = max(0, m.Timestamp-base)
			copy.MessageStreamID = stream
			// Use independent chunk streams, regardless of the publisher's IDs.
			switch copy.Type {
			case rtmp.Video:
				copy.ChunkStreamID = 6
			case rtmp.Audio:
				copy.ChunkStreamID = 4
			default:
				copy.ChunkStreamID = 5
			}
			if err := c.Write(&copy); err != nil {
				select {
				case <-s.failed:
					return errQueue
				default:
					return err
				}
			}
			o.mu.Lock()
			o.status.Bytes += uint64(len(copy.Body))
			o.status.State = "streaming"
			o.mu.Unlock()
		}
	}
}
