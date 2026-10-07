package rtmp

import (
	"bytes"
	"context"
	"net"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bluenviron/gortmplib"
	"github.com/bluenviron/gortmplib/pkg/amf0"
	"github.com/bluenviron/gortmplib/pkg/message"
)

func listener(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestClientInteroperatesWithReferenceServer(t *testing.T) {
	l := listener(t)
	done := make(chan error, 1)
	go func() {
		n, err := l.Accept()
		if err != nil {
			done <- err
			return
		}
		defer n.Close()
		_ = n.SetDeadline(time.Now().Add(5 * time.Second))
		s := &gortmplib.ServerConn{RW: n}
		if err = s.Initialize(); err != nil {
			done <- err
			return
		}
		if err = s.AcceptConn(); err != nil {
			done <- err
			return
		}
		if !s.Publish || s.URL.Path != "/app/literal-key" {
			done <- &testError{"wrong publish path"}
			return
		}
		if err = s.AcceptAction(); err != nil {
			done <- err
			return
		}
		if err = s.Write(&message.UserControlPingRequest{ServerTime: 1234}); err != nil {
			done <- err
			return
		}
		seenPing, seenData := false, false
		for !seenPing || !seenData {
			m, err := s.Read()
			if err != nil {
				done <- err
				return
			}
			switch m := m.(type) {
			case *message.UserControlPingResponse:
				seenPing = m.ServerTime == 1234
			case *message.DataAMF0:
				seenData = len(m.Payload) == 2 && m.Payload[0] == "onMetaData"
			}
		}
		done <- nil
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, stream, err := Dial(ctx, "rtmp://"+l.Addr().String()+"/app", "literal-key")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Net.Close()
	readerDone := make(chan struct{})
	go func() { defer close(readerDone); _, _ = c.Read() }()
	body, _ := amf0.Data{"onMetaData", amf0.Object{{Key: "width", Value: float64(3840)}}}.Marshal()
	if err := c.Write(&Message{ChunkStreamID: 5, MessageStreamID: stream, Type: Data, Body: body}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	c.Net.Close()
	<-readerDone
}

type testError struct{ text string }

func (e *testError) Error() string { return e.text }

func TestServerInteroperatesWithReferenceClient(t *testing.T) {
	l := listener(t)
	done := make(chan error, 1)
	want := []byte{0, 0, 0, 2, 0x65, 0x88}
	go func() {
		n, err := l.Accept()
		if err != nil {
			done <- err
			return
		}
		defer n.Close()
		c, err := Accept(n, func(app, key string) bool { return app == "live" && key == "secret" })
		if err != nil {
			done <- err
			return
		}
		_ = n.SetReadDeadline(time.Now().Add(5 * time.Second))
		m, err := c.Read()
		if err != nil {
			done <- err
			return
		}
		if m.Type != Video || !bytes.Equal(m.Body[5:], want) {
			done <- &testError{"payload changed"}
			return
		}
		done <- nil
	}()
	u, _ := url.Parse("rtmp://" + l.Addr().String() + "/live#secret")
	c := &gortmplib.Client{URL: u, Publish: true}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Write(&message.Video{ChunkStreamID: 6, MessageStreamID: StreamID, Codec: 7, IsKeyFrame: true, Type: message.VideoTypeAU, AU: want}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRTMPSRejectsUntrustedCertificate(t *testing.T) {
	s := httptest.NewTLSServer(nil)
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := Dial(ctx, strings.Replace(s.URL, "https://", "rtmps://", 1)+"/app", "key")
	if err == nil {
		c.Net.Close()
		t.Fatal("accepted untrusted TLS certificate")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDialCancellationInterruptsHandshake(t *testing.T) {
	l := listener(t)
	accepted := make(chan net.Conn, 1)
	go func() {
		n, err := l.Accept()
		if err == nil {
			accepted <- n
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, _, err := Dial(ctx, "rtmp://"+l.Addr().String()+"/app", "key")
	if err == nil || time.Since(started) > time.Second {
		t.Fatal("handshake was not promptly canceled")
	}
	select {
	case n := <-accepted:
		n.Close()
	default:
	}
}
