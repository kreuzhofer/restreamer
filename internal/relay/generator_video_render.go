package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func generatorBaseArgs() []string {
	return []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-max_alloc", "268435456", "-filter_threads", "1", "-filter_complex_threads", "1"}
}
func videoEncodingArgs(p config.BRBProfile) []string {
	return []string{"-c:v", "libx264", "-preset", "veryfast", "-profile:v", "high", "-bf", "0", "-g", strconv.Itoa(p.FPS), "-keyint_min", strconv.Itoa(p.FPS), "-sc_threshold", "0", "-threads", "2", "-video_track_timescale", strconv.Itoa(p.FPS * 1000)}
}
func sampleBoundary(frames, sampleRate, fps int) int64 {
	return int64(math.Round(float64(frames) * float64(sampleRate) / float64(fps)))
}

// The caller already holds preparation admission. No helper reacquires it.
func (s *Server) renderVideoSegment(ctx context.Context, scene mediaauthor.Scene, p config.BRBProfile, dst, dir string, budget int64, progress func(float64), style mediaauthor.Style, inputs mediaauthor.RenderInputs) error {
	if animatedArtwork(style) {
		return s.renderArtworkVideoSegment(ctx, scene, p, dst, dir, budget, progress, style, inputs)
	}
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
	backdrop := filepath.Join(dir, "video-backdrop.png")
	if err := saveGeneratorRaster(backdrop, mediaauthor.ThemeBackdrop(p.Width, p.Height, style, inputs)); err != nil {
		return err
	}
	defer os.Remove(backdrop)
	target := dst
	if frames > rangeFrames || style.Effect != "none" {
		target = filepath.Join(dir, "video-range.mp4")
		defer os.Remove(target)
	}
	args := generatorBaseArgs()
	args = append(args, "-ss", decimal(start))
	args = append(args, videoInputArgs(source.Name())...)
	args = append(args, "-loop", "1", "-framerate", strconv.Itoa(p.FPS), "-i", backdrop)
	// Allow at most one replicated terminal frame solely for source/output frame
	// quantization. The selected range is trimmed before padding or repetition.
	filter := fmt.Sprintf("[0:v]trim=duration=%s,setpts=PTS-STARTPTS,fps=%d,scale=%d:%d,setsar=1,format=yuv420p,tpad=stop_mode=clone:stop_duration=%s,trim=end_frame=%d[content];[1:v][content]overlay=%d:%d:shortest=1:format=rgb[video]", decimal(end-start), p.FPS, width, height, decimal(1/float64(p.FPS)), normalizedFrames, region.Min.X+(region.Dx()-width)/2, region.Min.Y+(region.Dy()-height)/2)
	if inputs.Logo != nil {
		foreground := filepath.Join(dir, "video-foreground.png")
		if err := saveGeneratorRaster(foreground, mediaauthor.ThemeForeground(p.Width, p.Height, style, inputs)); err != nil {
			return err
		}
		defer os.Remove(foreground)
		args = append(args, "-loop", "1", "-framerate", strconv.Itoa(p.FPS), "-i", foreground)
		filter += ";[video][2:v]overlay=0:0:format=rgb,format=yuv420p[branded]"
	} else {
		filter += ";[video]format=yuv420p[branded]"
	}
	args = append(args, "-filter_complex", filter, "-map", "[branded]", "-an", "-frames:v", strconv.Itoa(normalizedFrames))
	args = append(args, videoEncodingArgs(p)...)
	args = append(args, "-fs", strconv.FormatInt(budget, 10), "-progress", "pipe:1", target)
	if err := runGeneratorFFmpeg(ctx, args, progress); err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil || info.Size() >= budget {
		return errors.New("Video normalization reached the 512 MiB workspace limit. Shorten the design.")
	}
	if err := validateVideoFrameCount(ctx, target, normalizedFrames); err != nil {
		return err
	}
	if frames > rangeFrames || style.Effect != "none" {
		if budget-info.Size() < 1 {
			return errors.New("Video repetition exceeds the workspace limit.")
		}
		args = generatorBaseArgs()
		if frames > rangeFrames {
			args = append(args, "-stream_loop", "-1")
		}
		args = append(args, "-threads", "2", "-i", target)
		if style.Effect != "none" {
			sprite := filepath.Join(dir, "effect.png")
			start, y, step, slots := mediaauthor.EffectGeometry(p.Width, p.Height)
			filter := fmt.Sprintf("[0:v][1:v]overlay=x='%d+mod(floor(t*%d),%d)*%d':y=%d:format=yuv420,setsar=1,format=yuv420p[v]", start, style.EffectSpeed, slots, step, y)
			args = append(args, "-threads", "2", "-loop", "1", "-framerate", strconv.Itoa(p.FPS), "-i", sprite, "-filter_complex", filter, "-map", "[v]")
			args = append(args, videoEncodingArgs(p)...)
		} else {
			args = append(args, "-map", "0:v:0", "-c:v", "copy", "-video_track_timescale", strconv.Itoa(p.FPS*1000))
		}
		args = append(args, "-an", "-frames:v", strconv.Itoa(frames), "-fs", strconv.FormatInt(budget-info.Size(), 10), "-progress", "pipe:1", dst)
		if err := runGeneratorFFmpeg(ctx, args, progress); err != nil {
			return err
		}
		out, err := os.Stat(dst)
		if err != nil || out.Size()+info.Size() >= budget {
			return errors.New("Video repetition reached the workspace limit. Shorten the design.")
		}
	}

	return videoSourceUnchanged(source, meta)
}

type silentPCM struct{}

func (silentPCM) Read(p []byte) (int, error) { clear(p); return len(p), nil }

// appendScenePCM uses cumulative sample boundaries, including each repetition,
// so 44.1 kHz/24 fps scenes cannot accumulate half-sample rounding errors.
func (s *Server) appendScenePCM(ctx context.Context, scene mediaauthor.Scene, p config.BRBProfile, priorFrames, frames int, out *os.File, dir string, budget int64) error {
	samples := sampleBoundary(priorFrames+frames, p.SampleRate, p.FPS) - sampleBoundary(priorFrames, p.SampleRate, p.FPS)
	if !scene.IsVideo() || scene.Video == nil || !scene.Video.AudioEnabled || scene.Video.Volume() == 0 {
		_, err := io.CopyN(out, silentPCM{}, samples*4)
		return err
	}
	source, meta, err := s.openVideoAsset(scene)
	if err != nil {
		return err
	}
	defer source.Close()
	if !meta.HasAudio {
		return errors.New("The selected video has no audio. Disable source audio.")
	}
	start, end := videoRange(scene, meta)
	rangeFrames := int(math.Round((end - start) * float64(p.FPS)))
	if rangeFrames < 1 {
		return errors.New("The selected video audio range is empty.")
	}
	rangeSamples := int64(math.Ceil(float64(min(frames, rangeFrames)) * float64(p.SampleRate) / float64(p.FPS)))
	if rangeSamples*4 >= budget {
		return errors.New("Video audio reached the 512 MiB workspace limit. Shorten the design.")
	}
	path := filepath.Join(dir, "video-range.pcm")
	defer os.Remove(path)
	args := generatorBaseArgs()
	args = append(args, "-ss", decimal(start))
	args = append(args, videoInputArgs(source.Name())...)
	args = append(args, "-vn", "-map", "0:a:0", "-af", fmt.Sprintf("atrim=duration=%s,asetpts=PTS-STARTPTS,aresample=%d:async=1:first_pts=0,volume=%s,apad,atrim=end_sample=%d", decimal(end-start), p.SampleRate, decimal(scene.Video.Volume()/100), rangeSamples), "-ac", "2", "-ar", strconv.Itoa(p.SampleRate), "-c:a", "pcm_s16le", "-threads", "2", "-fs", strconv.FormatInt(rangeSamples*4+1, 10), "-f", "s16le", "-progress", "pipe:1", path)
	if err := runGeneratorFFmpeg(ctx, args, func(float64) {}); err != nil {
		return err
	}
	audio, err := os.Open(path)
	if err != nil {
		return errors.New("Cannot read normalized source audio.")
	}
	defer audio.Close()
	info, err := audio.Stat()
	if err != nil || info.Size() != rangeSamples*4 {
		return errors.New("Normalized source audio is incomplete.")
	}
	for done := 0; done < frames; {
		n := min(rangeFrames, frames-done)
		count := sampleBoundary(priorFrames+done+n, p.SampleRate, p.FPS) - sampleBoundary(priorFrames+done, p.SampleRate, p.FPS)
		if _, err := io.CopyN(out, io.NewSectionReader(audio, 0, count*4), count*4); err != nil {
			return errors.New("Cannot assemble source audio; check storage.")
		}
		done += n
	}
	return videoSourceUnchanged(source, meta)
}

func validateVideoFrameCount(ctx context.Context, path string, expected int) error {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-threads", "2", "-select_streams", "v:0", "-count_frames", "-show_entries", "stream=nb_read_frames", "-of", "json", path)
	var data boundedProbe
	cmd.Stdout = &data
	if cmd.Run() != nil {
		return errors.New("Cannot validate the selected video range.")
	}
	var decoded struct {
		Streams []struct {
			Frames string `json:"nb_read_frames"`
		} `json:"streams"`
	}
	if json.Unmarshal(data.data, &decoded) != nil || len(decoded.Streams) != 1 {
		return errors.New("Normalized video is incomplete.")
	}
	count, err := strconv.Atoi(decoded.Streams[0].Frames)
	if err != nil || count != expected {
		return errors.New("Selected video range did not produce all expected frames; adjust the range or replace the source.")
	}
	return nil
}
func videoSourceUnchanged(f *os.File, meta AssetRevision) error {
	opened, err := f.Stat()
	if err != nil {
		return errors.New("Cannot verify captured video.")
	}
	current, err := os.Stat(f.Name())
	if err != nil || !os.SameFile(opened, current) {
		return errors.New("Captured video changed during preparation.")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return verifyVideoRevision(f, meta)
}
