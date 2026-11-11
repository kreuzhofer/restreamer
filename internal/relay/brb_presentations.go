package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

// Capture an already encoded BRB visual, then use the same durable preparation
// queue and exact-revision selection as prestream/ending. Fallback stays separate.
func (s *Server) brbPresentationHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var input struct {
		Theme        *mediaauthor.ThemeRef   `json:"theme,omitempty"`
		Text         *string                 `json:"text,omitempty"`
		Base         string                  `json:"base_generation"`
		Name         string                  `json:"name"`
		DurationMode string                  `json:"duration_mode"`
		Soundtrack   *mediaauthor.Soundtrack `json:"soundtrack"`
	}
	if !generatorDecode(w, r, &input) {
		return
	}
	if !s.mediaMu.TryLock() {
		http.Error(w, "Wait for BRB preparation before capturing its visual", 409)
		return
	}
	defer s.mediaMu.Unlock()
	s.broadcast.mu.Lock()
	media := s.broadcast.media
	s.broadcast.mu.Unlock()
	if media == nil || input.Base != media.settings.Generation {
		http.Error(w, "BRB changed; refresh and review its visual again", 409)
		return
	}
	settings := media.settings
	if (input.Theme == nil) != (input.Text == nil) {
		http.Error(w, "Choose both a theme and a message", 422)
		return
	}
	if input.Text != nil {
		normalized, err := normalizeBRBText(*input.Text)
		if err != nil || strings.TrimSpace(normalized) == "" {
			http.Error(w, "Choose a valid BRB message", 422)
			return
		}
		settings.Text = normalized
		settings.Music, settings.CustomImage = false, false
	}
	if !validAssetName(input.Name) || (input.DurationMode != "fixed" && input.DurationMode != "music") {
		http.Error(w, "Choose a name and a fixed or music-driven duration", 422)
		return
	}
	d := mediaauthor.Design{Name: input.Name, Stage: "prestream", Theme: mediaauthor.ThemeRef{ID: "retro", Revision: 1}, DurationMode: input.DurationMode, Soundtrack: input.Soundtrack, Scenes: []mediaauthor.Scene{{ID: "brb", Layout: "title", DurationSeconds: float64(len(media.video.frames)) / float64(settings.Profile.FPS)}}}
	if settings.Theme != nil {
		d.Theme = mediaauthor.ThemeRef{ID: settings.Theme.ID, Revision: settings.Theme.Revision}
	}
	if !designBounds(d) {
		http.Error(w, "Invalid music selection", 422)
		return
	}
	g := s.generator
	g.mu.Lock()
	defer g.mu.Unlock()
	if input.Theme != nil {
		theme, err := g.resolveTheme(*input.Theme)
		if err != nil {
			http.Error(w, "Choose an available theme", 422)
			return
		}
		settings.Theme = &theme
		d.Theme = *input.Theme
		d.Scenes[0].Text = settings.Text
		d.Scenes[0].DurationSeconds = float64(themedBRBSeconds(theme.Style))
	}
	issues := append(g.assetIssues(d), g.musicIssues(d, settings.Profile.FPS, settings.Profile.SampleRate)...)
	if len(issues) > 0 {
		generatorJSON(w, 422, map[string]any{"issues": issues})
		return
	}
	j, err := newGenerationJob(d, settings.Profile)
	if err != nil {
		http.Error(w, "Cannot create preparation identity", 503)
		return
	}
	j.Kind = "brb"
	j.Design.Stage = "brb"
	j.BRBFrames = len(media.video.frames)
	if input.Theme != nil {
		j.BRBFrames = themedBRBSeconds(settings.Theme.Style) * settings.Profile.FPS
		j.BRBDraft = &settings
	}
	identity := []byte(input.Base)
	if input.Theme != nil {
		identity = []byte("brb-design")
	}
	sum := sha256.Sum256(identity)
	j.Design.ID = hex.EncodeToString(sum[:16])
	j.Design.Version = 1
	if settings.Theme != nil {
		theme := *settings.Theme
		j.ThemeSnapshot = &theme
	}
	if err := g.captureMusicPresentation(&j); err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	if input.Theme != nil {
		captured, _ := json.Marshal(struct {
			Text  string
			Theme *mediaauthor.Theme
		}{settings.Text, settings.Theme})
		digest := sha256.Sum256(captured)
		j.BRBDigest = hex.EncodeToString(digest[:])
		g.jobsMu.Lock()
		defer g.jobsMu.Unlock()
		g.admitJob(w, j)
		return
	}
	captureRoot := filepath.Join(filepath.Dir(g.root), "captures")
	if err := os.MkdirAll(captureRoot, 0700); err != nil {
		http.Error(w, "Cannot capture BRB visual", 507)
		return
	}
	capture := g.brbCapturePath(j.ID)
	if err := copyBRBPresentationCapture(filepath.Join(s.cfg.BRB.Directory, input.Base, "video.flv"), capture); err != nil {
		http.Error(w, "Cannot retain BRB visual; check storage", 507)
		return
	}
	admitted := false
	defer func() {
		if !admitted {
			os.Remove(capture)
		}
	}()
	j.BRBDigest, err = mediaDigest(capture)
	if err != nil {
		http.Error(w, "Cannot verify BRB visual", 507)
		return
	}
	g.jobsMu.Lock()
	defer g.jobsMu.Unlock()
	g.admitJob(w, j)
	admitted = g.jobs[j.ID] != nil
}
func (g *generatorStore) brbCapturePath(id string) string {
	return filepath.Join(filepath.Dir(g.root), "captures", id+".flv")
}
func jobDesignBounds(j GenerationJob) bool {
	d := j.Design
	if j.Kind == "brb" {
		if d.Stage != "brb" || !validRevisionID(j.BRBDigest) || j.BRBFrames < 1 || j.BRBFrames > 600*j.Profile.FPS {
			return false
		}
		if j.BRBDraft != nil {
			if j.BRBDraft.Theme == nil || j.BRBDraft.CustomImage || j.BRBDraft.Music || j.BRBDraft.Profile != j.Profile {
				return false
			}
			if _, err := normalizeBRBText(j.BRBDraft.Text); err != nil {
				return false
			}
		}
		d.Stage = "prestream"
	} else if j.Kind != "" {
		return false
	}
	return designBounds(d)
}
func (s *Server) retainBRBVisual(ctx context.Context, j *GenerationJob) (string, error) {
	source := s.generator.brbCapturePath(j.ID)

	if j.BRBDraft != nil {
		dir, err := os.MkdirTemp(s.cfg.BRB.Directory, "assets-")
		if err != nil {
			return "", errors.New("Cannot create draft BRB workspace")
		}
		defer os.RemoveAll(dir)
		if _, err = s.encodeBRBSettings(ctx, *j.BRBDraft, dir); err != nil {
			return "", err
		}
		source = filepath.Join(dir, "video.flv")
	} else {
		digest, err := mediaDigest(source)
		if err != nil || digest != j.BRBDigest {
			return "", errors.New("Captured BRB visual is missing or changed")
		}
	}
	dir := filepath.Join(s.generator.workRoot, j.ID)
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", errors.New("Cannot create BRB workspace")
	}
	defer os.RemoveAll(dir)
	output := filepath.Join(dir, "visual.flv")
	duration := float64(j.BRBFrames) / float64(j.Profile.FPS)
	args := append(generatorBaseArgs(), "-protocol_whitelist", "file,pipe", "-i", source, "-f", "lavfi", "-i", fmt.Sprintf("anullsrc=r=%d:cl=stereo", j.Profile.SampleRate), "-map", "0:v:0", "-map", "1:a:0", "-c:v", "copy", "-c:a", "aac", "-t", decimal(duration), "-threads", "2", "-f", "flv", output)
	if exec.CommandContext(ctx, "ffmpeg", args...).Run() != nil {
		return "", errors.New("Cannot prepare BRB visual with silent source audio")
	}
	idx, err := indexClipContext(ctx, output)
	if err != nil {
		return "", err
	}
	revision, err := s.library.prepareRetainedRevision(&LibraryEntry{ID: "generator-" + j.ID, Name: j.Design.Name, Profile: j.Profile, Duration: duration, path: output, index: idx}, GeneratedTiming{Version: 1, Frames: j.BRBFrames})
	if err != nil {
		return "", err
	}
	s.library.mu.Lock()
	s.library.revisions[revision.ID] = revision
	s.library.mu.Unlock()
	return revision.ID, nil
}

func copyBRBPresentationCapture(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	stat, err := in.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > maxMediaBytes {
		return errors.New("Invalid BRB capture")
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		out.Close()
		if !complete {
			os.Remove(destination)
		}
	}()
	n, err := io.Copy(out, io.LimitReader(in, maxMediaBytes+1))
	if err != nil || n != stat.Size() {
		return errors.New("Incomplete BRB capture")
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}

func (g *generatorStore) recoverBRBCaptures() error {
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(g.root), "captures"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("Cannot inspect BRB captures")
	}
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".flv")
		if strings.HasSuffix(entry.Name(), ".flv") && validDesignID(id) && g.jobs[id] == nil {
			if err := os.Remove(g.brbCapturePath(id)); err != nil {
				return errors.New("Cannot clean unsubmitted BRB capture")
			}
		}
	}
	return nil
}
