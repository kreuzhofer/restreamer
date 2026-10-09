package relay

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func generatorVideoFixture(t *testing.T, audio bool) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.mp4")
	args := []string{"-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=25:duration=2"}
	if audio {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000:duration=2", "-c:a", "aac", "-ac", "2")
	}
	args = append(args, "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", "-movflags", "+faststart", path)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("video fixture %v %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestGeneratorVideoAssetsRetainMetadataAndImmutableRevisions(t *testing.T) {
	s := libraryServer(t)
	data := generatorVideoFixture(t, true)
	w := assetUpload(t, s, "/api/generator/assets?kind=video", "source.mp4", data)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var a struct {
		ID, Kind  string
		Revision  int
		Revisions []struct {
			Duration      float64 `json:"duration_seconds"`
			Audio         bool    `json:"has_audio"`
			Width, Height int
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if a.Kind != "video" || a.Revision != 1 || len(a.Revisions) != 1 || a.Revisions[0].Duration != 2 || !a.Revisions[0].Audio || a.Revisions[0].Width != 320 {
		t.Fatalf("video metadata %+v", a)
	}
	original := dashboardRequest(s, "GET", "/api/generator/assets/"+a.ID+"/revisions/1", "")
	if original.Code != 200 || original.Header().Get("Content-Type") != "video/mp4" {
		t.Fatal(original.Code, original.Header())
	}
	replacement := assetUpload(t, s, "/api/generator/assets/"+a.ID+"/revisions?version=1&kind=video", "source.mp4", generatorVideoFixture(t, false))
	if replacement.Code != 201 {
		t.Fatal(replacement.Code, replacement.Body.String())
	}
	restarted := New(s.cfg, s.log)
	recovered := dashboardRequest(restarted, "GET", "/api/generator/assets/"+a.ID+"/revisions/1", "")
	if recovered.Code != 200 || string(recovered.Body.Bytes()) != string(original.Body.Bytes()) {
		t.Fatal("replacement or restart mutated video")
	}
	if malformed := assetUpload(t, s, "/api/generator/assets?kind=video", "bad.mp4", []byte("not a video")); malformed.Code != 422 {
		t.Fatal(malformed.Code)
	}
}

func TestGeneratorVideoSceneTimingMuteAndReferenceRetention(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "source.mp4", generatorVideoFixture(t, true)))
	body := fmt.Sprintf(`{"name":"Video show","stage":"prestream","theme":{"id":"retro","revision":1},"scenes":[{"id":"video","layout":"media","media_kind":"video","text":"Retained inactive title","duration_seconds":2,"video":{"asset":{"id":%q,"revision":1},"trim_start_seconds":0.4,"trim_end_seconds":1.2,"repeat":true}}]}`, a.ID)
	w := dashboardRequest(s, "POST", "/api/generator/designs", body)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var d mediaauthor.Design
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if w := dashboardRequest(s, "DELETE", "/api/generator/assets/"+a.ID, `{}`); w.Code != 409 {
		t.Fatal("video reference lost", w.Code)
	}
	if w := dashboardRequest(s, "POST", "/api/generator/preview", w.Body.String()); w.Code != 200 {
		t.Fatal("video scene quick preview", w.Code, w.Body.String())
	}
	generatorServe(t, s)
	job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if job.State != "ready" || job.Duration != 2 {
		t.Fatal(job)
	}
	preview := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
	if preview.Code != 200 {
		t.Fatal(preview.Code)
	}
	path := filepath.Join(t.TempDir(), "exact.mp4")
	if err := os.WriteFile(path, preview.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	pcm, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-map", "0:a:0", "-f", "s16le", "-ac", "2", "-ar", "48000", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range pcm {
		if b != 0 {
			t.Fatal("source audio was not muted by default")
		}
	}
	frames, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-count_frames", "-show_entries", "stream=nb_read_frames", "-of", "csv=p=0", path).Output()
	if err != nil || strings.TrimSpace(string(frames)) != "50" {
		t.Fatal("video timing", err, string(frames))
	}
}

func generatorSplitVideo(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "split.mp4")
	args := []string{"-v", "error", "-f", "lavfi", "-i", "color=red:size=320x180:rate=25:duration=1", "-f", "lavfi", "-i", "color=blue:size=320x180:rate=25:duration=1", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=1", "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000:duration=1", "-filter_complex_threads", "1", "-filter_complex", "[0:v][1:v]concat=n=2:v=1:a=0[v];[2:a][3:a]concat=n=2:v=0:a=1[a]", "-map", "[v]", "-map", "[a]", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", "-c:a", "aac", "-ac", "2", path}
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("split fixture %v %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func videoDesign(t *testing.T, s *Server, id string, seconds, start, end, volume float64, repeat, audio bool) mediaauthor.Design {
	t.Helper()
	body := fmt.Sprintf(`{"name":"Video boundaries","stage":"ending","theme":{"id":"retro","revision":1},"scenes":[{"id":"video","layout":"media","media_kind":"video","text":"","duration_seconds":%g,"video":{"asset":{"id":%q,"revision":1},"trim_start_seconds":%g,"trim_end_seconds":%g,"repeat":%t,"audio_enabled":%t,"audio_volume_percent":%g}}]}`, seconds, id, start, end, repeat, audio, volume)
	w := dashboardRequest(s, "POST", "/api/generator/designs", body)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var d mediaauthor.Design
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}
func exactGeneratedVideo(t *testing.T, s *Server, d mediaauthor.Design) string {
	t.Helper()
	j := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if j.State != "ready" {
		t.Fatal(j)
	}
	w := dashboardRequest(s, "GET", "/api/library/revisions/"+j.MediaRevision+"/preview", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	path := filepath.Join(t.TempDir(), "exact.mp4")
	if err := os.WriteFile(path, w.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func decodePCM(t *testing.T, path string) []float64 {
	t.Helper()
	data, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-map", "0:a:0", "-ac", "1", "-ar", "48000", "-f", "s16le", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float64, len(data)/2)
	for i := range out {
		out[i] = float64(int16(binary.LittleEndian.Uint16(data[i*2:])))
	}
	return out
}
func tonePower(data []float64, start float64, hz float64) float64 {
	real, imag := 0.0, 0.0
	for i := 0; i < 4800; i++ {
		sample := data[int(start*48000)+i]
		phase := 2 * math.Pi * hz * float64(i) / 48000
		real += sample * math.Cos(phase)
		imag += sample * math.Sin(phase)
	}
	return real*real + imag*imag
}
func TestGeneratorVideoRepeatsOnlyTrimmedFramesAndAudio(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "split.mp4", generatorSplitVideo(t)))
	generatorServe(t, s)
	d := videoDesign(t, s, a.ID, 2, .6, 1.4, 100, true, true)
	path := exactGeneratedVideo(t, s, d)
	rgb, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-map", "0:v:0", "-f", "rawvideo", "-pix_fmt", "rgb24", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	size := 320 * 180 * 3
	if len(rgb) != 50*size {
		t.Fatal("repeated frame count", len(rgb)/size)
	}
	for _, frame := range []int{0, 9, 20, 29, 40, 49} {
		if rgb[frame*size] < 200 || rgb[frame*size+2] > 30 {
			t.Fatalf("frame%d did not repeat selected red portion", frame)
		}
	}
	for _, frame := range []int{10, 19, 30, 39} {
		if rgb[frame*size+2] < 200 || rgb[frame*size] > 30 {
			t.Fatalf("frame%d did not repeat selected blue portion", frame)
		}
	}
	pcm := decodePCM(t, path)
	for _, point := range []struct{ time, hz float64 }{{.1, 440}, {.5, 880}, {.9, 440}, {1.3, 880}, {1.7, 440}} {
		other := 440.0
		if point.hz == 440 {
			other = 880
		}
		if tonePower(pcm, point.time, point.hz) < tonePower(pcm, point.time, other)*10 {
			t.Fatalf("wrong source audio at%g", point.time)
		}
	}
	quieter := videoDesign(t, s, a.ID, 2, .6, 1.4, 50, true, true)
	quiet := decodePCM(t, exactGeneratedVideo(t, s, quieter))
	ratio := math.Sqrt(tonePower(quiet, .1, 440) / tonePower(pcm, .1, 440))
	if ratio < .46 || ratio > .54 {
		t.Fatalf("independent source volume ratio%g", ratio)
	}
}
func TestGeneratorVideoRejectsInvalidRangesAndMissingAudio(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "silent.mp4", generatorVideoFixture(t, false)))
	for _, tc := range []struct {
		seconds, start, end float64
		repeat, audio       bool
		field               string
	}{{2, .4, 1.2, false, false, "duration_seconds"}, {1, -1, 1, false, false, "video.trim"}, {1, 0, 3, false, false, "video.trim"}, {1, 1, 1, true, false, "video.trim"}, {1, 0, 2, false, true, "video.audio_enabled"}} {
		d := videoDesign(t, s, a.ID, tc.seconds, tc.start, tc.end, 100, tc.repeat, tc.audio)
		data, _ := json.Marshal(d)
		validation := dashboardRequest(s, "POST", "/api/generator/validate", string(data))
		if validation.Code != 200 || !strings.Contains(validation.Body.String(), tc.field) {
			t.Fatal("missing timing/audio issue", validation.Code, validation.Body.String())
		}
		w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":1}`, d.ID))
		if w.Code != 422 {
			t.Fatal("invalid video admitted", w.Code, w.Body.String())
		}
	}
}

func TestGeneratorVideoTemplatesAndRetriesKeepCapturedSettings(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "split.mp4", generatorSplitVideo(t)))
	d := videoDesign(t, s, a.ID, 2, .6, 1.4, 35, true, true)
	data, _ := json.Marshal(map[string]any{"name": "Video template", "content": d})
	w := dashboardRequest(s, "POST", "/api/generator/templates", string(data))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var template ContentTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &template); err != nil {
		t.Fatal(err)
	}
	w = dashboardRequest(s, "POST", "/api/generator/templates/"+template.ID+"/designs", `{"name":"Video copy","version":1,"theme":{"id":"retro","revision":1}}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var copied mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &copied)
	if !reflect.DeepEqual(copied.Scenes[0].Video, d.Scenes[0].Video) || copied.Scenes[0].MediaKind != "video" {
		t.Fatal("template video settings lost", copied)
	}
	original := submitGenerator(t, s, copied)
	if w := dashboardRequest(s, "POST", "/api/generator/jobs/"+original.ID+"/cancel", `{}`); w.Code != 202 {
		t.Fatal(w.Code)
	}
	if w := assetUpload(t, s, "/api/generator/assets/"+a.ID+"/revisions?version=1&kind=video", "replacement.mp4", generatorVideoFixture(t, false)); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	copied.Scenes[0].Video.Asset.Revision = 2
	copied.Scenes[0].Video.AudioEnabled = false
	copied.Scenes[0].Layout = "title"
	copied.Scenes[0].Text = "Inactive video reference retained"
	data, _ = json.Marshal(copied)
	if w := dashboardRequest(s, "PUT", "/api/generator/designs/"+copied.ID, string(data)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	restarted := New(s.cfg, s.log)
	w = dashboardRequest(restarted, "POST", "/api/generator/jobs/"+original.ID+"/retry", `{}`)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	retry := generatorJob(t, w)
	if !reflect.DeepEqual(retry.Design.Scenes[0].Video, d.Scenes[0].Video) || retry.DesignRevision != original.DesignRevision || retry.Design.Scenes[0].MediaKind != "video" {
		t.Fatal("retry substituted newer video settings", retry)
	}
	uses := dashboardRequest(restarted, "DELETE", "/api/generator/assets/"+a.ID, `{}`)
	if uses.Code != 409 || !strings.Contains(uses.Body.String(), `"kind":"template"`) || !strings.Contains(uses.Body.String(), `"kind":"job"`) {
		t.Fatal("video uses lost after restart", uses.Code, uses.Body.String())
	}
	generatorServe(t, restarted)
	if ready := waitGeneratorJob(t, restarted, retry.ID); ready.State != "ready" {
		t.Fatal(ready)
	}
}

func TestGeneratorVideoAudioRemainsAlignedAt44100Hz24FPS(t *testing.T) {
	s := libraryServer(t)
	if w := assetRequest(t, s, map[string]string{"fps": "24", "sample_rate": "44100"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "tone.mp4", generatorVideoFixture(t, true)))
	d := videoDesign(t, s, a.ID, 1.0/24, 0, 1, 100, false, true)
	for i := 1; i < 20; i++ {
		scene := d.Scenes[0]
		scene.ID = fmt.Sprintf("video-%d", i)
		d.Scenes = append(d.Scenes, scene)
	}
	data, _ := json.Marshal(d)
	w := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(data))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &d)
	generatorServe(t, s)
	path := exactGeneratedVideo(t, s, d)
	frames, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-count_frames", "-show_entries", "stream=nb_read_frames", "-of", "csv=p=0", path).Output()
	if err != nil || strings.TrimSpace(string(frames)) != "20" {
		t.Fatal("short video frames lost", err, string(frames))
	}
	pcm, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-map", "0:a:0", "-ac", "1", "-ar", "44100", "-f", "s16le", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	// 20/24 seconds is 36750 samples, plus at most one final AAC packet of
	// padding. Encoding each one-frame scene as AAC would add many packets.
	samples := len(pcm) / 2
	if samples < 36750-1024 || samples > 36750+1024 {
		t.Fatalf("audio accumulated per-scene padding: %d samples", samples)
	}
	var energy float64
	for i := 0; i+1 < len(pcm); i += 2 {
		n := float64(int16(binary.LittleEndian.Uint16(pcm[i:])))
		energy += n * n
	}
	if energy/float64(samples) < 10000 {
		t.Fatal("enabled source audio was lost")
	}
}

func TestGeneratorVideoRoutesRejectTamperingAndUnauthenticatedReads(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "source.mp4", generatorVideoFixture(t, true)))
	path := "/api/generator/assets/" + a.ID + "/revisions/1"
	r := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("video exposed without authentication", w.Code)
	}
	r = httptest.NewRequest("POST", "/api/generator/assets?kind=video", bytes.NewReader(nil))
	r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	r.Header.Set("Origin", "https://untrusted.example")
	r.Header.Set("Content-Type", "multipart/form-data; boundary=fixture")
	r.Header.Set("X-Restreamer-Control", "1")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin video upload", w.Code)
	}
	d := videoDesign(t, s, a.ID, 1, 0, 2, 100, false, true)
	disk := filepath.Join(s.cfg.BRB.Directory, "library", "generator", "assets", a.ID, "1.mp4")
	data, err := os.ReadFile(disk)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 1
	if err := os.WriteFile(disk, data, 0600); err != nil {
		t.Fatal(err)
	}
	if w := dashboardRequest(s, "GET", path, ""); w.Code != 409 {
		t.Fatal("changed video was previewed", w.Code)
	}
	generatorServe(t, s)
	job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if job.State != "failed" || job.MediaRevision != "" {
		t.Fatal("changed video was rendered", job)
	}
}

func TestGeneratorVideoUploadRejectsUnsupportedBounds(t *testing.T) {
	s := libraryServer(t)
	for _, tc := range []struct{ name, source, codec string }{
		{"height", "color=size=320x1082:rate=25:duration=0.08", "libx264"},
		{"rate", "color=size=320x180:rate=61:duration=0.08", "libx264"},
		{"codec", "color=size=320x180:rate=25:duration=0.08", "mpeg4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "unsupported.mp4")
			if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", tc.source, "-c:v", tc.codec, "-threads", "2", path).CombinedOutput(); err != nil {
				t.Fatal(err, string(out))
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			w := assetUpload(t, s, "/api/generator/assets?kind=video", "unsupported.mp4", data)
			if w.Code != 422 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	w := dashboardRequest(s, "GET", "/api/generator/assets", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal("invalid uploads were retained", w.Code, w.Body.String())
	}
}
