package relay

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func deviceRequest(s *Server, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Restreamer-Control", "1")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func pairedDevice(t *testing.T, s *Server) (string, string) {
	t.Helper()
	w := deviceRequest(s, "POST", "/api/devices/pair", `{"name":"Desk controls"}`, "")
	if w.Code != 201 {
		t.Fatalf("pair: %d %s", w.Code, w.Body.String())
	}
	var pair map[string]string
	var response struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	w = dashboardRequest(s, "POST", "/api/devices/approve", `{"user_code":"`+response.UserCode+`","confirm":true}`)
	if w.Code != 200 {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	w = deviceRequest(s, "POST", "/api/devices/token", `{"device_code":"`+response.DeviceCode+`"}`, "")
	if w.Code != 200 {
		t.Fatalf("claim token: %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pair); err != nil {
		t.Fatal(err)
	}
	if len(pair["access_token"]) < 32 || pair["device_id"] == "" {
		t.Fatal("missing scoped credential")
	}
	return pair["access_token"], pair["device_id"]
}

func TestDeviceTokenScopesPersistenceAndRevocation(t *testing.T) {
	s := dashboardServer(t)
	token, id := pairedDevice(t, s)
	for _, path := range []string{"/api/dashboard", "/api/library"} {
		// This fixture has no media library; scoped reads must reach the same
		// handler response as the authenticated dashboard (including disabled).
		expected := dashboardRequest(s, "GET", path, "").Code
		if w := deviceRequest(s, "GET", path, "", token); w.Code != expected {
			t.Fatalf("permitted read %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/api/ingestion", "/api/destinations", "/api/devices", "/settings", "/api/generator/assets"} {
		if w := deviceRequest(s, "GET", path, "", token); w.Code != 403 {
			t.Fatalf("scope escape %s: %d", path, w.Code)
		}
	}
	w := deviceRequest(s, "POST", "/api/stage/commands", `{"id":"denied-command","action":"set_target","target":"one","enabled":true}`, token)
	if w.Code != 403 {
		t.Fatalf("configuration command permitted: %d", w.Code)
	}
	var snapshot struct {
		Stage StageStatus `json:"stage"`
	}
	w = deviceRequest(s, "GET", "/api/dashboard", "", token)
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	command := StageCommand{ID: "device-stop", ServerID: snapshot.Stage.ServerID, Context: snapshot.Stage.Context, Action: "stop_now", Confirmed: true}
	body, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	if w = deviceRequest(s, "POST", "/api/stage/commands", string(body), token); w.Code != 200 {
		t.Fatalf("allowed broadcast control: %d %s", w.Code, w.Body.String())
	}
	data, err := os.ReadFile(s.cfg.StateFile + ".devices")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) {
		t.Fatal("raw device token persisted")
	}
	info, err := os.Stat(s.cfg.StateFile + ".devices")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("device store must be private")
	}
	restarted := New(s.cfg, s.log)
	if w = deviceRequest(restarted, "GET", "/api/dashboard", "", token); w.Code != 200 {
		t.Fatalf("token lost on restart: %d", w.Code)
	}
	w = dashboardRequest(restarted, "DELETE", "/api/devices/"+id, `{"confirm":true}`)
	if w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	if w = deviceRequest(restarted, "GET", "/api/dashboard", "", token); w.Code != 401 {
		t.Fatalf("revoked token accepted: %d", w.Code)
	}
	if w = deviceRequest(New(s.cfg, s.log), "GET", "/api/dashboard", "", token); w.Code != 401 {
		t.Fatalf("revoked token revived after restart: %d", w.Code)
	}
}

func TestDevicePairingBoundsAndFailureDoNotGrantAccess(t *testing.T) {
	s := dashboardServer(t)
	for i := 0; i < 32; i++ {
		if w := deviceRequest(s, "POST", "/api/devices/pair", `{"name":"Pending"}`, ""); w.Code != 201 {
			t.Fatalf("pending connection %d: %d", i, w.Code)
		}
	}
	if w := deviceRequest(s, "POST", "/api/devices/pair", `{"name":"Over limit"}`, ""); w.Code != 429 {
		t.Fatal("unbounded pending connections")
	}
	s = dashboardServer(t)
	w := deviceRequest(s, "POST", "/api/devices/pair", `{"name":"Cannot persist"}`, "")
	var pair struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pair); err != nil {
		t.Fatal(err)
	}
	if w = dashboardRequest(s, "POST", "/api/devices/approve", `{"user_code":"`+pair.UserCode+`","confirm":true}`); w.Code != 200 {
		t.Fatal("approval failed")
	}
	// Block the atomic file replacement at the storage boundary.
	if err := os.Mkdir(s.deviceAccessPath(), 0700); err != nil {
		t.Fatal(err)
	}
	if w = deviceRequest(s, "POST", "/api/devices/token", `{"device_code":"`+pair.DeviceCode+`"}`, ""); w.Code != 503 {
		t.Fatalf("persistence failure granted access: %d", w.Code)
	}
	w = dashboardRequest(s, "GET", "/api/devices", "")
	var list struct {
		Devices []any `json:"devices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Devices) != 0 {
		t.Fatal("failed persistence changed device access")
	}
}

func TestDevicePairingExpiryAndPollingBounds(t *testing.T) {
	s := dashboardServer(t)
	now := time.Now()
	s.devices.now = func() time.Time { return now }
	w := deviceRequest(s, "POST", "/api/devices/pair", `{"name":"Desk controls"}`, "")
	var pair struct {
		DeviceCode string `json:"device_code"`
	}
	json.Unmarshal(w.Body.Bytes(), &pair)
	body := `{"device_code":"` + pair.DeviceCode + `"}`
	if w = deviceRequest(s, "POST", "/api/devices/token", body, ""); w.Code != 202 {
		t.Fatal("missing pending state")
	}
	if w = deviceRequest(s, "POST", "/api/devices/token", body, ""); w.Code != 429 {
		t.Fatal("rapid polls not limited")
	}
	now = now.Add(6 * time.Minute)
	if w = deviceRequest(s, "POST", "/api/devices/token", body, ""); w.Code != 400 {
		t.Fatal("expired pairing accepted")
	}
}

func TestDevicePairingRequiresDashboardApproval(t *testing.T) {
	s := dashboardServer(t)
	now := time.Now()
	s.devices.now = func() time.Time { return now }
	w := deviceRequest(s, "POST", "/api/devices/pair", `{"name":"Studio Stream Deck"}`, "")
	if w.Code != 201 {
		t.Fatalf("start pairing: %d %s", w.Code, w.Body.String())
	}
	var pair struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pair); err != nil {
		t.Fatal(err)
	}
	if len(pair.DeviceCode) < 32 || len(pair.UserCode) != 9 {
		t.Fatal("missing independent pairing codes")
	}
	poll := `{"device_code":"` + pair.DeviceCode + `"}`
	if w = deviceRequest(s, "POST", "/api/devices/token", poll, ""); w.Code != 202 {
		t.Fatalf("unapproved poll: %d", w.Code)
	}
	body := `{"user_code":"` + pair.UserCode + `","confirm":true}`
	if w = deviceRequest(s, "POST", "/api/devices/approve", body, ""); w.Code != 401 {
		t.Fatalf("unauthenticated approval: %d", w.Code)
	}
	if w = dashboardRequest(s, "POST", "/api/devices/approve", body); w.Code != 200 {
		t.Fatalf("approval: %d %s", w.Code, w.Body.String())
	}
	now = now.Add(3 * time.Second)
	if w = deviceRequest(s, "POST", "/api/devices/token", poll, ""); w.Code != 200 {
		t.Fatalf("approved exchange: %d", w.Code)
	}
	if w = deviceRequest(s, "POST", "/api/devices/token", poll, ""); w.Code != 400 {
		t.Fatal("pairing secret was reusable")
	}
}

func TestDevicePairingDeclineAndCrossOriginProtection(t *testing.T) {
	s := dashboardServer(t)
	r := httptest.NewRequest("POST", "/api/devices/pair", strings.NewReader(`{"name":"Untrusted"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Restreamer-Control", "1")
	r.Header.Set("Origin", "https://other.example")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin pairing accepted: %d", w.Code)
	}
	w = deviceRequest(s, "POST", "/api/devices/pair", `{"name":"Declined device"}`, "")
	var pair struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pair); err != nil {
		t.Fatal(err)
	}
	w = dashboardRequest(s, "POST", "/api/devices/deny", `{"user_code":"`+pair.UserCode+`","confirm":true}`)
	if w.Code != 200 {
		t.Fatalf("decline failed: %d", w.Code)
	}
	w = deviceRequest(s, "POST", "/api/devices/token", `{"device_code":"`+pair.DeviceCode+`"}`, "")
	if w.Code != 400 {
		t.Fatal("declined device can still pair")
	}
}

// A peer can pause halfway through JSON. That peer must not own the shared
// credential lock while unrelated controls and revocation need it.
func TestDeviceSlowRequestDoesNotBlockOtherDevices(t *testing.T) {
	for _, path := range []string{"/api/devices/pair", "/api/devices/token", "/api/devices/test-id"} {
		t.Run(path, func(t *testing.T) {
			s := dashboardServer(t)
			token, _ := pairedDevice(t, s)
			reader, writer := io.Pipe()
			defer writer.Close()
			method := "POST"
			if path == "/api/devices/test-id" {
				method = "DELETE"
			}
			r := httptest.NewRequest(method, path, reader)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Restreamer-Control", "1")
			r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
			done := make(chan struct{})
			go func() { defer close(done); s.Handler().ServeHTTP(httptest.NewRecorder(), r) }()
			// Pipe.Write returns only once the handler has started consuming the body.
			if _, err := writer.Write([]byte(`{"`)); err != nil {
				t.Fatal(err)
			}
			status := make(chan int, 1)
			go func() { status <- deviceRequest(s, "GET", "/api/dashboard", "", token).Code }()
			select {
			case code := <-status:
				if code != 200 {
					t.Errorf("unrelated status: %d", code)
				}
			case <-time.After(time.Second):
				t.Error("slow request blocked another device's status")
			}
			writer.Close()
			<-done
		})
	}
}
