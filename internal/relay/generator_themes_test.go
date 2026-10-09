package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratorThemeRevisionsAreImmutableAndAdoptionIsExplicit(t *testing.T) {
	s := libraryServer(t)
	created := dashboardRequest(s, "POST", "/api/generator/themes", `{"name":"Community colors","base":{"id":"retro","revision":1}}`)
	if created.Code != 201 {
		t.Fatalf("duplicate theme: %d %s", created.Code, created.Body.String())
	}
	var theme map[string]any
	json.Unmarshal(created.Body.Bytes(), &theme)
	themeID := theme["id"].(string)
	d := generatorDraft(t, s, 1)
	raw, _ := json.Marshal(d)
	var draft map[string]any
	json.Unmarshal(raw, &draft)
	draft["theme"] = map[string]any{"id": themeID, "revision": 1}
	raw, _ = json.Marshal(draft)
	saved := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(raw))
	if saved.Code != 200 {
		t.Fatal(saved.Code, saved.Body.String())
	}
	before := dashboardRequest(s, "POST", "/api/generator/preview", saved.Body.String())
	style := theme["style"].(map[string]any)
	style["background_color"] = "#203040"
	style["font"] = "go-mono"
	raw, _ = json.Marshal(theme)
	revised := dashboardRequest(s, "PUT", "/api/generator/themes/"+themeID, string(raw))
	if revised.Code != 200 {
		t.Fatal(revised.Code, revised.Body.String())
	}
	original := dashboardRequest(s, "GET", "/api/generator/themes/"+themeID+"/revisions/1", "")
	if original.Body.String() != created.Body.String() {
		t.Fatal("theme revision1 changed")
	}
	if w := dashboardRequest(s, "PUT", "/api/generator/themes/"+themeID, string(raw)); w.Code != 409 {
		t.Fatal("stale theme update accepted", w.Code)
	}
	after := dashboardRequest(s, "POST", "/api/generator/preview", saved.Body.String())
	if before.Code != 200 || after.Code != 200 || !bytes.Equal(before.Body.Bytes(), after.Body.Bytes()) {
		t.Fatal("shared theme update changed pinned preview", before.Code, after.Code)
	}
	json.Unmarshal(saved.Body.Bytes(), &draft)
	draft["theme"].(map[string]any)["revision"] = 2
	raw, _ = json.Marshal(draft)
	adopted := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(raw))
	if adopted.Code != 200 {
		t.Fatal(adopted.Code, adopted.Body.String())
	}
	changed := dashboardRequest(s, "POST", "/api/generator/preview", adopted.Body.String())
	if changed.Code != 200 || bytes.Equal(before.Body.Bytes(), changed.Body.Bytes()) {
		t.Fatal("explicit adoption did not apply colors/font")
	}
	restarted := New(s.cfg, s.log)
	if w := dashboardRequest(restarted, "GET", fmt.Sprintf("/api/generator/themes/%s/revisions/1", themeID), ""); w.Code != 200 || w.Body.String() != created.Body.String() {
		t.Fatal("pinned theme missing after restart", w.Code)
	}
}

func TestGeneratorCapturesThemeAndAnimatedFramesMatchQuickPreview(t *testing.T) {
	s := libraryServer(t)
	w := dashboardRequest(s, "POST", "/api/generator/themes", `{"name":"Animated variant","base":{"id":"retro","revision":2}}`)
	var theme map[string]any
	json.Unmarshal(w.Body.Bytes(), &theme)
	id := theme["id"].(string)
	draftResponse := dashboardRequest(s, "POST", "/api/generator/designs", fmt.Sprintf(`{"name":"Theme capture","stage":"prestream","theme":{"id":%q,"revision":1}}`, id))
	if draftResponse.Code != 201 {
		t.Fatal(draftResponse.Code, draftResponse.Body.String())
	}
	var d mediaauthor.Design
	json.Unmarshal(draftResponse.Body.Bytes(), &d)
	if d.Theme.ID != id {
		t.Fatal("blank creation discarded explicit theme", d.Theme)
	}
	d.Scenes[0].DurationSeconds = 2
	d.Scenes[0].Text = "Mixed Case café"
	raw, _ := json.Marshal(d)
	saved := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(raw))
	json.Unmarshal(saved.Body.Bytes(), &d)
	w = dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version))
	if w.Code != 202 {
		t.Fatalf("themed generation: %d %s", w.Code, w.Body.String())
	}
	job := generatorJob(t, w)
	var capture map[string]any
	json.Unmarshal(w.Body.Bytes(), &capture)
	snapshot, ok := capture["theme_snapshot"].(map[string]any)
	if !ok || snapshot["id"] != id || snapshot["revision"] != float64(1) {
		t.Fatal("resolved theme inputs not captured", capture)
	}
	expected := make([]image.Image, 2)
	for i, frame := range []int{0, 15} {
		p := dashboardRequest(s, "POST", fmt.Sprintf("/api/generator/preview?frame=%d", frame), saved.Body.String())
		var err error
		expected[i], err = png.Decode(p.Body)
		if err != nil {
			t.Fatal(p.Code, err)
		}
	}
	theme["style"].(map[string]any)["text_color"] = "#ff3366"
	raw, _ = json.Marshal(theme)
	if updated := dashboardRequest(s, "PUT", "/api/generator/themes/"+id, string(raw)); updated.Code != 200 {
		t.Fatal(updated.Code, updated.Body.String())
	}
	generatorServe(t, s)
	job = waitGeneratorJob(t, s, job.ID)
	if job.State != "ready" {
		t.Fatal(job)
	}
	preview := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
	path := filepath.Join(t.TempDir(), "theme.mp4")
	os.WriteFile(path, preview.Body.Bytes(), 0600)
	for i, frame := range []int{0, 15} {
		data, err := exec.Command("ffmpeg", "-v", "error", "-threads", "2", "-i", path, "-vf", fmt.Sprintf("select=eq(n\\,%d)", frame), "-frames:v", "1", "-threads", "2", "-f", "image2pipe", "-c:v", "png", "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		actual, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if delta := scenePixelDifference(actual, expected[i]); delta > 5 {
			t.Fatalf("captured theme typography changed: %.2f", delta)
		}
		band := func(src image.Image) image.Image {
			b := src.Bounds()
			out := image.NewRGBA(image.Rect(0, 0, b.Dx(), 12))
			draw.Draw(out, out.Bounds(), src, image.Pt(0, b.Dy()-12), draw.Src)
			return out
		}
		match, other := scenePixelDifference(band(actual), band(expected[i])), scenePixelDifference(band(actual), band(expected[1-i]))
		if match+0.2 >= other {
			t.Fatalf("frame%d effect differs from quick preview: match%.2f other%.2f", frame, match, other)
		}
	}
}

func TestGeneratorThemeAssetsOverridesAndTemplateCopies(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "brand.png", assetPNG(t, color.NRGBA{R: 220, G: 20, A: 255})))
	theme := mediaauthor.RetroTheme(2)
	theme.Style.Background = &mediaauthor.AssetRef{ID: a.ID, Revision: 1}
	theme.Style.Logo = theme.Style.Background
	raw, _ := json.Marshal(map[string]any{"name": "Brand", "base": mediaauthor.ThemeRef{ID: "retro", Revision: 2}, "style": theme.Style})
	w := dashboardRequest(s, "POST", "/api/generator/themes", string(raw))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &theme)
	if theme.Style.Background == nil {
		t.Fatal("save local settings as a new theme discarded background")
	}
	d := generatorDraft(t, s, 1)
	d.Theme = mediaauthor.ThemeRef{ID: theme.ID, Revision: 1}
	d.Stage = "ending"
	music := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "music.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", 1)))
	volume := 40.0
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: music.ID, Revision: 1}, Mode: "repeat", VolumePercent: &volume, FadeInSeconds: 0.1, FadeOutSeconds: 0.2}
	d.Scenes[0].Font = "go-mono"
	d.Scenes[0].FontSize = 30
	d.Scenes[0].Text = "Mixed Case\ncafé"
	d.Scenes[0].DurationSeconds = 7
	raw, _ = json.Marshal(d)
	w = dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(raw))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &d)
	before := dashboardRequest(s, "POST", "/api/generator/preview", w.Body.String())
	if before.Code != 200 {
		t.Fatal(before.Code, before.Body.String())
	}
	uploadedAsset(t, assetUpload(t, s, "/api/generator/assets/"+a.ID+"/revisions?version=1", "brand.png", assetPNG(t, color.NRGBA{B: 255, A: 255})))
	after := dashboardRequest(s, "POST", "/api/generator/preview", w.Body.String())
	if !bytes.Equal(before.Body.Bytes(), after.Body.Bytes()) {
		t.Fatal("image replacement changed pinned theme")
	}
	if response := dashboardRequest(s, "DELETE", "/api/generator/assets/"+a.ID, `{}`); response.Code != 409 || !bytes.Contains(response.Body.Bytes(), []byte(`"kind":"theme"`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"kind":"design"`)) {
		t.Fatal("theme asset retention", response.Code, response.Body.String())
	}
	other := d
	other.Stage = "prestream"
	other.Name = "Same brand prestream"
	raw, _ = json.Marshal(other)
	sibling := dashboardRequest(s, "POST", "/api/generator/designs", string(raw))
	if sibling.Code != 201 {
		t.Fatal(sibling.Code, sibling.Body.String())
	}
	json.Unmarshal(sibling.Body.Bytes(), &other)
	oldMusic, _ := json.Marshal(d.Soundtrack)
	oldScenes, _ := json.Marshal(d.Scenes)
	theme.Style.FontSize = 120
	theme.Style.LineSpacingPercent = 180
	raw, _ = json.Marshal(theme)
	w = dashboardRequest(s, "PUT", "/api/generator/themes/"+theme.ID, string(raw))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	d.Theme.Revision = 2
	raw, _ = json.Marshal(d)
	w = dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(raw))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &d)
	newScenes, _ := json.Marshal(d.Scenes)
	newMusic, _ := json.Marshal(d.Soundtrack)
	if !bytes.Equal(oldMusic, newMusic) {
		t.Fatal("theme adoption changed continuous soundtrack")
	}
	sibling = dashboardRequest(s, "GET", "/api/generator/designs/"+other.ID, "")
	var unchanged mediaauthor.Design
	json.Unmarshal(sibling.Body.Bytes(), &unchanged)
	if unchanged.Theme.Revision != 1 || unchanged.Stage != "prestream" {
		t.Fatal("shared theme edit changed another stage's draft")
	}

	if !bytes.Equal(oldScenes, newScenes) {
		t.Fatal("adoption changed content, timing or overrides")
	}
	if preview := dashboardRequest(s, "POST", "/api/generator/preview", w.Body.String()); preview.Code != 200 {
		t.Fatal("scene override lost", preview.Code, preview.Body.String())
	}
	templates := dashboardRequest(s, "GET", "/api/generator/templates", "")
	var catalog []ContentTemplate
	json.Unmarshal(templates.Body.Bytes(), &catalog)
	raw, _ = json.Marshal(map[string]any{"name": "Themed template copy", "version": catalog[0].Version, "theme": d.Theme})
	copy := dashboardRequest(s, "POST", "/api/generator/templates/"+catalog[0].ID+"/designs", string(raw))
	if copy.Code != 201 {
		t.Fatal(copy.Code, copy.Body.String())
	}
	var copied mediaauthor.Design
	json.Unmarshal(copy.Body.Bytes(), &copied)
	if copied.Theme != d.Theme {
		t.Fatal("template copy discarded theme")
	}
	theme.Style.Logo = &mediaauthor.AssetRef{ID: "missing", Revision: 1}
	theme.Revision = 2
	raw, _ = json.Marshal(theme)
	if w := dashboardRequest(s, "PUT", "/api/generator/themes/"+theme.ID, string(raw)); w.Code != 422 {
		t.Fatal("missing logo accepted", w.Code)
	}
}

func TestGeneratorThemeAdoptionRevalidatesTypography(t *testing.T) {
	s := libraryServer(t)
	w := dashboardRequest(s, "POST", "/api/generator/themes", `{"name":"Readable","base":{"id":"retro","revision":1}}`)
	var theme mediaauthor.Theme
	json.Unmarshal(w.Body.Bytes(), &theme)
	d := generatorDraft(t, s, 1)
	d.Theme = mediaauthor.ThemeRef{ID: theme.ID, Revision: 1}
	d.Scenes[0].Text = strings.Repeat("Mixed Case line\n", 7) + "Last line"
	d.Scenes[0].FontSize = 0
	raw, _ := json.Marshal(d)
	w = dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(raw))
	json.Unmarshal(w.Body.Bytes(), &d)
	if w := dashboardRequest(s, "POST", "/api/generator/preview", string(raw)); w.Code != 200 {
		t.Fatal("initial text does not fit", w.Code, w.Body.String())
	}
	theme.Style.FontSize = 120
	theme.Style.LineSpacingPercent = 180
	raw, _ = json.Marshal(theme)
	w = dashboardRequest(s, "PUT", "/api/generator/themes/"+theme.ID, string(raw))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	d.Theme.Revision = 2
	raw, _ = json.Marshal(d)
	w = dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(raw))
	if w.Code != 200 {
		t.Fatal("invalid layout draft must retain edits", w.Code)
	}
	json.Unmarshal(w.Body.Bytes(), &d)
	if w := dashboardRequest(s, "POST", "/api/generator/preview", string(raw)); w.Code != 422 || !strings.Contains(w.Body.String(), "scenes.0.text") {
		t.Fatal("adopted typography was not revalidated", w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version)); w.Code != 422 {
		t.Fatal("overflow generated", w.Code)
	}
}

func TestGeneratorThemeRoutesPreserveControlSecurity(t *testing.T) {
	s := libraryServer(t)
	for _, route := range []struct{ method, path string }{{"GET", "/api/generator/themes"}, {"POST", "/api/generator/themes"}, {"GET", "/api/generator/themes/retro/revisions/1"}, {"PUT", "/api/generator/themes/retro"}} {
		r := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("unprotected theme route", route, w.Code)
		}
		if route.method == "GET" {
			continue
		}
		r = httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
		r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
		r.Header.Set("Origin", "https://attacker.invalid")
		r.Header.Set("X-Restreamer-Control", "1")
		r.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("cross-origin theme mutation accepted", w.Code)
		}
	}
	theme := mediaauthor.RetroTheme(1)
	raw, _ := json.Marshal(theme)
	if w := dashboardRequest(s, "PUT", "/api/generator/themes/retro", string(raw)); w.Code != 409 {
		t.Fatal("built-in mutable", w.Code)
	}
	theme.Style.Effect = "unbounded"
	raw, _ = json.Marshal(map[string]any{"name": "Invalid effect", "base": mediaauthor.ThemeRef{ID: "retro", Revision: 1}, "style": theme.Style})
	if w := dashboardRequest(s, "POST", "/api/generator/themes", string(raw)); w.Code != 422 {
		t.Fatal("unsupported effect accepted", w.Code)
	}
}

func TestGeneratorThemeVideoRetainsLogoAndContinuousEffectAcrossRepeats(t *testing.T) {
	s := libraryServer(t)
	video := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "source.mp4", generatorVideoFixture(t, true)))
	logo := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "logo.png", assetPNG(t, color.NRGBA{G: 255, A: 255})))
	theme := mediaauthor.RetroTheme(2)
	theme.Style.BackgroundColor = "#553377"
	theme.Style.Logo = &mediaauthor.AssetRef{ID: logo.ID, Revision: 1}
	theme.Style.LogoHeightPercent = 8
	raw, _ := json.Marshal(map[string]any{"name": "Video brand", "base": mediaauthor.ThemeRef{ID: "retro", Revision: 2}, "style": theme.Style})
	w := dashboardRequest(s, "POST", "/api/generator/themes", string(raw))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &theme)
	body := fmt.Sprintf(`{"name":"Themed video","stage":"ending","theme":{"id":%q,"revision":1},"scenes":[{"id":"video","layout":"media","media_kind":"video","content_region":{"width_percent":60,"height_percent":60},"duration_seconds":2,"video":{"asset":{"id":%q,"revision":1},"trim_end_seconds":1,"repeat":true,"audio_enabled":true}}]}`, theme.ID, video.ID)
	w = dashboardRequest(s, "POST", "/api/generator/designs", body)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var d mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &d)
	want := make([]image.Image, 2)
	for i, frame := range []int{0, 35} {
		preview := dashboardRequest(s, "POST", fmt.Sprintf("/api/generator/preview?frame=%d", frame), w.Body.String())
		var err error
		want[i], err = png.Decode(preview.Body)
		if err != nil {
			t.Fatal(preview.Code, err)
		}
	}
	generatorServe(t, s)
	path := exactGeneratedVideo(t, s, d)
	for i, frame := range []int{0, 35} {
		data, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-vf", fmt.Sprintf("select=eq(n\\,%d)", frame), "-frames:v", "1", "-threads", "2", "-f", "image2pipe", "-c:v", "png", "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		got, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		outside := func(src image.Image) image.Image {
			im := image.NewRGBA(src.Bounds())
			draw.Draw(im, im.Bounds(), src, image.Point{}, draw.Src)
			draw.Draw(im, image.Rect(64, 36, 256, 144), image.NewUniform(color.Black), image.Point{}, draw.Src)
			return im
		}
		if diff := scenePixelDifference(outside(got), outside(want[i])); diff > 5 {
			t.Fatalf("frame%d lost themed backdrop/logo/effect: %.2f background got%v want%v logo got%v want%v", frame, diff, got.At(0, 0), want[i].At(0, 0), got.At(280, 8), want[i].At(280, 8))
		}
		band := func(src image.Image) image.Image {
			im := image.NewRGBA(image.Rect(0, 0, 320, 12))
			draw.Draw(im, im.Bounds(), src, image.Pt(0, 168), draw.Src)
			return im
		}
		match, other := scenePixelDifference(band(got), band(want[i])), scenePixelDifference(band(got), band(want[1-i]))
		if match+0.2 >= other {
			t.Fatalf("effect reset with source loop: frame%d match%.2f other%.2f", frame, match, other)
		}
	}
	if tonePower(decodePCM(t, path), 1.4, 880) < 1e8 {
		t.Fatal("theme composition lost repeated source audio")
	}
}
