package relay

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func TestPreviewOnlyLiveServesBroadcastMediaWithoutDestinationConnections(t *testing.T) {
	destination := newSink(t)
	s, address := startRelayWithLogger(t, []config.Target{destination.target("one")}, slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	if w := stageRequest(t, s, "go_live", map[string]any{"mode": "preview_only"}); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	publisher := publishInput(t, address)
	writePacket(t, publisher, previewVideoConfig())
	writePacket(t, publisher, audioConfig())
	writePacket(t, publisher, packet(rtmp.Audio, 0, 0xaf, 1, 1))
	writePacket(t, publisher, keyframe(0))
	eventually(t, func() bool { return readStage(t, s).Stage == "LIVE" })
	for _, enabled := range []bool{false, true} {
		if w := stageRequest(t, s, "set_target", map[string]any{"target": "one", "enabled": enabled}); w.Code != 200 {
			t.Fatal("rehearsal target preference", w.Code, w.Body.String())
		}
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Feed real RTMP while HTTP subscribes; the preview must wait for a fresh
	// keyframe and return actual encoded media, not just an isolated status flag.
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		var timestamp time.Duration
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				timestamp += 20 * time.Millisecond
				frame := keyframe(timestamp)
				frame.MessageStreamID, frame.ChunkStreamID = rtmp.StreamID, 6
				if publisher.Write(frame) != nil {
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-sent }()
	r, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/broadcast-preview", nil)
	r.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	response, err := ts.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("X-Preview-Codecs") == "" {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("rehearsal broadcast preview: %d %s", response.StatusCode, body)
	}
	data := make([]byte, 2048)
	n, err := io.ReadFull(response.Body, data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data[:n], []byte("ftyp")) || !bytes.Contains(data[:n], []byte("moof")) {
		t.Fatal("rehearsal preview did not contain initialization and media fragments")
	}
	if destination.count() != 0 || s.outputs[0].snapshot().Attempts != 0 || s.forwarding.Load() {
		t.Fatal("preview-only LIVE opened a destination session")
	}
	if w := stageRequest(t, s, "go_live", map[string]any{"mode": "real"}); w.Code != 409 {
		t.Fatal("real start during rehearsal was not rejected", w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "GET", "/api/broadcast-preview", ""); w.Code != 503 {
		t.Fatal("OFF broadcast preview did not stop", w.Code)
	}
}

func TestRehearsalLocksSharedProfileUntilStopped(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := assetRequest(t, s, map[string]string{"fps": "30"}, "", nil); w.Code != 409 {
		t.Fatalf("profile edit during rehearsal: %d %s", w.Code, w.Body.String())
	}
	state := readStage(t, s)
	if state.Stage != "PRESTREAM" || state.Mode != "preview_only" || state.Media.Revision != revision {
		t.Fatal("rejected profile edit disturbed rehearsal", state)
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := assetRequest(t, s, map[string]string{"fps": "30"}, "", nil); w.Code != 204 {
		t.Fatalf("profile edit after stopping rehearsal: %d %s", w.Code, w.Body.String())
	}
}

func TestSharedControllerWithoutBRBRejectsBRBMediaRequests(t *testing.T) {
	s := dashboardServer(t)
	readStage(t, s)
	if w := dashboardRequest(s, "GET", "/api/brb/image", ""); w.Code != 409 {
		t.Fatal("unconfigured BRB image", w.Code, w.Body.String())
	}
	if w := assetRequest(t, s, map[string]string{"text": "BE RIGHT BACK"}, "", nil); w.Code != 409 {
		t.Fatal("unconfigured BRB assets", w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "OFF" {
		t.Fatal("BRB request changed stage", state)
	}
}
