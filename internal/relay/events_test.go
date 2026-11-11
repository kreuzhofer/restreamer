package relay

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
)

func eventTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	return New(config.Config{Application: "live", StreamKey: "input-key-1234567890", DashboardUsername: "test", DashboardPassword: "test", StateFile: filepath.Join(dir, "switches.json"), EventsDirectory: filepath.Join(dir, "events"), QueueBytes: 16 << 20}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func createEvent(t *testing.T, s *Server, password string, replay bool) string {
	t.Helper()
	current := dashboardRequest(s, "GET", "/api/destinations", "")
	var state struct {
		Version uint64 `json:"version"`
	}
	if err := json.Unmarshal(current.Body.Bytes(), &state); err != nil {
		t.Fatal(current.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"version": state.Version, "name": "event", "type": "public_page", "password": password, "auto_replay": replay})
	w := dashboardRequest(s, "POST", "/api/destinations", string(body))
	if w.Code != 200 {
		t.Fatalf("create event: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Destinations []struct {
			EventID string `json:"event_id"`
		} `json:"destinations"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Destinations) != 1 || result.Destinations[0].EventID == "" {
		t.Fatal(w.Body.String())
	}
	return result.Destinations[0].EventID
}

func viewerRequest(s *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Restreamer-Control", "1")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestEventCreationSurvivesRestartWithoutOperatorAccess(t *testing.T) {
	s := eventTestServer(t)
	id := createEvent(t, s, "", false)
	for _, server := range []*Server{s, New(s.cfg, s.log)} {
		w := viewerRequest(server, "GET", "/watch/"+id+"/status", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"waiting"`) {
			t.Fatal(w.Code, w.Body.String())
		}
		if w = viewerRequest(server, "GET", "/watch/"+id, ""); w.Code != 200 || !strings.Contains(w.Header().Get("X-Robots-Tag"), "noindex") {
			t.Fatal(w.Code, w.Header())
		}
		if w = viewerRequest(server, "GET", "/api/events", ""); w.Code != 401 {
			t.Fatal("operator access leaked", w.Code)
		}
	}
}

func TestEventPasswordSessionRevokedAfterChange(t *testing.T) {
	s := eventTestServer(t)
	id := createEvent(t, s, "viewer-secret", false)
	path := "/watch/" + id
	if w := viewerRequest(s, "GET", path+"/status", ""); w.Code != 401 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := viewerRequest(s, "POST", path+"/login", `{"password":"wrong"}`); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := viewerRequest(s, "POST", path+"/login", `{"password":"viewer-secret"}`)
	if w.Code != 204 || len(w.Result().Cookies()) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	check := func(expected int) {
		r := httptest.NewRequest("GET", path+"/status", nil)
		r.AddCookie(cookie)
		out := httptest.NewRecorder()
		s.Handler().ServeHTTP(out, r)
		if out.Code != expected {
			t.Fatal(out.Code, out.Body.String())
		}
	}
	check(200)
	w = dashboardRequest(s, "PATCH", "/api/events/"+id, `{"version":1,"password":"changed-secret"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	check(401)
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatal("password leaked", w.Body.String())
	}
}

func startEventRelay(t *testing.T, s *Server) string {
	t.Helper()
	s.cfg.Application = "live"
	s.cfg.StreamKey = "input-key-1234567890"
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, l) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("shutdown timed out")
		}
	})
	return "rtmp://" + l.Addr().String() + "/live"
}

func TestEventLiveHLSPreservesPublicProgrammeAndClosesURLToReuse(t *testing.T) {
	s := eventTestServer(t)
	id := createEvent(t, s, "", false)
	address := startEventRelay(t, s)
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "event", "enabled": true}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	publisher := publishInput(t, address)
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	writePacket(t, publisher, previewVideoConfig())
	writePacket(t, publisher, audioConfig())
	writePacket(t, publisher, keyframe(0))
	eventually(t, func() bool { return readStage(t, s).Stage == "LIVE" })
	time.Sleep(50 * time.Millisecond)
	for i := 1; i <= 30; i++ {
		writePacket(t, publisher, keyframe(time.Duration(i)*time.Second))
		time.Sleep(3 * time.Millisecond)
	}
	var playlist string
	eventually(t, func() bool {
		w := viewerRequest(s, "GET", "/watch/"+id+"/live.m3u8", "")
		playlist = w.Body.String()
		return w.Code == 200 && strings.Contains(playlist, "#EXTINF:")
	})
	if !strings.Contains(playlist, "#EXT-X-MAP:") {
		t.Fatal(playlist)
	}
	for _, line := range strings.Split(playlist, "\n") {
		if strings.HasSuffix(line, ".m4s") {
			w := viewerRequest(s, "GET", "/watch/"+id+"/"+line, "")
			if w.Code != 200 || !strings.Contains(w.Body.String(), "moof") {
				t.Fatal(w.Code, w.Body.String())
			}
		}
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool {
		w := viewerRequest(s, "GET", "/watch/"+id+"/status", "")
		return strings.Contains(w.Body.String(), `"state":"ended"`)
	})
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "event", "enabled": true}); w.Code < 400 {
		t.Fatal("finished event reused")
	}
	if w := viewerRequest(s, "GET", "/watch/"+id+"/replay.m3u8", ""); w.Code != 404 {
		t.Fatal("unpublished replay accessible", w.Code)
	}
}

func recordEvent(t *testing.T, s *Server, id string) {
	t.Helper()
	address := startEventRelay(t, s)
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "event", "enabled": true}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	publisher := publishInput(t, address)
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	writePacket(t, publisher, previewVideoConfig())
	writePacket(t, publisher, audioConfig())
	writePacket(t, publisher, keyframe(0))
	eventually(t, func() bool { return readStage(t, s).Stage == "LIVE" })
	time.Sleep(50 * time.Millisecond)
	for i := 1; i <= 16; i++ {
		writePacket(t, publisher, keyframe(time.Duration(i)*time.Second))
		time.Sleep(3 * time.Millisecond)
	}
	eventually(t, func() bool { return viewerRequest(s, "GET", "/watch/"+id+"/live.m3u8", "").Code == 200 })
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool {
		return strings.Contains(dashboardRequest(s, "GET", "/api/events", "").Body.String(), `"state":"ended"`)
	})
}

func TestEventAutomaticReplayUsesOriginalURLAndSurvivesRestart(t *testing.T) {
	s := eventTestServer(t)
	id := createEvent(t, s, "", true)
	recordEvent(t, s, id)
	for _, server := range []*Server{s, New(s.cfg, s.log)} {
		w := viewerRequest(server, "GET", "/watch/"+id+"/replay.m3u8", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "#EXT-X-ENDLIST") || !strings.Contains(w.Body.String(), "#EXT-X-PLAYLIST-TYPE:VOD") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func eventVersion(t *testing.T, s *Server, id string) uint64 {
	t.Helper()
	w := dashboardRequest(s, "GET", "/api/events", "")
	var result struct {
		Events []struct {
			ID      string `json:"id"`
			Version uint64 `json:"version"`
		} `json:"events"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, e := range result.Events {
		if e.ID == id {
			return e.Version
		}
	}
	t.Fatal("event missing")
	return 0
}

func TestEventArchivePublicationWithdrawalAndDeletion(t *testing.T) {
	s := eventTestServer(t)
	id := createEvent(t, s, "", false)
	recordEvent(t, s, id)
	w := dashboardRequest(s, "GET", "/api/events/"+id+"/media/replay.m3u8", "")
	if w.Code != 200 {
		t.Fatal("operator preview", w.Code, w.Body.String())
	}
	segment := ""
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if strings.HasSuffix(line, ".m4s") {
			segment = line
			break
		}
	}
	mutate := func(method string, values map[string]any) {
		t.Helper()
		values["version"] = eventVersion(t, s, id)
		data, _ := json.Marshal(values)
		w := dashboardRequest(s, method, "/api/events/"+id, string(data))
		if w.Code != 200 && w.Code != 204 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	mutate("PATCH", map[string]any{"published": true})
	if w := viewerRequest(s, "GET", "/watch/"+id+"/"+segment, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	mutate("PATCH", map[string]any{"published": false})
	if w := viewerRequest(s, "GET", "/watch/"+id+"/"+segment, ""); w.Code != 404 {
		t.Fatal("withdrawn media leaked", w.Code)
	}
	mutate("DELETE", map[string]any{"confirmed": true})
	for _, server := range []*Server{s, New(s.cfg, s.log)} {
		for _, resource := range []string{"", "/status", "/replay.m3u8", "/" + segment} {
			if w := viewerRequest(server, "GET", "/watch/"+id+resource, ""); w.Code != 404 {
				t.Fatal("deleted event accessible", resource, w.Code)
			}
		}
	}
}

func TestEventRestartRecoversIncompleteRecordingWithoutAutomaticPublication(t *testing.T) {
	s := eventTestServer(t)
	id := createEvent(t, s, "", true)
	address := startEventRelay(t, s)
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "event", "enabled": true}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	publisher := publishInput(t, address)
	stageRequest(t, s, "go_live", nil)
	writePacket(t, publisher, previewVideoConfig())
	writePacket(t, publisher, audioConfig())
	writePacket(t, publisher, keyframe(0))
	eventually(t, func() bool { return readStage(t, s).Stage == "LIVE" })
	time.Sleep(50 * time.Millisecond)
	for i := 1; i <= 12; i++ {
		writePacket(t, publisher, keyframe(time.Duration(i)*time.Second))
		time.Sleep(3 * time.Millisecond)
	}
	eventually(t, func() bool { return viewerRequest(s, "GET", "/watch/"+id+"/live.m3u8", "").Code == 200 })
	// Snapshot only durable files, as a new process sees them after abrupt loss.
	snapshot := t.TempDir()
	if err := os.CopyFS(snapshot, os.DirFS(s.cfg.EventsDirectory)); err != nil {
		t.Fatal(err)
	}
	cfg := s.cfg
	cfg.EventsDirectory = snapshot
	restarted := New(cfg, s.log)
	w := dashboardRequest(restarted, "GET", "/api/events", "")
	if !strings.Contains(w.Body.String(), `"state":"interrupted"`) || !strings.Contains(w.Body.String(), `"incomplete":true`) {
		t.Fatal(w.Body.String())
	}
	if w := viewerRequest(restarted, "GET", "/watch/"+id+"/replay.m3u8", ""); w.Code != 404 {
		t.Fatal("incomplete replay automatically published", w.Code)
	}
	for _, ack := range []bool{false, true} {
		body, _ := json.Marshal(map[string]any{"version": eventVersion(t, restarted, id), "published": true, "acknowledge_incomplete": ack})
		w := dashboardRequest(restarted, "PATCH", "/api/events/"+id, string(body))
		want := 409
		if ack {
			want = 200
		}
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := viewerRequest(restarted, "GET", "/watch/"+id+"/replay.m3u8", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, restarted, "set_target", map[string]any{"target": "event", "enabled": true}); w.Code < 400 {
		t.Fatal("interrupted event reopened")
	}
}

func TestEventStorageFailureIsVisibleAndDoesNotStopAnotherDestination(t *testing.T) {
	s := eventTestServer(t)
	id := createEvent(t, s, "", true)
	sink := newSink(t)
	target := sink.target("other")
	body, _ := json.Marshal(map[string]any{"version": 1, "name": target.Name, "url": target.URL, "stream_key": target.StreamKey})
	if w := dashboardRequest(s, "POST", "/api/destinations", string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	address := startEventRelay(t, s)
	for _, name := range []string{"event", "other"} {
		if w := stageRequest(t, s, "set_target", map[string]any{"target": name, "enabled": true}); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	publisher := publishInput(t, address)
	stageRequest(t, s, "go_live", nil)
	writePacket(t, publisher, previewVideoConfig())
	writePacket(t, publisher, audioConfig())
	writePacket(t, publisher, keyframe(0))
	eventually(t, func() bool { return readStage(t, s).Stage == "LIVE" && sink.count() == 1 })
	time.Sleep(50 * time.Millisecond)
	for i := 1; i <= 8; i++ {
		writePacket(t, publisher, keyframe(time.Duration(i)*time.Second))
		time.Sleep(3 * time.Millisecond)
	}
	eventually(t, func() bool { return viewerRequest(s, "GET", "/watch/"+id+"/live.m3u8", "").Code == 200 })
	// Make atomic metadata replacement fail at the filesystem boundary.
	metadata := filepath.Join(s.cfg.EventsDirectory, id, "event.json")
	if err := os.Remove(metadata); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(metadata, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 9; i <= 16; i++ {
		writePacket(t, publisher, keyframe(time.Duration(i)*time.Second))
		time.Sleep(3 * time.Millisecond)
	}
	eventually(t, func() bool {
		return strings.Contains(dashboardRequest(s, "GET", "/api/events", "").Body.String(), `"incomplete":true`)
	})
	if readStage(t, s).Stage != "LIVE" || sink.count() != 1 {
		t.Fatal("event failure stopped other delivery")
	}
	if w := viewerRequest(s, "GET", "/watch/"+id+"/replay.m3u8", ""); w.Code != 404 {
		t.Fatal("failed recording published")
	}
	if err := os.Remove(metadata); err != nil {
		t.Fatal(err)
	}
}

func TestEventRejectsLegacyMutationAndRollsBackFailedCreation(t *testing.T) {
	s := eventTestServer(t)
	id := createEvent(t, s, "", false)
	for _, method := range []string{"PUT", "DELETE"} {
		w := dashboardRequest(s, method, "/api/destinations/event", `{"version":1,"name":"event","url":"rtmp://localhost/live"}`)
		if w.Code != 409 {
			t.Fatal("event lifecycle bypass", method, w.Code, w.Body.String())
		}
	}
	if w := viewerRequest(s, "GET", "/watch/"+id+"/status", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	other := eventTestServer(t)
	dashboardRequest(other, "GET", "/api/destinations", "")
	other.cfg.DestinationsFile = filepath.Join(t.TempDir(), "missing", "destinations.json")
	w := dashboardRequest(other, "POST", "/api/destinations", `{"version":0,"name":"event","type":"public_page"}`)
	if w.Code != 500 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = dashboardRequest(other, "GET", "/api/events", "")
	if !strings.Contains(w.Body.String(), `"events":[]`) {
		t.Fatal("failed creation left orphan", w.Body.String())
	}
}

func TestEventSuccessfulViewersDoNotExhaustLoginAttempts(t *testing.T) {
	s := eventTestServer(t)
	id := createEvent(t, s, "viewers", false)
	for i := 0; i < 12; i++ {
		if w := viewerRequest(s, "POST", "/watch/"+id+"/login", `{"password":"viewers"}`); w.Code != 204 {
			t.Fatalf("viewer %d: %d %s", i, w.Code, w.Body.String())
		}
	}
}

func TestEventArchiveDoesNotConsumeFutureDestinationSlots(t *testing.T) {
	s := eventTestServer(t)
	id := createEvent(t, s, "", true)
	recordEvent(t, s, id)
	next := createEvent(t, s, "", false)
	if next == id {
		t.Fatal("event URL reused")
	}
	if w := viewerRequest(s, "GET", "/watch/"+id+"/replay.m3u8", ""); w.Code != 200 {
		t.Fatal("archived event lost when creating the next destination", w.Code)
	}
}

func TestEventRestartRejectsCorruptMediaAndKeepsRecoveryPrivate(t *testing.T) {
	s := eventTestServer(t)
	id := createEvent(t, s, "", true)
	recordEvent(t, s, id)
	w := viewerRequest(s, "GET", "/watch/"+id+"/replay.m3u8", "")
	var damaged string
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if strings.HasSuffix(line, ".m4s") {
			damaged = line
			break
		}
	}
	path := filepath.Join(s.cfg.EventsDirectory, id, damaged)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Same-length corruption must not pass a size-only recovery check.
	for i := range data {
		data[i] = 0
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		restarted := New(s.cfg, s.log)
		if w := viewerRequest(restarted, "GET", "/watch/"+id+"/replay.m3u8", ""); w.Code != 404 {
			t.Fatal("corrupt recording remained published", w.Code)
		}
		w := dashboardRequest(restarted, "GET", "/api/events/"+id+"/media/replay.m3u8", "")
		if w.Code != 200 || strings.Contains(w.Body.String(), damaged) {
			t.Fatal("recovery did not isolate corrupt media", w.Code, w.Body.String())
		}
		if w := dashboardRequest(restarted, "GET", "/api/events", ""); !strings.Contains(w.Body.String(), `"incomplete":true`) {
			t.Fatal("missing recovery warning", w.Body.String())
		}
	}
}

func TestEventRehearsalAndDeliverySwitchesDoNotLeakOrFinishEvent(t *testing.T) {
	s := eventTestServer(t)
	id := createEvent(t, s, "", false)
	address := startEventRelay(t, s)
	stageRequest(t, s, "set_target", map[string]any{"target": "event", "enabled": true})
	publisher := publishInput(t, address)
	stageRequest(t, s, "go_live", map[string]any{"mode": "preview_only"})
	writePacket(t, publisher, previewVideoConfig())
	writePacket(t, publisher, audioConfig())
	writePacket(t, publisher, keyframe(0))
	eventually(t, func() bool { return readStage(t, s).Stage == "LIVE" })
	for i := 1; i <= 6; i++ {
		writePacket(t, publisher, keyframe(time.Duration(i)*time.Second))
	}
	if w := viewerRequest(s, "GET", "/watch/"+id+"/live.m3u8", ""); w.Code != 404 {
		t.Fatal("rehearsal leaked")
	}
	stageRequest(t, s, "stop_now", nil)
	if w := viewerRequest(s, "GET", "/watch/"+id+"/status", ""); !strings.Contains(w.Body.String(), `"state":"waiting"`) {
		t.Fatal("rehearsal consumed event", w.Body.String())
	}
	stageRequest(t, s, "go_live", nil)
	writePacket(t, publisher, keyframe(7*time.Second))
	eventually(t, func() bool { return readStage(t, s).Stage == "LIVE" })
	time.Sleep(50 * time.Millisecond)
	for i := 8; i <= 16; i++ {
		writePacket(t, publisher, keyframe(time.Duration(i)*time.Second))
		time.Sleep(3 * time.Millisecond)
	}
	eventually(t, func() bool { return viewerRequest(s, "GET", "/watch/"+id+"/live.m3u8", "").Code == 200 })
	stageRequest(t, s, "set_target", map[string]any{"target": "event", "enabled": false})
	if w := viewerRequest(s, "GET", "/watch/"+id+"/live.m3u8", ""); w.Code != 404 {
		t.Fatal("disabled target still public", w.Code)
	}
	if w := stageRequest(t, s, "set_target", map[string]any{"target": "event", "enabled": true}); w.Code != 200 {
		t.Fatal("paused event closed", w.Code, w.Body.String())
	}
	time.Sleep(50 * time.Millisecond)
	for i := 17; i <= 26; i++ {
		writePacket(t, publisher, keyframe(time.Duration(i)*time.Second))
		time.Sleep(3 * time.Millisecond)
	}
	eventually(t, func() bool { return viewerRequest(s, "GET", "/watch/"+id+"/live.m3u8", "").Code == 200 })
	stageRequest(t, s, "stop_now", nil)
}
