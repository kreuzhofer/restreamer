package relay

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/kreuzhofer/restreamer/internal/accounts"
)

// Credential initialization is separate from media initialization: anonymous
// requests must not start media workers just to check a password.
func (s *Server) initializeAccount() error {
	s.accountOnce.Do(func() {
		if err := s.validateStoragePaths(); err != nil {
			s.accountErr = err
			return
		}
		if path := s.cfg.UsersDatabasePath(); path != "" {
			s.accountStore, s.accountErr = accounts.Open(path, s.cfg.DashboardUsername, s.cfg.DashboardPassword)
		} else if s.cfg.DashboardUsername == "" || s.cfg.DashboardPassword == "" {
			s.accountErr = accounts.ErrUnconfigured
		}
	})
	return s.accountErr
}

func (s *Server) verifyAccount(user, password string) error {
	if s.accountStore != nil {
		return s.accountStore.Verify(user, password)
	}
	// Native configurations without persistent storage retain environment auth.
	// The account endpoint explicitly reports password management unavailable.
	expectedUser, expectedPassword := sha256.Sum256([]byte(s.cfg.DashboardUsername)), sha256.Sum256([]byte(s.cfg.DashboardPassword))
	actualUser, actualPassword := sha256.Sum256([]byte(user)), sha256.Sum256([]byte(password))
	if subtle.ConstantTimeCompare(expectedUser[:], actualUser[:])&subtle.ConstantTimeCompare(expectedPassword[:], actualPassword[:]) != 1 {
		return accounts.ErrCredentials
	}
	return nil
}

func (s *Server) verifyCachedAccount(user, password string) (bool, error) {
	if s.accountStore != nil {
		return s.accountStore.VerifyCached(user, password)
	}
	// Environment-only authentication has no expensive hash to throttle.
	return s.verifyAccount(user, password) == nil, nil
}

func (s *Server) accountUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Restreamer", charset="UTF-8"`)
	http.Error(w, "Authentication required", http.StatusUnauthorized)
}

func (s *Server) accountAuthError(w http.ResponseWriter, err error) {
	if errors.Is(err, accounts.ErrCredentials) {
		s.accountUnauthorized(w)
		return
	}
	status := http.StatusServiceUnavailable
	if errors.Is(err, accounts.ErrBusy) {
		status = http.StatusTooManyRequests
		w.Header().Set("Retry-After", "60")
	}
	http.Error(w, err.Error(), status)
}

func (s *Server) accountHTTP(w http.ResponseWriter, r *http.Request) {
	user, _, _ := r.BasicAuth()
	generatorJSON(w, 200, map[string]any{"username": user, "password_change_available": s.accountStore != nil})
}

func (s *Server) accountPasswordHTTP(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !generatorDecodeLimit(w, r, &input, 4096) {
		return
	}
	if s.accountStore == nil {
		http.Error(w, "Configure persistent users_database or state_file storage to change the password", 409)
		return
	}
	user, _, _ := r.BasicAuth()
	if err := s.accountStore.Change(user, input.Current, input.New); err != nil {
		if errors.Is(err, accounts.ErrCredentials) {
			s.accountRate(r, true)
		}
		// A wrong confirmation should not trigger a browser-wide Basic auth prompt.
		if errors.Is(err, accounts.ErrPassword) || errors.Is(err, accounts.ErrCredentials) {
			http.Error(w, err.Error(), 400)
			return
		}
		s.accountAuthError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Bound both per-address guessing and the number of remembered clients. Proxy
// headers are deliberately ignored: clients cannot choose their throttle key.
type accountFailures struct {
	mu      sync.Mutex
	clients map[string]accountFailure
}
type accountFailure struct {
	count   int
	expires time.Time
}

func (s *Server) accountRate(r *http.Request, failed bool) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	s.accountFailures.mu.Lock()
	defer s.accountFailures.mu.Unlock()
	now := time.Now()
	if s.accountFailures.clients == nil {
		s.accountFailures.clients = make(map[string]accountFailure)
	}
	for key, entry := range s.accountFailures.clients {
		if !now.Before(entry.expires) {
			delete(s.accountFailures.clients, key)
		}
	}
	entry := s.accountFailures.clients[host]
	if entry.count >= 20 {
		return false
	}
	if !failed {
		return len(s.accountFailures.clients) < 512 || entry.count > 0
	}
	if entry.count == 0 {
		if len(s.accountFailures.clients) >= 512 {
			return false
		}
		entry.expires = now.Add(time.Minute)
	}
	entry.count++
	s.accountFailures.clients[host] = entry
	return true
}
