package relay

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// StageStatus separates operator intent from the media actually selected.
type StageStatus struct {
	Ending             EndingStatus       `json:"ending"`
	Stage              string             `json:"stage"`
	Source             string             `json:"source"`
	Mode               string             `json:"mode"`
	ServerID           string             `json:"server_id"`
	Context            string             `json:"context"`
	Pending            *PendingTransition `json:"pending,omitempty"`
	Error              string             `json:"error,omitempty"`
	Outcomes           []CommandResult    `json:"outcomes"`
	Media              StageMediaStatus   `json:"media"`
	Playback           PlaybackStatus     `json:"playback"`
	ReturnStage        string             `json:"return_stage,omitempty"`
	ReturnMedia        *StageMediaStatus  `json:"return_media,omitempty"`
	ReturnPlayback     *PlaybackStatus    `json:"return_playback,omitempty"`
	ReturnAfterDiscard string             `json:"return_after_discard,omitempty"`
}

type StageMediaStatus struct {
	ID       string `json:"id,omitempty"`
	Revision string `json:"revision,omitempty"`
	Name     string `json:"name,omitempty"`
}

type stageControl struct {
	stage, mode, serverID string
	version               uint64
	pending               *PendingTransition
	commands              map[string]*commandRecord
	commandOrder          []string
	returnTo              *stageFrame
	failed                bool
	ending                EndingStatus
}

type PendingTransition struct {
	ID       string `json:"id"`
	Deadline int64  `json:"deadline"`
	Reason   string `json:"reason"`
	Mode     string `json:"mode"`
}

type CommandResult struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

type commandRecord struct {
	fingerprint [32]byte
	result      CommandResult
}

func newStageControl() stageControl {
	return stageControl{stage: "OFF", mode: "off", serverID: rand.Text(), version: 1, commands: make(map[string]*commandRecord)}
}

func (b *broadcast) stageStatus(now time.Time) StageStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stageStatusLocked(now)
}

func (b *broadcast) stageStatusLocked(now time.Time) StageStatus {
	c := &b.control
	source := "off"
	if c.stage != "OFF" {
		switch {
		case b.live:
			source = "obs"
		case b.active:
			source = "brb"
		case b.clip != nil && b.clip.streaming:
			source = "file"
		}
	}
	s := StageStatus{Stage: c.stage, Source: source, Mode: c.mode, ServerID: c.serverID, Context: b.stageContext(), Error: b.playbackError, Outcomes: []CommandResult{}}
	s.Ending = b.endingStatusLocked()
	s.Playback = b.playbackStatusLocked(now)
	if p := b.clip; p != nil {
		s.Media = StageMediaStatus{ID: p.ID, Revision: p.Revision, Name: p.Name}
	}
	if f := c.returnTo; f != nil {
		s.ReturnStage = f.stage
		if f.clip != nil {
			s.ReturnMedia = &StageMediaStatus{ID: f.clip.ID, Revision: f.clip.Revision, Name: f.clip.Name}
			p := f.clip
			playback := PlaybackStatus{ID: p.ID, Revision: p.Revision, Name: p.Name, State: "suspended", Source: "file", Position: p.position.Seconds(), Duration: p.reader.index.Duration.Seconds(), Loop: p.loop, PauseReason: "suspended", Error: f.error}
			playback.Remaining = max(0, playback.Duration-playback.Position)
			if p.paused {
				playback.State, playback.PauseReason = "paused", "user"
			}
			if f.error != "" {
				playback.State = "failed"
			}
			if f.returnTo != nil {
				playback.ReturnStage = f.returnTo.stage
			}
			s.ReturnPlayback = &playback
			if c.stage == "BRB" && f.stage == "CLIP" {
				destination := f.returnTo
				if destination != nil && destination.stage == "BRB" {
					destination = destination.returnTo
				}
				if destination != nil {
					s.ReturnAfterDiscard = destination.stage
				}
			}
		}
	}
	if c.pending != nil {
		copy := *c.pending
		s.Pending = &copy
	}
	for _, id := range c.commandOrder[max(0, len(c.commandOrder)-32):] {
		s.Outcomes = append(s.Outcomes, c.commands[id].result)
	}
	return s
}

func (b *broadcast) stageContext() string {
	return fmt.Sprintf("%s:%d", b.control.serverID, b.control.version)
}

func (s *Server) stageStatusHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.broadcast.stageStatus(time.Now()))
}

func (b *broadcast) invalidateStageContext() {
	b.mu.Lock()
	b.control.version++
	b.mu.Unlock()
}

func (b *broadcast) finishCommand(id, state, reason string) {
	if record := b.control.commands[id]; record != nil {
		record.result.State, record.result.Reason = state, reason
	}
}

// Keep a bounded reconciliation window. Advancing the review context before
// forgetting an identity prevents its original request from executing again.
// An outstanding transition remains queryable until it has an outcome.
func (b *broadcast) rememberCommand(cmd StageCommand, result CommandResult) {
	c := &b.control
	const maxCommandHistory = 256
	if len(c.commandOrder) >= maxCommandHistory {
		index := 0
		if c.pending != nil && c.commandOrder[index] == c.pending.ID {
			index++
		}
		delete(c.commands, c.commandOrder[index])
		c.commandOrder = append(c.commandOrder[:index], c.commandOrder[index+1:]...)
		c.version++
	}
	c.commands[cmd.ID] = &commandRecord{fingerprint: commandFingerprint(cmd), result: result}
	c.commandOrder = append(c.commandOrder, cmd.ID)
}

func (b *broadcast) cancelPending(state, reason string) {
	if pending := b.control.pending; pending != nil {
		b.finishCommand(pending.ID, state, reason)
		b.control.pending = nil
	}
}

func (b *broadcast) expirePending(now time.Time) {
	if p := b.control.pending; p != nil && now.UnixMilli() >= p.Deadline {
		b.cancelPending("timed_out", "OBS did not provide a valid fresh keyframe within 10 seconds; request cancelled")
	}
}

// Caller owns broadcast.mu; output workers never acquire it. The delivery gate
// is separate from stage execution so rehearsals can run the same sequencer.
func (b *broadcast) setDeliveryLocked(enabled bool) {
	if b.server.forwarding.Swap(enabled) == enabled {
		return
	}
	for _, o := range b.server.outputs {
		if enabled {
			o.resetDrain()
		}
		o.mu.Lock()
		o.blocked = !enabled
		o.failed = false
		o.status.RetryAt = 0
		if o.status.State == "idle" || o.status.State == "disabled" || o.status.State == "paused" {
			o.status.State = o.settledState()
		} else if !enabled {
			o.status.State = "stopping"
		}
		o.mu.Unlock()
		select {
		case o.changed <- struct{}{}:
		default:
		}
	}
}

func (b *broadcast) commitLive(now time.Time) {
	p := b.control.pending
	if p == nil {
		return
	}
	b.stopClip("")
	b.clearReturn()
	b.control.failed = false
	b.manual, b.live, b.active = false, false, false
	b.control.stage, b.control.mode = "LIVE", p.Mode
	b.control.version++
	b.finishCommand(p.ID, "completed", "")
	b.control.pending = nil
	b.setDeliveryLocked(p.Mode == "real")
}

func commandFingerprint(cmd StageCommand) [32]byte {
	data, _ := json.Marshal(cmd)
	return sha256.Sum256(data)
}
