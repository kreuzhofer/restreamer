package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
)

type brbSettings struct {
	Text        string            `json:"text"`
	Profile     config.BRBProfile `json:"profile"`
	Generation  string            `json:"generation"`
	CustomImage bool              `json:"custom_image"`
	Music       bool              `json:"music"`
	Volume      int               `json:"volume"`
}

func controlOriginAllowed(r *http.Request) bool {
	if r.Header.Get("X-Restreamer-Control") != "1" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host != r.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return false
		}
	}
	return true
}

func (s *Server) initializeBRB() error {
	if !s.cfg.BRB.IsEnabled() {
		return nil
	}
	root := s.cfg.BRB.Directory
	if err := os.MkdirAll(root, 0700); err != nil {
		return errors.New("cannot create BRB storage directory")
	}
	settings := brbSettings{Text: defaultBRBText, Volume: 50, Profile: s.cfg.BRB.BRBProfile}
	data, err := os.ReadFile(filepath.Join(root, "current.json"))
	if err == nil {
		if len(data) > 4096 || json.Unmarshal(data, &settings) != nil || !strings.HasPrefix(settings.Generation, "assets-") || strings.ContainsAny(settings.Generation, "/\\") || settings.Volume < 0 || settings.Volume > 100 {
			return errors.New("invalid BRB settings file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot read BRB settings")
	}
	settings.Text, err = normalizeBRBText(settings.Text)
	if err != nil {
		return err
	}
	if err := settings.Profile.Validate(); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(root, "assets-")
	if err != nil {
		return errors.New("cannot prepare BRB storage")
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(dir)
		}
	}()
	if settings.CustomImage {
		if err = copyAsset(filepath.Join(root, settings.Generation, "image.png"), filepath.Join(dir, "image.png")); err != nil {
			return err
		}
	} else if err = defaultBRBImage(filepath.Join(dir, "image.png"), settings.Text); err != nil {
		return err
	}
	if settings.Generation != "" {
		if settings.Music {
			if err = copyAsset(filepath.Join(root, settings.Generation, "music"), filepath.Join(dir, "music")); err != nil {
				return err
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	media, err := encodeBRB(ctx, settings.Profile, dir, settings.CustomImage, settings.Music, settings.Volume, settings.Text)
	if err != nil {
		return err
	}
	old := settings.Generation
	settings.Generation = filepath.Base(dir)
	if err = writeState(filepath.Join(root, "current.json"), settings); err != nil {
		return errors.New("cannot save BRB settings")
	}
	media.settings = settings
	s.broadcast = newBroadcast(s, media)
	keep = true
	if old != "" {
		os.RemoveAll(filepath.Join(root, old))
	}
	return nil
}

func copyAsset(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return errors.New("cannot read saved BRB asset")
	}
	defer f.Close()
	out, err := os.Create(dst)
	if err != nil {
		return errors.New("cannot prepare BRB asset")
	}
	_, err = io.Copy(out, io.LimitReader(f, 33<<20))
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot save BRB asset")
	}
	return nil
}

func (s *Server) brbImage(w http.ResponseWriter, r *http.Request) {
	if s.broadcast == nil {
		http.Error(w, "BRB is not configured", 409)
		return
	}
	s.broadcast.mu.Lock()
	if s.broadcast.media == nil {
		s.broadcast.mu.Unlock()
		http.Error(w, "BRB is not configured", 409)
		return
	}
	// Open while holding the same lock used to replace/delete an old generation.
	f, err := os.Open(filepath.Join(s.cfg.BRB.Directory, s.broadcast.media.settings.Generation, "image.png"))
	s.broadcast.mu.Unlock()
	if err != nil {
		http.Error(w, "BRB image unavailable", 503)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/png")
	io.Copy(w, f)
}

func (s *Server) brbAssets(w http.ResponseWriter, r *http.Request) {
	if !controlOriginAllowed(r) {
		http.Error(w, "Control requests require same origin", 403)
		return
	}
	typ, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if typ != "multipart/form-data" {
		http.Error(w, "Expected image/audio form", 400)
		return
	}
	if s.broadcast == nil {
		http.Error(w, "Configure BRB before uploading assets", 409)
		return
	}
	s.broadcast.mu.Lock()
	configured := s.broadcast.media != nil
	s.broadcast.mu.Unlock()
	if !configured {
		http.Error(w, "Configure BRB before uploading assets", 409)
		return
	}
	if !s.mediaMu.TryLock() {
		http.Error(w, "Another BRB upload is being prepared", 409)
		return
	}
	defer s.mediaMu.Unlock()
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(3 * time.Minute))
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(3 * time.Minute))
	r.Body = http.MaxBytesReader(w, r.Body, 43<<20)
	defer r.Body.Close()
	// All upload data stays in bounded memory, avoiding multipart spill files on
	// the read-only container filesystem. Processing uses the configured volume.
	if err := r.ParseMultipartForm(44 << 20); err != nil {
		http.Error(w, "Upload exceeds 43 MiB or is malformed", 400)
		return
	}
	defer r.MultipartForm.RemoveAll()
	for key, files := range r.MultipartForm.File {
		if (key != "image" && key != "music") || len(files) != 1 {
			http.Error(w, "Expected at most one image and one audio file", 400)
			return
		}
	}
	for key, values := range r.MultipartForm.Value {
		if (key != "text" && key != "volume" && key != "reset_image" && key != "remove_music" && key != "width" && key != "height" && key != "fps" && key != "sample_rate") || len(values) != 1 {
			http.Error(w, "Unknown or repeated BRB setting", 400)
			return
		}
	}
	s.broadcast.mu.Lock()
	settings := s.broadcast.media.settings
	s.broadcast.mu.Unlock()
	if values, ok := r.MultipartForm.Value["text"]; ok {
		value, err := normalizeBRBText(values[0])
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		settings.Text = value
	}
	if value := r.FormValue("volume"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 100 {
			http.Error(w, "Volume must be 0–100", 400)
			return
		}
		settings.Volume = n
	}
	oldProfile := settings.Profile
	for key, dst := range map[string]*int{"width": &settings.Profile.Width, "height": &settings.Profile.Height, "fps": &settings.Profile.FPS, "sample_rate": &settings.Profile.SampleRate} {
		if value := r.FormValue(key); value != "" {
			n, e := strconv.Atoi(value)
			if e != nil {
				http.Error(w, "Invalid BRB profile", 400)
				return
			}
			*dst = n
		}
	}
	if err := settings.Profile.Validate(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if oldProfile != settings.Profile {
		s.broadcast.mu.Lock()
		active := s.broadcast.control.stage != "OFF" || s.broadcast.control.pending != nil
		s.broadcast.mu.Unlock()
		if active {
			http.Error(w, "Stop the broadcast or rehearsal and cancel pending starts before changing the shared streaming profile", 409)
			return
		}
	}
	// Profile-only saves that match the active profile must not rebuild media.
	// Keep existing asset-update requests (including explicit re-preparation).
	profileOnly := len(r.MultipartForm.Value) > 0 && len(r.MultipartForm.File) == 0
	for key := range r.MultipartForm.Value {
		if key != "width" && key != "height" && key != "fps" && key != "sample_rate" {
			profileOnly = false
		}
	}
	if profileOnly && oldProfile == settings.Profile {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	root := s.cfg.BRB.Directory
	dir, err := os.MkdirTemp(root, "assets-")
	if err != nil {
		http.Error(w, "Cannot prepare BRB storage", 500)
		return
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(dir)
		}
	}()
	fail := func(err error) { http.Error(w, err.Error(), http.StatusUnprocessableEntity) }
	old := settings.Generation
	imagePath := filepath.Join(dir, "image.png")
	if file, header, e := r.FormFile("image"); e == nil {
		defer file.Close()
		if header.Size > 10<<20 {
			fail(errors.New("image must be at most 10 MiB"))
			return
		}
		if err = normalizeImage(file, imagePath); err != nil {
			fail(err)
			return
		}
		settings.CustomImage = true
	} else if r.FormValue("reset_image") == "true" {
		if err = defaultBRBImage(imagePath, settings.Text); err != nil {
			fail(errors.New("cannot prepare default image"))
			return
		}
		settings.CustomImage = false
	} else if err = copyAsset(filepath.Join(root, old, "image.png"), imagePath); err != nil {
		fail(err)
		return
	}
	if file, header, e := r.FormFile("music"); e == nil {
		defer file.Close()
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if header.Size > 32<<20 || (ext != ".mp3" && ext != ".wav") {
			fail(errors.New("choose an MP3 or WAV up to 32 MiB and 10 minutes"))
			return
		}
		out, e := os.Create(filepath.Join(dir, "music"))
		if e != nil {
			fail(errors.New("cannot save audio"))
			return
		}
		_, e = io.Copy(out, file)
		ce := out.Close()
		if e != nil || ce != nil {
			fail(errors.New("cannot save audio"))
			return
		}
		settings.Music = true
	} else if r.FormValue("remove_music") == "true" {
		settings.Music = false
	} else if settings.Music {
		if err = copyAsset(filepath.Join(root, old, "music"), filepath.Join(dir, "music")); err != nil {
			fail(err)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	media, err := encodeBRB(ctx, settings.Profile, dir, settings.CustomImage, settings.Music, settings.Volume, settings.Text)
	if err != nil {
		fail(err)
		return
	}
	settings.Generation = filepath.Base(dir)
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if oldProfile != settings.Profile {
		s.broadcast.mu.Lock()
		active := s.broadcast.control.stage != "OFF" || s.broadcast.control.pending != nil
		s.broadcast.mu.Unlock()
		if active {
			http.Error(w, "Profile unchanged: a broadcast, rehearsal, or pending start began during preparation", 409)
			return
		}
	}
	if err = writeState(filepath.Join(root, "current.json"), settings); err != nil {
		http.Error(w, "Cannot save BRB settings; previous assets remain active", 500)
		return
	}
	media.settings = settings
	s.broadcast.mu.Lock()
	s.broadcast.media = media
	if oldProfile != settings.Profile {
		if s.broadcast.closeInput != nil {
			s.broadcast.closeInput()
		}
		s.broadcast.resetInput()
	}
	if s.broadcast.active {
		s.broadcast.startFallback(time.Now())
	}
	os.RemoveAll(filepath.Join(root, old))
	s.broadcast.mu.Unlock()
	if s.library != nil && oldProfile != settings.Profile {
		s.library.setProfile(settings.Profile)
	}
	keep = true
	s.log.Info("BRB assets updated", "custom_image", settings.CustomImage, "music", settings.Music)
	w.WriteHeader(http.StatusNoContent)
}
