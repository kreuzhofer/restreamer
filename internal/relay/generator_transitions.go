package relay

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

// Each scene is decoded at most for its body and two neighboring overlaps.
// Encoded pieces are concatenated, never an increasingly long rendered prefix.
func (s *Server) assembleTransitions(ctx context.Context, plan mediaauthor.SequenceTiming, p config.BRBProfile, dir string, used int64, progress func(int)) (string, int64, error) {
	pcm, err := os.Create(filepath.Join(dir, "sequence.pcm"))
	if err != nil {
		return "", used, errors.New("Cannot create transition audio workspace; check storage.")
	}
	defer pcm.Close()
	var manifest strings.Builder
	for i, timing := range plan.Scenes {
		if err := ctx.Err(); err != nil {
			return "", used, err
		}
		source := filepath.Join(dir, fmt.Sprintf("scene-%03d.mp4", i))
		audio, err := os.Open(filepath.Join(dir, fmt.Sprintf("scene-%03d.pcm", i)))
		if err != nil {
			return "", used, errors.New("Cannot open normalized transition audio; check storage.")
		}
		render := func(name string, args []string, frames int) error {
			budget := int64(maxGeneratorOutputBytes) - used
			if budget <= 0 {
				return errors.New("Transitions reached the combined 512 MiB workspace limit. Shorten the design.")
			}
			target := filepath.Join(dir, name)
			args = append(args, "-an", "-frames:v", strconv.Itoa(frames), "-pix_fmt", "yuv420p")
			args = append(args, videoEncodingArgs(p)...)
			args = append(args, "-fs", strconv.FormatInt(budget, 10), "-progress", "pipe:1", target)
			if err := runGeneratorFFmpeg(ctx, args, func(float64) {}); err != nil {
				return err
			}
			info, err := os.Stat(target)
			if err != nil || info.Size() <= 0 || info.Size() >= budget {
				return errors.New("Transitions reached the combined 512 MiB workspace limit. Shorten the design.")
			}
			if err := validateVideoFrameCount(ctx, target, frames); err != nil {
				return errors.New("A transition segment is incomplete. Shorten the design or check storage.")
			}
			used += info.Size()
			fmt.Fprintf(&manifest, "file '%s'\nduration %.9f\n", name, float64(frames)/float64(p.FPS))
			return nil
		}
		bodyFrames := timing.Frames - timing.Incoming - timing.Outgoing
		if bodyFrames > 0 {
			args := generatorBaseArgs()
			args = append(args, "-protocol_whitelist", "file,pipe", "-threads", "2", "-i", source, "-vf", fmt.Sprintf("trim=start_frame=%d:end_frame=%d,settb=1/%d,setpts=N", timing.Incoming, timing.Frames-timing.Outgoing, p.FPS))
			err = render(fmt.Sprintf("body-%03d.mp4", i), args, bodyFrames)
			if err == nil {
				start := sampleBoundary(timing.Start+timing.Incoming, p.SampleRate, p.FPS)
				end := sampleBoundary(timing.Start+timing.Frames-timing.Outgoing, p.SampleRate, p.FPS)
				offset := start - sampleBoundary(timing.Start, p.SampleRate, p.FPS)
				err = copyTransitionPCM(ctx, pcm, io.NewSectionReader(audio, offset*4, (end-start)*4), (end-start)*4)
			}
		}
		if err == nil && timing.Outgoing > 0 {
			next := filepath.Join(dir, fmt.Sprintf("scene-%03d.mp4", i+1))
			args := generatorBaseArgs()
			args = append(args, "-protocol_whitelist", "file,pipe", "-threads", "2", "-i", source, "-protocol_whitelist", "file,pipe", "-threads", "2", "-i", next)
			filter := fmt.Sprintf("[0:v]trim=start_frame=%d:end_frame=%d,settb=1/%d,setpts=N[a];[1:v]trim=end_frame=%d,settb=1/%d,setpts=N[b];[a][b]xfade=transition=fade:duration=%s:offset=0,trim=end_frame=%d,setpts=N/(%d*TB),format=yuv420p[v]", timing.Frames-timing.Outgoing, timing.Frames, p.FPS, timing.Outgoing, p.FPS, decimal(float64(timing.Outgoing)/float64(p.FPS)), timing.Outgoing, p.FPS)
			args = append(args, "-filter_complex", filter, "-map", "[v]")
			err = render(fmt.Sprintf("overlap-%03d.mp4", i), args, timing.Outgoing)
			if err == nil {
				incoming, openErr := os.Open(filepath.Join(dir, fmt.Sprintf("scene-%03d.pcm", i+1)))
				if openErr != nil {
					err = errors.New("Cannot open incoming transition audio; check storage.")
				} else {
					start := sampleBoundary(timing.Start+timing.Frames-timing.Outgoing, p.SampleRate, p.FPS)
					end := sampleBoundary(timing.Start+timing.Frames, p.SampleRate, p.FPS)
					offset := start - sampleBoundary(timing.Start, p.SampleRate, p.FPS)
					err = mixTransitionPCM(ctx, pcm, io.NewSectionReader(audio, offset*4, (end-start)*4), incoming, end-start)
					incoming.Close()
				}
			}
		}
		audio.Close()
		if err != nil {
			return "", used, err
		}
		for _, path := range []string{source, audio.Name()} {
			info, err := os.Stat(path)
			if err != nil {
				return "", used, errors.New("Cannot inspect transition workspace; check storage.")
			}
			if err = os.Remove(path); err != nil {
				return "", used, errors.New("Cannot remove completed transition intermediates; check storage.")
			}
			used -= info.Size()
		}
		progress(60 + (i+1)*28/len(plan.Scenes))
	}
	if err := pcm.Close(); err != nil {
		return "", used, errors.New("Cannot save transition audio; check storage.")
	}
	info, err := os.Stat(pcm.Name())
	if err != nil || info.Size() != sampleBoundary(plan.CompositionFrames, p.SampleRate, p.FPS)*4 {
		return "", used, errors.New("Transition audio is incomplete.")
	}
	return manifest.String(), used, nil
}

func copyTransitionPCM(ctx context.Context, out io.Writer, in io.Reader, n int64) error {
	buffer := make([]byte, 32<<10)
	for n > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		size := int(min(n, int64(len(buffer))))
		if _, err := io.ReadFull(in, buffer[:size]); err != nil {
			return errors.New("Cannot read transition audio; check storage.")
		}
		if _, err := out.Write(buffer[:size]); err != nil {
			return errors.New("Cannot save transition audio; check storage.")
		}
		n -= int64(size)
	}
	return nil
}

// Complementary linear gains mix the actual source audio without clipping or
// per-scene AAC priming. Final background music is applied after this stream.
func mixTransitionPCM(ctx context.Context, out io.Writer, a, b io.Reader, samples int64) error {
	left, right := make([]byte, 32<<10), make([]byte, 32<<10)
	for done := int64(0); done < samples; {
		if err := ctx.Err(); err != nil {
			return err
		}
		count := min(samples-done, int64(len(left)/4))
		size := int(count * 4)
		if _, err := io.ReadFull(a, left[:size]); err != nil {
			return errors.New("Cannot read outgoing transition audio; check storage.")
		}
		if _, err := io.ReadFull(b, right[:size]); err != nil {
			return errors.New("Cannot read incoming transition audio; check storage.")
		}
		for j := int64(0); j < count; j++ {
			weight := float64(done+j) / float64(samples)
			for channel := 0; channel < 2; channel++ {
				at := int(j)*4 + channel*2
				x := float64(int16(binary.LittleEndian.Uint16(left[at:])))
				y := float64(int16(binary.LittleEndian.Uint16(right[at:])))
				mixed := int16(math.Round(x*(1-weight) + y*weight))
				binary.LittleEndian.PutUint16(left[at:], uint16(mixed))
			}
		}
		if _, err := out.Write(left[:size]); err != nil {
			return errors.New("Cannot save mixed transition audio; check storage.")
		}
		done += count
	}
	return nil
}
