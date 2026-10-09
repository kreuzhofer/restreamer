package relay

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	image, err := mediaauthor.RenderScene(j.Design.Scenes[0], j.Profile.Width, j.Profile.Height)
	if err != nil {
		return "", errors.New("The captured scene cannot be rendered; check its typography.")
	}
	still := filepath.Join(dir, "scene.png")
	file, err := os.Create(still)
	if err != nil {
		return "", errors.New("Cannot save scene raster; check storage.")
	}
	err = png.Encode(file, image)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return "", errors.New("Cannot save scene raster; check storage.")
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	output := filepath.Join(dir, "prepared.flv")
	p := j.Profile
	seconds := strconv.FormatFloat(j.Duration, 'f', 9, 64)
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-max_alloc", "268435456", "-filter_threads", "1", "-filter_complex_threads", "1",
		"-protocol_whitelist", "file,pipe", "-threads", "2", "-loop", "1", "-framerate", strconv.Itoa(p.FPS), "-i", still,
		"-f", "lavfi", "-i", fmt.Sprintf("anullsrc=r=%d:cl=stereo", p.SampleRate),
		"-map", "0:v:0", "-map", "1:a:0", "-t", seconds, "-vf", "setsar=1,format=yuv420p", "-c:v", "libx264", "-preset", "veryfast", "-tune", "stillimage", "-profile:v", "high", "-bf", "0", "-g", strconv.Itoa(p.FPS), "-keyint_min", strconv.Itoa(p.FPS), "-sc_threshold", "0", "-threads", "2",
		"-c:a", "aac", "-ac", "2", "-ar", strconv.Itoa(p.SampleRate), "-b:a", "128k", "-max_muxing_queue_size", "1024", "-fs", strconv.Itoa(maxGeneratorOutputBytes), "-progress", "pipe:1", "-f", "flv", output}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", errors.New("Cannot start generation.")
	}
	if cmd.Start() != nil {
		return "", errors.New("Cannot start FFmpeg; check the server installation.")
	}
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		if value, ok := strings.CutPrefix(scanner.Text(), "out_time_us="); ok {
			if n, err := strconv.ParseFloat(value, 64); err == nil {
				progress(int(min(95, max(0, n/1e6/j.Duration*95))))
			}
		}
	}
	if err := cmd.Wait(); err != nil || scanner.Err() != nil {
		return "", errors.New("Generation failed; check available storage and the FFmpeg installation.")
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
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
