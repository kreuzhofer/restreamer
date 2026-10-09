package relay

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func submitGenerator(t *testing.T, s *Server, d mediaauthor.Design) generatorJobView {
	t.Helper()
	w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version))
	if w.Code != 202 {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	return generatorJob(t, w)
}
func generatorQueue(t *testing.T, s *Server) []generatorJobView {
	t.Helper()
	w := dashboardRequest(s, "GET", "/api/generator/jobs", "")
	var jobs []generatorJobView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &jobs) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	return jobs
}
func TestGeneratorQueueIsBoundedFIFOAcrossBothStages(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, 600)
	ending := d
	ending.ID = ""
	ending.Stage = "ending"
	ending.Name = "Ending queue"
	data, _ := json.Marshal(ending)
	w := dashboardRequest(s, "POST", "/api/generator/designs", string(data))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if json.Unmarshal(w.Body.Bytes(), &ending) != nil {
		t.Fatal("ending draft")
	}
	var accepted []generatorJobView
	for i := 0; i < 8; i++ {
		design := d
		if i%2 == 1 {
			design = ending
		}
		accepted = append(accepted, submitGenerator(t, s, design))
	}
	if w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":1}`, d.ID)); w.Code != 409 {
		t.Fatal("queue admission", w.Code, w.Body.String())
	}
	// Cancelling an interior job removes it from the execution order and makes room.
	if w := dashboardRequest(s, "POST", "/api/generator/jobs/"+accepted[3].ID+"/cancel", `{}`); w.Code != 202 {
		t.Fatal(w.Code)
	}
	accepted = append(accepted, submitGenerator(t, s, d))
	queued := generatorQueue(t, s)
	for i, j := range queued {
		if j.ID != accepted[i].ID || j.Sequence != uint64(i+1) {
			t.Fatal("admission order not visible", queued)
		}
	}
	generatorServe(t, s)
	for i, expected := range accepted {
		if i == 3 {
			continue
		}
		eventually(t, func() bool {
			return generatorJob(t, dashboardRequest(s, "GET", "/api/generator/jobs/"+expected.ID, "")).State == "running"
		})
		active := 0
		for _, j := range generatorQueue(t, s) {
			if j.State == "running" || j.State == "cancelling" {
				active++
				if j.ID != expected.ID {
					t.Fatalf("out of order: got %s want %s", j.ID, expected.ID)
				}
			}
		}
		if active != 1 {
			t.Fatalf("active renders %d", active)
		}
		if w := dashboardRequest(s, "POST", "/api/generator/jobs/"+expected.ID+"/cancel", `{}`); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		if j := waitGeneratorJob(t, s, expected.ID); j.State != "cancelled" || j.MediaRevision != "" {
			t.Fatal("cancel published result", j)
		}
	}
	if readStage(t, s).Stage != "OFF" {
		t.Fatal("queue changed stage")
	}
}

func TestGeneratorRetryUsesInterruptedCaptureAfterDraftChanges(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, 1)
	original := submitGenerator(t, s, d)
	d.Scenes[0].Text = "New draft must not replace capture"
	data, _ := json.Marshal(d)
	if w := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(data)); w.Code != 200 {
		t.Fatal(w.Code)
	}
	restarted := New(s.cfg, s.log)
	interrupted := generatorJob(t, dashboardRequest(restarted, "GET", "/api/generator/jobs/"+original.ID, ""))
	if interrupted.State != "interrupted" {
		t.Fatal(interrupted)
	}
	generatorServe(t, restarted)
	time.Sleep(30 * time.Millisecond)
	if j := generatorJob(t, dashboardRequest(restarted, "GET", "/api/generator/jobs/"+original.ID, "")); j.State != "interrupted" {
		t.Fatal("silently restarted", j)
	}
	w := dashboardRequest(restarted, "POST", "/api/generator/jobs/"+original.ID+"/retry", `{}`)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	retry := generatorJob(t, w)
	if retry.ID == original.ID || retry.DesignRevision != original.DesignRevision || retry.Design.Scenes[0].Text != original.Design.Scenes[0].Text || retry.Sequence <= original.Sequence {
		t.Fatal("retry changed captured inputs", retry)
	}
	if j := waitGeneratorJob(t, restarted, retry.ID); j.State != "ready" {
		t.Fatal(j)
	}
	if j := generatorJob(t, dashboardRequest(restarted, "GET", "/api/generator/jobs/"+original.ID, "")); j.State != "interrupted" {
		t.Fatal("retry changed prior record", j)
	}
	if w := dashboardRequest(restarted, "POST", "/api/generator/jobs/"+retry.ID+"/retry", `{}`); w.Code != 409 {
		t.Fatal("ready job retried", w.Code)
	}
}

func TestGeneratorWaitsForLibraryPreparationAndCancelsWithoutOutput(t *testing.T) {
	_, source := preparedClip(t)
	original := filepath.Join(filepath.Dir(source), "source.mp4")
	long := filepath.Join(t.TempDir(), "long.mp4")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-stream_loop", "-1", "-i", original, "-t", "600", "-c", "copy", long).CombinedOutput(); err != nil {
		t.Fatalf("long source %v %s", err, out)
	}
	data, err := os.ReadFile(long)
	if err != nil {
		t.Fatal(err)
	}
	s := libraryServer(t)
	generatorServe(t, s)
	if w := videoUpload(t, s, "long.mp4", data); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Another upload triggers discovery of the now-stable first file without
	// depending on the five-second background scan cadence.
	short, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	if w := videoUpload(t, s, "next.mp4", short); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool {
		var status LibraryStatus
		w := dashboardRequest(s, "GET", "/api/library", "")
		json.Unmarshal(w.Body.Bytes(), &status)
		for _, file := range status.Files {
			if file.Name == "long.mp4" {
				return file.Progress > 0 && file.State == "preparing"
			}
		}
		return false
	})
	job := submitGenerator(t, s, generatorDraft(t, s, 1))
	eventually(t, func() bool {
		w := dashboardRequest(s, "GET", "/api/generator/jobs/"+job.ID, "")
		var status map[string]any
		json.Unmarshal(w.Body.Bytes(), &status)
		return status["state"] == "running" && strings.Contains(fmt.Sprint(status["message"]), "Waiting for media preparation")
	})
	if w := dashboardRequest(s, "POST", "/api/generator/jobs/"+job.ID+"/cancel", `{}`); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if j := waitGeneratorJob(t, s, job.ID); j.State != "cancelled" || j.MediaRevision != "" {
		t.Fatal("waiting cancellation published output", j)
	}
	// A following job eventually acquires the released preparation slot and finishes.
	ready := waitGeneratorJob(t, s, submitGenerator(t, s, generatorDraft(t, s, 1)).ID)
	if ready.State != "ready" {
		t.Fatal(ready)
	}
	if readStage(t, s).Stage != "OFF" {
		t.Fatal("preparation changed stage")
	}
}

func TestGeneratorCancellationCompletionRaceRetainsOnlyCompleteMedia(t *testing.T) {
	s := libraryServer(t)
	generatorServe(t, s)
	d := generatorDraft(t, s, 0.2)
	for _, delay := range []time.Duration{0, 5, 20, 50, 100, 200} {
		job := submitGenerator(t, s, d)
		time.Sleep(delay * time.Millisecond)
		w := dashboardRequest(s, "POST", "/api/generator/jobs/"+job.ID+"/cancel", `{}`)
		if w.Code != 202 && w.Code != 409 {
			t.Fatal(w.Code, w.Body.String())
		}
		job = waitGeneratorJob(t, s, job.ID)
		switch job.State {
		case "cancelled":
			if job.MediaRevision != "" {
				t.Fatal("cancelled job published media", job)
			}
		case "ready":
			preview := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
			if preview.Code != 200 {
				t.Fatal(preview.Code)
			}
			path := filepath.Join(t.TempDir(), "ready.mp4")
			if err := os.WriteFile(path, preview.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil {
				t.Fatalf("incomplete result %v %s", err, out)
			}
		default:
			t.Fatal("unexpected race outcome", job)
		}
	}
}

func TestGeneratorFailedRetryRetainsCapture(t *testing.T) {
	s := libraryServer(t)
	generatorServe(t, s)
	d := generatorDraft(t, s, 1)
	path := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	original := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if original.State != "failed" {
		t.Fatal(original)
	}
	t.Setenv("PATH", path)
	w := dashboardRequest(s, "POST", "/api/generator/jobs/"+original.ID+"/retry", `{}`)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	retry := waitGeneratorJob(t, s, generatorJob(t, w).ID)
	if retry.State != "ready" || retry.DesignRevision != original.DesignRevision {
		t.Fatal(retry)
	}
}
