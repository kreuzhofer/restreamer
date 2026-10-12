package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func TestDeleteDesignRemovesGeneratedFilesButPreservesUploads(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "keep.png", assetPNG(t, color.White)))
	d := generatorDraft(t, s, 1)
	d.Scenes[0].Layout = "text-image"
	d.Scenes[0].Image = &mediaauthor.AssetRef{ID: a.ID, Revision: 1}
	data, _ := json.Marshal(d)
	w := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(data))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	generation := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":2}`, d.ID))
	if generation.Code != 202 {
		t.Fatal(generation.Code, generation.Body.String())
	}
	job := generatorJob(t, generation)
	generatorServe(t, s)
	job = waitGeneratorJob(t, s, job.ID)
	if job.State != "ready" {
		t.Fatal(job)
	}
	w = dashboardRequest(s, "DELETE", "/api/generator/designs/"+d.ID, `{"version":2}`)
	if w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	trash, err := os.ReadDir(filepath.Join(s.cfg.BRB.Directory, "library", ".deletions"))
	if err != nil || len(trash) != 0 {
		t.Fatal("deletion acknowledged before physical cleanup", trash, err)
	}
	for _, server := range []*Server{s, New(s.cfg, s.log)} {
		if w = dashboardRequest(server, "GET", "/api/generator/designs/"+d.ID, ""); w.Code != 404 {
			t.Fatal("design survived", w.Code)
		}
		if w = dashboardRequest(server, "GET", "/api/generator/jobs/"+job.ID, ""); w.Code != 404 {
			t.Fatal("job survived", w.Code)
		}
		if w = dashboardRequest(server, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", ""); w.Code == 200 {
			t.Fatal("output survived")
		}
		if w = dashboardRequest(server, "GET", "/api/generator/assets/"+a.ID+"/revisions/1", ""); w.Code != 200 {
			t.Fatal("upload deleted", w.Code)
		}
	}
	// Physical storage is part of the deletion contract, not just catalog visibility.
	root := filepath.Join(s.cfg.BRB.Directory, "library")
	for _, path := range []string{"generator/designs/" + d.ID + ".json", "generator/jobs/" + job.ID + ".json", "revisions/" + job.MediaRevision + ".flv", "revisions/" + job.MediaRevision + ".json"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("file not removed: %s (%v)", path, err)
		}
	}
	if w = dashboardRequest(s, "DELETE", "/api/generator/assets/"+a.ID, `{}`); w.Code != 204 {
		t.Fatal("upload cannot be managed separately", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "generator/assets", a.ID)); !os.IsNotExist(err) {
		t.Fatal("separate asset deletion left files", err)
	}
}

func TestDeleteDesignRequiresFreshVersionAndUnselectedMedia(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, 1)
	w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":1}`, d.ID))
	generatorServe(t, s)
	job := waitGeneratorJob(t, s, generatorJob(t, w).ID)
	if job.State != "ready" {
		t.Fatal(job)
	}
	for _, selections := range []string{fmt.Sprintf(`{"prestream":%q}`, job.MediaRevision), fmt.Sprintf(`{"shortcuts":[{"name":"Keep","revision":%q}]}`, job.MediaRevision)} {
		if w = dashboardRequest(s, "PUT", "/api/stage-media", selections); w.Code != 204 {
			t.Fatal(w.Code, w.Body.String())
		}
		if w = dashboardRequest(s, "DELETE", "/api/generator/designs/"+d.ID, `{"version":1}`); w.Code != 409 {
			t.Fatal("selected media deletion", w.Code)
		}
		if w = dashboardRequest(s, "GET", "/api/generator/designs/"+d.ID, ""); w.Code != 200 {
			t.Fatal("blocked deletion removed design")
		}
	}
	if w = dashboardRequest(s, "PUT", "/api/stage-media", `{}`); w.Code != 204 {
		t.Fatal(w.Code)
	}
	d.Name = "Newer saved title"
	data, _ := json.Marshal(d)
	if w = dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(data)); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = dashboardRequest(s, "DELETE", "/api/generator/designs/"+d.ID, `{"version":1}`); w.Code != 409 {
		t.Fatal("stale deletion accepted", w.Code)
	}
	for _, authenticated := range []bool{false, true} {
		r := httptest.NewRequest("DELETE", "/api/generator/designs/"+d.ID, bytes.NewBufferString(`{"version":2}`))
		r.Header.Set("Content-Type", "application/json")
		want := 401
		if authenticated {
			r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
			r.Header.Set("Origin", "https://untrusted.example")
			want = 403
		}
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, r)
		if response.Code != want {
			t.Fatal("deletion authorization", response.Code, want)
		}
	}
	if w = dashboardRequest(s, "DELETE", "/api/generator/designs/"+d.ID, `{"version":2}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestDeleteCustomTemplatesAndThemesPreservesReferencedRevisionsAndUploads(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "logo.png", assetPNG(t, color.White)))
	w := dashboardRequest(s, "POST", "/api/generator/themes", `{"name":"Temporary","base":{"id":"retro","revision":1}}`)
	var theme mediaauthor.Theme
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &theme)
	theme.Style.Logo = &mediaauthor.AssetRef{ID: a.ID, Revision: 1}
	data, _ := json.Marshal(theme)
	w = dashboardRequest(s, "PUT", "/api/generator/themes/"+theme.ID, string(data))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	d := generatorDraft(t, s, 1)
	d.Theme = mediaauthor.ThemeRef{ID: theme.ID, Revision: 2}
	data, _ = json.Marshal(d)
	w = dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(data))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = dashboardRequest(s, "DELETE", "/api/generator/themes/"+theme.ID, `{"revision":2}`); w.Code != 409 {
		t.Fatal("referenced theme", w.Code)
	}
	if w = dashboardRequest(s, "DELETE", "/api/generator/designs/"+d.ID, `{"version":2}`); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = dashboardRequest(s, "DELETE", "/api/generator/themes/"+theme.ID, `{"revision":2}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, id := range []string{"retro", "arcade-after-hours", "neon-night"} {
		if w = dashboardRequest(s, "DELETE", "/api/generator/themes/"+id, `{"revision":2}`); w.Code != 409 {
			t.Fatal("built-in deleted", w.Code)
		}
	}
	d.Theme = mediaauthor.ThemeRef{ID: "retro", Revision: 1}
	d.Scenes[0].Image = &mediaauthor.AssetRef{ID: a.ID, Revision: 1}
	d.Scenes[0].Layout = "text-image"
	data, _ = json.Marshal(map[string]any{"name": "Disposable template", "content": d})
	w = dashboardRequest(s, "POST", "/api/generator/templates", string(data))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var template ContentTemplate
	json.Unmarshal(w.Body.Bytes(), &template)
	if w = dashboardRequest(s, "DELETE", "/api/generator/templates/"+template.ID, `{"version":1}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = dashboardRequest(s, "DELETE", "/api/generator/templates/starter-ending", `{"version":1}`); w.Code != 409 {
		t.Fatal("builtin template deleted", w.Code)
	}
	restarted := New(s.cfg, s.log)
	if w = dashboardRequest(restarted, "GET", "/api/generator/templates/"+template.ID, ""); w.Code != 404 {
		t.Fatal("template restored")
	}
	for _, revision := range []int{1, 2} {
		if w = dashboardRequest(restarted, "GET", fmt.Sprintf("/api/generator/themes/%s/revisions/%d", theme.ID, revision), ""); w.Code != 404 {
			t.Fatal("theme restored")
		}
	}
	if w = dashboardRequest(restarted, "DELETE", "/api/generator/assets/"+a.ID, `{}`); w.Code != 204 {
		t.Fatal("upload deletion blocked after owners removed", w.Code, w.Body.String())
	}
}

func TestDeleteGeneratedResultKeepsSharedOutputUntilLastOwner(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, 1)
	generatorServe(t, s)
	var jobs []generatorJobView
	for range 2 {
		w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":1}`, d.ID))
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		job := waitGeneratorJob(t, s, generatorJob(t, w).ID)
		if job.State != "ready" {
			t.Fatal(job)
		}
		jobs = append(jobs, job)
	}
	if jobs[0].MediaRevision != jobs[1].MediaRevision {
		t.Fatal("fixture did not produce shared bytes")
	}
	for i, job := range jobs {
		w := dashboardRequest(s, "DELETE", "/api/generator/jobs/"+job.ID, `{}`)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		w = dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
		if (w.Code == 200) != (i == 0) {
			t.Fatal("shared file lifetime", i, w.Code)
		}
	}
	if w := dashboardRequest(s, "GET", "/api/generator/designs/"+d.ID, ""); w.Code != 200 {
		t.Fatal("deleting a result removed its draft")
	}
}

func TestDeleteDesignStorageFailureLeavesFilesAndDraftIntact(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, 1)
	job := submitGenerator(t, s, d) // queued work is removed without rendering it
	root := filepath.Join(s.cfg.BRB.Directory, "library")
	path := filepath.Join(root, "generator/jobs", job.ID+".json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "unrelated"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	w := dashboardRequest(s, "DELETE", "/api/generator/designs/"+d.ID, `{"version":1}`)
	if w.Code != 507 {
		t.Fatal("unexpected storage acknowledged", w.Code, w.Body.String())
	}
	if w = dashboardRequest(s, "GET", "/api/generator/designs/"+d.ID, ""); w.Code != 200 {
		t.Fatal("partial deletion removed draft")
	}
	if _, err := os.Stat(filepath.Join(path, "unrelated")); err != nil {
		t.Fatal("unexpected directory removed", err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	if w = dashboardRequest(s, "DELETE", "/api/generator/designs/"+d.ID, `{"version":1}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(generatorQueue(t, s)) != 0 {
		t.Fatal("queued generation survived deletion")
	}
}

func TestDeleteDesignRecoversInterruptedFileCleanupAtStartup(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			s := libraryServer(t)
			d := generatorDraft(t, s, 1)
			root := filepath.Join(s.cfg.BRB.Directory, "library")
			dir := filepath.Join(root, ".deletions/delete-interrupted")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := "generator/designs/" + d.ID + ".json"
			manifest, _ := json.Marshal([]string{path})
			if err := os.WriteFile(filepath.Join(dir, "paths.json"), manifest, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(root, path), filepath.Join(dir, "0")); err != nil {
				t.Fatal(err)
			}
			if committed {
				if err := os.WriteFile(filepath.Join(dir, "committed"), []byte("true"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			restarted := New(s.cfg, s.log)
			w := dashboardRequest(restarted, "GET", "/api/generator/designs/"+d.ID, "")
			want := 200
			if committed {
				want = 404
			}
			if w.Code != want {
				t.Fatal("incorrect recovery", w.Code, w.Body.String())
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatal("staged files not cleaned up", err)
			}
		})
	}
}

func TestDeleteGeneratedResultProtectsPlayingAndReturnMedia(t *testing.T) {
	s := libraryServer(t)
	d := generatorDraft(t, s, 1)
	job := submitGenerator(t, s, d)
	generatorServe(t, s)
	job = waitGeneratorJob(t, s, job.ID)
	if job.State != "ready" {
		t.Fatal(job)
	}
	startReplacementPrestream(t, s, job.MediaRevision)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{}`); w.Code != 204 {
		t.Fatal(w.Code)
	}
	for _, action := range []string{"", "brb"} {
		if action != "" {
			if w := stageRequest(t, s, action, nil); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
		}
		before := readStage(t, s)
		w := dashboardRequest(s, "DELETE", "/api/generator/jobs/"+job.ID, `{}`)
		if w.Code != 409 {
			t.Fatal("active/return media deleted", w.Code, w.Body.String())
		}
		after := readStage(t, s)
		if after.Context != before.Context || after.Stage != before.Stage {
			t.Fatal("blocked deletion changed broadcast")
		}
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "DELETE", "/api/generator/jobs/"+job.ID, `{}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestDeleteDesignRacingGenerationAdmissionCannotLeaveAnOrphanJob(t *testing.T) {
	s := libraryServer(t)
	for range 5 {
		d := generatorDraft(t, s, 1)
		start := make(chan struct{})
		admission := make(chan int, 1)
		deletion := make(chan int, 1)
		go func() {
			<-start
			admission <- dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":1}`, d.ID)).Code
		}()
		go func() {
			<-start
			deletion <- dashboardRequest(s, "DELETE", "/api/generator/designs/"+d.ID, `{"version":1}`).Code
		}()
		close(start)
		if code := <-admission; code != 202 && code != 404 {
			t.Fatal("admission", code)
		}
		if code := <-deletion; code != 200 {
			t.Fatal("deletion", code)
		}
		if len(generatorQueue(t, s)) != 0 {
			t.Fatal("job escaped cascading deletion")
		}
	}
}
