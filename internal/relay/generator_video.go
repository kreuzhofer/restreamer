package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func videoRange(scene mediaauthor.Scene, meta AssetRevision) (float64, float64) {
	end := scene.Video.TrimEndSeconds
	if end == 0 {
		end = meta.DurationSeconds
	}
	return scene.Video.TrimStartSeconds, end
}
func (g *generatorStore) videoIssues(d mediaauthor.Design, fps int) []mediaauthor.Issue {
	var issues []mediaauthor.Issue
	for i, scene := range d.Scenes {
		if !scene.IsVideo() || scene.Video == nil {
			continue
		}
		v := scene.Video
		meta, ok := g.assetRevision(v.Asset)
		if !ok {
			continue
		}
		start, end := videoRange(scene, meta)
		add := func(field, message string) {
			issues = append(issues, mediaauthor.Issue{Field: fmtSceneField(i, field), Message: message})
		}
		if start < 0 || math.IsNaN(start) || math.IsInf(start, 0) || end <= start || end > meta.DurationSeconds+.000001 || math.IsNaN(end) || math.IsInf(end, 0) || math.Round((end-start)*float64(fps)) < 1 {
			add("video.trim", "Choose a source range within the video containing at least one output frame.")
		} else if !v.Repeat && math.Round(scene.DurationSeconds*float64(fps)) > math.Round((end-start)*float64(fps)) {
			add("duration_seconds", "The scene exceeds its selected video range. Shorten it or explicitly enable Repeat selected range.")
		}
		if v.Volume() < 0 || v.Volume() > 100 || math.IsNaN(v.Volume()) || math.IsInf(v.Volume(), 0) {
			add("video.audio_volume_percent", "Video volume must be between 0 and 100%.")
		}
		if v.AudioEnabled && !meta.HasAudio {
			add("video.audio_enabled", "This source has no audio. Disable source audio to generate a silent video scene.")
		}
	}
	return issues
}
func (s *Server) openVideoAsset(scene mediaauthor.Scene) (*os.File, AssetRevision, error) {
	if scene.Video == nil {
		return nil, AssetRevision{}, errors.New("Choose a video revision.")
	}
	g := s.generator
	g.mu.Lock()
	meta, ok := g.assetRevision(scene.Video.Asset)
	f, err := os.Open(g.assetPath(scene.Video.Asset))
	kind := g.assets[scene.Video.Asset.ID].Kind
	g.mu.Unlock()
	if !ok || kind != "video" || err != nil {
		if f != nil {
			f.Close()
		}
		return nil, meta, errors.New("The captured video revision is unavailable.")
	}
	if err := verifyVideoRevision(f, meta); err != nil {
		f.Close()
		return nil, meta, err
	}
	return f, meta, nil
}
func videoInputArgs(path string) []string {
	return []string{"-threads", "2", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,mp4,m4a,3gp,3g2,mj2", "-enable_drefs", "0", "-use_absolute_path", "0", "-i", path}
}
func decimal(n float64) string { return strconv.FormatFloat(n, 'f', 9, 64) }
func (s *Server) loadScenePreview(ctx context.Context, scene mediaauthor.Scene) (image.Image, error) {
	if !scene.IsVideo() {
		return s.loadSceneImage(scene)
	}
	f, _, err := s.openVideoAsset(scene)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	release, err := s.preparation.acquire(ctx)
	if err != nil {
		return nil, errors.New("Video preview is waiting for media preparation. Retry after current work finishes.")
	}
	defer release()
	args := []string{"-v", "error", "-nostdin", "-max_alloc", "268435456", "-ss", decimal(scene.Video.TrimStartSeconds)}
	args = append(args, videoInputArgs(f.Name())...)
	args = append(args, "-frames:v", "1", "-an", "-threads", "2", "-f", "image2pipe", "-c:v", "png", "-")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var data bytes.Buffer
	cmd.Stdout = &assetLimitWriter{Writer: &data, remaining: maxAssetImageBytes}
	if cmd.Run() != nil {
		return nil, errors.New("Cannot decode video preview; check the selected range and FFmpeg.")
	}
	img, err := png.Decode(&data)
	if err != nil {
		return nil, fmt.Errorf("Cannot read video preview.")
	}
	return img, nil
}
