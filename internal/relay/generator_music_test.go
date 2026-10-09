package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func musicFixture(t *testing.T, expression string, seconds float64) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "music.wav")
	args := []string{"-v", "error", "-f", "lavfi", "-i", expression, "-t", decimal(seconds), "-ac", "2", "-ar", "48000", "-c:a", "pcm_s16le", path}
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("music fixture %v %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestGeneratorMusicAssetsKeepExactRevisionsAndRejectInvalidAudio(t *testing.T) {
	s := libraryServer(t)
	data := musicFixture(t, "sine=frequency=440:sample_rate=48000", 1)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "track.wav", data))
	w := dashboardRequest(s, "GET", "/api/generator/assets/"+a.ID+"/revisions/1", "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "audio/wav" {
		t.Fatal(w.Code, w.Header())
	}
	original := append([]byte{}, w.Body.Bytes()...)
	uploadedAsset(t, assetUpload(t, s, "/api/generator/assets/"+a.ID+"/revisions?kind=audio&version=1", "track.wav", musicFixture(t, "sine=frequency=880:sample_rate=48000", .5)))
	restarted := New(s.cfg, s.log)
	w = dashboardRequest(restarted, "GET", "/api/generator/assets/"+a.ID+"/revisions/1", "")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), original) {
		t.Fatal("audio revision changed across replacement/restart", w.Code)
	}
	var catalog []struct {
		Kind      string
		Revisions []struct {
			Samples    int64 `json:"samples"`
			SampleRate int   `json:"sample_rate"`
		}
	}
	json.Unmarshal(dashboardRequest(s, "GET", "/api/generator/assets", "").Body.Bytes(), &catalog)
	if catalog[0].Kind != "audio" || catalog[0].Revisions[0].Samples != 48000 || catalog[0].Revisions[0].SampleRate != 48000 {
		t.Fatal(catalog)
	}
	if w = assetUpload(t, s, "/api/generator/assets?kind=audio", "bad.wav", []byte("invalid")); w.Code != 422 {
		t.Fatal(w.Code)
	}
	if w = dashboardRequest(s, "DELETE", "/api/generator/assets/"+a.ID, `{}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestGeneratorMusicContinuesAcrossScenesAndEndsWithoutRepeat(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "music.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", 1)))
	body := `{"name":"Music show","stage":"ending","theme":{"id":"retro","revision":1},"scenes":[{"id":"one","layout":"title","text":"One","duration_seconds":0.6},{"id":"two","layout":"title","text":"Two","duration_seconds":1.4}],"soundtrack":{"asset":{"id":"` + a.ID + `","revision":1},"mode":"end","volume_percent":50,"fade_out_seconds":0.4}}`
	w := dashboardRequest(s, "POST", "/api/generator/designs", body)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var d mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &d)
	generatorServe(t, s)
	pcm := decodePCM(t, exactGeneratedVideo(t, s, d))
	if tonePower(pcm, .2, 440) < 1e9 || tonePower(pcm, .7, 440) < 1e8 || tonePower(pcm, 1.3, 440) > 1e5 {
		t.Fatal("music stopped at scene boundary or continued after track end")
	}
	if tonePower(pcm, .8, 440) >= tonePower(pcm, .65, 440) {
		t.Fatal("fade-out did not occur at actual track end")
	}
}

func saveMusicDesign(t *testing.T, s *Server, d mediaauthor.Design) mediaauthor.Design {
	t.Helper()
	data, _ := json.Marshal(d)
	w := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(data))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &d)
	return d
}
func musicVolume(v float64) *float64 { return &v }
func musicResult(t *testing.T, s *Server, d mediaauthor.Design) ([]float64, float64) {
	t.Helper()
	j := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if j.State != "ready" {
		t.Fatal(j)
	}
	var result struct {
		Gain *float64 `json:"mix_gain"`
	}
	json.Unmarshal(dashboardRequest(s, "GET", "/api/generator/jobs/"+j.ID, "").Body.Bytes(), &result)
	if result.Gain == nil {
		t.Fatal("missing disclosed mix gain")
	}
	w := dashboardRequest(s, "GET", "/api/library/revisions/"+j.MediaRevision+"/preview", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	path := filepath.Join(t.TempDir(), "mix.mp4")
	os.WriteFile(path, w.Body.Bytes(), 0600)
	return decodePCM(t, path), *result.Gain
}
func TestGeneratorMusicRepeatsWithOneEnvelopeAndIndependentVolume(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "music.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .4)))
	d := generatorDraft(t, s, 2)
	d.Scenes = append(d.Scenes, mediaauthor.Scene{ID: "second", Layout: "title", Text: "Second", DurationSeconds: 1})
	d.Scenes[0].DurationSeconds = 1
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "repeat", VolumePercent: musicVolume(100), FadeInSeconds: .2, FadeOutSeconds: .2}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	full, gain := musicResult(t, s, d)
	if gain != 1 {
		t.Fatal("unclipped music attenuated", gain)
	}
	for _, pos := range []float64{.3, .7, 1.1, 1.5} {
		if tonePower(full, pos, 440) < 1e10 {
			t.Fatal("repeat restarted envelope or music at scene boundary", pos)
		}
	}
	if tonePower(full, .02, 440) >= tonePower(full, .3, 440) || tonePower(full, 1.85, 440) >= tonePower(full, 1.5, 440) {
		t.Fatal("global fades missing")
	}
	d.Soundtrack.VolumePercent = musicVolume(50)
	d = saveMusicDesign(t, s, d)
	half, gain := musicResult(t, s, d)
	ratio := math.Sqrt(tonePower(half, .7, 440) / tonePower(full, .7, 440))
	if gain != 1 || ratio < .47 || ratio > .53 {
		t.Fatal("music volume changed unexpectedly", gain, ratio)
	}
}
func TestGeneratorMusicUsesOneStablePeakGainWithoutDelay(t *testing.T) {
	s := libraryServer(t)
	path := filepath.Join(t.TempDir(), "loud.mp4")
	args := []string{"-v", "error", "-f", "lavfi", "-i", "color=blue:size=320x180:rate=25:duration=2", "-f", "lavfi", "-i", "aevalsrc=0.95*sin(2*PI*440*t):s=48000:d=2", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", "-c:a", "aac", "-ac", "2", path}
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	data, _ := os.ReadFile(path)
	v := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "loud.mp4", data))
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "loud.wav", musicFixture(t, "aevalsrc=0.95*sin(2*PI*880*t):s=48000", 2)))
	d := videoDesign(t, s, v.ID, 2, 0, 2, 100, false, true)
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "end", VolumePercent: musicVolume(0)}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	base, gain := musicResult(t, s, d)
	if gain != 1 {
		t.Fatal("muted music changed video level", gain)
	}
	d.Soundtrack.VolumePercent = musicVolume(25)
	d = saveMusicDesign(t, s, d)
	quiet, quietGain := musicResult(t, s, d)
	if quietGain != 1 {
		t.Fatal("unclipped mix changed levels", quietGain)
	}
	if ratio := math.Sqrt(tonePower(quiet, .3, 440) / tonePower(base, .3, 440)); math.Abs(ratio-1) > .04 {
		t.Fatal("music altered requested video level", ratio)
	}
	d.Soundtrack.VolumePercent = musicVolume(100)
	d = saveMusicDesign(t, s, d)
	mixed, gain := musicResult(t, s, d)
	if gain <= .5 || gain >= .9 {
		t.Fatal("missing peak protection", gain)
	}
	for _, pos := range []float64{.3, 1.3} {
		ratio := math.Sqrt(tonePower(mixed, pos, 440) / tonePower(base, pos, 440))
		if math.Abs(ratio-gain) > .04 {
			t.Fatal("gain varied over mix", pos, ratio, gain)
		}
	}
	if ratio := math.Sqrt(tonePower(quiet, .3, 880) / tonePower(mixed, .3, 880)); math.Abs(ratio-.25/gain) > .04 {
		t.Fatal("music/video independent levels lost", ratio, gain)
	}
	// Correlation peaks at the same sample offset: gain protection adds no delay.
	best, bestLag := -math.MaxFloat64, 0
	for lag := -24; lag <= 24; lag++ {
		sum := 0.
		for i := 24000; i < 28800; i++ {
			sum += base[i] * mixed[i+lag]
		}
		if sum > best {
			best, bestLag = sum, lag
		}
	}
	if bestLag < -1 || bestLag > 1 {
		t.Fatal("mixer shifted audio", bestLag)
	}
}

func TestGeneratorMusicSnapshotsTemplatesRetriesAndUsesStayPinned(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "track.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .5)))
	d := generatorDraft(t, s, 1)
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "repeat", VolumePercent: musicVolume(40), FadeOutSeconds: .1}
	d = saveMusicDesign(t, s, d)
	captured := submitGenerator(t, s, d)
	templateData, _ := json.Marshal(ContentTemplate{Name: "Music template", Content: d})
	w := dashboardRequest(s, "POST", "/api/generator/templates", string(templateData))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var template ContentTemplate
	json.Unmarshal(w.Body.Bytes(), &template)
	w = dashboardRequest(s, "POST", "/api/generator/templates/"+template.ID+"/designs", `{"name":"Music copy","version":1,"theme":{"id":"retro","revision":1}}`)
	var copy mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &copy)
	if w.Code != 201 || !reflect.DeepEqual(copy.Soundtrack, d.Soundtrack) {
		t.Fatal("template lost music", w.Code, w.Body.String())
	}
	uploadedAsset(t, assetUpload(t, s, "/api/generator/assets/"+a.ID+"/revisions?kind=audio&version=1", "track.wav", musicFixture(t, "sine=frequency=880:sample_rate=48000", .5)))
	original := d.Soundtrack
	d.Soundtrack = nil
	saveMusicDesign(t, s, d)
	if w = dashboardRequest(s, "POST", "/api/generator/jobs/"+captured.ID+"/cancel", `{}`); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	restarted := New(s.cfg, s.log)
	w = dashboardRequest(restarted, "POST", "/api/generator/jobs/"+captured.ID+"/retry", `{}`)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	retry := generatorJob(t, w)
	if !reflect.DeepEqual(retry.Design.Soundtrack, original) || retry.DesignRevision != captured.DesignRevision {
		t.Fatal("retry changed pinned music")
	}
	w = dashboardRequest(restarted, "DELETE", "/api/generator/assets/"+a.ID, `{}`)
	if w.Code != 409 || !bytes.Contains(w.Body.Bytes(), []byte(`"kind":"template"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"kind":"job"`)) {
		t.Fatal(w.Code, w.Body.String())
	}
	generatorServe(t, restarted)
	ready := waitGeneratorJob(t, restarted, retry.ID)
	if ready.State != "ready" {
		t.Fatal(ready)
	}
	w = dashboardRequest(restarted, "GET", "/api/library/revisions/"+ready.MediaRevision+"/preview", "")
	path := filepath.Join(t.TempDir(), "retained.mp4")
	os.WriteFile(path, w.Body.Bytes(), 0600)
	pcm := decodePCM(t, path)
	if tonePower(pcm, .2, 440) < 100*tonePower(pcm, .2, 880) {
		t.Fatal("replacement changed captured music")
	}
}
func TestGeneratorMusicValidatesFadesAndMissingAssetsBeforeRendering(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "track.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .5)))
	d := generatorDraft(t, s, 2)
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "end", VolumePercent: musicVolume(101), FadeInSeconds: .3, FadeOutSeconds: .3}
	d = saveMusicDesign(t, s, d)
	payload, _ := json.Marshal(d)
	w := dashboardRequest(s, "POST", "/api/generator/validate", string(payload))
	for _, field := range []string{"soundtrack.volume_percent", "soundtrack.fade_out_seconds"} {
		if !bytes.Contains(w.Body.Bytes(), []byte(field)) {
			t.Fatal("missing actionable music validation", w.Body.String())
		}
	}
	if w = dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version)); w.Code != 422 {
		t.Fatal(w.Code)
	}
	d.Soundtrack.VolumePercent = musicVolume(100)
	d.Soundtrack.FadeInSeconds = 0
	d.Soundtrack.FadeOutSeconds = 0
	d = saveMusicDesign(t, s, d)
	if err := os.Remove(filepath.Join(s.cfg.BRB.Directory, "library", "generator", "assets", a.ID, "1.wav")); err != nil {
		t.Fatal(err)
	}
	if w = dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version)); w.Code != 422 || !bytes.Contains(w.Body.Bytes(), []byte("soundtrack.asset")) {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestGeneratorMusicFractionalRateHasOneContinuousTimeline(t *testing.T) {
	s := libraryServer(t)
	if w := assetRequest(t, s, map[string]string{"fps": "24", "sample_rate": "44100"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code)
	}
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "short.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .12345)))
	d := generatorDraft(t, s, 1.0/24)
	for i := 1; i < 20; i++ {
		scene := d.Scenes[0]
		scene.ID = fmt.Sprint(i)
		d.Scenes = append(d.Scenes, scene)
	}
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "repeat"}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	path := exactGeneratedVideo(t, s, d)
	raw, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-map", "0:a:0", "-ar", "44100", "-ac", "2", "-f", "s16le", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(raw) / 4; n < 36750-1024 || n > 36750+1024 {
		t.Fatal("music accumulated scene or repeat rounding", n)
	}
	pcm := decodePCM(t, path)
	for _, pos := range []float64{.15, .4, .65} {
		if tonePower(pcm, pos, 440) < 1e9 {
			t.Fatal("gap in short repeating music", pos)
		}
	}
}

func TestGeneratorMusicAcceptsMP3AndRejectsOversizeOrChangedBytes(t *testing.T) {
	s := libraryServer(t)
	path := filepath.Join(t.TempDir(), "track.mp3")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.5", "-c:a", "libmp3lame", path).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	data, _ := os.ReadFile(path)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "track.mp3", data))
	if w := assetUpload(t, s, "/api/generator/assets?kind=audio", "large.wav", make([]byte, (32<<20)+1)); w.Code != 422 {
		t.Fatal("oversize audio accepted", w.Code)
	}
	normalized := filepath.Join(s.cfg.BRB.Directory, "library", "generator", "assets", a.ID, "1.wav")
	data, _ = os.ReadFile(normalized)
	data[len(data)-1] ^= 1
	os.WriteFile(normalized, data, 0600)
	w := dashboardRequest(s, "GET", "/api/generator/assets/"+a.ID+"/revisions/1", "")
	if w.Code != 409 {
		t.Fatal("corrupt audio served", w.Code)
	}
}
func TestGeneratorMusicFailureKeepsPreviousReadyOutputAndFailedReferences(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "track.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", 1)))
	d := generatorDraft(t, s, 1)
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "end"}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	first := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if first.State != "ready" {
		t.Fatal(first)
	}
	path := filepath.Join(s.cfg.BRB.Directory, "library", "generator", "assets", a.ID, "1.wav")
	data, _ := os.ReadFile(path)
	data[len(data)-1] ^= 1
	os.WriteFile(path, data, 0600)
	failed := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if failed.State != "failed" || failed.MediaRevision != "" {
		t.Fatal("changed music rendered", failed)
	}
	d.Soundtrack = nil
	saveMusicDesign(t, s, d)
	uses := dashboardRequest(s, "DELETE", "/api/generator/assets/"+a.ID, `{}`)
	if uses.Code != 409 || !bytes.Contains(uses.Body.Bytes(), []byte(failed.ID)) {
		t.Fatal("failed job lost music reference", uses.Body.String())
	}
	if w := dashboardRequest(s, "GET", "/api/library/revisions/"+first.MediaRevision+"/preview", ""); w.Code != 200 {
		t.Fatal("failure lost previous prepared output", w.Code)
	}
}
func TestGeneratorMusicLongTrackFadesAtDesignEnd(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "long.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", 3)))
	d := generatorDraft(t, s, 1)
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "end", FadeOutSeconds: .4}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	pcm, _ := musicResult(t, s, d)
	if tonePower(pcm, .75, 440) >= tonePower(pcm, .35, 440)*.5 {
		t.Fatal("long-track fade waited for source end")
	}
}
