package relay

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

//go:embed artwork/after-hours/loop.mp4
var afterHoursLoop []byte

//go:embed artwork/after-hours/poster.png
var afterHoursPoster []byte

//go:embed artwork/neon-night/loop.mp4
var neonNightLoop []byte

//go:embed artwork/neon-night/poster.png
var neonNightPoster []byte

type preparedArtwork struct {
	loop, poster []byte
	effect       string
	seconds      int
}

func themeArtwork(id string) (preparedArtwork, bool) {
	switch id {
	case "arcade-after-hours":
		return preparedArtwork{afterHoursLoop, afterHoursPoster, "arcade-palette", 16}, true
	case "neon-night":
		return preparedArtwork{neonNightLoop, neonNightPoster, "neon-palette", 16}, true
	default:
		return preparedArtwork{}, false
	}
}
func animatedArtwork(style mediaauthor.Style) bool {
	art, ok := themeArtwork(style.Artwork)
	return ok && style.Effect == art.effect
}
func themedBRBSeconds(style mediaauthor.Style) int {
	if animatedArtwork(style) {
		art, _ := themeArtwork(style.Artwork)
		return art.seconds
	}
	return 4
}

// Contain the illustration just like static ThemeBackdrop, including non-16:9
// profiles. Colors have already passed six-digit validation before rendering.
func artworkBackdropFilter(p config.BRBProfile, style mediaauthor.Style) string {
	return fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease:flags=area,format=rgb24,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=0x%s,setsar=1", p.Width, p.Height, p.Width, p.Height, strings.TrimPrefix(style.BackgroundColor, "#"))
}

// Preview uses the same prepared master, frame-rate conversion and sizing as encoding.
// The existing preview admission lock bounds concurrent decoder memory.
func artworkPreviewFrame(ctx context.Context, p config.BRBProfile, frame int, style mediaauthor.Style) (image.Image, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	art, ok := themeArtwork(style.Artwork)
	if !ok {
		return nil, errors.New("The selected artwork is unavailable.")
	}
	filter := fmt.Sprintf("fps=%d,select=eq(n\\,%d),%s", p.FPS, frame%(art.seconds*p.FPS), artworkBackdropFilter(p, style))
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-nostdin", "-threads", "2", "-filter_threads", "1", "-i", "pipe:0", "-vf", filter, "-frames:v", "1", "-threads", "2", "-f", "image2pipe", "-c:v", "png", "pipe:1")
	cmd.Stdin = bytes.NewReader(art.loop)
	data, err := cmd.Output()
	if err != nil {
		return nil, errors.New("Cannot decode animated artwork preview; check FFmpeg installation.")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("Cannot read animated artwork preview.")
	}
	return img, nil
}

// artworkOverlayArgs composes transparent content over the immutable artwork loop.
// It only writes into the caller-owned, bounded preparation workspace.
func artworkOverlayArgs(dir, foreground string, p config.BRBProfile, style mediaauthor.Style) ([]string, error) {
	art, ok := themeArtwork(style.Artwork)
	if !ok {
		return nil, errors.New("The selected artwork is unavailable.")
	}
	master := filepath.Join(dir, "artwork-master.mp4")
	if err := os.WriteFile(master, art.loop, 0600); err != nil {
		return nil, errors.New("Cannot save artwork preparation master; check storage.")
	}
	filter := fmt.Sprintf("[1:v]fps=%d,%s[backdrop];[backdrop][0:v]overlay=0:0:format=rgb,setsar=1,format=yuv420p[v]", p.FPS, artworkBackdropFilter(p, style))
	return []string{"-threads", "2", "-loop", "1", "-framerate", strconv.Itoa(p.FPS), "-i", foreground, "-threads", "2", "-stream_loop", "-1", "-i", master, "-filter_complex", filter, "-map", "[v]"}, nil
}

// Normalize only the clip range first, then repeat it over the full background
// cycle. Repeating a one-second inset must not restart the sixteen-second sky.
func (s *Server) renderArtworkVideoSegment(ctx context.Context, scene mediaauthor.Scene, p config.BRBProfile, dst, dir string, budget int64, progress func(float64), style mediaauthor.Style, inputs mediaauthor.RenderInputs) error {
	source, meta, err := s.openVideoAsset(scene)
	if err != nil {
		return err
	}
	defer source.Close()
	start, end := videoRange(scene, meta)
	frames := int(math.Round(scene.DurationSeconds * float64(p.FPS)))
	rangeFrames := int(math.Round((end - start) * float64(p.FPS)))
	if rangeFrames < 1 || frames < 1 || (!scene.Video.Repeat && frames > rangeFrames) {
		return errors.New("The captured video range cannot fill this scene.")
	}
	normalizedFrames := min(frames, rangeFrames)
	region, err := mediaauthor.MediaRegion(scene, p.Width, p.Height)
	if err != nil {
		return err
	}
	scale := math.Min(float64(region.Dx())/float64(meta.Width), float64(region.Dy())/float64(meta.Height))
	width, height := max(2, int(float64(meta.Width)*scale)/2*2), max(2, int(float64(meta.Height)*scale)/2*2)
	normalized := filepath.Join(dir, "artwork-video-range.mp4")
	defer os.Remove(normalized)
	args := append(generatorBaseArgs(), "-ss", decimal(start))
	args = append(args, videoInputArgs(source.Name())...)
	filter := fmt.Sprintf("trim=duration=%s,setpts=PTS-STARTPTS,fps=%d,scale=%d:%d,setsar=1,format=yuv420p,tpad=stop_mode=clone:stop_duration=%s,trim=end_frame=%d", decimal(end-start), p.FPS, width, height, decimal(1/float64(p.FPS)), normalizedFrames)
	args = append(args, "-vf", filter, "-an", "-frames:v", strconv.Itoa(normalizedFrames))
	args = append(args, videoEncodingArgs(p)...)
	args = append(args, "-fs", strconv.FormatInt(budget, 10), "-progress", "pipe:1", normalized)
	if err := runGeneratorFFmpeg(ctx, args, progress); err != nil {
		return err
	}
	if err := validateVideoFrameCount(ctx, normalized, normalizedFrames); err != nil {
		return err
	}
	info, err := os.Stat(normalized)
	if err != nil || info.Size() >= budget {
		return errors.New("Artwork video normalization reached its workspace limit.")
	}
	art, ok := themeArtwork(style.Artwork)
	if !ok {
		return errors.New("The selected artwork is unavailable.")
	}
	master := filepath.Join(dir, "artwork-master.mp4")
	if err := os.WriteFile(master, art.loop, 0600); err != nil {
		return errors.New("Cannot save artwork preparation master.")
	}
	inputs.TransparentBackdrop = true
	border := filepath.Join(dir, "artwork-video-border.png")
	defer os.Remove(border)
	if err := saveGeneratorRaster(border, mediaauthor.ThemeBackdrop(p.Width, p.Height, style, inputs)); err != nil {
		return err
	}
	overlay := mediaauthor.ThemeForeground(p.Width, p.Height, style, inputs)
	foreground := filepath.Join(dir, "artwork-video-foreground.png")
	defer os.Remove(foreground)
	if err := saveGeneratorRaster(foreground, overlay); err != nil {
		return err
	}
	args = append(generatorBaseArgs(), "-threads", "2", "-stream_loop", "-1", "-i", master)
	if frames > rangeFrames {
		args = append(args, "-stream_loop", "-1")
	}
	args = append(args, "-threads", "2", "-i", normalized, "-loop", "1", "-framerate", strconv.Itoa(p.FPS), "-i", border, "-loop", "1", "-framerate", strconv.Itoa(p.FPS), "-i", foreground)
	filter = fmt.Sprintf("[0:v]fps=%d,%s[art];[art][2:v]overlay=0:0:format=rgb[backdrop];[backdrop][1:v]overlay=%d:%d:format=rgb[content];[content][3:v]overlay=0:0:format=rgb,format=yuv420p[v]", p.FPS, artworkBackdropFilter(p, style), region.Min.X+(region.Dx()-width)/2, region.Min.Y+(region.Dy()-height)/2)
	args = append(args, "-filter_complex", filter, "-map", "[v]", "-an", "-frames:v", strconv.Itoa(frames))
	args = append(args, videoEncodingArgs(p)...)
	args = append(args, "-fs", strconv.FormatInt(budget-info.Size(), 10), "-progress", "pipe:1", dst)
	if err := runGeneratorFFmpeg(ctx, args, progress); err != nil {
		return err
	}
	out, err := os.Stat(dst)
	if err != nil || out.Size()+info.Size() >= budget {
		return errors.New("Artwork video composition reached its workspace limit.")
	}
	return videoSourceUnchanged(source, meta)
}
