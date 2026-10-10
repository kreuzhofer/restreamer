package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func arcadeThemeServer(t *testing.T) *Server {
	t.Helper()
	s := dashboardServer(t)
	s.cfg.BRB = &config.BRBConfig{Enabled: "true", Directory: t.TempDir(), BRBProfile: config.BRBProfile{Width: 640, Height: 360, FPS: 25, SampleRate: 48000}}
	if err := s.initialize(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAnimatedThemesAreSelectableImmutableAndTextIsEditable(t *testing.T) {
	for _, themeID := range []string{"arcade-after-hours", "neon-night"} {
		t.Run(themeID, func(t *testing.T) {
			s := arcadeThemeServer(t)
			w := dashboardRequest(s, "GET", "/api/generator/themes/"+themeID+"/revisions/1", "")
			if w.Code != 200 {
				t.Fatalf("arcade theme unavailable: %d %s", w.Code, w.Body.String())
			}
			var theme mediaauthor.Theme
			if err := json.Unmarshal(w.Body.Bytes(), &theme); err != nil {
				t.Fatal(err)
			}
			if !theme.Builtin || theme.ID != themeID {
				t.Fatal(theme)
			}
			if w := dashboardRequest(s, "PUT", "/api/generator/themes/"+themeID, w.Body.String()); w.Code != 409 {
				t.Fatal("builtin mutable", w.Code)
			}
			d := generatorDraft(t, s, 16)
			d.Theme = mediaauthor.ThemeRef{ID: themeID, Revision: 1}
			d.Scenes[0].Text = "STARTING SOON"
			d.Scenes[0].Font = ""
			d.Scenes[0].FontSize = 0
			raw, _ := json.Marshal(d)
			a := dashboardRequest(s, "POST", "/api/generator/preview", string(raw))
			if a.Code != 200 {
				t.Fatal(a.Code, a.Body.String())
			}
			d.Scenes[0].Text = "THANKS FOR PLAYING"
			raw, _ = json.Marshal(d)
			b := dashboardRequest(s, "POST", "/api/generator/preview", string(raw))
			if b.Code != 200 || bytes.Equal(a.Body.Bytes(), b.Body.Bytes()) {
				t.Fatal("theme text not editable", b.Code, b.Body.String())
			}
			// Existing revisions stay available, independent from the new theme.
			if w := dashboardRequest(s, "GET", "/api/generator/themes/retro/revisions/1", ""); w.Code != 200 {
				t.Fatal(w.Code)
			}

		})
	}
}

func TestAnimatedThemesMatchGeneratedAndBRBPreviews(t *testing.T) {
	for _, themeID := range []string{"arcade-after-hours", "neon-night"} {
		t.Run(themeID, func(t *testing.T) {
			s := arcadeThemeServer(t)
			d := generatorDraft(t, s, 16)
			d.Theme = mediaauthor.ThemeRef{ID: themeID, Revision: 1}
			d.Scenes[0].Text = "BE RIGHT BACK"
			raw, _ := json.Marshal(d)
			saved := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(raw))
			if saved.Code != 200 {
				t.Fatal(saved.Code, saved.Body.String())
			}
			json.Unmarshal(saved.Body.Bytes(), &d)
			expected := make([]image.Image, 2)
			for i, frame := range []int{0, 100} {
				w := dashboardRequest(s, "POST", fmt.Sprintf("/api/generator/preview?frame=%d", frame), saved.Body.String())
				var err error
				expected[i], err = png.Decode(w.Body)
				if err != nil {
					t.Fatal(w.Code, err)
				}
			}
			if delta := scenePixelDifference(expected[0], expected[1]); delta < 1 {
				t.Fatal("background is not animated", delta)
			}
			loop := dashboardRequest(s, "POST", "/api/generator/preview?frame=400", saved.Body.String())
			loopImage, err := png.Decode(loop.Body)
			if err != nil {
				t.Fatal(err)
			}
			if scenePixelDifference(expected[0], loopImage) != 0 {
				t.Fatal("animation endpoint does not repeat exactly")
			}
			w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version))
			if w.Code != 202 {
				t.Fatal(w.Code, w.Body.String())
			}
			job := generatorJob(t, w)
			generatorServe(t, s)
			job = waitGeneratorJob(t, s, job.ID)
			if job.State != "ready" {
				t.Fatal(job)
			}
			media := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
			assertArcadeFrames(t, media.Body.Bytes(), expected)
			status := dashboardRequest(s, "GET", "/api/dashboard", "")
			var current struct {
				Assets brbSettings `json:"brb_assets"`
			}
			json.Unmarshal(status.Body.Bytes(), &current)
			// Match the generator's text through the public BRB content controls.
			if w := assetRequest(t, s, map[string]string{"text": "BE RIGHT BACK"}, "", nil); w.Code != 204 {
				t.Fatal(w.Code, w.Body.String())
			}
			status = dashboardRequest(s, "GET", "/api/dashboard", "")
			json.Unmarshal(status.Body.Bytes(), &current)
			prepared := dashboardRequest(s, "POST", "/api/brb/theme/prepare", fmt.Sprintf(`{"theme":{"id":%q,"revision":1},"base_generation":%q}`, themeID, current.Assets.Generation))
			if prepared.Code != 201 {
				t.Fatal(prepared.Code, prepared.Body.String())
			}
			var candidate brbThemeCandidate
			json.Unmarshal(prepared.Body.Bytes(), &candidate)
			if candidate.VideoDuration != 16 {
				t.Fatal("BRB cut the palette cycle short", candidate.VideoDuration)
			}
			preview := dashboardRequest(s, "GET", "/api/brb/theme/candidate/preview", "")
			assertArcadeFrames(t, preview.Body.Bytes(), expected)
			if w := dashboardRequest(s, "POST", "/api/brb/theme/activate", fmt.Sprintf(`{"id":%q,"base_generation":%q}`, candidate.ID, candidate.Base)); w.Code != 204 {
				t.Fatal(w.Code, w.Body.String())
			}
			restarted := New(s.cfg, s.log)
			if err := restarted.initialize(); err != nil {
				t.Fatal("arcade theme did not survive restart", err)
			}

		})
	}
}

func assertArcadeFrames(t *testing.T, data []byte, expected []image.Image) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "arcade.mp4")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for i, frame := range []int{0, 100} {
		encoded, err := exec.Command("ffmpeg", "-v", "error", "-threads", "2", "-i", path, "-vf", fmt.Sprintf("select=eq(n\\,%d)", frame), "-frames:v", "1", "-threads", "2", "-f", "image2pipe", "-c:v", "png", "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		actual, err := png.Decode(bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		if delta := scenePixelDifference(actual, expected[i]); delta > 8 {
			t.Fatalf("frame %d differs from preview: %.2f", frame, delta)
		}
		if same, other := scenePixelDifference(actual, expected[i]), scenePixelDifference(actual, expected[1-i]); same >= other {
			t.Fatalf("frame %d uses wrong animation time: match %.2f other %.2f", frame, same, other)
		}
	}
}

func TestAnimatedThemeBackgroundContinuesBehindRepeatingInsetVideo(t *testing.T) {
	for _, themeID := range []string{"arcade-after-hours", "neon-night"} {
		t.Run(themeID, func(t *testing.T) {
			s := arcadeThemeServer(t)
			a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "source.mp4", generatorVideoFixture(t, false)))
			body := fmt.Sprintf(`{"name":"Arcade video","stage":"ending","ending_fade_seconds":0,"theme":{"id":%q,"revision":1},"scenes":[{"id":"video","layout":"media","media_kind":"video","text":"","content_region":{"width_percent":40,"height_percent":40},"duration_seconds":8,"video":{"asset":{"id":%q,"revision":1},"trim_end_seconds":1,"repeat":true}}]}`, themeID, a.ID)
			created := dashboardRequest(s, "POST", "/api/generator/designs", body)
			if created.Code != 201 {
				t.Fatal(created.Code, created.Body.String())
			}
			var d mediaauthor.Design
			json.Unmarshal(created.Body.Bytes(), &d)
			expected := make([]image.Image, 2)
			for i, frame := range []int{0, 100} {
				w := dashboardRequest(s, "POST", fmt.Sprintf("/api/generator/preview?frame=%d", frame), created.Body.String())
				var err error
				expected[i], err = png.Decode(w.Body)
				if err != nil {
					t.Fatal(w.Code, err)
				}
			}
			generatorServe(t, s)
			job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
			if job.State != "ready" {
				t.Fatal(job)
			}
			preview := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
			assertArcadeFrames(t, preview.Body.Bytes(), expected)

		})
	}
}

func TestSmallArcadeTextKeepsSeparateLineShadows(t *testing.T) {
	s := arcadeThemeServer(t)
	style := mediaauthor.RetroTheme(1).Style
	style.Font = "arcade-pixel"
	style.FontSize = 24
	style.BorderStyle = "none"
	style.BackgroundColor = "#000000"
	payload, _ := json.Marshal(map[string]any{"name": "Small pixels", "base": mediaauthor.ThemeRef{ID: "retro", Revision: 1}, "style": style})
	w := dashboardRequest(s, "POST", "/api/generator/themes", string(payload))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var theme mediaauthor.Theme
	json.Unmarshal(w.Body.Bytes(), &theme)
	d := generatorDraft(t, s, 1)
	d.Theme = mediaauthor.ThemeRef{ID: theme.ID, Revision: 1}
	d.Scenes[0].Text = "HI\nHI"
	raw, _ := json.Marshal(d)
	w = dashboardRequest(s, "POST", "/api/generator/preview", string(raw))
	img, err := png.Decode(w.Body)
	if err != nil {
		t.Fatal(w.Code, err)
	}
	runs := 0
	previous := false
	for y := 0; y < img.Bounds().Dy(); y++ {
		ink := false
		for x := 0; x < img.Bounds().Dx(); x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			ink = ink || r+g+b > 0
		}
		if ink && !previous {
			runs++
		}
		previous = ink
	}
	if runs != 2 {
		t.Fatalf("two text lines and their shadows overlap: got %d ink bands", runs)
	}
}

func TestArcadeCustomBorderStaysBehindFullFrameVideo(t *testing.T) {
	s := arcadeThemeServer(t)
	w := dashboardRequest(s, "GET", "/api/generator/themes/arcade-after-hours/revisions/1", "")
	var theme mediaauthor.Theme
	json.Unmarshal(w.Body.Bytes(), &theme)
	theme.Style.BorderStyle = "line"
	theme.Style.BorderWidth = 12
	theme.Style.AccentColor = "#ff0000"
	raw, _ := json.Marshal(map[string]any{"name": "Bordered arcade", "base": mediaauthor.ThemeRef{ID: theme.ID, Revision: 1}, "style": theme.Style})
	w = dashboardRequest(s, "POST", "/api/generator/themes", string(raw))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &theme)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "source.mp4", generatorVideoFixture(t, false)))
	body := fmt.Sprintf(`{"name":"Full frame","stage":"ending","ending_fade_seconds":0,"theme":{"id":%q,"revision":1},"scenes":[{"id":"video","layout":"media","media_kind":"video","text":"","duration_seconds":1,"video":{"asset":{"id":%q,"revision":1}}}]}`, theme.ID, a.ID)
	w = dashboardRequest(s, "POST", "/api/generator/designs", body)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var d mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &d)
	preview := dashboardRequest(s, "POST", "/api/generator/preview", w.Body.String())
	expected, err := png.Decode(preview.Body)
	if err != nil {
		t.Fatal(err)
	}
	generatorServe(t, s)
	job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if job.State != "ready" {
		t.Fatal(job)
	}
	media := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
	path := filepath.Join(t.TempDir(), "full.mp4")
	os.WriteFile(path, media.Body.Bytes(), 0600)
	data, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-frames:v", "1", "-threads", "2", "-f", "image2pipe", "-c:v", "png", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	band := func(src image.Image) image.Image {
		out := image.NewRGBA(image.Rect(0, 0, 600, 2))
		draw.Draw(out, out.Bounds(), src, image.Pt(20, 16), draw.Src)
		return out
	}
	if delta := scenePixelDifference(band(actual), band(expected)); delta > 10 {
		t.Fatalf("border overlays full-frame video only in prepared output: %.2f", delta)
	}
}

func TestArcadeArtworkKeepsProportionsOnFourByThreeProfile(t *testing.T) {
	s := arcadeThemeServer(t)
	if w := assetRequest(t, s, map[string]string{"width": "640", "height": "480"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	d := generatorDraft(t, s, 1)
	d.Theme = mediaauthor.ThemeRef{ID: "arcade-after-hours", Revision: 1}
	d.Scenes[0].Text = ""
	raw, _ := json.Marshal(d)
	w := dashboardRequest(s, "POST", "/api/generator/preview", string(raw))
	img, err := png.Decode(w.Body)
	if err != nil {
		t.Fatal(w.Code, err)
	}
	for y := 0; y < 50; y++ {
		for x := 0; x < 640; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r/257 != 2 || g/257 != 16 || b/257 != 47 {
				t.Fatalf("artwork stretched into the letterbox at %d,%d", x, y)
			}
		}
	}
}
