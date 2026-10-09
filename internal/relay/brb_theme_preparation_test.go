package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
	"image/color"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func preparationStatus(t *testing.T, s *Server) map[string]any {
	t.Helper()
	w := dashboardRequest(s, "GET", "/api/brb/theme/preparation", "")
	if w.Code != 200 {
		t.Fatalf("preparation status: %d %s", w.Code, w.Body.String())
	}
	var status map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	return status
}
func waitPreparation(t *testing.T, s *Server) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		status := preparationStatus(t, s)
		if status != nil && status["state"] != "running" && status["state"] != "cancelling" {
			return status
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("preparation did not finish")
	return nil
}
func preparationBody(s *Server, async bool) string {
	return fmt.Sprintf(`{"theme":{"id":"retro","revision":2},"base_generation":%q,"async":%t}`, brbHTTPSettingsForPreparation(s), async)
}
func brbHTTPSettingsForPreparation(s *Server) string {
	var v struct {
		Assets brbSettings `json:"brb_assets"`
	}
	json.Unmarshal(dashboardRequest(s, "GET", "/api/dashboard", "").Body.Bytes(), &v)
	return v.Assets.Generation
}

func TestBRBThemeAcceptedPreparationSurvivesRequestDisconnect(t *testing.T) {
	s := libraryServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest("POST", "/api/brb/theme/prepare", strings.NewReader(preparationBody(s, false))).WithContext(ctx)
	request.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	request.Header.Set("X-Restreamer-Control", "1")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { s.Handler().ServeHTTP(response, request); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for preparationStatus(t, s) == nil {
		if time.Now().After(deadline) {
			t.Fatal("request not accepted")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	status := waitPreparation(t, s)
	if status["state"] != "ready" {
		t.Fatal(status)
	}
	candidate := dashboardRequest(s, "GET", "/api/brb/theme/candidate", "")
	if !strings.Contains(candidate.Body.String(), status["candidate_id"].(string)) {
		t.Fatal(candidate.Body.String())
	}
	restarted := New(s.cfg, s.log)
	if got := preparationStatus(t, restarted); got["state"] != "ready" || got["id"] != status["id"] {
		t.Fatal(got)
	}
}

func TestBRBThemeCancellationIsScopedAndPreservesCandidate(t *testing.T) {
	s := libraryServer(t)
	original := brbHTTPSettingsForPreparation(s)
	old := dashboardRequest(s, "POST", "/api/brb/theme/prepare", preparationBody(s, false))
	if old.Code != 201 {
		t.Fatal(old.Code, old.Body.String())
	}
	start := func() map[string]any {
		w := dashboardRequest(s, "POST", "/api/brb/theme/prepare", preparationBody(s, true))
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		var v map[string]any
		json.Unmarshal(w.Body.Bytes(), &v)
		return v
	}
	first := start()
	cancel := func(id any) int {
		return dashboardRequest(s, "POST", "/api/brb/theme/preparation/cancel", fmt.Sprintf(`{"id":%q}`, id)).Code
	}
	if code := cancel(first["id"]); code != 202 {
		t.Fatal(code)
	}
	if got := waitPreparation(t, s); got["state"] != "cancelled" {
		t.Fatal(got)
	}
	second := start()
	if code := cancel(first["id"]); code != 409 {
		t.Fatalf("stale cancel: %d", code)
	}
	if code := cancel(second["id"]); code != 202 {
		t.Fatal(code)
	}
	if got := waitPreparation(t, s); got["state"] != "cancelled" {
		t.Fatal(got)
	}
	if got := dashboardRequest(s, "GET", "/api/brb/theme/candidate", ""); got.Body.String() != old.Body.String() {
		t.Fatal("cancel replaced candidate")
	}
	if brbHTTPSettingsForPreparation(s) != original {
		t.Fatal("cancel changed current BRB")
	}
}

func TestBRBThemeServerShutdownRetainsInterruptedPreparation(t *testing.T) {
	s := libraryServer(t)
	original := brbHTTPSettingsForPreparation(s)
	stop := serveEndingActivation(t, s)
	w := dashboardRequest(s, "POST", "/api/brb/theme/prepare", preparationBody(s, true))
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var accepted map[string]any
	json.Unmarshal(w.Body.Bytes(), &accepted)
	stop()
	restarted := New(s.cfg, s.log)
	got := preparationStatus(t, restarted)
	if got["state"] != "interrupted" || got["id"] != accepted["id"] {
		t.Fatal(got)
	}
	if brbHTTPSettingsForPreparation(s) != original {
		t.Fatal("shutdown changed BRB")
	}
	// Legacy startup refreshes its generation; saved content and profile remain unchanged.
	before, after := brbHTTPSettings(t, s), brbHTTPSettings(t, restarted)
	if before.Text != after.Text || before.Profile != after.Profile || after.Theme != nil {
		t.Fatal("restart changed saved BRB content")
	}
	if w := dashboardRequest(restarted, "GET", "/api/brb/theme/candidate", ""); strings.TrimSpace(w.Body.String()) != "null" {
		t.Fatal(w.Body.String())
	}
	if w := dashboardRequest(restarted, "POST", "/api/brb/theme/prepare", preparationBody(restarted, false)); w.Code != 201 {
		t.Fatal("restart could not prepare", w.Code, w.Body.String())
	}
}

func TestBRBThemePendingReferencesAndCrashStatusRemainVisible(t *testing.T) {
	s := libraryServer(t)
	logo := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "logo.png", assetPNG(t, color.White)))
	theme := mediaauthor.RetroTheme(2)
	theme.Style.Logo = &mediaauthor.AssetRef{ID: logo.ID, Revision: 1}
	raw, _ := json.Marshal(map[string]any{"name": "Pending logo", "base": mediaauthor.ThemeRef{ID: "retro", Revision: 2}, "style": theme.Style})
	created := dashboardRequest(s, "POST", "/api/generator/themes", string(raw))
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	json.Unmarshal(created.Body.Bytes(), &theme)
	body := fmt.Sprintf(`{"theme":{"id":%q,"revision":1},"base_generation":%q,"async":true}`, theme.ID, brbHTTPSettingsForPreparation(s))
	started := dashboardRequest(s, "POST", "/api/brb/theme/prepare", body)
	if started.Code != 202 {
		t.Fatal(started.Code, started.Body.String())
	}
	var record map[string]any
	json.Unmarshal(started.Body.Bytes(), &record)
	uses := dashboardRequest(s, "GET", "/api/generator/assets", "")
	if !strings.Contains(uses.Body.String(), `"kind":"brb_preparing"`) {
		t.Fatal("missing pending reference", uses.Body.String())
	}
	if w := dashboardRequest(s, "POST", "/api/brb/theme/prepare", body); w.Code != 409 {
		t.Fatal("accepted a second preparation", w.Code)
	}
	if w := dashboardRequest(s, "DELETE", "/api/generator/assets/"+logo.ID, `{}`); w.Code != 409 {
		t.Fatal("deleted pending input", w.Code)
	}
	if got := waitPreparation(t, s); got["state"] != "ready" {
		t.Fatal(got)
	}
	// A crash leaves the accepted record without a terminal write. Startup must
	// expose interruption explicitly while retaining the independently saved candidate.
	if err := os.WriteFile(filepath.Join(s.cfg.BRB.Directory, "theme-preparation.json"), started.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	restarted := New(s.cfg, s.log)
	if got := preparationStatus(t, restarted); got["state"] != "interrupted" || got["id"] != record["id"] {
		t.Fatal(got)
	}
	if w := dashboardRequest(restarted, "GET", "/api/brb/theme/candidate/preview", ""); w.Code != 200 {
		t.Fatal("restart lost candidate", w.Code)
	}
}

func TestBRBThemePublisherDisconnectDoesNotStopPreparation(t *testing.T) {
	s := libraryServer(t)
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
	publisher, _, err := rtmp.Dial(ctx, "rtmp://"+listener.Addr().String()+"/"+s.cfg.Application, s.cfg.StreamKey)
	if err != nil {
		t.Fatal(err)
	}
	publishing := func() bool {
		var state struct {
			Publishing bool `json:"publishing"`
		}
		json.Unmarshal(dashboardRequest(s, "GET", "/api/dashboard", "").Body.Bytes(), &state)
		return state.Publishing
	}
	eventually(t, publishing)
	publisher.Net.Close()
	eventually(t, func() bool { return !publishing() })
	w := dashboardRequest(s, "POST", "/api/brb/theme/prepare", preparationBody(s, false))
	if w.Code != 201 {
		t.Fatal("publisher disconnect stopped server-owned preparation", w.Code, w.Body.String())
	}
}
