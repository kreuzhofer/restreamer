package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGeneratedRevisionKeepsExactFrameDurationAfterRestart(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, 1.2)
	generatorServe(t, s)
	job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if job.State != "ready" {
		t.Fatal(job)
	}
	for _, server := range []*Server{s, New(s.cfg, s.log)} {
		w := dashboardRequest(server, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		path := filepath.Join(t.TempDir(), "exact.mp4")
		if err := os.WriteFile(path, w.Body.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		raw, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=duration", "-of", "csv=p=0", path).Output()
		duration, _ := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
		if err != nil || math.Abs(duration-1.2) > .001 {
			t.Fatalf("30frames at25fps must end at1.2s before and after restart, got%g: %v", duration, err)
		}
	}
}

func TestGeneratedCyclePreviewContainsTwoExactPassesWithoutActivation(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, .12)
	generatorServe(t, s)
	job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if job.State != "ready" {
		t.Fatal(job)
	}
	rawJob := dashboardRequest(s, "GET", "/api/generator/jobs/"+job.ID, "").Body.String()
	if !strings.Contains(rawJob, `"exact_timing":true`) {
		t.Fatal("ready job does not advertise exact preview timing", rawJob)
	}
	before := dashboardRequest(s, "GET", "/api/stage", "").Body.String()
	w := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview?passes=2", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	path := filepath.Join(t.TempDir(), "two.mp4")
	os.WriteFile(path, w.Body.Bytes(), 0600)
	raw, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-count_frames", "-show_entries", "stream=nb_read_frames,duration", "-of", "json", path).Output()
	var decoded struct {
		Streams []struct {
			Frames   string `json:"nb_read_frames"`
			Duration string `json:"duration"`
		}
	}
	json.Unmarshal(raw, &decoded)
	if err != nil || len(decoded.Streams) != 1 || decoded.Streams[0].Frames != "6" || decoded.Streams[0].Duration != "0.240000" {
		t.Fatal("two pass preview did not repeat exact3frame cycle", err, string(raw))
	}
	if after := dashboardRequest(s, "GET", "/api/stage", "").Body.String(); after != before {
		t.Fatal("candidate preview changed stage", before, after)
	}
	if w := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview?passes=3", ""); w.Code != 400 {
		t.Fatal("unbounded preview passes accepted", w.Code)
	}
}

func TestPrestreamBoundaryCrossfadeRotatesCompleteVideoCycle(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "split.mp4", generatorSplitVideo(t)))
	body := fmt.Sprintf(`{"name":"Circular video","stage":"prestream","theme":{"id":"retro","revision":1},"loop_transition":{"kind":"crossfade","duration_seconds":0.4},"scenes":[{"id":"video","layout":"media","media_kind":"video","text":"","duration_seconds":2,"video":{"asset":{"id":%q,"revision":1},"audio_enabled":true}}]}`, a.ID)
	w := dashboardRequest(s, "POST", "/api/generator/designs", body)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var d mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &d)
	generatorServe(t, s)
	job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if job.State != "ready" || job.Duration != 1.6 {
		t.Fatal("boundary overlap duration", job)
	}
	w = dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview?passes=2", "")
	path := filepath.Join(t.TempDir(), "cycle.mp4")
	os.WriteFile(path, w.Body.Bytes(), 0600)
	pixels, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-map", "0:v", "-vsync", "0", "-pix_fmt", "rgb24", "-f", "rawvideo", "-").Output()
	if err != nil || len(pixels) != 80*320*180*3 {
		t.Fatal("expected two40frame cycles", err, len(pixels))
	}
	pixel := func(frame int) []byte { at := frame*320*180*3 + (90*320+160)*3; return pixels[at : at+3] }
	for _, sample := range []struct {
		frame int
		r, b  byte
	}{{0, 252, 0}, {14, 252, 0}, {15, 0, 253}, {30, 0, 253}, {35, 126, 126}, {39, 226, 25}, {40, 252, 0}, {75, 126, 126}} {
		got := pixel(sample.frame)
		if math.Abs(float64(got[0])-float64(sample.r)) > 12 || math.Abs(float64(got[2])-float64(sample.b)) > 12 {
			t.Fatalf("frame%d did not blend true tail/head: %v", sample.frame, got)
		}
	}
	pcm := decodePCM(t, path)
	if tonePower(pcm, .15, 440) < 1e10 || tonePower(pcm, .8, 880) < 1e10 || tonePower(pcm, 1.35, 440) < 1e9 || tonePower(pcm, 1.35, 880) < 1e9 || tonePower(pcm, 1.75, 440) < 1e10 {
		t.Fatal("full-cycle source audio did not follow circular video transition")
	}
}

func TestPrestreamCyclicAudioAvoidsRepeatedEncoderStartupSilence(t *testing.T) {
	for _, seconds := range []float64{1.28, 1.2} {
		t.Run(fmt.Sprint(seconds), func(t *testing.T) {
			s := libraryServer(t)
			a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "tone.wav", musicFixture(t, "sine=frequency=437.5:sample_rate=48000", seconds)))
			d := generatorDraft(t, s, seconds)
			d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "repeat"}
			d = saveMusicDesign(t, s, d)
			generatorServe(t, s)
			job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
			if job.State != "ready" {
				t.Fatal(job)
			}
			w := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview?passes=2", "")
			path := filepath.Join(t.TempDir(), "warm.mp4")
			os.WriteFile(path, w.Body.Bytes(), 0600)
			pcm := decodePCM(t, path)
			rms := func(start float64) float64 {
				var sum float64
				for _, v := range pcm[int(start*48000) : int(start*48000)+480] {
					sum += v * v
				}
				return math.Sqrt(sum / 480)
			}
			before, after := rms(seconds-.03), rms(seconds)
			if before < 1000 || after < before*.7 {
				t.Fatalf("recurring AAC startup silence at cycle boundary: before%.1f after%.1f", before, after)
			}
		})
	}
}

func TestGeneratedPrestreamLoopsKeepFrameEpochAcrossDelayedTicks(t *testing.T) {
	prepared := libraryServer(t)
	if w := assetRequest(t, prepared, map[string]string{"fps": "30"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	d := generatorDraft(t, prepared, 1.0/30)
	generatorServe(t, prepared)
	job := waitGeneratorJob(t, prepared, submitGenerator(t, prepared, d).ID)
	if job.State != "ready" {
		t.Fatal(job)
	}
	// Reopened controller uses the existing timed-tick seam without a live ticker.
	s := New(prepared.cfg, prepared.log)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+job.MediaRevision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	sub := s.broadcast.hub.subscribe()
	defer s.broadcast.hub.unsubscribe(sub)
	epoch := s.broadcast.playbackStatus(time.Now()).Epoch
	start := time.Now()
	var stamps []time.Duration
	for elapsed := time.Duration(0); elapsed <= 4*time.Second; elapsed += 7 * time.Millisecond {
		s.broadcast.tick(start.Add(elapsed))
		if elapsed == 0 {
			epoch = s.broadcast.playbackStatus(start).Epoch
		}
		for len(sub.packets) > 0 {
			m := <-sub.packets
			s.broadcast.hub.consumed(sub, m)
			if isVideoFrame(m) {
				stamps = append(stamps, m.Timestamp)
			}
		}
	}
	if len(stamps) < 120 {
		t.Fatalf("loop accumulated scheduling gaps: %dframes in4seconds", len(stamps))
	}
	for i, stamp := range stamps {
		expected := time.Duration(i) * time.Second / 30
		if delta := stamp - stamps[0] - expected; delta < -time.Millisecond || delta > time.Millisecond {
			t.Fatalf("frame%d drifted by%s", i, delta)
		}
	}
	if s.broadcast.playbackStatus(time.Now()).Epoch != epoch {
		t.Fatal("ordinary cycle continuation reset broadcast preview")
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestGeneratedTimingDedupPreservesExistingReadersAndOrdinaryRevisions(t *testing.T) {
	for _, ordinary := range []bool{false, true} {
		t.Run(fmt.Sprint(ordinary), func(t *testing.T) {
			prepared := libraryServer(t)
			d := generatorDraft(t, prepared, 1.2)
			generatorServe(t, prepared)
			original := waitGeneratorJob(t, prepared, submitGenerator(t, prepared, d).ID)
			if original.State != "ready" {
				t.Fatal(original)
			}
			// A persisted older-release catalog is a system-boundary migration fixture.
			path := filepath.Join(prepared.cfg.BRB.Directory, "library", "revisions", original.MediaRevision+".json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var legacy map[string]any
			json.Unmarshal(raw, &legacy)
			delete(legacy, "timing")
			legacy["duration"] = 1.235
			if ordinary {
				legacy["library_id"] = "ordinary-import"
			}
			raw, _ = json.Marshal(legacy)
			if os.WriteFile(path, raw, 0600) != nil {
				t.Fatal("fixture write")
			}
			s := New(prepared.cfg, prepared.log)
			if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+original.MediaRevision+`","shortcuts":[]}`); w.Code != 204 {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			oldDuration := readStage(t, s).Playback.Duration
			generatorServe(t, s)
			newJob := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
			if ordinary {
				if newJob.State != "failed" || !strings.Contains(newJob.Error, "ordinary library revision") {
					t.Fatal("ordinary timing changed silently", newJob)
				}
			} else if newJob.State != "ready" || newJob.MediaRevision != original.MediaRevision {
				t.Fatal("legacy generated revision was not upgraded", newJob)
			}
			if readStage(t, s).Playback.Duration != oldDuration {
				t.Fatal("dedup mutated already-open reader timing")
			}
			raw, _ = os.ReadFile(path)
			var current map[string]any
			json.Unmarshal(raw, &current)
			if ordinary && current["timing"] != nil || !ordinary && current["timing"] == nil {
				t.Fatal("persisted timing policy", string(raw))
			}
		})
	}
}

func TestPrestreamLoopTimingValidationRetainsEditableChoices(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, 1)
	for _, tc := range []struct {
		kind     string
		duration float64
		field    string
	}{{"wipe", .2, "kind"}, {"cut", .2, "duration_seconds"}, {"crossfade", .001, "duration_seconds"}, {"crossfade", -.1, "duration_seconds"}, {"crossfade", .6, "duration_seconds"}} {
		d.LoopTransition = &mediaauthor.Transition{Kind: tc.kind, DurationSeconds: tc.duration}
		d = saveMusicDesign(t, s, d)
		raw, _ := json.Marshal(d)
		w := dashboardRequest(s, "POST", "/api/generator/validate", string(raw))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "loop_transition."+tc.field) {
			t.Fatal(w.Code, w.Body.String())
		}
		w = dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version))
		if w.Code != 422 {
			t.Fatal("accepted invalid boundary", w.Code, w.Body.String())
		}
	}
	d.LoopTransition = &mediaauthor.Transition{Kind: "crossfade", DurationSeconds: .4}
	d.Scenes = append(d.Scenes, mediaauthor.Scene{ID: "last", Layout: "title", Text: "Last", DurationSeconds: 1})
	d.Scenes[0].Transition = &mediaauthor.Transition{Kind: "crossfade", DurationSeconds: .8}
	raw, _ := json.Marshal(d)
	w := dashboardRequest(s, "POST", "/api/generator/validate", string(raw))
	if !strings.Contains(w.Body.String(), "loop_transition.duration_seconds") {
		t.Fatal("boundary ignored incoming/outgoing overlap", w.Body.String())
	}
}

func TestGeneratedPrestreamLocalRTMPKeepsVideoClockAndAudibleCycles(t *testing.T) {
	prepared := libraryServer(t)
	music := uploadedAsset(t, assetUpload(t, prepared, "/api/generator/assets?kind=audio", "tone.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .4)))
	d := generatorDraft(t, prepared, .4)
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: music.ID, Revision: 1}, Mode: "repeat"}
	d = saveMusicDesign(t, prepared, d)
	generatorServe(t, prepared)
	job := waitGeneratorJob(t, prepared, submitGenerator(t, prepared, d).ID)
	if job.State != "ready" {
		t.Fatal(job)
	}
	dest := newSink(t)
	cfg := prepared.cfg
	cfg.Targets = []config.Target{dest.target("local")}
	s := New(cfg, prepared.log)
	generatorServe(t, s)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+job.MediaRevision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "real"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var packets []*rtmp.Message
	var stamps []time.Duration
	deadline := time.After(5 * time.Second)
	for len(stamps) < 40 {
		select {
		case m := <-dest.packets:
			packets = append(packets, m)
			if isVideoFrame(m) {
				stamps = append(stamps, m.Timestamp)
			}
		case <-deadline:
			t.Fatal("local RTMP did not deliver four cycles")
		}
	}
	for i, v := range stamps {
		if v-stamps[0] != time.Duration(i)*40*time.Millisecond {
			t.Fatalf("RTMP frame%d drift: %s", i, v-stamps[0])
		}
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	path := filepath.Join(t.TempDir(), "sink.flv")
	writeTestFLV(t, path, packets)
	pcm := decodePCM(t, path)
	for _, at := range []float64{.1, .5, .9, 1.3} {
		if tonePower(pcm, at, 440) < 1e10 {
			t.Fatalf("cycle at%gs lost audible audio", at)
		}
	}
}

func TestPrestreamBoundaryKeepsMusicEndWithinEveryCycle(t *testing.T) {
	for _, kind := range []string{"cut", "crossfade"} {
		t.Run(kind, func(t *testing.T) {
			s := libraryServer(t)
			a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "short.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .6)))
			d := generatorDraft(t, s, 1.6)
			d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "end"}
			d.LoopTransition = &mediaauthor.Transition{Kind: kind}
			if kind == "crossfade" {
				d.LoopTransition.DurationSeconds = .4
			}
			d = saveMusicDesign(t, s, d)
			generatorServe(t, s)
			job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
			if job.State != "ready" {
				t.Fatal(job)
			}
			w := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview?passes=2", "")
			path := filepath.Join(t.TempDir(), "music-end.mp4")
			os.WriteFile(path, w.Body.Bytes(), 0600)
			pcm := decodePCM(t, path)
			for _, offset := range []float64{0, job.Duration} {
				if tonePower(pcm, offset+.05, 440) < 1e9 || tonePower(pcm, offset+.65, 440) > 1e6 {
					t.Fatal("music end/restart shifted relative to cycle", kind, offset)
				}
			}
		})
	}
}

func TestGeneratedRevisionRejectsConflictingSavedTiming(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, .4)
	generatorServe(t, s)
	job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if job.State != "ready" {
		t.Fatal(job)
	}
	path := filepath.Join(s.cfg.BRB.Directory, "library", "revisions", job.MediaRevision+".json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"duration", "frames", "version"} {
		t.Run(field, func(t *testing.T) {
			var data map[string]any
			json.Unmarshal(original, &data)
			switch field {
			case "duration":
				data["duration"] = 3
			case "frames":
				data["timing"].(map[string]any)["frames"] = 11
			case "version":
				data["timing"].(map[string]any)["version"] = 99
			}
			raw, _ := json.Marshal(data)
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			restarted := New(s.cfg, s.log)
			w := dashboardRequest(restarted, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
			if w.Code == 200 {
				t.Fatal("conflicting generated timing served", field)
			}
		})
	}
}

func TestOrdinaryImportPreservesIdenticalGeneratedRevisionTiming(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, .4)
	generatorServe(t, s)
	job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if job.State != "ready" {
		t.Fatal(job)
	}
	previewURL := "/api/library/revisions/" + job.MediaRevision + "/preview?passes=2"
	before := dashboardRequest(s, "GET", previewURL, "")
	if before.Code != 200 {
		t.Fatal(before.Code)
	}
	// At the external encoder boundary, return an existing valid encoded output.
	// This makes an otherwise codec-version-dependent same-byte collision deterministic.
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := t.TempDir()
	source := filepath.Join(s.cfg.BRB.Directory, "library", "revisions", job.MediaRevision+".flv")
	script := fmt.Sprintf("#!/usr/bin/python3\nimport os,sys,shutil\nif os.path.basename(sys.argv[-1]).startswith('.prepare-'):\n shutil.copyfile(%q,sys.argv[-1])\nelse:\n os.execv(%q,[%q]+sys.argv[1:])\n", source, ffmpeg, ffmpeg)
	if err := os.WriteFile(filepath.Join(wrapper, "ffmpeg"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", wrapper+string(os.PathListSeparator)+os.Getenv("PATH"))
	single := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
	if w := videoUpload(t, s, "same-encoded-content.mp4", single.Body.Bytes()); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if got := readyHTTPRevision(t, s); got != job.MediaRevision {
		t.Fatal("fixture did not retain identical bytes", got, job.MediaRevision)
	}
	for _, server := range []*Server{s, New(s.cfg, s.log)} {
		after := dashboardRequest(server, "GET", previewURL, "")
		if after.Code != 200 || !bytes.Equal(before.Body.Bytes(), after.Body.Bytes()) {
			t.Fatal("ordinary import replaced exact generated timing", after.Code)
		}
	}
}
