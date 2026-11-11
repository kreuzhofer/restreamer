package relay

import (
	"bytes"
	"encoding/json"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBRBGenerationCapturesDraftWithoutActivatingAppearance(t *testing.T) {
	s := libraryServer(t)
	before := s.broadcast.media.settings.Generation
	body := `{"base_generation":"` + before + `","name":"Workshop break","duration_mode":"fixed","theme":{"id":"arcade-after-hours","revision":2},"text":"BACK SOON"}`
	w := dashboardRequest(s, "POST", "/api/brb/presentations", body)
	if w.Code != 202 {
		t.Fatalf("draft generation rejected: %d %s", w.Code, w.Body.String())
	}
	var job GenerationJob
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.BRBDraft == nil || job.BRBDraft.Text != "BACK SOON" || job.ThemeSnapshot.ID != "arcade-after-hours" {
		t.Fatalf("draft not captured: %+v", job)
	}
	if s.broadcast.media.settings.Generation != before {
		t.Fatal("generation activated the draft")
	}
	if !jobDesignBounds(job) {
		t.Fatal("captured draft cannot survive restart")
	}
}

func TestFallbackSelectionUsesSharedOrExplicitOverride(t *testing.T) {
	for _, tc := range []struct {
		selections StageMediaSelections
		want       string
	}{
		{StageMediaSelections{BRB: "break", FallbackMode: "shared"}, "break"},
		{StageMediaSelections{BRB: "break", FallbackMode: "override", Fallback: "offline"}, "offline"},
		{StageMediaSelections{BRB: "break"}, ""},
	} {
		if got := tc.selections.fallbackRevision(); got != tc.want {
			t.Fatalf("got %q want %q", got, tc.want)
		}
	}
}

func TestSharedFallbackPinsCurrentSelectionAndFailsToEmergency(t *testing.T) {
	s, first := preparedRevisionServer(t)
	second := uploadReplacementRevision(t, s)
	selectBRB := func(id string) {
		t.Helper()
		w := dashboardRequest(s, "PUT", "/api/stage-media", `{"brb":"`+id+`","fallback_mode":"shared","shortcuts":[]}`)
		if w.Code != 204 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	selectBRB(first)
	b := s.broadcast
	now := time.Now()
	b.mu.Lock()
	b.control.stage = "LIVE"
	b.control.mode = "preview_only"
	b.startFallback(now)
	b.mu.Unlock()
	b.tick(now)
	if got := readStage(t, s); got.Fallback == nil || got.Fallback.Revision != first {
		t.Fatalf("shared fallback not active: %+v", got)
	}
	selectBRB(second)
	if got := readStage(t, s); got.Fallback.Revision != first {
		t.Fatal("selection replaced active fallback")
	}
	b.mu.Lock()
	b.fallback.reader.close()
	b.mu.Unlock()
	b.tick(now.Add(time.Second))
	got := readStage(t, s)
	if !got.Emergency || got.FallbackError == "" {
		t.Fatalf("missing emergency state: %+v", got)
	}
	b.tick(now.Add(2 * time.Second))
	if !readStage(t, s).Emergency {
		t.Fatal("failed fallback retried without action")
	}
	if w := stageRequest(t, s, "replace_fallback", map[string]any{"revision": second}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	b.tick(time.Now())
	if readStage(t, s).Emergency {
		t.Fatal("explicit recovery did not clear emergency")
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestSavedBRBDesignGeneratesAppearanceAndMusicTogether(t *testing.T) {
	s := libraryServer(t)
	w := dashboardRequest(s, "POST", "/api/generator/designs", `{"name":"Break draft","stage":"brb","theme":{"id":"arcade-after-hours","revision":2},"duration_mode":"fixed","scenes":[{"id":"brb","layout":"title","text":"BACK SOON","duration_seconds":4}]}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var d mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &d)
	raw, _ := json.Marshal(d)
	preview := dashboardRequest(s, "POST", "/api/generator/preview", string(raw))
	if preview.Code != 200 || preview.Header().Get("Content-Type") != "image/png" {
		t.Fatal("BRB draft preview failed", preview.Code, preview.Body.String())
	}
	generatorServe(t, s)
	j := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if j.State != "ready" {
		t.Fatal(j.Error)
	}
	if j.Design.ID != d.ID || j.Design.Stage != "brb" {
		t.Fatal("generation lost design identity")
	}
	if s.library.selections().BRB != "" || s.broadcast.control.stage != "OFF" {
		t.Fatal("generation changed selection or playback")
	}
	rename := dashboardRequest(s, "PUT", "/api/generator/jobs/"+j.ID+"/name", `{"name":"Workshop with quiet music"}`)
	if rename.Code != 200 {
		t.Fatal(rename.Code, rename.Body.String())
	}
	var named GenerationJob
	json.Unmarshal(rename.Body.Bytes(), &named)
	if named.Name != "Workshop with quiet music" || named.MediaRevision != j.MediaRevision {
		t.Fatal("rename changed exact output")
	}
}

func TestSuspendedEmergencyRetainsFailureUntilExplicitRecovery(t *testing.T) {
	s, id := preparedRevisionServer(t)
	w := dashboardRequest(s, "PUT", "/api/stage-media", `{"brb":"`+id+`","fallback_mode":"shared","shortcuts":[]}`)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	b := s.broadcast
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.control.stage = "LIVE"
	b.control.mode = "preview_only"
	b.startFallback(now)
	b.fallbackFailure("failed soundtrack", now)
	frame := b.suspendStage(now)
	b.closeFallback()
	b.restoreStage(frame, now.Add(time.Second))
	if !b.fallbackFailed || b.fallbackError != "failed soundtrack" || b.fallback != nil || b.fallbackMedia != b.emergency {
		t.Fatal("return silently retried failed fallback")
	}
	b.closeFallback()
	b.fallbackNext = nil
	b.startFallback(now.Add(2 * time.Second))
	if !b.fallbackFailed || b.fallbackMedia != b.emergency {
		t.Fatal("missing managed fallback silently used legacy media")
	}
}

func TestFallbackReplacementOnReturnKeepsCurrentClipAndNextSelection(t *testing.T) {
	s, first := preparedRevisionServer(t)
	second := uploadReplacementRevision(t, s)
	w := dashboardRequest(s, "PUT", "/api/stage-media", `{"brb":"`+first+`","fallback_mode":"shared","shortcuts":[]}`)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	b := s.broadcast
	b.mu.Lock()
	b.control.stage = "LIVE"
	b.control.mode = "preview_only"
	b.startFallback(time.Now())
	b.mu.Unlock()
	if w = stageRequest(t, s, "play_clip", map[string]any{"revision": first}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := readStage(t, s)
	if w = stageRequest(t, s, "replace_fallback_on_return", map[string]any{"revision": second}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	after := readStage(t, s)
	if before.Media.Revision != after.Media.Revision || s.library.selections().BRB != first {
		t.Fatal("replacement changed current clip or next selection")
	}
	if w = stageRequest(t, s, "return", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if got := readStage(t, s); got.Fallback == nil || got.Fallback.Revision != second {
		t.Fatal("did not return to reviewed fallback", got)
	}
}

func TestSharedFallbackRequiresAnExplicitBRBSelection(t *testing.T) {
	if err := validateSelections(StageMediaSelections{FallbackMode: "shared"}); err == nil {
		t.Fatal("shared fallback silently became legacy media")
	}
}

func TestFallbackMigrationPreservesLegacySelectionAndRestartIsIdempotent(t *testing.T) {
	s, id := preparedRevisionServer(t)
	path := filepath.Join(s.library.root, "stage-media.json")
	original := savedStageMedia{Version: 1, Selections: StageMediaSelections{BRB: id}}
	if err := writeState(path, original); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := s.broadcast.media.settings
	for range 2 {
		restarted := New(s.cfg, s.log)
		if err := restarted.initialize(); err != nil {
			t.Fatal(err)
		}
		if got := restarted.library.selections(); got.BRB != id || got.FallbackMode != "" {
			t.Fatal("legacy selections changed", got)
		}
		if restarted.broadcast.media.settings.Text != legacy.Text || restarted.broadcast.media.settings.Profile != legacy.Profile || restarted.broadcast.fallbackManaged {
			t.Fatal("legacy fallback was replaced")
		}
		if got := readStage(t, restarted); got.Stage != "OFF" {
			t.Fatal("restart began playback", got)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("startup rewrote legacy selections", err)
		}
	}
}

func TestManagedFallbackRestartsAndProtectsSelectedActiveAndSuspendedFiles(t *testing.T) {
	s, id := preparedRevisionServer(t)
	other := uploadReplacementRevision(t, s)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"brb":"`+other+`","fallback_mode":"override","fallback":"`+id+`"}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	restarted := New(s.cfg, s.log)
	if err := restarted.initialize(); err != nil {
		t.Fatal(err)
	}
	if got := readStage(t, restarted); got.Stage != "OFF" {
		t.Fatal(got)
	}
	if restarted.broadcast.emergency == nil || restarted.broadcast.fallbackNext == nil || restarted.broadcast.fallbackNext.Revision != id {
		t.Fatal("managed fallback did not survive restart")
	}
	file := libraryHTTPStatus(t, s)["files"].([]any)[0].(map[string]any)
	// Resolve the original upload by its retained media revision rather than list order.
	for _, raw := range libraryHTTPStatus(t, s)["files"].([]any) {
		candidate := raw.(map[string]any)
		if candidate["name"] == "show.mp4" {
			file = candidate
		}
	}
	body, _ := json.Marshal(map[string]any{"version": file["version"]})
	path := "/api/library/files/" + file["id"].(string)
	b := s.broadcast
	for _, phase := range []string{"selected", "active", "suspended"} {
		if phase == "active" {
			b.mu.Lock()
			b.control.stage = "LIVE"
			b.control.mode = "preview_only"
			b.startFallback(time.Now())
			b.mu.Unlock()
			if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"brb":"`+other+`","fallback_mode":"shared"}`); w.Code != 204 {
				t.Fatal(w.Code, w.Body.String())
			}
		}
		if phase == "suspended" {
			if w := stageRequest(t, s, "play_clip", map[string]any{"revision": other}); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
		}
		if w := dashboardRequest(s, "DELETE", path, string(body)); w.Code != 409 {
			t.Fatal(phase, w.Code, w.Body.String())
		}
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
