package relay

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestContinuousRehearsalDefersCapturedGenerationAndCancellationWins(t *testing.T) {
	s := libraryServer(t)
	generatorServe(t, s)
	ready := waitGeneratorJob(t, s, submitGenerator(t, s, generatorDraft(t, s, 1)).ID)
	if ready.State != "ready" {
		t.Fatal(ready)
	}
	if w := dashboardRequest(s, "PUT", "/api/stage-media", fmt.Sprintf(`{"prestream":%q,"shortcuts":[]}`, ready.MediaRevision)); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "POST", "/api/destinations/import", `{"version":0}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "POST", "/api/destinations", `{"version":1,"name":"local","url":"rtmp://localhost/app","stream_key":"test-key","output_mode":"continuous"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	draft := generatorDraft(t, s, 600)
	job := submitGenerator(t, s, draft)
	eventually(t, func() bool {
		j := generatorJob(t, dashboardRequest(s, "GET", "/api/generator/jobs/"+job.ID, ""))
		return j.State == "running" && j.Progress > 0
	})
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool {
		j := generatorJob(t, dashboardRequest(s, "GET", "/api/generator/jobs/"+job.ID, ""))
		return strings.Contains(j.Message, "broadcast capacity") && j.Progress == 0
	})
	// Editing the saved draft must not change the captured job while it waits.
	draft.Scenes[0].Text = "This edit is not the captured render"
	data, _ := json.Marshal(draft)
	if w := dashboardRequest(s, "PUT", "/api/generator/designs/"+draft.ID, string(data)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	second := submitGenerator(t, s, generatorDraft(t, s, 1))
	time.Sleep(250 * time.Millisecond)
	if j := generatorJob(t, dashboardRequest(s, "GET", "/api/generator/jobs/"+second.ID, "")); j.State != "queued" {
		t.Fatal("new work started during encoding", j)
	}
	if w := dashboardRequest(s, "POST", "/api/generator/jobs/"+job.ID+"/cancel", `{}`); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if j := waitGeneratorJob(t, s, job.ID); j.State != "cancelled" || j.MediaRevision != "" || j.Design.Scenes[0].Text != job.Design.Scenes[0].Text {
		t.Fatal("cancel or capture lost", j)
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if j := waitGeneratorJob(t, s, second.ID); j.State != "ready" {
		t.Fatal("queued work did not resume", j)
	}
	if j := generatorJob(t, dashboardRequest(s, "GET", "/api/generator/jobs/"+job.ID, "")); j.State != "cancelled" {
		t.Fatal("cancelled work restarted", j)
	}
}

func TestContinuousInterruptedGenerationRestartsItsCapturedRevision(t *testing.T) {
	s := libraryServer(t)
	generatorServe(t, s)
	ready := waitGeneratorJob(t, s, submitGenerator(t, s, generatorDraft(t, s, 1)).ID)
	if ready.State != "ready" {
		t.Fatal(ready)
	}
	if w := dashboardRequest(s, "PUT", "/api/stage-media", fmt.Sprintf(`{"prestream":%q,"shortcuts":[]}`, ready.MediaRevision)); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "POST", "/api/destinations/import", `{"version":0}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "POST", "/api/destinations", `{"version":1,"name":"local","url":"rtmp://localhost/app","stream_key":"test-key","output_mode":"continuous"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	draft := generatorDraft(t, s, 120)
	job := submitGenerator(t, s, draft)
	eventually(t, func() bool {
		j := generatorJob(t, dashboardRequest(s, "GET", "/api/generator/jobs/"+job.ID, ""))
		return j.State == "running" && j.Progress > 0
	})
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool {
		j := generatorJob(t, dashboardRequest(s, "GET", "/api/generator/jobs/"+job.ID, ""))
		return strings.Contains(j.Message, "broadcast capacity") && j.Progress == 0
	})
	draft.Scenes[0].Text = "New draft while captured work waits"
	data, _ := json.Marshal(draft)
	if w := dashboardRequest(s, "PUT", "/api/generator/designs/"+draft.ID, string(data)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	resumed := waitGeneratorJob(t, s, job.ID)
	if resumed.State != "ready" || resumed.DesignRevision != job.DesignRevision || resumed.Design.Scenes[0].Text != job.Design.Scenes[0].Text {
		t.Fatal("interrupted attempt did not resume captured inputs", resumed)
	}
	if w := dashboardRequest(s, "GET", "/api/library/revisions/"+ready.MediaRevision+"/preview", ""); w.Code != 200 {
		t.Fatal("previous ready media lost", w.Code, w.Body.String())
	}
}
