package relay

import (
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func TestThemeImageReferenceErrorsIdentifyCallerFields(t *testing.T) {
	s := libraryServer(t)
	for _, field := range []string{"background", "logo"} {
		style := mediaauthor.RetroTheme(1).Style
		invalid := &mediaauthor.AssetRef{ID: "invalid", Revision: 0}
		if field == "background" {
			style.Background = invalid
		} else {
			style.Logo = invalid
		}
		raw, _ := json.Marshal(map[string]any{"name": "Invalid reference", "base": mediaauthor.ThemeRef{ID: "retro", Revision: 1}, "style": style})
		w := dashboardRequest(s, "POST", "/api/generator/themes", string(raw))
		if w.Code != 422 || !strings.Contains(w.Body.String(), `"field":"style.`+field+`"`) || strings.Contains(w.Body.String(), `"field":"theme.`) {
			t.Fatal("theme editor field prefix", w.Code, w.Body.String())
		}
	}
	image := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "logo.png", assetPNG(t, color.White)))
	style := mediaauthor.RetroTheme(1).Style
	style.Logo = &mediaauthor.AssetRef{ID: image.ID, Revision: 1}
	raw, _ := json.Marshal(map[string]any{"name": "Pinned logo", "base": mediaauthor.ThemeRef{ID: "retro", Revision: 1}, "style": style})
	w := dashboardRequest(s, "POST", "/api/generator/themes", string(raw))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var theme mediaauthor.Theme
	json.Unmarshal(w.Body.Bytes(), &theme)
	d := generatorDraft(t, s, 1)
	d.Theme = mediaauthor.ThemeRef{ID: theme.ID, Revision: 1}
	d = saveMusicDesign(t, s, d)
	// External storage loss must point an authored design at its pinned theme,
	// while editing theme settings points at the style field.
	path := filepath.Join(s.cfg.BRB.Directory, "library", "generator", "assets", image.ID, "1.png")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(d)
	w = dashboardRequest(s, "POST", "/api/generator/validate", string(raw))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"field":"theme.logo"`) {
		t.Fatal("draft field prefix", w.Code, w.Body.String())
	}
	raw, _ = json.Marshal(theme)
	w = dashboardRequest(s, "PUT", "/api/generator/themes/"+theme.ID, string(raw))
	if w.Code != 422 || !strings.Contains(w.Body.String(), `"field":"style.logo"`) {
		t.Fatal("theme revision field prefix", w.Code, w.Body.String())
	}
}
