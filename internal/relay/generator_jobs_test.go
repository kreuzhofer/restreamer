package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

type generatorJobView struct {
	ID             string             `json:"id"`
	Sequence       uint64             `json:"sequence"`
	Progress       int                `json:"progress"`
	State          string             `json:"state"`
	Error          string             `json:"error"`
	DesignRevision string             `json:"design_revision"`
	Design         mediaauthor.Design `json:"design_snapshot"`
	MediaRevision  string             `json:"media_revision"`
	Duration       float64            `json:"duration"`
}

func generatorDraft(t *testing.T, s *Server, seconds float64) mediaauthor.Design {
	t.Helper()
	w := dashboardRequest(s, "POST", "/api/generator/designs", fmt.Sprintf(`{"name":"Generated title","stage":"prestream","theme":{"id":"retro","revision":1},"scenes":[{"id":"title","layout":"title","text":"Welcome München","duration_seconds":%g}]}`, seconds))
	if w.Code != 201 {
		t.Fatalf("draft: %d %s", w.Code, w.Body.String())
	}
	var d mediaauthor.Design
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}
func generatorJob(t *testing.T, w *httptest.ResponseRecorder) generatorJobView {
	t.Helper()
	var j generatorJobView
	if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
		t.Fatalf("job: %d %s", w.Code, w.Body.String())
	}
	return j
}
func generatorServe(t *testing.T, s *Server) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, l) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("generator shutdown did not finish")
		}
	})
}
func waitGeneratorJob(t *testing.T, s *Server, id string) generatorJobView {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		w := dashboardRequest(s, "GET", "/api/generator/jobs/"+id, "")
		if w.Code != 200 {
			t.Fatalf("status: %d %s", w.Code, w.Body.String())
		}
		j := generatorJob(t, w)
		if j.State != "queued" && j.State != "running" && j.State != "cancelling" {
			return j
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return generatorJobView{}
}
func TestGeneratorCapturesSavedRevisionAndPreviewsExactOutput(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		if os.Getenv("REQUIRE_FFMPEG_TESTS") == "1" {
			t.Fatal(err)
		}
		t.Skip("FFmpeg unavailable")
	}
	s := libraryServer(t)
	d := generatorDraft(t, s, 2)
	payload := fmt.Sprintf(`{"design_id":%q,"version":1}`, d.ID)
	requestCtx, disconnect := context.WithCancel(context.Background())
	request := httptest.NewRequest("POST", "/api/generator/jobs", bytes.NewBufferString(payload)).WithContext(requestCtx)
	request.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Restreamer-Control", "1")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, request)
	disconnect()
	if w.Code != 202 {
		t.Fatalf("generate: %d %s", w.Code, w.Body.String())
	}
	job := generatorJob(t, w)
	if job.Design.Version != 1 || job.Design.Scenes[0].Text != "Welcome München" || job.DesignRevision == "" {
		t.Fatalf("missing captured revision: %+v", job)
	}

	original, _ := json.Marshal(d)
	preview := dashboardRequest(s, "POST", "/api/generator/preview", string(original))
	d.Scenes[0].Text = "A newer draft"
	changed, _ := json.Marshal(d)
	if w := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(changed)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	generatorServe(t, s)
	job = waitGeneratorJob(t, s, job.ID)
	if job.State != "ready" || job.MediaRevision == "" || job.Design.Version != 1 || job.Design.Scenes[0].Text != "Welcome München" || job.Duration != 2 {
		t.Fatalf("wrong completed capture: %+v", job)
	}
	ready := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
	if ready.Code != 200 || ready.Header().Get("X-Media-Revision") != job.MediaRevision {
		t.Fatalf("exact preview: %d %s", ready.Code, ready.Body.String())
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "exact.mp4")
	if err := os.WriteFile(path, ready.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("ffmpeg", "-v", "error", "-threads", "2", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil {
		t.Fatalf("decode: %v %s", err, output)
	}
	output, err := exec.Command("ffmpeg", "-v", "error", "-threads", "2", "-i", path, "-frames:v", "1", "-threads", "2", "-f", "image2pipe", "-c:v", "png", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	want, err := png.Decode(preview.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds() != want.Bounds() {
		t.Fatal("profile differs from quick preview")
	}
	var delta uint64
	for y := 0; y < got.Bounds().Dy(); y++ {
		for x := 0; x < got.Bounds().Dx(); x++ {
			r, g, b, _ := got.At(x, y).RGBA()
			rr, gg, bb, _ := want.At(x, y).RGBA()
			for _, n := range []int64{int64(r) - int64(rr), int64(g) - int64(gg), int64(b) - int64(bb)} {
				if n < 0 {
					n = -n
				}
				delta += uint64(n)
			}
		}
	}
	if float64(delta)/float64(got.Bounds().Dx()*got.Bounds().Dy()*3*257) > 5 {
		t.Fatal("prepared typography differs from quick preview")
	}
	if got := readStage(t, s); got.Stage != "OFF" {
		t.Fatalf("generation activated stage: %+v", got)
	}
	if w := dashboardRequest(s, "POST", "/api/generator/jobs", payload); w.Code != 409 {
		t.Fatal("stale draft generation accepted", w.Code)
	}
	restarted := New(s.cfg, s.log)
	recovered := generatorJob(t, dashboardRequest(restarted, "GET", "/api/generator/jobs/"+job.ID, ""))
	if recovered.MediaRevision != job.MediaRevision || recovered.Design.Version != 1 {
		t.Fatal("captured revision not retained across restart")
	}
}

func TestGeneratorCancellationAndFailurePreserveReadyRevision(t *testing.T) {
	s := libraryServer(t)
	generatorServe(t, s)
	d := generatorDraft(t, s, 1)
	create := func(d mediaauthor.Design) generatorJobView {
		t.Helper()
		w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version))
		if w.Code != 202 {
			t.Fatalf("generate: %d %s", w.Code, w.Body.String())
		}
		return generatorJob(t, w)
	}
	ready := waitGeneratorJob(t, s, create(d).ID)
	if ready.State != "ready" {
		t.Fatal(ready)
	}
	previewURL := "/api/library/revisions/" + ready.MediaRevision + "/preview"
	before := dashboardRequest(s, "GET", previewURL, "").Body.Bytes()
	// A long real encode provides time to cancel through the public API. This
	// does not replace FFmpeg or inspect the worker's internal state.
	d = generatorDraft(t, s, 600)
	job := create(d)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job = generatorJob(t, dashboardRequest(s, "GET", "/api/generator/jobs/"+job.ID, ""))
		if job.State == "running" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if job.State != "running" {
		t.Fatalf("never running: %+v", job)
	}
	w := dashboardRequest(s, "POST", "/api/generator/jobs/"+job.ID+"/cancel", `{}`)
	if w.Code != 202 {
		t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
	}
	if cancelled := waitGeneratorJob(t, s, job.ID); cancelled.State != "cancelled" || cancelled.MediaRevision != "" {
		t.Fatalf("cancelled output accepted: %+v", cancelled)
	}
	// The executable is an external dependency. Exercise a real missing-FFmpeg
	// failure rather than replacing internal worker or renderer collaborators.
	t.Setenv("PATH", t.TempDir())
	failed := waitGeneratorJob(t, s, create(d).ID)
	if failed.State != "failed" || failed.Error == "" || failed.MediaRevision != "" {
		t.Fatalf("failure hidden: %+v", failed)
	}
	after := dashboardRequest(s, "GET", previewURL, "")
	if after.Code != 200 || !bytes.Equal(before, after.Body.Bytes()) {
		t.Fatal("cancel/failure changed prior exact media")
	}
	if stage := readStage(t, s); stage.Stage != "OFF" {
		t.Fatal("generation failure affected stage", stage.Stage)
	}
}

func TestGeneratorRejectsInvalidInputsAndUnmeasuredProfile(t *testing.T) {
	s := libraryServer(t)
	for _, seconds := range []float64{0.001, 601} {
		d := generatorDraft(t, s, seconds)
		w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":1}`, d.ID))
		if w.Code != 422 || !bytes.Contains(w.Body.Bytes(), []byte("duration_seconds")) {
			t.Fatalf("invalid duration accepted: %d %s", w.Code, w.Body.String())
		}
	}
	d := generatorDraft(t, s, 2)
	if w := assetRequest(t, s, map[string]string{"fps": "60"}, "", nil); w.Code != 204 {
		t.Fatalf("profile: %d %s", w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":1}`, d.ID)); w.Code != 422 || !bytes.Contains(w.Body.Bytes(), []byte("profile")) {
		t.Fatalf("unmeasured profile silently changed: %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{"/api/generator/jobs", "/api/generator/jobs/missing", "/api/generator/jobs/missing/cancel", "/api/generator/jobs/missing/retry"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("unauthenticated jobs", path, w.Code)
		}
	}
	for _, path := range []string{"/api/generator/jobs", "/api/generator/jobs/missing/cancel", "/api/generator/jobs/missing/retry"} {
		r := httptest.NewRequest("POST", path, bytes.NewBufferString(`{}`))
		r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
		r.Header.Set("Origin", "https://attacker.invalid")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Restreamer-Control", "1")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("cross-origin job control", path, w.Code)
		}
	}
}

func TestGeneratorProfileChangePreservesCapturedCandidateWithoutSubstitution(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, 1)
	w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":1}`, d.ID))
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	job := generatorJob(t, w)
	if w := assetRequest(t, s, map[string]string{"fps": "30"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	generatorServe(t, s)
	job = waitGeneratorJob(t, s, job.ID)
	if job.State != "failed" || job.MediaRevision == "" || !strings.Contains(job.Error, "profile changed") {
		t.Fatalf("captured profile silently substituted or completed candidate lost: %+v", job)
	}
	if w := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", ""); w.Code != 409 {
		t.Fatal("incompatible revision preview accepted", w.Code)
	}
	if w := assetRequest(t, s, map[string]string{"fps": "25"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", ""); w.Code != 200 {
		t.Fatal("captured exact candidate unavailable", w.Code, w.Body.String())
	}
}

func TestGeneratorRevisionSurvivesConcurrentLibraryPreparation(t *testing.T) {
	s := libraryServer(t)
	generatorServe(t, s)
	d := generatorDraft(t, s, 1)
	payload := fmt.Sprintf(`{"design_id":%q,"version":1}`, d.ID)
	create := func() generatorJobView {
		t.Helper()
		w := dashboardRequest(s, "POST", "/api/generator/jobs", payload)
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		return generatorJob(t, w)
	}
	first := waitGeneratorJob(t, s, create().ID)
	if first.State != "ready" {
		t.Fatal(first)
	}
	previewURL := "/api/library/revisions/" + first.MediaRevision + "/preview"
	before := dashboardRequest(s, "GET", previewURL, "")
	// Import the exact MP4 through the ordinary library upload/preparation API
	// while generating the same content again; neither worker owns the other's
	// immutable revision and both share the retained revision catalog.
	if w := videoUpload(t, s, "generated-copy.mp4", before.Body.Bytes()); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	second := waitGeneratorJob(t, s, create().ID)
	if second.State != "ready" || second.MediaRevision != first.MediaRevision {
		t.Fatalf("same content identity changed: %+v", second)
	}
	uploaded := readyHTTPRevision(t, s)
	if w := dashboardRequest(s, "GET", "/api/library/revisions/"+uploaded+"/preview", ""); w.Code != 200 {
		t.Fatal("ordinary prepared revision unavailable", w.Code)
	}
	if after := dashboardRequest(s, "GET", previewURL, ""); after.Code != 200 || !bytes.Equal(before.Body.Bytes(), after.Body.Bytes()) {
		t.Fatal("concurrent preparation mutated retained generator revision")
	}
}

func TestGeneratorKeepsLocalRTMPDeliveryResponsive(t *testing.T) {
	destination := newSink(t)
	failing := newSink(t)
	s, address, media := startDrainRelay(t, []config.Target{destination.target("one"), failing.target("two")})
	publisher := startDrainLive(t, s, address, media)
	ctx, cancel := context.WithCancel(context.Background())
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		var timestamp time.Duration
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				timestamp += 40 * time.Millisecond
				frame := *media.keyframe
				frame.Timestamp = timestamp
				frame.MessageStreamID, frame.ChunkStreamID = rtmp.StreamID, 6
				if publisher.Write(&frame) != nil {
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-sent }()
	eventually(t, func() bool { return readTargetDashboard(t, s).Outputs[0].Frames >= 5 })
	before := readTargetDashboard(t, s).Outputs[0]
	failing.listener.Close()
	failing.disconnect()
	var catalog LibraryStatus
	if w := dashboardRequest(s, "GET", "/api/library", ""); json.Unmarshal(w.Body.Bytes(), &catalog) != nil || len(catalog.Revisions) == 0 {
		t.Fatal("missing local fixture")
	}
	preview := dashboardRequest(s, "GET", "/api/library/revisions/"+catalog.Revisions[0].ID+"/preview", "")
	if preview.Code != 200 {
		t.Fatal(preview.Code)
	}
	started := time.Now()
	d := generatorDraft(t, s, 600)
	w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":1}`, d.ID))
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	job := generatorJob(t, w)
	eventually(t, func() bool {
		j := generatorJob(t, dashboardRequest(s, "GET", "/api/generator/jobs/"+job.ID, ""))
		return j.State == "running" && j.Progress > 0
	})
	for _, name := range []string{"while-live.mp4", "also-live.mp4"} {
		if w := videoUpload(t, s, name, preview.Body.Bytes()); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	eventually(t, func() bool {
		var status LibraryStatus
		w := dashboardRequest(s, "GET", "/api/library", "")
		json.Unmarshal(w.Body.Bytes(), &status)
		waiting, uploaded := false, false
		for _, file := range status.Files {
			if strings.Contains(file.Message, "Waiting for media preparation") && file.Progress == 0 {
				waiting = true
			}
			if file.Name == "while-live.mp4" && (file.State == "queued" || file.State == "preparing") {
				uploaded = true
			}
		}
		return waiting && uploaded
	})
	eventually(t, func() bool { return readTargetDashboard(t, s).Outputs[0].Frames >= before.Frames+25 })
	after := readTargetDashboard(t, s).Outputs[0]
	if after.DroppedFrames != before.DroppedFrames || readStage(t, s).Stage != "LIVE" || destination.count() != 1 {
		t.Fatal("generation disrupted relay", before, after)
	}
	job = generatorJob(t, dashboardRequest(s, "GET", "/api/generator/jobs/"+job.ID, ""))
	if job.State == "queued" || job.State == "running" {
		if w := dashboardRequest(s, "POST", "/api/generator/jobs/"+job.ID+"/cancel", `{}`); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	job = waitGeneratorJob(t, s, job.ID)
	if job.State != "cancelled" && job.State != "ready" {
		t.Fatal(job)
	}
	eventually(t, func() bool {
		var status LibraryStatus
		w := dashboardRequest(s, "GET", "/api/library", "")
		json.Unmarshal(w.Body.Bytes(), &status)
		for _, file := range status.Files {
			if file.Name == "while-live.mp4" {
				return file.State == "ready"
			}
		}
		return false
	})
	final := readTargetDashboard(t, s).Outputs[0]
	if final.DroppedFrames != before.DroppedFrames || destination.count() != 1 || final.Frames <= after.Frames {
		t.Fatal("library preparation or failed destination disrupted healthy delivery", final)
	}
	t.Logf("generation plus library preparation during LIVE: %s, healthy destination advanced %d frames, dropped delta %d, connections %d", time.Since(started), final.Frames-before.Frames, final.DroppedFrames-before.DroppedFrames, destination.count())
}
