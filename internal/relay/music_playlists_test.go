package relay

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func TestMusicPlaylistPinsOrderAndRevisionsAcrossRestart(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "first.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", .4)))
	b := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "second.wav", musicFixture(t, "sine=frequency=880:sample_rate=48000", .6)))
	tracks := []mediaauthor.AssetRef{{ID: a.ID, Revision: 1}, {ID: b.ID, Revision: 1}}
	data, _ := json.Marshal(map[string]any{"name": "Show music", "tracks": tracks})
	w := dashboardRequest(s, "POST", "/api/music/playlists", string(data))
	if w.Code != 201 {
		t.Fatalf("create playlist: %d %s", w.Code, w.Body.String())
	}
	var saved struct {
		ID       string
		Revision int
		Duration float64
		Tracks   []mediaauthor.AssetRef
	}
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 || saved.Duration != 1 || len(saved.Tracks) != 2 || saved.Tracks[0] != tracks[0] {
		t.Fatalf("wrong playlist: %+v", saved)
	}
	data, _ = json.Marshal(map[string]any{"name": "Reordered", "version": 1, "tracks": []mediaauthor.AssetRef{tracks[1], tracks[0]}})
	w = dashboardRequest(s, "PUT", "/api/music/playlists/"+saved.ID, string(data))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = dashboardRequest(s, "PUT", "/api/music/playlists/"+saved.ID, string(data)); w.Code != 409 {
		t.Fatal("stale edit accepted", w.Code)
	}
	restarted := New(s.cfg, s.log)
	w = dashboardRequest(restarted, "GET", fmt.Sprintf("/api/music/playlists/%s/revisions/1", saved.ID), "")
	var original struct {
		Name   string
		Tracks []mediaauthor.AssetRef
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &original) != nil || original.Name != "Show music" || original.Tracks[0] != tracks[0] {
		t.Fatal("captured playlist changed", w.Code, w.Body.String())
	}
	if w = dashboardRequest(restarted, "DELETE", "/api/generator/assets/"+a.ID, `{}`); w.Code != 409 {
		t.Fatal("playlist track deletion allowed", w.Code)
	}
	if w = dashboardRequest(restarted, "DELETE", "/api/music/playlists/"+saved.ID, `{"version":2}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = dashboardRequest(restarted, "GET", "/api/generator/assets/"+a.ID+"/revisions/1", ""); w.Code != 200 {
		t.Fatal("playlist deletion removed uploaded music", w.Code)
	}
}

func TestMusicPlaylistSelectionProtectsExactRevision(t *testing.T) {
	s := libraryServer(t)
	a := uploadedAsset(t, assetUpload(t, s, "/api/generator/assets?kind=audio", "tone.wav", musicFixture(t, "sine=frequency=440:sample_rate=48000", 1)))
	w := dashboardRequest(s, "POST", "/api/music/playlists", fmt.Sprintf(`{"name":"Opening","tracks":[{"id":%q,"revision":1}]}`, a.ID))
	var playlist musicPlaylistView
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &playlist) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	w = dashboardRequest(s, "POST", "/api/generator/designs", fmt.Sprintf(`{"name":"Playlist show","stage":"prestream","duration_mode":"music","soundtrack":{"playlist":{"id":%q,"revision":1},"mode":"repeat"}}`, playlist.ID))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var design mediaauthor.Design
	json.Unmarshal(w.Body.Bytes(), &design)
	w = dashboardRequest(s, "DELETE", "/api/music/playlists/"+playlist.ID, `{"version":1}`)
	if w.Code != 409 {
		t.Fatal("saved design did not protect playlist", w.Code, w.Body.String())
	}
	w = dashboardRequest(s, "GET", "/api/music/playlists", "")
	var catalog []musicPlaylistView
	if json.Unmarshal(w.Body.Bytes(), &catalog) != nil || len(catalog) != 1 || len(catalog[0].Uses) != 1 || catalog[0].Uses[0].Name != "Playlist show" {
		t.Fatal("missing playlist usage", w.Body.String())
	}
	w = dashboardRequest(s, "DELETE", "/api/generator/designs/"+design.ID, `{"version":1}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = dashboardRequest(s, "DELETE", "/api/music/playlists/"+playlist.ID, `{"version":1}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestMusicManagementRequiresAuthenticationAndSameOrigin(t *testing.T) {
	s := libraryServer(t)
	for _, path := range []string{"/api/music/playlists", "/api/brb/presentations"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal(path, "unprotected", w.Code)
		}
		r = httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Restreamer-Control", "1")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(path, "cross-site mutation allowed", w.Code)
		}
	}
}
