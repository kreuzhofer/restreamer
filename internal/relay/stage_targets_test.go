package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

type targetDashboardStatus struct {
	Outputs     []OutputStatus `json:"outputs"`
	Forwarding  bool           `json:"forwarding"`
	InputFrames uint64         `json:"input_frames"`
}

func TestNoDestinationsKeepsPreviewAndRejoinsAtAKeyframe(t *testing.T) {
	destination := newSink(t)
	s, address := startRelayWithLogger(t, []config.Target{destination.target("one")}, slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	publisher := publishInput(t, address)
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	writePacket(t, publisher, previewVideoConfig())
	writePacket(t, publisher, audioConfig())
	writePacket(t, publisher, packet(rtmp.Audio, 0, 0xaf, 1, 1))
	writePacket(t, publisher, keyframe(0))
	eventually(t, func() bool {
		return readStage(t, s).Stage == "LIVE" && readTargetDashboard(t, s).Outputs[0].State == "waiting_for_keyframe"
	})
	writePacket(t, publisher, keyframe(time.Second))
	for i := 0; i < 3; i++ {
		receive(t, destination)
	}
	before := readStage(t, s)
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool { return readTargetDashboard(t, s).Outputs[0].State == "disabled" })
	if after := readStage(t, s); after.Stage != "LIVE" || after.Mode != "real" || after.Source != "obs" || after.Playback.Epoch != before.Playback.Epoch {
		t.Fatal("last destination disable interrupted on-air sequence", after)
	}

	// Open a new preview only after delivery has stopped; its media therefore
	// cannot be stale browser data buffered before the destination change.
	httpServer := httptest.NewServer(s.Handler())
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", httpServer.URL+"/api/broadcast-preview", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	responses := make(chan *http.Response, 1)
	requestErrors := make(chan error, 1)
	go func() {
		response, err := httpServer.Client().Do(request)
		if err != nil {
			requestErrors <- err
			return
		}
		responses <- response
	}()
	var response *http.Response
	timestamp := 2 * time.Second
	eventually(t, func() bool {
		writePacket(t, publisher, keyframe(timestamp))
		timestamp += 40 * time.Millisecond
		select {
		case response = <-responses:
			return true
		case err := <-requestErrors:
			t.Fatal(err)
		default:
		}
		return false
	})
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("X-Preview-Codecs") == "" {
		t.Fatal("no-destination preview unavailable", response.StatusCode)
	}
	data := make([]byte, 512)
	if _, err := io.ReadFull(response.Body, data); err != nil || !bytes.Contains(data, []byte("ftyp")) {
		t.Fatal("no-destination preview contained no media", err)
	}
	if destination.count() != 1 {
		t.Fatal("disabled destination reconnected")
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": true, "confirmed": false}); w.Code != 409 {
		t.Fatal("unconfirmed destination joined", w.Code)
	}
	if destination.count() != 1 {
		t.Fatal("rejected destination change connected")
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": true}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool { return readTargetDashboard(t, s).Outputs[0].State == "waiting_for_keyframe" })
	frames := readTargetDashboard(t, s).InputFrames
	writePacket(t, publisher, packet(rtmp.Video, timestamp, 0x27, 1, 0, 0, 0, 0))
	eventually(t, func() bool { return readTargetDashboard(t, s).InputFrames > frames })
	select {
	case <-destination.packets:
		t.Fatal("destination joined on an interframe")
	default:
	}
	timestamp += 40 * time.Millisecond
	writePacket(t, publisher, keyframe(timestamp))
	video, audio, key := receive(t, destination), receive(t, destination), receive(t, destination)
	if video.Type != rtmp.Video || video.Body[1] != 0 || audio.Type != rtmp.Audio || audio.Body[1] != 0 || !isVideoFrame(key) || key.Body[0]>>4 != 1 {
		t.Fatal("destination did not rejoin with codec headers and a keyframe")
	}
	if destination.count() != 2 {
		t.Fatal("destination did not create a fresh session")
	}
}

func TestConcurrentDestinationChangesCannotBypassLastDestinationConfirmation(t *testing.T) {
	s := dashboardServer(t)
	cfg := s.cfg
	cfg.Targets = append(cfg.Targets, cfg.Targets[0])
	cfg.Targets[len(cfg.Targets)-1].Name = "two"
	s = New(cfg, s.log)
	startLiveControlTest(t, s)
	before := readStage(t, s)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for _, target := range []string{"one", "two"} {
		enabled := false
		body, err := json.Marshal(StageCommand{ID: "disable-" + target, ServerID: before.ServerID, Context: before.Context, Action: "set_target", Target: target, Enabled: &enabled})
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- dashboardRequest(s, "POST", "/api/stage/commands", string(body)).Code
		}()
	}
	wg.Wait()
	close(results)
	accepted, rejected := 0, 0
	for code := range results {
		switch code {
		case 200:
			accepted++
		case 409:
			rejected++
		default:
			t.Fatal("unexpected destination command result", code)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatal("concurrent stale requests bypassed reviewed effect", accepted, rejected)
	}
	var remaining string
	for _, target := range readTargetDashboard(t, s).Outputs {
		if target.Enabled {
			if remaining != "" {
				t.Fatal("neither target was disabled")
			}
			remaining = target.Name
		}
	}
	if remaining == "" {
		t.Fatal("both destinations were disabled without last-destination confirmation")
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": remaining, "enabled": false, "confirmed": false}); w.Code != 409 {
		t.Fatal("fresh last-destination change did not require confirmation", w.Code)
	}
}

func TestDestinationPreferencesPersistAtomicallyWithoutStartingAfterRestart(t *testing.T) {
	s := dashboardServer(t)
	before := readStage(t, s)
	for _, target := range []string{"missing-key", "invalid-url", "absent"} {
		w := stageRequest(t, s, "set_target", map[string]any{"target": target, "enabled": true, "confirmed": true})
		want := 409
		if target == "absent" {
			want = 404
		}
		if w.Code != want {
			t.Fatal("invalid target accepted", target, w.Code, w.Body.String())
		}
	}
	if readStage(t, s).Context != before.Context {
		t.Fatal("invalid request changed confirmation context")
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": false, "confirmed": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved := readStage(t, s)
	if err := os.Rename(s.cfg.StateFile, s.cfg.StateFile+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(s.cfg.StateFile, 0700); err != nil {
		t.Fatal(err)
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": true, "confirmed": false}); w.Code != 507 {
		t.Fatal("failed persistence was not surfaced", w.Code, w.Body.String())
	}
	if readTargetDashboard(t, s).Outputs[0].Enabled || readStage(t, s).Context != saved.Context {
		t.Fatal("failed persistence changed preferences or confirmation context")
	}
	if err := os.Remove(s.cfg.StateFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(s.cfg.StateFile+".saved", s.cfg.StateFile); err != nil {
		t.Fatal(err)
	}
	reloaded := New(s.cfg, s.log)
	if state := readStage(t, reloaded); state.Stage != "OFF" || state.Mode != "off" {
		t.Fatal("restart selected a broadcast", state)
	}
	if state := readTargetDashboard(t, reloaded); state.Forwarding || state.Outputs[0].Enabled {
		t.Fatal("restart lost preference or resumed delivery", state)
	}
}

func TestRehearsalDestinationChangesOnlyUpdatePreferences(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := readStage(t, s)
	for _, enabled := range []bool{false, true} {
		if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": enabled, "confirmed": false}); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if status := readTargetDashboard(t, s); status.Forwarding || status.Outputs[0].Enabled != enabled || status.Outputs[0].Attempts != 0 {
			t.Fatal("rehearsal preference enabled delivery", status)
		}
		if after := readStage(t, s); after.Stage != "PRESTREAM" || after.Mode != "preview_only" || after.Playback.Epoch != before.Playback.Epoch {
			t.Fatal("rehearsal target change disturbed sequence", after)
		}
	}
}

func TestLastDestinationDisablePreservesPrestreamPosition(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool { return readStage(t, s).Playback.Position >= 0.1 })
	before := readStage(t, s)
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	after := readStage(t, s)
	if after.Stage != "PRESTREAM" || after.Mode != "real" || after.Source != "file" || after.Media.Revision != revision || after.Playback.Epoch != before.Playback.Epoch || after.Playback.Position < before.Playback.Position {
		t.Fatal("last destination disable reset the playing prestream", before, after)
	}
}

func TestEndingDestinationChangesRejectNewEnablesUntilPlaybackFailure(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	settings := `{"prestream":"` + revision + `","ending":"` + revision + `","shortcuts":[]}`
	if w := dashboardRequest(s, "PUT", "/api/stage-media", settings); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "end_stream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": true}); w.Code != 409 {
		t.Fatal("ending permitted another destination", w.Code, w.Body.String())
	}
	if err := os.Truncate(s.library.revisionPath(revision), 13); err != nil {
		t.Fatal(err)
	}
	s.broadcast.tick(time.Now().Add(time.Second))
	if state := readStage(t, s); state.Stage != "ENDING" || state.Error == "" {
		t.Fatal("ending failure was not surfaced", state)
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": true}); w.Code != 200 {
		t.Fatal("failed ending did not release restriction", w.Code, w.Body.String())
	}
}

func readTargetDashboard(t *testing.T, s *Server) targetDashboardStatus {
	t.Helper()
	w := dashboardRequest(s, "GET", "/api/dashboard", "")
	var state targetDashboardStatus
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil {
		t.Fatal("cannot read destination status", w.Code, w.Body.String())
	}
	return state
}

func TestDestinationCommandsRequireEffectSpecificConfirmation(t *testing.T) {
	s := dashboardServer(t)
	change := func(enabled, confirmed bool, want int) {
		t.Helper()
		w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": enabled, "confirmed": confirmed})
		if w.Code != want {
			t.Fatalf("target enabled=%v confirmed=%v: %d %s", enabled, confirmed, w.Code, w.Body.String())
		}
	}
	change(false, false, 200)
	change(true, false, 200)
	startLiveControlTest(t, s)
	before := readStage(t, s)
	change(false, false, 409)
	if !readTargetDashboard(t, s).Outputs[0].Enabled {
		t.Fatal("unconfirmed last destination disable changed preference")
	}
	change(false, true, 200)
	after := readStage(t, s)
	if after.Stage != "LIVE" || after.Mode != "real" || after.Source != before.Source || !readTargetDashboard(t, s).Forwarding {
		t.Fatal("last destination disable changed stage or session", after)
	}
	if readTargetDashboard(t, s).Outputs[0].Enabled {
		t.Fatal("confirmed disable did not change preference")
	}
	change(true, false, 409)
	if readTargetDashboard(t, s).Outputs[0].Enabled {
		t.Fatal("unconfirmed enable joined the real broadcast")
	}
	change(true, true, 200)
	if !readTargetDashboard(t, s).Outputs[0].Enabled {
		t.Fatal("confirmed enable did not change preference")
	}
	context := readStage(t, s).Context
	change(true, false, 200)
	if readStage(t, s).Context != context {
		t.Fatal("idempotent destination action invalidated confirmations")
	}
}

func TestPendingRealStartRequiresDestinationsAtCommit(t *testing.T) {
	destination := newSink(t)
	s, address := startRelayWithLogger(t, []config.Target{destination.target("one")}, slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	if w := stageRequest(t, s, "go_live", map[string]any{"id": "pending-real-start"}); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": false, "confirmed": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	publisher := publishInput(t, address)
	writePacket(t, publisher, videoConfig())
	writePacket(t, publisher, audioConfig())
	writePacket(t, publisher, packet(rtmp.Audio, 0, 0xaf, 1, 1))
	writePacket(t, publisher, keyframe(0))
	eventually(t, func() bool { return readStage(t, s).Pending == nil })
	state := readStage(t, s)
	if state.Stage != "OFF" || state.Pending != nil || readTargetDashboard(t, s).Forwarding || destination.count() != 0 {
		t.Fatal("pending real start committed without selected destinations", state)
	}
	var outcome CommandResult
	w := dashboardRequest(s, "GET", "/api/stage/commands/pending-real-start", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &outcome) != nil || outcome.State != "cancelled" || outcome.Reason == "" {
		t.Fatal("missing actionable cancellation outcome", w.Code, w.Body.String())
	}
	// Restoring the preference and sending another keyframe must not replay the start.
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": true, "confirmed": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	writePacket(t, publisher, keyframe(time.Second))
	eventually(t, func() bool { return readTargetDashboard(t, s).InputFrames > 1 })
	if state := readStage(t, s); state.Stage != "OFF" || state.Pending != nil || destination.count() != 0 {
		t.Fatal("cancelled real start replayed after restoring a destination", state)
	}
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	writePacket(t, publisher, keyframe(2*time.Second))
	eventually(t, func() bool { return readStage(t, s).Stage == "LIVE" && destination.count() == 1 })
}
