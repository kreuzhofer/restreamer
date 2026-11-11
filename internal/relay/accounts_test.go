package relay

import (
	"github.com/kreuzhofer/restreamer/internal/config"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func accountRequest(s *Server, method, path, password, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.SetBasicAuth("operator", password)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Restreamer-Control", "1")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestOperatorPasswordChangePersistsAndRevokesOldCredentials(t *testing.T) {
	cfg := config.Config{DashboardUsername: "operator", DashboardPassword: "original-password", StateFile: filepath.Join(t.TempDir(), "targets.json")}
	makeServer := func() *Server { return New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil))) }
	s := makeServer()
	if w := accountRequest(s, "POST", "/api/account/password", "original-password", `{"current_password":"original-password","new_password":"replacement-password-long"}`); w.Code != 204 {
		t.Fatalf("change: %d %s", w.Code, w.Body.String())
	}
	for _, server := range []*Server{s, makeServer()} {
		if w := accountRequest(server, "GET", "/api/account", "original-password", ""); w.Code != 401 {
			t.Fatalf("old password accepted: %d", w.Code)
		}
		if w := accountRequest(server, "GET", "/api/account", "replacement-password-long", ""); w.Code != 200 {
			t.Fatalf("new password rejected after change/restart: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestOperatorAuthenticationThrottlesFailedSignIns(t *testing.T) {
	cfg := config.Config{DashboardUsername: "operator", DashboardPassword: "original-password", StateFile: filepath.Join(t.TempDir(), "targets.json")}
	s := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if w := accountRequest(s, "GET", "/api/account", "original-password", ""); w.Code != 200 {
		t.Fatalf("initial sign-in: %d", w.Code)
	}
	for i := 0; i < 20; i++ {
		if w := accountRequest(s, "GET", "/api/account", "wrong-password", ""); w.Code != 401 {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	if w := accountRequest(s, "GET", "/api/account", "wrong-password", ""); w.Code != 429 {
		t.Fatalf("unbounded failed sign-ins: %d", w.Code)
	}
	if w := accountRequest(s, "GET", "/api/account", "original-password", ""); w.Code != 200 {
		t.Fatalf("attack blocked an already authenticated operator: %d", w.Code)
	}
	if w := accountRequest(s, "POST", "/api/account/password", "original-password", `{"current_password":"original-password","new_password":"replacement-password-long"}`); w.Code != 204 {
		t.Fatalf("password change blocked for valid operator: %d", w.Code)
	}
	if w := accountRequest(s, "GET", "/api/account", "replacement-password-long", ""); w.Code != 200 {
		t.Fatalf("replacement password blocked: %d", w.Code)
	}
	if w := accountRequest(s, "GET", "/api/account", "original-password", ""); w.Code == 200 {
		t.Fatal("old proof bypassed revocation while throttled")
	}

}

func TestOperatorPasswordChangeRejectsInvalidRequestsAndSurvivesEnvironmentRemoval(t *testing.T) {
	cfg := config.Config{DashboardUsername: "operator", DashboardPassword: "original-password", StateFile: filepath.Join(t.TempDir(), "targets.json")}
	s := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, body := range []string{
		`{"current_password":"wrong","new_password":"replacement-password-long"}`,
		`{"current_password":"original-password","new_password":"short"}`,
		`{"current_password":"original-password","new_password":"replacement-password-long","username":"other"}`,
	} {
		if w := accountRequest(s, "POST", "/api/account/password", "original-password", body); w.Code != 400 {
			t.Fatalf("invalid change accepted: %d", w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/account/password", strings.NewReader(`{"current_password":"original-password","new_password":"replacement-password-long"}`))
	r.SetBasicAuth("operator", "original-password")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://untrusted.example")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin change accepted: %d", w.Code)
	}
	cfg.DashboardUsername = ""
	cfg.DashboardPassword = ""
	restarted := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if w := accountRequest(restarted, "GET", "/api/account", "original-password", ""); w.Code != 200 {
		t.Fatalf("persisted credentials lost without bootstrap env: %d %s", w.Code, w.Body.String())
	}
}

func TestOperatorAccountStorageFailureNeverRestoresBootstrapPassword(t *testing.T) {
	cfg := config.Config{DashboardUsername: "operator", DashboardPassword: "original-password", StateFile: filepath.Join(t.TempDir(), "targets.json")}
	s := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if w := accountRequest(s, "GET", "/api/account", "original-password", ""); w.Code != 200 {
		t.Fatalf("bootstrap: %d", w.Code)
	}
	if err := os.WriteFile(cfg.UsersDatabasePath(), []byte("damaged database"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, server := range []*Server{s, New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))} {
		if w := accountRequest(server, "GET", "/api/account", "original-password", ""); w.Code != 503 {
			t.Fatalf("damaged storage accepted bootstrap credentials: %d", w.Code)
		}
	}
}

func TestOperatorDatabaseRejectsJournalOverlapWithOtherStorage(t *testing.T) {
	database := filepath.Join(t.TempDir(), "users.sqlite")
	cfg := config.Config{DashboardUsername: "operator", DashboardPassword: "original-password", UsersDatabase: database, StateFile: database + "-journal"}
	s := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if w := accountRequest(s, "GET", "/api/account", "original-password", ""); w.Code != 503 {
		t.Fatalf("overlapping stores accepted: %d", w.Code)
	}
}
