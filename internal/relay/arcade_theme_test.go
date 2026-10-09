package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
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

func TestArcadeThemeIsSelectableImmutableAndTextIsEditable(t *testing.T) {
	s := arcadeThemeServer(t)
	w := dashboardRequest(s, "GET", "/api/generator/themes/arcade-after-hours/revisions/1", "")
	if w.Code != 200 {
		t.Fatalf("arcade theme unavailable: %d %s", w.Code, w.Body.String())
	}
	var theme mediaauthor.Theme
	if err := json.Unmarshal(w.Body.Bytes(), &theme); err != nil {
		t.Fatal(err)
	}
	if !theme.Builtin || theme.Name != "Arcade After Hours" {
		t.Fatal(theme)
	}
	if w := dashboardRequest(s, "PUT", "/api/generator/themes/arcade-after-hours", w.Body.String()); w.Code != 409 {
		t.Fatal("builtin mutable", w.Code)
	}
	d := generatorDraft(t, s, 16)
	d.Theme = mediaauthor.ThemeRef{ID: "arcade-after-hours", Revision: 1}
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
}

func TestArcadeAnimationMatchesGeneratedAndBRBPreviews(t *testing.T) {
	s := arcadeThemeServer(t)
	d := generatorDraft(t, s, 16)
	d.Theme = mediaauthor.ThemeRef{ID: "arcade-after-hours", Revision: 1}
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
	prepared := dashboardRequest(s, "POST", "/api/brb/theme/prepare", fmt.Sprintf(`{"theme":{"id":"arcade-after-hours","revision":1},"base_generation":%q}`, current.Assets.Generation))
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

func TestArcadeBackgroundContinuesBehindRepeatingInsetVideo(t *testing.T) {
	s := arcadeThemeServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "source.mp4", generatorVideoFixture(t, false)))
	body := fmt.Sprintf(`{"name":"Arcade video","stage":"ending","ending_fade_seconds":0,"theme":{"id":"arcade-after-hours","revision":1},"scenes":[{"id":"video","layout":"media","media_kind":"video","text":"","content_region":{"width_percent":40,"height_percent":40},"duration_seconds":8,"video":{"asset":{"id":%q,"revision":1},"trim_end_seconds":1,"repeat":true}}]}`, a.ID)
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
}
