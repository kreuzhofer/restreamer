package relay

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	xdraw "golang.org/x/image/draw"
)

const maxThemeRevisions = 200

func (g *generatorStore) loadThemes() error {
	g.themes = map[mediaauthor.ThemeRef]mediaauthor.Theme{}
	for _, t := range mediaauthor.BuiltinThemes() {
		g.themes[mediaauthor.ThemeRef{ID: t.ID, Revision: t.Revision}] = t
	}
	files, err := os.ReadDir(g.themesRoot)
	if err != nil {
		return errors.New("cannot read theme storage")
	}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		var t mediaauthor.Theme
		if readMediaJSON(filepath.Join(g.themesRoot, f.Name()), &t) != nil || !validDesignID(t.ID) || t.Revision < 1 || t.Builtin || !validThemeName(t.Name) || len(mediaauthor.ValidateStyle(t.Style)) > 0 || f.Name() != fmt.Sprintf("%s-%d.json", t.ID, t.Revision) {
			return errors.New("invalid saved theme revision")
		}
		if len(g.themes) >= maxThemeRevisions+len(mediaauthor.BuiltinThemes()) {
			return errors.New("theme revision limit exceeded")
		}
		g.themes[mediaauthor.ThemeRef{ID: t.ID, Revision: t.Revision}] = t
	}
	return nil
}
func validThemeName(name string) bool {
	return utf8.ValidString(name) && len(name) <= 180 && strings.TrimSpace(name) != ""
}
func (g *generatorStore) resolveTheme(ref mediaauthor.ThemeRef) (mediaauthor.Theme, error) {
	t, ok := g.themes[ref]
	if !ok {
		return t, errors.New("The selected theme revision is unavailable. Choose an available revision explicitly.")
	}
	return t, nil
}
func (g *generatorStore) themeIssues(ref mediaauthor.ThemeRef) []mediaauthor.Issue {
	t, err := g.resolveTheme(ref)
	if err != nil {
		return []mediaauthor.Issue{{Field: "theme", Message: err.Error()}}
	}
	return g.themeImageIssues(t.Style, "theme.")
}
func (g *generatorStore) themeStyleAssetIssues(style mediaauthor.Style) []mediaauthor.Issue {
	return g.themeImageIssues(style, "style.")
}

// Both the style editor and pinned design themes validate the same exact images.
func (g *generatorStore) themeImageIssues(style mediaauthor.Style, prefix string) []mediaauthor.Issue {
	out := make([]mediaauthor.Issue, 0)
	for _, entry := range []struct {
		field string
		ref   *mediaauthor.AssetRef
	}{{"background", style.Background}, {"logo", style.Logo}} {
		ref := entry.ref
		if ref == nil {
			continue
		}
		message := ""
		if !validDesignID(ref.ID) || ref.Revision < 1 || ref.Revision > maxAssetRevisions {
			message = "Choose an available exact image revision."
		} else {
			meta, ok := g.assetRevision(*ref)
			info, err := os.Lstat(g.assetPath(*ref))
			if !ok || g.assets[ref.ID].Kind != "image" || err != nil || !info.Mode().IsRegular() || info.Size() != meta.Bytes {
				message = "The pinned image revision is unavailable. Choose an available image revision."
			}
		}
		if message != "" {
			out = append(out, mediaauthor.Issue{Field: prefix + entry.field, Message: message})
		}
	}
	return out
}
func (s *Server) generatorThemesHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	g := s.generator
	var input struct {
		Name  string               `json:"name"`
		Base  mediaauthor.ThemeRef `json:"base"`
		Style *mediaauthor.Style   `json:"style,omitempty"`
	}
	if r.Method == "POST" && !generatorDecode(w, r, &input) {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.Method == "GET" {
		out := make([]mediaauthor.Theme, 0, len(g.themes))
		for _, t := range g.themes {
			out = append(out, t)
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Name == out[j].Name {
				return out[i].Revision > out[j].Revision
			}
			return out[i].Name < out[j].Name
		})
		generatorJSON(w, 200, out)
		return
	}
	if !validThemeName(input.Name) {
		http.Error(w, "Name the theme using at most 180 UTF-8 bytes", 400)
		return
	}
	base, err := g.resolveTheme(input.Base)
	if err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	if input.Style != nil {
		base.Style = *input.Style
	}
	if issues := append(mediaauthor.ValidateStyle(base.Style), g.themeStyleAssetIssues(base.Style)...); len(issues) > 0 {
		generatorJSON(w, 422, map[string]any{"issues": issues})
		return
	}
	id, err := assetIdentity()
	if err != nil {
		http.Error(w, "Cannot create theme identity", 503)
		return
	}
	base.ID = id
	base.Name = input.Name
	base.Revision = 1
	base.Builtin = false
	s.saveThemeHTTP(w, g, base, 201)
}
func (s *Server) saveThemeHTTP(w http.ResponseWriter, g *generatorStore, t mediaauthor.Theme, status int) {
	if len(g.themes) >= maxThemeRevisions+len(mediaauthor.BuiltinThemes()) {
		http.Error(w, "Theme history limit reached (200 custom revisions)", 409)
		return
	}
	if writeState(filepath.Join(g.themesRoot, fmt.Sprintf("%s-%d.json", t.ID, t.Revision)), t) != nil {
		http.Error(w, "Theme revision not saved; check storage and retry", 507)
		return
	}
	g.themes[mediaauthor.ThemeRef{ID: t.ID, Revision: t.Revision}] = t
	generatorJSON(w, status, t)
}
func (s *Server) generatorThemeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	g := s.generator
	var t mediaauthor.Theme
	if r.Method == "PUT" && !generatorDecode(w, r, &t) {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.Method == "GET" {
		rev, err := strconv.Atoi(r.PathValue("revision"))
		if err != nil {
			http.Error(w, "Invalid theme revision", 400)
			return
		}
		t, err = g.resolveTheme(mediaauthor.ThemeRef{ID: r.PathValue("id"), Revision: rev})
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		generatorJSON(w, 200, t)
		return
	}
	id := r.PathValue("id")
	if mediaauthor.IsBuiltinTheme(id) {
		http.Error(w, "Built-in themes are immutable. Duplicate one to edit it.", 409)
		return
	}
	latest := 0
	for ref := range g.themes {
		if ref.ID == id {
			latest = max(latest, ref.Revision)
		}
	}
	if latest == 0 {
		http.Error(w, "Theme not found", 404)
		return
	}
	if latest != t.Revision {
		http.Error(w, "A newer theme revision exists. Reload it or duplicate your selected revision before editing; local settings are retained.", 409)
		return
	}
	if !validThemeName(t.Name) {
		http.Error(w, "Name the theme using at most 180 UTF-8 bytes", 400)
		return
	}
	issues := append(mediaauthor.ValidateStyle(t.Style), g.themeStyleAssetIssues(t.Style)...)
	if len(issues) > 0 {
		generatorJSON(w, 422, map[string]any{"issues": issues})
		return
	}
	t.ID = id
	t.Revision++
	t.Builtin = false
	s.saveThemeHTTP(w, g, t, 200)
}

func (s *Server) loadThemeInputs(theme mediaauthor.Theme, width, height int) (mediaauthor.RenderInputs, error) {
	inputs := mediaauthor.RenderInputs{}
	var err error
	if theme.Style.Artwork == "arcade-after-hours" {
		inputs.Background, err = png.Decode(bytes.NewReader(afterHoursPoster))
		if err != nil {
			return inputs, errors.New("Cannot decode Arcade After Hours artwork.")
		}
	}
	if theme.Style.Background != nil {
		inputs.Background, err = s.loadAssetImage(*theme.Style.Background)
		if err != nil {
			return inputs, errors.New("The pinned theme background cannot be decoded or verified.")
		}
	}
	inputs.Background = boundedThemeImage(inputs.Background, width, height)
	if theme.Style.Logo != nil {
		inputs.Logo, err = s.loadAssetImage(*theme.Style.Logo)
		if err != nil {
			return inputs, errors.New("The pinned theme logo cannot be decoded or verified.")
		}
	}
	inputs.Logo = boundedThemeImage(inputs.Logo, max(1, width/5), max(1, height/10))
	return inputs, nil
}
func boundedThemeImage(src image.Image, width, height int) image.Image {
	if src == nil {
		return nil
	}
	b := src.Bounds()
	if b.Dx() <= width && b.Dy() <= height {
		return src
	}
	ratio := min(float64(width)/float64(b.Dx()), float64(height)/float64(b.Dy()))
	dst := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(b.Dx())*ratio)), max(1, int(float64(b.Dy())*ratio))))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}
