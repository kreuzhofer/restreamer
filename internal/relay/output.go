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
	Name              string `json:"name"`
	Enabled           bool   `json:"enabled"`
	State             string `json:"state"`
	Attempts          uint64 `json:"attempts"`
	Bytes             uint64 `json:"media_bytes_sent"`
	Frames            uint64 `json:"video_frames_sent"`
	DroppedFrames     uint64 `json:"dropped_frames"`
	SkippedFrames     uint64 `json:"skipped_frames"`
	PausedFrames      uint64 `json:"paused_frames"`
	CanEnable         bool   `json:"can_enable"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
	LastError         string `json:"last_error,omitempty"`
	LastErrorAt       int64  `json:"last_error_at,omitempty"`
	RetryAt           int64  `json:"retry_at,omitempty"`
}

type output struct {
	config  config.Target
	log     *slog.Logger
	mu      sync.Mutex
	status  OutputStatus
	changed chan struct{}
	failed  bool // Current interruption only; LastError intentionally survives recovery.
	blocked bool // Master gate; independent of the persisted target preference.
}

func (o *output) state(state string) {
	o.mu.Lock()
	if o.status.Enabled && !o.blocked {
		o.status.State = state
	}
	o.mu.Unlock()
}
func (o *output) snapshot() OutputStatus { o.mu.Lock(); defer o.mu.Unlock(); return o.status }

func (o *output) forwardingAllowed() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.status.Enabled && !o.blocked
}

func (o *output) settledState() string {
	if !o.status.Enabled {
		return "disabled"
	}
	if o.blocked {
		return "paused"
	}
	return "idle"
}

func (o *output) setEnabled(enabled bool) {
	o.mu.Lock()
	o.status.Enabled = enabled
	if enabled {
		o.failed = false
		if o.status.State == "disabled" || o.status.State == "paused" {
			o.status.State = o.settledState()
		}
	} else if o.status.State == "idle" || o.status.State == "disabled" || o.status.State == "paused" {
		o.status.State = "disabled"
	} else {
		o.status.State = "stopping"
	}
	o.mu.Unlock()
	select {
	case o.changed <- struct{}{}:
	default:
	}
}

// The owning session (publisher or protected broadcast) has one manager per
// target. It joins a cancelled worker
// before starting another, even when switches change rapidly.
func (o *output) manage(ctx context.Context, h *hub) {
	// Offline changes have already been applied to the desired state.
	select {
	case <-o.changed:
	default:
	}
	var cancel context.CancelFunc
	var done chan struct{}
	stop := func() {
		if cancel != nil {
			cancel()
			<-done
			cancel = nil
			done = nil
		}
	}
	settle := func() {
		o.mu.Lock()
		o.status.State = o.settledState()
		o.status.RetryAt = 0
		o.mu.Unlock()
	}
	defer func() { stop(); settle() }()
	for {
		if ctx.Err() != nil {
			return
		}
		if o.forwardingAllowed() {
			if cancel == nil {
				cancel, done = o.startWorker(ctx, h)
			}
		} else {
			stop()
			settle()
		}
		select {
		case <-ctx.Done():
			return
		case <-o.changed:
			// Always interrupt on a change: a rapid stop/resume must still
			// close the old destination session before creating a new one.
			stop()
			settle()
		}
	}
}

func (o *output) startWorker(ctx context.Context, h *hub) (context.CancelFunc, chan struct{}) {
	worker, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); o.run(worker, h) }()
	return cancel, done
}

func (o *output) run(ctx context.Context, h *hub) {
	delay := time.Second
	for ctx.Err() == nil {
		o.state("connecting")
		o.mu.Lock()
		o.status.Attempts++
		o.status.RetryAt = 0
		o.mu.Unlock()
		started := time.Now()
		err := o.attempt(ctx, h)
		if ctx.Err() != nil || !o.forwardingAllowed() {
			return
		}
		o.state("retrying")
		// Never log raw network/protocol errors: a peer may echo a stream key.
		o.log.Warn("output disconnected; reconnecting", "target", o.config.Name, "reason", reason(err))
		if time.Since(started) > 30*time.Second {
			delay = time.Second
		}
		wait := delay + time.Duration(rand.Int64N(int64(delay/4)))
		o.mu.Lock()
		o.status.LastError = reason(err)
		o.failed = true
		o.status.LastErrorAt = time.Now().UnixMilli()
		o.status.RetryAt = time.Now().Add(wait).UnixMilli()
		o.mu.Unlock()
		timer := time.NewTimer(wait)
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
	s := h.subscribeOutput(o)
	defer func() { h.finish(s, ctx.Err() != nil || !o.forwardingAllowed()) }()
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
			if !o.forwardingAllowed() {
				if isVideoFrame(m) {
					o.discardFrames(1, true)
				}
				return context.Canceled
			}
			if first {
				base = m.Timestamp
				first = false
			}
			// Some publishers timestamp later metadata/sequence headers at zero.
			// Keep those; skip only media that predates the starting keyframe.
			isHeader := m.Type == rtmp.Data || (len(m.Body) > 1 && m.Body[1] == 0)
			if m.Timestamp < base && !isHeader {
				if isVideoFrame(m) {
					o.discardFrames(1, true)
				}
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
				if isVideoFrame(m) {
					o.discardFrames(1, ctx.Err() != nil)
				}
				select {
				case <-s.failed:
					return errQueue
				default:
					return err
				}
			}
			o.recordSent(&copy)
		}
	}
}
