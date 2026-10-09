package relay

import (
	"errors"
	"math"
	"time"
)

func directPlaybackAction(action string) bool {
	return action == "pause" || action == "resume" || action == "seek" || action == "set_loop"
}

func (b *broadcast) validatePlaybackAction(cmd StageCommand) error {
	if !directPlaybackAction(cmd.Action) && cmd.Action != "stop_clip" {
		return nil
	}
	if b.control.stage != "CLIP" || b.clip == nil {
		return errors.New("Select a clip before using detailed playback controls")
	}
	if cmd.Action == "stop_clip" {
		if b.control.returnTo == nil {
			return errors.New("No return destination is recorded")
		}
		return nil
	}
	if b.control.failed {
		return errors.New("Playback failed; Retry or select another source")
	}
	if cmd.Action == "seek" && (math.IsNaN(cmd.Position) || math.IsInf(cmd.Position, 0) || cmd.Position < 0 || cmd.Position >= b.clip.reader.index.Duration.Seconds()) {
		return errors.New("Seek position must be inside the clip")
	}
	return nil
}

func (b *broadcast) applyPlaybackAction(cmd StageCommand, now time.Time) {
	p := b.clip
	switch cmd.Action {
	case "pause":
		if !p.paused {
			p.freeze(now)
			p.paused = true
			b.startFallback(now)
		}
	case "resume":
		if p.paused {
			p.paused = false
			if b.startClip(now) != nil {
				b.failPlayback("Cannot resume prepared clip; prepare it again and Retry", now)
			}
		}
	case "seek":
		pos, err := p.reader.seek(time.Duration(cmd.Position * float64(time.Second)))
		if err != nil {
			b.failPlayback("Cannot seek prepared clip; prepare it again and Retry", now)
			return
		}
		p.position, p.streaming, p.continuation, p.pending, p.eof = pos, false, false, nil, false
		if !p.paused {
			if b.startClip(now) != nil {
				b.failPlayback("Cannot seek prepared clip; prepare it again and Retry", now)
			}
		} else {
			b.resetBroadcastPreview()
		}
	case "set_loop":
		p.loop = cmd.Loop
	case "stop_clip":
		b.cancelPending("cancelled", "Superseded by Stop clip")
		b.restoreStage(b.control.returnTo, now)
	}
}
