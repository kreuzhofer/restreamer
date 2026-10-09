package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
)

func TestEndingRunsOnceInRehearsalAndRetainsCompletion(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","ending":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "end_stream", nil); w.Code != 409 || !strings.Contains(w.Body.String(), "Start prestream or go live first") {
		t.Fatal("ending started from OFF", w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "end_stream", map[string]any{"confirmed": false}); w.Code != 409 {
		t.Fatal("unconfirmed ending accepted", w.Code)
	}
	if w := stageRequest(t, s, "end_stream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state := readStage(t, s)
	if state.Stage != "ENDING" || state.Media.Revision != revision || state.Playback.Loop || state.ReturnStage != "" {
		t.Fatal("ending did not select its exact once-only source", state)
	}
	if w := stageRequest(t, s, "end_stream", map[string]any{"confirmed": false}); w.Code != 200 {
		t.Fatal("repeated ending is not a no-op", w.Code, w.Body.String())
	}
	if after := readStage(t, s); after.Context != state.Context || after.Playback.Epoch != state.Playback.Epoch {
		t.Fatal("repeated ending restarted media", after)
	}
	for _, action := range []string{"go_live", "prestream", "brb", "return", "play_clip", "pause", "seek", "set_loop"} {
		if w := stageRequest(t, s, action, map[string]any{"revision": revision}); w.Code != 409 || !strings.Contains(w.Body.String(), "Ending in progress") {
			t.Fatal("ending protection", action, w.Code, w.Body.String())
		}
	}
	now := time.Now()
	// No HTTP client remains attached while the server's normal sequence runs.
	for i := 0; i < 180; i++ {
		s.broadcast.tick(now.Add(time.Duration(i) * 20 * time.Millisecond))
	}
	if state := readStage(t, s); state.Stage != "OFF" || state.Source != "off" || state.Mode != "off" {
		t.Fatal("ending did not finish", state)
	}
	w := dashboardRequest(s, "GET", "/api/stage", "")
	var result struct {
		Ending struct {
			Draining, Completed bool
			Results             []any
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Ending.Draining || !result.Ending.Completed || len(result.Ending.Results) != 0 {
		t.Fatal("rehearsal completion was lost", w.Body.String())
	}
}

func TestOfflineStageActionsReportStartFirstWithoutPreparedMedia(t *testing.T) {
	s := dashboardServer(t)
	for _, action := range []string{"brb", "play_clip", "end_stream"} {
		if w := stageRequest(t, s, action, nil); w.Code != 409 || !strings.Contains(w.Body.String(), "Start prestream or go live first") {
			t.Fatal(action, w.Code, w.Body.String())
		}
	}
}

func TestEndingFinishesWithoutClientsAndReportsEachDestination(t *testing.T) {
	prepared, revision := preparedRevisionServer(t)
	if w := dashboardRequest(prepared, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","ending":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	healthy := newSink(t)
	pending, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	cfg := prepared.cfg
	cfg.Targets = []config.Target{
		healthy.target("healthy"),
		{Name: "pending", URL: "rtmp://" + pending.Addr().String() + "/app", StreamKey: "target-key"},
		{Name: "disabled", URL: "rtmp://" + pending.Addr().String() + "/app", StreamKey: "target-key", Enabled: "false"},
	}
	s := New(cfg, prepared.log)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	if w := stageRequest(t, s, "prestream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool { return healthy.count() == 1 })
	pending.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second))
	connection, err := pending.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	closed := make(chan struct{})
	go func() { io.Copy(io.Discard, connection); close(closed) }()
	if w := stageRequest(t, s, "end_stream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool { return readStage(t, s).Stage == "OFF" })
	state := readStage(t, s)
	if !state.Ending.Completed || state.Ending.Draining || len(state.Ending.Results) != 3 {
		t.Fatal("missing completion", state)
	}
	want := map[string]EndingDestinationResult{
		"healthy":  {Name: "healthy", Complete: true},
		"pending":  {Name: "pending", Reason: "unavailable"},
		"disabled": {Name: "disabled", Complete: true, Reason: "disabled"},
	}
	for _, result := range state.Ending.Results {
		if result != want[result.Name] {
			t.Fatal("incorrect destination completion", result)
		}
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("pending handshake survived ending")
	}
	eventually(t, func() bool {
		healthy.mu.Lock()
		defer healthy.mu.Unlock()
		return healthy.conn != nil && errors.Is(healthy.conn.SetReadDeadline(time.Time{}), net.ErrClosed)
	})
	if len(healthy.packets) == 0 {
		t.Fatal("ending never delivered media")
	}
}

func TestEndingRuntimeFailureHoldsFallbackAndUnlocksRecovery(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","ending":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, action := range []string{"prestream", "end_stream"} {
		if w := stageRequest(t, s, action, map[string]any{"mode": "preview_only"}); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	path := filepath.Join(s.library.root, "revisions", revision+".flv")
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.broadcast.tick(now)
	state := readStage(t, s)
	if state.Stage != "ENDING" || state.Source != "brb" || state.Error == "" || state.Media.Revision != revision || state.Ending.Completed {
		t.Fatal("runtime ending failure was not retained", state)
	}
	for i := 0; i < 200; i++ {
		s.broadcast.tick(now.Add(time.Duration(i) * 20 * time.Millisecond))
	}
	if state := readStage(t, s); state.Stage != "ENDING" || state.Source != "brb" || state.Error == "" {
		t.Fatal("failed ending shut down or lost failure", state)
	}
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal("ending failure retained command lock", w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "stop_now", map[string]any{"confirmed": false}); w.Code != 409 {
		t.Fatal("unconfirmed Stop now accepted")
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "OFF" || state.Pending != nil {
		t.Fatal("confirmed stop did not cancel failed ending", state)
	}
}

func TestEndingValidatesBeforeDiscardingReturnAndStopNowInterrupts(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","ending":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, action := range []string{"prestream", "brb"} {
		if w := stageRequest(t, s, action, map[string]any{"mode": "preview_only"}); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	before := readStage(t, s)
	path := filepath.Join(s.library.root, "revisions", revision+".flv")
	moved := path + ".unavailable"
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if w := stageRequest(t, s, "end_stream", nil); w.Code != 409 {
		t.Fatal("unavailable ending accepted", w.Code, w.Body.String())
	}
	if after := readStage(t, s); after.Stage != "BRB" || after.Context != before.Context || after.ReturnStage != "PRESTREAM" || after.ReturnMedia.Revision != revision {
		t.Fatal("invalid ending discarded current sequence", after)
	}
	if err := os.Rename(moved, path); err != nil {
		t.Fatal(err)
	}
	if w := stageRequest(t, s, "end_stream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "ENDING" || state.ReturnStage != "" {
		t.Fatal("ending retained obsolete return", state)
	}
	if w := stageRequest(t, s, "stop_now", map[string]any{"confirmed": false}); w.Code != 409 {
		t.Fatal("unconfirmed Stop now interrupted ending")
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "OFF" || state.Ending.Completed || state.Ending.Draining {
		t.Fatal("Stop now did not immediately cancel ending", state)
	}
}

func TestEndingDrainReportsDisabledDestinationWithoutClaimingQueuedWrites(t *testing.T) {
	stalled := newDrainDestination(t)
	s, address, media := startDrainRelay(t, []config.Target{stalled.target})
	publisher := startDrainLive(t, s, address, media)
	<-stalled.ready
	writePacket(t, publisher, blockedWriteKeyframe(media.keyframe, time.Second))
	eventually(t, func() bool { return readTargetDashboard(t, s).InputFrames == 2 })
	beginEndingDrain(t, s)
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "stalled", "enabled": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool { return readStage(t, s).Ending.Completed })
	if result, done := endingDestination(readStage(t, s), "stalled"); !done || result.Complete || result.Reason != "disabled" {
		t.Fatal("disabled destination result claimed writes or hid operator cancellation", result)
	}
}
