package relay

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

type ingestionCredential struct {
	Key      string `json:"key"`
	Revision string `json:"revision"`
}

var ingestionKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,256}$`)

func randomIngestionToken() string {
	var value [32]byte
	_, _ = rand.Read(value[:])
	return hex.EncodeToString(value[:])
}

func (s *Server) initializeIngestion() error {
	s.ingestion.Key = s.cfg.StreamKey
	if s.cfg.IngestionKeyFile == "" {
		return nil
	}
	path := s.cfg.IngestionKeyFile
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return errors.New("ingestion key file must be a regular file with permissions 0600")
		}
		f, err := os.Open(path)
		if err != nil {
			return errors.New("cannot read ingestion key file")
		}
		defer f.Close()
		dec := json.NewDecoder(io.LimitReader(f, 4096))
		dec.DisallowUnknownFields()
		if dec.Decode(&s.ingestion) != nil || dec.Decode(new(any)) != io.EOF || !ingestionKeyPattern.MatchString(s.ingestion.Key) || len(s.ingestion.Revision) != 64 {
			return errors.New("invalid ingestion key file")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot read ingestion key file")
	}
	if s.ingestion.Key == "" {
		s.ingestion.Key = randomIngestionToken()
	}
	s.ingestion.Revision = randomIngestionToken()
	return writeIngestionCredential(path, s.ingestion)
}

func writeIngestionCredential(path string, value ingestionCredential) error {
	failure := errors.New("cannot persist ingestion key; current key unchanged")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return failure
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".ingestion-key-*")
	if err != nil {
		return failure
	}
	defer os.Remove(f.Name())
	if err := json.NewEncoder(f).Encode(value); err != nil {
		f.Close()
		return failure
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return failure
	}
	if err := f.Close(); err != nil {
		return failure
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return failure
	}
	return nil
}

// Authentication and rotation serialize at the publish authorization boundary.
// Publishers already authorized remain connected until they disconnect normally.
func (s *Server) authorizeIngestion(key string) bool {
	s.ingestionMu.Lock()
	defer s.ingestionMu.Unlock()
	expected, provided := sha256.Sum256([]byte(s.ingestion.Key)), sha256.Sum256([]byte(key))
	return s.ingestion.Key != "" && subtle.ConstantTimeCompare(expected[:], provided[:]) == 1
}

func (s *Server) ingestionStatusHTTP(w http.ResponseWriter, r *http.Request) {
	s.ingestionMu.Lock()
	defer s.ingestionMu.Unlock()
	generatorJSON(w, 200, map[string]any{"managed": s.cfg.IngestionKeyFile != "", "revision": s.ingestion.Revision, "application": s.cfg.Application, "publisher_connected": s.active.Load(), "connected_publisher_behavior": "retained_until_disconnect"})
}

func (s *Server) ingestionKeyHTTP(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Confirm  bool   `json:"confirm"`
		Revision string `json:"revision"`
	}
	if !generatorDecodeLimit(w, r, &request, 4096) {
		return
	}
	s.ingestionMu.Lock()
	defer s.ingestionMu.Unlock()
	if s.cfg.IngestionKeyFile == "" {
		http.Error(w, "Configure ingestion_key_file to enable managed ingestion", 409)
		return
	}
	if r.URL.Path == "/api/ingestion/regenerate" {
		if !request.Confirm || request.Revision == "" || request.Revision != s.ingestion.Revision {
			http.Error(w, "Confirm regeneration using the current ingestion revision", 409)
			return
		}
		next := ingestionCredential{Key: randomIngestionToken(), Revision: randomIngestionToken()}
		if err := writeIngestionCredential(s.cfg.IngestionKeyFile, next); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		s.ingestion = next
		generatorJSON(w, 200, map[string]string{"revision": next.Revision})
		return
	}
	generatorJSON(w, 200, s.ingestion)
}
