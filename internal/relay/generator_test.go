package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/kreuzhofer/restreamer/internal/config"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratorDraftSurvivesRestartAndCopiesAreIndependent(t *testing.T) {
	s := libraryServer(t)
	w := dashboardRequest(s, "POST", "/api/generator/designs", `{"name":"Friday show","stage":"prestream"}`)
	if w.Code != 201 {
		t.Fatalf("create draft: %d %s", w.Code, w.Body.String())
	}
	var draft map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	id := draft["id"].(string)
	scene := draft["scenes"].([]any)[0].(map[string]any)
	if scene["duration_seconds"] != float64(10) || draft["theme"].(map[string]any)["id"] != "retro" {
		t.Fatal("blank defaults", draft)
	}
	scene["text"] = "Welcome, München!\nMixed CASE café"
	data, _ := json.Marshal(draft)
	w = dashboardRequest(s, "PUT", "/api/generator/designs/"+id, string(data))
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	saved := w.Body.String()
	reloaded := New(s.cfg, s.log)
	w = dashboardRequest(reloaded, "GET", "/api/generator/designs/"+id, "")
	if w.Code != 200 || w.Body.String() != saved {
		t.Fatalf("draft recovery: %d %s", w.Code, w.Body.String())
	}
	w = dashboardRequest(s, "POST", "/api/generator/designs", string(data))
	if w.Code != 201 {
		t.Fatalf("copy: %d %s", w.Code, w.Body.String())
	}
	var copied map[string]any
	json.Unmarshal(w.Body.Bytes(), &copied)
	if copied["id"] == id || copied["version"] != float64(1) {
		t.Fatal("copy is not independent", copied)
	}
	if w = dashboardRequest(s, "GET", "/api/generator/designs/"+id, ""); w.Body.String() != saved {
		t.Fatal("copy changed original")
	}
}

func TestGeneratorConflictPreservesNewerDraftAndStorageFailureIsNotAcknowledged(t *testing.T) {
	s := libraryServer(t)
	w := dashboardRequest(s, "POST", "/api/generator/designs", `{"name":"Concurrent","stage":"ending"}`)
	var draft map[string]any
	json.Unmarshal(w.Body.Bytes(), &draft)
	id := draft["id"].(string)
	stale := w.Body.String()
	draft["name"] = "Newer saved title"
	data, _ := json.Marshal(draft)
	saved := dashboardRequest(s, "PUT", "/api/generator/designs/"+id, string(data))
	if saved.Code != 200 {
		t.Fatal(saved.Body.String())
	}
	if w = dashboardRequest(s, "PUT", "/api/generator/designs/"+id, stale); w.Code != 409 {
		t.Fatalf("stale save accepted: %d", w.Code)
	}
	if w = dashboardRequest(s, "GET", "/api/generator/designs/"+id, ""); w.Body.String() != saved.Body.String() {
		t.Fatal("conflict overwrote newer data")
	}
	// Simulate actual storage failure at the configured filesystem boundary.
	root := filepath.Join(s.cfg.BRB.Directory, "library", "generator", "designs")
	if err := os.Rename(root, root+"-offline"); err != nil {
		t.Fatal(err)
	}
	if w = dashboardRequest(s, "PUT", "/api/generator/designs/"+id, saved.Body.String()); w.Code < 500 {
		t.Fatalf("storage failure acknowledged: %d", w.Code)
	}
	if err := os.Rename(root+"-offline", root); err != nil {
		t.Fatal(err)
	}
	if w = dashboardRequest(s, "GET", "/api/generator/designs/"+id, ""); w.Body.String() != saved.Body.String() {
		t.Fatal("failed save changed draft")
	}
}

func TestGeneratorPreviewValidatesGlyphsDurationAndOverflow(t *testing.T) {
	s := libraryServer(t)
	w := dashboardRequest(s, "POST", "/api/generator/designs", `{"name":"Preview","stage":"prestream"}`)
	var draft map[string]any
	json.Unmarshal(w.Body.Bytes(), &draft)
	scene := draft["scenes"].([]any)[0].(map[string]any)
	scene["text"] = "Welcome\nMünchen café Ελληνικά Русский"
	data, _ := json.Marshal(draft)
	w = dashboardRequest(s, "POST", "/api/generator/preview", string(data))
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	picture, err := png.Decode(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if picture.Bounds().Dx() != 320 || picture.Bounds().Dy() != 180 {
		t.Fatal("preview ignores profile", picture.Bounds())
	}
	for _, test := range []struct {
		field string
		value any
		want  string
	}{
		{"duration_seconds", 0, "duration_seconds"}, {"duration_seconds", -1, "duration_seconds"}, {"duration_seconds", 3601, "duration_seconds"},
		{"text", "unsupported 🦄", "text"}, {"text", strings.Repeat("a", 250), "text"}, {"text", strings.Repeat("line\n", 30), "text"},
	} {
		t.Run(fmt.Sprint(test.value), func(t *testing.T) {
			copyScene := map[string]any{}
			for k, v := range scene {
				copyScene[k] = v
			}
			copyScene[test.field] = test.value
			draft["scenes"] = []any{copyScene}
			data, _ := json.Marshal(draft)
			w = dashboardRequest(s, "POST", "/api/generator/validate", string(data))
			if w.Code != 200 || !strings.Contains(w.Body.String(), `scenes.0.`+test.want) {
				t.Fatalf("missing field validation: %d %s", w.Code, w.Body.String())
			}
			if preview := dashboardRequest(s, "POST", "/api/generator/preview", string(data)); preview.Code != 422 {
				t.Fatalf("invalid preview accepted: %d", preview.Code)
			}
		})
	}
}

func TestGeneratorControlsAreAuthenticatedBoundedAndSameOrigin(t *testing.T) {
	s := libraryServer(t)
	for _, path := range []string{"/generator", "/generator.js", "/generator.css", "/api/generator/designs", "/api/generator/preview", "/api/generator/validate"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("unprotected %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/api/generator/designs", "/api/generator/preview", "/api/generator/validate"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{"name":"test","stage":"ending"}`))
		r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
		r.Header.Set("Origin", "https://attacker.invalid")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Restreamer-Control", "1")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("cross-origin %s: %d", path, w.Code)
		}
	}
	if w := dashboardRequest(s, "POST", "/api/generator/designs", `{"name":"`+strings.Repeat("x", 65536)+`","stage":"ending"}`); w.Code != 400 {
		t.Fatal("unbounded body", w.Code)
	}
	if w := dashboardRequest(dashboardServer(t), "GET", "/api/generator/designs", ""); w.Code != 409 {
		t.Fatal("generator enabled without library")
	}
}

func TestGeneratorPreviewFitsEveryStreamingResolutionWithoutChangingText(t *testing.T) {
	s := libraryServer(t)
	for _, resolution := range [][2]int{{320, 180}, {1280, 720}, {1920, 1080}, {2560, 1440}, {3840, 2160}} {
		t.Run(fmt.Sprint(resolution), func(t *testing.T) {
			// Existing library profile seam supplies the active streaming dimensions.
			s.library.setProfile(config.BRBProfile{Width: resolution[0], Height: resolution[1], FPS: 25, SampleRate: 48000})
			w := dashboardRequest(s, "POST", "/api/generator/designs", `{"name":"Resolution","stage":"ending"}`)
			var d map[string]any
			json.Unmarshal(w.Body.Bytes(), &d)
			scene := d["scenes"].([]any)[0].(map[string]any)
			scene["text"] = "München café\nΕλληνικά Русский"
			scene["font"] = "go-mono"
			data, _ := json.Marshal(d)
			first := dashboardRequest(s, "POST", "/api/generator/preview", string(data))
			second := dashboardRequest(s, "POST", "/api/generator/preview", string(data))
			if first.Code != 200 || !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
				t.Fatalf("preview not deterministic: %d %s", first.Code, first.Body.String())
			}
			picture, err := png.Decode(first.Body)
			if err != nil {
				t.Fatal(err)
			}
			if picture.Bounds().Dx() != resolution[0] || picture.Bounds().Dy() != resolution[1] {
				t.Fatal("wrong raster dimensions")
			}
			scene["text"] = strings.Repeat("too many lines\n", 40)
			data, _ = json.Marshal(d)
			if w := dashboardRequest(s, "POST", "/api/generator/preview", string(data)); w.Code != 422 {
				t.Fatal("overflow silently fit", w.Code)
			}
		})
	}
}
