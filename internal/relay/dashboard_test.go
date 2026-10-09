package relay

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func dashboardServer(t *testing.T) *Server {
	t.Helper()
	return New(config.Config{DashboardUsername: "admin", DashboardPassword: "dashboard-secret", StateFile: filepath.Join(t.TempDir(), "targets.json"), Targets: []config.Target{
		{Name: "one", URL: "rtmp://localhost/app", StreamKey: "target-secret"},
		{Name: "missing-key", URL: "rtmp://localhost/app"},
		{Name: "invalid-url", URL: "https://invalid", StreamKey: "target-secret", Enabled: "false"},
	}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func dashboardRequest(s *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Restreamer-Control", "1")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestDashboardAuthenticationAndControl(t *testing.T) {
	s := dashboardServer(t)
	for _, path := range []string{"/", "/dashboard.js", "/stages.js", "/preview.js", "/dashboard.css", "/status", "/api/dashboard", "/api/preview", "/api/stage", "/api/stage/commands"} {
		for _, password := range []string{"", "incorrect"} {
			r := httptest.NewRequest("GET", path, nil)
			if password != "" {
				r.SetBasicAuth("admin", password)
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" {
				t.Fatalf("unprotected %s: %d", path, w.Code)
			}
		}
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 {
		t.Fatal("health requires authentication")
	}
	for _, path := range []string{"/", "/dashboard.js", "/dashboard.css", "/status", "/api/dashboard"} {
		w := dashboardRequest(s, "GET", path, "")
		if w.Code != 200 {
			t.Fatalf("authenticated %s: %d", path, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing security headers")
		}
		for _, secret := range []string{"target-secret", "dashboard-secret", "rtmp://localhost"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("credential exposed")
			}
		}
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": false}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if s.outputs[0].snapshot().Enabled {
		t.Fatal("target still enabled")
	}
	for _, tc := range []struct {
		target  string
		enabled any
		code    int
	}{
		{"one", nil, 409}, {"one", "true", 400},
		{"absent", true, 404}, {"missing-key", true, 409},
		{"invalid-url", true, 409},
	} {
		if w := stageRequest(t, s, "set_target", map[string]any{"target": tc.target, "enabled": tc.enabled}); w.Code != tc.code {
			t.Fatalf("%s %v: %d %s", tc.target, tc.enabled, w.Code, w.Body.String())
		}
	}
	state := readStage(t, s)
	command, err := json.Marshal(map[string]any{"id": "invalid-command", "server_id": state.ServerID, "context": state.Context, "action": "set_target", "target": "one", "enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		string(command[:len(command)-1]) + `,"key":"secret"}`,
		string(command) + `{}`,
		strings.Repeat(" ", 4097) + string(command),
	} {
		if w := dashboardRequest(s, "POST", "/api/stage/commands", body); w.Code != 400 {
			t.Fatalf("invalid command accepted: %d %s", w.Code, w.Body.String())
		}
	}
	if s.outputs[0].snapshot().Enabled {
		t.Fatal("invalid request changed state")
	}
	disabled := New(config.Config{}, s.log)
	if w := dashboardRequest(disabled, "GET", "/", ""); w.Code != 503 {
		t.Fatal("missing credentials should disable dashboard")
	}
}

func TestDashboardRejectsCrossOriginControl(t *testing.T) {
	s := dashboardServer(t)
	for _, tc := range []struct {
		origin, fetchSite, controlHeader, contentType string
		want                                          int
	}{
		{"https://evil.example", "", "1", "application/json", 403},
		{"null", "", "1", "application/json", 403},
		{"https://dashboard.example", "cross-site", "1", "application/json", 403},
		{"https://dashboard.example", "same-origin", "", "application/json", 403},
		{"https://dashboard.example", "same-origin", "1", "text/plain", 403},
		{"https://dashboard.example", "same-origin", "1", "application/json", 200},
		{"", "", "1", "application/json", 200},
	} {
		state := readStage(t, s)
		command, err := json.Marshal(map[string]any{"id": "origin-control", "server_id": state.ServerID, "context": state.Context, "action": "stop_now", "confirmed": true})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "http://dashboard.example/api/stage/commands", bytes.NewReader(command))
		r.SetBasicAuth("admin", "dashboard-secret")
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.fetchSite)
		r.Header.Set("X-Restreamer-Control", tc.controlHeader)
		r.Header.Set("Content-Type", tc.contentType)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%+v: %d", tc, w.Code)
		}
	}
	r := httptest.NewRequest(http.MethodOptions, "/api/stage/commands", nil)
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("cross-origin preflight allowed")
	}
}

func TestSupersededControlRoutesCannotBypassStageCommands(t *testing.T) {
	s := dashboardServer(t)
	for _, path := range []string{"/api/targets/one", "/api/forwarding", "/api/brb", "/api/playback"} {
		w := dashboardRequest(s, "PUT", path, `{"enabled":false,"confirmed":true}`)
		if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("superseded route still accepts controls: %s returned %d", path, w.Code)
		}
	}
	if !s.outputs[0].snapshot().Enabled || readStage(t, s).Stage != "OFF" {
		t.Fatal("superseded controls changed broadcast state")
	}
}

func TestPersistentSwitchesAndWriteFailure(t *testing.T) {
	s := dashboardServer(t)
	if err := s.setTarget("one", false); err != nil {
		t.Fatal(err)
	}
	reloaded := New(s.cfg, s.log)
	if err := reloaded.initialize(); err != nil {
		t.Fatal(err)
	}
	if reloaded.outputs[0].snapshot().Enabled {
		t.Fatal("switch lost on restart")
	}
	if err := reloaded.setTarget("one", true); err != nil {
		t.Fatal(err)
	}
	cfg := s.cfg
	cfg.Targets = append([]config.Target(nil), s.cfg.Targets...)
	cfg.Targets[0].StreamKey = ""
	noKey := New(cfg, s.log)
	if err := noKey.initialize(); err != nil {
		t.Fatal(err)
	}
	if noKey.outputs[0].snapshot().Enabled {
		t.Fatal("persisted enabled bypassed missing key")
	}
	content, err := os.ReadFile(s.cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "secret") {
		t.Fatal("secret persisted")
	}
	// The parent path is a file, so a save must fail without applying the switch.
	reloaded.cfg.StateFile = filepath.Join(s.cfg.StateFile, "cannot-save.json")
	if err := reloaded.setTarget("one", false); !errors.Is(err, errSaveState) {
		t.Fatalf("unexpected save error: %v", err)
	}
	if !reloaded.outputs[0].snapshot().Enabled {
		t.Fatal("failed save changed running state")
	}
	if err := os.WriteFile(s.cfg.StateFile, []byte(`{"version":1,"targets":`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := New(s.cfg, s.log).initialize(); err == nil {
		t.Fatal("corrupt persistence silently ignored")
	}
}

func TestBitrateHistoryAndRollingWindow(t *testing.T) {
	s := dashboardServer(t)
	start := time.Unix(1_700_000_000, 0)
	s.sample(start)
	s.inputBytes.Add(1_000_000)
	s.outputs[0].mu.Lock()
	s.outputs[0].status.Bytes = 500_000
	s.outputs[0].mu.Unlock()
	s.sample(start.Add(2 * time.Second))
	history := s.history(start.Add(2 * time.Second))
	if len(history) != 1 || history[0].Input != 4_000_000 || history[0].Outputs["one"] != 2_000_000 {
		t.Fatalf("bad elapsed-time rates: %+v", history)
	}
	for i := 3; i <= 1003; i++ {
		s.sample(start.Add(time.Duration(i) * time.Second))
	}
	history = s.history(start.Add(1003 * time.Second))
	if len(history) != 900 || history[0].Time != start.Add(104*time.Second).UnixMilli() {
		t.Fatal("wrong rolling history bounds")
	}
	if history[len(history)-1].Input != 0 || history[len(history)-1].Outputs["one"] != 0 {
		t.Fatal("idle/disabled bitrate not zero")
	}
	if len(s.history(start.Add(2000*time.Second))) != 0 {
		t.Fatal("stale history retained")
	}
}

func TestLiveTargetStopResumeAndIsolation(t *testing.T) {
	a, b := newSink(t), newSink(t)
	s, address := startRelay(t, []config.Target{a.target("one"), b.target("two")})
	c := publishInput(t, address)
	eventually(t, func() bool {
		return s.outputs[0].snapshot().State == "waiting_for_keyframe" && s.outputs[1].snapshot().State == "waiting_for_keyframe"
	})
	writePacket(t, c, videoConfig())
	writePacket(t, c, audioConfig())
	writePacket(t, c, keyframe(time.Second))
	for _, dest := range []*sink{a, b} {
		for i := 0; i < 3; i++ {
			receive(t, dest)
		}
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": false}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "disabled" })
	sent := s.outputs[0].snapshot().Bytes
	writePacket(t, c, keyframe(2*time.Second))
	if got := receive(t, b); got.Timestamp != time.Second {
		t.Fatal("other output interrupted")
	}
	if !s.active.Load() || b.count() != 1 || s.outputs[0].snapshot().Bytes != sent {
		t.Fatal("stop not isolated")
	}
	select {
	case <-a.packets:
		t.Fatal("stopped target received media")
	default:
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": true}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	eventually(t, func() bool { return a.count() == 2 && s.outputs[0].snapshot().State == "waiting_for_keyframe" })
	writePacket(t, c, packet(rtmp.Video, 3*time.Second, 0x27, 1, 0, 0, 0, 1))
	receive(t, b)
	select {
	case <-a.packets:
		t.Fatal("resume started without keyframe")
	default:
	}
	writePacket(t, c, keyframe(4*time.Second))
	for _, want := range []*rtmp.Message{videoConfig(), audioConfig(), keyframe(4 * time.Second)} {
		got := receive(t, a)
		if !bytes.Equal(got.Body, want.Body) || got.Timestamp != 0 {
			t.Fatal("resume missing headers or timestamp reset")
		}
	}
	if b.count() != 1 {
		t.Fatal("other output reconnected")
	}
	eventually(t, func() bool { return s.inputBytes.Load() > 0 && s.outputs[0].snapshot().Bytes > sent })
}

func TestRapidAndOfflineSwitches(t *testing.T) {
	dest := newSink(t)
	target := dest.target("one")
	target.Enabled = "false"
	s, address := startRelay(t, []config.Target{target})
	if err := s.setTarget("one", true); err != nil {
		t.Fatal(err)
	}
	if s.outputs[0].snapshot().State != "idle" || dest.count() != 0 {
		t.Fatal("offline toggle connected")
	}
	c := publishInput(t, address)
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "waiting_for_keyframe" })
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.setTarget("one", i%2 == 0); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if err := s.setTarget("one", false); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "disabled" })
	c.Net.Close()
	eventually(t, func() bool { return !s.active.Load() })
	if s.outputs[0].snapshot().Enabled {
		t.Fatal("rapid toggles lost final state")
	}
}

func TestTargetIssuesAreSafeAndRetained(t *testing.T) {
	dest := newSink(t)
	s, address := startRelay(t, []config.Target{dest.target("one")})
	c := publishInput(t, address)
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "waiting_for_keyframe" })
	dest.disconnect()
	eventually(t, func() bool { return s.outputs[0].snapshot().LastError != "" })
	w := dashboardRequest(s, "GET", "/api/dashboard", "")
	var status struct {
		Outputs []OutputStatus `json:"outputs"`
	}
	if json.Unmarshal(w.Body.Bytes(), &status) != nil || len(status.Outputs) != 1 {
		t.Fatal("invalid status")
	}
	if status.Outputs[0].LastErrorAt == 0 || status.Outputs[0].RetryAt == 0 {
		t.Fatal("issue missing timestamps")
	}
	if strings.Contains(w.Body.String(), "target-key") {
		t.Fatal("issue leaked secret")
	}
	eventually(t, func() bool { return dest.count() == 2 && s.outputs[0].snapshot().State == "waiting_for_keyframe" })
	writePacket(t, c, videoConfig())
	writePacket(t, c, keyframe(time.Second))
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "streaming" })
	if s.outputs[0].snapshot().LastError == "" {
		t.Fatal("lost last issue on recovery")
	}
}
