package relay

import (
	"errors"
	"time"
)

// Completion confirms writes by this relay, never playback by a platform.
type EndingStatus struct {
	Draining  bool                      `json:"draining"`
	Completed bool                      `json:"completed"`
	Results   []EndingDestinationResult `json:"results"`
}

type EndingDestinationResult struct {
	Name     string `json:"name"`
	Complete bool   `json:"complete"`
	Reason   string `json:"reason,omitempty"`
}

func (b *broadcast) endingStatusLocked() EndingStatus {
	status := b.control.ending
	status.Results = append([]EndingDestinationResult{}, status.Results...)
	return status
}

func (b *broadcast) validateEndingAction(cmd StageCommand) error {
	if b.control.stage != "ENDING" || b.control.failed {
		return nil
	}
	switch cmd.Action {
	case "end_stream", "stop_now", "set_target", "cancel_pending":
		return nil
	case "replace_now":
		if !b.control.ending.Draining {
			return nil
		}
	}
	return errors.New("Ending in progress")
}

func (b *broadcast) startEnding(candidate *clipPlayback, now time.Time) {
	b.cancelPending("cancelled", "Superseded by confirmed End stream")
	b.stopClip("")
	b.clearReturn()
	b.control.ending = EndingStatus{Results: []EndingDestinationResult{}}
	b.control.stage, b.control.failed = "ENDING", false
	b.control.version++
	b.clip = candidate
	candidate.loop = false
	b.manual, b.live, b.active = false, false, false
	if err := b.startClip(now); err != nil {
		b.failPlayback("Cannot read prepared ending; check storage and Retry or choose another stage", now)
	}
}

func (b *broadcast) finishEndingPlayback(now time.Time) {
	b.clip.freeze(now)
	b.clip.reader.close()
	b.manual, b.live, b.active = false, false, false
	b.control.ending.Draining = true
	b.control.version++
	// A rehearsal never opens or drains destination sessions.
	if b.control.mode == "real" {
		deadline := now.Add(10 * time.Second)
		for _, output := range b.server.outputs {
			output.beginDrain(deadline)
		}
	}
	b.tickEndingDrain(now)
}

// Call before playback/fallback so EOF cannot admit new packets into a drain.
func (b *broadcast) tickEndingDrain(_ time.Time) bool {
	if !b.control.ending.Draining {
		return false
	}
	results := []EndingDestinationResult{}
	done := true
	if b.control.mode == "real" {
		for _, output := range b.server.outputs {
			finished, complete, reason := output.drainStatus()
			done = done && finished
			if finished {
				results = append(results, EndingDestinationResult{Name: output.config.Name, Complete: complete, Reason: reason})
			}
		}
	}
	b.control.ending.Results = results
	if done {
		b.control.ending.Draining, b.control.ending.Completed = false, true
		b.control.stage, b.control.mode = "OFF", "off"
		b.control.version++
		b.stopClip("")
		b.setDeliveryLocked(false)
		b.resetBroadcastPreview()
	}
	return true
}

func (b *broadcast) cancelEnding() {
	if !b.control.ending.Draining {
		return
	}
	// Keep finished destinations' evidence but do not claim completion after
	// the operator interrupts outstanding writes.
	results := []EndingDestinationResult{}
	for _, output := range b.server.outputs {
		done, complete, reason := output.drainStatus()
		if !done {
			complete, reason = false, "cancelled"
		}
		results = append(results, EndingDestinationResult{Name: output.config.Name, Complete: complete, Reason: reason})
	}
	b.control.ending = EndingStatus{Results: results}
}
