package relay

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type eventLoginAttempts struct {
	Count int
	Since time.Time
}

var eventIdentifier = regexp.MustCompile(`^[a-f0-9]{32}$`)

type streamingEvent struct {
	begun         atomic.Bool
	delivering    atomic.Bool
	Segments      []eventSegment `json:"segments"`
	Bytes         int64          `json:"bytes"`
	Error         string         `json:"error,omitempty"`
	Incomplete    bool           `json:"incomplete"`
	mu            sync.Mutex
	dir           string
	ID            string `json:"id"`
	Name          string `json:"name"`
	State         string `json:"state"`
	AutoReplay    bool   `json:"auto_replay"`
	Published     bool   `json:"published"`
	Version       uint64 `json:"version"`
	Salt          []byte `json:"password_salt,omitempty"`
	Hash          []byte `json:"password_hash,omitempty"`
	SessionKey    []byte `json:"session_key"`
	loginAttempts map[string]eventLoginAttempts
}

func (s *Server) initializeEvents() error {
	s.events = make(map[string]*streamingEvent)
	if s.cfg.EventsDirectory == "" {
		return nil
	}
	if err := os.MkdirAll(s.cfg.EventsDirectory, 0700); err != nil {
		return errors.New("Cannot create event storage directory")
	}
	entries, err := os.ReadDir(s.cfg.EventsDirectory)
	if err != nil {
		return errors.New("Cannot read event storage directory")
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".deleted-") && eventIdentifier.MatchString(strings.TrimPrefix(entry.Name(), ".deleted-")) {
			if os.RemoveAll(filepath.Join(s.cfg.EventsDirectory, entry.Name())) != nil {
				s.log.Warn("event deletion cleanup pending")
			}
			continue
		}
		if !entry.IsDir() || !eventIdentifier.MatchString(entry.Name()) {
			continue
		}
		dir := filepath.Join(s.cfg.EventsDirectory, entry.Name())
		data, err := os.ReadFile(filepath.Join(dir, "event.json"))
		if err != nil {
			return errors.New("Cannot read event metadata")
		}
		e := &streamingEvent{dir: dir}
		if json.Unmarshal(data, e) != nil || e.ID != entry.Name() {
			return errors.New("Invalid event metadata")
		}
		if err := e.recoverRecording(); err != nil {
			return err
		}
		s.events[e.ID] = e
	}
	return nil
}

var eventMediaName = regexp.MustCompile(`^(segment-[0-9]{8}\.m4s|init-[a-f0-9]{32}\.mp4)$`)

func (e *streamingEvent) recoverRecording() error {
	if e.Version == 0 || len(e.SessionKey) != 32 || len(e.Salt) != 16 || len(e.Hash) != 0 && len(e.Hash) != 32 {
		return errors.New("Invalid event access metadata")
	}
	switch e.State {
	case "waiting", "live", "ended", "failed", "interrupted", "deleted":
	default:
		return errors.New("Invalid event state")
	}
	if e.State == "deleted" {
		return nil
	}
	changed := false
	if e.State == "live" {
		e.State = "interrupted"
		e.Incomplete = true
		e.Published = false
		e.Error = "Broadcast interrupted by server restart"
		changed = true
	}
	segments := make([]eventSegment, 0, len(e.Segments))
	var size int64
	gap := false
	for _, segment := range e.Segments {
		if !eventMediaName.MatchString(segment.Name) || !strings.HasSuffix(segment.Name, ".m4s") || !eventMediaName.MatchString(segment.Init) || !strings.HasSuffix(segment.Init, ".mp4") || segment.Duration <= 0 || segment.Duration > 6 || segment.Size <= 0 || segment.Size > eventSegmentLimit {
			return errors.New("Invalid event recording index")
		}
		media, err := os.Lstat(filepath.Join(e.dir, segment.Name))
		init, initErr := os.Lstat(filepath.Join(e.dir, segment.Init))
		if err != nil || initErr != nil || !media.Mode().IsRegular() || !init.Mode().IsRegular() || media.Size() != segment.Size || init.Size() == 0 || !eventFileMatches(filepath.Join(e.dir, segment.Name), segment.Checksum) || !eventFileMatches(filepath.Join(e.dir, segment.Init), segment.InitChecksum) {
			changed = true
			gap = true
			e.Incomplete = true
			e.Published = false
			e.Error = "Recording media is missing or incomplete"
			continue
		}
		if gap {
			segment.Discontinuity = true
			gap = false
		}
		segments = append(segments, segment)
		size += segment.Size
	}
	e.Segments, e.Bytes = segments, size
	if changed {
		e.Version++
		if err := e.save(); err != nil {
			return errors.New("Cannot save event recovery state")
		}
	}
	return nil
}

func eventFileMatches(path, checksum string) bool {
	if len(checksum) != sha256.Size*2 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == checksum
}

func (s *Server) event(id string) *streamingEvent {
	s.eventsMu.RLock()
	defer s.eventsMu.RUnlock()
	return s.events[id]
}

func (e *streamingEvent) save() error { return writeState(filepath.Join(e.dir, "event.json"), e) }

func (s *Server) createStreamingEvent(name, password string, auto bool) (*streamingEvent, error) {
	if s.cfg.EventsDirectory == "" {
		return nil, errors.New("Configure events_directory with persistent storage first")
	}
	if len(password) > 256 {
		return nil, errors.New("Password must be at most 256 bytes")
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, errors.New("Cannot create event identity")
	}
	e := &streamingEvent{ID: hex.EncodeToString(id), Name: name, State: "waiting", AutoReplay: auto, Version: 1}
	if err := e.setPassword(password); err != nil {
		return nil, err
	}
	e.dir = filepath.Join(s.cfg.EventsDirectory, e.ID)
	if err := os.Mkdir(e.dir, 0700); err != nil {
		return nil, errors.New("Cannot create event storage")
	}
	if err := e.save(); err != nil {
		_ = os.RemoveAll(e.dir)
		return nil, errors.New("Cannot save event")
	}
	s.eventsMu.Lock()
	s.events[e.ID] = e
	s.eventsMu.Unlock()
	return e, nil
}

func (e *streamingEvent) setPassword(password string) error {
	if len(password) > 256 {
		return errors.New("Password must be at most 256 bytes")
	}
	key := make([]byte, 32)
	salt := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return errors.New("Cannot secure viewer session")
	}
	if _, err := rand.Read(salt); err != nil {
		return errors.New("Cannot secure viewer password")
	}
	var hash []byte
	if password != "" {
		var err error
		hash, err = pbkdf2.Key(sha256.New, password, salt, 600000, 32)
		if err != nil {
			return errors.New("Cannot secure viewer password")
		}
	}
	e.Salt, e.Hash, e.SessionKey = salt, hash, key
	return nil
}

func (e *streamingEvent) view() map[string]any {
	return map[string]any{"id": e.ID, "name": e.Name, "state": e.State, "auto_replay": e.AutoReplay, "published": e.Published, "protected": len(e.Hash) > 0, "version": e.Version, "url": "/watch/" + e.ID, "bytes": e.Bytes, "incomplete": e.Incomplete, "error": e.Error, "recorded": len(e.Segments) > 0, "delivering": e.delivering.Load()}
}

func randomEventToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("secure random unavailable")
	}
	return hex.EncodeToString(b)
}
