package relay

import (
	"bufio"
	"context"
	"encoding/json"
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

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func (s *Server) renderGenerator(ctx context.Context, j GenerationJob, progress func(int)) (string, error) {
	dir := filepath.Join(s.generator.workRoot, j.ID)
	if os.Mkdir(dir, 0700) != nil {
		return "", errors.New("Cannot create render workspace; check storage.")
	}
	defer os.RemoveAll(dir)
	p := j.Profile
	theme := mediaauthor.RetroTheme(1)
	if j.ThemeSnapshot != nil {
		theme = *j.ThemeSnapshot
	}
	inputs, err := s.loadThemeInputs(theme, p.Width, p.Height)
	if err != nil {
		return "", err
	}
	sprite := filepath.Join(dir, "effect.png")
	if theme.Style.Effect != "none" {
		if err := saveGeneratorRaster(sprite, mediaauthor.EffectSprite(p.Height, theme.Style)); err != nil {
			return "", err
		}
	}
	output := filepath.Join(dir, "prepared.flv")
	// Normalize one scene at a time. The concat demuxer opens only the
	// current segment; a large design never creates a many-input filter graph.
	var concat strings.Builder
	pcmPath := filepath.Join(dir, "sequence.pcm")
	pcm, err := os.Create(pcmPath)
	if err != nil {
		return "", errors.New("Cannot create scene audio workspace.")
	}
	defer pcm.Close()
	// Reserve the complete continuous PCM stream before accepting scene segments.
	segmentBytes := sampleBoundary(int(math.Round(j.Duration*float64(p.FPS))), p.SampleRate, p.FPS) * 4
	completedFrames := 0
	totalFrames := int(math.Round(j.Duration * float64(p.FPS)))
	for i, scene := range j.Design.Scenes {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		frames := int(math.Round(scene.DurationSeconds * float64(p.FPS)))
		if err := s.appendScenePCM(ctx, scene, p, completedFrames, frames, pcm, dir, maxGeneratorOutputBytes-segmentBytes); err != nil {
			return "", err
		}
		still := filepath.Join(dir, "scene.png")
		name := fmt.Sprintf("scene-%03d.mp4", i)
		segment := filepath.Join(dir, name)
		sceneProgress := func(seconds float64) {
			progress(int(min(90, max(0, (float64(completedFrames)+seconds*float64(p.FPS))/float64(totalFrames)*90))))
		}
		if scene.IsVideo() {
			if err := s.renderVideoSegment(ctx, scene, p, segment, dir, maxGeneratorOutputBytes-segmentBytes, sceneProgress, theme.Style, inputs); err != nil {
				return "", err
			}
		} else {
			if err := s.writeSceneRaster(scene, p.Width, p.Height, still, theme.Style, inputs); err != nil {
				return "", err
			}
			args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-max_alloc", "268435456", "-filter_threads", "1", "-filter_complex_threads", "1",
				"-protocol_whitelist", "file,pipe", "-threads", "2", "-loop", "1", "-framerate", strconv.Itoa(p.FPS), "-i", still,
			}
			if theme.Style.Effect == "none" {
				args = append(args, "-map", "0:v:0", "-vf", "setsar=1,format=yuv420p")
			} else {
				start, y, step, slots := mediaauthor.EffectGeometry(p.Width, p.Height)
				filter := fmt.Sprintf("[0:v][1:v]overlay=x='%d+mod(floor(t*%d),%d)*%d':y=%d:format=auto,setsar=1,format=yuv420p[v]", start, theme.Style.EffectSpeed, slots, step, y)
				args = append(args, "-threads", "2", "-loop", "1", "-framerate", strconv.Itoa(p.FPS), "-i", sprite, "-filter_complex", filter, "-map", "[v]")
			}
			args = append(args, "-an", "-frames:v", strconv.Itoa(frames), "-c:v", "libx264", "-preset", "veryfast", "-tune", "stillimage", "-profile:v", "high", "-bf", "0", "-g", strconv.Itoa(p.FPS), "-keyint_min", strconv.Itoa(p.FPS), "-sc_threshold", "0", "-threads", "2",
				"-video_track_timescale", strconv.Itoa(p.FPS*1000), "-fs", strconv.FormatInt(maxGeneratorOutputBytes-segmentBytes, 10), "-progress", "pipe:1", segment)
			if err := runGeneratorFFmpeg(ctx, args, func(seconds float64) {
				progress(int(min(90, max(0, (float64(completedFrames)+seconds*float64(p.FPS))/float64(totalFrames)*90))))
			}); err != nil {
				return "", err
			}
		}
		info, err := os.Stat(segment)
		if err != nil || info.Size() <= 0 {
			return "", errors.New("Cannot read normalized scene; check storage.")
		}
		segmentBytes += info.Size()
		if segmentBytes >= maxGeneratorOutputBytes {
			return "", errors.New("Normalized scenes reached their combined 512 MiB limit. Shorten the design.")
		}
		fmt.Fprintf(&concat, "file '%s'\n", name)
		completedFrames += frames
	}
	if err := pcm.Close(); err != nil {
		return "", errors.New("Cannot save continuous scene audio.")
	}
	os.Remove(filepath.Join(dir, "scene.png"))
	manifest := filepath.Join(dir, "sequence.txt")
	if os.WriteFile(manifest, []byte(concat.String()), 0600) != nil {
		return "", errors.New("Cannot save the scene sequence; check storage.")
	}
	// Audio is encoded once for the full sequence, avoiding per-scene AAC
	// priming gaps. Scene intermediates plus final output are bounded to 1 GiB.
	seconds := strconv.FormatFloat(j.Duration, 'f', 9, 64)
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-max_alloc", "268435456", "-filter_threads", "1", "-filter_complex_threads", "1",
		"-protocol_whitelist", "file,pipe", "-f", "concat", "-safe", "1", "-i", manifest, "-f", "s16le", "-ar", strconv.Itoa(p.SampleRate), "-ac", "2", "-i", pcmPath,
		"-map", "0:v:0", "-map", "1:a:0", "-t", seconds, "-c:v", "copy", "-c:a", "aac", "-ac", "2", "-ar", strconv.Itoa(p.SampleRate), "-b:a", "128k", "-threads", "2", "-max_muxing_queue_size", "1024", "-fs", strconv.Itoa(maxGeneratorOutputBytes), "-progress", "pipe:1", "-f", "flv", output}
	if err := runGeneratorFFmpeg(ctx, args, func(seconds float64) { progress(90 + int(min(5, max(0, seconds/j.Duration*5)))) }); err != nil {
		return "", err
	}
	info, err := os.Stat(output)
	if err != nil || info.Size() <= 0 || info.Size() >= maxGeneratorOutputBytes {
		return "", errors.New("Generated output reached its 512 MiB limit or is unavailable. Shorten the design.")
	}
	// Decode/count every video frame before accepting output. FFmpeg can exit
	// successfully when -fs truncates a file; process success alone is insufficient.
	probe := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-threads", "2", "-select_streams", "v:0", "-count_frames", "-show_entries", "stream=width,height,nb_read_frames", "-of", "json", output)
	var data boundedProbe
	probe.Stdout = &data
	if probe.Run() != nil {
		return "", errors.New("Cannot validate generated output; check FFprobe installation.")
	}
	var decoded struct {
		Streams []struct {
			Width, Height int
			Frames        string `json:"nb_read_frames"`
		} `json:"streams"`
	}
	if json.Unmarshal(data.data, &decoded) != nil || len(decoded.Streams) != 1 {
		return "", errors.New("Generated output has no valid video track.")
	}
	frames, err := strconv.Atoi(decoded.Streams[0].Frames)
	if err != nil || frames != int(math.Round(j.Duration*float64(p.FPS))) || decoded.Streams[0].Width != p.Width || decoded.Streams[0].Height != p.Height {
		return "", errors.New("Generated video is incomplete or does not match the captured profile.")
	}
	idx, err := indexClipContext(ctx, output)
	if err != nil || math.Abs(idx.Duration.Seconds()-j.Duration) > 0.1 {
		return "", errors.New("Generated audio/video timing is incomplete.")
	}
	progress(98)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	entry := &LibraryEntry{ID: "generator-" + j.ID, Name: j.Design.Name, Profile: p, Duration: j.Duration, index: idx, path: output}
	revision, err := s.library.prepareRetainedRevision(entry)
	if err != nil {
		return "", errors.New("Cannot retain the generated media revision; check storage.")
	}
	s.library.mu.Lock()
	defer s.library.mu.Unlock()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	s.library.revisions[revision.ID] = revision
	if s.library.profile != p {
		return revision.ID, errors.New("The active streaming profile changed during generation. The completed candidate retains its original profile; generate again for the current profile.")
	}
	return revision.ID, nil
}

func (s *Server) writeSceneRaster(scene mediaauthor.Scene, width, height int, path string, style mediaauthor.Style, inputs mediaauthor.RenderInputs) error {
	asset, err := s.loadSceneImage(scene)
	if err != nil {
		return err
	}
	inputs.Image = asset
	img, err := mediaauthor.RenderSceneStyled(scene, width, height, style, inputs, -1, 0)
	if err != nil {
		return errors.New("A captured scene cannot be rendered; check its typography and layout.")
	}
	return saveGeneratorRaster(path, img)
}
func saveGeneratorRaster(path string, img image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return errors.New("Cannot save scene raster; check storage.")
	}
	err = png.Encode(file, img)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return errors.New("Cannot save scene raster; check storage.")
	}
	return nil
}

func runGeneratorFFmpeg(ctx context.Context, args []string, progress func(float64)) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return errors.New("Cannot start generation.")
	}
	if cmd.Start() != nil {
		return errors.New("Cannot start FFmpeg; check the server installation.")
	}
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		if value, ok := strings.CutPrefix(scanner.Text(), "out_time_us="); ok {
			if n, err := strconv.ParseFloat(value, 64); err == nil {
				progress(n / 1e6)
			}
		}
	}
	if err := cmd.Wait(); err != nil || scanner.Err() != nil {
		return errors.New("Generation failed; check available storage and the FFmpeg installation.")
	}
	return ctx.Err()
}
