package relay

import (
	"context"
	"sync"
	"time"

	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

const inputStall = 3 * time.Second

type BRBStatus struct {
	Enabled bool   `json:"enabled"`
	Ready   bool   `json:"ready"`
	Active  bool   `json:"active"`
	Manual  bool   `json:"manual"`
	Reason  string `json:"reason"`
	Error   string `json:"error,omitempty"`
}

// A broadcast outlives publishers. All source selection and timestamp mapping
// happen under mu, before fanout; output failures remain independent.
type broadcast struct {
	clip                    *clipPlayback
	playbackError           string
	previewChanged          chan struct{}
	previewEpoch            uint64
	lastTick                time.Time
	attached                time.Time
	mu                      sync.Mutex
	server                  *Server
	hub                     *hub
	media                   *brbMedia
	manual, active, live    bool
	headers                 [3]*rtmp.Message
	lastVideo, lastAudio    time.Time
	closeInput              func()
	offset, inputBase, last time.Duration
	trackLast               [2]time.Duration
	started                 time.Time
	fallbackStart           time.Time
	fallbackBase            time.Duration
	videoIndex, audioIndex  int
	videoLoop, audioLoop    time.Duration
	lastError               string
}

func newBroadcast(s *Server, media *brbMedia) *broadcast {
	return &broadcast{server: s, hub: newHub(s.cfg.QueueBytes, s.outputs...), media: media, started: time.Now(), previewChanged: make(chan struct{})}
}

func (b *broadcast) run(ctx context.Context) {
	defer func() { b.mu.Lock(); b.stopClip(""); b.resetBroadcastPreview(); b.mu.Unlock() }()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			b.tick(now)
		}
	}
}

func (b *broadcast) status() BRBStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	reason := "ready"
	if b.active {
		reason = "waiting_for_obs"
		if b.clip != nil {
			reason = "file_paused"
		}
		if b.manual {
			reason = "manual"
		}
	} else if b.live && b.server.forwarding.Load() {
		reason = "live"
	}
	return BRBStatus{Enabled: true, Ready: b.media != nil, Active: b.active && b.server.forwarding.Load(), Manual: b.manual, Reason: reason, Error: b.lastError}
}

func (b *broadcast) setManual(enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.manual == enabled {
		return
	}
	if b.clip != nil {
		b.clip.freeze(time.Now())
	}
	b.manual = enabled
	// Returning from manual mode always waits for a new decodable keyframe.
	b.live = false
	b.server.log.Info("manual BRB changed", "enabled", enabled)
}

func (b *broadcast) inputLost() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.resetInput()
}
func (b *broadcast) resetInput() {
	b.headers = [3]*rtmp.Message{}
	b.lastVideo = time.Time{}
	b.lastAudio = time.Time{}
	b.live = false
	b.closeInput = nil
}
func (b *broadcast) attach(closeInput func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.resetInput()
	b.closeInput = closeInput
	b.attached = time.Now()
}

func (b *broadcast) tick(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	previousTick := b.lastTick
	b.lastTick = now
	if !b.server.forwarding.Load() {
		b.active = false
		b.live = false
		return
	}
	stale := b.lastVideo.IsZero() || now.Sub(b.lastVideo) >= inputStall || b.lastAudio.IsZero() || now.Sub(b.lastAudio) >= inputStall
	if stale && b.live {
		b.live = false
	}
	// Evict a stalled publisher so a new authenticated OBS session can reconnect.
	if stale && b.closeInput != nil && now.Sub(b.attached) >= inputStall {
		b.closeInput()
		b.closeInput = nil
	}
	if b.clip != nil && !previousTick.IsZero() && now.Sub(previousTick) > 250*time.Millisecond {
		b.clip.freeze(now)
	}
	if b.tickClip(now) {
		return
	}
	if b.manual || !b.live {
		if !b.active {
			b.startFallback(now)
		}
		// Do not send an unbounded catch-up burst after process suspension.
		if !previousTick.IsZero() && now.Sub(previousTick) > 250*time.Millisecond {
			b.startFallback(now)
		}
		b.sendFallback(now)
	}
}

func (b *broadcast) startFallback(now time.Time) {
	b.resetBroadcastPreview()
	b.active = true
	b.live = false
	b.fallbackStart = now
	b.fallbackBase = max(b.last+time.Millisecond, now.Sub(b.started))
	b.videoIndex, b.audioIndex = 0, 0
	b.videoLoop, b.audioLoop = 0, 0
	b.hub.clearHeaders()
	b.emit(b.media.video.header, b.fallbackBase)
	b.emit(b.media.audio.header, b.fallbackBase)
	b.server.log.Info("BRB active", "manual", b.manual)
}

func (b *broadcast) sendFallback(now time.Time) {
	elapsed := now.Sub(b.fallbackStart)
	for sent := 0; sent < 256; sent++ {
		v := b.media.video.frames[b.videoIndex]
		a := b.media.audio.frames[b.audioIndex]
		vt, at := b.videoLoop+v.Timestamp, b.audioLoop+a.Timestamp
		if min(vt, at) > elapsed {
			return
		}
		if vt <= at {
			b.emit(v, b.fallbackBase+vt)
			b.videoIndex++
			if b.videoIndex == len(b.media.video.frames) {
				b.videoIndex = 0
				b.videoLoop += b.media.video.duration
			}
		} else {
			b.emit(a, b.fallbackBase+at)
			b.audioIndex++
			if b.audioIndex == len(b.media.audio.frames) {
				b.audioIndex = 0
				b.audioLoop += b.media.audio.duration
			}
		}
	}
}

// ingest receives already validated media. Headers belong only to this publisher;
// a disconnect clears them so a new publisher cannot inherit stale configuration.
func (b *broadcast) ingest(m *rtmp.Message, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	header := -1
	if m.Type == rtmp.Data && isMetadata(m.Body) {
		header = 0
	}
	if len(m.Body) > 1 && m.Body[1] == 0 {
		if m.Type == rtmp.Video {
			header = 1
		}
		if m.Type == rtmp.Audio {
			header = 2
		}
	}
	if header >= 0 {
		b.headers[header] = m
	}
	if isVideoFrame(m) {
		b.lastVideo = now
	}
	if m.Type == rtmp.Audio && len(m.Body) > 1 && m.Body[1] == 1 {
		b.lastAudio = now
	}
	if !b.server.forwarding.Load() || b.manual || b.clip != nil {
		if isVideoFrame(m) {
			for _, o := range b.server.outputs {
				o.discardFrames(1, true)
			}
		}
		return
	}
	key := isVideoFrame(m) && m.Body[0]>>4 == 1
	if !b.live {
		if !key || b.headers[1] == nil || b.headers[2] == nil || b.lastAudio.IsZero() || now.Sub(b.lastAudio) >= inputStall {
			if isVideoFrame(m) {
				for _, o := range b.server.outputs {
					o.discardFrames(1, true)
				}
			}
			return
		}
		b.inputBase = m.Timestamp
		base := max(b.last+time.Millisecond, now.Sub(b.started))
		b.offset = base - m.Timestamp
		b.hub.clearHeaders()
		for _, h := range b.headers {
			if h != nil {
				b.emit(h, base)
			}
		}
		b.resetBroadcastPreview()
		b.live = true
		b.active = false
		b.lastError = ""
		b.server.log.Info("live input resumed")
	}
	if header >= 0 {
		b.emit(m, max(b.last, m.Timestamp+b.offset))
		return
	}
	if (m.Type == rtmp.Video || m.Type == rtmp.Audio) && m.Timestamp >= b.inputBase {
		// AVC end-of-sequence would tell receivers to stop decoding during BRB.
		if m.Type == rtmp.Video && len(m.Body) > 1 && m.Body[1] == 2 {
			return
		}
		b.emit(m, m.Timestamp+b.offset)
	}
}

func (b *broadcast) emit(m *rtmp.Message, ts time.Duration) {
	copy := *m
	copy.Timestamp = ts
	if isVideoFrame(m) || m.Type == rtmp.Audio && len(m.Body) > 1 && m.Body[1] == 1 {
		track := 0
		if m.Type == rtmp.Audio {
			track = 1
		}
		if ts < b.trackLast[track] {
			return
		}
		b.trackLast[track] = ts
	}
	end := ts
	if isVideoFrame(m) && len(m.Body) >= 5 {
		cts := int32(m.Body[2])<<16 | int32(m.Body[3])<<8 | int32(m.Body[4])
		if cts&0x800000 != 0 {
			cts |= ^int32(0xffffff)
		}
		fps := b.media.settings.Profile.FPS
		if fps > 0 {
			end += max(0, time.Duration(cts)*time.Millisecond) + time.Second/time.Duration(fps)
		}
	} else if m.Type == rtmp.Audio && len(m.Body) > 1 && m.Body[1] == 1 {
		rate := b.media.settings.Profile.SampleRate
		if rate > 0 {
			end += 1024 * time.Second / time.Duration(rate)
		}
	}
	b.last = max(b.last, end)
	if err := b.hub.publish(&copy); err != nil {
		b.lastError = "BRB media could not be forwarded"
	}
}

func (h *hub) clearHeaders() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.headers = [3]*rtmp.Message{}
}
