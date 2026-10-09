package relay

import (
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"io/fs"
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
	private.HandleFunc("GET /api/stage", s.stageStatusHTTP)
	private.HandleFunc("POST /api/stage/commands", s.stageCommandHTTP)
	private.HandleFunc("GET /api/stage/commands/{id}", s.stageCommandOutcomeHTTP)
	private.HandleFunc("GET /api/preview", s.preview)
	private.HandleFunc("GET /api/broadcast-preview", s.preview)
	private.HandleFunc("GET /generator", func(w http.ResponseWriter, r *http.Request) {
		data, _ := dashboardFiles.ReadFile("web/generator.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})
	private.HandleFunc("GET /api/generator/themes", s.generatorThemesHTTP)
	private.HandleFunc("POST /api/generator/themes", s.generatorThemesHTTP)
	private.HandleFunc("PUT /api/generator/themes/{id}", s.generatorThemeHTTP)
	private.HandleFunc("GET /api/generator/themes/{id}/revisions/{revision}", s.generatorThemeHTTP)
	private.HandleFunc("POST /api/generator/preview", s.generatorPreviewHTTP)
	private.HandleFunc("POST /api/generator/validate", s.generatorPreviewHTTP)
	private.HandleFunc("GET /api/generator/templates", s.generatorTemplatesHTTP)
	private.HandleFunc("POST /api/generator/templates", s.generatorTemplatesHTTP)
	private.HandleFunc("GET /api/generator/templates/{id}", s.generatorTemplateHTTP)
	private.HandleFunc("PUT /api/generator/templates/{id}", s.generatorTemplateHTTP)
	private.HandleFunc("POST /api/generator/templates/{id}/designs", s.generatorTemplateCopyHTTP)
	private.HandleFunc("GET /api/generator/designs", s.generatorDesignsHTTP)
	private.HandleFunc("POST /api/generator/designs", s.generatorDesignsHTTP)
	private.HandleFunc("GET /api/generator/designs/{id}", s.generatorDesignHTTP)
	private.HandleFunc("PUT /api/generator/designs/{id}", s.generatorDesignHTTP)
	private.HandleFunc("GET /api/generator/assets", s.generatorAssetsHTTP)
	private.HandleFunc("POST /api/generator/assets", s.generatorAssetsHTTP)
	private.HandleFunc("POST /api/generator/assets/{id}/revisions", s.generatorAssetUpload)
	private.HandleFunc("GET /api/generator/assets/{id}/revisions/{revision}", s.generatorAssetImage)
	private.HandleFunc("DELETE /api/generator/assets/{id}", s.generatorAssetDelete)
	private.HandleFunc("GET /api/generator/jobs", s.generatorJobsHTTP)
	private.HandleFunc("POST /api/generator/jobs", s.generatorJobsHTTP)
	private.HandleFunc("GET /api/generator/jobs/{id}", s.generatorJobHTTP)
	private.HandleFunc("POST /api/generator/jobs/{id}/cancel", s.generatorCancelHTTP)
	private.HandleFunc("POST /api/generator/jobs/{id}/retry", s.generatorRetryHTTP)
	private.HandleFunc("GET /api/library", s.libraryStatus)
	private.HandleFunc("GET /api/stage-media", s.stageMediaSettings)
	private.HandleFunc("PUT /api/stage-media", s.stageMediaSettings)
	private.HandleFunc("PUT /api/library/prepare", s.libraryPrepare)
	private.HandleFunc("GET /api/library/revisions/{revision}/preview", s.libraryRevisionPreview)
	private.HandleFunc("POST /api/library/upload", s.libraryUpload)
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
		Stage          StageStatus     `json:"stage"`
	}{Time: now.UnixMilli(), Publishing: s.active.Load(), InputBytes: s.inputBytes.Load(), InputFrames: s.inputFrames.Load(), Outputs: make([]OutputStatus, 0, len(s.outputs)), Persistent: s.cfg.StateFile != ""}
	for _, o := range s.outputs {
		status.Outputs = append(status.Outputs, o.snapshot())
	}
	status.Forwarding = s.forwarding.Load()
	if s.broadcast != nil {
		status.Stage = s.broadcast.stageStatus(now)
		status.BRB = s.broadcast.status()
		status.Playback = s.broadcast.playbackStatus(now)
		status.LibraryEnabled = s.library != nil
		s.broadcast.mu.Lock()
		if s.broadcast.media != nil {
			status.BRBAssets = s.broadcast.media.settings
		}
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
