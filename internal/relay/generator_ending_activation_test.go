package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func serveEndingActivation(t *testing.T, s *Server) func() {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, listener) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(10 * time.Second):
				t.Error("ending activation fixture did not shut down")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func generatedEndingDesign(t *testing.T, s *Server, seconds float64, text string) mediaauthor.Design {
	t.Helper()
	d := generatorDraft(t, s, seconds)
	d.Stage = "ending"
	d.Scenes[0].Text = text
	return saveMusicDesign(t, s, d)
}
func readyGeneratedEnding(t *testing.T, s *Server, d mediaauthor.Design) generatorJobView {
	t.Helper()
	j := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if j.State != "ready" {
		t.Fatal(j)
	}
	return j
}
func selectGeneratedEnding(t *testing.T, s *Server, prestream, ending string) {
	t.Helper()
	w := dashboardRequest(s, "PUT", "/api/stage-media", fmt.Sprintf(`{"prestream":%q,"ending":%q,"shortcuts":[]}`, prestream, ending))
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func endingReplacementReview(t *testing.T, s *Server, id, revision string) string {
	t.Helper()
	state := readStage(t, s)
	raw, err := json.Marshal(StageCommand{ID: id, ServerID: state.ServerID, Context: state.Context, Action: "replace_now", Revision: revision, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func previewGeneratedEnding(t *testing.T, s *Server, revision string) {
	t.Helper()
	w := dashboardRequest(s, "GET", "/api/library/revisions/"+revision+"/preview", "")
	if w.Code != 200 {
		t.Fatal("ready candidate is no longer reviewable", w.Code, w.Body.String())
	}
	path := filepath.Join(t.TempDir(), "candidate.mp4")
	if err := os.WriteFile(path, w.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil {
		t.Fatal("generated candidate failed to decode", err, string(out))
	}
}

func TestGeneratedEndingReplacementKeepsCurrentMediaAndPostponesShutdownOnce(t *testing.T) {
	for _, mode := range []string{"real", "preview_only"} {
		t.Run(mode, func(t *testing.T) {
			prepared := libraryServer(t)
			destination := newSink(t)
			cfg := prepared.cfg
			cfg.Targets = []config.Target{destination.target("local")}
			s := New(cfg, prepared.log)
			serveEndingActivation(t, s)
			d := generatedEndingDesign(t, s, 4, "Original ending")
			original := readyGeneratedEnding(t, s, d)
			if readStage(t, s).Stage != "OFF" {
				t.Fatal("generation activated a broadcast")
			}
			selectGeneratedEnding(t, s, original.MediaRevision, original.MediaRevision)
			for _, action := range []string{"prestream", "end_stream"} {
				if w := stageRequest(t, s, action, map[string]any{"mode": mode}); w.Code != 200 {
					t.Fatal(action, w.Code, w.Body.String())
				}
			}
			before := readStage(t, s)
			// A new captured theme/content revision is prepared while the old ending runs.
			d.Theme.Revision = 2
			d.Scenes[0].Text = "Corrected ending"
			d.Scenes[0].DurationSeconds = 3
			d = saveMusicDesign(t, s, d)
			candidate := readyGeneratedEnding(t, s, d)
			previewGeneratedEnding(t, s, candidate.MediaRevision)
			after := readStage(t, s)
			if after.Stage != "ENDING" || after.Media.Revision != original.MediaRevision || after.Playback.Epoch != before.Playback.Epoch {
				t.Fatal("preparation or review interrupted running ending", after)
			}
			// Selecting B for the next show is separate from replacing A now.
			selectGeneratedEnding(t, s, original.MediaRevision, candidate.MediaRevision)
			if after := readStage(t, s); after.Media.Revision != original.MediaRevision || after.Playback.Epoch != before.Playback.Epoch {
				t.Fatal("next-use selection replaced active ending", after)
			}
			body := endingReplacementReview(t, s, "generated-ending-once", candidate.MediaRevision)
			eventually(t, func() bool { return readStage(t, s).Playback.Position > 3 })
			if w := dashboardRequest(s, "POST", "/api/stage/commands", body); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			replaced := readStage(t, s)
			if replaced.Stage != "ENDING" || replaced.Mode != mode || replaced.Media.Revision != candidate.MediaRevision || replaced.Playback.Position > .1 || replaced.Playback.Loop {
				t.Fatal("replacement did not restart finite exact ending", replaced)
			}
			if w := stageRequest(t, s, "brb", nil); w.Code != 409 {
				t.Fatal("replacement released ending lock", w.Code)
			}
			eventually(t, func() bool { return readStage(t, s).Playback.Position > 1.1 })
			advanced := readStage(t, s)
			if advanced.Stage != "ENDING" {
				t.Fatal("old ending deadline stopped replacement", advanced)
			}
			if w := dashboardRequest(s, "POST", "/api/stage/commands", body); w.Code != 200 {
				t.Fatal("duplicate command failed reconciliation", w.Code, w.Body.String())
			}
			if replay := readStage(t, s); replay.Playback.Epoch != replaced.Playback.Epoch || replay.Playback.Position < advanced.Playback.Position || replay.Context != replaced.Context {
				t.Fatal("duplicate replacement postponed shutdown again", replay)
			}
			eventually(t, func() bool { return readStage(t, s).Stage == "OFF" })
			finished := readStage(t, s)
			if !finished.Ending.Completed || finished.Ending.Draining {
				t.Fatal("replacement ending did not complete", finished)
			}
			if w := stageRequest(t, s, "replace_now", map[string]any{"revision": original.MediaRevision}); w.Code != 409 {
				t.Fatal("late candidate reopened completed ending", w.Code)
			}
			previewGeneratedEnding(t, s, candidate.MediaRevision)
			if mode == "preview_only" {
				if destination.count() != 0 || len(finished.Ending.Results) != 0 {
					t.Fatal("rehearsal opened a destination", destination.count(), finished)
				}
				for _, output := range readTargetDashboard(t, s).Outputs {
					if output.Attempts != 0 {
						t.Fatal("rehearsal attempted destination", output)
					}
				}
				return
			}
			if destination.count() != 1 || len(finished.Ending.Results) != 1 || !finished.Ending.Results[0].Complete {
				t.Fatal("generated replacement lost independent destination session/completion", destination.count(), finished)
			}
			var packets []*rtmp.Message
			var last [2]time.Duration
			for len(destination.packets) > 0 {
				p := <-destination.packets
				packets = append(packets, p)
				if isVideoFrame(p) || p.Type == rtmp.Audio && len(p.Body) > 1 && p.Body[1] == 1 {
					track := 0
					if p.Type == rtmp.Audio {
						track = 1
					}
					if p.Timestamp < last[track] {
						t.Fatal("replacement reversed destination timestamp", p.Timestamp, last[track])
					}
					last[track] = p.Timestamp
				}
			}
			path := filepath.Join(t.TempDir(), "ending-delivery.flv")
			writeTestFLV(t, path, packets)
			if out, err := exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil {
				t.Fatal("generated replacement destination stream failed decode", err, string(out))
			}
		})
	}
}

func TestGeneratedEndingCandidateSurvivesStaleSelectionCompletionAndSessions(t *testing.T) {
	prepared := libraryServer(t)
	stop := serveEndingActivation(t, prepared)
	a := readyGeneratedEnding(t, prepared, generatedEndingDesign(t, prepared, 1.2, "Original ending"))
	b := readyGeneratedEnding(t, prepared, generatedEndingDesign(t, prepared, 1.6, "Prepared correction"))
	stop()
	for _, change := range []string{"selection", "completed", "stopped", "later session", "server restart"} {
		t.Run(change, func(t *testing.T) {
			s := New(prepared.cfg, prepared.log)
			selectGeneratedEnding(t, s, a.MediaRevision, a.MediaRevision)
			start := func() {
				t.Helper()
				for _, action := range []string{"prestream", "end_stream"} {
					if w := stageRequest(t, s, action, map[string]any{"mode": "preview_only"}); w.Code != 200 {
						t.Fatal(action, w.Code, w.Body.String())
					}
				}
			}
			start()
			body := endingReplacementReview(t, s, "stale-generated-ending", b.MediaRevision)
			switch change {
			case "selection":
				selectGeneratedEnding(t, s, a.MediaRevision, b.MediaRevision)
			case "completed":
				now := time.Now()
				for i := 0; i < 100; i++ {
					s.broadcast.tick(now.Add(time.Duration(i) * 20 * time.Millisecond))
				}
				if state := readStage(t, s); state.Stage != "OFF" || !state.Ending.Completed {
					t.Fatal("ending did not finish", state)
				}
			case "stopped", "later session", "server restart":
				if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				if change == "server restart" {
					s = New(prepared.cfg, prepared.log)
				}
				if change != "stopped" {
					start()
				}
			}
			before := readStage(t, s)
			if w := dashboardRequest(s, "POST", "/api/stage/commands", body); w.Code != 409 {
				t.Fatal("stale review activated candidate", change, w.Code, w.Body.String())
			}
			after := readStage(t, s)
			if after.Stage != before.Stage || after.Context != before.Context || after.Playback.Epoch != before.Playback.Epoch || after.Media.Revision != before.Media.Revision || after.Ending.Completed != before.Ending.Completed {
				t.Fatal("stale activation changed controller", before, after)
			}
			previewGeneratedEnding(t, s, b.MediaRevision)
			// A stale review does not prevent deliberate selection for a later show.
			selectGeneratedEnding(t, s, a.MediaRevision, b.MediaRevision)
			if after := readStage(t, s); after.Stage != before.Stage || after.Playback.Epoch != before.Playback.Epoch || after.Media.Revision != before.Media.Revision {
				t.Fatal("later-use selection activated candidate", after)
			}
			if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

func TestGeneratedEndingCandidateCannotReplaceDuringActualDestinationDrain(t *testing.T) {
	stalled := newDrainDestination(t)
	s, address, media := startDrainRelay(t, []config.Target{stalled.target})
	a := readyGeneratedEnding(t, s, generatedEndingDesign(t, s, 1.2, "Ending to drain"))
	b := readyGeneratedEnding(t, s, generatedEndingDesign(t, s, 1.6, "Late correction"))
	selectGeneratedEnding(t, s, a.MediaRevision, a.MediaRevision)
	publisher := startDrainLive(t, s, address, media)
	<-stalled.ready
	// The existing input seam creates a real blocked output write, not a mocked
	// lifecycle state. Generated A then reaches EOF while that write is outstanding.
	writePacket(t, publisher, blockedWriteKeyframe(media.keyframe, time.Second))
	eventually(t, func() bool { return readTargetDashboard(t, s).InputFrames == 2 })
	if w := stageRequest(t, s, "end_stream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	review := endingReplacementReview(t, s, "review-before-generated-eof", b.MediaRevision)
	eventually(t, func() bool { return readStage(t, s).Ending.Draining })
	before := readStage(t, s)
	if w := dashboardRequest(s, "POST", "/api/stage/commands", review); w.Code != 409 {
		t.Fatal("review restarted draining ending", w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "replace_now", map[string]any{"revision": b.MediaRevision}); w.Code != 409 {
		t.Fatal("fresh command bypassed drain lock", w.Code, w.Body.String())
	}
	if after := readStage(t, s); !after.Ending.Draining || after.Media.Revision != a.MediaRevision || after.Context != before.Context {
		t.Fatal("candidate disturbed active drain", after)
	}
	previewGeneratedEnding(t, s, b.MediaRevision)
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	stopped := readStage(t, s)
	result, ok := endingDestination(stopped, "stalled")
	if stopped.Stage != "OFF" || stopped.Ending.Completed || !ok || result.Complete || result.Reason != "cancelled" {
		t.Fatal("controller lost interrupted drain result", stopped)
	}
	selectGeneratedEnding(t, s, a.MediaRevision, b.MediaRevision)
	if readStage(t, s).Stage != "OFF" {
		t.Fatal("later selection restarted stopped drain")
	}
}

func TestGeneratedEndingFailedCancelledAndIncompatibleCandidatesPreserveActiveRevision(t *testing.T) {
	s := libraryServer(t)
	serveEndingActivation(t, s)
	incompatible := readyGeneratedEnding(t, s, generatedEndingDesign(t, s, 2, "Earlier profile"))
	if w := assetRequest(t, s, map[string]string{"fps": "24"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	d := generatedEndingDesign(t, s, 20, "Keep this ending on air")
	original := readyGeneratedEnding(t, s, d)
	selectGeneratedEnding(t, s, original.MediaRevision, original.MediaRevision)
	for _, action := range []string{"prestream", "end_stream"} {
		if w := stageRequest(t, s, action, map[string]any{"mode": "preview_only"}); w.Code != 200 {
			t.Fatal(action, w.Code, w.Body.String())
		}
	}
	before := readStage(t, s)
	selections := dashboardRequest(s, "GET", "/api/stage-media", "").Body.String()
	if w := stageRequest(t, s, "replace_now", map[string]any{"revision": incompatible.MediaRevision}); w.Code != 409 {
		t.Fatal("incompatible generated result replaced ending", w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "PUT", "/api/stage-media", fmt.Sprintf(`{"prestream":%q,"ending":%q,"shortcuts":[]}`, original.MediaRevision, incompatible.MediaRevision)); w.Code != 409 {
		t.Fatal("incompatible result changed next selection", w.Code, w.Body.String())
	}
	d.Theme.Revision = 2
	d.Scenes[0].Text = "Failed correction"
	d = saveMusicDesign(t, s, d)
	// Missing external encoder is an actual preparation failure while playback
	// continues in native Go. No controller or worker state is injected.
	path := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	failed := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	t.Setenv("PATH", path)
	if failed.State != "failed" || failed.MediaRevision != "" {
		t.Fatal("failed preparation published a candidate", failed)
	}
	d.Scenes[0].Text = "Cancelled correction"
	d.Scenes[0].DurationSeconds = 600
	d = saveMusicDesign(t, s, d)
	queued := submitGenerator(t, s, d)
	eventually(t, func() bool {
		return generatorJob(t, dashboardRequest(s, "GET", "/api/generator/jobs/"+queued.ID, "")).State == "running"
	})
	if w := dashboardRequest(s, "POST", "/api/generator/jobs/"+queued.ID+"/cancel", `{}`); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	cancelled := waitGeneratorJob(t, s, queued.ID)
	if cancelled.State != "cancelled" || cancelled.MediaRevision != "" {
		t.Fatal("cancelled generation published a candidate", cancelled)
	}
	eventually(t, func() bool { return readStage(t, s).Playback.Position > before.Playback.Position+.2 })
	after := readStage(t, s)
	if after.Stage != "ENDING" || after.Media.Revision != original.MediaRevision || after.Playback.Epoch != before.Playback.Epoch || after.Context != before.Context || after.Error != "" || after.Ending.Completed || after.Ending.Draining {
		t.Fatal("candidate failure disturbed current ending", before, after)
	}
	if got := dashboardRequest(s, "GET", "/api/stage-media", "").Body.String(); got != selections {
		t.Fatal("failed candidate changed selected media", got)
	}
	previewGeneratedEnding(t, s, original.MediaRevision)
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
