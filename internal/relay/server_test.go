package relay

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

type sink struct {
	listener    net.Listener
	packets     chan *rtmp.Message
	mu          sync.Mutex
	conn        net.Conn
	connections int
}

func newSink(t *testing.T) *sink {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &sink{listener: l, packets: make(chan *rtmp.Message, 4096)}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			n, err := l.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer n.Close()
				stop := context.AfterFunc(ctx, func() { n.Close() })
				defer stop()
				c, err := rtmp.Accept(n, func(app, key string) bool { return app == "app" && key == "target-key" })
				if err != nil {
					return
				}
				s.mu.Lock()
				s.conn = n
				s.connections++
				s.mu.Unlock()
				for {
					m, err := c.Read()
					if err != nil {
						return
					}
					if m.Type == rtmp.Video || m.Type == rtmp.Audio || m.Type == rtmp.Data {
						select {
						case s.packets <- m:
						case <-ctx.Done():
							return
						}
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { cancel(); l.Close(); wg.Wait() })
	return s
}

func (s *sink) target(name string) config.Target {
	return config.Target{Name: name, URL: "rtmp://" + s.listener.Addr().String() + "/app", StreamKey: "target-key"}
}
func (s *sink) disconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		s.conn.Close()
	}
}
func (s *sink) count() int { s.mu.Lock(); defer s.mu.Unlock(); return s.connections }

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(6 * time.Second)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("condition did not become true")
		case <-tick.C:
		}
	}
}

func startRelay(t *testing.T, targets []config.Target) (*Server, string) {
	t.Helper()
	return startRelayWithLogger(t, targets, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func startRelayWithLogger(t *testing.T, targets []config.Target, log *slog.Logger, paused ...bool) (*Server, string) {
	t.Helper()
	cfg := config.Config{Listen: ":1935", HealthListen: ":8080", Application: "live", StreamKey: "input-key-1234567890", QueueBytes: 1 << 20, Targets: targets}
	cfg.DashboardUsername, cfg.DashboardPassword = "admin", "test-password"
	s := New(cfg, log)
	// Existing relay tests explicitly opt into forwarding. Production starts off.
	if len(paused) == 0 || !paused[0] {
		s.setForwarding(true)
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
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("relay did not shut down")
		}
	})
	return s, "rtmp://" + l.Addr().String() + "/live"
}

func publishInput(t *testing.T, address string) *rtmp.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := rtmp.Dial(ctx, address, "input-key-1234567890")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Net.Close() })
	return c
}

func writePacket(t *testing.T, c *rtmp.Conn, m *rtmp.Message) {
	t.Helper()
	copy := *m
	copy.MessageStreamID = rtmp.StreamID
	copy.ChunkStreamID = 6
	if m.Type == rtmp.Audio {
		copy.ChunkStreamID = 4
	}
	if err := c.Write(&copy); err != nil {
		t.Fatal(err)
	}
}

func receive(t *testing.T, s *sink) *rtmp.Message {
	t.Helper()
	select {
	case m := <-s.packets:
		return m
	case <-time.After(3 * time.Second):
		t.Fatal("sink received no packet")
		return nil
	}
}

func TestFanoutAndIndependentReconnect(t *testing.T) {
	a, b := newSink(t), newSink(t)
	s, address := startRelay(t, []config.Target{a.target("one"), b.target("two")})
	c := publishInput(t, address)
	eventually(t, func() bool {
		return s.outputs[0].snapshot().State == "waiting_for_keyframe" && s.outputs[1].snapshot().State == "waiting_for_keyframe"
	})
	writePacket(t, c, videoConfig())
	writePacket(t, c, audioConfig())
	writePacket(t, c, keyframe(10*time.Second))
	for _, dest := range []*sink{a, b} {
		for _, want := range []*rtmp.Message{videoConfig(), audioConfig(), keyframe(10 * time.Second)} {
			got := receive(t, dest)
			if !bytes.Equal(got.Body, want.Body) || got.Timestamp != 0 {
				t.Fatalf("payload changed or timestamp not rebased: %+v", got)
			}
		}
	}
	// Mid-stream headers may carry zero timestamps, even though the first
	// keyframe arrived at ten seconds. They must still reach both destinations.
	writePacket(t, c, audioConfig())
	for _, dest := range []*sink{a, b} {
		if got := receive(t, dest); !bytes.Equal(got.Body, audioConfig().Body) {
			t.Fatal("mid-stream codec header was dropped")
		}
	}
	a.disconnect()
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "retrying" })
	next := keyframe(11 * time.Second)
	writePacket(t, c, next)
	got := receive(t, b)
	if !bytes.Equal(got.Body, next.Body) || got.Timestamp != time.Second {
		t.Fatal("healthy output interrupted")
	}
	eventually(t, func() bool { return a.count() == 2 && s.outputs[0].snapshot().State == "waiting_for_keyframe" })
	writePacket(t, c, packet(rtmp.Video, 12*time.Second, 0x27, 1, 0, 0, 0, 1))
	if got := receive(t, b); got.Timestamp != 2*time.Second {
		t.Fatal("healthy stream restarted")
	}
	writePacket(t, c, keyframe(13*time.Second))
	for _, want := range []*rtmp.Message{videoConfig(), audioConfig(), keyframe(13 * time.Second)} {
		got := receive(t, a)
		if !bytes.Equal(got.Body, want.Body) || got.Timestamp != 0 {
			t.Fatal("reconnect did not bootstrap at keyframe")
		}
	}
	if b.count() != 1 {
		t.Fatal("healthy destination was reconnected")
	}
	c.Net.Close()
	eventually(t, func() bool { return !s.active.Load() })
	if state := readStage(t, s); state.Stage != "LIVE" || state.Source != "off" {
		t.Fatal("publisher loss changed established LIVE intent", state)
	}
	// The server-owned LIVE session retains destinations across publisher loss.
	// A new OBS session must provide fresh headers before it can resume delivery,
	// and its reset timestamps must continue the existing destination timeline.
	c2 := publishInput(t, address)
	writePacket(t, c2, keyframe(0))
	select {
	case <-a.packets:
		t.Fatal("previous session's headers reused")
	case <-time.After(30 * time.Millisecond):
	}
	writePacket(t, c2, videoConfig())
	writePacket(t, c2, keyframe(time.Second))
	if got := receive(t, a); got.Body[1] != 0 {
		t.Fatal("new session missing header")
	}
	if got := receive(t, a); !bytes.Equal(got.Body, keyframe(time.Second).Body) || got.Timestamp <= 0 {
		t.Fatal("new publisher did not continue destination timeline", got.Timestamp)
	}
	if a.count() != 2 || b.count() != 1 {
		t.Fatal("publisher reconnect replaced a healthy destination session")
	}
}

func TestAuthenticationSinglePublisherAndStatus(t *testing.T) {
	dest := newSink(t)
	s, address := startRelay(t, []config.Target{dest.target("one")})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c, _, err := rtmp.Dial(ctx, address, "wrong"); err == nil {
		c.Net.Close()
		t.Fatal("accepted wrong key")
	}
	if c, _, err := rtmp.Dial(ctx, strings.TrimSuffix(address, "live")+"wrong", "input-key-1234567890"); err == nil {
		c.Net.Close()
		t.Fatal("accepted wrong application")
	}
	c := publishInput(t, address)
	if second, _, err := rtmp.Dial(ctx, address, "input-key-1234567890"); err == nil {
		second.Net.Close()
		t.Fatal("accepted second publisher")
	}
	if !s.active.Load() {
		t.Fatal("second publisher displaced first")
	}
	request := httptest.NewRequest("GET", "/status", nil)
	request.SetBasicAuth("admin", "test-password")
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"publishing":true`) {
		t.Fatal("bad status")
	}
	if strings.Contains(response.Body.String(), "target-key") || strings.Contains(response.Body.String(), "input-key") || strings.Contains(response.Body.String(), "rtmp://") {
		t.Fatal("status exposed credentials")
	}
	c.Net.Close()
	eventually(t, func() bool { return !s.active.Load() })
}

func TestShutdownInterruptsPendingHandshake(t *testing.T) {
	dest := newSink(t)
	_, address := startRelay(t, []config.Target{dest.target("one")})
	host := strings.TrimSuffix(strings.TrimPrefix(address, "rtmp://"), "/live")
	c, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	// startRelay cleanup cancels while this connection has sent no handshake.
}

func TestDisabledTargetsNeverConnect(t *testing.T) {
	dest := newSink(t)
	noKey := dest.target("no-key")
	noKey.StreamKey = ""
	flagOff := dest.target("flag-off")
	flagOff.Enabled = "false"
	s, address := startRelay(t, []config.Target{dest.target("active"), noKey, flagOff})
	c := publishInput(t, address)
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "waiting_for_keyframe" })
	writePacket(t, c, videoConfig())
	writePacket(t, c, keyframe(time.Second))
	_ = receive(t, dest)
	_ = receive(t, dest)
	c.Net.Close()
	eventually(t, func() bool { return !s.active.Load() })
	if dest.count() != 1 {
		t.Fatal("a disabled target connected")
	}
	for _, o := range s.outputs[1:] {
		status := o.snapshot()
		if status.State != "disabled" || status.Enabled || status.Attempts != 0 {
			t.Fatalf("disabled target ran: %+v", status)
		}
	}
}

func TestAllTargetsDisabledAcceptsInput(t *testing.T) {
	s, address := startRelay(t, []config.Target{{Name: "disabled"}})
	c := publishInput(t, address)
	writePacket(t, c, videoConfig())
	writePacket(t, c, keyframe(time.Second))
	if !s.active.Load() {
		t.Fatal("publisher not accepted")
	}
	if status := s.outputs[0].snapshot(); status.State != "disabled" || status.Attempts != 0 {
		t.Fatalf("disabled output ran: %+v", status)
	}
}
