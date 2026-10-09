package relay

import (
	"encoding/json"
	"fmt"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestGeneratorEndingFinishValidationAndPersistence(t *testing.T) {
	s := libraryServer(t)
	for _, tc := range []struct {
		name, fade string
		seconds    float64
		valid      bool
	}{
		{"default", "", 2, true}, {"whole sequence", "", 1, true}, {"short default", "", .4, false},
		{"changed", `,"ending_fade_seconds":0.4`, .4, true}, {"disabled", `,"ending_fade_seconds":0`, .04, true},
		{"negative", `,"ending_fade_seconds":-1`, 2, false}, {"too long", `,"ending_fade_seconds":2.04`, 2, false},
		{"one frame", `,"ending_fade_seconds":0.04`, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"name":"Ending","stage":"ending","theme":{"id":"retro","revision":1},"scenes":[{"id":"end","layout":"title","text":"Thanks","duration_seconds":%g}]%s}`, tc.seconds, tc.fade)
			w := dashboardRequest(s, "POST", "/api/generator/designs", body)
			if w.Code != 201 {
				t.Fatal(w.Code, w.Body.String())
			}
			var d map[string]any
			json.Unmarshal(w.Body.Bytes(), &d)
			restarted := New(s.cfg, s.log)
			got := dashboardRequest(restarted, "GET", "/api/generator/designs/"+d["id"].(string), "")
			if got.Body.String() != w.Body.String() {
				t.Fatal("finish lost on restart", got.Body.String())
			}
			validation := dashboardRequest(s, "POST", "/api/generator/validate", got.Body.String())
			var result struct {
				Duration float64                  `json:"duration_seconds"`
				Issues   []struct{ Field string } `json:"issues"`
			}
			if json.Unmarshal(validation.Body.Bytes(), &result) != nil || validation.Code != 200 {
				t.Fatal(validation.Code, validation.Body.String())
			}
			if result.Duration != tc.seconds {
				t.Fatal("finish appended time", result.Duration)
			}
			hasError := strings.Contains(validation.Body.String(), "ending_fade_seconds")
			if hasError == tc.valid {
				t.Fatal("finish validation", validation.Body.String())
			}
			if !tc.valid {
				job := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":1}`, d["id"]))
				if job.Code != 422 || !strings.Contains(job.Body.String(), "ending_fade_seconds") {
					t.Fatal("invalid finish admitted", job.Code, job.Body.String())
				}
			}
		})
	}
}

// Inspect the exact prepared preview, including the authored presentation end;
// AAC decoder padding after that interval is not evidence of an ending fade.
func TestGeneratorEndingFinishFadesWholeCompositionWithoutAddingTime(t *testing.T) {
	s := libraryServer(t)
	video := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "split.mp4", generatorSplitVideo(t)))
	music := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "music.wav", musicFixture(t, "sine=frequency=1320:sample_rate=48000", 3)))
	d := videoDesign(t, s, video.ID, 2, 0, 1, 100, true, true)
	second := d.Scenes[0]
	second.ID = "last"
	second.DurationSeconds = .4
	second.Video = &mediaauthor.VideoScene{Asset: mediaauthor.AssetRef{ID: video.ID, Revision: 1}, TrimStartSeconds: 1, TrimEndSeconds: 1.4, AudioEnabled: true}
	d.Scenes[0].Transition = &mediaauthor.Transition{Kind: "crossfade", DurationSeconds: .2}
	d.Scenes = append(d.Scenes, second)
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: music.ID, Revision: 1}, Mode: "end"}
	generatorServe(t, s)
	before := readStage(t, s)
	selections := dashboardRequest(s, "GET", "/api/stage-media", "").Body.String()
	for _, tc := range []struct {
		name      string
		fade      *float64
		fadeStart int
	}{{"default", nil, 30}, {"changed", musicVolume(.4), 45}, {"disabled", musicVolume(0), 55}} {
		t.Run(tc.name, func(t *testing.T) {
			d.EndingFadeSeconds = tc.fade
			d = saveMusicDesign(t, s, d)
			j := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
			if j.State != "ready" || j.Duration != 2.2 {
				t.Fatal("finite ending failed or appended time", j)
			}
			w := dashboardRequest(s, "GET", "/api/library/revisions/"+j.MediaRevision+"/preview", "")
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			path := filepath.Join(t.TempDir(), "ending.mp4")
			if err := os.WriteFile(path, w.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			rgb, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-map", "0:v:0", "-vsync", "0", "-pix_fmt", "rgb24", "-f", "rawvideo", "-").Output()
			if err != nil {
				t.Fatal(err)
			}
			size := 320 * 180 * 3
			if len(rgb) != 55*size {
				t.Fatal("wrong frame count", len(rgb)/size)
			}
			duration, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=duration", "-of", "csv=p=0", path).Output()
			seconds, parseErr := strconv.ParseFloat(strings.TrimSpace(string(duration)), 64)
			if err != nil || parseErr != nil || math.Abs(seconds-2.2) > .001 {
				t.Fatal("prepared preview disagrees with authored duration", string(duration), err, parseErr)
			}
			last := rgb[54*size:]
			maxPixel := byte(0)
			for _, v := range last {
				if v > maxPixel {
					maxPixel = v
				}
			}
			if tc.fadeStart < 55 && maxPixel != 0 {
				t.Fatal("last whole composition frame must be exact black", maxPixel)
			}
			if tc.fadeStart == 55 && last[(90*320+160)*3+2] < 200 {
				t.Fatal("disabled finish lost final image")
			}
			// The red body falls only after the selected final-envelope start.
			redAt := func(n int) byte { return rgb[n*size+(90*320+160)*3] }
			if tc.fadeStart == 30 && (redAt(40) > 170 || redAt(40) < 100) {
				t.Fatal("default fade did not cover complete sequence", redAt(40))
			}
			if tc.fadeStart >= 45 && redAt(40) < 220 {
				t.Fatal("changed/disabled envelope began too early", redAt(40))
			}
			after := readStage(t, s)
			if after.Stage != before.Stage || after.Mode != before.Mode || after.Context != before.Context || dashboardRequest(s, "GET", "/api/stage-media", "").Body.String() != selections {
				t.Fatal("generation or preview changed broadcast control", before, after)
			}
			pcm := decodePCM(t, path)
			if len(pcm) < int(2.2*48000) {
				t.Fatal("audio ended early", len(pcm))
			}
			power := tonePower(pcm, 2.05, 1320)
			source := tonePower(pcm, 2.05, 880)
			if tc.fadeStart == 55 && (power < 1e10 || source < 1e10) {
				t.Fatal("disabled finish lost mixed audio", power, source)
			}
			if tc.fadeStart < 55 && (power/tonePower(pcm, .2, 1320) > .15 || source/tonePower(pcm, .2, 440) > .15) {
				t.Fatal("finish did not fade complete mixed audio", power, source)
			}
			var peak float64
			for _, v := range pcm[int(2.19*48000):int(2.2*48000)] {
				peak = max(peak, math.Abs(v))
			}
			if tc.fadeStart < 55 && peak > 4 {
				t.Fatal("final presented audio window is not silent", peak)
			}
			if tc.fadeStart == 55 && peak < 100 {
				t.Fatal("disabled finish unexpectedly silenced endpoint", peak)
			}
		})
	}
}

func TestGeneratorEndingFinishTemplatesSnapshotsAndRetryRemainIndependent(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, 2)
	d.Stage = "ending"
	d.EndingFadeSeconds = musicVolume(.4)
	d = saveMusicDesign(t, s, d)
	body, _ := json.Marshal(ContentTemplate{Name: "Reusable ending", Content: d})
	w := dashboardRequest(s, "POST", "/api/generator/templates", string(body))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var template ContentTemplate
	json.Unmarshal(w.Body.Bytes(), &template)
	w = dashboardRequest(s, "POST", "/api/generator/templates/"+template.ID+"/designs", `{"name":"Independent ending","version":1,"theme":{"id":"retro","revision":2}}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var copied mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &copied)
	if copied.EndingFadeSeconds == nil || *copied.EndingFadeSeconds != .4 {
		t.Fatal("theme/template copy lost finish", copied)
	}
	captured := submitGenerator(t, s, copied)
	if captured.Design.EndingFadeSeconds == nil || *captured.Design.EndingFadeSeconds != .4 {
		t.Fatal("job lost finish", captured)
	}
	w = dashboardRequest(s, "POST", "/api/generator/jobs/"+captured.ID+"/cancel", `{}`)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	copied.EndingFadeSeconds = musicVolume(0)
	copied.Theme.Revision = 1
	copied = saveMusicDesign(t, s, copied)
	restarted := New(s.cfg, s.log)
	w = dashboardRequest(restarted, "GET", "/api/generator/templates/"+template.ID, "")
	json.Unmarshal(w.Body.Bytes(), &template)
	if template.Content.EndingFadeSeconds == nil || *template.Content.EndingFadeSeconds != .4 {
		t.Fatal("editing copied draft changed template", w.Body.String())
	}
	w = dashboardRequest(restarted, "POST", "/api/generator/jobs/"+captured.ID+"/retry", `{}`)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	retry := generatorJob(t, w)
	if retry.Design.EndingFadeSeconds == nil || *retry.Design.EndingFadeSeconds != .4 || retry.DesignRevision != captured.DesignRevision {
		t.Fatal("retry substituted edited finish", retry)
	}
	// Omitted default is explicit in the immutable snapshot; inert settings on
	// PRESTREAM remain saved, without applying ENDING validation there.
	d.EndingFadeSeconds = nil
	d = saveMusicDesign(t, s, d)
	j := submitGenerator(t, s, d)
	if j.Design.EndingFadeSeconds == nil || *j.Design.EndingFadeSeconds != 1 {
		t.Fatal("default not pinned in snapshot", j)
	}
	d.Stage = "prestream"
	d.EndingFadeSeconds = musicVolume(600)
	d.Scenes[0].DurationSeconds = .4
	d = saveMusicDesign(t, s, d)
	raw, _ := json.Marshal(d)
	w = dashboardRequest(s, "POST", "/api/generator/validate", string(raw))
	if strings.Contains(w.Body.String(), "ending_fade_seconds") {
		t.Fatal("ending finish applied to PRESTREAM", w.Body.String())
	}
}

func TestGeneratorEndingFinishUsesFrameRoundedBoundaryAtSupportedRates(t *testing.T) {
	for _, fps := range []int{24, 25, 30} {
		t.Run(fmt.Sprint(fps), func(t *testing.T) {
			s := libraryServer(t)
			if w := assetRequest(t, s, map[string]string{"fps": fmt.Sprint(fps), "sample_rate": "44100"}, "", nil); w.Code != 204 {
				t.Fatal(w.Code, w.Body.String())
			}
			d := generatorDraft(t, s, 4/float64(fps))
			d.Stage = "ending"
			d.EndingFadeSeconds = musicVolume(2 / float64(fps))
			d = saveMusicDesign(t, s, d)
			generatorServe(t, s)
			path := exactGeneratedVideo(t, s, d)
			rgb, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-map", "0:v:0", "-vsync", "0", "-pix_fmt", "rgb24", "-f", "rawvideo", "-").Output()
			if err != nil {
				t.Fatal(err)
			}
			size := 320 * 180 * 3
			if len(rgb) != 4*size {
				t.Fatal("short ending frame count", len(rgb)/size)
			}
			for _, v := range rgb[3*size:] {
				if v != 0 {
					t.Fatal("short ending did not finish black")
					break
				}
			}
		})
	}
}
