package relay

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"golang.org/x/image/font/gofont/gobold"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func brandZIP(t *testing.T, manifest any, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	w, err := z.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	w.Write(data)
	for name, data := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestBrandPackageRejectsUnsafeAndCorruptArchivesWithoutPublishing(t *testing.T) {
	for _, name := range []string{"../outside.png", "/absolute.png", "assets\\escape.png", "undeclared.png"} {
		t.Run(name, func(t *testing.T) {
			s := libraryServer(t)
			m := map[string]any{"format_version": 1, "id": "unsafe", "version": 1, "name": "Unsafe", "assets": []any{}, "themes": []any{}, "templates": []any{}}
			w := assetUpload(t, s, "/api/generator/packages", "bad.zip", brandZIP(t, m, map[string][]byte{name: []byte("bad")}))
			if w.Code != 422 {
				t.Fatal(w.Code, w.Body.String())
			}
			w = dashboardRequest(s, "GET", "/api/generator/packages", "")
			if w.Body.String() != "[]\n" {
				t.Fatal("failed import published", w.Body.String())
			}
		})
	}
}

func TestBrandPackageRemovalPreservesAReferencedTheme(t *testing.T) {
	s := libraryServer(t)
	imageData := assetPNG(t, color.NRGBA{G: 255, A: 255})
	m := map[string]any{"format_version": 1, "id": "remove-kit", "version": 1, "name": "Remove kit", "assets": []any{map[string]any{"id": "unused", "name": "Unused", "kind": "image", "path": "unused.png", "sha256": fmt.Sprintf("%x", sha256.Sum256(imageData))}}, "themes": []any{map[string]any{"id": "primary", "name": "Primary", "style": mediaauthor.RetroTheme(1).Style}}, "templates": []any{}}
	w := assetUpload(t, s, "/api/generator/packages", "kit.zip", brandZIP(t, m, map[string][]byte{"unused.png": imageData}))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var p struct{ Themes []struct{ ID string } }
	json.Unmarshal(w.Body.Bytes(), &p)
	w = dashboardRequest(s, "POST", "/api/generator/designs", fmt.Sprintf(`{"name":"Keep my design","stage":"prestream","theme":{"id":%q,"revision":1},"scenes":[{"id":"one","layout":"title","text":"Hello","duration_seconds":4}]}`, p.Themes[0].ID))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = dashboardRequest(s, "GET", "/api/generator/packages/remove-kit/versions/1/removal", "")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(p.Themes[0].ID)) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = dashboardRequest(s, "DELETE", "/api/generator/packages/remove-kit/versions/1", `{}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = dashboardRequest(s, "GET", "/api/generator/packages", "")
	if w.Body.String() != "[]\n" {
		t.Fatal(w.Body.String())
	}
	w = dashboardRequest(New(s.cfg, s.log), "GET", "/api/generator/themes/"+p.Themes[0].ID+"/revisions/1", "")
	if w.Code != 200 {
		t.Fatal("referenced theme removed", w.Code, w.Body.String())
	}
}

func TestBrandPackageMotionAndBorderUseExistingPreviewEndpoints(t *testing.T) {
	s := libraryServer(t)
	video := generatorVideoFixture(t, true)
	border := assetPNG(t, color.NRGBA{R: 80, G: 220, A: 255})
	raw, _ := json.Marshal(mediaauthor.RetroTheme(1).Style)
	var style map[string]any
	json.Unmarshal(raw, &style)
	style["motion"] = map[string]any{"asset": map[string]any{"id": "motion", "revision": 1}, "cycle_seconds": 2}
	style["obs_border"] = map[string]any{"id": "border", "revision": 1}
	m := map[string]any{"format_version": 1, "id": "motion-kit", "version": 1, "name": "Motion kit", "assets": []any{map[string]any{"id": "motion", "name": "Silent loop", "kind": "video", "path": "motion.mp4", "sha256": fmt.Sprintf("%x", sha256.Sum256(video))}, map[string]any{"id": "border", "name": "OBS border", "kind": "image", "path": "border.png", "sha256": fmt.Sprintf("%x", sha256.Sum256(border))}}, "themes": []any{map[string]any{"id": "primary", "name": "Motion theme", "style": style}}, "templates": []any{}}
	w := assetUpload(t, s, "/api/generator/packages", "motion.zip", brandZIP(t, m, map[string][]byte{"motion.mp4": video, "border.png": border}))
	if w.Code != 201 {
		t.Fatalf("motion import: %d %s", w.Code, w.Body.String())
	}
	var p struct {
		Themes []struct {
			ID    string
			Style map[string]any
		}
	}
	json.Unmarshal(w.Body.Bytes(), &p)
	body, _ := json.Marshal(map[string]any{"style": p.Themes[0].Style, "view": "border"})
	w = dashboardRequest(s, "POST", "/api/generator/themes/preview", string(body))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("border: %d %s", w.Code, w.Body.String())
	}
	d := mediaauthor.Design{Name: "Motion show", Stage: "prestream", Theme: mediaauthor.ThemeRef{ID: p.Themes[0].ID, Revision: 1}, Scenes: []mediaauthor.Scene{{ID: "one", Layout: "title", Text: "Hello", DurationSeconds: 2}}}
	body, _ = json.Marshal(d)
	w = dashboardRequest(s, "POST", "/api/generator/preview?frame=10", string(body))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("motion preview: %d %s", w.Code, w.Body.String())
	}
	saved := dashboardRequest(s, "POST", "/api/generator/designs", string(body))
	if saved.Code != 201 {
		t.Fatal(saved.Code, saved.Body.String())
	}
	json.Unmarshal(saved.Body.Bytes(), &d)
	generatorServe(t, s)
	output := exactGeneratedVideo(t, s, d)
	samples := decodePCM(t, output)
	for _, sample := range samples {
		if math.Abs(sample) > 0.001 {
			t.Fatal("motion background leaked source audio")
		}
	}

}

func TestBrandPackageFontRendersAndPersistsWithoutBundledBrandFiles(t *testing.T) {
	s := libraryServer(t)
	raw, _ := json.Marshal(mediaauthor.RetroTheme(1).Style)
	var style map[string]any
	json.Unmarshal(raw, &style)
	style["font"] = "brand-font"
	style["font_asset"] = map[string]any{"id": "heading", "revision": 1}
	m := map[string]any{"format_version": 1, "id": "font-kit", "version": 1, "name": "Font kit", "assets": []any{map[string]any{"id": "heading", "name": "Example heading", "kind": "font", "path": "fonts/heading.ttf", "sha256": fmt.Sprintf("%x", sha256.Sum256(gobold.TTF))}}, "themes": []any{map[string]any{"id": "primary", "name": "Custom font", "style": style}}, "templates": []any{}}
	w := assetUpload(t, s, "/api/generator/packages", "fonts.zip", brandZIP(t, m, map[string][]byte{"fonts/heading.ttf": gobold.TTF}))
	if w.Code != 201 {
		t.Fatalf("font import: %d %s", w.Code, w.Body.String())
	}
	var p struct {
		Themes []struct{ Style map[string]any }
		Assets []struct{ ID string }
	}
	json.Unmarshal(w.Body.Bytes(), &p)
	fontResponse := dashboardRequest(s, "GET", "/api/generator/assets/"+p.Assets[0].ID+"/revisions/1", "")
	if fontResponse.Code != 200 || fontResponse.Header().Get("Content-Type") != "font/otf" || !bytes.Equal(fontResponse.Body.Bytes(), gobold.TTF) {
		t.Fatal("font download did not preserve uploaded bytes", fontResponse.Code)
	}
	body, _ := json.Marshal(map[string]any{"style": p.Themes[0].Style, "view": "prestream"})
	w = dashboardRequest(New(s.cfg, s.log), "POST", "/api/generator/themes/preview", string(body))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("font preview: %d %s", w.Code, w.Body.String())
	}
}

func TestBrandPackageRecoveryRemovesIncompletePublicationBeforeServing(t *testing.T) {
	s := libraryServer(t)
	// Simulate a process interruption after an asset was published but before
	// the package commit marker, using the documented recovery journal format.
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	root := filepath.Join(s.library.root, "generator")
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets", id), 0700); err != nil {
		t.Fatal(err)
	}
	journal := `{"format_version":1,"id":"interrupted","version":1,"name":"Interrupted","assets":[{"id":"` + id + `"}],"themes":[],"templates":[]}`
	if err := os.WriteFile(filepath.Join(root, "packages", ".pending-interrupted-1.json"), []byte(journal), 0600); err != nil {
		t.Fatal(err)
	}
	restarted := New(s.cfg, s.log)
	w := dashboardRequest(restarted, "GET", "/api/generator/assets", "")
	if w.Code != 200 || w.Body.String() != "[]\n" {
		t.Fatalf("incomplete publication survived: %d %s", w.Code, w.Body.String())
	}
	w = dashboardRequest(restarted, "GET", "/api/generator/packages", "")
	if w.Code != 200 || w.Body.String() != "[]\n" {
		t.Fatalf("partial kit visible: %d %s", w.Code, w.Body.String())
	}
}

func TestBrandPackageImportsConnectedAssetsThemesAndTemplates(t *testing.T) {
	s := libraryServer(t)
	logo := assetPNG(t, color.NRGBA{R: 180, G: 230, A: 255})
	style := mediaauthor.RetroTheme(1).Style
	style.Logo = &mediaauthor.AssetRef{ID: "logo", Revision: 1}
	m := map[string]any{"format_version": 1, "id": "connected", "version": 1, "name": "Connected kit", "assets": []any{map[string]any{"id": "logo", "name": "Brand logo", "kind": "image", "path": "assets/logo.png", "sha256": fmt.Sprintf("%x", sha256.Sum256(logo))}}, "themes": []any{map[string]any{"id": "primary", "name": "Brand primary", "style": style}}, "templates": []any{map[string]any{"id": "welcome", "name": "Brand welcome", "theme": "primary", "content": mediaauthor.Design{Stage: "prestream", Scenes: []mediaauthor.Scene{{ID: "welcome", Layout: "title", Text: "Welcome", DurationSeconds: 8}}}}}}
	w := assetUpload(t, s, "/api/generator/packages", "kit.zip", brandZIP(t, m, map[string][]byte{"assets/logo.png": logo}))
	if w.Code != 201 {
		t.Fatalf("import: %d %s", w.Code, w.Body.String())
	}
	var installed struct {
		Assets    []struct{ ID string }
		Themes    []struct{ ID string }
		Templates []struct{ ID string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &installed); err != nil {
		t.Fatal(err)
	}
	if len(installed.Assets) != 1 || len(installed.Themes) != 1 || len(installed.Templates) != 1 {
		t.Fatal(w.Body.String())
	}
	for _, route := range []string{"/api/generator/assets/" + installed.Assets[0].ID + "/revisions/1", "/api/generator/themes/" + installed.Themes[0].ID + "/revisions/1", "/api/generator/templates/" + installed.Templates[0].ID} {
		if w = dashboardRequest(s, "GET", route, ""); w.Code != 200 {
			t.Fatal(route, w.Code, w.Body.String())
		}
	}
	w = dashboardRequest(s, "POST", "/api/generator/templates/"+installed.Templates[0].ID+"/designs", fmt.Sprintf(`{"name":"My branded show","version":1,"theme":{"id":%q,"revision":1}}`, installed.Themes[0].ID))
	if w.Code != 201 {
		t.Fatalf("create show: %d %s", w.Code, w.Body.String())
	}
	var d mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &d)
	w = dashboardRequest(s, "POST", "/api/generator/jobs", fmt.Sprintf(`{"design_id":%q,"version":1}`, d.ID))
	if w.Code != 202 {
		t.Fatalf("queue branded design: %d %s", w.Code, w.Body.String())
	}
}

func TestBrandPackagePreviewDoesNotPublishAndImportSurvivesRestart(t *testing.T) {
	s := libraryServer(t)
	manifest := map[string]any{"format_version": 1, "id": "example-brand", "version": 1, "name": "Example brand", "assets": []any{}, "themes": []any{}, "templates": []any{}}
	data := brandZIP(t, manifest, nil)
	w := assetUpload(t, s, "/api/generator/packages/preview", "example.zip", data)
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	w = dashboardRequest(s, "GET", "/api/generator/packages", "")
	if w.Code != 200 || w.Body.String() != "[]\n" {
		t.Fatalf("preview published: %d %s", w.Code, w.Body.String())
	}
	w = assetUpload(t, s, "/api/generator/packages", "example.zip", data)
	if w.Code != 201 {
		t.Fatalf("import: %d %s", w.Code, w.Body.String())
	}
	restarted := New(s.cfg, s.log)
	w = dashboardRequest(restarted, "GET", "/api/generator/packages", "")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("Example brand")) {
		t.Fatalf("restart: %d %s", w.Code, w.Body.String())
	}
	w = assetUpload(t, restarted, "/api/generator/packages", "example.zip", data)
	if w.Code != 200 {
		t.Fatalf("idempotent import: %d %s", w.Code, w.Body.String())
	}
	manifest["name"] = "Conflicting contents"
	w = assetUpload(t, restarted, "/api/generator/packages", "example.zip", brandZIP(t, manifest, nil))
	if w.Code != 409 {
		t.Fatalf("conflict: %d %s", w.Code, w.Body.String())
	}
}

func TestBrandPackageRejectsInvalidMusicBeforePublishing(t *testing.T) {
	s := libraryServer(t)
	audio := musicFixture(t, "sine=frequency=440:sample_rate=48000", 1)
	m := map[string]any{"format_version": 1, "id": "invalid-music", "version": 1, "name": "Invalid music", "assets": []any{map[string]any{"id": "track", "name": "Track", "kind": "audio", "path": "track.wav", "sha256": fmt.Sprintf("%x", sha256.Sum256(audio))}}, "themes": []any{map[string]any{"id": "primary", "name": "Primary", "style": mediaauthor.RetroTheme(1).Style}}, "templates": []any{map[string]any{"id": "welcome", "name": "Welcome", "theme": "primary", "content": map[string]any{"stage": "prestream", "scenes": []any{map[string]any{"id": "one", "layout": "title", "text": "Hello", "duration_seconds": 4}}, "soundtrack": map[string]any{"asset": map[string]any{"id": "track", "revision": 1}, "mode": "invalid"}}}}}
	w := assetUpload(t, s, "/api/generator/packages", "kit.zip", brandZIP(t, m, map[string][]byte{"track.wav": audio}))
	if w.Code != 422 {
		t.Fatalf("invalid music published: %d %s", w.Code, w.Body.String())
	}
	w = dashboardRequest(s, "GET", "/api/generator/packages", "")
	if w.Body.String() != "[]\n" {
		t.Fatal(w.Body.String())
	}
}

func TestBrandPackageUpgradeKeepsExistingDesignOnExactTheme(t *testing.T) {
	s := libraryServer(t)
	m := brandManifest{FormatVersion: 1, ID: "upgrade", Version: 1, Name: "Upgrade kit", Themes: []brandTheme{{ID: "primary", Name: "Primary", Style: mediaauthor.RetroTheme(1).Style}}}
	w := assetUpload(t, s, "/api/generator/packages", "kit.zip", brandZIP(t, m, nil))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var first brandPackage
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	w = dashboardRequest(s, "POST", "/api/generator/designs", fmt.Sprintf(`{"name":"Existing show","stage":"prestream","theme":{"id":%q,"revision":1},"scenes":[{"id":"one","layout":"title","text":"Hello","duration_seconds":4}]}`, first.Themes[0].ID))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var design mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &design)
	m.Version = 2
	m.Themes[0].Style.TextColor = "#ffffff"
	w = assetUpload(t, s, "/api/generator/packages", "kit.zip", brandZIP(t, m, nil))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var second brandPackage
	json.Unmarshal(w.Body.Bytes(), &second)
	if second.Themes[0].ID == first.Themes[0].ID {
		t.Fatal("upgrade overwrote theme identity")
	}
	w = dashboardRequest(New(s.cfg, s.log), "GET", "/api/generator/designs/"+design.ID, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var retained mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &retained)
	if retained.Theme != design.Theme || retained.Version != design.Version {
		t.Fatal("upgrade changed existing design", w.Body.String())
	}
}

func TestBrandPackageMembersRequirePackageRemoval(t *testing.T) {
	s := libraryServer(t)
	m := brandManifest{FormatVersion: 1, ID: "owned", Version: 1, Name: "Owned kit", Themes: []brandTheme{{ID: "primary", Name: "Primary", Style: mediaauthor.RetroTheme(1).Style}}, Templates: []brandTemplate{{ID: "welcome", Name: "Welcome", Theme: "primary", Content: mediaauthor.Design{Stage: "prestream", Scenes: []mediaauthor.Scene{{ID: "one", Layout: "title", Text: "Hello", DurationSeconds: 4}}}}}}
	w := assetUpload(t, s, "/api/generator/packages", "kit.zip", brandZIP(t, m, nil))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var p brandPackage
	json.Unmarshal(w.Body.Bytes(), &p)
	for _, request := range []struct{ route, body string }{{"/api/generator/templates/" + p.Templates[0].ID, `{"version":1}`}, {"/api/generator/themes/" + p.Themes[0].ID, `{"revision":1}`}} {
		w = dashboardRequest(s, "DELETE", request.route, request.body)
		if w.Code != 409 || !bytes.Contains(w.Body.Bytes(), []byte("package")) {
			t.Errorf("package member deletion: %d %s", w.Code, w.Body.String())
		}
	}
	w = dashboardRequest(s, "DELETE", "/api/generator/packages/owned/versions/1", `{}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
