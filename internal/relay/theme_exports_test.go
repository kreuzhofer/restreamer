package relay

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func TestThemeSceneDownloadsUseUnpublishedStyleWithoutSaving(t *testing.T) {
	s := libraryServer(t)
	before := dashboardRequest(s, "GET", "/api/generator/themes", "").Body.String()
	style := mediaauthor.RetroTheme(1).Style
	style.BackgroundColor = "#123456"
	for _, view := range []string{"prestream", "agenda", "brb", "ending"} {
		raw, _ := json.Marshal(map[string]any{"style": style, "view": view})
		w := dashboardRequest(s, "POST", "/api/generator/themes/preview", string(raw))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", view, w.Code, w.Body.String())
		}
		img, err := png.Decode(w.Body)
		if err != nil || img.Bounds() != image.Rect(0, 0, 1920, 1080) {
			t.Fatal("download is not a full-HD PNG", err)
		}
		r, g, b, a := img.At(0, 0).RGBA()
		if r != 0x1212 || g != 0x3434 || b != 0x5656 || a != 0xffff {
			t.Fatal("download ignored local colors", r, g, b, a)
		}
		if !strings.Contains(w.Header().Get("Content-Disposition"), view+".png") {
			t.Fatal("missing download filename", w.Header())
		}
	}
	if after := dashboardRequest(s, "GET", "/api/generator/themes", "").Body.String(); after != before {
		t.Fatal("preview published a theme revision")
	}
}

func TestThemeExportsRejectInvalidStyleAssetAndView(t *testing.T) {
	s := libraryServer(t)
	for _, change := range []func(map[string]any){
		func(input map[string]any) { input["view"] = "unknown" },
		func(input map[string]any) { input["style"].(map[string]any)["font_size"] = 10000 },
		func(input map[string]any) {
			input["style"].(map[string]any)["background"] = map[string]any{"id": "missing", "revision": 1}
		},
		func(input map[string]any) { input["view"] = "border" },
	} {
		raw, _ := json.Marshal(map[string]any{"style": mediaauthor.RetroTheme(1).Style, "view": "brb"})
		var input map[string]any
		json.Unmarshal(raw, &input)
		change(input)
		raw, _ = json.Marshal(input)
		if w := dashboardRequest(s, "POST", "/api/generator/themes/preview", string(raw)); w.Code != 400 && w.Code != 422 {
			t.Fatal("invalid export accepted", w.Code, w.Body.String())
		}
	}
}

func TestEveryThemeExportsTransparentBorderAndSceneSamples(t *testing.T) {
	s := libraryServer(t)
	for _, theme := range mediaauthor.BuiltinThemes() {
		t.Run(fmt.Sprintf("%s/%d", theme.ID, theme.Revision), func(t *testing.T) {
			for _, view := range []string{"border", "prestream", "agenda", "brb", "ending"} {
				raw, _ := json.Marshal(map[string]any{"style": theme.Style, "view": view})
				w := dashboardRequest(s, "POST", "/api/generator/themes/preview", string(raw))
				if w.Code != 200 {
					t.Fatalf("%s: %d %s", view, w.Code, w.Body.String())
				}
				img, err := png.Decode(w.Body)
				if err != nil {
					t.Fatal(err)
				}
				_, _, _, edge := img.At(0, 0).RGBA()
				_, _, _, center := img.At(960, 540).RGBA()
				if (view == "border" && center != 0) || (view != "border" && center != 65535) {
					t.Fatalf("%s lost its intended opacity: edge=%d center=%d", view, edge, center)
				}
			}
		})
	}
}

func TestThemeExportSharesPreviewLimit(t *testing.T) {
	s := libraryServer(t)
	s.generator.previewMu.Lock()
	defer s.generator.previewMu.Unlock()
	raw, _ := json.Marshal(map[string]any{"style": mediaauthor.RetroTheme(1).Style, "view": "border"})
	if w := dashboardRequest(s, "POST", "/api/generator/themes/preview", string(raw)); w.Code != 429 {
		t.Fatal("concurrent render was not bounded", w.Code)
	}
}

func TestRetiredThemesRemainResolvableButLeaveCatalog(t *testing.T) {
	s := libraryServer(t)
	w := dashboardRequest(s, "GET", "/api/generator/themes", "")
	var themes []mediaauthor.Theme
	if err := json.Unmarshal(w.Body.Bytes(), &themes); err != nil {
		t.Fatal(err)
	}
	if len(themes) != 6 {
		t.Fatalf("expected six current themes, got %d", len(themes))
	}
	for _, theme := range themes {
		if theme.ID == "retro" || ((theme.ID == "arcade-after-hours" || theme.ID == "neon-night") && theme.Revision == 1) {
			t.Fatal("retired theme advertised", theme.ID, theme.Revision)
		}
	}
	for _, ref := range []string{"retro/revisions/1", "retro/revisions/2", "arcade-after-hours/revisions/1", "neon-night/revisions/1"} {
		if w := dashboardRequest(s, "GET", "/api/generator/themes/"+ref, ""); w.Code != 200 {
			t.Fatal("pinned legacy revision lost", ref, w.Code)
		}
	}
}

func TestOBSFramesHaveLargeCleanAlphaCenter(t *testing.T) {
	s := libraryServer(t)
	for _, theme := range mediaauthor.BuiltinThemes() {
		t.Run(fmt.Sprint(theme.ID, theme.Revision), func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"style": theme.Style, "view": "border"})
			w := dashboardRequest(s, "POST", "/api/generator/themes/preview", string(raw))
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			img, err := png.Decode(w.Body)
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds() != image.Rect(0, 0, 1920, 1080) {
				t.Fatal("incorrect OBS size", img.Bounds())
			}
			transparent := 0
			for y := 0; y < 1080; y++ {
				for x := 0; x < 1920; x++ {
					_, _, _, a := img.At(x, y).RGBA()
					if a == 0 {
						transparent++
					}
					if x >= 240 && x < 1680 && y >= 220 && y < 860 && a != 0 {
						t.Fatal("artwork intrudes into content area", x, y)
					}
				}
			}
			ratio := float64(transparent) / (1920 * 1080)
			if ratio < 0.75 || ratio > 0.98 {
				t.Fatal("frame must be thin, visible and transparent", ratio)
			}
		})
	}
}
