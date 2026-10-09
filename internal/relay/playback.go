package relay

import (
	"errors"
	"io"
	"time"

	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

type PlaybackStatus struct {
	State          string  `json:"state"`
	Source         string  `json:"source"`
	ID             string  `json:"id,omitempty"`
	Revision       string  `json:"revision,omitempty"`
	Name           string  `json:"name,omitempty"`
	Position       float64 `json:"position"`
	Duration       float64 `json:"duration"`
	Remaining      float64 `json:"remaining"`
	Loop           bool    `json:"loop"`
	PauseReason    string  `json:"pause_reason,omitempty"`
	ReturnStage    string  `json:"return_stage,omitempty"`
	Error          string  `json:"error,omitempty"`
	Epoch          uint64  `json:"epoch"`
	TimelineBaseMS int64   `json:"timeline_base_ms"`
}

type clipPlayback struct {
	continuation                           bool
	ID, Name, Revision                     string
	reader                                 *clipReader
	loop, paused, streaming, eof           bool
	position, startPosition, broadcastBase time.Duration
	started                                time.Time
	pending                                *rtmp.Message
}

func (p *clipPlayback) positionAt(now time.Time) time.Duration {
	pos := p.position
	if p.streaming {
		pos += max(0, now.Sub(p.started))
	}
	return min(pos, p.reader.index.Duration)
}
func (p *clipPlayback) freeze(now time.Time) {
	p.position = p.positionAt(now)
	p.streaming = false
	p.continuation = false
}

func (b *broadcast) playbackStatus(now time.Time) PlaybackStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.playbackStatusLocked(now)
}

func (b *broadcast) playbackStatusLocked(now time.Time) PlaybackStatus {
	s := PlaybackStatus{State: "idle", Source: "off", Error: b.playbackError, Epoch: b.previewEpoch}
	if b.control.stage != "OFF" {
		s.Source = "brb"
		if b.live {
			s.Source = "obs"
		}
	}
	if p := b.clip; p != nil {
		s.ID, s.Name, s.Loop = p.ID, p.Name, p.loop
		s.Revision = p.Revision
		s.Duration = p.reader.index.Duration.Seconds()
		s.Position = p.positionAt(now).Seconds()
		s.Remaining = max(0, s.Duration-s.Position)
		s.State = "playing"
		s.Source = "file"
		s.TimelineBaseMS = (p.broadcastBase - p.startPosition).Milliseconds()
		if b.manual || p.paused {
			s.State = "paused"
			s.Source = "brb"
			s.PauseReason = "user"
			if b.manual {
				s.PauseReason = "manual_brb"
			}
		}
		if b.control.failed {
			s.State, s.Source = "failed", "brb"
		}
	}
	return s
}

// Caller holds b.mu. Preview clients reconnect on source changes/seeks so their
// buffered old frames cannot masquerade as the current file position.
func (b *broadcast) resetBroadcastPreview() {
	if b.previewChanged != nil {
		close(b.previewChanged)
	}
	b.previewChanged = make(chan struct{})
	b.previewEpoch++
}
func (b *broadcast) stopClip(reason string) {
	if b.clip != nil {
		b.clip.reader.close()
		b.clip = nil
		b.live = false
		b.active = false
		b.resetBroadcastPreview()
	}
	b.playbackError = reason
}

func (b *broadcast) startClip(now time.Time) error {
	p := b.clip
	if p.position >= p.reader.index.Duration {
		p.position = 0
	}
	pos, err := p.reader.seek(p.position)
	if err != nil {
		return err
	}
	p.position, p.startPosition = pos, pos
	p.started = now
	p.pending = nil
	p.eof = false
	p.streaming = true
	p.broadcastBase = max(b.last+time.Millisecond, now.Sub(b.started))
	b.live, b.active = false, false
	b.hub.clearHeaders()
	if !p.continuation {
		b.resetBroadcastPreview()
	}
	p.continuation = false
	b.emit(p.reader.index.Video, p.broadcastBase)
	b.emit(p.reader.index.Audio, p.broadcastBase)
	return nil
}

// Bounded, paced disk reads use the same output hub as OBS and BRB. No FFmpeg
// process or decoder is needed during playback, pause, seek or looping.
func (b *broadcast) tickClip(now time.Time) bool {
	p := b.clip
	if p == nil || p.paused || b.manual || b.control.failed {
		return false
	}
	if !p.streaming {
		if p.position >= p.reader.index.Duration && !p.loop {
			b.finishPlayback(now)
			return true
		}
		if b.startClip(now) != nil {
			b.failPlayback("Cannot seek prepared video; prepare it again and Retry", now)
			return false
		}
	}
	for sent := 0; sent < 256; sent++ {
		if p.pending == nil && !p.eof {
			msg, err := p.reader.next()
			if errors.Is(err, io.EOF) {
				p.eof = true
			} else if err != nil {
				b.failPlayback("Cannot read prepared video; check storage and prepare it again and Retry", now)
				return false
			} else {
				p.pending = msg
			}
		}
		if p.eof {
			if p.positionAt(now) < p.reader.index.Duration {
				return true
			}
			if p.loop {
				p.position = 0
				p.continuation = true
				p.streaming = false
				return true
			}
			b.finishPlayback(now)
			return true
		}
		elapsed := now.Sub(p.started)
		if p.pending.Timestamp-p.startPosition > elapsed {
			return true
		}
		b.emit(p.pending, p.broadcastBase+p.pending.Timestamp-p.startPosition)
		p.pending = nil
	}
	return true
}
