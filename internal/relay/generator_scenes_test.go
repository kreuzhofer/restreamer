package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratorSequencePersistsIndependentCopiesAndSelectedPreviews(t *testing.T) {
	s := libraryServer(t)
	payload := `{"name":"Community announcements","stage":"ending","theme":{"id":"retro","revision":1},"scenes":[{"id":"welcome","layout":"title","text":"Community Night","duration_seconds":1},{"id":"announcements","layout":"list","text":"Announcements","items":["Café meetup\nSaturday","Help our library"],"duration_seconds":2,"alignment":"left","content_region":{"width_percent":70,"height_percent":75}},{"id":"copy","layout":"list","text":"Different heading","items":["Café meetup\nSaturday","Help our library"],"duration_seconds":1}]}`
	w := dashboardRequest(s, "POST", "/api/generator/designs", payload)
	if w.Code != 201 {
		t.Fatalf("create sequence: %d %s", w.Code, w.Body.String())
	}
	var draft map[string]any
	json.Unmarshal(w.Body.Bytes(), &draft)
	id := draft["id"].(string)
	scenes := draft["scenes"].([]any)
	scenes[2].(map[string]any)["items"].([]any)[0] = "Independent copy"
	draft["scenes"] = []any{scenes[2], scenes[0], scenes[1]}
	data, _ := json.Marshal(draft)
	saved := dashboardRequest(s, "PUT", "/api/generator/designs/"+id, string(data))
	if saved.Code != 200 {
		t.Fatal(saved.Body.String())
	}
	reloaded := New(s.cfg, s.log)
	got := dashboardRequest(reloaded, "GET", "/api/generator/designs/"+id, "")
	if got.Code != 200 || got.Body.String() != saved.Body.String() {
		t.Fatalf("scene order/content not recovered: %d %s", got.Code, got.Body.String())
	}
	var recovered map[string]any
	json.Unmarshal(got.Body.Bytes(), &recovered)
	last := recovered["scenes"].([]any)[2].(map[string]any)
	if last["items"].([]any)[0] != "Café meetup\nSaturday" {
		t.Fatal("copy edit mutated original", last)
	}
	for i := 0; i < 3; i++ {
		if w := dashboardRequest(s, "POST", fmt.Sprintf("/api/generator/preview?scene=%d", i), got.Body.String()); w.Code != 200 {
			t.Fatalf("scene %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	if w := dashboardRequest(s, "POST", "/api/generator/preview?scene=3", got.Body.String()); w.Code != 400 {
		t.Fatal("invalid selected scene accepted", w.Code)
	}
}

func TestGeneratorSequenceReportsSceneSpecificErrorsAndBoundedInputs(t *testing.T) {
	s := libraryServer(t)
	makeDraft := func(scenes string) string {
		return `{"name":"Composition","stage":"prestream","theme":{"id":"retro","revision":1},"scenes":` + scenes + `}`
	}
	valid := `{"id":"first","layout":"title","text":"Welcome","duration_seconds":10}`
	for _, tc := range []struct{ name, scene, field string }{
		{"empty list", `{"id":"second","layout":"list","duration_seconds":10,"items":[]}`, "items"},
		{"glyph", `{"id":"second","layout":"list","duration_seconds":10,"items":["🦄"]}`, "items.0"},
		{"overflow", `{"id":"second","layout":"list","duration_seconds":10,"items":["` + strings.Repeat("wide", 100) + `"]}`, "items.0"},
		{"region", `{"id":"second","layout":"list","duration_seconds":10,"items":["hello"],"content_region":{"width_percent":101}}`, "content_region"},
		{"alignment", `{"id":"second","layout":"title","duration_seconds":10,"alignment":"random"}`, "alignment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := makeDraft("[" + valid + "," + tc.scene + "]")
			w := dashboardRequest(s, "POST", "/api/generator/validate", body)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "scenes.1."+tc.field) {
				t.Fatalf("field error missing: %d %s", w.Code, w.Body.String())
			}
			if w := dashboardRequest(s, "POST", "/api/generator/preview?scene=0", body); w.Code != 200 {
				t.Fatal("invalid other scene blocks selected preview", w.Code, w.Body.String())
			}
			if w := dashboardRequest(s, "POST", "/api/generator/preview?scene=1", body); w.Code != 422 {
				t.Fatal("invalid selected scene accepted", w.Code)
			}
		})
	}
	if w := dashboardRequest(s, "POST", "/api/generator/validate", makeDraft("[]")); w.Code != 200 || !strings.Contains(w.Body.String(), `"field":"scenes"`) {
		t.Fatal("empty composition not explained", w.Code, w.Body.String())
	}
	oversized := makeDraft("[" + strings.Repeat(valid+",", 20) + valid + "]")
	if w := dashboardRequest(s, "POST", "/api/generator/designs", oversized); w.Code != 400 {
		t.Fatal("more than 20 scenes accepted", w.Code)
	}
}

func TestGeneratorCutSequenceCapturesOrderAndFrameRoundedDuration(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, 1.02)
	d.Scenes[0].Text = "Last scene"
	d.Scenes[0].FontSize = 120
	// Use JSON at the public seam so the test asserts persisted list/region inputs too.
	raw, _ := json.Marshal(d)
	var body map[string]any
	json.Unmarshal(raw, &body)
	title := body["scenes"].([]any)[0]
	body["scenes"] = []any{map[string]any{"id": "list", "layout": "list", "text": "First scene", "items": []string{"München\nCommunity", "Library night"}, "duration_seconds": 1.02, "alignment": "left"}, title}
	encoded, _ := json.Marshal(body)
	saved := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(encoded))
	if saved.Code != 200 {
		t.Fatal(saved.Code, saved.Body.String())
	}
	json.Unmarshal(saved.Body.Bytes(), &d)
	w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version))
	if w.Code != 202 {
		t.Fatalf("generate sequence: %d %s", w.Code, w.Body.String())
	}
	job := generatorJob(t, w)
	if job.Duration != 2.08 {
		t.Fatalf("round each scene to 26frames at25fps, got %g", job.Duration)
	}
	expected := make([]image.Image, 2)
	for i := range expected {
		w := dashboardRequest(s, "POST", fmt.Sprintf("/api/generator/preview?scene=%d", i), saved.Body.String())
		var err error
		expected[i], err = png.Decode(w.Body)
		if err != nil {
			t.Fatal(err)
		}
	}
	// Capture, then edit/reorder the draft before its render begins.
	d.Scenes[0], d.Scenes[1] = d.Scenes[1], d.Scenes[0]
	d.Scenes[0].Text = "Unrelated newer draft"
	encoded, _ = json.Marshal(d)
	if w := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(encoded)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	generatorServe(t, s)
	job = waitGeneratorJob(t, s, job.ID)
	if job.State != "ready" {
		t.Fatalf("sequence failed: %+v", job)
	}
	ready := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
	if ready.Code != 200 {
		t.Fatal(ready.Code, ready.Body.String())
	}
	path := filepath.Join(t.TempDir(), "sequence.mp4")
	if err := os.WriteFile(path, ready.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct{ frame, index int }{{0, 0}, {25, 0}, {26, 1}, {51, 1}} {
		filter := fmt.Sprintf("select=eq(n\\,%d)", sample.frame)
		raster, err := exec.Command("ffmpeg", "-v", "error", "-threads", "2", "-i", path, "-vf", filter, "-frames:v", "1", "-threads", "2", "-f", "image2pipe", "-c:v", "png", "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		actual, err := png.Decode(bytes.NewReader(raster))
		if err != nil {
			t.Fatal(err)
		}
		match, other := scenePixelDifference(actual, expected[sample.index]), scenePixelDifference(actual, expected[1-sample.index])
		if match > 5 || match+0.3 >= other {
			t.Fatalf("frame%d content/order differs: expected error%.3f other%.3f", sample.frame, match, other)
		}
	}
}

func scenePixelDifference(a, b image.Image) float64 {
	var sum float64
	for y := 0; y < a.Bounds().Dy(); y++ {
		for x := 0; x < a.Bounds().Dx(); x++ {
			ar, ag, ab, _ := a.At(x, y).RGBA()
			br, bg, bb, _ := b.At(x, y).RGBA()
			sum += math.Abs(float64(ar)-float64(br)) + math.Abs(float64(ag)-float64(bg)) + math.Abs(float64(ab)-float64(bb))
		}
	}
	return sum / float64(a.Bounds().Dx()*a.Bounds().Dy()*3*257)
}

func TestGeneratorSequenceRejectsEmptyOrExcessTotalDuration(t *testing.T) {
	s := libraryServer(t)
	for _, scenes := range []string{`[]`, `[{"id":"a","layout":"title","duration_seconds":400},{"id":"b","layout":"title","duration_seconds":300}]`} {
		w := dashboardRequest(s, "POST", "/api/generator/designs", `{"name":"Invalid sequence","stage":"ending","theme":{"id":"retro","revision":1},"scenes":`+scenes+`}`)
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
		var d map[string]any
		json.Unmarshal(w.Body.Bytes(), &d)
		w = dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":1}`, d["id"]))
		if w.Code != 422 || !strings.Contains(w.Body.String(), `"field":"scenes"`) {
			t.Fatalf("invalid composition accepted: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestGeneratorListInputBudgetsAreAppliedAcrossHeadingAndItems(t *testing.T) {
	s := libraryServer(t)
	for _, tc := range []struct {
		name, heading string
		items         []string
	}{
		{"item count", "", make([]string, 21)},
		{"shared UTF-8 budget", strings.Repeat("a", 2048), []string{strings.Repeat("é", 1025)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"name": "Bounded list", "stage": "prestream", "theme": map[string]any{"id": "retro", "revision": 1}, "scenes": []any{map[string]any{"id": "list", "layout": "list", "text": tc.heading, "items": tc.items, "duration_seconds": 10}}}
			encoded, _ := json.Marshal(body)
			if w := dashboardRequest(s, "POST", "/api/generator/designs", string(encoded)); w.Code != 400 {
				t.Fatalf("input budget not enforced: %d", w.Code)
			}
		})
	}
}
