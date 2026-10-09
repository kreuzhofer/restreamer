package relay

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func TestTruncatedClipHoldsFallbackUntilExplicitRetry(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, action := range []string{"prestream", "play_clip"} {
		if w := stageRequest(t, s, action, map[string]any{"mode": "preview_only", "revision": revision}); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	path := filepath.Join(s.library.root, "revisions", revision+".flv")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.broadcast.tick(now)
	failed := readStage(t, s)
	if failed.Stage != "CLIP" || failed.Source != "brb" || failed.Media.Revision != revision || failed.Error == "" || failed.Playback.State != "failed" {
		t.Fatal("Truncation did not hold an actionable failure", failed)
	}
	s.broadcast.ingest(videoConfig(), now)
	s.broadcast.ingest(audioConfig(), now)
	s.broadcast.ingest(packet(rtmp.Audio, 0, 0xaf, 1, 1), now)
	s.broadcast.ingest(keyframe(0), now)
	if state := readStage(t, s); state.Source != "brb" || state.Error == "" {
		t.Fatal("OBS overrode failed clip", state)
	}
	if w := stageRequest(t, s, "retry", nil); w.Code != 409 {
		t.Fatal("Unavailable revision retried", w.Code, w.Body.String())
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if state := readStage(t, s); state.Source != "brb" || state.Error == "" {
		t.Fatal("Restoring file automatically recovered", state)
	}
	if w := stageRequest(t, s, "retry", map[string]any{"confirmed": false}); w.Code != 409 {
		t.Fatal("Retry did not require confirmation", w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "retry", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "CLIP" || state.Source != "file" || state.Media.Revision != revision || state.Error != "" || state.ReturnStage != "PRESTREAM" {
		t.Fatal("Retry lost original clip or return intent", state)
	}
}
