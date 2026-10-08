package relay

import (
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"time"
)

//go:embed web/*
var dashboardFiles embed.FS

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})
	private := http.NewServeMux()
	private.HandleFunc("GET /status", s.dashboardStatus)
	private.HandleFunc("GET /api/dashboard", s.dashboardStatus)
	private.HandleFunc("PUT /api/targets/{name}", s.targetControl)
	private.HandleFunc("PUT /api/forwarding", s.targetControl)
	private.HandleFunc("GET /api/preview", s.preview)
	private.HandleFunc("GET /api/broadcast-preview", s.preview)
	private.HandleFunc("GET /api/library", s.libraryStatus)
	private.HandleFunc("POST /api/library/upload", s.libraryUpload)
	private.HandleFunc("PUT /api/playback", s.playbackControl)
	private.HandleFunc("PUT /api/brb", s.targetControl)
	private.HandleFunc("POST /api/brb/assets", s.brbAssets)
	private.HandleFunc("GET /api/brb/image", s.brbImage)
	assets, _ := fs.Sub(dashboardFiles, "web")
	private.Handle("GET /", http.FileServer(http.FS(assets)))
	mux.Handle("/", s.authenticate(private))
	return mux
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; media-src 'self' blob:; img-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if s.cfg.DashboardUsername == "" || s.cfg.DashboardPassword == "" {
			http.Error(w, "Dashboard disabled: configure DASHBOARD_USERNAME and DASHBOARD_PASSWORD.", http.StatusServiceUnavailable)
			return
		}
		user, password, ok := r.BasicAuth()
		expectedUser, expectedPassword := sha256.Sum256([]byte(s.cfg.DashboardUsername)), sha256.Sum256([]byte(s.cfg.DashboardPassword))
		actualUser, actualPassword := sha256.Sum256([]byte(user)), sha256.Sum256([]byte(password))
		validUser := subtle.ConstantTimeCompare(expectedUser[:], actualUser[:])
		validPassword := subtle.ConstantTimeCompare(expectedPassword[:], actualPassword[:])
		if !ok || validUser&validPassword != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restreamer", charset="UTF-8"`)
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}
		if err := s.initialize(); err != nil {
			http.Error(w, err.Error(), 503)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) dashboardStatus(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	status := struct {
		Time           int64           `json:"time"`
		Publishing     bool            `json:"publishing"`
		Forwarding     bool            `json:"forwarding"`
		InputBytes     uint64          `json:"input_bytes"`
		InputFrames    uint64          `json:"input_frames"`
		Outputs        []OutputStatus  `json:"outputs"`
		History        []bitrateSample `json:"history,omitempty"`
		Persistent     bool            `json:"persistent"`
		Playback       PlaybackStatus  `json:"playback"`
		LibraryEnabled bool            `json:"library_enabled"`
		BRB            BRBStatus       `json:"brb"`
		BRBAssets      brbSettings     `json:"brb_assets"`
		BRBProfile     any             `json:"brb_profile,omitempty"`
	}{Time: now.UnixMilli(), Publishing: s.active.Load(), InputBytes: s.inputBytes.Load(), InputFrames: s.inputFrames.Load(), Outputs: make([]OutputStatus, 0, len(s.outputs)), Persistent: s.cfg.StateFile != ""}
	for _, o := range s.outputs {
		status.Outputs = append(status.Outputs, o.snapshot())
	}
	status.Forwarding = s.forwarding.Load()
	if s.broadcast != nil {
		status.BRB = s.broadcast.status()
		status.Playback = s.broadcast.playbackStatus(now)
		status.LibraryEnabled = s.library != nil
		s.broadcast.mu.Lock()
		status.BRBAssets = s.broadcast.media.settings
		s.broadcast.mu.Unlock()
		b := status.BRBAssets.Profile
		status.BRBProfile = map[string]int{"width": b.Width, "height": b.Height, "fps": b.FPS, "sample_rate": b.SampleRate}
	}
	if r.URL.Path == "/api/dashboard" {
		status.History = s.history(now)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) targetControl(w http.ResponseWriter, r *http.Request) {
	// A custom header plus JSON forces cross-origin browser requests through a
	// preflight, which this server does not allow. Also reject hostile Origins.
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !controlOriginAllowed(r) || mediaType != "application/json" {
		http.Error(w, "Control requests require same-origin JSON", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	defer r.Body.Close()
	var body struct {
		Enabled   *bool `json:"enabled"`
		Confirmed bool  `json:"confirmed,omitempty"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || body.Enabled == nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "Expected one JSON object with enabled: true or false", http.StatusBadRequest)
		return
	}
	if r.URL.Path == "/api/brb" {
		if s.broadcast == nil {
			http.Error(w, "BRB is not configured", 409)
			return
		}
		s.broadcast.setManual(*body.Enabled)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/api/forwarding" {
		if !*body.Enabled && !body.Confirmed {
			http.Error(w, "Confirm turning master forwarding off: this ends the broadcast, including BRB.", http.StatusConflict)
			return
		}
		s.setForwarding(*body.Enabled)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.setTarget(r.PathValue("name"), *body.Enabled); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, errUnknownTarget) {
			code = http.StatusNotFound
		}
		if errors.Is(err, errTargetUnavailable) {
			code = http.StatusConflict
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
