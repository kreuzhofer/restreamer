package relay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

// One retained record makes accepted work discoverable without adding a queue.
// mediaMu excludes BRB mutations; preparation shares encoder admission with jobs.
type brbThemePreparation struct {
	candidate   *brbThemeCandidate
	ID          string      `json:"id"`
	State       string      `json:"state"`
	Base        string      `json:"base_generation"`
	Settings    brbSettings `json:"settings"`
	CandidateID string      `json:"candidate_id,omitempty"`
	Error       string      `json:"error,omitempty"`
}

func (p *brbThemePreparation) pending() bool { return p.State == "running" || p.State == "cancelling" }
func (s *Server) saveBRBPreparation() error {
	return writeState(filepath.Join(s.cfg.BRB.Directory, "theme-preparation.json"), s.brbPreparation)
}
func (s *Server) loadBRBPreparation() error {
	data, err := os.ReadFile(filepath.Join(s.cfg.BRB.Directory, "theme-preparation.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("cannot read BRB preparation status")
	}
	var p brbThemePreparation
	if len(data) > 32<<10 || json.Unmarshal(data, &p) != nil || !validBRBGeneration(p.ID) || !validBRBGeneration(p.Base) {
		return errors.New("invalid BRB preparation status")
	}
	switch p.State {
	case "running", "cancelling", "ready", "failed", "cancelled", "interrupted":
	default:
		return errors.New("invalid BRB preparation state")
	}
	s.brbPreparation = &p
	if p.pending() {
		p.State = "interrupted"
		p.Error = "Server restarted during preparation. Current BRB and earlier candidate are unchanged; prepare again."
		return s.saveBRBPreparation()
	}
	return nil
}
func (s *Server) brbThemePreparationHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	s.brbPreparationMu.Lock()
	defer s.brbPreparationMu.Unlock()
	generatorJSON(w, 200, s.brbPreparation)
}
func (s *Server) brbThemePreparationCancelHTTP(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID string `json:"id"`
	}
	if !generatorDecode(w, r, &input) {
		return
	}
	s.brbPreparationMu.Lock()
	defer s.brbPreparationMu.Unlock()
	p := s.brbPreparation
	if p == nil || p.ID != input.ID {
		http.Error(w, "Preparation changed; reload its status before cancelling", 409)
		return
	}
	if p.pending() {
		previous := p.State
		p.State = "cancelling"
		if err := s.saveBRBPreparation(); err != nil {
			p.State = previous
			http.Error(w, "Cannot retain cancellation status", 507)
			return
		}
		s.brbPreparationCancel()
	}
	generatorJSON(w, 202, p)
}
func (s *Server) stopBRBPreparation() {
	s.brbPreparationMu.Lock()
	s.brbPreparationStopped = true
	done := s.brbPreparationDone
	if s.brbPreparationCancel != nil {
		s.brbPreparationCancel()
	}
	s.brbPreparationMu.Unlock()
	if done != nil {
		<-done
	}
}
func (s *Server) brbThemePrepare(w http.ResponseWriter, r *http.Request) {
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(10 * time.Second))
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(3 * time.Minute))
	if !s.generatorAvailable(w) {
		return
	}
	var input struct {
		Theme mediaauthor.ThemeRef `json:"theme"`
		Base  string               `json:"base_generation"`
		Async bool                 `json:"async"`
	}
	if !generatorDecode(w, r, &input) {
		return
	}
	if r.Context().Err() != nil {
		http.Error(w, "Request cancelled before preparation was accepted", 422)
		return
	}
	if !s.mediaMu.TryLock() {
		http.Error(w, "Another BRB preparation is running", 409)
		return
	}
	accepted := false
	defer func() {
		if !accepted {
			s.mediaMu.Unlock()
		}
	}()
	s.broadcast.mu.Lock()
	settings := s.broadcast.media.settings
	s.broadcast.mu.Unlock()
	if input.Base != settings.Generation {
		http.Error(w, "BRB settings changed; reload before preparing a theme", 409)
		return
	}
	if settings.Profile.Width > 1920 || settings.Profile.Height > 1080 || settings.Profile.FPS > 30 {
		http.Error(w, "Shared BRB themes support up to 1920 × 1080 and 30 fps; legacy BRB is unchanged", 422)
		return
	}
	// Hold catalog protection until captured references become visible to deletion checks.
	s.generator.mu.Lock()
	theme, err := s.generator.resolveTheme(input.Theme)
	if err != nil {
		s.generator.mu.Unlock()
		http.Error(w, "Choose an available exact theme revision", 422)
		return
	}
	dir, err := os.MkdirTemp(s.cfg.BRB.Directory, "assets-")
	if err != nil {
		s.generator.mu.Unlock()
		http.Error(w, "Cannot create BRB candidate storage", 507)
		return
	}
	source := settings
	settings.Theme = &theme
	p := &brbThemePreparation{ID: filepath.Base(dir), Base: input.Base, Settings: settings, State: "running"}
	s.brbPreparationMu.Lock()
	if s.brbPreparationStopped {
		s.brbPreparationMu.Unlock()
		s.generator.mu.Unlock()
		os.RemoveAll(dir)
		http.Error(w, "Server is stopping; prepare again after restart", 503)
		return
	}
	previous := s.brbPreparation
	s.brbPreparation = p
	if err = s.saveBRBPreparation(); err != nil {
		s.brbPreparation = previous
		s.brbPreparationMu.Unlock()
		s.generator.mu.Unlock()
		os.RemoveAll(dir)
		http.Error(w, "Cannot retain BRB preparation status", 507)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	done := make(chan struct{})
	s.brbPreparationCancel = cancel
	s.brbPreparationDone = done
	snapshot := *p
	s.brbPreparationMu.Unlock()
	s.generator.mu.Unlock()
	accepted = true
	go s.runBRBPreparation(ctx, cancel, done, p, source, dir)
	if input.Async {
		generatorJSON(w, 202, snapshot)
		return
	}
	select {
	case <-r.Context().Done():
		return
	case <-done:
	}
	s.brbPreparationMu.Lock()
	result := *p
	s.brbPreparationMu.Unlock()
	if result.State != "ready" {
		http.Error(w, result.Error, 422)
		return
	}
	generatorJSON(w, 201, result.candidate)
}
func (s *Server) runBRBPreparation(ctx context.Context, cancel context.CancelFunc, done chan struct{}, p *brbThemePreparation, source brbSettings, dir string) {
	defer cancel()
	candidate, err := s.buildBRBCandidate(ctx, p, source, dir)
	s.brbPreparationMu.Lock()
	// Serialize cancellation with candidate publication: a successful cancel can never publish.
	if ctx.Err() != nil || p.State == "cancelling" || s.brbPreparationStopped {
		if s.brbPreparationStopped {
			p.State = "interrupted"
			p.Error = "Server stopped during preparation. Prepare again after restart."
		} else if p.State == "cancelling" {
			p.State = "cancelled"
			p.Error = "Preparation cancelled. Current BRB and earlier candidate are unchanged."
		} else {
			p.State = "failed"
			p.Error = "Preparation timed out. Current BRB and earlier candidate are unchanged."
		}
	} else if err != nil {
		p.State = "failed"
		p.Error = err.Error()
	} else {
		s.brbCandidateMu.Lock()
		if err = writeState(filepath.Join(s.cfg.BRB.Directory, "theme-candidate.json"), candidate); err != nil {
			p.State = "failed"
			p.Error = "Cannot retain BRB candidate; current media is unchanged"
		} else {
			old := s.brbCandidate
			s.brbCandidate = candidate
			p.candidate = candidate
			p.State = "ready"
			p.CandidateID = candidate.ID
			if old != nil {
				os.RemoveAll(filepath.Join(s.cfg.BRB.Directory, old.ID))
			}
		}
		s.brbCandidateMu.Unlock()
	}
	if p.State != "ready" {
		os.RemoveAll(dir)
	}
	if s.saveBRBPreparation() != nil {
		p.Error = "Cannot retain final preparation status. Reload the candidate before preparing again."
		s.log.Error("Cannot retain BRB preparation status")
	}
	s.brbPreparationCancel = nil
	s.mediaMu.Unlock()
	close(done)
	s.brbPreparationMu.Unlock()
}
func (s *Server) buildBRBCandidate(ctx context.Context, p *brbThemePreparation, source brbSettings, dir string) (*brbThemeCandidate, error) {
	root := s.cfg.BRB.Directory
	settings := p.Settings
	if settings.CustomImage {
		if err := copyAsset(brbSourceImage(root, source), filepath.Join(dir, "image.png")); err != nil {
			return nil, err
		}
	}
	if settings.Music {
		if err := copyAsset(filepath.Join(root, p.Base, "music"), filepath.Join(dir, "music")); err != nil {
			return nil, err
		}
	}
	release, err := s.preparation.acquire(ctx)
	if err != nil {
		return nil, errors.New("BRB preparation interrupted while waiting for other media preparation")
	}
	defer release()
	media, err := s.encodeBRBSettings(ctx, settings, dir)
	if err != nil {
		return nil, err
	}
	args := []string{"-protocol_whitelist", "file,pipe", "-stream_loop", "-1", "-i", filepath.Join(dir, "video.flv"), "-protocol_whitelist", "file,pipe", "-stream_loop", "-1", "-i", filepath.Join(dir, "audio.flv"), "-map", "0:v:0", "-map", "1:a:0", "-t", "4", "-c", "copy", "-movflags", "+faststart", "-fs", "134217728", filepath.Join(dir, "preview.mp4")}
	if err = runBRBFFmpeg(ctx, args...); err != nil {
		return nil, errors.New("Cannot prepare exact BRB preview")
	}
	settings.Generation = p.ID
	media.settings = settings
	return &brbThemeCandidate{ID: p.ID, Base: p.Base, Settings: settings, AudioDuration: media.audio.duration.Seconds(), VideoDuration: media.video.duration.Seconds(), media: media}, nil
}
