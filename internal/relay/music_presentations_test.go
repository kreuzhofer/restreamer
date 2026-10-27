package relay

import (
	"encoding/json"
	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func TestMusicPresentationUsesDecodedDurationAndReusesVisuals(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "track.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", 1.01)))
	d := generatorDraft(t, s, .6)
	d.DurationMode = "music"
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "repeat"}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	first := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if first.State != "ready" {
		t.Fatal(first.Error)
	}
	if first.Duration != 1.04 {
		t.Fatalf("follow music duration = %g, want 26 frames / 25 fps", first.Duration)
	}
	var original struct {
		VisualRevision string `json:"visual_revision"`
	}
	json.Unmarshal(dashboardRequest(s, "GET", "/api/generator/jobs/"+first.ID, "").Body.Bytes(), &original)
	if original.VisualRevision == "" {
		t.Fatal("no reusable visual media")
	}
	d.Soundtrack.VolumePercent = musicVolume(50)
	d = saveMusicDesign(t, s, d)
	second := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if second.State != "ready" {
		t.Fatal(second.Error)
	}
	var changed struct {
		VisualRevision string `json:"visual_revision"`
		ReusedVisual   bool   `json:"reused_visual"`
	}
	json.Unmarshal(dashboardRequest(s, "GET", "/api/generator/jobs/"+second.ID, "").Body.Bytes(), &changed)
	if changed.VisualRevision != original.VisualRevision || !changed.ReusedVisual {
		t.Fatalf("music-only change re-rendered video: %+v vs %+v", changed, original)
	}
	if second.MediaRevision == first.MediaRevision {
		t.Fatal("music change did not produce an exact presentation revision")
	}
	if s.broadcast.control.stage != "OFF" {
		t.Fatal("preparation activated playback")
	}
}

func TestMusicDrivenEndingKeepsIndependentClocksAndRequiresStop(t *testing.T) {
	prepared := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, prepared, "/api/generator/assets?kind=audio", "music.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .51)))
	d := generatorDraft(t, prepared, .28)
	d.Stage = "ending"
	d.EndingFadeSeconds = musicVolume(0)
	d.DurationMode = "music"
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "repeat"}
	d = saveMusicDesign(t, prepared, d)
	generatorServe(t, prepared)
	j := waitGeneratorJob(t, prepared, submitGenerator(t, prepared, d).ID)
	if j.State != "ready" {
		t.Fatal(j.Error)
	}
	s := New(prepared.cfg, prepared.log)
	for _, req := range []struct{ method, path, body string }{
		{"PUT", "/api/stage-media", `{"prestream":"` + j.MediaRevision + `","ending":"` + j.MediaRevision + `","shortcuts":[]}`},
	} {
		if w := dashboardRequest(s, req.method, req.path, req.body); w.Code != 204 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "end_stream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	defer stageRequest(t, s, "stop_now", nil)
	sub := s.broadcast.hub.subscribe()
	defer s.broadcast.hub.unsubscribe(sub)
	start := time.Now()
	var video, audio []time.Duration
	for elapsed := time.Duration(0); elapsed < 2200*time.Millisecond; elapsed += 10 * time.Millisecond {
		s.broadcast.tick(start.Add(elapsed))
		for len(sub.packets) > 0 {
			m := <-sub.packets
			s.broadcast.hub.consumed(sub, m)
			if len(m.Body) > 1 && m.Body[1] == 1 {
				if m.Type == rtmp.Video {
					video = append(video, m.Timestamp)
				} else if m.Type == rtmp.Audio {
					audio = append(audio, m.Timestamp)
				}
			}
		}
		time.Sleep(time.Millisecond)
	}
	if st := readStage(t, s); st.Stage != "ENDING" || st.Ending.Draining {
		t.Fatalf("music EOF ended the show: %+v", st)
	}
	if len(video) < 50 || len(audio) < 90 {
		t.Fatalf("independent playback stalled: video %d audio %d", len(video), len(audio))
	}
	for i := 1; i < len(video); i++ {
		if video[i]-video[i-1] != 40*time.Millisecond {
			t.Fatalf("visual cycle changed speed: %v", video[i]-video[i-1])
		}
	}
	for i := 1; i < len(audio); i++ {
		if audio[i]-audio[i-1] > 23*time.Millisecond || audio[i] <= audio[i-1] {
			t.Fatalf("music encoder restarted at a boundary: %v", audio[i]-audio[i-1])
		}
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if readStage(t, s).Stage != "OFF" {
		t.Fatal("explicit Stop did not stop music presentation")
	}
	if readStage(t, New(s.cfg, s.log)).Stage != "OFF" {
		t.Fatal("restart resumed broadcast")
	}
}

func TestMusicPresentationDeletionKeepsSharedVisualAndUploads(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "music.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .3)))
	d := generatorDraft(t, s, .12)
	d.DurationMode = "music"
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "repeat"}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	first := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	d.Soundtrack.VolumePercent = musicVolume(50)
	d = saveMusicDesign(t, s, d)
	second := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if first.State != "ready" || second.State != "ready" {
		t.Fatal(first.Error, second.Error)
	}
	if w := dashboardRequest(s, "DELETE", "/api/generator/jobs/"+first.ID, `{}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(s.library.presentationPCM(first.MediaRevision, "music")); !os.IsNotExist(err) {
		t.Fatal("deleted presentation audio remains", err)
	}
	clip, _, err := s.library.openRevision(second.MediaRevision)
	if err != nil {
		t.Fatal("shared visual was deleted", err)
	}
	clip.reader.close()
	var captured GenerationJob
	json.Unmarshal(dashboardRequest(s, "GET", "/api/generator/jobs/"+second.ID, "").Body.Bytes(), &captured)
	if w := dashboardRequest(s, "DELETE", "/api/generator/jobs/"+second.ID, `{}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, path := range []string{s.library.presentationPCM(second.MediaRevision, "music"), s.library.revisionPath(captured.VisualRevision)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("last presentation left generated files", path, err)
		}
	}
	if w := dashboardRequest(s, "GET", "/api/generator/assets/"+a.ID+"/revisions/1", ""); w.Code != 200 {
		t.Fatal("upload lost", w.Code, w.Body.String())
	}
}

func TestBRBMusicPreparationUsesCurrentVisualWithoutActivation(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "music.wav", musicFixture(t, "sine=frequency=880:sample_rate=48000", .3)))
	s.broadcast.mu.Lock()
	generation := s.broadcast.media.settings.Generation
	s.broadcast.mu.Unlock()
	body, _ := json.Marshal(map[string]any{"base_generation": generation, "name": "Break music", "duration_mode": "music", "soundtrack": mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "repeat"}})
	generatorServe(t, s)
	w := dashboardRequest(s, "POST", "/api/brb/presentations", string(body))
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var job GenerationJob
	json.Unmarshal(w.Body.Bytes(), &job)
	ready := waitGeneratorJob(t, s, job.ID)
	if ready.State != "ready" {
		t.Fatal(ready.Error)
	}
	if readStage(t, s).Stage != "OFF" || s.library.selections().BRB != "" {
		t.Fatal("preparation activated BRB")
	}
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+ready.MediaRevision+`","brb":"`+ready.MediaRevision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "brb", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if st := readStage(t, s); st.Stage != "BRB" || st.Media.Revision != ready.MediaRevision {
		t.Fatal("BRB did not use selected presentation", st)
	}
	if w := stageRequest(t, s, "return", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	stageRequest(t, s, "stop_now", nil)
}

func TestFixedMusicPresentationPreservesEndingFade(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "music.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .8)))
	d := generatorDraft(t, s, .8)
	d.Stage = "ending"
	d.EndingFadeSeconds = musicVolume(.4)
	d.DurationMode = "fixed"
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "repeat"}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	j := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if j.State != "ready" {
		t.Fatal(j.Error)
	}
	pcm := decodePCM(t, s.library.revisionPath(j.MediaRevision))
	var peak float64
	for _, v := range pcm[int(.79*48000):int(.8*48000)] {
		peak = math.Max(peak, math.Abs(v))
	}
	if peak > 5 {
		t.Fatalf("fixed ending no longer fades its whole mix: peak %.1f", peak)
	}
}

func TestMusicPlaylistLongPresentationAndCapacity(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "thirty.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", 30)))
	refs := make([]mediaauthor.AssetRef, 21)
	for i := range refs {
		refs[i] = mediaauthor.AssetRef{ID: a.ID, Revision: 1}
	}
	body, _ := json.Marshal(map[string]any{"name": "Long show", "tracks": refs})
	w := dashboardRequest(s, "POST", "/api/music/playlists", string(body))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var playlist musicPlaylistView
	json.Unmarshal(w.Body.Bytes(), &playlist)
	d := generatorDraft(t, s, .12)
	d.DurationMode = "music"
	d.Soundtrack = &mediaauthor.Soundtrack{Playlist: &mediaauthor.AssetRef{ID: playlist.ID, Revision: 1}, Mode: "repeat"}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	j := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if j.State != "ready" || j.Duration != 630 {
		t.Fatal("playlist did not exceed the old ten-minute render bound", j)
	}
	restarted := New(s.cfg, s.log)
	if w := dashboardRequest(restarted, "GET", "/api/library", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	clip, _, err := restarted.library.openRevision(j.MediaRevision)
	if err != nil {
		t.Fatal(err)
	}
	clip.reader.close()
	refs = append(refs, refs...)
	refs = append(refs, refs[:9]...)
	body, _ = json.Marshal(map[string]any{"name": "Too many", "tracks": refs})
	if w := dashboardRequest(s, "POST", "/api/music/playlists", string(body)); w.Code != 422 {
		t.Fatal("51 tracks accepted", w.Code, w.Body.String())
	}
}

func TestMusicPresentationMissingAudioAndEncoderFailuresAreExplicit(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "tone.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .3)))
	d := generatorDraft(t, s, .12)
	d.DurationMode = "music"
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "repeat"}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	j := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if j.State != "ready" {
		t.Fatal(j.Error)
	}
	local := New(s.cfg, s.log)
	selectGeneratedEnding(t, local, j.MediaRevision, j.MediaRevision)
	t.Setenv("PATH", t.TempDir())
	if w := stageRequest(t, local, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if st := readStage(t, local); st.Playback.State != "failed" || st.Error == "" {
		t.Fatal("encoder start failure hidden", st)
	}
	stageRequest(t, local, "stop_now", nil)
	if err := os.Remove(local.library.presentationPCM(j.MediaRevision, "music")); err != nil {
		t.Fatal(err)
	}
	if w := stageRequest(t, local, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 409 {
		t.Fatal("missing music silently substituted", w.Code, w.Body.String())
	}
	if readStage(t, local).Stage != "OFF" {
		t.Fatal("failed validation activated a stage")
	}
}

func TestMusicPresentationLocalRTMPAndRehearsal(t *testing.T) {
	prepared := libraryServer(t)
	destination := newSink(t)
	cfg := prepared.cfg
	cfg.Targets = []config.Target{destination.target("local")}
	s := New(cfg, prepared.log)
	serveEndingActivation(t, s)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "first.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .4)))
	b := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "second.wav", musicFixture(t, "sine=frequency=880:sample_rate=48000", .6)))
	body, _ := json.Marshal(map[string]any{"name": "Ordered", "tracks": []mediaauthor.AssetRef{{ID: a.ID, Revision: 1}, {ID: b.ID, Revision: 1}}})
	w := dashboardRequest(s, "POST", "/api/music/playlists", string(body))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var p musicPlaylistView
	json.Unmarshal(w.Body.Bytes(), &p)
	d := generatorDraft(t, s, .28)
	d.Stage = "ending"
	d.EndingFadeSeconds = musicVolume(0)
	d.DurationMode = "music"
	d.Soundtrack = &mediaauthor.Soundtrack{Playlist: &mediaauthor.AssetRef{ID: p.ID, Revision: 1}, Mode: "repeat"}
	d = saveMusicDesign(t, s, d)
	j := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if j.State != "ready" {
		t.Fatal(j.Error)
	}
	selectGeneratedEnding(t, s, j.MediaRevision, j.MediaRevision)
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	time.Sleep(120 * time.Millisecond)
	if destination.count() != 0 {
		t.Fatal("rehearsal opened a destination")
	}
	stageRequest(t, s, "stop_now", nil)
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "real"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "end_stream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	defer stageRequest(t, s, "stop_now", nil)
	// Editing the playlist never changes the captured on-air presentation.
	body, _ = json.Marshal(map[string]any{"name": "Reversed", "version": 1, "tracks": []mediaauthor.AssetRef{{ID: b.ID, Revision: 1}, {ID: a.ID, Revision: 1}}})
	if w := dashboardRequest(s, "PUT", "/api/music/playlists/"+p.ID, string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	start := time.Now()
	packets := []*rtmp.Message{}
	for time.Since(start) < 2400*time.Millisecond {
		select {
		case msg := <-destination.packets:
			packets = append(packets, msg)
		case <-time.After(5 * time.Millisecond):
		}
	}
	st := readStage(t, s)
	if st.Stage != "ENDING" || st.Media.Revision != j.MediaRevision || st.Error != "" || destination.count() != 1 {
		t.Fatal("music cycles changed stage/revision/session", st, destination.count())
	}
	path := filepath.Join(t.TempDir(), "live.flv")
	writeTestFLV(t, path, packets)
	pcm := decodePCM(t, path)
	if len(pcm) < int(1.8*48000) {
		t.Fatal("music ended prematurely", len(pcm))
	}
	// Local sink joins at the next visual keyframe; inspect periodic tone content
	// across several cycles rather than assuming network startup has zero latency.
	var first, second int
	for at := .2; at < 1.8; at += .1 {
		if tonePower(pcm, at, 440) > 1e10 {
			first++
		}
		if tonePower(pcm, at, 880) > 1e10 {
			second++
		}
	}
	if first < 4 || second < 6 {
		t.Fatal("playlist music did not continue across short visual loops", first, second)
	}
	preview := dashboardRequest(s, "GET", "/api/library/revisions/"+j.MediaRevision+"/preview", "")
	if preview.Code != 200 {
		t.Fatal(preview.Code, preview.Body.String())
	}
	path = filepath.Join(t.TempDir(), "preview.mp4")
	os.WriteFile(path, preview.Body.Bytes(), 0600)
	one := decodePCM(t, path)
	if tonePower(one, .15, 440) < 1e10 || tonePower(one, .65, 880) < 1e10 {
		t.Fatal("preview order changed with edited playlist")
	}
}

func TestMusicPresentationPreservesSceneAudioAcrossVisualLoops(t *testing.T) {
	s := libraryServer(t)
	video := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=video", "split.mp4", generatorSplitVideo(t)))
	music := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "music.wav", musicFixture(t, "sine=frequency=1320:sample_rate=48000", 3)))
	d := videoDesign(t, s, video.ID, 2, 0, 2, 100, true, true)
	d.Stage = "prestream"
	d.DurationMode = "music"
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: music.ID, Revision: 1}, Mode: "repeat"}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	job := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if job.State != "ready" {
		t.Fatal(job.Error)
	}
	pcm := decodePCM(t, s.library.revisionPath(job.MediaRevision))
	if tonePower(pcm, .2, 440) < 1e10 || tonePower(pcm, 1.2, 880) < 1e10 || tonePower(pcm, 2.2, 440) < 1e10 || tonePower(pcm, 2.2, 1320) < 1e10 {
		t.Fatal("music-only preparation lost scene audio or did not repeat it with the visual")
	}
}

func TestFixedPlaylistKeepsCircularFadeAndWarmAAC(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "first.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", 1)))
	b := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "second.wav", musicFixture(t, "sine=frequency=880:sample_rate=48000", 1)))
	body, _ := json.Marshal(map[string]any{"name": "Crossfade", "tracks": []mediaauthor.AssetRef{{ID: a.ID, Revision: 1}, {ID: b.ID, Revision: 1}}})
	w := dashboardRequest(s, "POST", "/api/music/playlists", string(body))
	var playlist musicPlaylistView
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &playlist) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	d := generatorDraft(t, s, 2)
	d.DurationMode = "fixed"
	d.LoopTransition = &mediaauthor.Transition{Kind: "crossfade", DurationSeconds: .4}
	d.Soundtrack = &mediaauthor.Soundtrack{Playlist: &mediaauthor.AssetRef{ID: playlist.ID, Revision: 1}, Mode: "repeat"}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	j := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if j.State != "ready" {
		t.Fatal(j.Error)
	}
	w = dashboardRequest(s, "GET", "/api/library/revisions/"+j.MediaRevision+"/preview?passes=2", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	path := filepath.Join(t.TempDir(), "circular.mp4")
	os.WriteFile(path, w.Body.Bytes(), 0600)
	pcm := decodePCM(t, path)
	if tonePower(pcm, .15, 440) < 1e10 || tonePower(pcm, .8, 880) < 1e10 || tonePower(pcm, 1.35, 440) < 1e9 || tonePower(pcm, 1.35, 880) < 1e9 || tonePower(pcm, 1.75, 440) < 1e10 {
		t.Fatal("fixed playlist lost circular music crossfade or rotation")
	}
	var boundary float64
	for _, v := range pcm[int(1.6*48000):int(1.61*48000)] {
		boundary += v * v
	}
	if math.Sqrt(boundary/480) < 1000 {
		t.Fatal("AAC priming silence recurs at fixed visual boundary")
	}
}

func TestInterruptedPresentationRetainsItsSharedVisual(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "music.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .3)))
	d := generatorDraft(t, s, .12)
	d.DurationMode = "music"
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "repeat"}
	d = saveMusicDesign(t, s, d)
	generatorServe(t, s)
	first := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	d.Soundtrack.VolumePercent = musicVolume(50)
	d = saveMusicDesign(t, s, d)
	second := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if first.State != "ready" || second.State != "ready" {
		t.Fatal(first.Error, second.Error)
	}
	var interrupted GenerationJob
	json.Unmarshal(dashboardRequest(s, "GET", "/api/generator/jobs/"+second.ID, "").Body.Bytes(), &interrupted)
	visual := interrupted.VisualRevision
	interrupted.State = "running"
	interrupted.VisualRevision = ""
	interrupted.MediaRevision = ""
	interrupted.VisualFingerprint = ""
	if err := writeState(s.generator.jobPath(second.ID), interrupted); err != nil {
		t.Fatal(err)
	}
	restarted := New(s.cfg, s.log)
	if w := dashboardRequest(restarted, "DELETE", "/api/generator/jobs/"+first.ID, `{}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(restarted, "GET", "/api/library/revisions/"+second.MediaRevision+"/preview", ""); w.Code != 200 {
		t.Fatal("recovered presentation lost its shared visual", w.Code, w.Body.String())
	}
	if w := dashboardRequest(restarted, "DELETE", "/api/generator/jobs/"+second.ID, `{}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(restarted.library.revisionPath(visual)); !os.IsNotExist(err) {
		t.Fatal("last interrupted owner left shared visual behind", err)
	}
}

func TestCorruptPresentationVideoDoesNotBlockStop(t *testing.T) {
	prepared := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, prepared, "/api/generator/assets?kind=audio", "music.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .3)))
	d := generatorDraft(t, prepared, .12)
	d.DurationMode = "music"
	d.Soundtrack = &mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "repeat"}
	d = saveMusicDesign(t, prepared, d)
	generatorServe(t, prepared)
	job := waitGeneratorJob(t, prepared, submitGenerator(t, prepared, d).ID)
	if job.State != "ready" {
		t.Fatal(job.Error)
	}
	local := New(prepared.cfg, prepared.log)
	selectGeneratedEnding(t, local, job.MediaRevision, job.MediaRevision)
	if w := stageRequest(t, local, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var captured GenerationJob
	json.Unmarshal(dashboardRequest(local, "GET", "/api/generator/jobs/"+job.ID, "").Body.Bytes(), &captured)
	path := local.library.revisionPath(captured.VisualRevision)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for offset := 13; offset+15 <= len(data); {
		size := int(data[offset+1])<<16 | int(data[offset+2])<<8 | int(data[offset+3])
		if data[offset] == rtmp.Video {
			data[offset] = rtmp.Audio
		}
		offset += 15 + size
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { local.broadcast.tick(time.Now()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("corrupted video blocked broadcast command lock")
	}
	if st := readStage(t, local); st.Error == "" {
		t.Fatal("corrupt video failed silently", st)
	}
	if w := stageRequest(t, local, "stop_now", nil); w.Code != 200 {
		t.Fatal("Stop blocked", w.Code, w.Body.String())
	}
}

func TestBRBMusicReuseRespectsChangedAudioProfile(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "music.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .3)))
	generatorServe(t, s)
	prepare := func() generatorJobView {
		var dash struct {
			Assets brbSettings `json:"brb_assets"`
		}
		json.Unmarshal(dashboardRequest(s, "GET", "/api/dashboard", "").Body.Bytes(), &dash)
		body, _ := json.Marshal(map[string]any{"base_generation": dash.Assets.Generation, "name": "Break", "duration_mode": "music", "soundtrack": mediaauthor.Soundtrack{Asset: mediaauthor.AssetRef{ID: a.ID, Revision: 1}, Mode: "repeat"}})
		w := dashboardRequest(s, "POST", "/api/brb/presentations", string(body))
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		var job GenerationJob
		json.Unmarshal(w.Body.Bytes(), &job)
		ready := waitGeneratorJob(t, s, job.ID)
		if ready.State != "ready" {
			t.Fatal(ready.Error)
		}
		return ready
	}
	first := prepare()
	if w := assetRequest(t, s, map[string]string{"sample_rate": "44100"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	second := prepare()
	if second.MediaRevision == first.MediaRevision {
		t.Fatal("audio profile reused incompatible presentation")
	}
	previewGeneratedEnding(t, s, second.MediaRevision)
}

func TestInterruptedPresentationRemovesUnpublishedFiles(t *testing.T) {
	s := libraryServer(t)
	id := strings.Repeat("a", 64)
	if err := os.MkdirAll(filepath.Join(s.library.root, "presentations"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{s.library.presentationPCM(id, "music"), s.library.presentationPCM(id, "scene"), s.library.revisionPath(id)} {
		if err := os.WriteFile(path, []byte("interrupted"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	restarted := New(s.cfg, s.log)
	if w := dashboardRequest(restarted, "GET", "/api/library", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, path := range []string{s.library.presentationPCM(id, "music"), s.library.presentationPCM(id, "scene"), s.library.revisionPath(id)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("unpublished presentation file survived restart", path, err)
		}
	}
}
