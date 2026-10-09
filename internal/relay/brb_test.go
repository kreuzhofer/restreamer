package relay

import (
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func testBRB(t *testing.T) (*Server, *broadcast, *subscription) {
	t.Helper()
	s := dashboardServer(t)
	s.cfg.QueueBytes = 1 << 20
	s.setForwarding(true)
	clip := &brbMedia{
		video: mediaTrack{header: videoConfig(), frames: []*rtmp.Message{keyframe(0)}, duration: time.Second},
		audio: mediaTrack{header: audioConfig(), frames: []*rtmp.Message{packet(rtmp.Audio, 0, 0xaf, 1, 0)}, duration: time.Second},
	}
	b := newBroadcast(s, clip)
	s.broadcast = b
	sub := b.hub.subscribe()
	return s, b, sub
}

func TestBRBManualAndAutomaticNeverExpire(t *testing.T) {
	s, b, sub := testBRB(t)
	now := time.Now()
	b.tick(now)
	if !b.status().Active {
		t.Fatal("missing source did not start BRB")
	}
	b.tick(now.Add(24 * time.Hour))
	if !b.status().Active {
		t.Fatal("BRB expired")
	}
	// A returning source cannot override manual BRB.
	b.setManual(true)
	b.ingest(videoConfig(), now)
	b.ingest(audioConfig(), now)
	b.ingest(packet(rtmp.Audio, 0, 0xaf, 1, 0), now)
	b.ingest(keyframe(0), now)
	if !b.status().Manual || !b.status().Active {
		t.Fatal("manual BRB was overridden")
	}
	b.setManual(false)
	if !b.status().Active {
		t.Fatal("resumed without a fresh keyframe")
	}
	b.ingest(keyframe(time.Second), now)
	if b.status().Active {
		t.Fatal("did not return to OBS")
	}
	b.inputLost()
	b.tick(now.Add(25 * time.Hour))
	if !b.status().Active {
		t.Fatal("disconnect failed to protect stream")
	}
	b.setManual(true)
	b.setManual(false)
	if !b.status().Active {
		t.Fatal("disabling manual BRB stopped protection")
	}
	s.setForwarding(false)
	b.tick(now.Add(26 * time.Hour))
	if b.status().Active {
		t.Fatal("master off did not stop BRB")
	}
	b.hub.unsubscribe(sub)
}

func TestBRBTransitionsPreserveOutputTimeline(t *testing.T) {
	_, b, sub := testBRB(t)
	now := time.Now()
	b.tick(now)
	b.ingest(videoConfig(), now)
	b.ingest(audioConfig(), now)
	b.ingest(packet(rtmp.Audio, 0, 0xaf, 1, 0), now)
	b.ingest(keyframe(12*time.Second), now)
	b.ingest(packet(rtmp.Audio, 12010*time.Millisecond, 0xaf, 1, 0), now)
	b.inputLost()
	b.tick(now.Add(time.Second))
	b.ingest(videoConfig(), now.Add(time.Second))
	b.ingest(audioConfig(), now.Add(time.Second))
	b.ingest(packet(rtmp.Audio, 0, 0xaf, 1, 0), now.Add(time.Second))
	b.ingest(keyframe(0), now.Add(time.Second))
	previous := map[uint8]time.Duration{}
	count := 0
	for len(sub.packets) > 0 {
		m := <-sub.packets
		b.hub.consumed(sub, m)
		if m.Type != rtmp.Video && m.Type != rtmp.Audio || len(m.Body) < 2 || m.Body[1] != 1 {
			continue
		}
		if m.Timestamp < previous[m.Type] {
			t.Fatalf("timestamp moved backwards: %v < %v", m.Timestamp, previous[m.Type])
		}
		previous[m.Type] = m.Timestamp
		count++
	}
	if count < 5 {
		t.Fatalf("too few packets across transitions: %d", count)
	}
}

func TestMasterOffRequiresExplicitConfirmation(t *testing.T) {
	s := dashboardServer(t)
	startLiveControlTest(t, s)
	for _, confirmed := range []any{nil, false} {
		w := stageRequest(t, s, "stop_now", map[string]any{"confirmed": confirmed})
		if w.Code != 409 || !s.forwarding.Load() {
			t.Fatalf("unconfirmed shutdown: %d", w.Code)
		}
	}
	w := stageRequest(t, s, "stop_now", nil)
	if w.Code != 200 || s.forwarding.Load() {
		t.Fatalf("confirmed stop failed: %d", w.Code)
	}
}

func TestBRBRepeatedManualOffDoesNotInterruptLive(t *testing.T) {
	_, b, _ := testBRB(t)
	now := time.Now()
	b.ingest(videoConfig(), now)
	b.ingest(audioConfig(), now)
	b.ingest(packet(rtmp.Audio, 0, 0xaf, 1, 0), now)
	b.ingest(keyframe(0), now)
	b.setManual(false)
	b.tick(now)
	if b.status().Active {
		t.Fatal("idempotent manual off interrupted live input")
	}
}
