package relay

import (
	"encoding/json"
	"image/png"
	"strings"
	"testing"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func TestLongAuthoringDraftKeepsQuickPreviewButCannotGenerate(t *testing.T) {
	s := libraryServer(t)
	for _, durations := range [][]float64{{900}, {400, 300}} {
		d := generatorDraft(t, s, durations[0])
		if len(durations) > 1 {
			d.Scenes = append(d.Scenes, mediaauthor.Scene{ID: "second", Layout: "title", Text: "Next", DurationSeconds: durations[1]})
		}
		d = saveMusicDesign(t, s, d)
		raw, _ := json.Marshal(d)
		validation := dashboardRequest(s, "POST", "/api/generator/validate", string(raw))
		if validation.Code != 200 || !strings.Contains(validation.Body.String(), "600 seconds") {
			t.Fatal("missing generation cap", validation.Code, validation.Body.String())
		}
		var detail struct {
			PreviewIssues []mediaauthor.Issue `json:"preview_issues"`
		}
		if err := json.Unmarshal(validation.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(validation.Body.String(), `"preview_issues"`) || len(detail.PreviewIssues) != 0 {
			t.Fatal("editor cannot distinguish quick-preview validation", validation.Body.String())
		}
		for i := range d.Scenes {
			query := "/api/generator/preview"
			if i == 1 {
				query += "?scene=1"
			}
			preview := dashboardRequest(s, "POST", query, string(raw))
			if preview.Code != 200 {
				t.Fatal("valid long scene lost quick preview", preview.Code, preview.Body.String())
			}
			if _, err := png.Decode(preview.Body); err != nil {
				t.Fatal(err)
			}
		}
		request, _ := json.Marshal(map[string]any{"design_id": d.ID, "version": d.Version})
		job := dashboardRequest(s, "POST", "/api/generator/jobs", string(request))
		if job.Code != 422 || !strings.Contains(job.Body.String(), "600 seconds") {
			t.Fatal("long generation admitted", job.Code, job.Body.String())
		}
	}
}

func TestQuickPreviewRetainsTransitionAndEndingValidation(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, 1)
	d.Scenes = append(d.Scenes, mediaauthor.Scene{ID: "next", Layout: "title", Text: "Next", DurationSeconds: 1})
	d.Scenes[0].Transition = &mediaauthor.Transition{Kind: "crossfade", DurationSeconds: 2}
	raw, _ := json.Marshal(d)
	if w := dashboardRequest(s, "POST", "/api/generator/preview", string(raw)); w.Code != 422 || !strings.Contains(w.Body.String(), "transition.duration_seconds") {
		t.Fatal(w.Code, w.Body.String())
	}
	d.Stage = "ending"
	d.Scenes = d.Scenes[:1]
	d.Scenes[0].Transition = nil
	d.EndingFadeSeconds = musicVolume(2)
	raw, _ = json.Marshal(d)
	if w := dashboardRequest(s, "POST", "/api/generator/preview", string(raw)); w.Code != 422 || !strings.Contains(w.Body.String(), "ending_fade_seconds") {
		t.Fatal(w.Code, w.Body.String())
	}
}
