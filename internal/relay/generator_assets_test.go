package relay

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type assetView struct {
	ID        string `json:"id"`
	Revision  int    `json:"revision"`
	Revisions []struct {
		Revision int    `json:"revision"`
		Digest   string `json:"sha256"`
	} `json:"revisions"`
	Uses []struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	} `json:"uses"`
}

func assetPNG(t *testing.T, c color.Color) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 80, 60))
	for y := 10; y < 50; y++ {
		for x := 10; x < 70; x++ {
			im.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func assetUpload(t *testing.T, s *Server, path, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	m := multipart.NewWriter(&b)
	p, err := m.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	p.Write(data)
	m.Close()
	r := httptest.NewRequest("POST", path, &b)
	r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	r.Header.Set("Content-Type", m.FormDataContentType())
	r.Header.Set("X-Restreamer-Control", "1")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func uploadedAsset(t *testing.T, w *httptest.ResponseRecorder) assetView {
	t.Helper()
	if w.Code != 201 {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	var a assetView
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	return a
}
func TestGeneratorImageRevisionsAreImmutableAndUnusedAssetsCanBeDeleted(t *testing.T) {
	s := libraryServer(t)
	data := assetPNG(t, color.NRGBA{R: 255, A: 255})
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "logo.png", data))
	if a.Revision != 1 || len(a.Revisions) != 1 {
		t.Fatal("missing image revision", a)
	}
	original := dashboardRequest(s, "GET", "/api/generator/assets/"+a.ID+"/revisions/1", "")
	if original.Code != 200 {
		t.Fatal(original.Code, original.Body.String())
	}
	decoded, err := png.Decode(bytes.NewReader(original.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, alpha := decoded.At(0, 0).RGBA()
	if alpha != 0 {
		t.Fatal("logo transparency lost")
	}
	replacement := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets/"+a.ID+"/revisions?version=1", "logo.png", assetPNG(t, color.NRGBA{B: 255, A: 255})))
	if replacement.ID != a.ID || replacement.Revision != 2 {
		t.Fatal("replacement lost asset identity", replacement)
	}
	if w := assetUpload(t, s, "/api/generator/assets/"+a.ID+"/revisions?version=1", "logo.png", data); w.Code != 409 {
		t.Fatal("stale replacement accepted", w.Code)
	}
	if w := dashboardRequest(s, "GET", "/api/generator/assets/"+a.ID+"/revisions/1", ""); !bytes.Equal(w.Body.Bytes(), original.Body.Bytes()) {
		t.Fatal("replacement mutated revision1")
	}
	sameName := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "logo.png", data))
	if sameName.ID == a.ID {
		t.Fatal("filename silently replaced another asset")
	}
	restarted := New(s.cfg, s.log)
	w := dashboardRequest(restarted, "GET", "/api/generator/assets", "")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(a.ID)) {
		t.Fatal("asset history lost afterrestart", w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "DELETE", "/api/generator/assets/"+sameName.ID, `{}`); w.Code != 204 {
		t.Fatal("unused deletion failed", w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "GET", "/api/generator/assets/"+sameName.ID+"/revisions/1", ""); w.Code != 404 {
		t.Fatal("deleted asset still exposed", w.Code)
	}
}

func TestGeneratorImageLayoutsCapturePinnedRevisionsAndProtectUses(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "sponsor.png", assetPNG(t, color.NRGBA{R: 255, A: 255})))
	d := generatorDraft(t, s, 1)
	d.Scenes[0].Layout = "text-image"
	d.Scenes[0].Image = &mediaauthor.AssetRef{ID: a.ID, Revision: 1}
	d.Scenes = append(d.Scenes, mediaauthor.Scene{ID: "full", Layout: "media", DurationSeconds: 1, Image: &mediaauthor.AssetRef{ID: a.ID, Revision: 1}})
	data, _ := json.Marshal(d)
	w := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(data))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &d)
	previews := []image.Image{}
	for i := range d.Scenes {
		data, _ = json.Marshal(d)
		p := dashboardRequest(s, "POST", fmt.Sprintf("/api/generator/preview?scene=%d", i), string(data))
		if p.Code != 200 {
			t.Fatalf("image preview: %d %s", p.Code, p.Body.String())
		}
		im, err := png.Decode(p.Body)
		if err != nil {
			t.Fatal(err)
		}
		previews = append(previews, im)
	}
	if w := dashboardRequest(s, "DELETE", "/api/generator/assets/"+a.ID, `{}`); w.Code != 409 || !bytes.Contains(w.Body.Bytes(), []byte(`"kind":"design"`)) {
		t.Fatal("referenced image deleted", w.Code, w.Body.String())
	}
	w = dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version))
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	job := generatorJob(t, w)
	uploadedAsset(t, assetUpload(t, s, "/api/generator/assets/"+a.ID+"/revisions?version=1", "sponsor.png", assetPNG(t, color.NRGBA{B: 255, A: 255})))
	d.Scenes[0].Image.Revision = 2
	d.Scenes[1].Image = nil
	d.Scenes[1].Layout = "title"
	data, _ = json.Marshal(d)
	w = dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(data))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	generatorServe(t, s)
	job = waitGeneratorJob(t, s, job.ID)
	if job.State != "ready" || job.Design.Scenes[0].Image.Revision != 1 {
		t.Fatalf("asset adoption changed captured job: %+v", job)
	}
	exact := dashboardRequest(s, "GET", "/api/library/revisions/"+job.MediaRevision+"/preview", "")
	path := filepath.Join(t.TempDir(), "images.mp4")
	if err := os.WriteFile(path, exact.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for i, frame := range []int{0, 25} {
		raster, err := exec.Command("ffmpeg", "-v", "error", "-threads", "2", "-i", path, "-vf", fmt.Sprintf("select=eq(n\\,%d)", frame), "-frames:v", "1", "-threads", "2", "-f", "image2pipe", "-c:v", "png", "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := png.Decode(bytes.NewReader(raster))
		if err != nil {
			t.Fatal(err)
		}
		if difference := scenePixelDifference(decoded, previews[i]); difference > 5 {
			t.Fatalf("captured image differs from preview: %g", difference)
		}
	}
	if selected := dashboardRequest(s, "PUT", "/api/stage-media", fmt.Sprintf(`{"prestream":%q,"shortcuts":[]}`, job.MediaRevision)); selected.Code != 204 {
		t.Fatal(selected.Code, selected.Body.String())
	}
	if started := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only", "revision": job.MediaRevision}); started.Code != 200 {
		t.Fatal(started.Code, started.Body.String())
	}
	if catalog := dashboardRequest(s, "GET", "/api/generator/assets", ""); !bytes.Contains(catalog.Body.Bytes(), []byte(`"kind":"on_air"`)) {
		t.Fatal("on-air asset use missing", catalog.Body.String())
	}
	if clip := stageRequest(t, s, "play_clip", map[string]any{"revision": job.MediaRevision}); clip.Code != 200 {
		t.Fatal(clip.Code, clip.Body.String())
	}
	if catalog := dashboardRequest(s, "GET", "/api/generator/assets", ""); !bytes.Contains(catalog.Body.Bytes(), []byte(`"kind":"return"`)) {
		t.Fatal("suspended asset use missing", catalog.Body.String())
	}
	if stopped := stageRequest(t, s, "stop_now", nil); stopped.Code != 200 {
		t.Fatal(stopped.Code)
	}
	json.Unmarshal(w.Body.Bytes(), &d)
	for i := range d.Scenes {
		d.Scenes[i].Image = nil
		d.Scenes[i].Layout = "title"
	}
	data, _ = json.Marshal(d)
	if w := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(data)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "DELETE", "/api/generator/assets/"+a.ID, `{}`); w.Code != 409 || !bytes.Contains(w.Body.Bytes(), []byte(`"kind":"media"`)) {
		t.Fatal("prepared output inputs deleted", w.Code, w.Body.String())
	}
}

func TestGeneratorImageRejectsMalformedOversizedAndUnavailableRevisions(t *testing.T) {
	s := libraryServer(t)
	for _, data := range [][]byte{[]byte("<svg onload='alert(1)'></svg>"), []byte("not an image"), bytes.Repeat([]byte{0}, (10<<20)+1)} {
		if w := assetUpload(t, s, "/api/generator/assets", "bad.png", data); w.Code < 400 {
			t.Fatal("invalid image accepted", w.Code)
		}
	}
	data := assetPNG(t, color.White)
	binary.BigEndian.PutUint32(data[16:20], 100_000)
	binary.BigEndian.PutUint32(data[20:24], 100_000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if w := assetUpload(t, s, "/api/generator/assets", "huge.png", data); w.Code != 422 {
		t.Fatal("oversized decoded image accepted", w.Code, w.Body.String())
	}
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "logo.png", assetPNG(t, color.White)))
	d := generatorDraft(t, s, 1)
	d.Scenes[0].Layout = "media"
	d.Scenes[0].Image = &mediaauthor.AssetRef{ID: a.ID, Revision: 1}
	encoded, _ := json.Marshal(d)
	w := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(encoded))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &d)
	if err := os.Remove(filepath.Join(s.cfg.BRB.Directory, "library", "generator", "assets", a.ID, "1.png")); err != nil {
		t.Fatal(err)
	}
	if w := dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version)); w.Code != 422 || !bytes.Contains(w.Body.Bytes(), []byte("scenes.0.image")) {
		t.Fatal("missing image accepted", w.Code, w.Body.String())
	}
	restarted := New(s.cfg, s.log)
	encoded, _ = json.Marshal(d)
	if w := dashboardRequest(restarted, "POST", "/api/generator/preview", string(encoded)); w.Code != 422 {
		t.Fatal("missing image hidden after restart", w.Code)
	}
}

func TestGeneratorImageReferencesSurviveCancelledAndFailedJobs(t *testing.T) {
	for _, state := range []string{"cancelled", "failed"} {
		t.Run(state, func(t *testing.T) {
			s := libraryServer(t)
			a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "logo.png", assetPNG(t, color.White)))
			d := generatorDraft(t, s, 1)
			d.Scenes[0].Layout = "media"
			d.Scenes[0].Image = &mediaauthor.AssetRef{ID: a.ID, Revision: 1}
			data, _ := json.Marshal(d)
			w := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(data))
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			json.Unmarshal(w.Body.Bytes(), &d)
			w = dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":%d}`, d.ID, d.Version))
			if w.Code != 202 {
				t.Fatal(w.Code, w.Body.String())
			}
			job := generatorJob(t, w)
			if state == "cancelled" {
				if w := dashboardRequest(s, "POST", "/api/generator/jobs/"+job.ID+"/cancel", `{}`); w.Code != 202 {
					t.Fatal(w.Code)
				}
			} else {
				t.Setenv("PATH", t.TempDir())
				generatorServe(t, s)
			}
			job = waitGeneratorJob(t, s, job.ID)
			if job.State != state {
				t.Fatal(job)
			}
			d.Scenes[0].Image = nil
			d.Scenes[0].Layout = "title"
			data, _ = json.Marshal(d)
			if w := dashboardRequest(s, "PUT", "/api/generator/designs/"+d.ID, string(data)); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := dashboardRequest(s, "DELETE", "/api/generator/assets/"+a.ID, `{}`); w.Code != 409 || !bytes.Contains(w.Body.Bytes(), []byte(`"kind":"job"`)) {
				t.Fatal("historical job input deleted", w.Code, w.Body.String())
			}
		})
	}
}

func TestGeneratorImageReadsRejectSameSizeCorruption(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "logo.png", assetPNG(t, color.White)))
	path := filepath.Join(s.cfg.BRB.Directory, "library", "generator", "assets", a.ID, "1.png")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if w := dashboardRequest(s, "GET", "/api/generator/assets/"+a.ID+"/revisions/1", ""); w.Code != 409 {
		t.Fatal("changed exact image served", w.Code)
	}
}

func TestGeneratorImageTemplateReferencesRoundTripAndProtectDeletion(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "template.png", assetPNG(t, color.White)))
	content := mediaauthor.Design{Stage: "prestream", Scenes: []mediaauthor.Scene{{ID: "image", Layout: "media", DurationSeconds: 1, Image: &mediaauthor.AssetRef{ID: a.ID, Revision: 1}}}}
	data, _ := json.Marshal(ContentTemplate{Name: "Image template", Content: content})
	w := dashboardRequest(s, "POST", "/api/generator/templates", string(data))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var template ContentTemplate
	json.Unmarshal(w.Body.Bytes(), &template)
	s = New(s.cfg, s.log)
	w = dashboardRequest(s, "DELETE", "/api/generator/assets/"+a.ID, `{}`)
	if w.Code != 409 || !bytes.Contains(w.Body.Bytes(), []byte(`"kind":"template"`)) {
		t.Fatal("template did not protect asset", w.Code, w.Body.String())
	}
	w = dashboardRequest(s, "POST", "/api/generator/templates/"+template.ID+"/designs", `{"name":"Copy","version":1,"theme":{"id":"retro","revision":1}}`)
	var copied mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &copied)
	if w.Code != 201 || copied.Scenes[0].Image == nil || *copied.Scenes[0].Image != *content.Scenes[0].Image {
		t.Fatal("image ref lost in template copy", w.Code, w.Body.String())
	}
	if err := os.Remove(filepath.Join(s.cfg.BRB.Directory, "library", "generator", "assets", a.ID, "1.png")); err != nil {
		t.Fatal(err)
	}
	for _, req := range []struct{ method, path string }{{"POST", "/api/generator/templates"}, {"PUT", "/api/generator/templates/" + template.ID}} {
		data, _ = json.Marshal(template)
		w = dashboardRequest(s, req.method, req.path, string(data))
		if w.Code != 422 {
			t.Fatal("missing image published in template", req, w.Code, w.Body.String())
		}
	}
	w = dashboardRequest(s, "POST", "/api/generator/templates/"+template.ID+"/designs", `{"name":"Unavailable","version":1,"theme":{"id":"retro","revision":1}}`)
	if w.Code != 422 {
		t.Fatal("missing image copied to draft", w.Code, w.Body.String())
	}
}

func TestGeneratorImageDeletionSerializesWithTemplatePublication(t *testing.T) {
	s := libraryServer(t)
	for range 8 {
		a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "race.png", assetPNG(t, color.White)))
		content := mediaauthor.Design{Stage: "prestream", Scenes: []mediaauthor.Scene{{ID: "image", Layout: "media", DurationSeconds: 1, Image: &mediaauthor.AssetRef{ID: a.ID, Revision: 1}}}}
		data, _ := json.Marshal(ContentTemplate{Name: "Concurrent template", Content: content})
		start := make(chan struct{})
		publication := make(chan int, 1)
		deletion := make(chan int, 1)
		go func() {
			<-start
			publication <- dashboardRequest(s, "POST", "/api/generator/templates", string(data)).Code
		}()
		go func() { <-start; deletion <- dashboardRequest(s, "DELETE", "/api/generator/assets/"+a.ID, `{}`).Code }()
		close(start)
		saved, removed := <-publication, <-deletion
		if !((saved == 201 && removed == 409) || (saved == 422 && removed == 204)) {
			t.Fatalf("inconsistent publication/deletion: %d/%d", saved, removed)
		}
	}
}

func TestGeneratorImageRoutesRequireAuthenticationAndSameOrigin(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "auth.png", assetPNG(t, color.White)))
	for _, path := range []string{"/api/generator/assets", "/api/generator/assets/" + a.ID + "/revisions/1"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatal("unauthenticated asset exposed", path, w.Code)
		}
	}
	r := httptest.NewRequest("DELETE", "/api/generator/assets/"+a.ID, bytes.NewBufferString(`{}`))
	r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	r.Header.Set("Origin", "https://untrusted.example")
	r.Header.Set("X-Restreamer-Control", "1")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin deletion accepted", w.Code)
	}
}
