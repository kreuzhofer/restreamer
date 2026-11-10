package relay

import (
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/kreuzhofer/restreamer/internal/config"
)

func TestContinuousDestinationSelectionPersistsWithoutChangingLegacyDefaults(t *testing.T) {
	cfg := config.Config{DashboardUsername: "test", DashboardPassword: "test", StateFile: filepath.Join(t.TempDir(), "switches.json")}
	newServer := func() *Server { return New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil))) }
	s := newServer()
	for _, body := range []string{
		`{"version":0,"name":"processed","url":"rtmp://localhost/app","stream_key":"test-key","output_mode":"continuous"}`,
		`{"version":1,"name":"native","url":"rtmp://localhost/app","stream_key":"other-test-key"}`,
		`{"version":2,"name":"processed","url":"rtmp://localhost/updated"}`,
	} {
		method, path := "POST", "/api/destinations"
		if body == `{"version":2,"name":"processed","url":"rtmp://localhost/updated"}` {
			method, path = "PUT", "/api/destinations/processed"
		}
		if w := dashboardRequest(s, method, path, body); w.Code != 200 {
			t.Fatalf("save: %d %s", w.Code, w.Body.String())
		}
	}
	s = newServer()
	w := dashboardRequest(s, "GET", "/api/destinations", "")
	var view struct {
		Destinations []struct {
			Name       string `json:"name"`
			OutputMode string `json:"output_mode"`
			HasKey     bool   `json:"has_key"`
		} `json:"destinations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Destinations) != 2 || view.Destinations[0].OutputMode != "continuous" || view.Destinations[1].OutputMode != "passthrough" || !view.Destinations[0].HasKey {
		t.Fatalf("mode or retained key changed: %s", w.Body.String())
	}
	if stage := readStage(t, s); stage.Stage != "OFF" {
		t.Fatal("restart started delivery", stage)
	}
}

func TestContinuousModeChangesRequireOffWithoutPendingStart(t *testing.T) {
	s := New(config.Config{DashboardUsername: "test", DashboardPassword: "test", StateFile: filepath.Join(t.TempDir(), "switches.json")}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if w := dashboardRequest(s, "POST", "/api/destinations", `{"version":0,"name":"local","url":"rtmp://localhost/app","stream_key":"test-key"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "PUT", "/api/destinations/local", `{"version":1,"name":"local","url":"rtmp://localhost/app","output_mode":"unknown"}`); w.Code != 400 {
		t.Fatal("unknown output mode accepted", w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "go_live", map[string]any{"mode": "preview_only"}); w.Code != 202 {
		t.Fatal("expected pending rehearsal", w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "PUT", "/api/destinations/local", `{"version":1,"name":"local","url":"rtmp://localhost/app","output_mode":"continuous"}`); w.Code != 409 {
		t.Fatal("mode changed during a pending start", w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "PUT", "/api/destinations/local", `{"version":1,"name":"local","url":"rtmp://localhost/app","output_mode":"continuous"}`); w.Code != 200 {
		t.Fatal("mode could not be selected after stopping", w.Code, w.Body.String())
	}
}

func TestContinuousMonitorRejectsUnknownOutputPath(t *testing.T) {
	s := New(config.Config{DashboardUsername: "test", DashboardPassword: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if w := dashboardRequest(s, "GET", "/api/broadcast-preview?output_mode=unknown", ""); w.Code != 400 {
		t.Fatal("unknown monitor path accepted", w.Code, w.Body.String())
	}
}
