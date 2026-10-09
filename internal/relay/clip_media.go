package relay

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

const maxClipBytes int64 = 32 << 30
const maxClipPacket = 4 << 20
const maxClipDuration = 12 * time.Hour

type clipKey struct {
	Position time.Duration
	Offset   int64
}
type clipIndex struct {
	ExactFPS             int
	Frames               int
	LastVideo, LastAudio time.Duration
	Exact                bool
	Video, Audio         *rtmp.Message
	Keys                 []clipKey
	Duration, Base       time.Duration
	Size                 int64
}
type clipReader struct {
	file   *os.File
	index  *clipIndex
	offset int64
}

func clipTag(f *os.File, offset, size int64) (uint8, time.Duration, int, int64, error) {
	if offset == size {
		return 0, 0, 0, 0, io.EOF
	}
	var h [11]byte
	if offset < 13 || offset+15 > size {
		return 0, 0, 0, 0, errors.New("truncated prepared video")
	}
	if _, err := f.ReadAt(h[:], offset); err != nil {
		return 0, 0, 0, 0, err
	}
	n := int(h[1])<<16 | int(h[2])<<8 | int(h[3])
	next := offset + 15 + int64(n)
	if n < 2 || n > maxClipPacket || next > size {
		return 0, 0, 0, 0, errors.New("invalid prepared video packet")
	}
	var tail [4]byte
	if _, err := f.ReadAt(tail[:], next-4); err != nil || binary.BigEndian.Uint32(tail[:]) != uint32(n+11) {
		return 0, 0, 0, 0, errors.New("invalid prepared video tag")
	}
	ts := uint32(h[7])<<24 | uint32(h[4])<<16 | uint32(h[5])<<8 | uint32(h[6])
	return h[0], time.Duration(ts) * time.Millisecond, n, next, nil
}

// Index only headers/keyframe offsets, not encoded media bodies. Playback reads
// one bounded packet at a time, independent of the length of the uploaded file.
func indexClip(path string) (*clipIndex, error) { return indexClipContext(context.Background(), path) }
func indexClipContext(ctx context.Context, path string) (*clipIndex, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxClipBytes {
		return nil, errors.New("prepared video exceeds 32 GiB")
	}
	var header [13]byte
	if _, err := io.ReadFull(f, header[:]); err != nil || string(header[:3]) != "FLV" || binary.BigEndian.Uint32(header[5:9]) != 9 {
		return nil, errors.New("invalid prepared video header")
	}
	idx := &clipIndex{Size: info.Size()}
	var last [2]time.Duration
	for offset := int64(13); offset < idx.Size; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		typ, ts, n, next, err := clipTag(f, offset, idx.Size)
		if err != nil {
			return nil, err
		}
		if typ == rtmp.Video || typ == rtmp.Audio {
			var prefix [2]byte
			if _, err = f.ReadAt(prefix[:], offset+11); err != nil {
				return nil, err
			}
			if prefix[1] == 0 {
				body := make([]byte, n)
				if _, err = f.ReadAt(body, offset+11); err != nil {
					return nil, err
				}
				msg := &rtmp.Message{Type: typ, Body: body}
				if typ == rtmp.Video {
					idx.Video = msg
				} else {
					idx.Audio = msg
				}
			} else if prefix[1] == 1 {
				track := 0
				if typ == rtmp.Audio {
					track = 1
				}
				if ts < last[track] || ts > maxClipDuration+time.Second {
					return nil, errors.New("invalid prepared video timing")
				}
				last[track] = ts
				if typ == rtmp.Video {
					idx.Frames++
				}
				if typ == rtmp.Video && prefix[0]>>4 == 1 {
					if len(idx.Keys) == 0 {
						idx.Base = ts
					}
					if len(idx.Keys) > int(maxClipDuration/time.Second)+1 {
						return nil, errors.New("too many prepared seek points")
					}
					idx.Keys = append(idx.Keys, clipKey{ts - idx.Base, offset})
				}
			}
		}
		offset = next
	}
	if idx.Video == nil || idx.Audio == nil || len(idx.Keys) == 0 {
		return nil, errors.New("prepared video needs H.264 video and AAC audio")
	}
	idx.LastVideo, idx.LastAudio = last[0]-idx.Base, last[1]-idx.Base
	idx.Duration = max(last[0], last[1]) - idx.Base + time.Second/25
	if idx.Duration <= 0 {
		return nil, errors.New("prepared video is empty")
	}
	return idx, nil
}

func openClip(path string, idx *clipIndex) (*clipReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || info.Size() != idx.Size {
		f.Close()
		return nil, errors.New("prepared file changed; prepare it again")
	}
	return &clipReader{file: f, index: idx, offset: idx.Keys[0].Offset}, nil
}
func (r *clipReader) close() { r.file.Close() }
func (r *clipReader) seek(position time.Duration) (time.Duration, error) {
	info, err := r.file.Stat()
	if err != nil || info.Size() != r.index.Size {
		return 0, errors.New("prepared file is unavailable or changed")
	}
	if position < 0 || position >= r.index.Duration {
		return 0, errors.New("seek must be within the video")
	}
	key := r.index.Keys[0]
	for _, candidate := range r.index.Keys {
		if candidate.Position > position {
			break
		}
		key = candidate
	}
	r.offset = key.Offset
	return key.Position, nil
}
func (r *clipReader) next() (*rtmp.Message, error) {
	for {
		typ, ts, n, next, err := clipTag(r.file, r.offset, r.index.Size)
		if err != nil {
			if errors.Is(err, io.EOF) && r.offset != r.index.Size {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		offset := r.offset
		r.offset = next
		if typ != rtmp.Video && typ != rtmp.Audio {
			continue
		}
		body := make([]byte, n)
		if _, err = r.file.ReadAt(body, offset+11); err != nil {
			return nil, err
		}
		if body[1] != 1 || ts < r.index.Base {
			continue
		}
		return &rtmp.Message{Type: typ, Timestamp: ts - r.index.Base, Body: body}, nil
	}
}

type boundedProbe struct{ data []byte }

func (b *boundedProbe) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 64<<10 {
		return 0, errors.New("probe output too large")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func prepareClip(ctx context.Context, src, dst string, profile config.BRBProfile, progress func(float64)) error {
	probe := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,mp4,m4a,3gp,3g2,mj2", "-enable_drefs", "0", "-use_absolute_path", "0", "-show_entries", "format=duration:stream=codec_type,width,height", "-of", "json", src)
	var output boundedProbe
	probe.Stdout = &output
	if probe.Run() != nil {
		return errors.New("Cannot read MP4; check the file and FFprobe installation")
	}
	var info struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Type          string `json:"codec_type"`
			Width, Height int
		} `json:"streams"`
	}
	if json.Unmarshal(output.data, &info) != nil {
		return errors.New("Cannot read MP4 properties")
	}
	duration, err := strconv.ParseFloat(info.Format.Duration, 64)
	if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration < 0.1 || duration > maxClipDuration.Seconds() {
		return errors.New("MP4 duration must be between 0.1 seconds and 12 hours")
	}
	video, audio := false, false
	for _, stream := range info.Streams {
		if stream.Type == "video" && !video {
			video = true
			if stream.Width < 1 || stream.Height < 1 || stream.Width > 8192 || stream.Height > 8192 {
				return errors.New("Source resolution exceeds 8192 × 8192")
			}
		}
		if stream.Type == "audio" {
			audio = true
		}
	}
	if !video {
		return errors.New("MP4 has no video track")
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-max_alloc", "268435456", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,mp4,m4a,3gp,3g2,mj2", "-enable_drefs", "0", "-use_absolute_path", "0", "-i", src}
	audioMap := "0:a:0"
	if !audio {
		args = append(args, "-f", "lavfi", "-i", fmt.Sprintf("anullsrc=r=%d:cl=stereo", profile.SampleRate))
		audioMap = "1:a:0"
	}
	vf := fmt.Sprintf("setpts=PTS-STARTPTS,fps=%d,scale=%d:%d:force_original_aspect_ratio=decrease:force_divisible_by=2,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,format=yuv420p", profile.FPS, profile.Width, profile.Height, profile.Width, profile.Height)
	args = append(args, "-map", "0:v:0", "-map", audioMap, "-t", strconv.FormatFloat(duration, 'f', 3, 64), "-vf", vf, "-af", "asetpts=PTS-STARTPTS,aresample=async=1:first_pts=0,apad", "-c:v", "libx264", "-preset", "veryfast", "-profile:v", "high", "-bf", "0", "-g", fmt.Sprint(profile.FPS), "-keyint_min", fmt.Sprint(profile.FPS), "-sc_threshold", "0", "-threads", "2", "-c:a", "aac", "-ac", "2", "-ar", fmt.Sprint(profile.SampleRate), "-b:a", "128k", "-max_muxing_queue_size", "1024", "-fs", fmt.Sprint(maxClipBytes), "-progress", "pipe:1", "-f", "flv", dst)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return errors.New("Cannot start video preparation")
	}
	if cmd.Start() != nil {
		return errors.New("Cannot start FFmpeg")
	}
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		if value, ok := strings.CutPrefix(scanner.Text(), "out_time_us="); ok {
			if n, err := strconv.ParseFloat(value, 64); err == nil {
				progress(min(99, max(0, n/1e6/duration*100)))
			}
		}
	}
	if err := cmd.Wait(); err != nil || scanner.Err() != nil {
		return errors.New("Video preparation failed; check MP4, available disk space and FFmpeg")
	}
	idx, err := indexClipContext(ctx, dst)
	if err != nil {
		return errors.New("Prepared video is invalid or incomplete")
	}
	if math.Abs(idx.Duration.Seconds()-duration) > 1 {
		return errors.New("Video preparation was truncated; check disk space and the 32 GiB prepared-file limit")
	}
	return nil
}
