package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func TestBRBThemePreparationRequiresExplicitActivationAndPinsRevision(t *testing.T) {
	s := libraryServer(t)
	before := dashboardRequest(s, "GET", "/api/dashboard", "")
	var original struct {
		Assets brbSettings `json:"brb_assets"`
	}
	json.Unmarshal(before.Body.Bytes(), &original)
	created := dashboardRequest(s, "POST", "/api/generator/themes", `{"name":"BRB night","base":{"id":"retro","revision":2}}`)
	var theme map[string]any
	json.Unmarshal(created.Body.Bytes(), &theme)
	body := fmt.Sprintf(`{"theme":{"id":%q,"revision":1},"base_generation":%q}`, theme["id"], original.Assets.Generation)
	prepared := dashboardRequest(s, "POST", "/api/brb/theme/prepare", body)
	if prepared.Code != 201 {
		t.Fatalf("prepare: %d %s", prepared.Code, prepared.Body.String())
	}
	var candidate struct {
		ID       string      `json:"id"`
		Base     string      `json:"base_generation"`
		Settings brbSettings `json:"settings"`
	}
	json.Unmarshal(prepared.Body.Bytes(), &candidate)
	unchanged := dashboardRequest(s, "GET", "/api/dashboard", "")
	var current struct {
		Assets brbSettings `json:"brb_assets"`
	}
	json.Unmarshal(unchanged.Body.Bytes(), &current)
	if current.Assets.Generation != original.Assets.Generation {
		t.Fatal("preparation activated BRB")
	}
	sharedDesign := mediaauthor.Design{Name: "Shared BRB", Stage: "ending", Theme: mediaauthor.ThemeRef{ID: theme["id"].(string), Revision: 1}, Scenes: []mediaauthor.Scene{{ID: "brb", Layout: "title", Text: original.Assets.Text, DurationSeconds: 4}}}
	sharedBody, _ := json.Marshal(sharedDesign)
	expected := dashboardRequest(s, "POST", "/api/generator/preview?frame=20", string(sharedBody))
	style := theme["style"].(map[string]any)
	style["background_color"] = "#234567"
	data, _ := json.Marshal(theme)
	if w := dashboardRequest(s, "PUT", "/api/generator/themes/"+theme["id"].(string), string(data)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	preview := dashboardRequest(s, "GET", "/api/brb/theme/candidate/preview", "")
	if preview.Code != 200 || preview.Header().Get("Content-Type") != "video/mp4" || len(preview.Body.Bytes()) < 1000 {
		t.Fatalf("exact preview: %d", preview.Code)
	}
	path := filepath.Join(t.TempDir(), "preview.mp4")
	if err := os.WriteFile(path, preview.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	raster, err := exec.Command("ffmpeg", "-v", "error", "-threads", "2", "-i", path, "-vf", `select=eq(n\,20)`, "-frames:v", "1", "-threads", "2", "-f", "image2pipe", "-c:v", "png", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := png.Decode(bytes.NewReader(raster))
	if err != nil {
		t.Fatal(err)
	}
	want, err := png.Decode(expected.Body)
	if err != nil {
		t.Fatal(expected.Code, err)
	}
	if difference := scenePixelDifference(actual, want); difference > 5 {
		t.Fatalf("BRB differs from shared theme rendering: %.2f", difference)
	}
	activate := fmt.Sprintf(`{"id":%q,"base_generation":%q}`, candidate.ID, candidate.Base)
	if w := dashboardRequest(s, "POST", "/api/brb/theme/activate", activate); w.Code != 204 {
		t.Fatalf("activate: %d %s", w.Code, w.Body.String())
	}
	final := dashboardRequest(s, "GET", "/api/dashboard", "")
	var got map[string]any
	json.Unmarshal(final.Body.Bytes(), &got)
	assets := got["brb_assets"].(map[string]any)
	if assets["generation"] != candidate.ID || assets["text"] != original.Assets.Text || assets["volume"] != float64(original.Assets.Volume) || assets["theme"].(map[string]any)["revision"] != float64(1) {
		t.Fatal("activation lost content or adopted a newer theme", assets)
	}
	restarted := New(s.cfg, s.log)
	final = dashboardRequest(restarted, "GET", "/api/dashboard", "")
	json.Unmarshal(final.Body.Bytes(), &got)
	if got["brb_assets"].(map[string]any)["generation"] != candidate.ID {
		t.Fatal("startup regenerated themed media instead of retaining exact prepared generation")
	}
}

func TestBRBThemeAssetsAreRetainedAndCustomContentSurvives(t *testing.T) {
	s := libraryServer(t)
	logo := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets", "logo.png", assetPNG(t, color.NRGBA{R: 255, A: 255})))
	theme := mediaauthor.RetroTheme(2)
	theme.Style.Logo = &mediaauthor.AssetRef{ID: logo.ID, Revision: 1}
	payload, _ := json.Marshal(map[string]any{"name": "BRB with logo", "base": mediaauthor.ThemeRef{ID: "retro", Revision: 2}, "style": theme.Style})
	created := dashboardRequest(s, "POST", "/api/generator/themes", string(payload))
	json.Unmarshal(created.Body.Bytes(), &theme)
	custom := assetPNG(t, color.NRGBA{B: 255, A: 255})
	if w := assetRequest(t, s, map[string]string{"text": "MY BREAK", "volume": "23"}, "custom.png", custom); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	current := brbHTTPSettings(t, s)
	request := fmt.Sprintf(`{"theme":{"id":%q,"revision":1},"base_generation":%q}`, theme.ID, current.Generation)
	prepared := dashboardRequest(s, "POST", "/api/brb/theme/prepare", request)
	if prepared.Code != 201 {
		t.Fatal(prepared.Code, prepared.Body.String())
	}
	var candidate brbThemeCandidate
	json.Unmarshal(prepared.Body.Bytes(), &candidate)
	uses := dashboardRequest(s, "GET", "/api/generator/assets", "")
	if !strings.Contains(uses.Body.String(), `"kind":"brb_prepared"`) {
		t.Fatal("prepared BRB usage is missing", uses.Body.String())
	}
	if w := dashboardRequest(s, "DELETE", "/api/generator/assets/"+logo.ID, `{}`); w.Code != 409 {
		t.Fatal("referenced BRB logo deleted")
	}
	body := fmt.Sprintf(`{"id":%q,"base_generation":%q}`, candidate.ID, candidate.Base)
	if w := dashboardRequest(s, "POST", "/api/brb/theme/activate", body); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	uses = dashboardRequest(s, "GET", "/api/generator/assets", "")
	if !strings.Contains(uses.Body.String(), `"kind":"brb"`) {
		t.Fatal("active BRB usage missing")
	}
	got := brbHTTPSettings(t, s)
	if !got.CustomImage || got.Text != "MY BREAK" || got.Volume != 23 {
		t.Fatal("theme adoption lost BRB content", got)
	}
	first := dashboardRequest(s, "GET", "/api/brb/image", "").Body.Bytes()
	if w := assetRequest(t, s, map[string]string{"volume": "24"}, "", nil); w.Code != 204 {
		t.Fatal("legacy settings API no longer works with named theme", w.Code, w.Body.String())
	}
	second := dashboardRequest(s, "GET", "/api/brb/image", "").Body.Bytes()
	if !bytes.Equal(first, second) {
		t.Fatal("re-preparation used decorated poster as source and changed custom content")
	}
	if w := assetRequest(t, s, map[string]string{"reset_theme": "true"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	restored := brbHTTPSettings(t, s)
	if restored.Theme != nil || !restored.CustomImage || restored.Text != "MY BREAK" || restored.Volume != 24 {
		t.Fatal("restoring legacy style lost saved content", restored)
	}
	image := dashboardRequest(s, "GET", "/api/brb/image", "")
	if !bytes.Equal(image.Body.Bytes(), custom) {
		t.Fatal("legacy restoration lost original custom image")
	}

}
func brbHTTPSettings(t *testing.T, s *Server) brbSettings {
	t.Helper()
	w := dashboardRequest(s, "GET", "/api/dashboard", "")
	var response struct {
		Settings brbSettings `json:"brb_assets"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil {
		t.Fatal("BRB settings unavailable", w.Code, w.Body.String())
	}
	return response.Settings
}

func TestBRBThemeStaleActivationCancellationAndDiscardPreserveActiveMedia(t *testing.T) {
	s := libraryServer(t)
	settings := brbHTTPSettings(t, s)
	payload := fmt.Sprintf(`{"theme":{"id":"retro","revision":2},"base_generation":%q}`, settings.Generation)
	w := dashboardRequest(s, "POST", "/api/brb/theme/prepare", payload)
	var candidate brbThemeCandidate
	json.Unmarshal(w.Body.Bytes(), &candidate)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	preview := dashboardRequest(s, "GET", "/api/brb/theme/candidate/preview", "").Body.Bytes()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest("POST", "/api/brb/theme/prepare", strings.NewReader(payload)).WithContext(ctx)
	request.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Restreamer-Control", "1")
	recorder := httptest.NewRecorder()
	s.Handler().ServeHTTP(recorder, request)
	if recorder.Code < 400 || brbHTTPSettings(t, s).Generation != settings.Generation {
		t.Fatal("cancelled preparation changed active BRB")
	}
	if got := dashboardRequest(s, "GET", "/api/brb/theme/candidate/preview", ""); !bytes.Equal(preview, got.Body.Bytes()) {
		t.Fatal("cancelled preparation removed previous ready candidate")
	}
	if w = assetRequest(t, s, map[string]string{"volume": "12"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	changed := brbHTTPSettings(t, s)
	activation := fmt.Sprintf(`{"id":%q,"base_generation":%q}`, candidate.ID, candidate.Base)
	if w = dashboardRequest(s, "POST", "/api/brb/theme/activate", activation); w.Code != 409 {
		t.Fatal("stale candidate activated", w.Code)
	}
	if got := dashboardRequest(s, "GET", "/api/brb/theme/candidate/preview", ""); !bytes.Equal(preview, got.Body.Bytes()) {
		t.Fatal("stale candidate unavailable for review")
	}
	if w = dashboardRequest(s, "DELETE", "/api/brb/theme/candidate", fmt.Sprintf(`{"id":%q}`, candidate.ID)); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if brbHTTPSettings(t, s).Generation != changed.Generation {
		t.Fatal("discard removed active generation")
	}
	if w = dashboardRequest(s, "GET", "/api/brb/image", ""); w.Code != 200 {
		t.Fatal("active BRB poster removed")
	}
	files, err := os.ReadDir(s.cfg.BRB.Directory)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, file := range files {
		if strings.HasPrefix(file.Name(), "assets-") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("cancel/discard leaked prepared generations: %d", count)
	}
}

func TestBRBThemeFailedAndCancelledPreparationDoesNotInterruptLocalBroadcast(t *testing.T) {
	destination := newSink(t)
	base := libraryServer(t)
	cfg := base.cfg
	cfg.Targets = []config.Target{destination.target("local")}
	s := New(cfg, base.log)
	generatorServe(t, s)
	d := generatorDraft(t, s, 1)
	ready := waitGeneratorJob(t, s, submitGenerator(t, s, d).ID)
	if ready.State != "ready" {
		t.Fatal(ready)
	}
	if w := dashboardRequest(s, "PUT", "/api/stage-media", fmt.Sprintf(`{"prestream":%q,"shortcuts":[]}`, ready.MediaRevision)); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "real"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "brb", map[string]any{"mode": "real"}); w.Code != 200 {
		t.Fatal("start BRB", w.Code, w.Body.String())
	}
	eventually(t, func() bool { return destination.count() == 1 })
	initial := brbHTTPSettings(t, s)
	payload := fmt.Sprintf(`{"theme":{"id":"retro","revision":2},"base_generation":%q}`, initial.Generation)
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	failed := dashboardRequest(s, "POST", "/api/brb/theme/prepare", payload)
	t.Setenv("PATH", originalPath)
	if failed.Code != 422 {
		t.Fatal("renderer failure not surfaced", failed.Code, failed.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/api/brb/theme/prepare", strings.NewReader(payload)).WithContext(ctx)
	req.SetBasicAuth(cfg.DashboardUsername, cfg.DashboardPassword)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Restreamer-Control", "1")
	result := httptest.NewRecorder()
	s.Handler().ServeHTTP(result, req)
	if result.Code < 400 {
		t.Fatal("cancelled preparation acknowledged")
	}
	if brbHTTPSettings(t, s).Generation != initial.Generation || readStage(t, s).Stage != "BRB" {
		t.Fatal("failed/cancelled preparation interrupted broadcast")
	}
	// Observe fresh destination packets after both failed operations, not merely connection state.
	for len(destination.packets) > 0 {
		<-destination.packets
	}
	select {
	case packet := <-destination.packets:
		if packet.Type != rtmp.Audio && packet.Type != rtmp.Video {
			t.Fatal("unexpected output")
		}
	case <-time.After(time.Second):
		t.Fatal("local destination stopped receiving")
	}
	if destination.count() != 1 {
		t.Fatal("preparation reconnected destination")
	}
	prepared := dashboardRequest(s, "POST", "/api/brb/theme/prepare", payload)
	var readyBRB brbThemeCandidate
	json.Unmarshal(prepared.Body.Bytes(), &readyBRB)
	if prepared.Code != 201 || brbHTTPSettings(t, s).Generation != initial.Generation {
		t.Fatal("successful preparation changed on-air BRB", prepared.Code, prepared.Body.String())
	}
	if preview := dashboardRequest(s, "GET", "/api/brb/theme/candidate/preview", ""); preview.Code != 200 {
		t.Fatal("prepared BRB exact preview unavailable")
	}
	if activated := dashboardRequest(s, "POST", "/api/brb/theme/activate", fmt.Sprintf(`{"id":%q,"base_generation":%q}`, readyBRB.ID, readyBRB.Base)); activated.Code != 204 {
		t.Fatal(activated.Code, activated.Body.String())
	}
	if readStage(t, s).Stage != "BRB" || brbHTTPSettings(t, s).Generation != readyBRB.ID || destination.count() != 1 {
		t.Fatal("reviewed BRB activation changed stage or destination session")
	}

	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestBRBThemeControlsAreAuthenticatedAndSameOrigin(t *testing.T) {
	s := libraryServer(t)
	for _, entry := range []struct{ method, path string }{{"POST", "/api/brb/theme/prepare"}, {"GET", "/api/brb/theme/candidate"}, {"GET", "/api/brb/theme/candidate/preview"}, {"POST", "/api/brb/theme/activate"}, {"DELETE", "/api/brb/theme/candidate"}} {
		req := httptest.NewRequest(entry.method, entry.path, strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatal("unprotected BRB theme route", entry.path, w.Code)
		}
		if entry.method == "GET" {
			continue
		}
		req = httptest.NewRequest(entry.method, entry.path, strings.NewReader(`{}`))
		req.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Restreamer-Control", "1")
		req.Header.Set("Origin", "https://attacker.invalid")
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal("cross-origin theme mutation", entry.path, w.Code)
		}
	}
}

func TestBRBThemeKeepsCompleteMusicLoopAndPreparedCandidateAcrossRestart(t *testing.T) {
	s := libraryServer(t)
	path := filepath.Join(t.TempDir(), "music.wav")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "6", "-ac", "2", path).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	wav, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if w := assetRequest(t, s, map[string]string{"volume": "37"}, "music.wav", wav); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	current := brbHTTPSettings(t, s)
	request := fmt.Sprintf(`{"theme":{"id":"retro","revision":2},"base_generation":%q}`, current.Generation)
	w := dashboardRequest(s, "POST", "/api/brb/theme/prepare", request)
	var candidate struct {
		ID            string  `json:"id"`
		Base          string  `json:"base_generation"`
		AudioDuration float64 `json:"audio_duration"`
		VideoDuration float64 `json:"video_duration"`
	}
	json.Unmarshal(w.Body.Bytes(), &candidate)
	if w.Code != 201 || candidate.AudioDuration < 6 || candidate.AudioDuration > 6.1 || candidate.VideoDuration != 4 {
		t.Fatal("theme truncated music to its visual loop", w.Code, w.Body.String())
	}
	preview := dashboardRequest(s, "GET", "/api/brb/theme/candidate/preview", "")
	previewPath := filepath.Join(t.TempDir(), "preview.mp4")
	os.WriteFile(previewPath, preview.Body.Bytes(), 0600)
	if tonePower(decodePCM(t, previewPath), 1.4, 440) < 1e7 {
		t.Fatal("exact preview lost captured music")
	}
	orphan := filepath.Join(s.cfg.BRB.Directory, "assets-interrupted")
	if err := os.Mkdir(orphan, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "partial"), []byte("interrupted render"), 0600); err != nil {
		t.Fatal(err)
	}
	restart := New(s.cfg, s.log)
	recovered := dashboardRequest(restart, "GET", "/api/brb/theme/candidate", "")
	var got struct {
		ID string `json:"id"`
	}
	json.Unmarshal(recovered.Body.Bytes(), &got)
	if got.ID != candidate.ID {
		t.Fatal("restart lost retained prepared candidate", recovered.Body.String())
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("startup retained interrupted preparation files")
	}
	if w := dashboardRequest(restart, "GET", "/api/brb/theme/candidate/preview", ""); w.Code != 200 {
		t.Fatal("startup cleanup removed prepared candidate preview")
	}

	// Legacy startup regenerates its own media; the candidate remains reviewable but stale.
	if w := dashboardRequest(restart, "POST", "/api/brb/theme/activate", fmt.Sprintf(`{"id":%q,"base_generation":%q}`, candidate.ID, candidate.Base)); w.Code != 409 {
		t.Fatal("restart silently adopted stale base generation", w.Code)
	}
}

type slowBRBPreview struct {
	*httptest.ResponseRecorder
	started, release chan struct{}
	once             sync.Once
}

func (w *slowBRBPreview) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.started); <-w.release })
	return w.ResponseRecorder.Write(data)
}

func TestReplacingBRBCandidateKeepsConcurrentPreviewOpen(t *testing.T) {
	s := libraryServer(t)
	current := brbHTTPSettings(t, s)
	payload := fmt.Sprintf(`{"theme":{"id":"retro","revision":2},"base_generation":%q}`, current.Generation)
	first := dashboardRequest(s, "POST", "/api/brb/theme/prepare", payload)
	if first.Code != 201 {
		t.Fatal(first.Code, first.Body.String())
	}
	var candidate brbThemeCandidate
	json.Unmarshal(first.Body.Bytes(), &candidate)
	expected := dashboardRequest(s, "GET", "/api/brb/theme/candidate/preview", "").Body.Bytes()
	slow := &slowBRBPreview{ResponseRecorder: httptest.NewRecorder(), started: make(chan struct{}), release: make(chan struct{})}
	request := httptest.NewRequest("GET", "/api/brb/theme/candidate/preview?id="+candidate.ID, nil)
	request.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	done := make(chan struct{})
	go func() { s.Handler().ServeHTTP(slow, request); close(done) }()
	select {
	case <-slow.started:
	case <-time.After(time.Second):
		close(slow.release)
		t.Fatal("preview did not begin")
	}
	second := dashboardRequest(s, "POST", "/api/brb/theme/prepare", payload)
	close(slow.release)
	<-done
	if second.Code != 201 {
		t.Fatal(second.Code, second.Body.String())
	}
	if slow.Code != 200 || !bytes.Equal(slow.Body.Bytes(), expected) {
		t.Fatal("replacing candidate truncated an already-open exact preview")
	}
	if w := dashboardRequest(s, "GET", "/api/brb/theme/candidate/preview?id="+candidate.ID, ""); w.Code != 404 {
		t.Fatal("stale preview identity resolved a different candidate")
	}
	files, err := os.ReadDir(s.cfg.BRB.Directory)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, file := range files {
		if strings.HasPrefix(file.Name(), "assets-") {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("candidate replacement exceeded active+prepared retention bound: %d", count)
	}
}

func TestBRBThemeCopiesLargeNormalizedImagesAndRejectsOversizedSources(t *testing.T) {
	s := libraryServer(t)
	if w := assetRequest(t, s, nil, "custom.png", assetPNG(t, color.NRGBA{R: 70, G: 80, B: 90, A: 128})); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	current := brbHTTPSettings(t, s)
	// A valid externally retained PNG fixture larger than the old silent 33 MiB
	// copy limit, without a costly noisy 20MP JPEG/PNG encode. The documented 20MP
	// normalized-image contract permits this pixel count and RGBA encoding.
	raster := image.NewNRGBA(image.Rect(0, 0, 3200, 2800))
	for i := 0; i < len(raster.Pix); i += 4 {
		raster.Pix[i] = 70
		raster.Pix[i+1] = 80
		raster.Pix[i+2] = 90
		raster.Pix[i+3] = 128
	}
	path := filepath.Join(s.cfg.BRB.Directory, current.Generation, "image.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err = encoder.Encode(file, raster); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(original) <= 33<<20 {
		t.Fatal("fixture does not exercise normalized-image copy boundary")
	}
	payload := fmt.Sprintf(`{"theme":{"id":"retro","revision":1},"base_generation":%q}`, current.Generation)
	w := dashboardRequest(s, "POST", "/api/brb/theme/prepare", payload)
	if w.Code != 201 {
		t.Fatalf("large valid image copy failed: %d %s", w.Code, w.Body.String())
	}
	var candidate brbThemeCandidate
	json.Unmarshal(w.Body.Bytes(), &candidate)
	if w = dashboardRequest(s, "POST", "/api/brb/theme/activate", fmt.Sprintf(`{"id":%q,"base_generation":%q}`, candidate.ID, candidate.Base)); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = assetRequest(t, s, map[string]string{"reset_theme": "true"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	restored := dashboardRequest(s, "GET", "/api/brb/image", "")
	if restored.Code != 200 || !bytes.Equal(restored.Body.Bytes(), original) {
		t.Fatal("large original image was truncated through theme/legacy round-trip")
	}
	current = brbHTTPSettings(t, s)
	path = filepath.Join(s.cfg.BRB.Directory, current.Generation, "image.png")
	if err = os.Truncate(path, 193<<20); err != nil {
		t.Fatal(err)
	}
	payload = fmt.Sprintf(`{"theme":{"id":"retro","revision":1},"base_generation":%q}`, current.Generation)
	w = dashboardRequest(s, "POST", "/api/brb/theme/prepare", payload)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "192 MiB") {
		t.Fatal("oversized stored source did not fail explicitly", w.Code, w.Body.String())
	}
	if brbHTTPSettings(t, s).Generation != current.Generation {
		t.Fatal("oversized source replaced active media")
	}
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestBRBThemeRejectsUnmeasuredProfileWithoutChangingLegacyBRB(t *testing.T) {
	s := libraryServer(t)
	if w := assetRequest(t, s, map[string]string{"fps": "60"}, "", nil); w.Code != 204 {
		t.Fatal("legacy60fps profile regressed", w.Code, w.Body.String())
	}
	current := brbHTTPSettings(t, s)
	payload := fmt.Sprintf(`{"theme":{"id":"retro","revision":2},"base_generation":%q}`, current.Generation)
	w := dashboardRequest(s, "POST", "/api/brb/theme/prepare", payload)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "30 fps") {
		t.Fatal("named theme did not report its measured profile boundary", w.Code, w.Body.String())
	}
	after := brbHTTPSettings(t, s)
	if after.Generation != current.Generation || after.Profile.FPS != 60 || after.Theme != nil {
		t.Fatal("unsupported named theme changed legacy profile or media")
	}
	if candidate := dashboardRequest(s, "GET", "/api/brb/theme/candidate", ""); strings.TrimSpace(candidate.Body.String()) != "null" {
		t.Fatal("unsupported profile published a candidate")
	}
}
