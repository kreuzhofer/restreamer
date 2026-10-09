package relay

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func TestStageStatusStartsOffWithoutBRB(t *testing.T) {
	s := dashboardServer(t)
	w := dashboardRequest(s, "GET", "/api/stage", "")
	if w.Code != 200 {
		t.Fatalf("stage status: %d %s", w.Code, w.Body.String())
	}
	var state struct {
		Stage, Source, Mode, Context string
		ServerID                     string `json:"server_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Stage != "OFF" || state.Source != "off" || state.Mode != "off" || state.Context == "" || state.ServerID == "" {
		t.Fatalf("unexpected initial stage: %+v", state)
	}
}

func readStage(t *testing.T, s *Server) StageStatus {
	t.Helper()
	w := dashboardRequest(s, "GET", "/api/stage", "")
	if w.Code != 200 {
		t.Fatalf("stage: %d %s", w.Code, w.Body.String())
	}
	var state StageStatus
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func stageRequest(t *testing.T, s *Server, action string, extra map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	state := readStage(t, s)
	body := map[string]any{"id": fmt.Sprintf("test-%d", time.Now().UnixNano()), "server_id": state.ServerID, "context": state.Context, "action": action, "confirmed": true}
	for key, value := range extra {
		body[key] = value
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return dashboardRequest(s, "POST", "/api/stage/commands", string(data))
}

func TestGoLiveWithoutBRBWaitsForFreshInput(t *testing.T) {
	sink := newSink(t)
	s, address := startRelayWithLogger(t, []config.Target{sink.target("one")}, slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	if w := stageRequest(t, s, "go_live", map[string]any{"confirmed": false}); w.Code != 409 {
		t.Fatalf("unconfirmed start: %d %s", w.Code, w.Body.String())
	}
	w := stageRequest(t, s, "go_live", map[string]any{"id": "start-once"})
	if w.Code != 202 {
		t.Fatalf("pending start: %d %s", w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "OFF" || s.forwarding.Load() || sink.count() != 0 {
		t.Fatal("unready start enabled delivery", state)
	}
	publisher := publishInput(t, address)
	writePacket(t, publisher, videoConfig())
	writePacket(t, publisher, audioConfig())
	writePacket(t, publisher, packet(rtmp.Audio, 0, 0xaf, 1, 1))
	writePacket(t, publisher, keyframe(0))
	eventually(t, func() bool { return readStage(t, s).Stage == "LIVE" })
	if state := readStage(t, s); state.Source != "obs" || state.Mode != "real" {
		t.Fatal("live source", state)
	}
	eventually(t, func() bool { return sink.count() == 1 })
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatalf("stop: %d %s", w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "OFF" || s.forwarding.Load() {
		t.Fatal("stop failed", state)
	}
}

func TestPrestreamLoopsWithoutGrantingLiveIntent(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only", "revision": revision}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	now := time.Now()
	for i := 0; i < 190; i++ {
		s.broadcast.tick(now.Add(time.Duration(i) * 20 * time.Millisecond))
	}
	if state := readStage(t, s); state.Stage != "PRESTREAM" || state.Source != "file" || state.Mode != "preview_only" || s.forwarding.Load() {
		t.Fatal("prestream did not remain isolated and on air", state)
	}
	s.broadcast.ingest(videoConfig(), now)
	s.broadcast.ingest(audioConfig(), now)
	s.broadcast.ingest(packet(rtmp.Audio, 0, 0xaf, 1, 1), now)
	s.broadcast.ingest(keyframe(0), now)
	if state := readStage(t, s); state.Source != "file" {
		t.Fatal("OBS leaked into prestream", state)
	}
	if w := stageRequest(t, s, "go_live", map[string]any{"id": "timeout-live"}); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.broadcast.tick(time.Now().Add(11 * time.Second))
	if state := readStage(t, s); state.Stage != "PRESTREAM" || state.Pending != nil {
		t.Fatal("timeout disturbed prestream", state)
	}
	s.broadcast.ingest(keyframe(time.Second), time.Now().Add(12*time.Second))
	if state := readStage(t, s); state.Stage != "PRESTREAM" {
		t.Fatal("expired request selected OBS", state)
	}
	w := dashboardRequest(s, "GET", "/api/stage/commands/timeout-live", "")
	var outcome CommandResult
	if json.Unmarshal(w.Body.Bytes(), &outcome) != nil || outcome.State != "timed_out" {
		t.Fatal("missing timeout outcome", w.Body.String())
	}
}

func TestDeliberateBRBReturnsToPrestreamRevision(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "brb", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.broadcast.tick(time.Now())
	s.broadcast.ingest(videoConfig(), time.Now())
	s.broadcast.ingest(audioConfig(), time.Now())
	s.broadcast.ingest(packet(rtmp.Audio, 0, 0xaf, 1, 1), time.Now())
	s.broadcast.ingest(keyframe(0), time.Now())
	if state := readStage(t, s); state.Stage != "BRB" || state.Source != "brb" {
		t.Fatal("OBS overrode deliberate BRB", state)
	}
	if w := stageRequest(t, s, "return", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.broadcast.tick(time.Now())
	if state := readStage(t, s); state.Stage != "PRESTREAM" || state.Media.Revision != revision || state.Source != "file" || !state.Playback.Loop {
		t.Fatal("Return lost prestream", state)
	}
}

func TestClipCompletionRestoresPrestreamAndRetainsPendingGoLive(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "play_clip", map[string]any{"revision": revision}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "CLIP" || state.ReturnStage != "PRESTREAM" {
		t.Fatal("clip missing return context", state)
	}
	if w := stageRequest(t, s, "go_live", map[string]any{"id": "clip-live"}); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := readStage(t, s).Pending.Deadline
	now := time.Now()
	for i := 0; i < 175; i++ {
		s.broadcast.tick(now.Add(time.Duration(i) * 20 * time.Millisecond))
	}
	if state := readStage(t, s); state.Stage != "PRESTREAM" || state.Media.Revision != revision || state.Pending == nil || state.Pending.Deadline != deadline {
		t.Fatal("clip completion lost normal return or pending request", state)
	}
	s.broadcast.tick(time.UnixMilli(deadline + 1))
	if state := readStage(t, s); state.Stage != "PRESTREAM" || state.Pending != nil {
		t.Fatal("timeout rewound the completed clip", state)
	}
}

func TestPausedClipSurvivesDeliberateBRBAndRepeatedSelection(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, cmd := range []struct {
		action string
		extra  map[string]any
	}{
		{"prestream", map[string]any{"mode": "preview_only"}},
		{"play_clip", map[string]any{"revision": revision, "loop": true}},
		{"pause", map[string]any{"confirmed": false}},
		{"seek", map[string]any{"position": 1.5, "confirmed": false}},
	} {
		if w := stageRequest(t, s, cmd.action, cmd.extra); w.Code != 200 {
			t.Fatal(cmd.action, w.Code, w.Body.String())
		}
	}
	paused := readStage(t, s)
	if paused.Playback.State != "paused" || paused.Playback.Position < 0.5 || !paused.Playback.Loop {
		t.Fatal("pause or seek failed", paused)
	}
	if w := stageRequest(t, s, "play_clip", map[string]any{"revision": revision, "confirmed": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Playback.Position != paused.Playback.Position || state.Playback.State != "paused" || !state.Playback.Loop {
		t.Fatal("same clip restarted or changed its loop", state)
	}
	if w := stageRequest(t, s, "brb", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := dashboardRequest(s, "GET", "/api/stage", "")
	var suspended struct {
		ReturnPlayback *PlaybackStatus `json:"return_playback"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &suspended); err != nil {
		t.Fatal(err)
	}
	if p := suspended.ReturnPlayback; p == nil || p.State != "paused" || p.PauseReason != "user" || p.Position != paused.Playback.Position || !p.Loop {
		t.Fatal("Suspended playback lost explicit pause status", w.Body.String())
	}
	if w := stageRequest(t, s, "return", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "CLIP" || state.Playback.State != "paused" || state.Playback.Position != paused.Playback.Position || state.ReturnStage != "PRESTREAM" {
		t.Fatal("BRB lost paused clip context", state)
	}
	if w := stageRequest(t, s, "resume", map[string]any{"confirmed": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "stop_clip", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "PRESTREAM" {
		t.Fatal("Stop clip failed to return", state)
	}
}
