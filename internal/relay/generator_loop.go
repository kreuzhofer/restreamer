package relay

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

// Circular composition rotates the full sequence past its overlapped head, so
// every retained pass transitions true tail -> true head without a one-time intro.
func (s *Server) prepareCircularSequence(ctx context.Context, plan mediaauthor.SequenceTiming, p config.BRBProfile, manifest, pcmPath, dir string, used int64) (string, int64, error) {
	budget := int64(maxGeneratorOutputBytes) - used
	if budget <= 0 {
		return "", used, errors.New("Loop composition reached the 512 MiB workspace limit. Shorten the design.")
	}
	video := filepath.Join(dir, "cycle.mp4")
	args := generatorBaseArgs()
	for range 2 {
		args = append(args, "-protocol_whitelist", "file,pipe", "-threads", "2", "-f", "concat", "-safe", "1", "-i", manifest)
	}
	n, b := plan.CompositionFrames, plan.LoopFrames
	filter := fmt.Sprintf("[0:v]settb=1/%d,setpts=N[a];[1:v]settb=1/%d,setpts=N[b];[a][b]xfade=transition=fade:duration=%s:offset=%s,trim=start_frame=%d:end_frame=%d,setpts=N/(%d*TB),format=yuv420p[v]", p.FPS, p.FPS, decimal(float64(b)/float64(p.FPS)), decimal(float64(n-b)/float64(p.FPS)), b, n, p.FPS)
	args = append(args, "-filter_complex", filter, "-map", "[v]", "-an", "-frames:v", strconv.Itoa(plan.Frames))
	args = append(args, videoEncodingArgs(p)...)
	args = append(args, "-fs", strconv.FormatInt(budget, 10), "-progress", "pipe:1", video)
	if err := runGeneratorFFmpeg(ctx, args, func(float64) {}); err != nil {
		return "", used, err
	}
	info, err := os.Stat(video)
	if err != nil || info.Size() <= 0 || info.Size() >= budget {
		return "", used, errors.New("Loop composition reached the 512 MiB workspace limit. Shorten the design.")
	}
	if err := validateVideoFrameCount(ctx, video, plan.Frames); err != nil {
		return "", used, errors.New("The circular video is incomplete; check storage and regenerate.")
	}
	used += info.Size()
	files, err := os.ReadDir(dir)
	if err != nil {
		return "", used, errors.New("Cannot inspect loop workspace.")
	}
	for _, file := range files {
		if !file.Type().IsRegular() || !strings.HasSuffix(file.Name(), ".mp4") || file.Name() == "cycle.mp4" {
			continue
		}
		path := filepath.Join(dir, file.Name())
		info, err := file.Info()
		if err != nil {
			return "", used, errors.New("Cannot inspect completed scene media.")
		}
		if os.Remove(path) != nil {
			return "", used, errors.New("Cannot release completed scene media; check storage.")
		}
		used -= info.Size()
	}
	source, err := os.Open(pcmPath)
	if err != nil {
		return "", used, errors.New("Cannot open complete music mix.")
	}
	defer source.Close()
	inputSamples := sampleBoundary(n, p.SampleRate, p.FPS)
	outputSamples := sampleBoundary(plan.Frames, p.SampleRate, p.FPS)
	info, err = source.Stat()
	if err != nil || info.Size() != inputSamples*4 {
		return "", used, errors.New("Complete music mix timing is invalid.")
	}
	if outputSamples*4 >= int64(maxGeneratorOutputBytes)-used {
		return "", used, errors.New("Circular audio reached the 512 MiB workspace limit. Shorten the design.")
	}
	targetPath := filepath.Join(dir, "cycle.pcm")
	target, err := os.Create(targetPath)
	if err != nil {
		return "", used, errors.New("Cannot create circular audio.")
	}
	defer target.Close()
	start := sampleBoundary(b, p.SampleRate, p.FPS)
	body := sampleBoundary(n-2*b, p.SampleRate, p.FPS)
	outgoing := &cyclicPCMReader{file: source, offset: start * 4, size: inputSamples * 4}
	if err := copyTransitionPCM(ctx, target, outgoing, body*4); err != nil {
		return "", used, err
	}
	incoming := &cyclicPCMReader{file: source, size: inputSamples * 4}
	if err := mixTransitionPCM(ctx, target, outgoing, incoming, outputSamples-body); err != nil {
		return "", used, err
	}
	if target.Close() != nil {
		return "", used, errors.New("Cannot save circular audio.")
	}
	if source.Close() != nil || os.Rename(targetPath, pcmPath) != nil {
		return "", used, errors.New("Cannot retain circular audio.")
	}
	used += (outputSamples - inputSamples) * 4
	result := filepath.Join(dir, "cycle.txt")
	if os.WriteFile(result, []byte("file 'cycle.mp4'\n"), 0600) != nil {
		return "", used, errors.New("Cannot save circular sequence.")
	}
	return result, used, nil
}

// A tiny cycle can be shorter than an AAC warm-up interval. Repeating a bounded
// section reader handles those cycles without allocating their decoded content.
type cyclicPCMReader struct {
	file         io.ReaderAt
	offset, size int64
}

func (r *cyclicPCMReader) Read(p []byte) (int, error) {
	if r.size <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	r.offset %= r.size
	count := int(min(int64(len(p)), r.size-r.offset))
	n, err := r.file.ReadAt(p[:count], r.offset)
	r.offset += int64(n)
	if err == io.EOF && n == count {
		err = nil
	}
	return n, err
}

const cyclicWarmSamples = 4096

// Surround the complete mixed cycle with a bounded cyclic tail/head. Shift PCM
// in place, so warm-up needs only32KiB extra disk, not another full sequence.
func warmCircularPCM(ctx context.Context, path string, samples int64, budget int64) error {
	extra := int64(cyclicWarmSamples * 8)
	if extra >= budget {
		return errors.New("Cyclic audio warm-up reached the workspace limit. Shorten the design.")
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return errors.New("Cannot open cyclic audio workspace.")
	}
	defer f.Close()
	size := samples * 4
	info, err := f.Stat()
	if err != nil || info.Size() != size || size <= 0 {
		return errors.New("Cyclic audio timing is incomplete.")
	}
	pre, post := make([]byte, cyclicWarmSamples*4), make([]byte, cyclicWarmSamples*4)
	tail := &cyclicPCMReader{file: f, offset: ((samples - cyclicWarmSamples%samples) % samples) * 4, size: size}
	head := &cyclicPCMReader{file: f, size: size}
	if _, err := io.ReadFull(tail, pre); err != nil {
		return errors.New("Cannot read cyclic audio tail.")
	}
	if _, err := io.ReadFull(head, post); err != nil {
		return errors.New("Cannot read cyclic audio head.")
	}
	if f.Truncate(size+extra) != nil {
		return errors.New("Cannot reserve cyclic audio warm-up.")
	}
	buffer := make([]byte, 32<<10)
	for end := size; end > 0; {
		if err := ctx.Err(); err != nil {
			return err
		}
		count := min(end, int64(len(buffer)))
		start := end - count
		if _, err := f.ReadAt(buffer[:count], start); err != nil {
			return errors.New("Cannot read cyclic audio.")
		}
		if _, err := f.WriteAt(buffer[:count], start+int64(len(pre))); err != nil {
			return errors.New("Cannot shift cyclic audio.")
		}
		end = start
	}
	if _, err := f.WriteAt(pre, 0); err != nil {
		return errors.New("Cannot save cyclic audio tail.")
	}
	if _, err := f.WriteAt(post, int64(len(pre))+size); err != nil {
		return errors.New("Cannot save cyclic audio head.")
	}
	return f.Close()
}

// Keep the encoded cycle's packets after AAC warm-up and rebase its first video
// to zero. Compact in place: every write ends before the next unread tag, and a
// packet is bounded by maxClipPacket. No duplicate full-size output is created.
func retainCircularFLV(ctx context.Context, path string, duration time.Duration) error {
	idx, err := indexClipContext(ctx, path)
	if err != nil {
		return errors.New("Cannot index cyclic encoded output.")
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return errors.New("Cannot open cyclic encoded output.")
	}
	defer f.Close()
	writeOffset := int64(13)
	for offset := int64(13); offset < idx.Size; {
		if err := ctx.Err(); err != nil {
			return err
		}
		typ, ts, n, next, err := clipTag(f, offset, idx.Size)
		if err != nil {
			return errors.New("Cyclic output contains an incomplete packet.")
		}
		if typ != rtmp.Video && typ != rtmp.Audio {
			offset = next
			continue
		}
		body := make([]byte, n)
		if _, err := f.ReadAt(body, offset+11); err != nil {
			return errors.New("Cannot read cyclic encoded packet.")
		}
		keep := body[1] == 0 || body[1] == 1 && ts >= idx.Base && ts-idx.Base < duration
		if keep {
			stamp := uint32(0)
			if body[1] == 1 {
				stamp = uint32((ts - idx.Base) / time.Millisecond)
			}
			var header [11]byte
			header[0] = typ
			header[1] = byte(n >> 16)
			header[2] = byte(n >> 8)
			header[3] = byte(n)
			header[4] = byte(stamp >> 16)
			header[5] = byte(stamp >> 8)
			header[6] = byte(stamp)
			header[7] = byte(stamp >> 24)
			var previous [4]byte
			binary.BigEndian.PutUint32(previous[:], uint32(n+11))
			for _, part := range [][]byte{header[:], body, previous[:]} {
				if _, err := f.WriteAt(part, writeOffset); err != nil {
					return errors.New("Cannot retain cyclic encoded packet.")
				}
				writeOffset += int64(len(part))
			}
		}
		offset = next
	}
	if f.Truncate(writeOffset) != nil {
		return errors.New("Cannot finish cyclic encoded output.")
	}
	return f.Close()
}
