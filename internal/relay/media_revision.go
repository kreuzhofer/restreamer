package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	Timing    *GeneratedTiming  `json:"timing,omitempty"`
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
		if readMediaJSON(filepath.Join(l.root, "revisions", file.Name()), &revision) != nil || revision.ID != id || revision.Profile.Validate() != nil || validateRevisionTiming(revision) != nil {
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
	revision, err := l.prepareRetainedRevision(e)
	if err != nil {
		return err
	}
	if l.revisions == nil {
		l.revisions = make(map[string]*MediaRevision)
	}
	l.revisions[revision.ID] = revision
	e.Revision = revision.ID
	return nil
}

// prepareRetainedRevision performs bounded file IO without accessing the catalog.
// Callers publish its result under l.mu; generators need not hold that lock while
// hashing their completed output.
func (l *videoLibrary) prepareRetainedRevision(e *LibraryEntry, timing ...GeneratedTiming) (*MediaRevision, error) {
	var captured *GeneratedTiming
	if len(timing) > 0 {
		value := timing[0]
		captured = &value
	}
	id, err := mediaDigest(e.path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(l.root, "revisions"), 0700); err != nil {
		return nil, err
	}
	path := l.revisionPath(id)
	if err := os.Link(e.path, path); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	// A pre-existing revision must still contain the bytes its identity promises.
	if existing, err := mediaDigest(path); err != nil || existing != id {
		return nil, errors.New("Retained media revision changed")
	}
	if e.index == nil {
		return nil, errors.New("Prepared media index is unavailable")
	}
	index := *e.index // Never retime an index held by an already-open reader.
	var previous MediaRevision
	metadataPath := filepath.Join(l.root, "revisions", id+".json")
	metadataErr := readMediaJSON(metadataPath, &previous)
	if metadataErr == nil {
		if previous.ID != id || previous.Profile != e.Profile || previous.Bytes != index.Size || validateRevisionTiming(previous) != nil {
			return nil, errors.New("Retained media metadata conflicts with these bytes; prepare a new revision")
		}
		if captured != nil && previous.Timing == nil && !strings.HasPrefix(previous.LibraryID, "generator-") {
			return nil, errGeneratedTimingCollision
		}
		if previous.Timing != nil {
			if captured != nil && *captured != *previous.Timing {
				return nil, errors.New("Retained generated frame timing conflicts with these bytes")
			}
			value := *previous.Timing
			captured = &value
			if err := applyGeneratedTiming(&index, captured, e.Profile); err != nil {
				return nil, err
			}
			previous.index = &index
			return &previous, nil
		}
	} else if !errors.Is(metadataErr, os.ErrNotExist) {
		return nil, errors.New("Cannot read retained media metadata")
	}
	if err := applyGeneratedTiming(&index, captured, e.Profile); err != nil {
		return nil, err
	}
	revision := &MediaRevision{Timing: captured, ID: id, LibraryID: e.ID, Name: e.Name, State: "ready", Duration: e.Duration, Bytes: e.index.Size, Profile: e.Profile, index: &index}
	if captured != nil {
		revision.Duration = index.Duration.Seconds()
	}
	if err := writeState(filepath.Join(l.root, "revisions", id+".json"), revision); err != nil {
		return nil, err
	}
	return revision, nil
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
		if err := applyGeneratedTiming(idx, r.Timing, r.Profile); err != nil {
			return nil, r.Profile, err
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
	// Library validation and persistence must not hold the command lock: an
	// operator must still be able to stop the show while storage is busy.
	l.mu.Lock()
	defer l.mu.Unlock()
	changed, err := l.persistSelectionsLocked(settings)
	if err != nil {
		code := 409
		if errors.Is(err, errStageMediaPersistence) {
			code = 507
		}
		http.Error(w, err.Error(), code)
		return
	}
	// Publish the selections and their review context together, after the save
	// succeeds. Stage commands never acquire the library lock while holding the
	// broadcast lock, and recheck context after opening their candidate.
	s.broadcast.mu.Lock()
	l.selected = settings
	if changed {
		s.broadcast.control.version++
	}
	s.broadcast.mu.Unlock()
	w.WriteHeader(204)
}

var errStageMediaPersistence = errors.New("Cannot save stage media; selections unchanged")

// Caller holds l.mu through persistence and publication of the new selections.
func (l *videoLibrary) persistSelectionsLocked(settings StageMediaSelections) (bool, error) {
	ids := []string{settings.Prestream, settings.Ending}
	for _, shortcut := range settings.Shortcuts {
		ids = append(ids, shortcut.Revision)
	}
	validated := make(map[string]bool)
	for _, id := range ids {
		if id == "" || validated[id] {
			continue
		}
		clip, _, err := l.openRevisionLocked(id)
		if err != nil {
			return false, err
		}
		clip.reader.close()
		validated[id] = true
	}
	if err := writeState(filepath.Join(l.root, "stage-media.json"), savedStageMedia{Version: 1, Selections: settings}); err != nil {
		return false, errStageMediaPersistence
	}
	changed := settings.Prestream != l.selected.Prestream || settings.Ending != l.selected.Ending || !slices.Equal(settings.Shortcuts, l.selected.Shortcuts)
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
	passes := 1
	if requested := r.URL.Query().Get("passes"); requested != "" && requested != "1" {
		if requested != "2" {
			http.Error(w, "Preview supports one or two passes", 400)
			return
		}
		passes = 2
	}
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
	if passes == 2 && !clip.reader.index.Exact {
		http.Error(w, "Two-pass preview requires a generated frame-timed revision", 409)
		return
	}
	mux := previewMux{}
	for _, header := range []*rtmp.Message{clip.reader.index.Video, clip.reader.index.Audio} {
		if _, err := mux.push(header); err != nil {
			http.Error(w, "Cannot preview prepared revision", 422)
			return
		}
	}
	started := false
	pass := 0
	for r.Context().Err() == nil {
		msg, err := clip.reader.next()
		finished := errors.Is(err, io.EOF)
		if finished && pass+1 < passes {
			if _, err := clip.reader.seek(0); err != nil {
				return
			}
			pass++
			continue
		}
		if msg != nil {
			copy := *msg
			copy.Timestamp += time.Duration(pass) * clip.reader.index.Duration
			msg = &copy
		}
		if finished && mux.pending != nil {
			// The streaming mux keeps one video frame until its duration is
			// known. At finite EOF the indexed duration supplies that boundary.
			last := *mux.pending
			last.Timestamp = time.Duration(passes) * clip.reader.index.Duration
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
			w.Header().Set("X-Preview-Passes", fmt.Sprint(passes))
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
