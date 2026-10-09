package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func libraryHTTPStatus(t *testing.T, s *Server) map[string]any {
	t.Helper()
	w := dashboardRequest(s, "GET", "/api/library", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var status map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func preparedRevisionServer(t *testing.T) (*Server, string) {
	t.Helper()
	_, path := preparedClip(t)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "source.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	s := libraryServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.library.worker(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	if w := videoUpload(t, s, "show.mp4", data); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.library.scan()
	return s, readyHTTPRevision(t, s)
}

func TestStageMediaRejectsChangedPreparedRevisionAndReportsSelectionError(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	body := `{"prestream":"` + revision + `","shortcuts":[]}`
	if w := dashboardRequest(s, "PUT", "/api/stage-media", body); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	path := filepath.Join(s.library.root, "revisions", revision+".flv")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if w := dashboardRequest(s, "GET", "/api/library/revisions/"+revision+"/preview", ""); w.Code != 409 {
		t.Fatal("preview silently read changed revision", w.Code)
	}
	if w := dashboardRequest(s, "PUT", "/api/stage-media", body); w.Code != 409 {
		t.Fatal("saved changed revision", w.Code)
	}
	revisions := libraryHTTPStatus(t, s)["revisions"].([]any)
	if revisions[0].(map[string]any)["state"] != "changed" {
		t.Fatal("changed selected revision remains advertised as ready", revisions)
	}
}

func TestStageMediaSettingsAuthenticationAndAtomicSaveFailure(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	for _, path := range []string{"/api/stage-media", "/api/library/prepare", "/api/library/revisions/" + revision + "/preview"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatal("unprotected media route", path, w.Code)
		}
	}
	body := `{"prestream":"` + revision + `","shortcuts":[]}`
	request := httptest.NewRequest("PUT", "/api/stage-media", bytes.NewBufferString(body))
	request.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	request.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, request)
	if w.Code != 403 {
		t.Fatal("settings accepted without same-origin control header", w.Code)
	}
	if w := dashboardRequest(s, "PUT", "/api/stage-media", body); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := dashboardRequest(s, "GET", "/api/stage-media", "").Body.String()
	path := filepath.Join(s.library.root, "stage-media.json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"shortcuts":[]}`); w.Code != 507 {
		t.Fatal("storage failure not reported", w.Code, w.Body.String())
	}
	if after := dashboardRequest(s, "GET", "/api/stage-media", "").Body.String(); before != after {
		t.Fatal("failed save changed settings", after)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	reloaded := New(s.cfg, s.log)
	if after := dashboardRequest(reloaded, "GET", "/api/stage-media", "").Body.String(); before != after {
		t.Fatal("failed save changed persisted settings", after)
	}
}

func TestSavedMediaRevisionSurvivesPreparationAndProfileChanges(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	settings := `{"prestream":"` + revision + `","shortcuts":[]}`
	if w := dashboardRequest(s, "PUT", "/api/stage-media", settings); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	previewURL := "/api/library/revisions/" + revision + "/preview"
	before := dashboardRequest(s, "GET", previewURL, "")
	files := libraryHTTPStatus(t, s)["files"].([]any)
	id := files[0].(map[string]any)["id"].(string)
	if w := dashboardRequest(s, "PUT", "/api/library/prepare", `{"id":"`+id+`"}`); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if after := dashboardRequest(s, "GET", previewURL, ""); after.Code != 200 || !bytes.Equal(before.Body.Bytes(), after.Body.Bytes()) {
		t.Fatal("re-preparation lost or changed retained media", after.Code)
	}
	if w := assetRequest(t, s, map[string]string{"fps": "30"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "PUT", "/api/stage-media", settings); w.Code != 409 {
		t.Fatal("incompatible profile selection was accepted", w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "GET", "/api/stage-media", ""); !bytes.Contains(w.Body.Bytes(), []byte(revision)) {
		t.Fatal("profile change rewrote saved exact selection")
	}
	if w := assetRequest(t, s, map[string]string{"fps": "25"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if after := dashboardRequest(s, "GET", previewURL, ""); after.Code != 200 || !bytes.Equal(before.Body.Bytes(), after.Body.Bytes()) {
		t.Fatal("profile preparation changed retained media", after.Code)
	}
}

func TestLibraryAcceptsCompleteUploadsAndPreparesAfterClientCloses(t *testing.T) {
	_, path := preparedClip(t)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "source.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	s := libraryServer(t)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "upload.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := func(payload []byte, ctx context.Context) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/library/upload", bytes.NewReader(payload)).WithContext(ctx)
		r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
		r.Header.Set("X-Restreamer-Control", "1")
		r.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := request(body.Bytes()[:body.Len()/2], context.Background()); w.Code != 400 {
		t.Fatal("interrupted upload was accepted as a preparation job", w.Code, w.Body.String())
	}
	if files := libraryHTTPStatus(t, s)["files"].([]any); len(files) != 0 {
		t.Fatal("interrupted upload appeared in library")
	}
	client, closeClient := context.WithCancel(context.Background())
	if w := request(body.Bytes(), client); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	closeClient()
	worker, stopWorker := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.library.worker(worker); close(done) }()
	defer func() { stopWorker(); <-done }()
	s.library.scan()
	revision := readyHTTPRevision(t, s)
	if w := dashboardRequest(s, "GET", "/api/library/revisions/"+revision+"/preview", ""); w.Code != 200 {
		t.Fatal("closed upload client prevented preparation", w.Code, w.Body.String())
	}
}

func TestCandidatePreviewIncludesEveryPreparedVideoFrame(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	w := dashboardRequest(s, "GET", "/api/library/revisions/"+revision+"/preview", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	path := filepath.Join(t.TempDir(), "preview.mp4")
	if err := os.WriteFile(path, w.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-count_packets", "-show_entries", "stream=nb_read_packets", "-of", "default=noprint_wrappers=1:nokey=1", path).CombinedOutput()
	if err != nil {
		t.Fatalf("preview probe: %v %s", err, output)
	}
	// The fixture has three seconds of prepared video at 25 frames per second.
	if strings.TrimSpace(string(output)) != "75" {
		t.Fatalf("candidate preview dropped prepared frames: %s", output)
	}
}

func TestStageMediaChangesInvalidateOnlyMatchingOpenConfirmations(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	before := readStage(t, s)
	body := `{"prestream":"` + revision + `","shortcuts":[]}`
	if w := dashboardRequest(s, "PUT", "/api/stage-media", body); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	after := readStage(t, s)
	if after.Context == before.Context {
		t.Fatal("changed selection did not invalidate open confirmations")
	}
	if w := stageRequest(t, s, "go_live", map[string]any{"context": before.Context}); w.Code != 409 {
		t.Fatal("old confirmation activated after selection changed", w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "PUT", "/api/stage-media", body); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if unchanged := readStage(t, s); unchanged.Context != after.Context {
		t.Fatal("identical save unnecessarily invalidated confirmation")
	}
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"ending":"missing"}`); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	if unchanged := readStage(t, s); unchanged.Context != after.Context {
		t.Fatal("failed save invalidated confirmation")
	}
}

func TestLibraryDetectsChangedContentWithUnchangedFilenameSizeAndTimestamp(t *testing.T) {
	_, path := preparedClip(t)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "source.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	s := libraryServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.library.worker(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	if w := videoUpload(t, s, "same.mp4", data); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.library.scan()
	revision := readyHTTPRevision(t, s)
	original := filepath.Join(s.library.root, "originals", "same.mp4")
	info, err := os.Stat(original)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Repeat([]byte("x"), len(data))
	if err := os.WriteFile(original, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(original, time.Now(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	s.library.scan()
	files := libraryHTTPStatus(t, s)["files"].([]any)
	if files[0].(map[string]any)["revision"] == revision {
		t.Fatal("changed file silently reused its earlier ready revision")
	}
}

func readyHTTPRevision(t *testing.T, s *Server) string {
	t.Helper()
	var revision string
	eventually(t, func() bool {
		files := libraryHTTPStatus(t, s)["files"].([]any)
		if len(files) != 1 {
			return false
		}
		file := files[0].(map[string]any)
		if file["state"] != "ready" {
			return false
		}
		revision, _ = file["revision"].(string)
		return true
	})
	if revision == "" {
		t.Fatal("ready media has no exact revision identity")
	}
	return revision
}

func TestStageMediaSelectionsRetainExactRevisionAcrossSourceChangesAndRestart(t *testing.T) {
	_, path := preparedClip(t)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "source.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	s := libraryServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.library.worker(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	if w := videoUpload(t, s, "show.mp4", data); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.library.scan()
	revision := readyHTTPRevision(t, s)
	previewURL := "/api/library/revisions/" + revision + "/preview"
	preview := dashboardRequest(s, "GET", previewURL, "")
	if preview.Code != 200 || preview.Header().Get("Content-Type") != "video/mp4" || !bytes.Contains(preview.Body.Bytes(), []byte("ftyp")) {
		t.Fatalf("candidate cannot be previewed: %d %s", preview.Code, preview.Body.String())
	}
	settings := `{"prestream":"` + revision + `","ending":"` + revision + `","shortcuts":[{"name":"Highlight","revision":"` + revision + `"}]}`
	if w := dashboardRequest(s, "PUT", "/api/stage-media", settings); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"missing","shortcuts":[]}`); w.Code != 409 {
		t.Fatal("invalid media selection was accepted", w.Code, w.Body.String())
	}
	before := dashboardRequest(s, "GET", "/api/stage-media", "")
	if before.Code != 200 || !bytes.Contains(before.Body.Bytes(), []byte(revision)) {
		t.Fatal("failed save changed the selected revision", before.Code, before.Body.String())
	}
	// A file-management change under the same name must not replace saved media.
	if err := os.WriteFile(filepath.Join(s.library.root, "originals", "show.mp4"), []byte("invalid replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	s.library.scan()
	s.library.scan()
	eventually(t, func() bool {
		files := libraryHTTPStatus(t, s)["files"].([]any)
		return files[0].(map[string]any)["state"] == "failed"
	})
	after := dashboardRequest(s, "GET", previewURL, "")
	if after.Code != 200 || !bytes.Equal(after.Body.Bytes(), preview.Body.Bytes()) {
		t.Fatal("changed original replaced or lost the exact ready candidate", after.Code)
	}
	reloaded := New(s.cfg, s.log)
	w := dashboardRequest(reloaded, "GET", "/api/stage-media", "")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), before.Body.Bytes()) {
		t.Fatal("restart lost saved exact selections", w.Code, w.Body.String())
	}
	after = dashboardRequest(reloaded, "GET", previewURL, "")
	if after.Code != 200 || !bytes.Equal(after.Body.Bytes(), preview.Body.Bytes()) {
		t.Fatal("restart lost prepared revision", after.Code)
	}
	var status map[string]any
	if err := json.Unmarshal(dashboardRequest(reloaded, "GET", "/status", "").Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["forwarding"] != false || status["playback"].(map[string]any)["state"] != "idle" {
		t.Fatal("saving or restoring stage media started playback")
	}
}
