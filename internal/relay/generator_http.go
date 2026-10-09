package relay

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func generatorJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func (s *Server) generatorAvailable(w http.ResponseWriter) bool {
	if s.generator == nil {
		http.Error(w, "Enable BRB and its video library to author media", 409)
		return false
	}
	return true
}

func generatorDecode(w http.ResponseWriter, r *http.Request, dst any) bool {
	return generatorDecodeLimit(w, r, dst, maxDesignBytes)
}

func generatorDecodeLimit(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	typ, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !controlOriginAllowed(r) || typ != "application/json" {
		http.Error(w, "Control requests require same origin and JSON", 403)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(dst) != nil || dec.Decode(new(any)) != io.EOF {
		http.Error(w, fmt.Sprintf("Invalid or oversized authoring JSON (maximum %d KiB)", limit/1024), 400)
		return false
	}
	return true
}

func (s *Server) generatorDesignsHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	g := s.generator
	var draft mediaauthor.Design
	if r.Method == "POST" {
		if !generatorDecode(w, r, &draft) {
			return
		}
		if draft.Scenes == nil {
			draft.Scenes = []mediaauthor.Scene{{ID: "scene-1", Layout: "title", DurationSeconds: 10}}
			if draft.Theme.ID == "" && draft.Theme.Revision == 0 {
				draft.Theme = mediaauthor.ThemeRef{ID: "retro", Revision: 1}
			}
		}
		if !designBounds(draft) {
			http.Error(w, "Design requires prestream or ending, at most 20 scenes, a name up to 180 bytes, and at most 4096 text bytes per scene", 400)
			return
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	designs, err := g.list()
	if err != nil {
		http.Error(w, "Cannot read drafts; check generator storage", 503)
		return
	}
	if r.Method == "GET" {
		generatorJSON(w, 200, designs)
		return
	}
	if len(designs) >= maxDesigns {
		http.Error(w, "Draft limit reached (200 designs)", 409)
		return
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		http.Error(w, "Cannot create draft identity", 503)
		return
	}
	if issues := append(g.assetIssues(draft), g.themeIssues(draft.Theme)...); len(issues) > 0 {
		generatorJSON(w, 422, map[string]any{"issues": issues})
		return
	}
	draft.ID = hex.EncodeToString(id[:])
	draft.Version = 1
	draft.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err = g.write(draft); err != nil {
		if errors.Is(err, errDesignTooLarge) {
			http.Error(w, "Design exceeds 64 KiB after JSON encoding; reduce its content and retry", 400)
			return
		}
		http.Error(w, "Draft not saved; check generator storage and retry", 507)
		return
	}
	generatorJSON(w, 201, draft)
}

func (s *Server) generatorDesignHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var draft mediaauthor.Design
	if r.Method == "PUT" {
		if !generatorDecode(w, r, &draft) {
			return
		}
		if !designBounds(draft) {
			http.Error(w, "Invalid design structure or input limits", 400)
			return
		}
	}
	g := s.generator
	g.mu.Lock()
	defer g.mu.Unlock()
	current, err := g.read(r.PathValue("id"))
	if errors.Is(err, os.ErrNotExist) {
		http.Error(w, "Design not found", 404)
		return
	}
	if err != nil {
		http.Error(w, "Cannot read draft; check generator storage", 503)
		return
	}
	if r.Method == "GET" {
		generatorJSON(w, 200, current)
		return
	}
	if draft.Version != current.Version {
		generatorJSON(w, 409, map[string]any{"error": "A newer draft was saved in another tab. Reload or save your local work as a copy.", "current": current})
		return
	}
	if issues := append(g.assetIssues(draft), g.themeIssues(draft.Theme)...); len(issues) > 0 {
		generatorJSON(w, 422, map[string]any{"issues": issues})
		return
	}
	draft.ID = current.ID
	draft.Version = current.Version + 1
	draft.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err = g.write(draft); err != nil {
		if errors.Is(err, errDesignTooLarge) {
			http.Error(w, "Design exceeds 64 KiB after JSON encoding; reduce its content and retry", 400)
			return
		}
		http.Error(w, "Draft not saved; check generator storage and retry", 507)
		return
	}
	generatorJSON(w, 200, draft)
}

func (s *Server) generatorPreviewHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var draft mediaauthor.Design
	if !generatorDecode(w, r, &draft) {
		return
	}
	if !designBounds(draft) {
		http.Error(w, "Invalid design structure or input limits", 400)
		return
	}
	if !s.generator.previewMu.TryLock() {
		http.Error(w, "Another scene preview is rendering; retry shortly", 429)
		return
	}
	defer s.generator.previewMu.Unlock()
	s.library.mu.Lock()
	profile := s.library.profile
	s.library.mu.Unlock()
	s.generator.mu.Lock()
	theme, themeErr := s.generator.resolveTheme(draft.Theme)
	s.generator.mu.Unlock()
	issues := mediaauthor.ValidateWithTheme(draft, theme, profile.Width, profile.Height)
	if themeErr != nil {
		issues = append(issues, mediaauthor.Issue{Field: "theme", Message: themeErr.Error()})
	}
	s.generator.mu.Lock()
	issues = append(issues, s.generator.assetIssues(draft)...)
	issues = append(issues, s.generator.themeIssues(draft.Theme)...)
	issues = append(issues, s.generator.videoIssues(draft, profile.FPS)...)
	issues = append(issues, s.generator.musicIssues(draft, profile.FPS, profile.SampleRate)...)
	s.generator.mu.Unlock()
	if r.URL.Path == "/api/generator/validate" {
		previewIssues := append(append([]mediaauthor.Issue{}, issues...), mediaauthor.ValidatePreviewTiming(draft, profile.FPS)...)
		issues = append(issues, mediaauthor.ValidateTiming(draft, profile.FPS)...)
		generatorJSON(w, 200, map[string]any{"issues": issues, "preview_issues": previewIssues, "profile": profile, "duration_seconds": mediaauthor.SequenceDuration(draft, profile.FPS)})
		return
	}
	issues = append(issues, mediaauthor.ValidatePreviewTiming(draft, profile.FPS)...)
	sceneIndex := 0
	if selected := r.URL.Query().Get("scene"); selected != "" {
		var err error
		sceneIndex, err = strconv.Atoi(selected)
		if err != nil || sceneIndex < 0 || sceneIndex >= len(draft.Scenes) {
			http.Error(w, "Select an existing scene", 400)
			return
		}
	}
	selectedIssues := make([]mediaauthor.Issue, 0)
	for _, issue := range issues {
		if !strings.HasPrefix(issue.Field, "scenes.") || strings.HasPrefix(issue.Field, fmtSceneField(sceneIndex, "")) {
			selectedIssues = append(selectedIssues, issue)
		}
	}
	if len(selectedIssues) > 0 {
		generatorJSON(w, 422, map[string]any{"issues": selectedIssues})
		return
	}
	if draft.Scenes[sceneIndex].IsVideo() || animatedArcade(theme.Style) {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(35 * time.Second))
	}
	asset, err := s.loadScenePreview(r.Context(), draft.Scenes[sceneIndex])
	if err != nil {
		generatorJSON(w, 422, map[string]any{"issues": []mediaauthor.Issue{{Field: fmtSceneField(sceneIndex, "image"), Message: err.Error()}}})
		return
	}
	inputs, err := s.loadThemeInputs(theme, profile.Width, profile.Height)
	if err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	inputs.Image = asset
	frame := 0
	if value := r.URL.Query().Get("frame"); value != "" {
		frame, err = strconv.Atoi(value)
		if err != nil || frame < 0 || frame > profile.FPS*3600 {
			http.Error(w, "Preview frame must be within 3600 seconds", 400)
			return
		}
	}
	if animatedArcade(theme.Style) {
		inputs.Background, err = arcadePreviewFrame(r.Context(), profile, frame, theme.Style)
		if err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
	}
	img, err := mediaauthor.RenderSceneStyled(draft.Scenes[sceneIndex], profile.Width, profile.Height, theme.Style, inputs, frame, profile.FPS)
	if err != nil {
		http.Error(w, "Cannot render scene preview", 422)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	png.Encode(w, img)
}
