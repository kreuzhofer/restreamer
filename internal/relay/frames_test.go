package relay

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func TestVideoFrameClassification(t *testing.T) {
	for _, tc := range []struct {
		m    *rtmp.Message
		want bool
	}{
		{videoConfig(), false}, {audioConfig(), false}, {packet(rtmp.Audio, 0, 0xaf, 1, 1), false},
		{packet(rtmp.Video, 0, 0x17, 2, 0, 0, 0), false}, {packet(rtmp.Video, 0, 0x17, 1), false},
		{packet(rtmp.Video, 0, 0x17, 1, 0, 0, 0), false}, {packet(rtmp.Data, 0, 1, 2, 3), false},
		{keyframe(time.Second), true}, {packet(rtmp.Video, 0, 0x27, 1, 0, 0, 0, 1), true},
	} {
		if isVideoFrame(tc.m) != tc.want {
			t.Fatalf("wrong frame classification: %+v", tc.m)
		}
	}
}

func TestFrameAccountingQueueFailureAndIntentionalSkips(t *testing.T) {
	o := dashboardServer(t).outputs[0]
	o.blocked = false // These unit tests exercise an open master gate.
	h := newHub(32, o)
	sub := h.subscribeOutput(o)
	publish := func(m *rtmp.Message) {
		t.Helper()
		if err := h.publish(m); err != nil {
			t.Fatal(err)
		}
	}
	publish(videoConfig())
	publish(packet(rtmp.Video, 0, 0x27, 1, 0, 0, 0, 1)) // Await initial keyframe.
	publish(keyframe(time.Second))
	for range 2 {
		m := <-sub.packets
		h.consumed(sub, m)
		o.recordSent(m)
	} // Config + keyframe.
	publish(keyframe(2 * time.Second))
	publish(keyframe(3 * time.Second)) // 24 bytes queued.
	publish(keyframe(4 * time.Second)) // Overflow: one frame fails admission.
	publish(keyframe(5 * time.Second)) // Broken subscription: one more frame omitted.
	if o.snapshot().DroppedFrames != 2 {
		t.Fatalf("wrong immediate drops: %+v", o.snapshot())
	}
	h.finish(sub, false) // Two buffered frames discarded exactly once.
	if got := o.snapshot(); got.Frames != 1 || got.DroppedFrames != 4 || got.SkippedFrames != 1 {
		t.Fatalf("wrong queue accounting: %+v", got)
	}
	publish(keyframe(6 * time.Second)) // No subscription after failure.
	if o.snapshot().DroppedFrames != 5 {
		t.Fatal("disconnected target frames not counted")
	}
	o.setEnabled(false)
	publish(keyframe(7 * time.Second))
	if got := o.snapshot(); got.DroppedFrames != 5 || got.PausedFrames != 1 {
		t.Fatalf("pause counted as a failure: %+v", got)
	}
	o.setEnabled(true)
	sub = h.subscribeOutput(o)
	publish(packet(rtmp.Video, 8*time.Second, 0x27, 1, 0, 0, 0, 1))
	publish(keyframe(9 * time.Second))
	h.finish(sub, true) // Publisher ends; pending frame is intentionally skipped.
	if got := o.snapshot(); got.DroppedFrames != 5 || got.SkippedFrames != 3 {
		t.Fatalf("resync/shutdown counted as drops: %+v", got)
	}
}

func TestFrameAccountingInFlightFailureAndPause(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(map[bool]string{false: "network failure", true: "manual pause"}[paused], func(t *testing.T) {
			o := dashboardServer(t).outputs[0]
			o.blocked = false // These unit tests exercise an open master gate.
			h := newHub(1024, o)
			sub := h.subscribeOutput(o)
			_ = h.publish(videoConfig())
			_ = h.publish(keyframe(time.Second))
			_ = h.publish(keyframe(2 * time.Second))
			header := <-sub.packets
			h.consumed(sub, header)
			o.recordSent(header)
			frame := <-sub.packets
			h.consumed(sub, frame) // In-flight frame no longer belongs to queue.
			if paused {
				o.setEnabled(false)
			}
			o.discardFrames(1, paused)
			h.finish(sub, paused)
			got := o.snapshot()
			if paused {
				if got.PausedFrames != 2 || got.DroppedFrames != 0 {
					t.Fatalf("bad pause: %+v", got)
				}
			} else if got.DroppedFrames != 2 {
				t.Fatalf("in-flight frame double counted or lost: %+v", got)
			}
			if got.Frames != 0 {
				t.Fatal("codec header counted as a sent video frame")
			}
		})
	}
}

func TestFPSHistoryAndAPICompatibility(t *testing.T) {
	s := dashboardServer(t)
	start := time.Now().Add(-2 * time.Second)
	s.sample(start)
	s.inputFrames.Add(120)
	s.outputs[0].recordSent(videoConfig())
	for range 90 {
		s.outputs[0].recordSent(keyframe(0))
	}
	s.sample(start.Add(2 * time.Second))
	history := s.history(start.Add(2 * time.Second))
	if len(history) != 1 || history[0].InputFPS != 60 || history[0].OutputFPS["one"] != 45 {
		t.Fatalf("incorrect FPS: %+v", history)
	}
	s.sample(start.Add(3 * time.Second))
	history = s.history(start.Add(3 * time.Second))
	if history[1].InputFPS != 0 || history[1].OutputFPS["one"] != 0 {
		t.Fatal("idle FPS is not zero")
	}
	w := dashboardRequest(s, "GET", "/api/dashboard", "")
	var status struct {
		InputFrames uint64 `json:"input_frames"`
		History     []struct {
			Input     *float64           `json:"input"`
			Outputs   map[string]float64 `json:"outputs"`
			InputFPS  *float64           `json:"input_fps"`
			OutputFPS map[string]float64 `json:"output_fps"`
		} `json:"history"`
		Outputs []OutputStatus `json:"outputs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.InputFrames != 120 || status.Outputs[0].Frames != 90 || len(status.History) != 2 || status.History[0].Input == nil || status.History[0].Outputs == nil || status.History[0].InputFPS == nil || status.History[0].OutputFPS == nil {
		t.Fatalf("missing new fields or incompatible bitrate API: %s", w.Body.String())
	}
	for i := 4; i <= 1004; i++ {
		s.sample(start.Add(time.Duration(i) * time.Second))
	}
	history = s.history(start.Add(1004 * time.Second))
	if len(history) != 900 || history[0].InputFPS != 0 || history[0].Time <= start.Add(104*time.Second).UnixMilli() {
		t.Fatal("FPS history did not expire with bitrate history")
	}
}

func TestFrameCountersLivePauseResumeAndReconnect(t *testing.T) {
	a, b := newSink(t), newSink(t)
	s, address := startRelay(t, []config.Target{a.target("one"), b.target("two")})
	c := publishInput(t, address)
	eventually(t, func() bool {
		return s.outputs[0].snapshot().State == "waiting_for_keyframe" && s.outputs[1].snapshot().State == "waiting_for_keyframe"
	})
	writePacket(t, c, videoConfig())
	writePacket(t, c, audioConfig())
	writePacket(t, c, keyframe(time.Second))
	for _, sink := range []*sink{a, b} {
		for range 3 {
			receive(t, sink)
		}
	}
	eventually(t, func() bool {
		return s.inputFrames.Load() == 1 && s.outputs[0].snapshot().Frames == 1 && s.outputs[1].snapshot().Frames == 1
	})
	if err := s.setTarget("one", false); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "disabled" })
	writePacket(t, c, keyframe(2*time.Second))
	receive(t, b)
	eventually(t, func() bool { return s.outputs[0].snapshot().PausedFrames == 1 })
	if s.outputs[0].snapshot().DroppedFrames != 0 {
		t.Fatal("pause inflated drops")
	}
	if err := s.setTarget("one", true); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "waiting_for_keyframe" })
	writePacket(t, c, packet(rtmp.Video, 3*time.Second, 0x27, 1, 0, 0, 0, 1))
	receive(t, b)
	writePacket(t, c, keyframe(4*time.Second))
	receive(t, b)
	for range 3 {
		receive(t, a)
	}
	eventually(t, func() bool { return s.outputs[0].snapshot().Frames == 2 })
	if got := s.outputs[0].snapshot(); got.SkippedFrames != 1 || got.DroppedFrames != 0 {
		t.Fatalf("bad resync counts: %+v", got)
	}
	a.disconnect()
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "retrying" && s.outputs[0].snapshot().RetryAt > 0 })
	writePacket(t, c, keyframe(5*time.Second))
	receive(t, b)
	eventually(t, func() bool { return s.outputs[0].snapshot().DroppedFrames == 1 })
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "waiting_for_keyframe" })
	writePacket(t, c, keyframe(6*time.Second))
	receive(t, b)
	for range 3 {
		receive(t, a)
	}
	eventually(t, func() bool {
		return s.outputs[0].snapshot().Frames == 3 && s.outputs[1].snapshot().Frames == 6 && s.inputFrames.Load() == 6
	})
	if got := s.outputs[0].snapshot(); got.Frames+got.PausedFrames+got.SkippedFrames+got.DroppedFrames != 6 {
		t.Fatalf("frames lost or double counted: %+v", got)
	}
	if got := s.outputs[1].snapshot(); got.DroppedFrames != 0 || got.SkippedFrames != 0 || got.PausedFrames != 0 {
		t.Fatalf("healthy target affected: %+v", got)
	}
	c.Net.Close()
	eventually(t, func() bool { return !s.active.Load() })
	// New OBS session: counters remain cumulative, old errors don't turn startup into drops.
	c = publishInput(t, address)
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "waiting_for_keyframe" })
	writePacket(t, c, videoConfig())
	writePacket(t, c, keyframe(0))
	for range 2 {
		receive(t, a)
	}
	eventually(t, func() bool { return s.outputs[0].snapshot().Frames == 4 && s.inputFrames.Load() == 7 })
	if s.outputs[0].snapshot().DroppedFrames != 1 {
		t.Fatal("new publisher inherited old outage")
	}
}
