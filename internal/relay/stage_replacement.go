package relay

import (
	"errors"
	"time"
)

// Candidates are opened and verified before acquiring broadcast.mu so library
// hashing and preparation cannot interrupt the source currently on air.
func (b *broadcast) validateReplacement(cmd StageCommand, candidate *clipPlayback) (bool, error) {
	if b.control.ending.Draining {
		return false, errors.New("Ending is draining; its media can no longer be replaced")
	}
	current := b.clip
	stage := b.control.stage
	if cmd.Action == "replace_on_return" {
		frame := b.control.returnTo
		if frame == nil {
			return false, errors.New("No suspended prepared media is available to replace")
		}
		current, stage = frame.clip, frame.stage
	}
	if (stage != "PRESTREAM" && stage != "CLIP" && stage != "ENDING") || current == nil {
		return false, errors.New("No prepared stage media is selected for replacement")
	}
	if current.Revision == cmd.Revision {
		return true, nil
	}
	if candidate == nil || b.media == nil {
		return false, errors.New("Select an exact ready replacement and prepare fallback BRB")
	}
	return false, nil
}

// Caller has validated the command, its reviewed context, and the candidate.
func (b *broadcast) applyReplacement(cmd StageCommand, candidate *clipPlayback, now time.Time) {
	if cmd.Action == "replace_on_return" {
		frame := b.control.returnTo
		candidate.loop, candidate.paused = frame.clip.loop, frame.clip.paused
		frame.clip.reader.close()
		frame.clip, frame.error = candidate, ""
		b.control.version++
		return
	}
	previous := b.clip
	candidate.loop = previous.loop
	candidate.paused = previous.paused
	b.cancelPending("cancelled", "Superseded by confirmed media replacement")
	if candidate.paused {
		previous.reader.close()
		b.clip = candidate
		b.playbackError = ""
		b.control.failed = false
		b.control.version++
		return
	}
	b.stopClip("")
	b.clip = candidate
	b.control.failed = false
	b.control.version++
	b.manual, b.live, b.active = false, false, false
	if err := b.startClip(now); err != nil {
		b.failPlayback("Cannot start the replacement; prepare it again and Retry", now)
	}
}

func (b *broadcast) retainsPlayback(candidate *clipPlayback) bool {
	if candidate == b.clip {
		return true
	}
	for frame := b.control.returnTo; frame != nil; frame = frame.returnTo {
		if frame.clip == candidate {
			return true
		}
	}
	return false
}
