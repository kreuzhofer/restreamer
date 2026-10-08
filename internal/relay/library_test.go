package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func libraryServer(t *testing.T) *Server {
	t.Helper()
	s := dashboardServer(t)
	s.cfg.BRB = &config.BRBConfig{Enabled: "true", Directory: t.TempDir(), BRBProfile: config.BRBProfile{Width: 320, Height: 180, FPS: 25, SampleRate: 48000}}
	s.cfg.QueueBytes = 1 << 20
	if err := s.initialize(); err != nil {
		t.Fatal(err)
	}
	return s
}
func videoUpload(t *testing.T, s *Server, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	writer.Close()
	r := httptest.NewRequest("POST", "/api/library/upload", &body)
	r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	r.Header.Set("X-Restreamer-Control", "1")
	r.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestLibraryInvalidOriginalCannotBeRequeued(t *testing.T) {
	l := &videoLibrary{root: t.TempDir(), limit: 16, entries: make(map[string]*LibraryEntry), wake: make(chan struct{}, 1)}
	if err := os.Mkdir(filepath.Join(l.root, "originals"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.root, "originals", "large.mp4"), make([]byte, 17), 0600); err != nil {
		t.Fatal(err)
	}
	l.scan()
	l.setProfile(config.BRBProfile{Width: 320, Height: 180, FPS: 25, SampleRate: 48000})
	if l.status().Files[0].State != "failed" {
		t.Fatal("profile change requeued oversized original")
	}
	if err := l.retry(clipHash("large.mp4")); err == nil {
		t.Fatal("retry accepted oversized original")
	}
}

func TestLibraryRejectsOriginalChangedSinceDiscovery(t *testing.T) {
	l := &videoLibrary{root: t.TempDir(), limit: 16, entries: make(map[string]*LibraryEntry), wake: make(chan struct{}, 1)}
	if err := os.Mkdir(filepath.Join(l.root, "originals"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(l.root, "originals", "changing.mp4")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	l.scan()
	job := l.status().Files[0]
	if err := l.validateOriginal(&job); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := l.validateOriginal(&job); err == nil {
		t.Fatal("accepted changed original")
	}
}
func TestLibraryDiscoveryUploadPersistenceAndProfileChanges(t *testing.T) {
	_, path := preparedClip(t)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "source.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	s := libraryServer(t)
	l := s.library
	if w := videoUpload(t, s, "clip.mp4", data); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := videoUpload(t, s, "clip.mp4", []byte("replace")); w.Code != 409 {
		t.Fatal("duplicate replaced original", w.Code)
	}
	if w := videoUpload(t, s, "bad.txt", data); w.Code != 400 {
		t.Fatal("non-MP4 accepted")
	}
	if err := os.Symlink(filepath.Join(l.root, "originals", "clip.mp4"), filepath.Join(l.root, "originals", "link.mp4")); err != nil {
		t.Fatal(err)
	}
	l.scan()
	if len(l.status().Files) != 1 {
		t.Fatal("symlink discovered")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.worker(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	eventually(t, func() bool { files := l.status().Files; return len(files) == 1 && files[0].State == "ready" })
	entry := l.status().Files[0]
	s.setForwarding(true)
	body := `{"action":"play","id":"` + entry.ID + `","loop":true}`
	if w := dashboardRequest(s, "PUT", "/api/playback", body); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.broadcast.tick(time.Now())
	started := s.broadcast.clip.started
	if w := dashboardRequest(s, "PUT", "/api/playback", `{"action":"resume"}`); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if !s.broadcast.clip.streaming || s.broadcast.clip.started != started {
		t.Fatal("duplicate resume restarted an already playing file")
	}
	if w := dashboardRequest(s, "PUT", "/api/playback", `{"action":"seek","position":1.8}`); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if s.broadcast.playbackStatus(time.Now()).Position > 1.8 {
		t.Fatal("seek rounded forward")
	}
	if w := dashboardRequest(s, "PUT", "/api/playback", `{"action":"seek","position":999}`); w.Code != 400 {
		t.Fatal("invalid seek accepted")
	}
	if w := dashboardRequest(s, "PUT", "/api/playback", `{"action":"pause"}`); w.Code != 204 {
		t.Fatal(w.Code)
	}
	s.broadcast.tick(time.Now())
	if !s.broadcast.status().Active {
		t.Fatal("pause did not show BRB")
	}
	s.broadcast.setManual(true)
	if w := dashboardRequest(s, "PUT", "/api/playback", `{"action":"resume"}`); w.Code != 409 {
		t.Fatal("resume bypassed manual BRB")
	}
	s.broadcast.setManual(false)
	if s.broadcast.playbackStatus(time.Now()).State != "paused" {
		t.Fatal("manual BRB cleared explicit pause")
	}
	s.setForwarding(false)
	if w := assetRequest(t, s, map[string]string{"fps": "30"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, _, err := l.open(entry.ID); err == nil {
		t.Fatal("old profile could play during conversion")
	}
	eventually(t, func() bool { e := l.status().Files[0]; return e.State == "ready" && e.Profile.FPS == 30 })
	reloaded := New(s.cfg, s.log)
	if err := reloaded.initialize(); err != nil {
		t.Fatal(err)
	}
	reloaded.library.scan()
	reloaded.library.scan()
	job := reloaded.library.status().Files[0]
	// Job identity/internal fields are populated in the same in-memory snapshot.
	reloaded.library.prepare(context.Background(), &job)
	if reloaded.library.status().Files[0].State != "ready" || reloaded.forwarding.Load() || reloaded.broadcast.clip != nil {
		t.Fatal("restart lost library or auto-started playback")
	}
	original, err := os.ReadFile(filepath.Join(l.root, "originals", "clip.mp4"))
	if err != nil || !bytes.Equal(original, data) {
		t.Fatal("original was not preserved")
	}
	if err := os.WriteFile(filepath.Join(l.root, "originals", "new.mp4"), data, 0600); err != nil {
		t.Fatal(err)
	}
	l.scan()
	l.scan()
	eventually(t, func() bool { return len(l.status().Files) == 2 })
}

func TestLibraryAuthenticationAndUploadLimits(t *testing.T) {
	s := dashboardServer(t)
	for _, path := range []string{"/api/library", "/api/library/upload", "/api/playback", "/api/broadcast-preview", "/library.js"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatal("unprotected route", path, w.Code)
		}
	}
	if w := dashboardRequest(s, "PUT", "/api/playback", `{"action":"play"}`); w.Code != 409 {
		t.Fatal("library available without BRB")
	}
	requireFFmpeg(t)
	s = libraryServer(t)
	s.library.limit = 16
	if w := videoUpload(t, s, "huge.mp4", []byte(strings.Repeat("x", 17))); w.Code != 413 {
		t.Fatal("upload limit ignored", w.Code)
	}
	files, err := os.ReadDir(filepath.Join(s.library.root, "originals"))
	if err != nil || len(files) != 0 {
		t.Fatal("partial upload left behind")
	}
	r := httptest.NewRequest("PUT", "/api/playback", strings.NewReader(`{"action":"stop"}`))
	r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("missing control header accepted")
	}
	for _, name := range []string{"../bad.mp4", ".hidden.mp4", "bad\\name.mp4", "bad\nname.mp4"} {
		if validClipName(name) {
			t.Fatal("unsafe name accepted")
		}
	}
	var status map[string]any
	if err := json.Unmarshal(dashboardRequest(s, "GET", "/status", "").Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["library_enabled"] != true {
		t.Fatal("missing library status")
	}
}

func preparedClip(t *testing.T) (*clipIndex, string) {
	t.Helper()
	requireFFmpeg(t)
	if _, err := exec.LookPath("ffprobe"); err != nil {
		if os.Getenv("REQUIRE_FFMPEG_TESTS") == "1" {
			t.Fatal(err)
		}
		t.Skip("FFprobe required")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "source.mp4")
	dst := filepath.Join(dir, "video.flv")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=30", "-t", "3", "-c:v", "libx264", "-threads", "2", src).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	profile := config.BRBProfile{Width: 320, Height: 180, FPS: 25, SampleRate: 48000}
	if err := prepareClip(context.Background(), src, dst, profile, func(float64) {}); err != nil {
		t.Fatal(err)
	}
	index, err := indexClip(dst)
	if err != nil {
		t.Fatal(err)
	}
	if index.Duration < 2900*time.Millisecond || index.Duration > 3200*time.Millisecond {
		t.Fatal("wrong duration", index.Duration)
	}
	return index, dst
}

func TestClipPreparationAndDiskSeek(t *testing.T) {
	index, path := preparedClip(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := indexClipContext(cancelled, path); err == nil {
		t.Fatal("indexing ignored cancellation")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	truncated := filepath.Join(filepath.Dir(path), "truncated.flv")
	if err := os.WriteFile(truncated, data[:len(data)-1], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := indexClip(truncated); err == nil {
		t.Fatal("accepted truncated conversion")
	}
	if len(index.Keys) < 3 || index.Video == nil || index.Audio == nil {
		t.Fatal("missing seek points or silent audio")
	}
	reader, err := openClip(path, index)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	position, err := reader.seek(1750 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if position > 1750*time.Millisecond || position < 750*time.Millisecond {
		t.Fatal("seek exceeded one-second precision", position)
	}
	first, err := reader.next()
	if err != nil || !isVideoFrame(first) || first.Body[0]>>4 != 1 {
		t.Fatal("seek must start at a keyframe", err)
	}
	for {
		_, err = reader.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestClipSelectionPauseBRBLoopAndStop(t *testing.T) {
	index, path := preparedClip(t)
	s, b, sub := testBRB(t)
	drain := func() {
		for len(sub.packets) > 0 {
			m := <-sub.packets
			b.hub.consumed(sub, m)
		}
	}
	start := time.Now()
	reader, err := openClip(path, index)
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.clip = &clipPlayback{ID: "test", Name: "test.mp4", reader: reader, loop: true}
	b.mu.Unlock()
	b.tick(start)
	drain()
	if b.playbackStatus(start).State != "playing" {
		t.Fatal("file did not start without OBS")
	}
	for i := 1; i < 80; i++ {
		b.tick(start.Add(time.Duration(i) * 50 * time.Millisecond))
		drain()
	}
	if b.playbackStatus(start.Add(4*time.Second)).State != "playing" {
		t.Fatal("loop stopped")
	}
	b.setManual(true)
	pos := b.playbackStatus(time.Now()).Position
	b.tick(start.Add(5 * time.Second))
	drain()
	if b.playbackStatus(start.Add(5*time.Second)).Position != pos || !b.status().Active {
		t.Fatal("manual BRB did not freeze playback")
	}
	b.setManual(false)
	b.tick(start.Add(6 * time.Second))
	drain()
	if b.playbackStatus(start.Add(6*time.Second)).State != "playing" {
		t.Fatal("manual BRB did not resume playback")
	}
	b.ingest(packet(rtmp.Video, 0, 0x17, 1, 0, 0, 0, 0), start.Add(6*time.Second))
	if b.clip == nil {
		t.Fatal("OBS replaced file")
	}
	s.setForwarding(false)
	if b.clip != nil {
		t.Fatal("master off did not stop file")
	}
}

func TestFileBRBSeekAndOBSRemainDecodableOnOneTimeline(t *testing.T) {
	idx, path := preparedClip(t)
	media, profile, dir := preparedBRB(t, 25)
	s := dashboardServer(t)
	s.cfg.QueueBytes = 1 << 20
	s.setForwarding(true)
	b := newBroadcast(s, media)
	s.broadcast = b
	sub := b.hub.subscribe()
	defer b.hub.unsubscribe(sub)
	var packets []*rtmp.Message
	drain := func() {
		for len(sub.packets) > 0 {
			m := <-sub.packets
			b.hub.consumed(sub, m)
			packets = append(packets, m)
		}
	}
	now := time.Now()
	tick := func(count int) {
		for i := 0; i < count; i++ {
			b.tick(now)
			drain()
			now = now.Add(20 * time.Millisecond)
		}
	}
	tick(30)
	reader, err := openClip(path, idx)
	if err != nil {
		t.Fatal(err)
	}
	b.clip = &clipPlayback{ID: "test", reader: reader, loop: true}
	tick(200)
	epoch := b.previewEpoch
	tick(160)
	if b.previewEpoch != epoch {
		t.Fatal("loop reset the live preview")
	}
	b.clip.freeze(now)
	b.clip.paused = true
	tick(30)
	if !b.status().Active {
		t.Fatal("file pause failed to show BRB")
	}
	b.clip.position = 1500 * time.Millisecond
	b.clip.paused = false
	tick(60)
	b.clip.loop = false
	tick(150)
	if b.clip != nil || !b.status().Active {
		t.Fatal("play-once completion did not return to BRB")
	}
	// A fresh OBS keyframe resumes after the file, without restarting outputs.
	b.ingest(media.video.header, now)
	b.ingest(media.audio.header, now)
	b.ingest(media.audio.frames[0], now)
	for _, frame := range media.video.frames[:profile.FPS] {
		b.ingest(frame, now.Add(frame.Timestamp))
		drain()
	}
	if !b.live {
		t.Fatal("did not return to OBS")
	}
	var previous [2]time.Duration
	for _, m := range packets {
		if isVideoFrame(m) || m.Type == rtmp.Audio && len(m.Body) > 1 && m.Body[1] == 1 {
			track := 0
			if m.Type == rtmp.Audio {
				track = 1
			}
			if m.Timestamp < previous[track] {
				t.Fatal("timestamp reversal")
			}
			previous[track] = m.Timestamp
		}
	}
	output := filepath.Join(dir, "file-switches.flv")
	writeTestFLV(t, output, packets)
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-v", "error", "-xerror", "-i", output, "-f", "null", "-").CombinedOutput(); err != nil {
		t.Fatalf("file switching failed to decode: %v %s", err, out)
	}
}
