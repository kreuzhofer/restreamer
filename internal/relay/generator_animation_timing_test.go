package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAnimationLoopTimingUsesThemeCycleAndCrossfadeAllowance(t *testing.T) {
	s := libraryServer(t)
	body := `{"name":"Whole animations","stage":"prestream","theme":{"id":"arcade-after-hours","revision":2},"loop_transition":{"kind":"crossfade","duration_seconds":0.5},"scenes":[{"id":"one","layout":"title","text":"Hello","duration_seconds":10,"animation_loops":1,"transition":{"kind":"crossfade","duration_seconds":0.5}},{"id":"two","layout":"title","text":"Again","duration_seconds":10,"animation_loops":2}]}`
	saved := dashboardRequest(s, "POST", "/api/generator/designs", body)
	if saved.Code != 201 {
		t.Fatal(saved.Code, saved.Body.String())
	}
	var design struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
		Scenes  []struct {
			Duration float64 `json:"duration_seconds"`
			Loops    int     `json:"animation_loops"`
		} `json:"scenes"`
	}
	if err := json.Unmarshal(saved.Body.Bytes(), &design); err != nil {
		t.Fatal(err)
	}
	if design.Scenes[0].Duration != 16.52 || design.Scenes[1].Duration != 32.52 || design.Scenes[1].Loops != 2 {
		t.Fatalf("25fps crossfades round to13frames: %+v", design)
	}
	validation := dashboardRequest(s, "POST", "/api/generator/validate", saved.Body.String())
	var timing struct {
		Duration float64 `json:"duration_seconds"`
		Cycle    int     `json:"animation_loop_seconds"`
		Issues   []any   `json:"issues"`
	}
	if err := json.Unmarshal(validation.Body.Bytes(), &timing); err != nil {
		t.Fatal(err)
	}
	if validation.Code != 200 || timing.Duration != 48 || timing.Cycle != 16 || timing.Issues == nil || len(timing.Issues) != 0 {
		t.Fatal(validation.Code, validation.Body.String())
	}
	jobResponse := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, design.ID, design.Version))
	job := generatorJob(t, jobResponse)
	if jobResponse.Code != 202 || job.Duration != 48 || job.Design.Scenes[0].DurationSeconds != 16.52 {
		t.Fatal("generation did not capture resolved loop timing", jobResponse.Code, jobResponse.Body.String())
	}
	read := dashboardRequest(s, "GET", "/api/generator/designs/"+design.ID, "")
	if read.Code != 200 || read.Body.String() != saved.Body.String() {
		t.Fatal("loop choice did not survive reload", read.Body.String())
	}
}

func TestAnimationLoopTimingThemeChangesAndLimits(t *testing.T) {
	s := libraryServer(t)
	for _, tc := range []struct {
		name, stage, theme string
		revision, loops    int
		cycle, duration    float64
		invalid            bool
	}{
		{"neon", "prestream", "neon-night", 2, 2, 16, 32, false},
		{"workshop", "prestream", "maker-workshop", 1, 2, 12, 24, false},
		{"maze", "ending", "pacman-maze", 1, 2, 40, 80, false},
		{"one shot", "ending", "maker-workshop", 1, 1, 0, 10, true},
		{"static", "prestream", "retro", 1, 1, 0, 10, true},
		{"zero", "prestream", "pacman-maze", 1, 0, 40, 10, true},
		{"negative", "prestream", "pacman-maze", 1, -1, 40, 10, true},
		{"too long", "prestream", "pacman-maze", 1, 91, 40, 10, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"name":"Timing","stage":%q,"theme":{"id":%q,"revision":%d},"scenes":[{"id":"one","layout":"title","text":"Hello","duration_seconds":10,"animation_loops":%d}]}`, tc.stage, tc.theme, tc.revision, tc.loops)
			w := dashboardRequest(s, "POST", "/api/generator/validate", body)
			var v struct {
				Cycle    float64             `json:"animation_loop_seconds"`
				Duration float64             `json:"duration_seconds"`
				Issues   []mediaauthor.Issue `json:"issues"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
				t.Fatal(w.Code, w.Body.String())
			}
			if v.Cycle != tc.cycle || v.Duration != tc.duration || (len(v.Issues) > 0) != tc.invalid {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	// The loop count follows explicit theme adoption, while fixed seconds remain fixed.
	d := generatorDraft(t, s, 10)
	d.Theme = mediaauthor.ThemeRef{ID: "arcade-after-hours", Revision: 2}
	loops := 2
	d.Scenes[0].AnimationLoops = &loops
	d = saveMusicDesign(t, s, d)
	if d.Scenes[0].DurationSeconds != 32 {
		t.Fatal(d)
	}
	d.Theme = mediaauthor.ThemeRef{ID: "pacman-maze", Revision: 1}
	d = saveMusicDesign(t, s, d)
	if d.Scenes[0].DurationSeconds != 80 {
		t.Fatal(d)
	}
	d.Scenes[0].AnimationLoops = nil
	d.Scenes[0].DurationSeconds = 10
	d = saveMusicDesign(t, s, d)
	if d.Scenes[0].DurationSeconds != 10 {
		t.Fatal(d)
	}
}

func TestAnimatedBackgroundContinuesAcrossSceneCuts(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, 2)
	d.Theme = mediaauthor.ThemeRef{ID: "pacman-maze", Revision: 1}
	d.Scenes[0].Text = ""
	d.Scenes = append(d.Scenes, mediaauthor.Scene{ID: "second", Layout: "title", DurationSeconds: 1})
	d = saveMusicDesign(t, s, d)
	raw, _ := json.Marshal(d)
	first := dashboardRequest(s, "POST", "/api/generator/preview?scene=0&frame=50", string(raw))
	second := dashboardRequest(s, "POST", "/api/generator/preview?scene=1&frame=0", string(raw))
	if first.Code != 200 || second.Code != 200 || !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatal("next scene reset the background instead of continuing at two seconds", first.Code, second.Code)
	}
	expected, err := png.Decode(bytes.NewReader(first.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
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
	preview := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
	path := filepath.Join(t.TempDir(), "continuous.mp4")
	if err := os.WriteFile(path, preview.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	pixels, err := exec.Command("ffmpeg", "-v", "error", "-threads", "2", "-i", path, "-vf", `select=eq(n\,50)`, "-frames:v", "1", "-threads", "2", "-f", "image2pipe", "-c:v", "png", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := png.Decode(bytes.NewReader(pixels))
	if err != nil {
		t.Fatal(err)
	}
	if delta := scenePixelDifference(actual, expected); delta > 20 {
		t.Fatalf("encoded boundary differs from continuous preview: %.2f", delta)
	}
	reset := dashboardRequest(s, "POST", "/api/generator/preview?scene=0&frame=0", string(raw))
	resetImage, err := png.Decode(reset.Body)
	if err != nil {
		t.Fatal(err)
	}
	if scenePixelDifference(actual, resetImage) <= scenePixelDifference(actual, expected)+0.2 {
		t.Fatal("encoded boundary resembles the restarted animation")
	}
}
