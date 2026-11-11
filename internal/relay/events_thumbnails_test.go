package relay

import (
	"bytes"
	"encoding/json"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func recordedThumbnailEvent(t *testing.T, password string, replay bool) (*Server, string) {
	t.Helper()
	prepared, revision := preparedRevisionServer(t)
	cfg := prepared.cfg
	cfg.Targets = nil
	cfg.EventsDirectory = filepath.Join(t.TempDir(), "events")
	cfg.StateFile = filepath.Join(t.TempDir(), "switches.json")
	s := New(cfg, prepared.log)
	id := createEvent(t, s, password, replay)
	startEventRelay(t, s)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	stageRequest(t, s, "set_target", map[string]any{"target": "event", "enabled": true})
	if w := stageRequest(t, s, "prestream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool { return dashboardRequest(s, "GET", "/events/"+id+"/replay.m3u8", "").Code == 200 })
	stageRequest(t, s, "stop_now", nil)
	eventually(t, func() bool {
		body := dashboardRequest(s, "GET", "/api/events", "").Body.Bytes()
		return bytes.Contains(body, []byte(`"state":"ended"`)) && !bytes.Contains(body, []byte(`"finalizing":true`))
	})
	return s, id
}

func TestEventReplayThumbnailsUseRecordedFramesAndPersist(t *testing.T) {
	s, id := recordedThumbnailEvent(t, "", true)
	base := "/watch/" + id + "/"
	w := viewerRequest(s, "GET", base+"thumbnails.json", "")
	var index struct {
		Duration float64 `json:"duration"`
		Frames   []struct {
			Time     float64 `json:"time"`
			Resource string  `json:"resource"`
		} `json:"frames"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &index) != nil || index.Duration <= 0 || len(index.Frames) == 0 || index.Frames[0].Time != 0 {
		t.Fatal("missing replay thumbnail timeline", w.Code, w.Body.String())
	}
	path := base + index.Frames[0].Resource
	w = viewerRequest(s, "GET", path, "")
	image, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes()))
	if w.Code != 200 || err != nil {
		t.Fatal("missing decoded thumbnail", w.Code, err, w.Body.String())
	}
	if image.Bounds().Dx() != 192 || image.Bounds().Dy() != 108 {
		t.Fatal("unbounded thumbnail size", image.Bounds())
	}
	poster := viewerRequest(s, "GET", base+"poster.jpg", "")
	posterImage, err := jpeg.DecodeConfig(bytes.NewReader(poster.Body.Bytes()))
	if poster.Code != 200 || err != nil || posterImage.Width != 1280 || posterImage.Height != 720 {
		t.Fatal("missing first-frame poster", poster.Code, err)
	}
	expected := append([]byte{}, w.Body.Bytes()...)
	restarted := New(s.cfg, s.log)
	if w := viewerRequest(restarted, "GET", base+"status", ""); w.Code != 200 {
		t.Fatal("restart failed", w.Code, w.Body.String())
	}
	t.Setenv("PATH", t.TempDir()) // Cached images need no new decoder process.
	w = viewerRequest(restarted, "GET", path, "")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), expected) {
		t.Fatal("thumbnail cache did not survive restart", w.Code)
	}
	if w = viewerRequest(restarted, "GET", base+"thumbnail-99999999.jpg", ""); w.Code != 404 {
		t.Fatal("unknown segment accepted", w.Code)
	}
}

func TestEventReplayThumbnailsFollowEventAccess(t *testing.T) {
	s, id := recordedThumbnailEvent(t, "viewer-secret", false)
	base := "/watch/" + id + "/"
	resources := []string{"thumbnails.json", "thumbnail-00000000.jpg", "poster.jpg"}
	var cookie *http.Cookie
	get := func(resource string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", base+resource, nil)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	check := func(code int) {
		t.Helper()
		for _, resource := range resources {
			if w := get(resource); w.Code != code {
				t.Fatalf("%s: %d want %d: %s", resource, w.Code, code, w.Body.String())
			}
		}
	}
	login := func(password string) {
		t.Helper()
		w := viewerRequest(s, "POST", base+"login", `{"password":"`+password+`"}`)
		if w.Code != 204 {
			t.Fatal(w.Code, w.Body.String())
		}
		cookie = w.Result().Cookies()[0]
	}
	patch := func(fields map[string]any) {
		t.Helper()
		fields["version"] = eventVersion(t, s, id)
		body, _ := json.Marshal(fields)
		w := dashboardRequest(s, "PATCH", "/api/events/"+id, string(body))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	check(401)
	login("viewer-secret")
	check(404)
	for _, resource := range resources {
		if w := dashboardRequest(s, "GET", "/events/"+id+"/"+resource, ""); w.Code != 200 {
			t.Fatal("private operator thumbnail unavailable", w.Code, w.Body.String())
		}
	}
	patch(map[string]any{"published": true})
	check(200)
	patch(map[string]any{"password": "replacement-secret"})
	check(401)
	login("replacement-secret")
	check(200)
	patch(map[string]any{"published": false})
	check(404)
	body, _ := json.Marshal(map[string]any{"version": eventVersion(t, s, id), "confirmed": true})
	if w := dashboardRequest(s, "DELETE", "/api/events/"+id, string(body)); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	check(404)
}

func TestEventReplayThumbnailFailureDoesNotBlockPlayback(t *testing.T) {
	s, id := recordedThumbnailEvent(t, "", true)
	t.Setenv("PATH", t.TempDir())
	base := "/watch/" + id + "/"
	for _, resource := range []string{"poster.jpg", "thumbnail-00000000.jpg"} {
		w := viewerRequest(s, "GET", base+resource, "")
		if w.Code != 503 || !bytes.Contains(w.Body.Bytes(), []byte("Cannot create replay preview")) {
			t.Fatal("decoder failure not explicit", w.Code, w.Body.String())
		}
	}
	if w := viewerRequest(s, "GET", base+"replay.m3u8", ""); w.Code != 200 {
		t.Fatal("preview failure blocked replay", w.Code, w.Body.String())
	}
	if w := viewerRequest(s, "GET", base+"status", ""); bytes.Contains(w.Body.Bytes(), []byte(`"state":"failed"`)) {
		t.Fatal("preview failure changed event lifecycle", w.Body.String())
	}
}
