package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func uploadReplacementRevision(t *testing.T, s *Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "replacement.mp4")
	if output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:size=320x180:rate=25", "-t", "4", "-c:v", "libx264", "-threads", "2", path).CombinedOutput(); err != nil {
		t.Fatalf("replacement fixture: %v %s", err, output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if w := videoUpload(t, s, "replacement.mp4", data); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.library.scan()
	var revision string
	eventually(t, func() bool {
		for _, raw := range libraryHTTPStatus(t, s)["files"].([]any) {
			file := raw.(map[string]any)
			if file["name"] == "replacement.mp4" && file["state"] == "ready" {
				revision, _ = file["revision"].(string)
				return revision != ""
			}
		}
		return false
	})
	return revision
}

func startReplacementPrestream(t *testing.T, s *Server, revision string) {
	t.Helper()
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","ending":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestPrestreamReplacementConfirmsExactCandidateAndDeduplicates(t *testing.T) {
	s, original := preparedRevisionServer(t)
	startReplacementPrestream(t, s, original)
	candidate := uploadReplacementRevision(t, s)
	if state := readStage(t, s); state.Media.Revision != original {
		t.Fatal("preparation replaced the active revision", state)
	}
	if w := dashboardRequest(s, "GET", "/api/library/revisions/"+candidate+"/preview", ""); w.Code != 200 {
		t.Fatal("candidate cannot be reviewed", w.Code)
	}
	if w := stageRequest(t, s, "go_live", map[string]any{"id": "replace-pending"}); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "replace_now", map[string]any{"revision": candidate, "confirmed": false}); w.Code != 409 {
		t.Fatal("replacement accepted without confirmation", w.Code)
	}
	if w := stageRequest(t, s, "replace_now", map[string]any{"revision": "missing"}); w.Code != 409 {
		t.Fatal("unready replacement accepted", w.Code)
	}
	if state := readStage(t, s); state.Pending == nil || state.Media.Revision != original {
		t.Fatal("invalid replacement disturbed current sequence", state)
	}
	reviewed := readStage(t, s)
	body, err := json.Marshal(StageCommand{ID: "replace-once", ServerID: reviewed.ServerID, Context: reviewed.Context, Action: "replace_now", Confirmed: true, Revision: candidate})
	if err != nil {
		t.Fatal(err)
	}
	// Timed ticks are the existing sequencer seam: ordinary progress must not
	// invalidate a confirmation or make it select some later library revision.
	s.broadcast.tick(time.Now().Add(500 * time.Millisecond))
	if w := dashboardRequest(s, "POST", "/api/stage/commands", string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	after := readStage(t, s)
	if after.Stage != "PRESTREAM" || after.Media.Revision != candidate || !after.Playback.Loop || after.Playback.Position > .1 || after.Pending != nil || after.Mode != "preview_only" {
		t.Fatal("replacement lost stage or did not restart exact media", after)
	}
	if w := dashboardRequest(s, "POST", "/api/stage/commands", string(body)); w.Code != 200 {
		t.Fatal("lost-response replay failed", w.Code, w.Body.String())
	}
	if replay := readStage(t, s); replay.Playback.Epoch != after.Playback.Epoch || replay.Context != after.Context {
		t.Fatal("duplicate replacement restarted playback", replay)
	}
	if w := dashboardRequest(s, "GET", "/api/stage/commands/replace-pending", ""); w.Code != 200 || !json.Valid(w.Body.Bytes()) {
		t.Fatal(w.Code, w.Body.String())
	}
	if s.forwarding.Load() {
		t.Fatal("replacement escaped rehearsal")
	}
}

func TestClipReplacementPreservesPauseLoopAndReturn(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(map[bool]string{false: "playing", true: "paused"}[paused], func(t *testing.T) {
			s, original := preparedRevisionServer(t)
			candidate := uploadReplacementRevision(t, s)
			startReplacementPrestream(t, s, original)
			if w := stageRequest(t, s, "play_clip", map[string]any{"revision": original, "loop": true}); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if paused {
				if w := stageRequest(t, s, "pause", map[string]any{"confirmed": false}); w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			if w := stageRequest(t, s, "seek", map[string]any{"position": 1.5, "confirmed": false}); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := stageRequest(t, s, "replace_now", map[string]any{"revision": candidate}); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			state := readStage(t, s)
			if state.Stage != "CLIP" || state.Media.Revision != candidate || state.ReturnStage != "PRESTREAM" || state.ReturnMedia == nil || state.ReturnMedia.Revision != original || !state.Playback.Loop || state.Playback.Position > .1 {
				t.Fatal("replacement lost clip sequence", state)
			}
			wantState := "playing"
			if paused {
				wantState = "paused"
			}
			if state.Playback.State != wantState {
				t.Fatal("replacement changed explicit pause state", state)
			}
			if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
				t.Fatal(w.Code, w.Body.String())
			}
			before := readStage(t, s)
			if w := stageRequest(t, s, "replace_now", map[string]any{"revision": candidate, "confirmed": false}); w.Code != 200 {
				t.Fatal("same revision was not a noop", w.Code, w.Body.String())
			}
			if after := readStage(t, s); after.Context != before.Context || after.Playback.Epoch != before.Playback.Epoch || after.Pending == nil {
				t.Fatal("same revision disturbed playback or pending transition", after)
			}
			if w := stageRequest(t, s, "stop_clip", nil); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if state := readStage(t, s); state.Stage != "PRESTREAM" || state.Media.Revision != original {
				t.Fatal("replacement changed the original return source", state)
			}
		})
	}
}

func TestReplaceSuspendedPrestreamLeavesClipAndPendingTransitionUninterrupted(t *testing.T) {
	s, original := preparedRevisionServer(t)
	candidate := uploadReplacementRevision(t, s)
	startReplacementPrestream(t, s, original)
	if w := stageRequest(t, s, "play_clip", map[string]any{"revision": original}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := readStage(t, s)
	if w := stageRequest(t, s, "replace_on_return", map[string]any{"revision": candidate}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	after := readStage(t, s)
	if after.Stage != "CLIP" || after.Media.Revision != original || after.Playback.Epoch != before.Playback.Epoch || after.ReturnMedia == nil || after.ReturnMedia.Revision != candidate || after.Pending == nil || after.Pending.Deadline != before.Pending.Deadline {
		t.Fatal("return replacement disturbed the active clip", after)
	}
	now := time.Now()
	for i := 0; i < 165; i++ {
		s.broadcast.tick(now.Add(time.Duration(i) * 20 * time.Millisecond))
	}
	state := readStage(t, s)
	if state.Stage != "PRESTREAM" || state.Media.Revision != candidate || !state.Playback.Loop || state.Pending == nil {
		t.Fatal("clip completion did not restore replacement", state)
	}
	// The new source starts at zero rather than inheriting the suspended
	// source's elapsed time or the duration of the temporary clip.
	if state.Playback.Position > .1 {
		t.Fatal("return replacement did not start at its beginning", state)
	}
}

func TestReplaceSuspendedClipPreservesBRBPauseLoopAndOriginalReturn(t *testing.T) {
	s, original := preparedRevisionServer(t)
	candidate := uploadReplacementRevision(t, s)
	if w := stageRequest(t, s, "go_live", map[string]any{"mode": "preview_only"}); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Establish live intent through the existing input seam before suspending it.
	now := time.Now()
	s.broadcast.ingest(videoConfig(), now)
	s.broadcast.ingest(audioConfig(), now)
	s.broadcast.ingest(packet(rtmp.Audio, 0, 0xaf, 1, 1), now)
	s.broadcast.ingest(keyframe(0), now)
	if state := readStage(t, s); state.Stage != "LIVE" {
		t.Fatal(state)
	}
	if w := stageRequest(t, s, "play_clip", map[string]any{"revision": original, "loop": true}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "pause", map[string]any{"confirmed": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "seek", map[string]any{"position": 1.5, "confirmed": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "brb", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := readStage(t, s)
	if w := stageRequest(t, s, "replace_on_return", map[string]any{"revision": candidate}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "BRB" || state.Source != "brb" || state.Playback.Epoch != before.Playback.Epoch || state.ReturnMedia == nil || state.ReturnMedia.Revision != candidate {
		t.Fatal("replacement interrupted deliberate BRB", state)
	}
	if w := stageRequest(t, s, "return", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state := readStage(t, s)
	if state.Stage != "CLIP" || state.Media.Revision != candidate || state.Playback.State != "paused" || state.Playback.Position != 0 || !state.Playback.Loop || state.ReturnStage != "LIVE" {
		t.Fatal("Return lost paused replacement context", state)
	}
	if w := stageRequest(t, s, "stop_clip", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "LIVE" {
		t.Fatal("replacement lost established live return", state)
	}
}

func TestReplacementRejectsStaleActiveAndSuspendedContexts(t *testing.T) {
	s, original := preparedRevisionServer(t)
	candidate := uploadReplacementRevision(t, s)
	startReplacementPrestream(t, s, original)
	reviewed := readStage(t, s)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+candidate+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "replace_now", map[string]any{"revision": candidate, "context": reviewed.Context}); w.Code != 409 {
		t.Fatal("stale configured-selection confirmation accepted", w.Code)
	}
	if w := stageRequest(t, s, "play_clip", map[string]any{"revision": original}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	reviewed = readStage(t, s)
	if w := stageRequest(t, s, "stop_clip", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "replace_on_return", map[string]any{"revision": candidate, "context": reviewed.Context}); w.Code != 409 {
		t.Fatal("discarded suspended context accepted replacement", w.Code)
	}
	if state := readStage(t, s); state.Media.Revision != original {
		t.Fatal("stale replacement affected current source", state)
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "replace_now", map[string]any{"revision": candidate}); w.Code != 409 {
		t.Fatal("candidate reopened stopped broadcast", w.Code)
	}
}

func TestEndingReplacementNearEOFDeduplicatesAndPostponesShutdown(t *testing.T) {
	s, original := preparedRevisionServer(t)
	candidate := uploadReplacementRevision(t, s)
	startReplacementPrestream(t, s, original)
	if w := stageRequest(t, s, "end_stream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	reviewed := readStage(t, s)
	body, err := json.Marshal(StageCommand{ID: "replace-ending-once", ServerID: reviewed.ServerID, Context: reviewed.Context, Action: "replace_now", Confirmed: true, Revision: candidate})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 140; i++ {
		s.broadcast.tick(now.Add(time.Duration(i) * 20 * time.Millisecond))
	}
	if w := dashboardRequest(s, "POST", "/api/stage/commands", string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	after := readStage(t, s)
	if after.Stage != "ENDING" || after.Media.Revision != candidate || after.Playback.Loop || after.Playback.Position > .1 {
		t.Fatal("replacement did not restart ending", after)
	}
	if w := stageRequest(t, s, "brb", nil); w.Code != 409 {
		t.Fatal("replacement released ending protection", w.Code)
	}
	if w := dashboardRequest(s, "POST", "/api/stage/commands", string(body)); w.Code != 200 {
		t.Fatal("ending lost-response reconciliation failed", w.Code, w.Body.String())
	}
	if replay := readStage(t, s); replay.Playback.Epoch != after.Playback.Epoch || replay.Context != after.Context {
		t.Fatal("duplicate replacement postponed shutdown again", replay)
	}
	now = time.Now()
	for i := 0; i < 165; i++ {
		s.broadcast.tick(now.Add(time.Duration(i) * 20 * time.Millisecond))
	}
	if state := readStage(t, s); state.Stage != "ENDING" {
		t.Fatal("original ending deadline stopped replacement", state)
	}
	for i := 165; i < 225; i++ {
		s.broadcast.tick(now.Add(time.Duration(i) * 20 * time.Millisecond))
	}
	if state := readStage(t, s); state.Stage != "OFF" || !state.Ending.Completed {
		t.Fatal("replacement did not complete ending", state)
	}
	if w := stageRequest(t, s, "replace_now", map[string]any{"revision": original}); w.Code != 409 {
		t.Fatal("late candidate reopened completed ending", w.Code)
	}
}

func TestStageReplacementsKeepOneDecodableDestinationSession(t *testing.T) {
	prepared, original := preparedRevisionServer(t)
	candidate := uploadReplacementRevision(t, prepared)
	destination := newSink(t)
	cfg := prepared.cfg
	cfg.Targets = []config.Target{destination.target("local")}
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
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+original+`","ending":"`+original+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, command := range []struct {
		action string
		extra  map[string]any
	}{
		{"replace_now", map[string]any{"revision": candidate}},
		{"play_clip", map[string]any{"revision": original}},
		{"replace_now", map[string]any{"revision": candidate}},
		{"end_stream", nil},
		{"replace_now", map[string]any{"revision": candidate}},
	} {
		before := readTargetDashboard(t, s).Outputs[0].Frames
		eventually(t, func() bool { return readTargetDashboard(t, s).Outputs[0].Frames >= before+10 })
		if w := stageRequest(t, s, command.action, command.extra); w.Code != 200 {
			t.Fatal(command.action, w.Code, w.Body.String())
		}
	}
	eventually(t, func() bool { return readStage(t, s).Stage == "OFF" })
	if !readStage(t, s).Ending.Completed {
		t.Fatal("ending replacement did not finish delivery")
	}
	if destination.count() != 1 {
		t.Fatal("replacement reconnected destination", destination.count())
	}
	var packets []*rtmp.Message
	var previous [2]time.Duration
	videoHeaders := 0
	for len(destination.packets) > 0 {
		message := <-destination.packets
		packets = append(packets, message)
		if message.Type == rtmp.Video && len(message.Body) > 1 && message.Body[1] == 0 {
			videoHeaders++
		}
		if isVideoFrame(message) || message.Type == rtmp.Audio && len(message.Body) > 1 && message.Body[1] == 1 {
			track := 0
			if message.Type == rtmp.Audio {
				track = 1
			}
			if message.Timestamp < previous[track] {
				t.Fatal("replacement reversed output media timestamps", message.Timestamp, previous[track])
			}
			previous[track] = message.Timestamp
		}
	}
	if videoHeaders < 6 {
		t.Fatal("not every switched source reached destination with new headers", videoHeaders)
	}
	path := filepath.Join(t.TempDir(), "replacements.flv")
	writeTestFLV(t, path, packets)
	if output, err := exec.Command("ffmpeg", "-hide_banner", "-v", "error", "-xerror", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil {
		t.Fatalf("replacement stream did not decode: %v %s", err, output)
	}
}

func TestFailedPreparationLeavesActiveRevisionAndPendingTransitionUntouched(t *testing.T) {
	s, original := preparedRevisionServer(t)
	startReplacementPrestream(t, s, original)
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := readStage(t, s)
	if w := videoUpload(t, s, "broken.mp4", []byte("not an MP4")); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.library.scan()
	eventually(t, func() bool {
		for _, raw := range libraryHTTPStatus(t, s)["files"].([]any) {
			file := raw.(map[string]any)
			if file["name"] == "broken.mp4" {
				return file["state"] == "failed" && file["error"] != ""
			}
		}
		return false
	})
	if after := readStage(t, s); after.Media.Revision != original || after.Context != before.Context || after.Playback.Epoch != before.Playback.Epoch || after.Pending == nil || after.Pending.Deadline != before.Pending.Deadline || after.Error != "" {
		t.Fatal("failed background preparation disturbed active sequence", after)
	}
}

func TestClosedUploadClientAndLeftStageCannotActivatePreparedCandidate(t *testing.T) {
	prepared, original := preparedRevisionServer(t)
	// This server owns a fresh preparation queue; deliberately start its worker
	// after the operator has left the stage, as a slow accepted job could do.
	s := New(prepared.cfg, prepared.log)
	startReplacementPrestream(t, s, original)
	path := filepath.Join(t.TempDir(), "late.mp4")
	if output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=green:size=320x180:rate=25", "-t", "2", "-c:v", "libx264", "-threads", "2", path).CombinedOutput(); err != nil {
		t.Fatalf("late fixture: %v %s", err, output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "late.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	client, closeClient := context.WithCancel(context.Background())
	request := httptest.NewRequest("POST", "/api/library/upload", &body).WithContext(client)
	request.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-Restreamer-Control", "1")
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}
	closeClient()
	if state := readStage(t, s); state.Media.Revision != original {
		t.Fatal("accepted upload replaced current source", state)
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.library.worker(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	s.library.scan()
	var candidate string
	eventually(t, func() bool {
		for _, raw := range libraryHTTPStatus(t, s)["files"].([]any) {
			file := raw.(map[string]any)
			if file["name"] == "late.mp4" && file["state"] == "ready" {
				candidate, _ = file["revision"].(string)
				return candidate != ""
			}
		}
		return false
	})
	if state := readStage(t, s); state.Stage != "OFF" {
		t.Fatal("late preparation reopened stopped stage", state)
	}
	if w := dashboardRequest(s, "GET", "/api/library/revisions/"+candidate+"/preview", ""); w.Code != 200 {
		t.Fatal("closed client lost its server-owned ready candidate", w.Code, w.Body.String())
	}
}

func TestSameFilenamePreparationPinsActiveRevisionUntilConfirmedReplacement(t *testing.T) {
	s, original := preparedRevisionServer(t)
	candidate := uploadReplacementRevision(t, s)
	startReplacementPrestream(t, s, original)
	data, err := os.ReadFile(filepath.Join(s.library.root, "originals", "replacement.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	// File-management edits may replace original content under its old name;
	// the selected prepared revision must still remain pinned independently.
	if err := os.WriteFile(filepath.Join(s.library.root, "originals", "show.mp4"), data, 0600); err != nil {
		t.Fatal(err)
	}
	s.library.scan()
	s.library.scan() // A second discovery pass confirms the changed file is stable.
	eventually(t, func() bool {
		for _, raw := range libraryHTTPStatus(t, s)["files"].([]any) {
			file := raw.(map[string]any)
			if file["name"] == "show.mp4" {
				return file["state"] == "ready" && file["revision"] == candidate
			}
		}
		return false
	})
	if state := readStage(t, s); state.Media.Revision != original {
		t.Fatal("same-filename preparation changed active media", state)
	}
	if w := dashboardRequest(s, "GET", "/api/library/revisions/"+original+"/preview", ""); w.Code != 200 {
		t.Fatal("same-filename edit lost pinned revision", w.Code)
	}
	if w := stageRequest(t, s, "replace_now", map[string]any{"revision": candidate}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Media.Revision != candidate || state.Stage != "PRESTREAM" {
		t.Fatal("confirmed same-filename replacement did not activate exact content", state)
	}
}
