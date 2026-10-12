package relay

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
)

func TestDestinationCreatePreserveKeyAndRestart(t *testing.T) {
	cfg := config.Config{DashboardUsername: "test", DashboardPassword: "test", StateFile: filepath.Join(t.TempDir(), "switches.json")}
	s := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := dashboardRequest(s, "POST", "/api/destinations", `{"version":0,"name":"local","url":"rtmp://localhost/live","stream_key":"private-key"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "private-key") {
		t.Fatal("secret exposed")
	}
	w = dashboardRequest(s, "PUT", "/api/destinations/local", `{"version":1,"name":"local","url":"rtmp://localhost/next"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	restarted := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w = dashboardRequest(restarted, "GET", "/api/destinations", "")
	var status struct {
		Version      int `json:"version"`
		Destinations []struct {
			Name   string `json:"name"`
			HasKey bool   `json:"has_key"`
			URL    string `json:"url"`
		} `json:"destinations"`
	}
	if json.Unmarshal(w.Body.Bytes(), &status) != nil || status.Version != 2 || len(status.Destinations) != 1 || !status.Destinations[0].HasKey || status.Destinations[0].URL != "rtmp://localhost/next" {
		t.Fatal(w.Body.String())
	}
	if readStage(t, restarted).Stage != "OFF" {
		t.Fatal("restart resumed broadcast")
	}
	w = dashboardRequest(restarted, "DELETE", "/api/destinations/local", `{"version":2}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(readTargetDashboard(t, restarted).Outputs) != 0 {
		t.Fatal("destination not removed")
	}
}

func TestDestinationManagementRejectsStaleWritesAndPreservesPreviousSettings(t *testing.T) {
	s := New(config.Config{DashboardUsername: "test", DashboardPassword: "test", StateFile: filepath.Join(t.TempDir(), "state.json")}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if w := dashboardRequest(s, "POST", "/api/destinations", `{"version":0,"name":"local","url":"rtmp://localhost/app"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, body := range []string{`{"version":0,"name":"local","url":"rtmp://localhost/new"}`, `{"version":1,"name":"local","url":"https://localhost/new","stream_key":"secret"}`} {
		w := dashboardRequest(s, "PUT", "/api/destinations/local", body)
		if w.Code < 400 {
			t.Fatal("invalid save accepted", w.Body.String())
		}
	}
	// Force a persistence failure without altering credentials.
	s.cfg.DestinationsFile = filepath.Join(t.TempDir(), "missing", "destinations.json")
	if w := dashboardRequest(s, "DELETE", "/api/destinations/local", `{"version":1}`); w.Code != 500 {
		t.Fatal(w.Code, w.Body.String())
	}
	if outputs := readTargetDashboard(t, s).Outputs; len(outputs) != 1 || outputs[0].CanEnable || outputs[0].Enabled {
		t.Fatal(outputs)
	}
}

func TestDestinationImportIsExplicitAndManagedValuesWin(t *testing.T) {
	cfg := config.Config{DashboardUsername: "test", DashboardPassword: "test", StateFile: filepath.Join(t.TempDir(), "switches.json"), Targets: []config.Target{{Name: "legacy", URL: "rtmp://localhost/app", StreamKey: "legacy-secret"}}}
	s := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if w := dashboardRequest(s, "DELETE", "/api/destinations/legacy", `{"version":0}`); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "legacy", "enabled": false}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := dashboardRequest(s, "POST", "/api/destinations/import", `{"version":0}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "PUT", "/api/destinations/legacy", `{"version":1,"name":"renamed","url":"rtmp://localhost/new"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cfg.Targets[0].StreamKey = "changed-legacy-secret"
	s = New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	outputs := readTargetDashboard(t, s).Outputs
	if len(outputs) != 1 || outputs[0].Name != "renamed" || outputs[0].Enabled {
		t.Fatal(outputs)
	}
	for _, path := range []string{"/status", "/api/dashboard", "/api/destinations", "/healthz"} {
		w := dashboardRequest(s, "GET", path, "")
		if strings.Contains(w.Body.String(), "secret") {
			t.Fatal("key leaked", path)
		}
	}
}

func TestDestinationDynamicDeliveryAndActiveEditConfirmation(t *testing.T) {
	destination := newSink(t)
	other := newSink(t)
	s, address := startRelayWithLogger(t, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	// Initialize before changing this test-only persistence setting.
	_ = readStage(t, s)
	s.cfg.DestinationsFile = filepath.Join(t.TempDir(), "destinations.json")
	for i, target := range []config.Target{destination.target("one"), other.target("two")} {
		body, _ := json.Marshal(map[string]any{"version": i, "name": target.Name, "url": target.URL, "stream_key": target.StreamKey})
		if w := dashboardRequest(s, "POST", "/api/destinations", string(body)); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if w := stageRequest(t, s, "set_target", map[string]any{"target": target.Name, "enabled": true}); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	publisher := publishInput(t, address)
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	writePacket(t, publisher, previewVideoConfig())
	writePacket(t, publisher, audioConfig())
	writePacket(t, publisher, keyframe(0))
	eventually(t, func() bool { return destination.count() == 1 && other.count() == 1 })
	status := dashboardRequest(s, "GET", "/api/destinations", "")
	var current struct {
		Version int `json:"version"`
	}
	_ = json.Unmarshal(status.Body.Bytes(), &current)
	edit := map[string]any{"version": current.Version, "name": "one", "url": destination.target("one").URL, "stream_key": "replacement-key"}
	body, _ := json.Marshal(edit)
	if w := dashboardRequest(s, "PUT", "/api/destinations/one", string(body)); w.Code != 409 {
		t.Fatal("unconfirmed active edit", w.Code)
	}
	stage := readStage(t, s)
	edit["confirmed"] = true
	edit["server_id"] = stage.ServerID
	edit["context"] = stage.Context
	body, _ = json.Marshal(edit)
	if w := dashboardRequest(s, "PUT", "/api/destinations/one", string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	writePacket(t, publisher, keyframe(time.Second))
	for i := 0; i < 3; i++ {
		receive(t, other)
	}
	if other.count() != 1 {
		t.Fatal("unrelated destination reconnected")
	}
}

func TestDestinationAuthenticationOriginAndConcurrentEdits(t *testing.T) {
	s := New(config.Config{DashboardUsername: "test", DashboardPassword: "test", StateFile: filepath.Join(t.TempDir(), "state.json")}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, authenticated := range []bool{false, true} {
		r := httptest.NewRequest("POST", "/api/destinations", strings.NewReader(`{"version":0,"name":"x","url":"rtmp://localhost/app"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://foreign.example")
		if authenticated {
			r.SetBasicAuth("test", "test")
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		expected := 401
		if authenticated {
			expected = 403
		}
		if w.Code != expected {
			t.Fatal(w.Code)
		}
	}
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			results <- dashboardRequest(s, "POST", "/api/destinations", `{"version":0,"name":"local","url":"rtmp://localhost/app"}`).Code
		}()
	}
	a, b := <-results, <-results
	if !((a == 200 && b == 409) || (a == 409 && b == 200)) {
		t.Fatal(a, b)
	}
	info, err := os.Stat(s.destinationsPath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credentials permissions", info, err)
	}
}

func TestDestinationManagementDuringRehearsalAndEnding(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	s.cfg.DestinationsFile = filepath.Join(t.TempDir(), "destinations.json")
	// This fixture has legacy destinations; explicitly import before authoring.
	if w := dashboardRequest(s, "POST", "/api/destinations/import", `{"version":0}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","ending":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "POST", "/api/destinations", `{"version":1,"name":"rehearsal","url":"rtmp://localhost/app","stream_key":"test-key"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "rehearsal", "enabled": true}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if status := readTargetDashboard(t, s); status.Forwarding {
		t.Fatal("rehearsal enabled delivery")
	}
	if w := stageRequest(t, s, "end_stream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		path := "/api/destinations"
		if method != "POST" {
			path += "/rehearsal"
		}
		if w := dashboardRequest(s, method, path, `{"version":2,"name":"rehearsal","url":"rtmp://localhost/next"}`); w.Code != 409 {
			t.Fatal(method, w.Code, w.Body.String())
		}
	}
	now := time.Now()
	for i := 0; i < 180; i++ {
		s.broadcast.tick(now.Add(time.Duration(i) * 20 * time.Millisecond))
	}
	if !readStage(t, s).Ending.Completed {
		t.Fatal("ending not complete")
	}
	if w := dashboardRequest(s, "DELETE", "/api/destinations/rehearsal", `{"version":2}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !readStage(t, s).Ending.Completed {
		t.Fatal("management erased ending result")
	}
}
