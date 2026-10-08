package relay

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		if os.Getenv("REQUIRE_FFMPEG_TESTS") == "1" {
			t.Fatal("FFmpeg is required")
		}
		t.Skip("FFmpeg not installed; media integration tests require FFmpeg")
	}
}
func preparedBRB(t *testing.T, fps int) (*brbMedia, config.BRBProfile, string) {
	t.Helper()
	requireFFmpeg(t)
	dir := t.TempDir()
	profile := config.BRBProfile{Width: 320, Height: 180, FPS: fps, SampleRate: 48000}
	if err := defaultBRBImage(filepath.Join(dir, "image.png")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	media, err := encodeBRB(ctx, profile, dir, false, 50)
	if err != nil {
		t.Fatal(err)
	}
	media.settings = brbSettings{Profile: profile, Volume: 50}
	return media, profile, dir
}

func TestBRBMediaSwitchingDecodesAt25And30FPS(t *testing.T) {
	for _, fps := range []int{25, 30} {
		t.Run(fmt.Sprint(fps), func(t *testing.T) {
			media, profile, dir := preparedBRB(t, fps)
			s := dashboardServer(t)
			s.cfg.QueueBytes = 1 << 20
			s.setForwarding(true)
			b := newBroadcast(s, media)
			s.broadcast = b
			for _, h := range []*rtmp.Message{media.video.header, media.audio.header} {
				if err := s.validateBRBInput(h); err != nil {
					t.Fatal(err)
				}
			}
			livePath := filepath.Join(dir, "live.flv")
			cmdLive := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=320x180:rate=%d", fps), "-t", "2", "-c:v", "libx264", "-threads", "2", "-bf", "2", "-g", fmt.Sprint(fps), "-f", "flv", livePath)
			if out, err := cmdLive.CombinedOutput(); err != nil {
				t.Fatalf("live fixture: %v %s", err, out)
			}
			live, err := readMediaTrack(livePath, rtmp.Video, time.Second/time.Duration(fps))
			if err != nil {
				t.Fatal(err)
			}
			sub := b.hub.subscribe()
			var packets []*rtmp.Message
			drain := func() {
				for len(sub.packets) > 0 {
					m := <-sub.packets
					b.hub.consumed(sub, m)
					packets = append(packets, m)
				}
			}
			now := time.Now()
			// Fallback, OBS, manual fallback, returning OBS, disconnect fallback.
			for i := 0; i < 100; i++ {
				b.tick(now.Add(time.Duration(i) * 20 * time.Millisecond))
				drain()
			}
			sendLive := func(at time.Time) {
				b.ingest(live.header, at)
				b.ingest(media.audio.header, at)
				b.ingest(media.audio.frames[0], at)
				for _, v := range live.frames {
					b.ingest(v, at.Add(v.Timestamp))
					drain()
				}
				for _, a := range media.audio.frames {
					b.ingest(a, at.Add(a.Timestamp))
					drain()
				}
			}
			sendLive(now.Add(2 * time.Second))
			if b.status().Active {
				t.Fatal("did not resume live")
			}
			b.setManual(true)
			for i := 0; i < 100; i++ {
				b.tick(now.Add(4*time.Second + time.Duration(i)*20*time.Millisecond))
				drain()
			}
			b.inputLost()
			sendLive(now.Add(6 * time.Second))
			if !b.status().Active {
				t.Fatal("OBS overrode manual BRB")
			}
			b.setManual(false)
			sendLive(now.Add(8 * time.Second))
			b.inputLost()
			for i := 0; i < 100; i++ {
				b.tick(now.Add(10*time.Second + time.Duration(i)*20*time.Millisecond))
				drain()
			}
			previous := map[uint8]time.Duration{}
			for _, m := range packets {
				if len(m.Body) > 1 && m.Body[1] == 1 {
					if m.Timestamp < previous[m.Type] {
						t.Fatal("backwards media timestamp")
					}
					previous[m.Type] = m.Timestamp
				}
			}
			path := filepath.Join(dir, "switches.flv")
			writeTestFLV(t, path, packets)
			cmd := exec.Command("ffmpeg", "-hide_banner", "-v", "error", "-xerror", "-i", path, "-f", "null", "-")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%dfps switching did not decode: %v\n%s", profile.FPS, err, out)
			}
		})
	}
}
func writeTestFLV(t *testing.T, path string, packets []*rtmp.Message) {
	t.Helper()
	var buf bytes.Buffer
	buf.Write([]byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0})
	for _, m := range packets {
		n := len(m.Body)
		ts := uint32(m.Timestamp / time.Millisecond)
		buf.Write([]byte{m.Type, byte(n >> 16), byte(n >> 8), byte(n), byte(ts >> 16), byte(ts >> 8), byte(ts), byte(ts >> 24), 0, 0, 0})
		buf.Write(m.Body)
		var tail [4]byte
		binary.BigEndian.PutUint32(tail[:], uint32(n+11))
		buf.Write(tail[:])
	}
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func assetRequest(t *testing.T, s *Server, fields map[string]string, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if filename != "" {
		field := "music"
		if filepath.Ext(filename) == ".png" {
			field = "image"
		}
		f, err := writer.CreateFormFile(field, filename)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(data)
	}
	writer.Close()
	r := httptest.NewRequest("POST", "/api/brb/assets", &body)
	r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("X-Restreamer-Control", "1")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestBRBUploadsAreAtomicPersistentAndProfileLocked(t *testing.T) {
	requireFFmpeg(t)
	s := dashboardServer(t)
	s.cfg.QueueBytes = 1 << 20
	s.cfg.BRB = &config.BRBConfig{Enabled: "true", Directory: t.TempDir(), BRBProfile: config.BRBProfile{Width: 320, Height: 180, FPS: 30, SampleRate: 48000}}
	if err := s.initialize(); err != nil {
		t.Fatal(err)
	}
	original := s.broadcast.media
	for _, file := range []string{"bad.png", "bad.mp3", "bad.wav"} {
		w := assetRequest(t, s, nil, file, []byte("not media"))
		if w.Code != 422 || s.broadcast.media != original {
			t.Fatalf("bad upload replaced working assets: %s %d", file, w.Code)
		}
	}
	s.setForwarding(true)
	if w := assetRequest(t, s, map[string]string{"fps": "25"}, "", nil); w.Code != 409 {
		t.Fatal("changed profile while on", w.Code)
	}
	s.setForwarding(false)
	if w := assetRequest(t, s, map[string]string{"fps": "25", "volume": "25"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	imageData, err := os.ReadFile(filepath.Join(s.cfg.BRB.Directory, s.broadcast.media.settings.Generation, "image.png"))
	if err != nil {
		t.Fatal(err)
	}
	if w := assetRequest(t, s, nil, "custom.png", imageData); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Valid WAV upload is encoded and persisted. Silence is sufficient to exercise
	// the actual WAV demuxer/AAC encoder; no external files or real credentials.
	var wav bytes.Buffer
	wav.WriteString("RIFF")
	binary.Write(&wav, binary.LittleEndian, uint32(36+4800*2))
	wav.WriteString("WAVEfmt ")
	binary.Write(&wav, binary.LittleEndian, uint32(16))
	binary.Write(&wav, binary.LittleEndian, uint16(1))
	binary.Write(&wav, binary.LittleEndian, uint16(1))
	binary.Write(&wav, binary.LittleEndian, uint32(48000))
	binary.Write(&wav, binary.LittleEndian, uint32(96000))
	binary.Write(&wav, binary.LittleEndian, uint16(2))
	binary.Write(&wav, binary.LittleEndian, uint16(16))
	wav.WriteString("data")
	binary.Write(&wav, binary.LittleEndian, uint32(9600))
	wav.Write(make([]byte, 9600))
	if w := assetRequest(t, s, nil, "music.wav", wav.Bytes()); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	reloaded := New(s.cfg, s.log)
	if err := reloaded.initialize(); err != nil {
		t.Fatal(err)
	}
	settings := reloaded.broadcast.media.settings
	if settings.Profile.FPS != 25 || settings.Volume != 25 || !settings.Music || !settings.CustomImage || reloaded.forwarding.Load() {
		t.Fatalf("wrong restart settings: %+v", settings)
	}
	// The configured default is 30fps; the saved dashboard profile must win.
	if err := reloaded.validateBRBInput(reloaded.broadcast.media.video.header); err != nil {
		t.Fatal(err)
	}
	if w := assetRequest(t, reloaded, map[string]string{"remove_music": "true", "reset_image": "true"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if reloaded.broadcast.media.settings.Music || reloaded.broadcast.media.settings.CustomImage {
		t.Fatal("asset resets failed")
	}
	if w := dashboardRequest(reloaded, "GET", "/api/brb/image", ""); w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatal("missing authenticated preview")
	}
}

func TestBRBControlAndUploadAuthentication(t *testing.T) {
	s := dashboardServer(t)
	for _, path := range []string{"/api/brb", "/api/brb/image", "/api/brb/assets"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code != 401 {
			t.Fatal("unprotected BRB route", path, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/brb/assets", bytes.NewReader(nil))
	r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	r.Header.Set("X-Restreamer-Control", "1")
	r.Header.Set("Origin", "https://attacker.invalid")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin upload allowed")
	}
	if w := dashboardRequest(s, "PUT", "/api/brb", `{"enabled":true}`); w.Code != 409 {
		t.Fatal("unconfigured BRB enabled")
	}
}

func TestBRBPublisherReconnectKeepsDestinationSession(t *testing.T) {
	requireFFmpeg(t)
	dest := newSink(t)
	cfg := config.Config{Listen: ":1935", HealthListen: ":8080", Application: "live", StreamKey: "input-key-1234567890", QueueBytes: 1 << 20, Targets: []config.Target{dest.target("one")}, BRB: &config.BRBConfig{Enabled: "true", Directory: t.TempDir(), BRBProfile: config.BRBProfile{Width: 320, Height: 180, FPS: 25, SampleRate: 48000}}}
	s := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.initialize(); err != nil {
		t.Fatal(err)
	}
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
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("protected relay failed to stop")
		}
	})
	s.setForwarding(true)
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "streaming" })
	address := "rtmp://" + l.Addr().String() + "/live"
	send := func(c *rtmp.Conn) {
		writePacket(t, c, s.broadcast.media.video.header)
		writePacket(t, c, s.broadcast.media.audio.header)
		writePacket(t, c, s.broadcast.media.audio.frames[0])
		writePacket(t, c, s.broadcast.media.video.frames[0])
	}
	c := publishInput(t, address)
	send(c)
	eventually(t, func() bool { return !s.broadcast.status().Active })
	c.Net.Close()
	eventually(t, func() bool { return !s.active.Load() && s.broadcast.status().Active })
	s.broadcast.setManual(true)
	c = publishInput(t, address)
	send(c)
	eventually(t, func() bool { return s.inputFrames.Load() == 2 })
	if !s.broadcast.status().Active {
		t.Fatal("reconnecting publisher disabled manual BRB")
	}
	s.broadcast.setManual(false)
	send(c)
	eventually(t, func() bool { return !s.broadcast.status().Active })
	// A connected but stalled OBS is detected independently of the RTMP deadline.
	eventually(t, func() bool { return s.broadcast.status().Active && !s.active.Load() })
	if dest.count() != 1 {
		t.Fatalf("publisher transitions reopened destination: %d", dest.count())
	}
	before := s.outputs[0].snapshot().Frames
	eventually(t, func() bool { return s.outputs[0].snapshot().Frames > before+5 })
	s.setForwarding(false)
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "paused" })
	frames := s.outputs[0].snapshot().Frames
	time.Sleep(100 * time.Millisecond)
	if s.outputs[0].snapshot().Frames != frames {
		t.Fatal("BRB bypassed master off")
	}
}
