package relay

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratorTransitionsPersistAndReportOverlappedDuration(t *testing.T) {
	s := libraryServer(t)
	body := `{"name":"Transitions","stage":"prestream","theme":{"id":"retro","revision":1},"scenes":[{"id":"a","layout":"title","text":"First","duration_seconds":0.8,"transition":{"kind":"crossfade","duration_seconds":0.2}},{"id":"b","layout":"title","text":"Second","duration_seconds":0.6,"transition":{"kind":"crossfade","duration_seconds":0.2}},{"id":"c","layout":"title","text":"Third","duration_seconds":1}]}`
	w := dashboardRequest(s, "POST", "/api/generator/designs", body)
	if w.Code != 201 {
		t.Fatalf("save transition: %d %s", w.Code, w.Body.String())
	}
	var saved map[string]any
	json.Unmarshal(w.Body.Bytes(), &saved)
	restarted := New(s.cfg, s.log)
	got := dashboardRequest(restarted, "GET", "/api/generator/designs/"+saved["id"].(string), "")
	if got.Code != 200 || got.Body.String() != w.Body.String() {
		t.Fatal("transition did not survive restart", got.Code, got.Body.String())
	}
	validation := dashboardRequest(s, "POST", "/api/generator/validate", got.Body.String())
	var result struct {
		Duration float64 `json:"duration_seconds"`
		Issues   []any   `json:"issues"`
	}
	if json.Unmarshal(validation.Body.Bytes(), &result) != nil || result.Duration != 2 || len(result.Issues) != 0 {
		t.Fatal("expected 60 frames minus two 5-frame overlaps", validation.Body.String())
	}
}

func TestGeneratorCrossfadeBlendsActualVideoAndSourceAudio(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "split.mp4", generatorSplitVideo(t)))
	d := videoDesign(t, s, a.ID, 1, 0, 1, 100, false, true)
	d.Scenes[0].Transition = &mediaauthor.Transition{Kind: "crossfade", DurationSeconds: .4}
	second := d.Scenes[0]
	second.ID = "second"
	second.Transition = nil
	second.Video = &mediaauthor.VideoScene{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, TrimStartSeconds: 1, TrimEndSeconds: 2, AudioEnabled: true}
	d.Scenes = append(d.Scenes, second)
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	path := exactGeneratedVideo(t, s, d)
	frames, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-map", "0:v:0", "-pix_fmt", "rgb24", "-f", "rawvideo", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 40*320*180*3 {
		t.Fatalf("expected40frames: bytes%d", len(frames))
	}
	pixel := func(n int) []byte { off := n*320*180*3 + (90*320+160)*3; return frames[off : off+3] }
	if p := pixel(5); p[0] < 200 || p[2] > 20 {
		t.Fatal("outgoing head changed", p)
	}
	if p := pixel(20); p[0] < 90 || p[0] > 165 || p[2] < 90 || p[2] > 165 {
		t.Fatal("crossfade midpoint must contain both actual scenes", p)
	}
	if p := pixel(30); p[2] < 200 || p[0] > 20 {
		t.Fatal("incoming body lost", p)
	}
	pcm := decodePCM(t, path)
	if tonePower(pcm, .2, 440) < 1e10 || tonePower(pcm, .2, 880) > 1e7 {
		t.Fatal("outgoing source audio changed")
	}
	if tonePower(pcm, .75, 440) < 1e9 || tonePower(pcm, .75, 880) < 1e9 {
		t.Fatal("source audio not crossfaded with video")
	}
	if tonePower(pcm, 1.2, 880) < 1e10 || tonePower(pcm, 1.2, 440) > 1e7 {
		t.Fatal("incoming source audio lost")
	}
}

func TestGeneratorTransitionValidationExplainsAdjacentOverlapAndRetainsDraft(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, 1)
	d.Scenes = append(d.Scenes, mediaauthor.Scene{ID: "middle", Layout: "title", Text: "Middle", DurationSeconds: .4}, mediaauthor.Scene{ID: "last", Layout: "title", Text: "Last", DurationSeconds: 1})
	for _, tc := range []struct {
		name, kind    string
		first, second float64
		field         string
	}{
		{"adjacent collision", "crossfade", .28, .2, "scenes.0.transition.duration_seconds"},
		{"outgoing too long", "crossfade", 1.2, 0, "scenes.0.transition.duration_seconds"},
		{"negative", "crossfade", -.1, 0, "scenes.0.transition.duration_seconds"},
		{"less than one frame", "crossfade", .001, 0, "scenes.0.transition.duration_seconds"},
		{"unknown kind", "wipe", .2, 0, "scenes.0.transition.kind"},
		{"cut with overlap", "cut", .2, 0, "scenes.0.transition.duration_seconds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d.Scenes[0].Transition = &mediaauthor.Transition{Kind: tc.kind, DurationSeconds: tc.first}
			d.Scenes[1].Transition = nil
			if tc.second > 0 {
				d.Scenes[1].Transition = &mediaauthor.Transition{Kind: "crossfade", DurationSeconds: tc.second}
			}
			d = saveMusicDesign(t, s, d)
			raw, _ := json.Marshal(d)
			w := dashboardRequest(s, "POST", "/api/generator/validate", string(raw))
			if w.Code != 200 || !strings.Contains(w.Body.String(), tc.field) {
				t.Fatal(w.Code, w.Body.String())
			}
			w = dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version))
			if w.Code != 422 || !strings.Contains(w.Body.String(), tc.field) {
				t.Fatal("accepted invalid timing", w.Code, w.Body.String())
			}
		})
	}
	// A middle scene can consist entirely of two adjacent, nonintersecting fades.
	d.Scenes[0].Transition = &mediaauthor.Transition{Kind: "crossfade", DurationSeconds: .2}
	d.Scenes[1].Transition = &mediaauthor.Transition{Kind: "crossfade", DurationSeconds: .2}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	path := exactGeneratedVideo(t, s, d)
	frames, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-count_frames", "-show_entries", "stream=nb_read_frames", "-of", "csv=p=0", path).Output()
	if err != nil || strings.TrimSpace(string(frames)) != "50" {
		t.Fatal("zero-body scene timing", err, string(frames))
	}
}

func TestGeneratorCrossfadeKeepsMusicOnShortenedSequenceTimeline(t *testing.T) {
	s := libraryServer(t)
	music := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "music.wav", musicFixture(t, "aevalsrc=if(lt(t\\,0.8)\\,0.1*sin(2*PI*440*t)\\,0.1*sin(2*PI*880*t)):s=48000", 2)))
	d := generatorDraft(t, s, 1)
	d.Scenes[0].Transition = &mediaauthor.Transition{Kind: "crossfade", DurationSeconds: .4}
	d.Scenes = append(d.Scenes, mediaauthor.Scene{ID: "second", Layout: "list", Text: "Next", Items: []string{"List content"}, DurationSeconds: 1})
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: music.ID, Revision: 1}, Mode: "end"}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	pcm, gain := musicResult(t, s, d)
	if gain != 1 {
		t.Fatal("unexpected attenuation", gain)
	}
	if tonePower(pcm, .3, 440) < 1e10 || tonePower(pcm, 1, 880) < 1e10 || tonePower(pcm, 1, 440) > 1e7 {
		t.Fatal("music timeline restarted or shifted by transition")
	}
	// Fades validate against1.6s final output, not the2s scene-duration sum.
	d.Soundtrack.FadeInSeconds = 1
	d.Soundtrack.FadeOutSeconds = .8
	d = saveMusicDesign(t, s, d)
	w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version))
	if w.Code != 422 || !strings.Contains(w.Body.String(), "soundtrack.fade") {
		t.Fatal("music fades ignored shortened duration", w.Code, w.Body.String())
	}
}

func TestGeneratorTransitionsKeepTemplatesRetriesAndPinnedAssets(t *testing.T) {
	s := libraryServer(t)
	image := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "image.png", assetPNG(t, color.RGBA{R: 255, A: 255})))
	d := generatorDraft(t, s, .4)
	d.Scenes[0].Transition = &mediaauthor.Transition{Kind: "crossfade", DurationSeconds: .2}
	d.Scenes = append(d.Scenes, mediaauthor.Scene{ID: "image", Layout: "text-image", Text: "Image", Image: &mediaauthor.AssetRef{ID: image.ID, Revision: 1}, DurationSeconds: .4})
	d = saveMusicDesign(t, s, d)
	data, _ := json.Marshal(ContentTemplate{Name: "Reusable transition", Content: d})
	w := dashboardRequest(s, "POST", "/api/generator/templates", string(data))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var template ContentTemplate
	json.Unmarshal(w.Body.Bytes(), &template)
	w = dashboardRequest(s, "POST", "/api/generator/templates/"+template.ID+"/designs", `{"name":"Independent copy","version":1,"theme":{"id":"retro","revision":1}}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var copied mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &copied)
	if copied.Scenes[0].Transition == nil || copied.Scenes[0].Transition.DurationSeconds != .2 {
		t.Fatal("template lost transition", copied)
	}
	original := submitGenerator(t, s, copied)
	if w = dashboardRequest(s, "POST", "/api/generator/jobs/"+original.ID+"/cancel", `{}`); w.Code != 202 {
		t.Fatal(w.Code)
	}
	copied.Scenes[0].Transition.DurationSeconds = .04
	copied.Scenes[0], copied.Scenes[1] = copied.Scenes[1], copied.Scenes[0]
	saveMusicDesign(t, s, copied)
	restarted := New(s.cfg, s.log)
	w = dashboardRequest(restarted, "GET", "/api/generator/templates/"+template.ID, "")
	var retained ContentTemplate
	json.Unmarshal(w.Body.Bytes(), &retained)
	if retained.Content.Scenes[0].Transition.DurationSeconds != .2 {
		t.Fatal("copy edit mutated template")
	}
	w = dashboardRequest(restarted, "POST", "/api/generator/jobs/"+original.ID+"/retry", `{}`)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	retry := generatorJob(t, w)
	if retry.Design.Scenes[0].Transition.DurationSeconds != .2 || retry.Duration != .6 || retry.DesignRevision != original.DesignRevision {
		t.Fatal("retry substituted draft timing", retry)
	}
	w = dashboardRequest(restarted, "DELETE", "/api/generator/assets/"+image.ID, `{}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"kind":"job"`) || !strings.Contains(w.Body.String(), `"kind":"template"`) {
		t.Fatal("transition inputs not retained", w.Code, w.Body.String())
	}
	generatorServe(t, restarted)
	if ready := waitGeneratorJob(t, restarted, retry.ID); ready.State != "ready" {
		t.Fatal(ready)
	}
}

func TestGeneratorCrossfadesKeepShortScenesAlignedAt44100Hz24FPS(t *testing.T) {
	s := libraryServer(t)
	if w := assetRequest(t, s, map[string]string{"fps": "24", "sample_rate": "44100"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "tone.mp4", generatorVideoFixture(t, true)))
	d := videoDesign(t, s, a.ID, 3.0/24, 0, 1, 100, false, true)
	d.Scenes[0].Transition = &mediaauthor.Transition{Kind: "crossfade", DurationSeconds: 1.0 / 24}
	for i := 1; i < 20; i++ {
		scene := d.Scenes[0]
		scene.ID = fmt.Sprintf("short-%d", i)
		d.Scenes = append(d.Scenes, scene)
	}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	path := exactGeneratedVideo(t, s, d)
	frames, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-count_frames", "-show_entries", "stream=nb_read_frames", "-of", "csv=p=0", path).Output()
	if err != nil || strings.TrimSpace(string(frames)) != "41" {
		t.Fatal("expected60frames minus19overlapframes", err, string(frames))
	}
	pcm, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-map", "0:a:0", "-ac", "1", "-ar", "44100", "-f", "s16le", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	// 41/24seconds is75337.5samples: cumulative rounding yields75338,
	// with only the final AAC packet allowed to pad that presentation window.
	if samples := len(pcm) / 2; samples < 75338-1024 || samples > 75338+1024 {
		t.Fatalf("audio drift or per-piece AAC padding: %d", samples)
	}
	var energy float64
	for i := int(.4*44100) * 2; i < int(1.2*44100)*2; i += 2 {
		sample := float64(int16(binary.LittleEndian.Uint16(pcm[i:])))
		energy += sample * sample
	}
	if energy < 1e9 {
		t.Fatal("source audio lost during one-frame transitions")
	}
}

func TestGeneratorTransitionCancellationCleansWorkspaceAndPreservesReadyOutput(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, .2)
	generatorServe(t, s)
	good := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if good.State != "ready" {
		t.Fatal(good)
	}
	d.Scenes[0].DurationSeconds = 240
	d.Scenes[0].Transition = &mediaauthor.Transition{Kind: "crossfade", DurationSeconds: 100}
	d.Scenes = append(d.Scenes, mediaauthor.Scene{ID: "second", Layout: "title", Text: "Next", DurationSeconds: 240})
	d = saveMusicDesign(t, s, d)
	job := submitGenerator(t, s, d)
	eventually(t, func() bool {
		current := generatorJob(t, dashboardRequest(s, "GET", "/api/generator/jobs/"+job.ID, ""))
		return current.State == "running" && current.Progress >= 60
	})
	w := dashboardRequest(s, "POST", "/api/generator/jobs/"+job.ID+"/cancel", `{}`)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if result := waitGeneratorJob(t, s, job.ID); result.State != "cancelled" || result.MediaRevision != "" {
		t.Fatal("cancelled transition published output", result)
	}
	if _, err := os.Stat(filepath.Join(s.cfg.LibraryDirectory, "generator", "work", job.ID)); !os.IsNotExist(err) {
		t.Fatal("cancelled workspace remains", err)
	}
	if w := dashboardRequest(s, "GET", "/api/library/revisions/"+good.MediaRevision+"/preview", ""); w.Code != 200 {
		t.Fatal("last good output lost", w.Code)
	}
	d.Scenes = d.Scenes[:1]
	d.Scenes[0].DurationSeconds = .2
	d.Scenes[0].Transition = nil
	d = saveMusicDesign(t, s, d)
	if next := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID); next.State != "ready" {
		t.Fatal("cancel did not release shared preparation slot", next)
	}
}
