package relay

import (
	"context"
	"encoding/json"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ingestionKey(t *testing.T, s *Server) (string, string) {
	t.Helper()
	w := dashboardRequest(s, "POST", "/api/ingestion/key", `{}`)
	if w.Code != 200 {
		t.Fatalf("reveal: %d %s", w.Code, w.Body.String())
	}
	var value struct {
		Key      string `json:"key"`
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value.Key, value.Revision
}

func TestManagedIngestionPersistsAndKeepsStatusSecretFree(t *testing.T) {
	s := dashboardServer(t)
	s.cfg.IngestionKeyFile = filepath.Join(t.TempDir(), "ingestion-key.json")
	key, rev := ingestionKey(t, s)
	if len(key) < 32 || rev == "" {
		t.Fatal("missing generated credential")
	}
	for _, path := range []string{"/api/ingestion", "/api/dashboard", "/status", "/healthz"} {
		w := dashboardRequest(s, "GET", path, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), key) {
			t.Fatalf("unsafe status %s: %d", path, w.Code)
		}
	}
	info, err := os.Stat(s.cfg.IngestionKeyFile)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("secret must persist with mode 0600", err)
	}
	restarted := New(s.cfg, s.log)
	again, againRev := ingestionKey(t, restarted)
	if key != again || rev != againRev {
		t.Fatal("restart changed credential")
	}
	if restarted.forwarding.Load() {
		t.Fatal("restart resumed broadcast")
	}
}

func TestIngestionRotationIsConfirmedSerializedAndDurable(t *testing.T) {
	s := dashboardServer(t)
	s.cfg.IngestionKeyFile = filepath.Join(t.TempDir(), "key.json")
	s.cfg.StreamKey = "legacy-input-key-123456"
	key, rev := ingestionKey(t, s)
	if key != s.cfg.StreamKey {
		t.Fatal("legacy migration changed key")
	}
	body := `{"confirm":true,"revision":"` + rev + `"}`
	results := make(chan int, 2)
	for range 2 {
		go func() { results <- dashboardRequest(s, "POST", "/api/ingestion/regenerate", body).Code }()
	}
	first, second := <-results, <-results
	if first+second != 609 {
		t.Fatalf("concurrent rotation: %d %d", first, second)
	}
	next, nextRev := ingestionKey(t, s)
	if key == next || rev == nextRev {
		t.Fatal("rotation retained old credential")
	}
	restarted := New(s.cfg, s.log)
	persisted, _ := ingestionKey(t, restarted)
	if persisted != next {
		t.Fatal("legacy configuration overwrote managed key")
	}
	// A filesystem that cannot atomically replace the credential must retain the working key.
	if err := os.Remove(s.cfg.IngestionKeyFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(s.cfg.IngestionKeyFile, 0700); err != nil {
		t.Fatal(err)
	}
	w := dashboardRequest(s, "POST", "/api/ingestion/regenerate", `{"confirm":true,"revision":"`+nextRev+`"}`)
	if w.Code != 500 {
		t.Fatal(w.Code, w.Body.String())
	}
	unchanged, unchangedRev := ingestionKey(t, s)
	if unchanged != next || unchangedRev != nextRev {
		t.Fatal("failed write changed working key")
	}
}

func TestIngestionSecretControlsRequireAuthenticationAndSameOrigin(t *testing.T) {
	s := dashboardServer(t)
	s.cfg.IngestionKeyFile = filepath.Join(t.TempDir(), "key.json")
	for _, path := range []string{"/api/ingestion/key", "/api/ingestion/regenerate"} {
		for _, auth := range []bool{false, true} {
			r := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Restreamer-Control", "1")
			r.Header.Set("Origin", "https://attacker.invalid")
			if auth {
				r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			want := 401
			if auth {
				want = 403
			}
			if w.Code != want {
				t.Fatalf("%s auth=%v: %d", path, auth, w.Code)
			}
		}
	}
	key, rev := ingestionKey(t, s)
	for _, body := range []string{`{}`, `{"confirm":true}`, `{"confirm":false,"revision":"` + rev + `"}`, `{"confirm":true,"revision":"stale"}`} {
		w := dashboardRequest(s, "POST", "/api/ingestion/regenerate", body)
		if w.Code != 409 || strings.Contains(w.Body.String(), key) {
			t.Fatal("unconfirmed rotation accepted")
		}
	}
}

func TestIngestionRotationKeepsConnectedPublisherAndRejectsOldKey(t *testing.T) {
	s := dashboardServer(t)
	s.cfg.Application = "live"
	s.cfg.QueueBytes = 1 << 20
	s.cfg.IngestionKeyFile = filepath.Join(t.TempDir(), "key.json")
	key, rev := ingestionKey(t, s)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, listener) }()
	t.Cleanup(func() { cancel(); <-done })
	address := "rtmp://" + listener.Addr().String() + "/live"
	dial := func(key string) (*rtmp.Conn, error) {
		cctx, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		c, _, err := rtmp.Dial(cctx, address, key)
		return c, err
	}
	publisher, err := dial(key)
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Net.Close()
	w := dashboardRequest(s, "POST", "/api/ingestion/regenerate", `{"confirm":true,"revision":"`+rev+`"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Media still reaches the relay over the original authenticated connection.
	writePacket(t, publisher, &rtmp.Message{Type: rtmp.Audio, Body: []byte{0xaf, 0x00, 0x12, 0x10}})
	eventually(t, func() bool {
		w := dashboardRequest(s, "GET", "/api/dashboard", "")
		var status struct {
			InputBytes uint64 `json:"input_bytes"`
		}
		json.Unmarshal(w.Body.Bytes(), &status)
		return status.InputBytes > 0
	})
	publisher.Net.Close()
	eventually(t, func() bool {
		w := dashboardRequest(s, "GET", "/api/dashboard", "")
		var status struct {
			Publishing bool `json:"publishing"`
		}
		json.Unmarshal(w.Body.Bytes(), &status)
		return !status.Publishing
	})
	if old, err := dial(key); err == nil {
		old.Net.Close()
		t.Fatal("old key authenticated")
	}
	next, _ := ingestionKey(t, s)
	replacement, err := dial(next)
	if err != nil {
		t.Fatal("new key rejected", err)
	}
	replacement.Net.Close()
}

func TestIngestionInvalidStorageFailsWithoutRotatingOrLeaking(t *testing.T) {
	for _, mode := range []os.FileMode{0600, 0644} {
		t.Run(mode.String(), func(t *testing.T) {
			s := dashboardServer(t)
			s.cfg.IngestionKeyFile = filepath.Join(t.TempDir(), "key.json")
			contents := []byte(`{"key":"do-not-leak-this-private-key","revision":"invalid"}`)
			if err := os.WriteFile(s.cfg.IngestionKeyFile, contents, mode); err != nil {
				t.Fatal(err)
			}
			w := dashboardRequest(s, "POST", "/api/ingestion/key", `{}`)
			if w.Code != 503 || strings.Contains(w.Body.String(), "do-not-leak") {
				t.Fatal("unsafe initialization failure", w.Code, w.Body.String())
			}
			unchanged, err := os.ReadFile(s.cfg.IngestionKeyFile)
			if err != nil || string(unchanged) != string(contents) {
				t.Fatal("invalid stored credential overwritten", err)
			}
		})
	}
}

func TestIngestionSettingsPageIsAuthenticatedAndAvailableWithoutRevealingKey(t *testing.T) {
	s := dashboardServer(t)
	s.cfg.IngestionKeyFile = filepath.Join(t.TempDir(), "key.json")
	key, _ := ingestionKey(t, s)
	for _, path := range []string{"/settings", "/ingestion.js", "/ingestion.css"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatalf("unprotected settings %s", path)
		}
		w = dashboardRequest(s, "GET", path, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), key) {
			t.Fatalf("unsafe settings %s", path)
		}
	}
}

func TestIngestionManagementLivesInSettingsAndPreservesOldLink(t *testing.T) {
	s := dashboardServer(t)
	w := dashboardRequest(s, "GET", "/settings", "")
	for _, content := range []string{`id="ingestion-settings"`, `data-ingestion-settings`, `src="/ingestion.js"`} {
		if !strings.Contains(w.Body.String(), content) {
			t.Errorf("Settings missing ingestion management: %s", content)
		}
	}
	if strings.Contains(w.Body.String(), `href="/ingestion-settings.html"`) {
		t.Fatal("old header entry remains")
	}
	redirect := dashboardRequest(s, "GET", "/ingestion-settings.html", "")
	if redirect.Code != 307 || redirect.Header().Get("Location") != "/settings#ingestion-settings" {
		t.Fatal("old ingestion link did not preserve access", redirect.Code)
	}
}
