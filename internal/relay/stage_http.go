package relay

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
)

type StageCommand struct {
	ID        string  `json:"id"`
	ServerID  string  `json:"server_id"`
	Context   string  `json:"context"`
	Action    string  `json:"action"`
	Confirmed bool    `json:"confirmed"`
	Mode      string  `json:"mode,omitempty"`
	Revision  string  `json:"revision,omitempty"`
	Loop      bool    `json:"loop,omitempty"`
	Position  float64 `json:"position,omitempty"`
	Target    string  `json:"target,omitempty"`
	Enabled   *bool   `json:"enabled,omitempty"`
}

func writeCommandResult(w http.ResponseWriter, code int, result CommandResult) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) stageCommandHTTP(w http.ResponseWriter, r *http.Request) {
	typ, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !controlOriginAllowed(r) || typ != "application/json" {
		writeCommandResult(w, 403, CommandResult{State: "rejected", Reason: "Control requests require same-origin JSON"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	var cmd StageCommand
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cmd) != nil || decoder.Decode(new(any)) != io.EOF || len(cmd.ID) == 0 || len(cmd.ID) > 128 || strings.ContainsAny(cmd.ID, "/\\\r\n\x00") {
		writeCommandResult(w, 400, CommandResult{State: "rejected", Reason: "Expected one identified stage command"})
		return
	}
	b := s.broadcast
	var candidate *clipPlayback
	var profile config.BRBProfile
	b.mu.Lock()
	var endingErr error
	if b.control.commands[cmd.ID] == nil {
		endingErr = b.validateEndingAction(cmd)
	}
	b.mu.Unlock()
	if endingErr != nil {
		writeCommandResult(w, 409, CommandResult{ID: cmd.ID, State: "rejected", Reason: endingErr.Error()})
		return
	}
	if cmd.Action == "prestream" || cmd.Action == "play_clip" || cmd.Action == "end_stream" || cmd.Action == "replace_now" || cmd.Action == "replace_on_return" || cmd.Action == "retry" {
		b.mu.Lock()
		skip := b.control.commands[cmd.ID] != nil || cmd.Action == "end_stream" && (b.control.stage == "ENDING" || b.control.stage == "OFF") || cmd.Action == "prestream" && b.control.stage == "PRESTREAM" || cmd.Action == "play_clip" && (b.control.stage == "OFF" || b.control.stage == "CLIP" && b.clip != nil && b.clip.Revision == cmd.Revision)
		b.mu.Unlock()
		if !skip {
			var err error
			mediaCmd := cmd
			if cmd.Action == "retry" {
				b.mu.Lock()
				if b.clip != nil && b.control.failed {
					mediaCmd.Revision = b.clip.Revision
				}
				b.mu.Unlock()
			}
			candidate, profile, err = s.openCommandMedia(mediaCmd)
			if err != nil {
				writeCommandResult(w, 409, CommandResult{ID: cmd.ID, State: "rejected", Reason: err.Error()})
				return
			}
		}
	}
	// Media hashing and indexing can be slow. Serialize only the application
	// of the command; commandLocked rechecks identity and reviewed context after
	// validation, including any Stop now accepted while storage was busy.
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	if candidate != nil && b.control.commands[cmd.ID] == nil && (b.media == nil || profile != b.media.settings.Profile) {
		candidate.reader.close()
		writeCommandResult(w, 409, CommandResult{ID: cmd.ID, State: "rejected", Reason: "Prepare compatible fallback BRB before selecting prepared media"})
		return
	}
	result, code := b.commandLocked(cmd, time.Now(), candidate)
	if candidate != nil && !b.retainsPlayback(candidate) {
		candidate.reader.close()
	}
	writeCommandResult(w, code, result)
}

func (s *Server) openCommandMedia(cmd StageCommand) (*clipPlayback, config.BRBProfile, error) {
	if s.library == nil {
		return nil, config.BRBProfile{}, errors.New("Prepare fallback BRB and a ready media revision first")
	}
	revision := cmd.Revision
	if cmd.Action == "prestream" || cmd.Action == "end_stream" {
		selected := s.library.selections().Prestream
		if cmd.Action == "end_stream" {
			selected = s.library.selections().Ending
		}
		if revision != "" && revision != selected {
			return nil, config.BRBProfile{}, errors.New("Stage media selection changed; review it again")
		}
		revision = selected
	}
	if revision == "" {
		return nil, config.BRBProfile{}, errors.New("Select a ready media revision first")
	}
	return s.library.openRevision(revision)
}

func (s *Server) stageCommandOutcomeHTTP(w http.ResponseWriter, r *http.Request) {
	b := s.broadcast
	b.mu.Lock()
	defer b.mu.Unlock()
	if record := b.control.commands[r.PathValue("id")]; record != nil {
		writeCommandResult(w, 200, record.result)
		return
	}
	writeCommandResult(w, 404, CommandResult{ID: r.PathValue("id"), State: "rejected", Reason: "Unknown command; refresh state and require a new user action"})
}

func (b *broadcast) commandLocked(cmd StageCommand, now time.Time, candidate *clipPlayback) (CommandResult, int) {
	c := &b.control
	reject := func(reason string) (CommandResult, int) {
		return CommandResult{ID: cmd.ID, State: "rejected", Reason: reason}, 409
	}
	if cmd.ServerID != c.serverID {
		return reject("Server restarted; refresh state and require a new user action")
	}
	fingerprint := commandFingerprint(cmd)
	if record := c.commands[cmd.ID]; record != nil {
		if record.fingerprint != fingerprint {
			return reject("Command identity was already used for a different request")
		}
		code := 200
		if record.result.State == "pending" {
			code = 202
		}
		return record.result, code
	}
	if cmd.Context != b.stageContext() {
		return reject("Broadcast selection changed; review its current state and confirm again")
	}
	if cmd.Action == "set_target" {
		_, err := b.applyTargetCommand(cmd)
		if err != nil {
			code := 409
			if errors.Is(err, errUnknownTarget) {
				code = 404
			}
			if errors.Is(err, errSaveState) {
				code = 507
			}
			return CommandResult{ID: cmd.ID, State: "rejected", Reason: err.Error()}, code
		}
		result := CommandResult{ID: cmd.ID, State: "completed"}
		b.rememberCommand(cmd, result)
		return result, 200
	}
	if err := b.validateEndingAction(cmd); err != nil {
		return reject(err.Error())
	}
	noop := cmd.Action == "end_stream" && c.stage == "ENDING" || cmd.Action == "go_live" && c.stage == "LIVE" || cmd.Action == "prestream" && c.stage == "PRESTREAM" || cmd.Action == "brb" && c.stage == "BRB" || cmd.Action == "stop_now" && c.stage == "OFF" && c.pending == nil || cmd.Action == "play_clip" && c.stage == "CLIP" && b.clip != nil && b.clip.Revision == cmd.Revision
	if cmd.Action == "replace_now" || cmd.Action == "replace_on_return" {
		var err error
		noop, err = b.validateReplacement(cmd, candidate)
		if err != nil {
			return reject(err.Error())
		}
	}
	if !noop && cmd.Action != "cancel_pending" && !directPlaybackAction(cmd.Action) && !cmd.Confirmed {
		return reject("Review and confirm this broadcast action")
	}
	if cmd.Action != "go_live" && cmd.Action != "prestream" && cmd.Action != "play_clip" && cmd.Action != "brb" && cmd.Action != "return" && cmd.Action != "stop_now" && cmd.Action != "cancel_pending" && !directPlaybackAction(cmd.Action) && cmd.Action != "stop_clip" && cmd.Action != "end_stream" && cmd.Action != "replace_now" && cmd.Action != "replace_on_return" && cmd.Action != "retry" {
		return reject("Unknown stage action")
	}
	if c.stage == "OFF" && (cmd.Action == "brb" || cmd.Action == "return" || cmd.Action == "play_clip" || cmd.Action == "end_stream") {
		return reject("Start prestream or go live first")
	}
	if cmd.Action == "play_clip" && !noop && (candidate == nil || b.media == nil) {
		return reject("Select a ready clip and prepare fallback BRB")
	}
	if cmd.Action == "end_stream" && !noop && (candidate == nil || b.media == nil) {
		return reject("Select ready ending and fallback BRB media")
	}
	if cmd.Action == "brb" && b.media == nil {
		return reject("Prepare fallback BRB before entering deliberate BRB")
	}
	if cmd.Action == "return" && c.returnTo == nil {
		return reject("No return destination is recorded")
	}
	if cmd.Action == "retry" && (!c.failed || b.clip == nil || candidate == nil) {
		return reject("No failed playback is available to Retry")
	}
	if err := b.validatePlaybackAction(cmd); err != nil {
		return reject(err.Error())
	}
	if cmd.Mode != "" && c.stage != "OFF" && cmd.Mode != c.mode {
		return reject("Stop this session before starting a different delivery mode")
	}
	if cmd.Mode != "" && cmd.Mode != "real" && cmd.Mode != "preview_only" {
		return reject("Unknown session delivery mode")
	}
	if (cmd.Action == "go_live" || cmd.Action == "prestream") && !noop {
		mode := cmd.Mode
		if mode == "" {
			mode = c.mode
			if mode == "off" {
				mode = "real"
			}
		}
		if c.stage != "OFF" && mode != c.mode {
			return reject("Stop this session before starting a different delivery mode")
		}
		if mode == "real" && c.stage == "OFF" && !b.hasEligibleDestination() {
			return reject("Select a destination or explicitly start a preview-only rehearsal")
		}
		if cmd.Action == "prestream" {
			if candidate == nil || b.media == nil {
				return reject("Select ready starting-soon and fallback BRB media")
			}
			b.cancelPending("cancelled", "Superseded by confirmed Prestream")
			b.stopClip("")
			b.clearReturn()
			c.failed = false
			b.clip = candidate
			candidate.loop = true
			b.manual, b.live, b.active = false, false, false
			c.stage, c.mode = "PRESTREAM", mode
			c.version++
			if err := b.startClip(now); err != nil {
				b.failPlayback("Cannot seek prepared video; prepare it again and Retry", now)
			}
			b.setDeliveryLocked(mode == "real")
		} else {
			b.cancelPending("cancelled", "Superseded by a confirmed Go live request")
			c.pending = &PendingTransition{ID: cmd.ID, Deadline: now.Add(10 * time.Second).UnixMilli(), Reason: "Waiting for a valid fresh OBS keyframe", Mode: mode}
		}
	}
	result := CommandResult{ID: cmd.ID, State: "completed"}
	if !noop {
		switch cmd.Action {
		case "retry":
			b.cancelPending("cancelled", "Superseded by confirmed Retry")
			candidate.loop, candidate.paused = b.clip.loop, b.clip.paused
			b.stopClip("")
			b.clip = candidate
			c.failed = false
			c.version++
			b.live, b.active = false, false
			if candidate.paused {
				b.startFallback(now)
			} else if err := b.startClip(now); err != nil {
				b.failPlayback("Cannot retry prepared media; prepare it again", now)
			}
		case "end_stream":
			b.startEnding(candidate, now)
		case "replace_now", "replace_on_return":
			b.applyReplacement(cmd, candidate, now)
		case "pause", "resume", "seek", "set_loop", "stop_clip":
			b.applyPlaybackAction(cmd, now)
		case "go_live":
			result.State = "pending"
		case "cancel_pending":
			b.cancelPending("cancelled", "Cancelled by operator")
		case "play_clip":
			b.cancelPending("cancelled", "Superseded by confirmed clip selection")
			if c.stage == "CLIP" {
				b.stopClip("")
			} else {
				if c.stage == "BRB" && c.returnTo != nil && c.returnTo.stage == "CLIP" {
					old := c.returnTo
					if old.clip != nil {
						old.clip.reader.close()
					}
					c.returnTo = old.returnTo
					// The current deliberate break already represents the clip's
					// BRB return. Retain its underlying destination only once.
					if c.returnTo != nil && c.returnTo.stage == "BRB" {
						c.returnTo = c.returnTo.returnTo
					}
				}
				c.returnTo = b.suspendStage(now)
			}
			b.clip = candidate
			candidate.loop = cmd.Loop
			b.manual, b.live, b.active = false, false, false
			b.playbackError = ""
			c.stage, c.failed = "CLIP", false
			c.version++
			if err := b.startClip(now); err != nil {
				b.failPlayback("Cannot seek prepared clip; prepare it again and Retry", now)
			}
		case "brb":
			b.cancelPending("cancelled", "Superseded by deliberate BRB")
			frame := b.suspendStage(now)
			c.returnTo = frame
			c.stage = "BRB"
			c.failed = false
			b.playbackError = ""
			b.manual, b.live, b.active = true, false, false
			c.version++
			b.startFallback(now)
		case "return":
			b.cancelPending("cancelled", "Superseded by Return")
			b.restoreStage(c.returnTo, now)
		case "stop_now":
			b.cancelEnding()
			b.cancelPending("cancelled", "Broadcast stopped")
			b.stopClip("")
			b.clearReturn()
			c.failed = false
			b.manual, b.live, b.active = false, false, false
			c.stage, c.mode = "OFF", "off"
			c.version++
			b.setDeliveryLocked(false)
			b.resetBroadcastPreview()
		}
	}
	b.rememberCommand(cmd, result)
	code := 200
	if result.State == "pending" {
		code = 202
	}
	return result, code
}
