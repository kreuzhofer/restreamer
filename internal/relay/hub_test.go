package relay

import (
	"bytes"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func packet(typ uint8, ts time.Duration, body ...byte) *rtmp.Message {
	return &rtmp.Message{Type: typ, Timestamp: ts, Body: body}
}
func videoConfig() *rtmp.Message { return packet(rtmp.Video, 0, 0x17, 0, 0, 0, 0, 1, 0x64, 0, 0x1f) }
func audioConfig() *rtmp.Message { return packet(rtmp.Audio, 0, 0xaf, 0, 0x12, 0x10) }
func keyframe(ts time.Duration) *rtmp.Message {
	return packet(rtmp.Video, ts, 0x17, 1, 0, 0, 0, 0, 0, 0, 2, 0x65, 0x88)
}

func TestReconnectWaitsForKeyframeAndReplaysHeaders(t *testing.T) {
	h := newHub(1 << 20)
	for _, m := range []*rtmp.Message{videoConfig(), audioConfig()} {
		if err := h.publish(m); err != nil {
			t.Fatal(err)
		}
	}
	s := h.subscribe()
	defer h.unsubscribe(s)
	_ = h.publish(packet(rtmp.Video, time.Second, 0x27, 1, 0, 0, 0, 1))
	_ = h.publish(packet(rtmp.Audio, time.Second, 0xaf, 1, 1))
	if len(s.packets) != 0 {
		t.Fatal("forwarded before keyframe")
	}
	k := keyframe(2 * time.Second)
	if err := h.publish(k); err != nil {
		t.Fatal(err)
	}
	for _, want := range []*rtmp.Message{videoConfig(), audioConfig(), k} {
		got := <-s.packets
		if got.Timestamp != k.Timestamp || !bytes.Equal(got.Body, want.Body) {
			t.Fatalf("incorrect bootstrap: %+v", got)
		}
		h.consumed(s, got)
	}
	if s.bytes != 0 {
		t.Fatal("queue byte accounting did not return to zero")
	}
	if h.headers[1].Timestamp != 0 {
		t.Fatal("mutated cached header")
	}
}

func TestSlowOutputDoesNotBlockHealthyOutput(t *testing.T) {
	h := newHub(1024)
	_ = h.publish(videoConfig())
	slow, healthy := h.subscribe(), h.subscribe()
	for i := range 100 {
		if err := h.publish(keyframe(time.Duration(i) * time.Second)); err != nil {
			t.Fatal(err)
		}
		for len(healthy.packets) > 0 {
			m := <-healthy.packets
			h.consumed(healthy, m)
		}
	}
	select {
	case <-slow.failed:
	default:
		t.Fatal("slow output was not disconnected")
	}
	select {
	case <-healthy.failed:
		t.Fatal("healthy output was affected")
	default:
	}
	if slow.bytes > h.limit {
		t.Fatal("unbounded queue")
	}
}

func TestRejectUnsupportedCodec(t *testing.T) {
	h := newHub(1 << 20)
	for _, m := range []*rtmp.Message{packet(rtmp.Video, 0, 0x1c, 0, 0, 0, 0), packet(rtmp.Video, 0, 0x90, 'h', 'v', 'c', '1'), packet(rtmp.Audio, 0, 0x2f, 0)} {
		if err := h.publish(m); err != errCodec {
			t.Fatalf("expected codec error, got %v", err)
		}
	}
}
