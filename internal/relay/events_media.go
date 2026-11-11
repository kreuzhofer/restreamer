package relay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

const eventSegmentLimit = 64 << 20
const eventLiveSegments = 5

type eventSegment struct {
	Name          string  `json:"name"`
	Init          string  `json:"init"`
	Duration      float64 `json:"duration"`
	Size          int64   `json:"size"`
	Discontinuity bool    `json:"discontinuity,omitempty"`
	Checksum      string  `json:"checksum"`
	InitChecksum  string  `json:"init_checksum"`
}

// One packager owns an event's media writes. Neither viewers nor other outputs
// participate in this queue. Headers and source changes start independent HLS
// timelines; ordinary segments keep the same media decode timeline.
type eventPackager struct {
	event         *streamingEvent
	mux           previewMux
	video, audio  *rtmp.Message
	data          bytes.Buffer
	init          string
	initChecksum  string
	start, last   time.Duration
	frameDuration time.Duration
	discontinuity bool
}

func (p *eventPackager) push(m *rtmp.Message) error {
	if m.Type != rtmp.Video && m.Type != rtmp.Audio {
		return nil
	}
	if len(m.Body) < 2 {
		return errPreviewCodec
	}
	if m.Body[1] == 0 {
		previous := p.audio
		if m.Type == rtmp.Video {
			previous = p.video
		}
		if previous != nil && bytes.Equal(previous.Body, m.Body) && !p.mux.started {
			return nil
		}
		if p.mux.started {
			if err := p.flushTail(); err != nil {
				return err
			}
			p.mux = previewMux{}
			p.init = ""
			p.discontinuity = true
		}
		if m.Type == rtmp.Video {
			p.video = m
		} else {
			p.audio = m
		}
		p.mux = previewMux{}
		for _, header := range []*rtmp.Message{p.video, p.audio} {
			if header != nil {
				if _, err := p.mux.push(header); err != nil {
					return err
				}
			}
		}
		return nil
	}
	wasStarted := p.mux.started
	data, err := p.mux.push(m)
	if err != nil {
		return err
	}
	if !p.mux.started {
		return nil
	}
	if !wasStarted {
		// A random init identity prevents collisions after interrupted writes/restarts.
		p.init = "init-" + randomEventToken() + ".mp4"
		if err := writeEventMedia(filepath.Join(p.event.dir, p.init), data); err != nil {
			return err
		}
		p.initChecksum = fmt.Sprintf("%x", sha256.Sum256(data))
		p.start, p.last = m.Timestamp, m.Timestamp
		p.frameDuration = 40 * time.Millisecond
		return nil
	}
	if p.data.Len()+len(data) > eventSegmentLimit {
		return errors.New("Public event segment exceeded 64 MiB")
	}
	p.data.Write(data)
	if isVideoFrame(m) {
		p.frameDuration = m.Timestamp - p.last
		p.last = m.Timestamp
		duration := m.Timestamp - p.start
		if duration > 6*time.Second {
			return errors.New("Public event requires video keyframes at least every two seconds")
		}
		if m.Body[0]>>4 == 1 && duration >= 4*time.Second {
			if err := p.flush(duration); err != nil {
				return err
			}
			p.start = m.Timestamp
		}
	}
	return nil
}

func (p *eventPackager) flushTail() error {
	if !p.mux.started || p.mux.pending == nil {
		return nil
	}
	next := *p.mux.pending
	next.Timestamp += max(time.Millisecond, min(p.frameDuration, time.Second))
	data, err := p.mux.push(&next)
	if err != nil {
		return err
	}
	if p.data.Len()+len(data) > eventSegmentLimit {
		return errors.New("Public event segment exceeded 64 MiB")
	}
	p.data.Write(data)
	return p.flush(next.Timestamp - p.start)
}

func (p *eventPackager) flush(duration time.Duration) error {
	if p.data.Len() == 0 {
		return nil
	}
	if duration <= 0 || duration > 6*time.Second {
		return errors.New("Invalid public event segment duration")
	}
	e := p.event
	e.mu.Lock()
	defer e.mu.Unlock()
	name := fmt.Sprintf("segment-%08d.m4s", len(e.Segments))
	data := p.data.Bytes()
	if err := writeEventMedia(filepath.Join(e.dir, name), data); err != nil {
		return err
	}
	segment := eventSegment{Name: name, Init: p.init, Duration: duration.Seconds(), Size: int64(len(data)), Discontinuity: p.discontinuity}
	segment.Checksum = fmt.Sprintf("%x", sha256.Sum256(data))
	segment.InitChecksum = p.initChecksum
	e.Segments = append(e.Segments, segment)
	e.Bytes += segment.Size
	if err := e.save(); err != nil {
		e.Segments = e.Segments[:len(e.Segments)-1]
		e.Bytes -= segment.Size
		return errors.New("Cannot save event recording index")
	}
	p.data.Reset()
	p.discontinuity = false
	return nil
}

func writeEventMedia(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".media-*")
	if err != nil {
		return errors.New("Cannot write event storage; check free space and permissions")
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = f.Close()
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return errors.New("Cannot write event storage; check free space and permissions")
	}
	return nil
}

func (e *streamingEvent) playlist(replay bool) string {
	segments := e.Segments
	start := 0
	if !replay && len(segments) > eventLiveSegments {
		start = len(segments) - eventLiveSegments
		segments = segments[start:]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:6\n#EXT-X-MEDIA-SEQUENCE:%d\n", start)
	discontinuities := 0
	for _, segment := range e.Segments[:start] {
		if segment.Discontinuity {
			discontinuities++
		}
	}
	fmt.Fprintf(&b, "#EXT-X-DISCONTINUITY-SEQUENCE:%d\n", discontinuities)
	if replay {
		b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	}
	init := ""
	for _, segment := range segments {
		if segment.Discontinuity {
			b.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		if segment.Init != init {
			fmt.Fprintf(&b, "#EXT-X-MAP:URI=\"%s\"\n", segment.Init)
			init = segment.Init
		}
		fmt.Fprintf(&b, "#EXTINF:%.6f,\n%s\n", segment.Duration, segment.Name)
	}
	if replay {
		b.WriteString("#EXT-X-ENDLIST\n")
	}
	return b.String()
}

func (o *output) manageEvent(ctx context.Context, h *hub) {
	e := o.event
	var sub *subscription
	var pack *eventPackager
	fail := func(err error) {
		e.mu.Lock()
		if e.State == "deleted" {
			e.mu.Unlock()
			return
		}
		e.State = "failed"
		e.Incomplete = true
		e.Published = false
		e.Error = err.Error()
		e.Version++
		_ = e.save()
		e.mu.Unlock()
		o.mu.Lock()
		o.status.CanEnable = false
		o.status.LastError = err.Error()
		o.status.LastErrorAt = time.Now().UnixMilli()
		o.status.State = "failed"
		o.status.UnavailableReason = "Recording failed; create a new event"
		o.mu.Unlock()
	}
	stop := func() {
		e.delivering.Store(false)
		if sub != nil {
			h.finish(sub, true)
			sub = nil
		}
		if pack != nil {
			if err := pack.flushTail(); err != nil {
				fail(err)
			}
			pack = nil
		}
		o.mu.Lock()
		o.connected = false
		o.mu.Unlock()
	}
	defer stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		draining, deadline := o.drainState()
		finishing := o.eventFinish.Load() || draining && e.begun.Load()
		if finishing && (sub == nil || h.pending(sub) == 0 || draining && time.Now().After(deadline)) {
			if sub != nil && h.pending(sub) > 0 {
				fail(errors.New("Event finalization exceeded the drain deadline"))
			}
			stop()
			e.mu.Lock()
			if e.begun.Load() {
				if e.Incomplete {
					e.State = "interrupted"
				} else {
					e.State = "ended"
				}
				if err := e.recoverRecording(); err != nil {
					e.Incomplete = true
					e.Error = "Cannot validate finalized event recording"
				}
				e.Published = e.AutoReplay && !e.Incomplete && len(e.Segments) > 0
				e.Version++
				if err := e.save(); err != nil {
					e.Incomplete = true
					e.Published = false
					e.State = "failed"
					e.Error = "Cannot finalize event recording"
				}
			}
			complete := !e.Incomplete
			e.mu.Unlock()
			o.mu.Lock()
			o.status.CanEnable = false
			o.status.Enabled = false
			o.status.State = "ended"
			o.status.UnavailableReason = "Event has finished"
			o.mu.Unlock()
			o.finishDrain(complete, "event_finalized")
			return
		}
		if !finishing && !o.forwardingAllowed() {
			stop()
		}
		if !finishing && o.forwardingAllowed() && sub == nil && o.snapshot().CanEnable {
			e.mu.Lock()
			e.State = "live"
			e.Version++
			err := e.save()
			e.mu.Unlock()
			if err != nil {
				fail(errors.New("Cannot start recording; check event storage"))
			} else {
				e.begun.Store(true)
				e.delivering.Store(true)
				sub = h.subscribeOutput(o)
				pack = &eventPackager{event: e, discontinuity: true}
				o.mu.Lock()
				o.connected = true
				o.status.State = "waiting_for_keyframe"
				o.status.Attempts++
				o.mu.Unlock()
			}
		}
		var packets <-chan *rtmp.Message
		var failed <-chan struct{}
		if sub != nil {
			packets = sub.packets
			failed = sub.failed
		}
		select {
		case <-ctx.Done():
			stop()
			e.mu.Lock()
			if e.begun.Load() && e.State == "live" {
				e.State = "interrupted"
				e.Incomplete = true
				e.Published = false
				e.Error = "Broadcast interrupted by server shutdown"
				e.Version++
				_ = e.save()
			}
			e.mu.Unlock()
			return
		case <-o.changed:
			if !o.eventFinish.Load() {
				stop()
			}
		case <-ticker.C:
		case <-failed:
			fail(errors.New("Event recording queue overflowed"))
			stop()
		case m := <-packets:
			h.consumed(sub, m)
			err := pack.push(m)
			h.delivered(sub)
			if err != nil {
				fail(err)
				stop()
			} else {
				o.recordSent(m)
				o.state("streaming")
			}
		}
	}
}

// Called by the shared delivery gate; event state survives temporary target pauses.
func (o *output) updateEventDelivery(enabled bool) {
	if o.event == nil {
		return
	}
	if !enabled {
		o.event.delivering.Store(false)
		if o.event.begun.Load() {
			o.eventFinish.Store(true)
		}
	} else if status := o.snapshot(); status.Enabled && status.CanEnable {
		o.event.begun.Store(true)
	}
}
