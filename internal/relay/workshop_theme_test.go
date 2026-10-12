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

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func TestWorkshopStagePreviewsSelectMotionAndHoldEnding(t *testing.T) {
	for _, id := range []string{"maker-workshop", "maker-halloween", "maker-christmas"} {
		t.Run(id, func(t *testing.T) {
			s := arcadeThemeServer(t)
			d := generatorDraft(t, s, 12)
			d.Theme = mediaauthor.ThemeRef{ID: id, Revision: 1}
			d.Scenes[0].Font = ""
			d.Scenes[0].FontSize = 0
			d.Scenes[0].Text = "HELLO MAKERS"
			preview := func(stage string, frame int) []byte {
				t.Helper()
				d.Stage = stage
				raw, _ := json.Marshal(d)
				w := dashboardRequest(s, "POST", fmt.Sprintf("/api/generator/preview?frame=%d", frame), string(raw))
				if w.Code != 200 {
					t.Fatalf("%s frame %d: %d %s", stage, frame, w.Code, w.Body.String())
				}
				if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
					t.Fatal(err)
				}
				return append([]byte(nil), w.Body.Bytes()...)
			}
			start := preview("prestream", 0)
			if !bytes.Equal(start, preview("prestream", 300)) {
				t.Fatal("prestream motion does not loop")
			}
			end := preview("ending", 0)
			if bytes.Equal(start, end) {
				t.Fatal("ending reused the prestream choreography")
			}
			quiet := preview("ending", 300)
			if !bytes.Equal(quiet, preview("ending", 600)) {
				t.Fatal("ending restarts instead of holding its final pose")
			}
			d.Scenes[0].Text = "SEE YOU SOON"
			if bytes.Equal(quiet, preview("ending", 600)) {
				t.Fatal("ending headline is not editable")
			}
		})
	}
}

func TestWorkshopBRBPreparesActivatesAndSurvivesRestart(t *testing.T) {
	for _, id := range []string{"maker-workshop", "maker-halloween", "maker-christmas"} {
		t.Run(id, func(t *testing.T) {
			s := arcadeThemeServer(t)
			status := dashboardRequest(s, "GET", "/api/dashboard", "")
			var current struct {
				Assets brbSettings `json:"brb_assets"`
			}
			json.Unmarshal(status.Body.Bytes(), &current)
			prepared := dashboardRequest(s, "POST", "/api/brb/theme/prepare", fmt.Sprintf(`{"theme":{"id":%q,"revision":1},"base_generation":%q}`, id, current.Assets.Generation))
			if prepared.Code != 201 {
				t.Fatal(prepared.Code, prepared.Body.String())
			}
			var candidate brbThemeCandidate
			json.Unmarshal(prepared.Body.Bytes(), &candidate)
			if candidate.VideoDuration != 12 || candidate.Settings.Theme.ID != id {
				t.Fatal(candidate)
			}
			if _, err := os.Stat(filepath.Join(s.cfg.BRB.Directory, candidate.ID, "artwork-master.mp4")); !os.IsNotExist(err) {
				t.Fatal("preparation master was not cleaned", err)
			}
			media := dashboardRequest(s, "GET", "/api/brb/theme/candidate/preview", "")
			if media.Code != 200 {
				t.Fatal(media.Code, media.Body.String())
			}
			path := filepath.Join(t.TempDir(), "brb.mp4")
			if err := os.WriteFile(path, media.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if scenePixelDifference(workshopDecodedFrame(t, path, 0), workshopDecodedFrame(t, path, 100)) < 1 {
				t.Fatal("BRB is static")
			}
			activated := dashboardRequest(s, "POST", "/api/brb/theme/activate", fmt.Sprintf(`{"id":%q,"base_generation":%q}`, candidate.ID, candidate.Base))
			if activated.Code != 204 {
				t.Fatal(activated.Code, activated.Body.String())
			}
			restarted := New(s.cfg, s.log)
			if err := restarted.initialize(); err != nil {
				t.Fatal(err)
			}
			restored := restarted.broadcast.media.settings
			if restored.Generation != candidate.ID || restored.Theme.ID != id || restored.Theme.Revision != 1 {
				t.Fatal("lost retained seasonal BRB", restored)
			}
		})
	}
}

func workshopDecodedFrame(t *testing.T, path string, frame int) image.Image {
	t.Helper()
	data, err := exec.Command("ffmpeg", "-v", "error", "-threads", "2", "-i", path, "-vf", fmt.Sprintf("select=eq(n\\,%d)", frame), "-frames:v", "1", "-threads", "2", "-f", "image2pipe", "-c:v", "png", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestWorkshopGeneratedEndingHoldsPastMasterAndFinishesBlack(t *testing.T) {
	s := arcadeThemeServer(t)
	d := generatorDraft(t, s, 16)
	d.Stage = "ending"
	d.Theme = mediaauthor.ThemeRef{ID: "maker-halloween", Revision: 1}
	d.Scenes[0].Text = "SEE YOU SOON"
	d.Scenes[0].Font = ""
	d.Scenes[0].FontSize = 0
	fade := 1.0
	d.EndingFadeSeconds = &fade
	// An overlapping text change must not restart the one-shot backdrop.
	next := d.Scenes[0]
	next.ID, next.DurationSeconds = "follow-up", 14.4
	d.Scenes[0].DurationSeconds = 2
	d.Scenes[0].Transition = &mediaauthor.Transition{Kind: "crossfade", DurationSeconds: .4}
	d.Scenes = append(d.Scenes, next)
	raw, _ := json.Marshal(d)
	saved := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(raw))
	if saved.Code != 200 {
		t.Fatal(saved.Code, saved.Body.String())
	}
	json.Unmarshal(saved.Body.Bytes(), &d)
	expectedResponse := dashboardRequest(s, "POST", "/api/generator/preview?frame=325", saved.Body.String())
	expected, err := png.Decode(expectedResponse.Body)
	if err != nil {
		t.Fatal(expectedResponse.Code, err)
	}
	globalFrame := dashboardRequest(s, "POST", "/api/generator/preview?frame=65", saved.Body.String())
	nextScene := dashboardRequest(s, "POST", "/api/generator/preview?scene=1&frame=25", saved.Body.String())
	if globalFrame.Code != 200 || !bytes.Equal(globalFrame.Body.Bytes(), nextScene.Body.Bytes()) {
		t.Fatal("crossfade scene preview did not use composition timing")
	}
	resetFrame := dashboardRequest(s, "POST", "/api/generator/preview?frame=25", saved.Body.String())
	resetExpected, err := png.Decode(bytes.NewReader(resetFrame.Body.Bytes()))
	if err != nil {
		t.Fatal(resetFrame.Code, err)
	}
	earlyExpected, err := png.Decode(bytes.NewReader(globalFrame.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	generatorServe(t, s)
	job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if job.State != "ready" {
		t.Fatal(job)
	}
	media := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
	if media.Code != 200 {
		t.Fatal(media.Code)
	}
	path := filepath.Join(t.TempDir(), "ending.mp4")
	os.WriteFile(path, media.Body.Bytes(), 0600)
	actualEarly := workshopDecodedFrame(t, path, 65)
	alignedDelta := scenePixelDifference(actualEarly, earlyExpected)
	resetDelta := scenePixelDifference(actualEarly, resetExpected)
	if alignedDelta >= resetDelta {
		t.Fatalf("ending animation restarted at crossfade: aligned %f, reset %f", alignedDelta, resetDelta)
	}

	if delta := scenePixelDifference(workshopDecodedFrame(t, path, 325), expected); delta > 8 {
		t.Fatalf("ending restarted instead of holding: %f", delta)
	}
	last := workshopDecodedFrame(t, path, 399)
	for y := 0; y < last.Bounds().Dy(); y++ {
		for x := 0; x < last.Bounds().Dx(); x++ {
			r, g, b, _ := last.At(x, y).RGBA()
			if r+g+b != 0 {
				t.Fatal("ending did not fade to black")
			}
		}
	}
}

func TestWorkshopEndingHoldsBehindRepeatingInsetVideo(t *testing.T) {
	s := arcadeThemeServer(t)
	asset := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "source.mp4", generatorVideoFixture(t, false)))
	body := fmt.Sprintf(`{"name":"Workshop ending video","stage":"ending","ending_fade_seconds":0,"theme":{"id":"maker-christmas","revision":1},"scenes":[{"id":"video","layout":"media","media_kind":"video","text":"","content_region":{"width_percent":40,"height_percent":40},"duration_seconds":14,"video":{"asset":{"id":%q,"revision":1},"trim_end_seconds":1,"repeat":true}}]}`, asset.ID)
	created := dashboardRequest(s, "POST", "/api/generator/designs", body)
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	var d mediaauthor.Design
	json.Unmarshal(created.Body.Bytes(), &d)
	// The goodbye happens once, before the later inset scene starts.
	d.Scenes = append([]mediaauthor.Scene{{ID: "thanks", Layout: "title", Text: "THANK YOU", DurationSeconds: 8}}, d.Scenes...)
	raw, _ := json.Marshal(d)
	saved := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(raw))
	if saved.Code != 200 {
		t.Fatal(saved.Code, saved.Body.String())
	}
	json.Unmarshal(saved.Body.Bytes(), &d)
	firstInset := dashboardRequest(s, "POST", "/api/generator/preview?scene=1&frame=0", saved.Body.String())
	settledInset := dashboardRequest(s, "POST", "/api/generator/preview?frame=200", created.Body.String())
	if firstInset.Code != 200 || !bytes.Equal(firstInset.Body.Bytes(), settledInset.Body.Bytes()) {
		t.Fatal("ending preview restarted its goodbye at the next scene", firstInset.Code)
	}
	preview := dashboardRequest(s, "POST", "/api/generator/preview?scene=1&frame=125", saved.Body.String())
	expected, err := png.Decode(preview.Body)
	if err != nil {
		t.Fatal(preview.Code, err)
	}
	generatorServe(t, s)
	job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if job.State != "ready" {
		t.Fatal(job)
	}
	media := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
	if media.Code != 200 {
		t.Fatal(media.Code)
	}
	path := filepath.Join(t.TempDir(), "inset.mp4")
	if err := os.WriteFile(path, media.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if delta := scenePixelDifference(workshopDecodedFrame(t, path, 325), expected); delta > 8 {
		t.Fatalf("inset reset the settled workshop: %f", delta)
	}
}
