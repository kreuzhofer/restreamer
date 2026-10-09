package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

// One retained candidate bounds memory and storage independently from the active BRB.
type brbThemeCandidate struct {
	ID            string      `json:"id"`
	Base          string      `json:"base_generation"`
	Settings      brbSettings `json:"settings"`
	AudioDuration float64     `json:"audio_duration"`
	VideoDuration float64     `json:"video_duration"`
	media         *brbMedia
}

func brbSourceImage(root string, settings brbSettings) string {
	name := "image.png"
	if settings.Theme != nil && settings.CustomImage {
		name = "source.png"
	}
	return filepath.Join(root, settings.Generation, name)
}
func validBRBGeneration(id string) bool {
	return strings.HasPrefix(id, "assets-") && len(id) <= 80 && !strings.ContainsAny(id, "/\\")
}

func loadThemedBRB(root string, settings brbSettings) (*brbMedia, error) {
	if !validBRBGeneration(settings.Generation) || settings.Theme == nil || len(mediaauthor.ValidateStyle(settings.Theme.Style)) > 0 || settings.Theme.Revision < 1 || (settings.Theme.ID != "retro" && !validDesignID(settings.Theme.ID)) {
		return nil, errors.New("invalid captured BRB theme")
	}
	if err := settings.Profile.Validate(); err != nil {
		return nil, err
	}
	if _, err := normalizeBRBText(settings.Text); err != nil || settings.Volume < 0 || settings.Volume > 100 {
		return nil, errors.New("invalid retained BRB content settings")
	}
	for _, ref := range mediaauthor.ThemeAssetRefs(*settings.Theme) {
		if !validDesignID(ref.ID) || ref.Revision < 1 {
			return nil, errors.New("invalid retained BRB theme asset reference")
		}
	}

	dir := filepath.Join(root, settings.Generation)
	v, err := readMediaTrack(filepath.Join(dir, "video.flv"), rtmp.Video, time.Second/time.Duration(settings.Profile.FPS))
	if err != nil {
		return nil, errors.New("cannot read retained themed BRB video")
	}
	a, err := readMediaTrack(filepath.Join(dir, "audio.flv"), rtmp.Audio, 1024*time.Second/time.Duration(settings.Profile.SampleRate))
	if err != nil {
		return nil, errors.New("cannot read retained themed BRB audio")
	}
	if len(v.frames) != 4*settings.Profile.FPS || a.duration > 600*time.Second {
		return nil, errors.New("retained themed BRB timing is invalid")
	}
	for _, m := range []*rtmp.Message{v.header, a.header} {
		check := brbInputCheck{ingest: map[string]any{}}
		if m.Type == rtmp.Video {
			check.video(m.Body, settings.Profile)
		} else {
			check.audio(m.Body, settings.Profile)
		}
		if len(check.mismatches) > 0 {
			return nil, errors.New("retained themed BRB does not match its profile")
		}
	}
	v.duration = 4 * time.Second
	return &brbMedia{video: v, audio: a, settings: settings}, nil
}

func (s *Server) encodeBRBSettings(ctx context.Context, settings brbSettings, dir string) (*brbMedia, error) {
	if settings.Theme == nil {
		return encodeBRB(ctx, settings.Profile, dir, settings.CustomImage, settings.Music, settings.Volume, settings.Text)
	}
	p := settings.Profile
	if p.Width > 1920 || p.Height > 1080 || p.FPS > 30 {
		return nil, errors.New("Shared BRB themes support up to 1920 × 1080 and 30 fps; keep legacy BRB or change the shared profile while stopped")
	}
	if len(mediaauthor.ValidateStyle(settings.Theme.Style)) > 0 {
		return nil, errors.New("Captured BRB theme settings are invalid")
	}
	inputs, err := s.loadThemeInputs(*settings.Theme, p.Width, p.Height)
	if err != nil {
		return nil, err
	}
	scene := mediaauthor.Scene{ID: "brb", Layout: "title", Text: settings.Text, DurationSeconds: 4}
	if settings.CustomImage {
		f, err := os.Open(filepath.Join(dir, "image.png"))
		if err != nil {
			return nil, errors.New("cannot read BRB custom image")
		}
		inputs.Image, err = png.Decode(f)
		f.Close()
		if err != nil {
			return nil, errors.New("cannot decode BRB custom image")
		}
		if err = copyAsset(filepath.Join(dir, "image.png"), filepath.Join(dir, "source.png")); err != nil {
			return nil, err
		}
		scene.Layout = "media"
		// Existing BRB uploads are pinned by their captured generation, independently of the authoring catalog.
		scene.Image = &mediaauthor.AssetRef{ID: settings.Generation, Revision: 1}
		scene.Text = ""
	}
	raster, err := mediaauthor.RenderSceneStyled(scene, p.Width, p.Height, settings.Theme.Style, inputs, -1, p.FPS)
	if err != nil {
		return nil, fmt.Errorf("BRB layout: %s", err.Error())
	}
	defer os.Remove(filepath.Join(dir, "base.png"))
	defer os.Remove(filepath.Join(dir, "effect.png"))
	if err = saveGeneratorRaster(filepath.Join(dir, "base.png"), raster); err != nil {
		return nil, err
	}
	poster, err := mediaauthor.RenderSceneStyled(scene, p.Width, p.Height, settings.Theme.Style, inputs, 0, p.FPS)
	if err != nil {
		return nil, errors.New("cannot render themed BRB poster")
	}
	if err = saveGeneratorRaster(filepath.Join(dir, "image.png"), poster); err != nil {
		return nil, err
	}
	args := []string{"-filter_threads", "1", "-filter_complex_threads", "1", "-protocol_whitelist", "file,pipe", "-threads", "2", "-loop", "1", "-framerate", strconv.Itoa(p.FPS), "-i", filepath.Join(dir, "base.png")}
	style := settings.Theme.Style
	if style.Effect == "none" {
		args = append(args, "-map", "0:v:0", "-vf", "setsar=1,format=yuv420p")
	} else {
		sprite := filepath.Join(dir, "effect.png")
		if err = saveGeneratorRaster(sprite, mediaauthor.EffectSprite(p.Height, style)); err != nil {
			return nil, err
		}
		start, y, step, slots := mediaauthor.EffectGeometry(p.Width, p.Height)
		filter := fmt.Sprintf("[0:v][1:v]overlay=x='%d+mod(floor(t*%d),%d)*%d':y=%d:format=auto,setsar=1,format=yuv420p[v]", start, style.EffectSpeed, slots, step, y)
		args = append(args, "-threads", "2", "-loop", "1", "-framerate", strconv.Itoa(p.FPS), "-i", sprite, "-filter_complex", filter, "-map", "[v]")
	}
	args = append(args, "-an", "-frames:v", strconv.Itoa(4*p.FPS), "-c:v", "libx264", "-preset", "veryfast", "-tune", "stillimage", "-profile:v", "high", "-bf", "0", "-g", strconv.Itoa(p.FPS), "-threads", "2", "-fs", strconv.Itoa(maxMediaBytes), "-f", "flv", filepath.Join(dir, "video.flv"))
	if err = runBRBFFmpeg(ctx, args...); err != nil {
		return nil, err
	}
	if err = encodeBRBAudio(ctx, p, dir, settings.Music, settings.Volume); err != nil {
		return nil, err
	}
	settings.Generation = filepath.Base(dir)
	return loadThemedBRB(filepath.Dir(dir), settings)
}

func (s *Server) loadBRBCandidate() error {
	data, err := os.ReadFile(filepath.Join(s.cfg.BRB.Directory, "theme-candidate.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s.cleanBRBGenerations("")
	}
	if err != nil {
		return errors.New("cannot read prepared BRB candidate")
	}
	var candidate brbThemeCandidate
	if len(data) > 32<<10 || json.Unmarshal(data, &candidate) != nil || candidate.ID != candidate.Settings.Generation || !validBRBGeneration(candidate.Base) {
		return errors.New("invalid prepared BRB candidate")
	}
	if candidate.ID == s.broadcast.media.settings.Generation {
		if err := os.Remove(filepath.Join(s.cfg.BRB.Directory, "theme-candidate.json")); err != nil {
			return err
		}
		return s.cleanBRBGenerations("")
	}
	candidate.media, err = loadThemedBRB(s.cfg.BRB.Directory, candidate.Settings)
	if err != nil {
		return err
	}
	candidate.AudioDuration = candidate.media.audio.duration.Seconds()
	candidate.VideoDuration = candidate.media.video.duration.Seconds()
	s.brbCandidate = &candidate
	return s.cleanBRBGenerations(candidate.ID)
}

// Generation directories are server-owned. Remove interrupted preparations only
// after current and candidate metadata have both been validated successfully.
func (s *Server) cleanBRBGenerations(candidate string) error {
	root := s.cfg.BRB.Directory
	entries, err := os.ReadDir(root)
	if err != nil {
		return errors.New("cannot inspect BRB generation storage")
	}
	current := s.broadcast.media.settings.Generation
	for _, entry := range entries {
		if entry.IsDir() && validBRBGeneration(entry.Name()) && entry.Name() != current && entry.Name() != candidate {
			if os.RemoveAll(filepath.Join(root, entry.Name())) != nil {
				return errors.New("cannot clean interrupted BRB preparation")
			}
		}
	}
	return nil
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
	}
	if !generatorDecode(w, r, &input) {
		return
	}
	if !s.mediaMu.TryLock() {
		http.Error(w, "Another BRB preparation is running", 409)
		return
	}
	defer s.mediaMu.Unlock()
	s.broadcast.mu.Lock()
	settings := s.broadcast.media.settings
	s.broadcast.mu.Unlock()
	if input.Base != settings.Generation {
		http.Error(w, "BRB settings changed; reload before preparing a theme", 409)
		return
	}
	g := s.generator
	g.mu.Lock()
	theme, err := g.resolveTheme(input.Theme)
	g.mu.Unlock()
	if err != nil {
		http.Error(w, "Choose an available exact theme revision", 422)
		return
	}
	sourceSettings := settings
	settings.Theme = &theme
	root := s.cfg.BRB.Directory
	dir, err := os.MkdirTemp(root, "assets-")
	if err != nil {
		http.Error(w, "Cannot create BRB candidate storage", 507)
		return
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(dir)
		}
	}()
	if settings.CustomImage {
		if err = copyAsset(brbSourceImage(root, sourceSettings), filepath.Join(dir, "image.png")); err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
	}
	if settings.Music {
		if err = copyAsset(filepath.Join(root, input.Base, "music"), filepath.Join(dir, "music")); err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	release, err := s.preparation.acquire(ctx)
	if err != nil {
		http.Error(w, "BRB preparation cancelled while waiting for other media preparation", 422)
		return
	}
	defer release()
	media, err := s.encodeBRBSettings(ctx, settings, dir)
	if err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	// Preview remuxes the exact encoded tracks. Music still loops independently on air.
	args := []string{"-protocol_whitelist", "file,pipe", "-stream_loop", "-1", "-i", filepath.Join(dir, "video.flv"), "-protocol_whitelist", "file,pipe", "-stream_loop", "-1", "-i", filepath.Join(dir, "audio.flv"), "-map", "0:v:0", "-map", "1:a:0", "-t", "4", "-c", "copy", "-movflags", "+faststart", "-fs", "134217728", filepath.Join(dir, "preview.mp4")}
	if err = runBRBFFmpeg(ctx, args...); err != nil {
		http.Error(w, "Cannot prepare exact BRB preview", 422)
		return
	}
	if ctx.Err() != nil {
		http.Error(w, "BRB preparation cancelled; current media is unchanged", 422)
		return
	}
	settings.Generation = filepath.Base(dir)
	media.settings = settings
	candidate := &brbThemeCandidate{ID: settings.Generation, Base: input.Base, Settings: settings, AudioDuration: media.audio.duration.Seconds(), VideoDuration: media.video.duration.Seconds(), media: media}
	s.brbCandidateMu.Lock()
	defer s.brbCandidateMu.Unlock()
	if err = writeState(filepath.Join(root, "theme-candidate.json"), candidate); err != nil {
		http.Error(w, "Cannot retain BRB candidate; current media is unchanged", 507)
		return
	}
	old := s.brbCandidate
	s.brbCandidate = candidate
	keep = true
	if old != nil {
		os.RemoveAll(filepath.Join(root, old.ID))
	}
	generatorJSON(w, 201, candidate)
}
func (s *Server) brbThemeCandidateHTTP(w http.ResponseWriter, r *http.Request) {
	s.brbCandidateMu.Lock()
	candidate := s.brbCandidate
	s.brbCandidateMu.Unlock()
	generatorJSON(w, 200, candidate)
}
func (s *Server) brbThemePreview(w http.ResponseWriter, r *http.Request) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(3 * time.Minute))
	s.brbCandidateMu.Lock()
	candidate := s.brbCandidate
	if candidate == nil || (r.URL.Query().Get("id") != "" && r.URL.Query().Get("id") != candidate.ID) {
		s.brbCandidateMu.Unlock()
		http.Error(w, "No prepared BRB candidate", 404)
		return
	}
	f, err := os.Open(filepath.Join(s.cfg.BRB.Directory, candidate.ID, "preview.mp4"))
	s.brbCandidateMu.Unlock()
	if err != nil {
		http.Error(w, "Prepared BRB preview unavailable", 503)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "Prepared BRB preview unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, "preview.mp4", info.ModTime(), f)
}
func (s *Server) brbThemeActivate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID   string `json:"id"`
		Base string `json:"base_generation"`
	}
	if !generatorDecode(w, r, &input) {
		return
	}
	if !s.mediaMu.TryLock() {
		http.Error(w, "Another BRB preparation is running", 409)
		return
	}
	defer s.mediaMu.Unlock()
	s.brbCandidateMu.Lock()
	defer s.brbCandidateMu.Unlock()
	candidate := s.brbCandidate
	if candidate == nil || candidate.ID != input.ID || candidate.Base != input.Base {
		http.Error(w, "Prepared BRB candidate changed; preview it again", 409)
		return
	}
	s.controlMu.Lock()
	s.broadcast.mu.Lock()
	current := s.broadcast.media.settings
	s.broadcast.mu.Unlock()
	if current.Generation != candidate.Base || current.Profile != candidate.Settings.Profile {
		s.controlMu.Unlock()
		http.Error(w, "BRB settings or profile changed; prepare and preview again", 409)
		return
	}
	if err := writeState(filepath.Join(s.cfg.BRB.Directory, "current.json"), candidate.Settings); err != nil {
		s.controlMu.Unlock()
		http.Error(w, "Cannot save BRB settings; previous media remains active", 507)
		return
	}
	s.broadcast.mu.Lock()
	s.broadcast.media = candidate.media
	if s.broadcast.active {
		s.broadcast.startFallback(time.Now())
	}
	s.broadcast.mu.Unlock()
	s.controlMu.Unlock()
	s.brbCandidate = nil
	os.Remove(filepath.Join(s.cfg.BRB.Directory, "theme-candidate.json"))
	os.RemoveAll(filepath.Join(s.cfg.BRB.Directory, current.Generation))
	w.WriteHeader(204)
}
func (s *Server) brbThemeDiscard(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID string `json:"id"`
	}
	if !generatorDecode(w, r, &input) {
		return
	}
	if !s.mediaMu.TryLock() {
		http.Error(w, "Cancel the running preparation before discarding its candidate", 409)
		return
	}
	defer s.mediaMu.Unlock()
	s.brbCandidateMu.Lock()
	defer s.brbCandidateMu.Unlock()
	if s.brbCandidate == nil || s.brbCandidate.ID != input.ID {
		http.Error(w, "Prepared candidate changed; reload it", 409)
		return
	}
	if err := os.Remove(filepath.Join(s.cfg.BRB.Directory, "theme-candidate.json")); err != nil {
		http.Error(w, "Cannot discard BRB candidate; retry", 507)
		return
	}
	os.RemoveAll(filepath.Join(s.cfg.BRB.Directory, s.brbCandidate.ID))
	s.brbCandidate = nil
	w.WriteHeader(204)
}
