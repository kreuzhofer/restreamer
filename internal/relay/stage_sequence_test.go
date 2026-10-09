package relay

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func establishSequenceLive(t *testing.T, s *Server) {
	t.Helper()
	if w := stageRequest(t, s, "go_live", map[string]any{"mode": "preview_only"}); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	sequenceFreshOBS(s, time.Now())
	if state := readStage(t, s); state.Stage != "LIVE" || state.Source != "obs" {
		t.Fatal("LIVE not established", state)
	}
}

func sequenceFreshOBS(s *Server, now time.Time) {
	s.broadcast.ingest(videoConfig(), now)
	s.broadcast.ingest(audioConfig(), now)
	s.broadcast.ingest(packet(rtmp.Audio, 0, 0xaf, 1, 1), now)
	s.broadcast.ingest(keyframe(0), now)
}

func completeSequenceClip(s *Server) {
	now := time.Now()
	// Advance the approved sequencer clock in bounded increments; jumping straight
	// to EOF intentionally exercises process suspension, not ordinary completion.
	for i := 0; i < 300; i++ {
		s.broadcast.tick(now.Add(time.Duration(i) * 20 * time.Millisecond))
	}
}

func TestRepeatedClipsWithinDeliberateBRBRetainOneReturnToLive(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	establishSequenceLive(t, s)
	if w := stageRequest(t, s, "brb", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for i := 0; i < 4; i++ {
		if w := stageRequest(t, s, "play_clip", map[string]any{"revision": revision}); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if w := stageRequest(t, s, "brb", nil); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if state := readStage(t, s); state.ReturnAfterDiscard != "LIVE" {
			t.Fatal("Discard confirmation lost its final return destination", state)
		}
	}
	if w := stageRequest(t, s, "play_clip", map[string]any{"revision": revision}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	completeSequenceClip(s)
	if state := readStage(t, s); state.Stage != "BRB" || state.ReturnStage != "LIVE" {
		t.Fatal("Repeated breaks stacked duplicate BRB destinations", state)
	}
	if w := stageRequest(t, s, "return", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "LIVE" || state.ReturnStage != "" {
		t.Fatal("Return did not leave the deliberate break", state)
	}
}

func TestClipReplacementReturnsToEstablishedLiveAndRecoversOBS(t *testing.T) {
	s, a := preparedRevisionServer(t)
	b := uploadReplacementRevision(t, s)
	establishSequenceLive(t, s)
	for _, revision := range []string{a, b} {
		if w := stageRequest(t, s, "play_clip", map[string]any{"revision": revision}); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if state := readStage(t, s); state.Media.Revision != b || state.ReturnStage != "LIVE" {
		t.Fatal("B did not replace A while retaining LIVE return", state)
	}
	completeSequenceClip(s)
	state := readStage(t, s)
	if state.Stage != "LIVE" || state.Source != "brb" || state.Media.Revision != "" || state.ReturnStage != "" || state.Error == "" {
		t.Fatal("normal completion must hold fallback with LIVE intent and an unavailable-source reason", state)
	}
	sequenceFreshOBS(s, time.Now().Add(7*time.Second))
	if state := readStage(t, s); state.Stage != "LIVE" || state.Source != "obs" || state.Error != "" {
		t.Fatal("established LIVE did not recover automatically", state)
	}
}

func TestDeliberateBRBResumesClipThenReturnsToLive(t *testing.T) {
	s, a := preparedRevisionServer(t)
	establishSequenceLive(t, s)
	if w := stageRequest(t, s, "play_clip", map[string]any{"revision": a}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "seek", map[string]any{"position": 1.5, "confirmed": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := readStage(t, s)
	if w := stageRequest(t, s, "brb", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	sequenceFreshOBS(s, time.Now())
	state := readStage(t, s)
	if state.Stage != "BRB" || state.Source != "brb" || state.ReturnMedia == nil || state.ReturnMedia.Revision != a {
		t.Fatal("BRB did not retain suspended clip or exposed OBS", state)
	}
	if p := state.ReturnPlayback; p == nil || p.State != "suspended" || p.PauseReason != "suspended" || p.ReturnStage != "LIVE" {
		t.Fatal("BRB suspension was not distinct from user pause", state)
	}
	if w := stageRequest(t, s, "return", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state = readStage(t, s)
	if state.Stage != "CLIP" || state.Media.Revision != a || state.ReturnStage != "LIVE" || state.Playback.Position < before.Playback.Position-1.1 {
		t.Fatal("Return lost clip position or original LIVE return", state)
	}
	completeSequenceClip(s)
	if state := readStage(t, s); state.Stage != "LIVE" || state.ReturnStage != "" {
		t.Fatal("resumed clip did not complete to LIVE", state)
	}
}

func TestNewClipDuringBRBDiscardsSuspendedClipButKeepsOriginalReturn(t *testing.T) {
	s, a := preparedRevisionServer(t)
	b := uploadReplacementRevision(t, s)
	establishSequenceLive(t, s)
	if w := stageRequest(t, s, "play_clip", map[string]any{"revision": a}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "brb", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "play_clip", map[string]any{"revision": b}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "CLIP" || state.Media.Revision != b || state.ReturnStage != "BRB" {
		t.Fatal("B did not interrupt BRB", state)
	}
	completeSequenceClip(s)
	if state := readStage(t, s); state.Stage != "BRB" || state.ReturnStage != "LIVE" || state.ReturnMedia != nil {
		t.Fatal("B resumed discarded A instead of BRB with LIVE return", state)
	}
	sequenceFreshOBS(s, time.Now().Add(7*time.Second))
	if state := readStage(t, s); state.Source != "brb" {
		t.Fatal("OBS escaped deliberate BRB", state)
	}
	if w := stageRequest(t, s, "return", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "LIVE" || state.ReturnStage != "" || state.Media.Revision != "" {
		t.Fatal("Return resurrected discarded clip", state)
	}
}

func TestUnavailablePrestreamReturnHoldsFallbackUntilExplicitRetry(t *testing.T) {
	s, a := preparedRevisionServer(t)
	b := uploadReplacementRevision(t, s)
	startReplacementPrestream(t, s, a)
	if w := stageRequest(t, s, "play_clip", map[string]any{"revision": b}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	path := filepath.Join(s.library.root, "revisions", a+".flv")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	completeSequenceClip(s)
	state := readStage(t, s)
	if state.Stage != "PRESTREAM" || state.Source != "brb" || state.Media.Revision != a || state.Error == "" {
		t.Fatal("unavailable prepared return did not retain intent and actionable failure", state)
	}
	// Restoring storage alone must not silently resume prepared media or expose OBS.
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	sequenceFreshOBS(s, time.Now().Add(7*time.Second))
	s.broadcast.tick(time.Now().Add(7 * time.Second))
	if state := readStage(t, s); state.Stage != "PRESTREAM" || state.Source != "brb" || state.Error == "" {
		t.Fatal("failed prepared return recovered without explicit Retry", state)
	}
	if w := stageRequest(t, s, "retry", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "PRESTREAM" || state.Source != "file" || state.Media.Revision != a || state.Error != "" || !state.Playback.Loop {
		t.Fatal("explicit Retry did not restore selected prestream", state)
	}
}

func TestSuccessfulBaseStageSelectionDiscardsSuspendedClipHistory(t *testing.T) {
	for _, action := range []string{"prestream", "go_live", "end_stream"} {
		t.Run(action, func(t *testing.T) {
			s, a := preparedRevisionServer(t)
			if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+a+`","ending":"`+a+`","shortcuts":[]}`); w.Code != 204 {
				t.Fatal(w.Code, w.Body.String())
			}
			establishSequenceLive(t, s)
			if w := stageRequest(t, s, "play_clip", map[string]any{"revision": a}); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := stageRequest(t, s, "brb", nil); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			w := stageRequest(t, s, action, nil)
			wantStage := "PRESTREAM"
			if action == "go_live" {
				if w.Code != 202 {
					t.Fatal(w.Code, w.Body.String())
				}
				if state := readStage(t, s); state.ReturnStage != "CLIP" || state.ReturnMedia == nil || state.ReturnMedia.Revision != a {
					t.Fatal("pending Go live prematurely discarded suspended clip", state)
				}
				sequenceFreshOBS(s, time.Now())
				wantStage = "LIVE"
			} else {
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				if action == "end_stream" {
					wantStage = "ENDING"
				}
			}
			state := readStage(t, s)
			if state.Stage != wantStage || state.ReturnStage != "" || state.ReturnMedia != nil {
				t.Fatal("successful base selection retained temporary history", state)
			}
			if w := stageRequest(t, s, "return", nil); w.Code != 409 {
				t.Fatal("discarded return remained executable", w.Code, w.Body.String())
			}
		})
	}
}

func TestTimedOutGoLiveRetainsSuspendedClipAndItsReturn(t *testing.T) {
	s, a := preparedRevisionServer(t)
	establishSequenceLive(t, s)
	if w := stageRequest(t, s, "play_clip", map[string]any{"revision": a}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "pause", map[string]any{"confirmed": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "seek", map[string]any{"position": 1.5, "confirmed": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := readStage(t, s)
	if w := stageRequest(t, s, "brb", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.broadcast.tick(time.Now().Add(11 * time.Second))
	if state := readStage(t, s); state.Stage != "BRB" || state.Pending != nil || state.ReturnMedia == nil || state.ReturnMedia.Revision != a {
		t.Fatal("Go live timeout discarded suspended clip", state)
	}
	sequenceFreshOBS(s, time.Now().Add(12*time.Second))
	if state := readStage(t, s); state.Stage != "BRB" || state.Source != "brb" {
		t.Fatal("late OBS fulfilled expired Go live", state)
	}
	if w := stageRequest(t, s, "return", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "CLIP" || state.Media.Revision != a || state.Playback.State != "paused" || state.Playback.Position != before.Playback.Position || state.ReturnStage != "LIVE" {
		t.Fatal("timeout changed saved clip or original return", state)
	}
}
