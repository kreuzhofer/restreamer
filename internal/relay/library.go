package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kreuzhofer/restreamer/internal/config"
)

type LibraryEntry struct {
	ID                         string            `json:"id"`
	Revision                   string            `json:"revision,omitempty"`
	Name                       string            `json:"name"`
	State                      string            `json:"state"`
	Progress                   int               `json:"progress"`
	Duration                   float64           `json:"duration"`
	Bytes                      int64             `json:"bytes"`
	Error                      string            `json:"error,omitempty"`
	Message                    string            `json:"message,omitempty"`
	Profile                    config.BRBProfile `json:"profile"`
	stamp, key, path, identity string
	index                      *clipIndex
}
type LibraryStatus struct {
	Enabled     bool                 `json:"enabled"`
	UploadLimit int64                `json:"upload_limit"`
	Error       string               `json:"error,omitempty"`
	Files       []LibraryEntry       `json:"files"`
	Revisions   []MediaRevision      `json:"revisions"`
	Selections  StageMediaSelections `json:"selections"`
}
type videoLibrary struct {
	mu, scanMu, uploadMu sync.Mutex
	root                 string
	limit                int64
	profile              config.BRBProfile
	entries              map[string]*LibraryEntry
	revisions            map[string]*MediaRevision
	selected             StageMediaSelections
	wake                 chan struct{}
	cancel               context.CancelFunc
	jobID                string
	lastError            string
	preparation          *preparationGate
}

func validClipName(name string) bool {
	if !utf8.ValidString(name) || len(name) > 180 || strings.TrimSpace(name) == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") || !strings.EqualFold(filepath.Ext(name), ".mp4") || strings.HasPrefix(name, ".") {
		return false
	}
	for _, ch := range name {
		if ch < 32 || ch == 127 {
			return false
		}
	}
	return true
}
func clipHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}
func clipCacheKey(id, stamp string, p config.BRBProfile) string {
	return id + "-" + clipHash(fmt.Sprintf("v1:%s:%d:%d:%d:%d", stamp, p.Width, p.Height, p.FPS, p.SampleRate))
}

func (s *Server) initializeLibrary() error {
	if s.broadcast == nil || s.broadcast.media == nil {
		return nil
	}
	root := s.cfg.LibraryDirectory
	if root == "" {
		root = filepath.Join(s.cfg.BRB.Directory, "library")
	}
	limit := s.cfg.LibraryUploadMiB
	if limit == 0 {
		limit = 4096
	}
	for _, name := range []string{"originals", "prepared", "revisions"} {
		if os.MkdirAll(filepath.Join(root, name), 0700) != nil {
			return errors.New("cannot create persistent video library")
		}
	}
	s.library = &videoLibrary{preparation: s.preparation, root: root, limit: int64(limit) << 20, profile: s.broadcast.media.settings.Profile, entries: make(map[string]*LibraryEntry), revisions: make(map[string]*MediaRevision), wake: make(chan struct{}, 1)}
	return s.library.loadRevisions()
}
func (l *videoLibrary) notify() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}
func (l *videoLibrary) status() LibraryStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	status := LibraryStatus{Enabled: true, UploadLimit: l.limit, Error: l.lastError, Files: make([]LibraryEntry, 0, len(l.entries))}
	for _, e := range l.entries {
		status.Files = append(status.Files, *e)
	}
	sort.Slice(status.Files, func(i, j int) bool { return status.Files[i].Name < status.Files[j].Name })
	status.Selections = l.selectionsLocked()
	status.Revisions = l.revisionsLocked()
	return status
}
func (l *videoLibrary) setProfile(profile config.BRBProfile) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.profile == profile {
		return
	}
	l.profile = profile
	if l.cancel != nil {
		l.cancel()
	}
	for _, e := range l.entries {
		wasDiscovering := e.State == "discovering"
		e.Message = ""
		e.State = "queued"
		e.Progress = 0
		e.Error = ""
		e.index = nil
		e.Revision = ""
		e.Profile = profile
		e.key = clipCacheKey(e.ID, e.stamp, profile)
		if wasDiscovering {
			e.State = "discovering"
		}
		if err := l.validateOriginal(e); err != nil {
			e.Message = ""
			e.State = "failed"
			e.Error = err.Error()
		}
	}
	l.notify()
}
func (l *videoLibrary) scan() {
	l.scanMu.Lock()
	defer l.scanMu.Unlock()
	files, err := os.ReadDir(filepath.Join(l.root, "originals"))
	l.mu.Lock()
	defer l.mu.Unlock()
	if err != nil {
		l.lastError = "Cannot read video library folder"
		return
	}
	l.lastError = ""
	seen := make(map[string]bool)
	for _, file := range files {
		if !validClipName(file.Name()) || !file.Type().IsRegular() {
			continue
		}
		if len(seen) >= 1000 {
			l.lastError = "Library supports at most 1000 MP4 files"
			break
		}
		info, err := file.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		id := clipHash(file.Name())
		seen[id] = true
		e := l.entries[id]
		identity := sourceFileIdentity(info)
		stamp := fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
		var digestErr error
		if e != nil && identity != "" && e.identity == identity {
			stamp = e.stamp
		} else if info.Size() > 0 && info.Size() <= l.limit {
			var digest string
			digest, digestErr = mediaDigest(filepath.Join(l.root, "originals", file.Name()))
			stamp += ":" + digest
		}
		if e == nil || e.stamp != stamp {
			if l.jobID == id && l.cancel != nil {
				l.cancel()
			}
			e = &LibraryEntry{ID: id, Name: file.Name(), State: "discovering", Bytes: info.Size(), stamp: stamp, identity: identity, Profile: l.profile}
			e.key = clipCacheKey(id, stamp, l.profile)
			l.entries[id] = e
		} else if e.State == "discovering" {
			e.Message = ""
			e.State = "queued"
		}
		if info.Size() > l.limit || info.Size() == 0 {
			e.Message = ""
			e.State = "failed"
			e.Error = "MP4 is empty or exceeds the configured upload limit"
		}
		if digestErr != nil {
			e.Message = ""
			e.State = "failed"
			e.Error = "Cannot read original MP4; check storage"
		}
	}
	if l.lastError == "" {
		for id := range l.entries {
			if !seen[id] {
				if l.jobID == id && l.cancel != nil {
					l.cancel()
				}
				delete(l.entries, id)
			}
		}
	}
	l.notify()
}
func (l *videoLibrary) run(ctx context.Context) {
	l.scan()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); l.worker(ctx) }()
	defer wg.Wait()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.scan()
		}
	}
}
func (l *videoLibrary) worker(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		l.mu.Lock()
		var job *LibraryEntry
		ids := make([]string, 0, len(l.entries))
		for id := range l.entries {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			e := l.entries[id]
			if e.State == "queued" {
				copy := *e
				job = &copy
				e.State = "preparing"
				break
			}
		}
		if job == nil {
			l.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-l.wake:
			}
			continue
		}
		jobCtx, cancel := context.WithTimeout(ctx, 12*time.Hour)
		l.cancel = cancel
		l.jobID = job.ID
		l.mu.Unlock()
		l.prepare(jobCtx, job)
		cancel()
		l.mu.Lock()
		l.cancel = nil
		l.jobID = ""
		l.mu.Unlock()
	}
}

func (l *videoLibrary) validateOriginal(e *LibraryEntry) error {
	info, err := os.Lstat(filepath.Join(l.root, "originals", e.Name))
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("Original MP4 is unavailable")
	}
	if info.Size() == 0 || info.Size() > l.limit {
		return errors.New("MP4 is empty or exceeds the configured upload limit")
	}
	digest, err := mediaDigest(filepath.Join(l.root, "originals", e.Name))
	if err != nil || fmt.Sprintf("%d:%d:%s", info.Size(), info.ModTime().UnixNano(), digest) != e.stamp {
		return errors.New("Original MP4 changed; waiting for library discovery")
	}
	return nil
}
func (l *videoLibrary) prepare(ctx context.Context, job *LibraryEntry) {
	l.mu.Lock()
	if e := l.entries[job.ID]; e != nil && e.key == job.key {
		e.Message = "Waiting for media preparation; live delivery continues."
	}
	l.mu.Unlock()
	release, admissionErr := l.preparation.acquire(ctx)
	if admissionErr != nil {
		l.mu.Lock()
		if e := l.entries[job.ID]; e != nil && e.key == job.key && ctx.Err() == context.DeadlineExceeded {
			e.Message = ""
			e.State = "failed"
			e.Error = "Preparation exceeded the 12-hour deadline while waiting for other media preparation"
		}
		l.mu.Unlock()
		return
	}
	defer release()
	l.mu.Lock()
	if e := l.entries[job.ID]; e != nil && e.key == job.key {
		e.Message = "Preparing media."
	}
	l.mu.Unlock()
	path := filepath.Join(l.root, "prepared", job.key+".flv")
	var idx *clipIndex
	err := l.validateOriginal(job)
	if info, e := os.Lstat(path); err == nil && e == nil && info.Mode().IsRegular() {
		idx, err = indexClipContext(ctx, path)
	}
	if err == nil && idx == nil {
		temp, e := os.CreateTemp(filepath.Join(l.root, "prepared"), ".prepare-*")
		if e != nil {
			err = errors.New("Cannot create prepared video; check storage")
		} else {
			tempPath := temp.Name()
			temp.Close()
			defer os.Remove(tempPath)
			err = prepareClip(ctx, filepath.Join(l.root, "originals", job.Name), tempPath, job.Profile, func(progress float64) {
				l.mu.Lock()
				defer l.mu.Unlock()
				if e := l.entries[job.ID]; e != nil && e.key == job.key {
					e.Progress = int(progress)
				}
			})
			if err == nil {
				idx, err = indexClipContext(ctx, tempPath)
			}
			if err == nil {
				err = os.Rename(tempPath, path)
			}
		}
	}
	if err == nil && idx != nil {
		check := &brbInputCheck{ingest: map[string]any{}, brb: map[string]any{}}
		check.video(idx.Video.Body, job.Profile)
		check.audio(idx.Audio.Body, job.Profile)
		if len(check.mismatches) > 0 {
			err = errors.New("Prepared video does not match the active profile")
		}
	}
	if err == nil {
		err = l.validateOriginal(job)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[job.ID]
	if e == nil || e.key != job.key || ctx.Err() == context.Canceled {
		return
	}
	if ctx.Err() == context.DeadlineExceeded {
		err = errors.New("Preparation exceeded the 12-hour deadline")
	}
	if err != nil || idx == nil {
		e.Message = ""
		e.State = "failed"
		e.Error = "Cannot prepare MP4; check the file, disk space and FFmpeg/FFprobe"
		if err != nil && strings.HasPrefix(err.Error(), "MP4 duration") {
			e.Error = err.Error()
		}
		return
	}
	e.State = "ready"
	e.Message = ""
	e.Progress = 100
	e.Duration = idx.Duration.Seconds()
	e.path = path
	e.index = idx
	e.Error = ""
	if err := l.retainRevision(e); err != nil {
		e.Message = ""
		e.State = "failed"
		e.Error = "Cannot retain prepared revision; check storage"
		return
	}
	// Keep originals; obsolete conversions can be unlinked after replacement.
	// An already-open playing file remains readable on the supported Unix hosts.
	files, _ := os.ReadDir(filepath.Join(l.root, "prepared"))
	for _, f := range files {
		if f.Type().IsRegular() && strings.HasPrefix(f.Name(), job.ID+"-") && f.Name() != job.key+".flv" {
			os.Remove(filepath.Join(l.root, "prepared", f.Name()))
		}
	}
}
func (l *videoLibrary) open(id string) (*clipPlayback, config.BRBProfile, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[id]
	if l.lastError != "" || e == nil || e.State != "ready" || e.Profile != l.profile {
		return nil, config.BRBProfile{}, errors.New("Choose a video that is ready for the active profile")
	}
	if err := l.validateOriginal(e); err != nil {
		return nil, e.Profile, err
	}
	reader, err := openClip(e.path, e.index)
	if err != nil {
		return nil, e.Profile, errors.New("Cannot open prepared video; prepare it again")
	}
	return &clipPlayback{ID: e.ID, Name: e.Name, Revision: e.Revision, reader: reader}, e.Profile, nil
}
func (l *videoLibrary) retry(id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[id]
	if e == nil {
		return errors.New("Video not found")
	}
	if e.State == "preparing" {
		return errors.New("Video is already preparing")
	}
	if e.State == "discovering" {
		return errors.New("Wait for the original MP4 to finish copying")
	}
	if err := l.validateOriginal(e); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(l.root, "prepared", e.key+".flv")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("Cannot remove old conversion; check storage")
	}
	e.Message = ""
	e.State = "queued"
	e.Error = ""
	e.Progress = 0
	l.notify()
	return nil
}
