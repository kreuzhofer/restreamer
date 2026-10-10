package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func TestLatestBuiltinThemesFitUneditedStarters(t *testing.T) {
	for _, size := range [][2]int{{320, 180}, {640, 360}, {1920, 1080}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			s := dashboardServer(t)
			s.cfg.BRB = &config.BRBConfig{Enabled: "true", Directory: t.TempDir(), BRBProfile: config.BRBProfile{Width: size[0], Height: size[1], FPS: 25, SampleRate: 48000}}
			if err := s.initialize(); err != nil {
				t.Fatal(err)
			}
			var themes []mediaauthor.Theme
			response := dashboardRequest(s, "GET", "/api/generator/themes", "")
			if err := json.Unmarshal(response.Body.Bytes(), &themes); err != nil {
				t.Fatal(err)
			}
			latest := map[string]mediaauthor.Theme{}
			for _, theme := range themes {
				if theme.Revision > latest[theme.ID].Revision {
					latest[theme.ID] = theme
				}
			}
			for id, theme := range latest {
				for _, stage := range []string{"prestream", "ending"} {
					t.Run(id+"/"+stage, func(t *testing.T) {
						copied := dashboardRequest(s, "POST", "/api/generator/templates/starter-"+stage+"/designs", fmt.Sprintf(`{"name":"Default starter","version":1,"theme":{"id":%q,"revision":%d}}`, id, theme.Revision))
						if copied.Code != 201 {
							t.Fatal(copied.Code, copied.Body.String())
						}
						validated := dashboardRequest(s, "POST", "/api/generator/validate", copied.Body.String())
						var result struct {
							Issues []mediaauthor.Issue `json:"issues"`
						}
						if err := json.Unmarshal(validated.Body.Bytes(), &result); err != nil {
							t.Fatal(err)
						}
						if validated.Code != 200 || len(result.Issues) != 0 {
							t.Fatalf("unedited starter is invalid: %d %s", validated.Code, validated.Body.String())
						}
					})
				}
			}
		})
	}
}

func TestBlankThemeTextDoesNotOverflowAndErrorsBelongToTheirScene(t *testing.T) {
	s := libraryServer(t)
	for _, themeID := range []string{"arcade-after-hours", "neon-night"} {
		t.Run(themeID, func(t *testing.T) {
			d := generatorDraft(t, s, 1)
			d.Theme = mediaauthor.ThemeRef{ID: themeID, Revision: 2}
			d.Scenes[0].Text = "  \n\n\n\n  "
			d.Scenes = append(d.Scenes, mediaauthor.Scene{ID: "agenda", Layout: "list", Text: "", Items: []string{""}, DurationSeconds: 1})
			raw, _ := json.Marshal(d)
			w := dashboardRequest(s, "POST", "/api/generator/validate", string(raw))
			var result struct {
				Issues []mediaauthor.Issue `json:"issues"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Issues) != 1 || result.Issues[0].Field != "scenes.1.items.0" || result.Issues[0].Message != "Enter text for this list item or remove it." {
				t.Fatalf("empty fields misreported: %s", w.Body.String())
			}
			if preview := dashboardRequest(s, "POST", "/api/generator/preview?scene=0", string(raw)); preview.Code != 200 {
				t.Fatal("another scene blocked the blank preview", preview.Code, preview.Body.String())
			}
		})
	}
}

func TestIllustratedThemeListOverridesAndGeneratedStarters(t *testing.T) {
	s := arcadeThemeServer(t)
	generatorServe(t, s)
	for _, themeID := range []string{"arcade-after-hours", "neon-night"} {
		for _, stage := range []string{"prestream", "ending"} {
			t.Run(themeID+"/"+stage, func(t *testing.T) {
				response := dashboardRequest(s, "POST", "/api/generator/templates/starter-"+stage+"/designs", fmt.Sprintf(`{"name":"Starter output","version":1,"theme":{"id":%q,"revision":1}}`, themeID))
				if response.Code != 201 {
					t.Fatal(response.Code, response.Body.String())
				}
				var d mediaauthor.Design
				if err := json.Unmarshal(response.Body.Bytes(), &d); err != nil {
					t.Fatal(err)
				}
				// Adoption changes styling explicitly; saved content stays intact.
				originalText := d.Scenes[1].Text
				d.Theme.Revision = 2
				d.EndingFadeSeconds = new(float64)
				for i := range d.Scenes {
					d.Scenes[i].DurationSeconds = 1
				}
				raw, _ := json.Marshal(d)
				saved := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(raw))
				if saved.Code != 200 {
					t.Fatal(saved.Code, saved.Body.String())
				}
				if err := json.Unmarshal(saved.Body.Bytes(), &d); err != nil {
					t.Fatal(err)
				}
				if d.Scenes[1].Text != originalText || d.Scenes[1].FontSize != 0 {
					t.Fatal("adoption altered scene content or authored size")
				}
				reloaded := dashboardRequest(s, "GET", "/api/generator/designs/"+d.ID, "")
				if reloaded.Body.String() != saved.Body.String() {
					t.Fatal("saved adopted draft changed")
				}
				quick := dashboardRequest(s, "POST", "/api/generator/preview?scene=1", saved.Body.String())
				expected, err := png.Decode(quick.Body)
				if err != nil {
					t.Fatal(quick.Code, err)
				}
				// Explicit sizing remains authoritative; clearing it restores the list default.
				d.Scenes[1].FontSize = 120
				raw, _ = json.Marshal(d)
				invalid := dashboardRequest(s, "POST", "/api/generator/preview?scene=1", string(raw))
				if invalid.Code != 422 || !strings.Contains(invalid.Body.String(), "overflows") {
					t.Fatal("explicit oversized text silently shrank", invalid.Code)
				}
				d.Scenes[1].FontSize = 0
				job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
				if job.State != "ready" {
					t.Fatal(job)
				}
				preview := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
				path := filepath.Join(t.TempDir(), "starter.mp4")
				if err := os.WriteFile(path, preview.Body.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				frame, err := exec.Command("ffmpeg", "-v", "error", "-threads", "2", "-i", path, "-vf", `select=eq(n\,25)`, "-frames:v", "1", "-threads", "2", "-f", "image2pipe", "-c:v", "png", "-").Output()
				if err != nil {
					t.Fatal(err)
				}
				actual, err := png.Decode(bytes.NewReader(frame))
				if err != nil {
					t.Fatal(err)
				}
				if delta := scenePixelDifference(actual, expected); delta > 8 {
					t.Fatal("generated list differs from quick preview", delta)
				}
			})
		}
	}
}

func TestListFontSizeValidationAndLegacyThemeRevisions(t *testing.T) {
	s := libraryServer(t)
	for _, themeID := range []string{"arcade-after-hours", "neon-night"} {
		old := dashboardRequest(s, "GET", "/api/generator/themes/"+themeID+"/revisions/1", "")
		var original mediaauthor.Theme
		if err := json.Unmarshal(old.Body.Bytes(), &original); err != nil {
			t.Fatal(err)
		}
		if original.Style.ListFontSize != 0 || original.Style.FontSize != 120 {
			t.Fatal("legacy theme revision changed")
		}
		current := dashboardRequest(s, "GET", "/api/generator/themes/"+themeID+"/revisions/2", "")
		var theme mediaauthor.Theme
		if err := json.Unmarshal(current.Body.Bytes(), &theme); err != nil {
			t.Fatal(err)
		}
		theme.Style.ListFontSize = 121
		raw, _ := json.Marshal(map[string]any{"name": "Invalid list size", "base": mediaauthor.ThemeRef{ID: themeID, Revision: 2}, "style": theme.Style})
		invalid := dashboardRequest(s, "POST", "/api/generator/themes", string(raw))
		if invalid.Code != 422 || !strings.Contains(invalid.Body.String(), "style.list_font_size") {
			t.Fatal("invalid list size accepted", invalid.Code)
		}
		theme.Style.ListFontSize = 56
		raw, _ = json.Marshal(map[string]any{"name": "Custom list size", "base": mediaauthor.ThemeRef{ID: themeID, Revision: 2}, "style": theme.Style})
		created := dashboardRequest(s, "POST", "/api/generator/themes", string(raw))
		if created.Code != 201 {
			t.Fatal(created.Code, created.Body.String())
		}
		if err := json.Unmarshal(created.Body.Bytes(), &theme); err != nil {
			t.Fatal(err)
		}
		restarted := New(s.cfg, s.log)
		recovered := dashboardRequest(restarted, "GET", "/api/generator/themes/"+theme.ID+"/revisions/1", "")
		if recovered.Code != 200 || recovered.Body.String() != created.Body.String() {
			t.Fatal("list typography did not survive restart")
		}
	}
}
