package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func TestPacmanMazePreparesFullLoopWithoutReplacingActiveRevision(t *testing.T) {
	s := arcadeThemeServer(t)
	themeResponse := dashboardRequest(s, "GET", "/api/generator/themes/pacman-maze/revisions/1", "")
	if themeResponse.Code != 200 {
		t.Fatal(themeResponse.Code, themeResponse.Body.String())
	}
	var theme mediaauthor.Theme
	if err := json.Unmarshal(themeResponse.Body.Bytes(), &theme); err != nil {
		t.Fatal(err)
	}
	if !theme.Builtin || theme.Revision != 1 {
		t.Fatal(theme)
	}
	if w := dashboardRequest(s, "PUT", "/api/generator/themes/pacman-maze", themeResponse.Body.String()); w.Code != 409 {
		t.Fatal("builtin was mutable", w.Code)
	}
	if w := assetRequest(t, s, map[string]string{"text": "BACK TO MAKING"}, "", nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	original := brbHTTPSettings(t, s)
	prepared := dashboardRequest(s, "POST", "/api/brb/theme/prepare", fmt.Sprintf(`{"theme":{"id":"pacman-maze","revision":1},"base_generation":%q}`, original.Generation))
	if prepared.Code != 201 {
		t.Fatal(prepared.Code, prepared.Body.String())
	}
	var candidate brbThemeCandidate
	if err := json.Unmarshal(prepared.Body.Bytes(), &candidate); err != nil {
		t.Fatal(err)
	}
	if candidate.VideoDuration != 40 || candidate.Settings.Text != "BACK TO MAKING" {
		t.Fatal(candidate)
	}
	if brbHTTPSettings(t, s).Generation != original.Generation {
		t.Fatal("preparing changed the active BRB")
	}
	preview := dashboardRequest(s, "GET", "/api/brb/theme/candidate/preview", "")
	if preview.Code != 200 {
		t.Fatal(preview.Code, preview.Body.String())
	}
	path := filepath.Join(t.TempDir(), "pacman.mp4")
	if err := os.WriteFile(path, preview.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("ffprobe", "-v", "error", "-count_frames", "-select_streams", "v:0", "-show_entries", "stream=nb_read_frames", "-of", "csv=p=0", path).Output()
	if err != nil || string(bytes.TrimSpace(output)) != "1000" {
		t.Fatalf("40-second 25 fps loop was truncated: %q %v", output, err)
	}
	// Compare the approved exit positions against actual encoded frames, so a
	// renderer that silently trims/restarts before rebirth cannot pass.
	if delta := scenePixelDifference(workshopDecodedFrame(t, path, 25*28), workshopDecodedFrame(t, path, 25*31)); delta < .05 {
		t.Fatal("ghost regeneration/exit motion is absent", delta)
	}
	if output, err := exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	activated := dashboardRequest(s, "POST", "/api/brb/theme/activate", fmt.Sprintf(`{"id":%q,"base_generation":%q}`, candidate.ID, candidate.Base))
	if activated.Code != 204 {
		t.Fatal(activated.Code, activated.Body.String())
	}
	video := filepath.Join(s.cfg.BRB.Directory, candidate.ID, "video.flv")
	before, err := os.ReadFile(video)
	if err != nil {
		t.Fatal(err)
	}
	restarted := New(s.cfg, s.log)
	if err := restarted.initialize(); err != nil {
		t.Fatal(err)
	}
	restored := brbHTTPSettings(t, restarted)
	if restored.Generation != candidate.ID || restored.Theme.ID != "pacman-maze" || restored.Text != "BACK TO MAKING" {
		t.Fatal("restart lost exact generation", restored)
	}
	after, err := os.ReadFile(video)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("restart regenerated retained tracks", err)
	}
	if w := dashboardRequest(s, "GET", "/api/generator/themes/retro/revisions/1", ""); w.Code != 200 {
		t.Fatal("legacy theme removed", w.Code)
	}
}

func TestPacmanMotionRejectsOtherArtwork(t *testing.T) {
	style := mediaauthor.RetroTheme(1).Style
	style.Effect = "pacman-motion"
	if len(mediaauthor.ValidateStyle(style)) == 0 {
		t.Fatal("mismatched motion accepted")
	}
}
