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
