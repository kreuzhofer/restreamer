package relay

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func previewVideoConfig() *rtmp.Message {
	sps := []byte{0x67, 0x42, 0xc0, 0x28, 0xd9, 0, 0x78, 2, 0x27, 0xe5, 0x84, 0, 0, 3, 0, 4, 0, 0, 3, 0, 0xf0, 0x3c, 0x60, 0xc9, 0x20}
	pps := []byte{0x68, 0xce, 0x3c, 0x80}
	body := []byte{0x17, 0, 0, 0, 0, 1, 0x42, 0xc0, 0x28, 0xff, 0xe1, 0, byte(len(sps))}
	body = append(body, sps...)
	body = append(body, 1, 0, byte(len(pps)))
	body = append(body, pps...)
	return packet(rtmp.Video, 0, body...)
}

func TestPreviewMuxPreservesMediaAndTiming(t *testing.T) {
	m := previewMux{}
	for _, p := range []*rtmp.Message{previewVideoConfig(), audioConfig()} {
		if data, err := m.push(p); err != nil || len(data) != 0 {
			t.Fatal("header rejected", err)
		}
	}
	key := packet(rtmp.Video, time.Second, 0x17, 1, 0, 0, 33, 0, 0, 0, 2, 0x65, 0x88)
	initData, err := m.push(key)
	if err != nil {
		t.Fatal(err)
	}
	var init fmp4.Init
	if err := init.Unmarshal(bytes.NewReader(initData)); err != nil || len(init.Tracks) != 2 {
		t.Fatal("invalid initialization", err)
	}
	data, err := m.push(packet(rtmp.Video, 1040*time.Millisecond, 0x27, 1, 0xff, 0xff, 0xfe, 0, 0, 0, 2, 0x41, 0x88))
	if err != nil {
		t.Fatal(err)
	}
	var parts fmp4.Parts
	if err := parts.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	sample := parts[0].Tracks[0].Samples[0]
	if sample.Duration != 40 || sample.PTSOffset != 33 || sample.IsNonSyncSample || !bytes.Equal(sample.Payload, key.Body[5:]) {
		t.Fatalf("modified video: %+v", sample)
	}
	data, err = m.push(packet(rtmp.Audio, 1046*time.Millisecond, 0xaf, 1, 1, 2, 3))
	if err != nil {
		t.Fatal(err)
	}
	parts = nil
	if err := parts.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	track := parts[0].Tracks[0]
	if track.ID != 2 || track.BaseTime != 2028 || track.Samples[0].Duration != 1024 || !bytes.Equal(track.Samples[0].Payload, []byte{1, 2, 3}) {
		t.Fatal("modified audio timing or payload")
	}
	data, err = m.push(keyframe(1080 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	parts = nil
	if err = parts.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if sample := parts[0].Tracks[0].Samples[0]; sample.PTSOffset != -2 || !sample.IsNonSyncSample {
		t.Fatal("B-frame composition offset lost")
	}
	// Scale milliseconds rather than nanoseconds so long previews do not overflow.
	data, err = m.push(packet(rtmp.Audio, time.Second+72*time.Hour, 0xaf, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	parts = nil
	if err = parts.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if parts[0].Tracks[0].BaseTime != 72*3600*44100 {
		t.Fatal("long-running audio timestamp overflow")
	}
	changed := previewVideoConfig()
	changed.Body[8]++
	if _, err = m.push(changed); err == nil {
		t.Fatal("codec change not surfaced")
	}
}

func TestPreviewVideoOnlyAndQueueIsolation(t *testing.T) {
	m := previewMux{}
	if _, err := m.push(previewVideoConfig()); err != nil {
		t.Fatal(err)
	}
	initData, err := m.push(keyframe(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var init fmp4.Init
	if err = init.Unmarshal(bytes.NewReader(initData)); err != nil || len(init.Tracks) != 1 {
		t.Fatal("video-only initialization", err)
	}
	if _, err = m.push(keyframe(500 * time.Millisecond)); err == nil {
		t.Fatal("backwards video timestamp accepted")
	}
	server := dashboardServer(t)
	server.setForwarding(true)
	h := newHub(100, server.outputs[0])
	slow := h.subscribe()
	healthy := h.subscribeOutput(server.outputs[0])
	if err = h.publish(videoConfig()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if err = h.publish(keyframe(time.Duration(i) * time.Second)); err != nil {
			t.Fatal(err)
		}
		count := 1
		if i == 0 {
			count++
		} // First keyframe also sends the cached header.
		for j := 0; j < count; j++ {
			p := <-healthy.packets
			h.consumed(healthy, p)
			server.outputs[0].recordSent(p)
		}
	}
	select {
	case <-slow.failed:
	default:
		t.Fatal("slow preview was not bounded")
	}
	if healthy.broken || server.outputs[0].snapshot().DroppedFrames != 0 || server.outputs[0].snapshot().Frames != 30 {
		t.Fatal("slow preview affected output")
	}
	h.unsubscribe(slow)
	h.unsubscribe(healthy)
}

func TestPreviewViewerLimit(t *testing.T) {
	s := dashboardServer(t)
	for i := 0; i < cap(s.previewSlots); i++ {
		s.previewSlots <- struct{}{}
	}
	if w := dashboardRequest(s, "GET", "/api/preview", ""); w.Code != 429 {
		t.Fatal("unbounded preview viewers")
	}
}

func TestPreviewInvalidConfig(t *testing.T) {
	for _, p := range []*rtmp.Message{packet(rtmp.Video, 0, 0x17, 0, 0, 0, 0, 1, 0x64, 0, 0x1f), packet(rtmp.Video, 0, 0x17, 0, 0, 0, 0), packet(rtmp.Audio, 0, 0xaf, 0, 0xff)} {
		m := previewMux{}
		if _, err := m.push(p); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}

func TestPreviewHTTPWhileForwardingOff(t *testing.T) {
	s := dashboardServer(t)
	if w := dashboardRequest(s, "GET", "/api/preview", ""); w.Code != 503 {
		t.Fatal("offline preview", w.Code)
	}
	h := newHub(1<<20, s.outputs...)
	done := make(chan struct{})
	s.previewHub, s.previewDone = h, done
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/preview", nil)
	r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	result := make(chan []byte, 1)
	go func() {
		res, err := ts.Client().Do(r)
		if err != nil {
			result <- nil
			return
		}
		defer res.Body.Close()
		if res.StatusCode != 200 || res.Header.Get("X-Preview-Codecs") == "" {
			result <- nil
			return
		}
		data, _ := io.ReadAll(res.Body)
		result <- data
	}()
	eventually(t, func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.subs) == 1 })
	for _, p := range []*rtmp.Message{previewVideoConfig(), audioConfig(), keyframe(time.Second), keyframe(1040 * time.Millisecond)} {
		if err := h.publish(p); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		for sub := range h.subs {
			if sub.bytes == 0 {
				return true
			}
		}
		return false
	})
	close(done)
	select {
	case data := <-result:
		if !bytes.Contains(data, []byte("ftyp")) || !bytes.Contains(data, []byte("moof")) {
			t.Fatal("no preview media")
		}
	case <-ctx.Done():
		t.Fatal("preview did not close")
	}
	if s.forwarding.Load() || s.outputs[0].snapshot().Attempts != 0 {
		t.Fatal("preview enabled forwarding")
	}
	eventually(t, func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.subs) == 0 })
}
