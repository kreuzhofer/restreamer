package relay

import (
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
)

func TestForwardingStartsOffAndIsNotPersisted(t *testing.T) {
	s := dashboardServer(t)
	if s.forwarding.Load() {
		t.Fatal("forwarding started enabled")
	}
	if w := dashboardRequest(s, "PUT", "/api/forwarding", `{"enabled":true}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !s.forwarding.Load() {
		t.Fatal("forwarding not enabled")
	}
	if err := s.setTarget("one", false); err != nil {
		t.Fatal(err)
	}
	if w := dashboardRequest(s, "PUT", "/api/forwarding", `{"enabled":false}`); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if s.outputs[0].snapshot().Enabled {
		t.Fatal("master changed target preference")
	}
	s.setForwarding(true)
	reloaded := New(s.cfg, s.log)
	if err := reloaded.initialize(); err != nil {
		t.Fatal(err)
	}
	if reloaded.forwarding.Load() || reloaded.outputs[0].snapshot().Enabled {
		t.Fatal("wrong restart state")
	}
	var status struct {
		Forwarding bool `json:"forwarding"`
	}
	w := dashboardRequest(s, "GET", "/api/dashboard", "")
	if json.Unmarshal(w.Body.Bytes(), &status) != nil || !status.Forwarding {
		t.Fatal("missing master status")
	}
	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":true,"other":1}`, `{"enabled":true}{}`} {
		if w := dashboardRequest(s, "PUT", "/api/forwarding", body); w.Code != 400 {
			t.Fatal("invalid control accepted")
		}
	}
}

func TestRapidMasterChangesAndPublisherReconnect(t *testing.T) {
	dest := newSink(t)
	s, address := startRelay(t, []config.Target{dest.target("one")})
	c := publishInput(t, address)
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "waiting_for_keyframe" })
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); s.setForwarding(i%2 == 0) }(i)
	}
	wg.Wait()
	s.setForwarding(false)
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "paused" })
	c.Net.Close()
	eventually(t, func() bool { return !s.active.Load() })
	connections := dest.count()
	c = publishInput(t, address)
	writePacket(t, c, videoConfig())
	writePacket(t, c, keyframe(time.Second))
	eventually(t, func() bool { return s.inputFrames.Load() > 0 })
	if dest.count() != connections || !s.outputs[0].snapshot().Enabled || s.forwarding.Load() {
		t.Fatal("reconnect bypassed master or changed target")
	}
	s.setForwarding(true)
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "waiting_for_keyframe" })
	writePacket(t, c, keyframe(2*time.Second))
	receive(t, dest)
	receive(t, dest)
}

func TestMasterPauseResumeKeepsInputAndTargetPreferences(t *testing.T) {
	a, b := newSink(t), newSink(t)
	off := b.target("two")
	off.Enabled = "false"
	s, address := startRelayWithLogger(t, []config.Target{a.target("one"), off}, slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	c := publishInput(t, address)
	writePacket(t, c, videoConfig())
	writePacket(t, c, keyframe(time.Second))
	eventually(t, func() bool { return s.inputFrames.Load() == 1 })
	if a.count() != 0 || b.count() != 0 {
		t.Fatal("connected while master off")
	}
	if s.outputs[0].snapshot().PausedFrames != 1 {
		t.Fatal("master pause counted as drop")
	}
	s.setForwarding(true)
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "waiting_for_keyframe" })
	writePacket(t, c, keyframe(2*time.Second))
	receive(t, a)
	receive(t, a)
	s.setForwarding(false)
	eventually(t, func() bool { return s.outputs[0].snapshot().State == "paused" })
	before := s.outputs[0].snapshot()
	writePacket(t, c, keyframe(3*time.Second))
	eventually(t, func() bool { return s.inputFrames.Load() == 3 })
	if !s.active.Load() || !before.Enabled || s.outputs[1].snapshot().Enabled || b.count() != 0 {
		t.Fatal("master changed preferences or input")
	}
	if after := s.outputs[0].snapshot(); after.Bytes != before.Bytes || after.DroppedFrames != 0 {
		t.Fatal("paused target sent or dropped media")
	}
	// Changing an individual preference while paused must not connect it.
	if err := s.setTarget("two", true); err != nil {
		t.Fatal(err)
	}
	writePacket(t, c, keyframe(4*time.Second))
	eventually(t, func() bool { return s.inputFrames.Load() == 4 })
	if b.count() != 0 {
		t.Fatal("target bypassed master")
	}
	s.setForwarding(true)
	eventually(t, func() bool {
		return s.outputs[0].snapshot().State == "waiting_for_keyframe" && s.outputs[1].snapshot().State == "waiting_for_keyframe"
	})
	writePacket(t, c, keyframe(5*time.Second))
	for _, dest := range []*sink{a, b} {
		receive(t, dest)
		receive(t, dest)
	}
}
