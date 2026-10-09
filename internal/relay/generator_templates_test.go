package relay

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func TestContentTemplateCopiesPersistIndependentlyOfDraftsAndThemes(t *testing.T) {
	s := libraryServer(t)
	content := mediaauthor.Design{Name: "Original draft", Stage: "prestream", Theme: mediaauthor.ThemeRef{ID: "retro", Revision: 1}, Scenes: []mediaauthor.Scene{{ID: "welcome", Layout: "list", Text: "Welcome", Items: []string{"First topic", "Second topic"}, DurationSeconds: 10, Font: "go-mono", FontSize: 48, Alignment: "left", ContentRegion: mediaauthor.ContentRegion{WidthPercent: 70, HeightPercent: 75}}}}
	data, _ := json.Marshal(map[string]any{"name": "Weekly show", "content": content})
	w := dashboardRequest(s, "POST", "/api/generator/templates", string(data))
	if w.Code != 201 {
		t.Fatalf("save template: %d %s", w.Code, w.Body.String())
	}
	var template struct {
		ID      string             `json:"id"`
		Version int                `json:"version"`
		Content mediaauthor.Design `json:"content"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &template); err != nil {
		t.Fatal(err)
	}
	if template.Content.Theme.ID != "" || template.Content.Name != "" {
		t.Fatal("template retained draft identity or theme")
	}
	savedTemplate := w.Body.String()
	copyBody := `{"name":"Friday","version":1,"theme":{"id":"retro","revision":1}}`
	w = dashboardRequest(s, "POST", "/api/generator/templates/"+template.ID+"/designs", copyBody)
	if w.Code != 201 {
		t.Fatalf("copy: %d %s", w.Code, w.Body.String())
	}
	var first mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &first)
	if first.Theme.ID != "retro" || first.Version != 1 || first.Scenes[0].Items[1] != "Second topic" || first.Scenes[0].FontSize != 48 || first.Scenes[0].ContentRegion.WidthPercent != 70 {
		t.Fatal("copy lost scene content or separately selected theme", first)
	}
	originalCopy := w.Body.String()
	w = dashboardRequest(s, "POST", "/api/generator/templates/"+template.ID+"/designs", copyBody)
	var second mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &second)
	if second.ID == first.ID {
		t.Fatal("copies share identity")
	}
	second.Scenes[0].Items[0] = "Show-specific edit"
	data, _ = json.Marshal(second)
	if w = dashboardRequest(s, "PUT", "/api/generator/designs/"+second.ID, string(data)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	restarted := New(s.cfg, s.log)
	if w = dashboardRequest(restarted, "GET", "/api/generator/templates/"+template.ID, ""); w.Body.String() != savedTemplate {
		t.Fatal("draft autosave mutated template", w.Body.String())
	}
	if w = dashboardRequest(restarted, "GET", "/api/generator/designs/"+first.ID, ""); w.Body.String() != originalCopy {
		t.Fatal("editing one copy mutated another", w.Body.String())
	}
}

func TestContentTemplateStartersAreEditableAndUpdatesRequireExplicitVersion(t *testing.T) {
	s := libraryServer(t)
	list := dashboardRequest(s, "GET", "/api/generator/templates", "")
	var templates []ContentTemplate
	if err := json.Unmarshal(list.Body.Bytes(), &templates); err != nil {
		t.Fatal(err)
	}
	if len(templates) != 2 {
		t.Fatalf("want two built-in starters: %s", list.Body.String())
	}
	for _, starter := range templates {
		count := 2
		if starter.Content.Stage == "prestream" {
			count = 3
		}
		if !starter.Builtin || len(starter.Content.Scenes) != count {
			t.Fatal("starter missing editable scene sequence", starter)
		}
		w := dashboardRequest(s, "POST", "/api/generator/templates/"+starter.ID+"/designs", `{"name":"My show","version":1,"theme":{"id":"retro","revision":1}}`)
		if w.Code != 201 {
			t.Fatalf("copy starter: %d %s", w.Code, w.Body.String())
		}
		var d mediaauthor.Design
		json.Unmarshal(w.Body.Bytes(), &d)
		d.Scenes[0].Text = "My own welcome"
		d.Scenes = d.Scenes[:1]
		data, _ := json.Marshal(d)
		if w = dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(data)); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		payload, _ := json.Marshal(ContentTemplate{Name: "My reusable show", Content: d})
		w = dashboardRequest(s, "POST", "/api/generator/templates", string(payload))
		if w.Code != 201 {
			t.Fatal(w.Body.String())
		}
		var saved ContentTemplate
		json.Unmarshal(w.Body.Bytes(), &saved)
		stale := w.Body.String()
		saved.Content.Scenes[0].Text = "Updated template"
		payload, _ = json.Marshal(saved)
		w = dashboardRequest(s, "PUT", "/api/generator/templates/"+saved.ID, string(payload))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		updated := w.Body.String()
		if w = dashboardRequest(s, "PUT", "/api/generator/templates/"+saved.ID, stale); w.Code != 409 {
			t.Fatalf("stale update: %d", w.Code)
		}
		if w = dashboardRequest(s, "GET", "/api/generator/templates/"+saved.ID, ""); w.Body.String() != updated {
			t.Fatal("stale update overwrote template")
		}
		if w = dashboardRequest(s, "GET", "/api/generator/designs/"+d.ID, ""); !strings.Contains(w.Body.String(), "My own welcome") {
			t.Fatal("explicit template update changed existing copy")
		}
		if w = dashboardRequest(s, "POST", "/api/generator/templates/"+saved.ID+"/designs", `{"name":"Stale","version":1,"theme":{"id":"retro","revision":1}}`); w.Code != 409 {
			t.Fatal("stale template was silently copied")
		}
		if w = dashboardRequest(s, "PUT", "/api/generator/templates/"+starter.ID, stale); w.Code != 409 {
			t.Fatal("built-in starter overwritten")
		}
	}
}

func TestTemplateUpdatesCannotChangeCapturedGenerationInputs(t *testing.T) {
	s := libraryServer(t)
	w := dashboardRequest(s, "POST", "/api/generator/templates/starter-ending/designs", `{"name":"Captured ending","version":1,"theme":{"id":"retro","revision":1}}`)
	var d mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &d)
	encoded, _ := json.Marshal(ContentTemplate{Name: "Closing", Content: d})
	w = dashboardRequest(s, "POST", "/api/generator/templates", string(encoded))
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	var template ContentTemplate
	json.Unmarshal(w.Body.Bytes(), &template)
	w = dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":1}`, d.ID))
	if w.Code != 202 {
		t.Fatalf("capture: %d %s", w.Code, w.Body.String())
	}
	var captured generatorJobView
	json.Unmarshal(w.Body.Bytes(), &captured)
	template.Content.Scenes[0].Text = "Changed reusable thanks"
	encoded, _ = json.Marshal(template)
	if w = dashboardRequest(s, "PUT", "/api/generator/templates/"+template.ID, string(encoded)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	restarted := New(s.cfg, s.log)
	job := generatorJob(t, dashboardRequest(restarted, "GET", "/api/generator/jobs/"+captured.ID, ""))
	if job.Design.Scenes[0].Text != "Thanks for watching!" || job.DesignRevision != captured.DesignRevision {
		t.Fatal("template edit mutated captured job", job)
	}
}

func TestTemplateControlsRejectUntrustedRequestsAndSaveFailures(t *testing.T) {
	s := libraryServer(t)
	for _, path := range []string{"/api/generator/templates", "/api/generator/templates/starter-ending", "/api/generator/templates/starter-ending/designs"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("unprotected %s: %d", path, w.Code)
		}
		method := "POST"
		if path == "/api/generator/templates/starter-ending" {
			method = "PUT"
		}
		r = httptest.NewRequest(method, path, strings.NewReader(`{}`))
		r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://attacker.invalid")
		r.Header.Set("X-Restreamer-Control", "1")
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("cross-origin %s: %d", path, w.Code)
		}
	}
	if w := dashboardRequest(s, "POST", "/api/generator/templates", `{"name":"`+strings.Repeat("x", 65536)+`"}`); w.Code != 400 {
		t.Fatal("oversized template accepted")
	}
	if w := dashboardRequest(s, "POST", "/api/generator/templates/starter-ending/designs", `{"name":"No selected theme","version":1}`); w.Code != 422 {
		t.Fatal("template copy supplied an implicit theme")
	}
	body := `{"name":"Stored","content":{"stage":"ending","scenes":[]}}`
	w := dashboardRequest(s, "POST", "/api/generator/templates", body)
	var template ContentTemplate
	json.Unmarshal(w.Body.Bytes(), &template)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	saved := w.Body.String()
	root := filepath.Join(s.cfg.BRB.Directory, "library", "generator", "templates")
	if err := os.Rename(root, root+"-offline"); err != nil {
		t.Fatal(err)
	}
	if w = dashboardRequest(s, "PUT", "/api/generator/templates/"+template.ID, saved); w.Code < 400 {
		t.Fatal("storage failure acknowledged")
	}
	if err := os.Rename(root+"-offline", root); err != nil {
		t.Fatal(err)
	}
	if w = dashboardRequest(s, "GET", "/api/generator/templates/"+template.ID, ""); w.Body.String() != saved {
		t.Fatal("failed update changed saved template")
	}
}

func TestTemplateEnvelopePreservesAValidNearLimitDesign(t *testing.T) {
	s := libraryServer(t)
	d := mediaauthor.Design{Name: "Large reusable composition", Stage: "prestream", Theme: mediaauthor.ThemeRef{ID: "retro", Revision: 1}}
	for i := 0; i < 20; i++ {
		d.Scenes = append(d.Scenes, mediaauthor.Scene{ID: fmt.Sprintf("scene-%d", i), Layout: "title", DurationSeconds: 1})
	}
	encoded, _ := json.Marshal(d)
	remaining := maxDesignBytes - 128 - len(encoded)
	for i := range d.Scenes {
		n := remaining
		if n > 4000 {
			n = 4000
		}
		d.Scenes[i].Text = strings.Repeat("x", n)
		remaining -= n
	}
	encoded, _ = json.Marshal(d)
	w := dashboardRequest(s, "POST", "/api/generator/designs", string(encoded))
	if w.Code != 201 {
		t.Fatalf("valid near-limit draft: %d %s", w.Code, w.Body.String())
	}
	var saved mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &saved)
	payload, _ := json.Marshal(ContentTemplate{Name: strings.Repeat("T", 180), Content: saved})
	if len(payload) <= maxDesignBytes {
		t.Fatal("fixture did not exercise template envelope overhead")
	}
	w = dashboardRequest(s, "POST", "/api/generator/templates", string(payload))
	if w.Code != 201 {
		t.Fatalf("template envelope rejected valid content: %d %s", w.Code, w.Body.String())
	}
	var template ContentTemplate
	json.Unmarshal(w.Body.Bytes(), &template)
	template.Name = strings.Repeat("U", 180)
	payload, _ = json.Marshal(template)
	if w = dashboardRequest(s, "PUT", "/api/generator/templates/"+template.ID, string(payload)); w.Code != 200 {
		t.Fatalf("near-limit template update: %d %s", w.Code, w.Body.String())
	}
	restarted := New(s.cfg, s.log)
	w = dashboardRequest(restarted, "GET", "/api/generator/templates/"+template.ID, "")
	if w.Code != 200 {
		t.Fatalf("reload: %d %s", w.Code, w.Body.String())
	}
	w = dashboardRequest(restarted, "POST", "/api/generator/templates/"+template.ID+"/designs", `{"name":"Recovered","version":2,"theme":{"id":"retro","revision":1}}`)
	if w.Code != 201 {
		t.Fatalf("copy: %d %s", w.Code, w.Body.String())
	}
	var copy mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &copy)
	if !reflect.DeepEqual(copy.Scenes, saved.Scenes) {
		t.Fatal("near-limit content lost during template round-trip")
	}
}

func TestDesignEncodingLimitIsAnInputErrorAndPreservesSavedDraft(t *testing.T) {
	s := libraryServer(t)
	initial := dashboardRequest(s, "POST", "/api/generator/designs", `{"name":"Last good","stage":"prestream"}`)
	var d mediaauthor.Design
	json.Unmarshal(initial.Body.Bytes(), &d)
	rawScenes := `[{"id":"one","layout":"title","duration_seconds":1,"text":"` + strings.Repeat("<", 4096) + `"},{"id":"two","layout":"title","duration_seconds":1,"text":"` + strings.Repeat("<", 4096) + `"},{"id":"three","layout":"title","duration_seconds":1,"text":"` + strings.Repeat("<", 4096) + `"}]`
	body := fmt.Sprintf(`{"name":"Escaped content","stage":"prestream","version":%d,"theme":{"id":"retro","revision":1},"scenes":%s}`, d.Version, rawScenes)
	for _, request := range []struct{ method, path string }{{"POST", "/api/generator/designs"}, {"PUT", "/api/generator/designs/" + d.ID}} {
		w := dashboardRequest(s, request.method, request.path, body)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "64 KiB") {
			t.Fatalf("encoding bound is not actionable input error: %d %s", w.Code, w.Body.String())
		}
	}
	if w := dashboardRequest(s, "GET", "/api/generator/designs/"+d.ID, ""); w.Body.String() != initial.Body.String() {
		t.Fatal("invalid-size save changed last good draft")
	}
}
