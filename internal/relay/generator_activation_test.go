package relay

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

// Generated revisions have no original upload. Exercise their complete public
// handoff through selection, review, replacement and return in both session modes.
func TestGeneratedPrestreamReviewedActivationPreservesSequence(t *testing.T) {
	for _, mode := range []string{"real", "preview_only"} {
		t.Run(mode, func(t *testing.T) {
			prepared := libraryServer(t)
			sink := newSink(t)
			cfg := prepared.cfg
			cfg.Targets = []config.Target{sink.target("local")}
			s := New(cfg, prepared.log)
			generatorServe(t, s)
			generate := func(name string) generatorJobView {
				d := generatorDraft(t, s, 2)
				d.Name = name
				d.Scenes[0].Text = name
				body, _ := json.Marshal(d)
				saved := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(body))
				if saved.Code != 200 {
					t.Fatal(saved.Code, saved.Body.String())
				}
				if err := json.Unmarshal(saved.Body.Bytes(), &d); err != nil {
					t.Fatal(err)
				}
				w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version))
				if w.Code != 202 {
					t.Fatal(w.Code, w.Body.String())
				}
				job := generatorJob(t, w)
				// Editing after submission cannot alter the captured output or the source.
				d.Scenes[0].Text = "Later unsaved in result"
				body, _ = json.Marshal(d)
				if w := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(body)); w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				job = waitGeneratorJob(t, s, job.ID)
				if job.State != "ready" || job.Design.Scenes[0].Text != name {
					t.Fatal(job)
				}
				return job
			}
			a := generate("Generated original")
			selections := fmt.Sprintf(`{"prestream":%q,"ending":%q,"shortcuts":[]}`, a.MediaRevision, a.MediaRevision)
			if w := dashboardRequest(s, "PUT", "/api/stage-media", selections); w.Code != 204 {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := stageRequest(t, s, "prestream", map[string]any{"mode": mode}); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			before := readStage(t, s)
			b := generate("Generated correction")
			after := readStage(t, s)
			if after.Stage != "PRESTREAM" || after.Media.Revision != a.MediaRevision || after.Playback.Epoch != before.Playback.Epoch {
				t.Fatal("generation changed active playback", after)
			}
			var saved StageMediaSelections
			if got := dashboardRequest(s, "GET", "/api/stage-media", ""); json.Unmarshal(got.Body.Bytes(), &saved) != nil || saved.Prestream != a.MediaRevision || saved.Ending != a.MediaRevision {
				t.Fatal("generation changed saved selections", got.Body.String())
			}
			var catalog LibraryStatus
			w := dashboardRequest(s, "GET", "/api/library", "")
			json.Unmarshal(w.Body.Bytes(), &catalog)
			if len(catalog.Files) != 0 || len(catalog.Revisions) < 2 {
				t.Fatal("generated revision depends on original upload", catalog)
			}
			if w := dashboardRequest(s, "GET", "/api/library/revisions/"+b.MediaRevision+"/preview", ""); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := stageRequest(t, s, "replace_now", map[string]any{"revision": b.MediaRevision, "confirmed": false}); w.Code != 409 {
				t.Fatal("unconfirmed replacement accepted", w.Code)
			}
			review := readStage(t, s)
			body, _ := json.Marshal(StageCommand{ID: "generated-replace", ServerID: review.ServerID, Context: review.Context, Action: "replace_now", Revision: b.MediaRevision, Confirmed: true})
			if w := dashboardRequest(s, "POST", "/api/stage/commands", string(body)); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			replaced := readStage(t, s)
			if replaced.Media.Revision != b.MediaRevision || replaced.Stage != "PRESTREAM" || !replaced.Playback.Loop || replaced.Playback.Position > .1 {
				t.Fatal(replaced)
			}
			if w := dashboardRequest(s, "POST", "/api/stage/commands", string(body)); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if readStage(t, s).Playback.Epoch != replaced.Playback.Epoch {
				t.Fatal("duplicate restarted generated media")
			}
			if w := stageRequest(t, s, "play_clip", map[string]any{"revision": a.MediaRevision, "loop": true}); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
				t.Fatal(w.Code, w.Body.String())
			}
			interrupted := readStage(t, s)
			if w := stageRequest(t, s, "replace_on_return", map[string]any{"revision": a.MediaRevision}); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			retained := readStage(t, s)
			if retained.Media.Revision != interrupted.Media.Revision || retained.Playback.Epoch != interrupted.Playback.Epoch || retained.ReturnStage != "PRESTREAM" || retained.ReturnMedia.Revision != a.MediaRevision || retained.Pending.Deadline != interrupted.Pending.Deadline {
				t.Fatal("return replacement disturbed clip", retained)
			}
			if w := stageRequest(t, s, "stop_clip", nil); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if state := readStage(t, s); state.Stage != "PRESTREAM" || state.Media.Revision != a.MediaRevision || !state.Playback.Loop {
				t.Fatal(state)
			}
			body, _ = json.Marshal(StageCommand{ID: "stale-generated", ServerID: review.ServerID, Context: review.Context, Action: "replace_now", Revision: b.MediaRevision, Confirmed: true})
			if w := dashboardRequest(s, "POST", "/api/stage/commands", string(body)); w.Code != 409 {
				t.Fatal("stale generated review accepted", w.Code)
			}
			if mode == "real" {
				eventually(t, func() bool { return readTargetDashboard(t, s).Outputs[0].Frames >= 30 })
			} else {
				time.Sleep(150 * time.Millisecond)
				if sink.count() != 0 || readTargetDashboard(t, s).Outputs[0].Attempts != 0 {
					t.Fatal("rehearsal connected to destination")
				}
			}
			if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if mode == "real" {
				if sink.count() != 1 {
					t.Fatal("replacement reconnected", sink.count())
				}
				var packets []*rtmp.Message
				var previous [2]time.Duration
				for len(sink.packets) > 0 {
					packet := <-sink.packets
					packets = append(packets, packet)
					if isVideoFrame(packet) || packet.Type == rtmp.Audio && len(packet.Body) > 1 && packet.Body[1] == 1 {
						track := 0
						if packet.Type == rtmp.Audio {
							track = 1
						}
						if packet.Timestamp < previous[track] {
							t.Fatal("timestamp reversed")
						}
						previous[track] = packet.Timestamp
					}
				}
				path := filepath.Join(t.TempDir(), "generated-replacements.flv")
				writeTestFLV(t, path, packets)
				if output, err := exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil {
					t.Fatalf("generated delivery decode: %v %s", err, output)
				}
			}
		})
	}
}
