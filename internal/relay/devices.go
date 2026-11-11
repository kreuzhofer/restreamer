package relay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const pairingLifetime = 5 * time.Minute

type devicePairing struct {
	Code                string
	Name                string
	Expires             time.Time
	PollAfter           time.Time
	Approved            bool
	ControlDestinations bool
}

type deviceAccess struct {
	mu      sync.Mutex
	pending map[string]*devicePairing
	now     func() time.Time
	once    sync.Once
	err     error
	records []deviceCredential
}

type deviceCredential struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Created             int64  `json:"created"`
	Hash                string `json:"hash"`
	ControlDestinations bool   `json:"control_destinations,omitempty"`
}

type devicePrincipal struct{}

func (s *Server) initializeDeviceAccess() error {
	d := s.devices
	d.once.Do(func() {
		path := s.deviceAccessPath()
		if path == "" {
			return
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			d.err = errors.New("device access file must be a regular file with permissions 0600")
			return
		}
		f, err := os.Open(path)
		if err != nil {
			d.err = errors.New("cannot read device access file")
			return
		}
		defer f.Close()
		dec := json.NewDecoder(io.LimitReader(f, 65537))
		dec.DisallowUnknownFields()
		if info.Size() > 65536 || dec.Decode(&d.records) != nil || dec.Decode(new(any)) != io.EOF || len(d.records) > 64 {
			d.err = errors.New("invalid device access file")
			return
		}
		seen := map[string]bool{}
		for _, record := range d.records {
			if len(record.ID) != 64 || len(record.Hash) != 64 || record.Name == "" || len(record.Name) > 80 || record.Created <= 0 || seen[record.ID] {
				d.err = errors.New("invalid device access record")
				return
			}
			if _, err := hex.DecodeString(record.Hash); err != nil {
				d.err = errors.New("invalid device access record")
				return
			}
			seen[record.ID] = true
		}
	})
	return d.err
}

func (s *Server) saveDeviceAccess(records []deviceCredential) error {
	path := s.deviceAccessPath()
	if path == "" {
		return errors.New("persistent storage is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return errors.New("cannot save device access")
	}
	if err := writeState(path, records); err != nil {
		return errors.New("cannot save device access; access remains unchanged")
	}
	s.devices.records = records
	return nil
}

func (s *Server) deviceAuthorized(token string) (deviceCredential, bool) {
	if len(token) != 64 || s.initializeDeviceAccess() != nil {
		return deviceCredential{}, false
	}
	d := s.devices
	d.mu.Lock()
	defer d.mu.Unlock()
	hash := deviceHash(token)
	for _, record := range d.records {
		if record.Hash == hash {
			return record, true
		}
	}
	return deviceCredential{}, false
}

func deviceRouteAllowed(r *http.Request) bool {
	return r.Method == "GET" && (r.URL.Path == "/api/dashboard" || r.URL.Path == "/api/library") || r.Method == "POST" && r.URL.Path == "/api/stage/commands"
}

func deviceCommandAllowed(r *http.Request, cmd StageCommand) bool {
	if r.Context().Value(devicePrincipal{}) == nil {
		return true
	}
	if cmd.Action == "set_target" {
		return deviceCanControlDestinations(r) && cmd.Target != "" && cmd.Enabled != nil && cmd.Mode == "" && cmd.Revision == "" && !cmd.Loop && cmd.Position == 0
	}
	if cmd.Mode != "" || cmd.Target != "" || cmd.Enabled != nil || cmd.Loop || cmd.Position != 0 {
		return false
	}
	switch cmd.Action {
	case "prestream", "go_live", "brb", "return", "end_stream", "play_clip", "stop_now":
		return true
	}
	return false
}

func deviceCanControlDestinations(r *http.Request) bool {
	value := r.Context().Value(devicePrincipal{})
	if value == nil {
		return true
	}
	record, ok := value.(deviceCredential)
	return ok && record.ControlDestinations
}

func withDevicePrincipal(r *http.Request, record deviceCredential) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), devicePrincipal{}, record))
}

func newDeviceAccess() *deviceAccess {
	return &deviceAccess{pending: make(map[string]*devicePairing), now: time.Now}
}

func deviceHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Server) deviceAccessPath() string {
	if s.cfg.StateFile == "" {
		return ""
	}
	return s.cfg.StateFile + ".devices"
}

func (s *Server) pairingHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if s.cfg.DashboardUsername == "" || s.cfg.DashboardPassword == "" || s.deviceAccessPath() == "" {
		generatorJSON(w, 503, map[string]string{"error": "Pairing requires dashboard authentication and persistent TARGET_STATE_FILE storage."})
		return
	}
	// Read untrusted request bodies before acquiring shared device state. A
	// stalled peer must never hold up another device's status or revocation.
	var input struct {
		Name       string `json:"name,omitempty"`
		DeviceCode string `json:"device_code,omitempty"`
	}
	if !generatorDecodeLimit(w, r, &input, 1024) {
		return
	}
	// No expensive media initialization is triggered by anonymous requests.
	d := s.devices
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	for id, pair := range d.pending {
		if !now.Before(pair.Expires) {
			delete(d.pending, id)
		}
	}
	if r.URL.Path == "/api/devices/pair" {
		input.Name = strings.TrimSpace(input.Name)
		if input.Name == "" || len(input.Name) > 80 || strings.ContainsAny(input.Name, "\r\n\x00") {
			generatorJSON(w, 400, map[string]string{"error": "Enter a device name of 1–80 bytes."})
			return
		}
		if len(d.pending) >= 32 {
			generatorJSON(w, 429, map[string]string{"error": "Too many pending connections. Try again in five minutes."})
			return
		}
		secret := randomIngestionToken()
		var codeBytes [5]byte
		_, _ = rand.Read(codeBytes[:])
		code := base32.StdEncoding.EncodeToString(codeBytes[:])
		code = code[:4] + "-" + code[4:]
		d.pending[deviceHash(secret)] = &devicePairing{Code: code, Name: input.Name, Expires: now.Add(pairingLifetime)}
		generatorJSON(w, 201, map[string]any{"device_code": secret, "user_code": code, "expires_in": 300, "interval": 3})
		return
	}
	pair := d.pending[deviceHash(input.DeviceCode)]
	if pair == nil {
		generatorJSON(w, 400, map[string]string{"error": "expired_token"})
		return
	}
	if now.Before(pair.PollAfter) {
		generatorJSON(w, 429, map[string]string{"error": "slow_down"})
		return
	}
	pair.PollAfter = now.Add(3 * time.Second)
	if pair.Approved {
		if len(d.records) >= 64 {
			generatorJSON(w, 409, map[string]string{"error": "Device limit reached. Revoke an unused device in Settings."})
			return
		}
		token := randomIngestionToken()
		record := deviceCredential{ID: randomIngestionToken(), Name: pair.Name, Created: now.UnixMilli(), Hash: deviceHash(token), ControlDestinations: pair.ControlDestinations}
		records := append(append([]deviceCredential{}, d.records...), record)
		if err := s.saveDeviceAccess(records); err != nil {
			generatorJSON(w, 503, map[string]string{"error": err.Error()})
			return
		}
		delete(d.pending, deviceHash(input.DeviceCode))
		generatorJSON(w, 200, map[string]string{"access_token": token, "device_id": record.ID, "token_type": "Bearer"})
		return
	}
	generatorJSON(w, 202, map[string]string{"state": "pending"})
}

func (s *Server) approveDeviceHTTP(w http.ResponseWriter, r *http.Request) {
	var input struct {
		UserCode            string `json:"user_code"`
		Confirm             bool   `json:"confirm"`
		ControlDestinations bool   `json:"control_destinations,omitempty"`
	}
	if !generatorDecodeLimit(w, r, &input, 1024) {
		return
	}
	if !input.Confirm {
		generatorJSON(w, 400, map[string]string{"error": "Confirm that this code matches your Stream Deck."})
		return
	}
	d := s.devices
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, pair := range d.pending {
		if pair.Code == input.UserCode && d.now().Before(pair.Expires) {
			if r.URL.Path == "/api/devices/deny" {
				delete(d.pending, id)
				generatorJSON(w, 200, map[string]string{"state": "denied"})
				return
			}
			if pair.Approved {
				generatorJSON(w, 409, map[string]string{"error": "Connection already approved. Start a new pairing to change permissions."})
				return
			}
			pair.ControlDestinations = input.ControlDestinations
			pair.Approved = true
			generatorJSON(w, 200, map[string]string{"state": "approved"})
			return
		}
	}
	generatorJSON(w, 404, map[string]string{"error": "Connection code expired or not found. Start again in Stream Deck."})
}

func (s *Server) devicesHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "DELETE" {
		var input struct {
			Confirm bool `json:"confirm"`
		}
		if !generatorDecodeLimit(w, r, &input, 1024) {
			return
		}
		if !input.Confirm {
			generatorJSON(w, 400, map[string]string{"error": "Confirm device revocation."})
			return
		}
	}
	d := s.devices
	d.mu.Lock()
	defer d.mu.Unlock()
	if r.Method == "DELETE" {
		records := make([]deviceCredential, 0, len(d.records))
		for _, record := range d.records {
			if record.ID != r.PathValue("id") {
				records = append(records, record)
			}
		}
		if len(records) == len(d.records) {
			generatorJSON(w, 404, map[string]string{"error": "Device already revoked or not found."})
			return
		}
		if err := s.saveDeviceAccess(records); err != nil {
			generatorJSON(w, 503, map[string]string{"error": err.Error()})
			return
		}
		generatorJSON(w, 200, map[string]string{"state": "revoked"})
		return
	}
	devices := make([]map[string]any, 0, len(d.records))
	for _, record := range d.records {
		devices = append(devices, map[string]any{"id": record.ID, "name": record.Name, "created": record.Created, "control_destinations": record.ControlDestinations})
	}
	var pending any
	for _, pair := range d.pending {
		if pair.Code == r.URL.Query().Get("code") && d.now().Before(pair.Expires) {
			pending = map[string]any{"name": pair.Name, "code": pair.Code, "approved": pair.Approved}
			break
		}
	}
	generatorJSON(w, 200, map[string]any{"enabled": s.deviceAccessPath() != "", "devices": devices, "pending": pending})
}
