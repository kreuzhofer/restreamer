package relay

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteLibraryUploadRemovesOnlyItsFiles(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	status := libraryHTTPStatus(t, s)
	file := status["files"].([]any)[0].(map[string]any)
	body, _ := json.Marshal(map[string]any{"version": file["version"]})
	w := dashboardRequest(s, "DELETE", "/api/library/files/"+file["id"].(string), string(body))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(libraryHTTPStatus(t, s)["files"].([]any)) != 0 {
		t.Fatal("deleted upload remains in catalog")
	}
	for _, path := range []string{"originals/show.mp4", "revisions/" + revision + ".json", "revisions/" + revision + ".flv"} {
		if _, err := os.Stat(filepath.Join(s.library.root, path)); !os.IsNotExist(err) {
			t.Fatal("physical file remains", path, err)
		}
	}
	files, err := os.ReadDir(filepath.Join(s.library.root, "prepared"))
	if err != nil || len(files) != 0 {
		t.Fatal("prepared copies remain", files, err)
	}
	restarted := New(s.cfg, s.log)
	if w := dashboardRequest(restarted, "GET", "/api/library/revisions/"+revision+"/preview", ""); w.Code == 200 {
		t.Fatal("deleted revision restored", w.Code)
	}
}

func TestDeleteLibraryUploadProtectsSelectionPlayingAndReturnMedia(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	file := libraryHTTPStatus(t, s)["files"].([]any)[0].(map[string]any)
	body, _ := json.Marshal(map[string]any{"version": file["version"]})
	path := "/api/library/files/" + file["id"].(string)
	startReplacementPrestream(t, s, revision)
	for _, stage := range []string{"selected", "playing", "return"} {
		if stage == "playing" {
			if w := dashboardRequest(s, "PUT", "/api/stage-media", `{}`); w.Code != 204 {
				t.Fatal(w.Code)
			}
		}
		if stage == "return" {
			if w := stageRequest(t, s, "brb", nil); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
		}
		before := readStage(t, s)
		if w := dashboardRequest(s, "DELETE", path, string(body)); w.Code != 409 {
			t.Fatal(stage, w.Code, w.Body.String())
		}
		if after := readStage(t, s); before.Context != after.Context || before.Stage != after.Stage {
			t.Fatal("blocked deletion changed broadcast")
		}
		if _, err := os.Stat(filepath.Join(s.library.root, "originals/show.mp4")); err != nil {
			t.Fatal("protected original removed", err)
		}
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := dashboardRequest(s, "DELETE", path, string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestDeleteLibraryUploadRequiresFreshAuthenticatedSameOriginRequest(t *testing.T) {
	s := libraryServer(t)
	if w := videoUpload(t, s, "invalid.mp4", []byte("invalid video")); w.Code != 202 {
		t.Fatal(w.Code)
	}
	file := libraryHTTPStatus(t, s)["files"].([]any)[0].(map[string]any)
	path := "/api/library/files/" + file["id"].(string)
	body, _ := json.Marshal(map[string]any{"version": file["version"]})
	for _, authenticated := range []bool{false, true} {
		request := httptest.NewRequest("DELETE", path, bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Restreamer-Control", "1")
		want := 401
		if authenticated {
			request.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
			request.Header.Set("Origin", "https://untrusted.example")
			want = 403
		}
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, request)
		if response.Code != want {
			t.Fatal("deletion authorization", response.Code, want)
		}
	}
	if w := dashboardRequest(s, "DELETE", path, `{"version":"old"}`); w.Code != 409 {
		t.Fatal("stale deletion", w.Code)
	}
	if w := dashboardRequest(s, "DELETE", path, string(body)); w.Code != 200 {
		t.Fatal("invalid unprepared upload cannot be cleaned up", w.Code, w.Body.String())
	}
}

func TestDeleteLibraryUploadStorageFailureKeepsOriginalAndRevision(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	file := libraryHTTPStatus(t, s)["files"].([]any)[0].(map[string]any)
	body, _ := json.Marshal(map[string]any{"version": file["version"]})
	path := filepath.Join(s.library.root, "revisions", revision+".json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if w := dashboardRequest(s, "DELETE", "/api/library/files/"+file["id"].(string), string(body)); w.Code != 507 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.library.root, "originals/show.mp4")); err != nil {
		t.Fatal("failed transaction removed original", err)
	}
	if len(libraryHTTPStatus(t, s)["files"].([]any)) != 1 {
		t.Fatal("failed transaction removed catalog entry")
	}
}

func TestDeleteLibraryUploadRejectsHostReplacementWithoutRescan(t *testing.T) {
	for _, mode := range []string{"in-place", "replacement", "identical replacement"} {
		replace := mode != "in-place"
		t.Run(mode, func(t *testing.T) {
			s := libraryServer(t)
			if w := videoUpload(t, s, "reviewed.mp4", []byte("first-invalid-video")); w.Code != 202 {
				t.Fatal(w.Code)
			}
			file := libraryHTTPStatus(t, s)["files"].([]any)[0].(map[string]any)
			body, _ := json.Marshal(map[string]any{"version": file["version"]})
			path := filepath.Join(s.library.root, "originals", "reviewed.mp4")
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			changed := []byte("other-invalid-video")
			if mode == "identical replacement" {
				changed = []byte("first-invalid-video")
			}
			target := path
			if replace {
				target += ".replacement"
			}
			if err := os.WriteFile(target, changed, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			if replace {
				if err := os.Rename(target, path); err != nil {
					t.Fatal(err)
				}
			}
			// Do not refresh the catalog: this is the interval between host edits and discovery.
			w := dashboardRequest(s, "DELETE", "/api/library/files/"+file["id"].(string), string(body))
			if w.Code != 409 {
				t.Fatal("unreviewed replacement deleted", w.Code, w.Body.String())
			}
			if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, changed) {
				t.Fatal("replacement was not restored", string(data), err)
			}
			if len(libraryHTTPStatus(t, s)["files"].([]any)) != 1 {
				t.Fatal("rejected deletion removed catalog entry")
			}
		})
	}
}
