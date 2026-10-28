package relay

import "time"

// A frame retains the original source of a temporary interruption. Clip
// replacement updates the current clip; it never pushes another clip frame.
type stageFrame struct {
	fallback       *clipPlayback
	fallbackMedia  *brbMedia
	fallbackFailed bool
	fallbackError  string
	stage          string
	clip           *clipPlayback
	returnTo       *stageFrame
	error          string
}

func closeStageFrame(frame *stageFrame) {
	if frame == nil {
		return
	}
	if frame.fallback != nil {
		frame.fallback.reader.close()
	}
	if frame.clip != nil {
		frame.clip.reader.close()
	}
	closeStageFrame(frame.returnTo)
}

func (b *broadcast) clearReturn() {
	closeStageFrame(b.control.returnTo)
	b.control.returnTo = nil
}

func (b *broadcast) suspendStage(now time.Time) *stageFrame {
	if b.fallback != nil {
		b.fallback.freeze(now)
	}
	if b.clip != nil {
		b.clip.freeze(now)
	}
	f := &stageFrame{fallback: b.fallback, fallbackMedia: b.fallbackMedia, fallbackFailed: b.fallbackFailed, fallbackError: b.fallbackError, stage: b.control.stage, clip: b.clip, returnTo: b.control.returnTo, error: b.playbackError}
	b.fallback = nil
	b.fallbackMedia = nil
	b.fallbackFailed = false
	b.fallbackError = ""
	b.clip = nil
	b.control.returnTo = nil
	return f
}

func (b *broadcast) restoreStage(frame *stageFrame, now time.Time) {
	b.stopClip("")
	b.closeFallback()
	b.fallback = frame.fallback
	b.fallbackMedia = frame.fallbackMedia
	b.fallbackFailed = frame.fallbackFailed
	b.fallbackError = frame.fallbackError
	b.control.stage = frame.stage
	b.control.returnTo = frame.returnTo
	b.control.failed = frame.error != ""
	b.playbackError = frame.error
	if frame.stage == "LIVE" {
		b.playbackError = "Waiting for a fresh OBS keyframe to resume LIVE"
		b.control.failed = false
	}
	b.clip = frame.clip
	b.manual, b.live, b.active = frame.stage == "BRB" && frame.clip == nil, false, false
	b.control.version++
	if b.clip != nil && !b.clip.paused && !b.control.failed {
		if err := b.startClip(now); err != nil {
			b.failPlayback("Return source is unavailable; prepare it again and Retry", now)
		}
	} else if b.media != nil {
		b.startFallback(now)
	}
}

func (b *broadcast) failPlayback(reason string, now time.Time) {
	if b.playingFallback {
		b.fallbackFailure(reason, now)
		return
	}
	if b.clip != nil {
		b.clip.freeze(now)
		b.clip.reader.close()
	}
	b.control.failed = true
	b.playbackError = reason
	b.live, b.active = false, false
	if b.media != nil {
		b.startFallback(now)
	}
}

func (b *broadcast) finishPlayback(now time.Time) {
	if b.control.stage == "ENDING" {
		b.finishEndingPlayback(now)
		return
	}
	if b.control.stage == "CLIP" && b.control.returnTo != nil {
		b.restoreStage(b.control.returnTo, now)
		return
	}
	b.stopClip("")
}
