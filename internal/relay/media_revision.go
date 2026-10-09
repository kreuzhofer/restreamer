package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

// A revision identifies immutable encoded content, independently of its source
// filename. Original discovery and re-preparation never mutate these files.
type MediaRevision struct {
	ID        string            `json:"id"`
	LibraryID string            `json:"library_id"`
	Name      string            `json:"name"`
	State     string            `json:"state"`
	Duration  float64           `json:"duration"`
	Bytes     int64             `json:"bytes"`
	Profile   config.BRBProfile `json:"profile"`
	Error     string            `json:"error,omitempty"`
	index     *clipIndex
}

type ClipShortcut struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
}

type StageMediaSelections struct {
	Prestream string         `json:"prestream"`
	Ending    string         `json:"ending"`
	Shortcuts []ClipShortcut `json:"shortcuts"`
}

type savedStageMedia struct {
	Version    int                  `json:"version"`
	Selections StageMediaSelections `json:"selections"`
}

func validRevisionID(id string) bool {
	if len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil && id == strings.ToLower(id)
}

func mediaDigest(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("Media file is unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func readMediaJSON(path string, dst any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 64<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("Invalid media state")
	}
	return nil
}

func (l *videoLibrary) revisionPath(id string) string {
	return filepath.Join(l.root, "revisions", id+".flv")
}

func (l *videoLibrary) loadRevisions() error {
	files, err := os.ReadDir(filepath.Join(l.root, "revisions"))
	if err != nil {
		return errors.New("cannot read retained media revisions")
	}
	for _, file := range files {
		id := strings.TrimSuffix(file.Name(), ".json")
		if !file.Type().IsRegular() || !strings.HasSuffix(file.Name(), ".json") || !validRevisionID(id) {
			continue
		}
		var revision MediaRevision
		if readMediaJSON(filepath.Join(l.root, "revisions", file.Name()), &revision) != nil || revision.ID != id || revision.Profile.Validate() != nil {
			return errors.New("invalid retained media revision")
		}
		l.revisions[id] = &revision
	}
	var saved savedStageMedia
	err = readMediaJSON(filepath.Join(l.root, "stage-media.json"), &saved)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || saved.Version != 1 || validateSelections(saved.Selections) != nil {
		return errors.New("invalid saved stage media selections")
	}
	l.selected = saved.Selections
	return nil
}

// Called with l.mu held only once a complete, validated conversion exists.
func (l *videoLibrary) retainRevision(e *LibraryEntry) error {
	id, err := mediaDigest(e.path)
	if err != nil {
		return err
	}
	if l.revisions == nil {
		l.revisions = make(map[string]*MediaRevision)
	}
	if err := os.MkdirAll(filepath.Join(l.root, "revisions"), 0700); err != nil {
		return err
	}
	path := l.revisionPath(id)
	if err := os.Link(e.path, path); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	// A pre-existing revision must still contain the bytes its identity promises.
	if existing, err := mediaDigest(path); err != nil || existing != id {
		return errors.New("Retained media revision changed")
	}
	revision := &MediaRevision{ID: id, LibraryID: e.ID, Name: e.Name, State: "ready", Duration: e.Duration, Bytes: e.index.Size, Profile: e.Profile, index: e.index}
	if err := writeState(filepath.Join(l.root, "revisions", id+".json"), revision); err != nil {
		return err
	}
	l.revisions[id] = revision
	e.Revision = id
	return nil
}

func (l *videoLibrary) selectionsLocked() StageMediaSelections {
	settings := l.selected
	settings.Shortcuts = append([]ClipShortcut{}, l.selected.Shortcuts...)
	return settings
}

func (l *videoLibrary) selections() StageMediaSelections {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.selectionsLocked()
}

func (l *videoLibrary) revisionsLocked() []MediaRevision {
	revisions := make([]MediaRevision, 0, len(l.revisions))
	for _, saved := range l.revisions {
		r := *saved
		info, err := os.Lstat(l.revisionPath(r.ID))
		switch {
		case err != nil || !info.Mode().IsRegular():
			r.State, r.Error = "missing", "Prepared revision is unavailable"
		case info.Size() != r.Bytes:
			r.State, r.Error = "changed", "Prepared revision changed; prepare the original again"
		case r.Profile != l.profile:
			r.State, r.Error = "incompatible", "Prepared revision does not match the active profile"
		}
		revisions = append(revisions, r)
	}
	ids := []string{l.selected.Prestream, l.selected.Ending}
	for _, shortcut := range l.selected.Shortcuts {
		ids = append(ids, shortcut.Revision)
	}
	missing := make(map[string]bool)
	for _, id := range ids {
		if id != "" && l.revisions[id] == nil && !missing[id] {
			revisions = append(revisions, MediaRevision{ID: id, State: "missing", Error: "Selected prepared revision is unavailable"})
			missing[id] = true
		}
	}
	sort.Slice(revisions, func(i, j int) bool { return revisions[i].ID < revisions[j].ID })
	return revisions
}

func (l *videoLibrary) openRevisionLocked(id string) (*clipPlayback, config.BRBProfile, error) {
	r := l.revisions[id]
	if !validRevisionID(id) || r == nil {
		return nil, config.BRBProfile{}, errors.New("Selected media revision is missing; select a ready revision")
	}
	if r.Profile != l.profile {
		return nil, r.Profile, errors.New("Selected media revision is incompatible with the active profile")
	}
	path := l.revisionPath(id)
	if digest, err := mediaDigest(path); err != nil || digest != id {
		r.State, r.Error = "changed", "Selected media revision is missing or changed; prepare the original again"
		return nil, r.Profile, errors.New("Selected media revision is missing or changed; prepare the original again")
	}
	r.State, r.Error = "ready", ""
	if r.index == nil {
		idx, err := indexClip(path)
		if err != nil {
			return nil, r.Profile, errors.New("Selected media revision is invalid; prepare the original again")
		}
		r.index = idx
	}
	reader, err := openClip(path, r.index)
	if err != nil {
		return nil, r.Profile, errors.New("Cannot open selected media revision")
	}
	return &clipPlayback{ID: r.LibraryID, Name: r.Name, Revision: id, reader: reader}, r.Profile, nil
}

func (l *videoLibrary) openRevision(id string) (*clipPlayback, config.BRBProfile, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.openRevisionLocked(id)
}

func validateSelections(settings StageMediaSelections) error {
	if len(settings.Shortcuts) > 32 {
		return errors.New("Save at most 32 clip shortcuts")
	}
	for _, id := range []string{settings.Prestream, settings.Ending} {
		if id != "" && !validRevisionID(id) {
			return errors.New("Select an exact ready media revision")
		}
	}
	names := make(map[string]bool)
	for _, shortcut := range settings.Shortcuts {
		if !utf8.ValidString(shortcut.Name) || strings.TrimSpace(shortcut.Name) != shortcut.Name || shortcut.Name == "" || len(shortcut.Name) > 80 || !validRevisionID(shortcut.Revision) || names[shortcut.Name] {
			return errors.New("Each clip shortcut needs a unique name up to 80 bytes and an exact ready revision")
		}
		for _, ch := range shortcut.Name {
			if ch < 32 || ch == 127 {
				return errors.New("Shortcut names cannot contain control characters")
			}
		}
		names[shortcut.Name] = true
	}
	return nil
}

func (s *Server) stageMediaSettings(w http.ResponseWriter, r *http.Request) {
	l := s.library
	if l == nil {
		http.Error(w, "Enable prepared media before selecting stage media", 409)
		return
	}
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(l.selections())
		return
	}
	typ, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !controlOriginAllowed(r) || typ != "application/json" {
		http.Error(w, "Control requests require same-origin JSON", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	var settings StageMediaSelections
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&settings) != nil || d.Decode(new(any)) != io.EOF {
		http.Error(w, "Expected one stage media settings object", 400)
		return
	}
	if err := validateSelections(settings); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	changed, err := l.saveSelections(settings)
	if err != nil {
		code := 409
		if errors.Is(err, errStageMediaPersistence) {
			code = 507
		}
		http.Error(w, err.Error(), code)
		return
	}
	if changed && s.broadcast != nil {
		s.broadcast.invalidateStageContext()
	}
	w.WriteHeader(204)
}

var errStageMediaPersistence = errors.New("Cannot save stage media; selections unchanged")

func (l *videoLibrary) saveSelections(settings StageMediaSelections) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ids := []string{settings.Prestream, settings.Ending}
	for _, shortcut := range settings.Shortcuts {
		ids = append(ids, shortcut.Revision)
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		clip, _, err := l.openRevisionLocked(id)
		if err != nil {
			return false, err
		}
		clip.reader.close()
	}
	if err := writeState(filepath.Join(l.root, "stage-media.json"), savedStageMedia{Version: 1, Selections: settings}); err != nil {
		return false, errStageMediaPersistence
	}
	changed := settings.Prestream != l.selected.Prestream || settings.Ending != l.selected.Ending || !slices.Equal(settings.Shortcuts, l.selected.Shortcuts)
	l.selected = settings
	return changed, nil
}

func (s *Server) libraryPrepare(w http.ResponseWriter, r *http.Request) {
	typ, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !controlOriginAllowed(r) || typ != "application/json" {
		http.Error(w, "Control requests require same-origin JSON", 403)
		return
	}
	if s.library == nil {
		http.Error(w, "Enable prepared media before preparing videos", 409)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	defer r.Body.Close()
	var body struct {
		ID string `json:"id"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&body) != nil || d.Decode(new(any)) != io.EOF {
		http.Error(w, "Expected one preparation request", 400)
		return
	}
	if err := s.library.retry(body.ID); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.WriteHeader(202)
}

// Candidate preview reuses the browser-compatible packet mux. It reads only
// the selected immutable revision and never touches the broadcast controller.
func (s *Server) libraryRevisionPreview(w http.ResponseWriter, r *http.Request) {
	if s.library == nil {
		http.Error(w, "Prepared media is unavailable", 409)
		return
	}
	select {
	case s.previewSlots <- struct{}{}:
		defer func() { <-s.previewSlots }()
	default:
		http.Error(w, "Preview viewer limit reached", 429)
		return
	}
	clip, _, err := s.library.openRevision(r.PathValue("revision"))
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	defer clip.reader.close()
	mux := previewMux{}
	for _, header := range []*rtmp.Message{clip.reader.index.Video, clip.reader.index.Audio} {
		if _, err := mux.push(header); err != nil {
			http.Error(w, "Cannot preview prepared revision", 422)
			return
		}
	}
	started := false
	for r.Context().Err() == nil {
		msg, err := clip.reader.next()
		finished := errors.Is(err, io.EOF)
		if finished && mux.pending != nil {
			// The streaming mux keeps one video frame until its duration is
			// known. At finite EOF the indexed duration supplies that boundary.
			last := *mux.pending
			last.Timestamp = clip.reader.index.Duration
			msg, err = &last, nil
		}
		if err != nil {
			return
		}
		data, err := mux.push(msg)
		if err != nil {
			if !started {
				http.Error(w, "Cannot preview prepared revision", 422)
			}
			return
		}
		if len(data) == 0 {
			continue
		}
		if !started {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("X-Preview-Codecs", mux.mime)
			w.Header().Set("X-Preview-Base-Ms", "0")
			w.Header().Set("X-Media-Revision", clip.Revision)
			started = true
		}
		controller := http.NewResponseController(w)
		_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
		if _, err := w.Write(data); err != nil {
			return
		}
		if err := controller.Flush(); err != nil {
			return
		}
		if finished {
			return
		}
	}
}
