package relay

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

const maxMediaBytes = 64 << 20

type mediaTrack struct {
	header   *rtmp.Message
	frames   []*rtmp.Message
	duration time.Duration
}
type brbMedia struct {
	video, audio mediaTrack
	settings     brbSettings
}

// readMediaTrack accepts only bounded, locally encoded FLV, never uploaded FLV.
func readMediaTrack(path string, typ uint8, step time.Duration) (mediaTrack, error) {
	var track mediaTrack
	data, err := os.ReadFile(path)
	if err != nil {
		return track, err
	}
	if len(data) > maxMediaBytes || len(data) < 13 || string(data[:3]) != "FLV" {
		return track, errors.New("invalid prepared BRB media")
	}
	pos := int(binary.BigEndian.Uint32(data[5:9])) + 4
	for pos+15 <= len(data) {
		n := int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
		if n > maxMediaBytes || pos+11+n+4 > len(data) {
			return track, errors.New("truncated prepared media")
		}
		ts := uint32(data[pos+7])<<24 | uint32(data[pos+4])<<16 | uint32(data[pos+5])<<8 | uint32(data[pos+6])
		body := data[pos+11 : pos+11+n]
		if data[pos] == typ && len(body) > 1 {
			msg := &rtmp.Message{Type: typ, Timestamp: time.Duration(ts) * time.Millisecond, Body: body}
			if body[1] == 0 {
				track.header = msg
			} else if body[1] == 1 {
				track.frames = append(track.frames, msg)
			}
		}
		pos += 11 + n + 4
	}
	if track.header == nil || len(track.frames) == 0 {
		return track, errors.New("prepared media has no usable track")
	}
	base := track.frames[0].Timestamp
	previous := time.Duration(0)
	for _, m := range track.frames {
		m.Timestamp -= base
		if m.Timestamp < previous {
			return track, errors.New("prepared timestamps are not ordered")
		}
		previous = m.Timestamp
	}
	if typ == rtmp.Video && track.frames[0].Body[0]>>4 != 1 {
		return track, errors.New("prepared video must start with a keyframe")
	}
	track.duration = previous + step
	if typ == rtmp.Audio {
		track.duration = time.Duration(len(track.frames)) * step
	}
	return track, nil
}

func encodeBRB(ctx context.Context, cfg config.BRBProfile, dir string, customImage, music bool, volume int, text string) (*brbMedia, error) {
	videoPath := filepath.Join(dir, "video.flv")
	fps := fmt.Sprint(cfg.FPS)
	vf := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,format=yuv420p", cfg.Width, cfg.Height, cfg.Width, cfg.Height)
	seconds := 2
	videoArgs := []string{"-protocol_whitelist", "file,pipe", "-loop", "1", "-framerate", fps, "-i", filepath.Join(dir, "image.png")}
	filters := []string{"-vf", vf}
	tune := "stillimage"
	if !customImage {
		seconds = 32
		master := filepath.Join(dir, "arcade.mkv")
		if err := os.WriteFile(master, defaultBRBVideo, 0600); err != nil {
			return nil, errors.New("cannot prepare default BRB animation")
		}
		defer os.Remove(master)
		band, err := brbTextImage(text)
		if err != nil {
			return nil, err
		}
		textPath := filepath.Join(dir, "text.png")
		if err := writeBRBPNG(textPath, band); err != nil {
			return nil, errors.New("cannot prepare arcade message")
		}
		defer os.Remove(textPath)
		if err := defaultBRBImage(filepath.Join(dir, "image.png"), text); err != nil {
			return nil, errors.New("cannot prepare arcade preview")
		}
		videoArgs = []string{"-protocol_whitelist", "file,pipe", "-i", master, "-loop", "1", "-i", textPath}
		// Keep pixel edges crisp; the bundled 60 fps master supports every profile.
		vf = "fps=" + fps + "," + fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease:flags=neighbor,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,format=yuv420p", cfg.Width, cfg.Height, cfg.Width, cfg.Height)
		filters = []string{"-filter_complex_threads", "2", "-filter_complex", "[0:v][1:v]overlay=0:70:format=auto," + vf + "[video]", "-map", "[video]"}
		tune = "animation"
	}
	videoArgs = append(videoArgs, filters...)
	videoArgs = append(videoArgs, "-t", fmt.Sprint(seconds), "-an", "-c:v", "libx264", "-preset", "veryfast", "-tune", tune, "-profile:v", "high", "-bf", "0", "-g", fmt.Sprint(cfg.FPS*2), "-threads", "2", "-fs", fmt.Sprint(maxMediaBytes), "-f", "flv", videoPath)
	if err := runBRBFFmpeg(ctx, videoArgs...); err != nil {
		return nil, err
	}
	if err := encodeBRBAudio(ctx, cfg, dir, music, volume); err != nil {
		return nil, err
	}
	audioPath := filepath.Join(dir, "audio.flv")
	v, err := readMediaTrack(videoPath, rtmp.Video, time.Second/time.Duration(cfg.FPS))
	if err != nil {
		return nil, err
	}
	a, err := readMediaTrack(audioPath, rtmp.Audio, 1024*time.Second/time.Duration(cfg.SampleRate))
	if err != nil {
		return nil, err
	}
	if a.duration > 600*time.Second {
		return nil, errors.New("BRB audio must be no longer than 10 minutes")
	}
	v.duration = time.Duration(len(v.frames)) * time.Second / time.Duration(cfg.FPS)
	if len(v.frames) != cfg.FPS*seconds {
		return nil, errors.New("BRB video preparation was incomplete")
	}
	return &brbMedia{video: v, audio: a}, nil
}

func runBRBFFmpeg(ctx context.Context, args ...string) error {
	base := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-max_alloc", "67108864"}
	if exec.CommandContext(ctx, "ffmpeg", append(base, args...)...).Run() != nil {
		return errors.New("BRB preparation failed; check the file and FFmpeg installation")
	}
	return nil
}
func encodeBRBAudio(ctx context.Context, cfg config.BRBProfile, dir string, music bool, volume int) error {
	audioPath := filepath.Join(dir, "audio.flv")
	audioArgs := []string{"-f", "lavfi", "-i", fmt.Sprintf("anullsrc=r=%d:cl=stereo", cfg.SampleRate), "-t", "2"}
	if music {
		audioArgs = []string{"-protocol_whitelist", "file,pipe", "-format_whitelist", "mp3,wav", "-i", filepath.Join(dir, "music"), "-t", "601"}
	}
	audioArgs = append(audioArgs, "-vn", "-af", fmt.Sprintf("volume=%.2f", float64(volume)/100), "-ac", "2", "-ar", fmt.Sprint(cfg.SampleRate), "-c:a", "aac", "-b:a", "128k", "-threads", "2", "-fs", fmt.Sprint(maxMediaBytes), "-f", "flv", audioPath)
	return runBRBFFmpeg(ctx, audioArgs...)
}

func normalizeImage(src io.Reader, path string) error {
	data, err := io.ReadAll(io.LimitReader(src, 10<<20+1))
	if err != nil || len(data) > 10<<20 {
		return errors.New("image must be at most 10 MiB")
	}
	info, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || info.Width < 1 || info.Height < 1 || int64(info.Width)*int64(info.Height) > 20000000 {
		return errors.New("choose a PNG or JPEG image up to 20 megapixels")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return errors.New("cannot decode image")
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// The lossless master is generated from artwork/brb/animation.mjs. Runtime
// preparation only needs FFmpeg; Node and a browser are development tools.
//
//go:embed artwork/arcade.mkv
var defaultBRBVideo []byte

//go:embed artwork/poster.png
var defaultBRBPoster []byte
