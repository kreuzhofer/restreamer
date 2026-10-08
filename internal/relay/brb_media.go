package relay

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
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

func encodeBRB(ctx context.Context, cfg config.BRBProfile, dir string, music bool, volume int) (*brbMedia, error) {
	run := func(args ...string) error {
		base := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-max_alloc", "67108864"}
		cmd := exec.CommandContext(ctx, "ffmpeg", append(base, args...)...)
		// Errors are fixed strings: uploaded metadata and filenames never reach logs.
		if err := cmd.Run(); err != nil {
			return errors.New("BRB preparation failed; check the file and FFmpeg installation")
		}
		return nil
	}
	videoPath := filepath.Join(dir, "video.flv")
	fps := fmt.Sprint(cfg.FPS)
	vf := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,format=yuv420p", cfg.Width, cfg.Height, cfg.Width, cfg.Height)
	if err := run("-protocol_whitelist", "file,pipe", "-loop", "1", "-framerate", fps, "-i", filepath.Join(dir, "image.png"), "-t", "2", "-vf", vf, "-an", "-c:v", "libx264", "-preset", "veryfast", "-tune", "stillimage", "-profile:v", "high", "-bf", "0", "-g", fmt.Sprint(cfg.FPS*2), "-threads", "2", "-fs", fmt.Sprint(maxMediaBytes), "-f", "flv", videoPath); err != nil {
		return nil, err
	}
	audioPath := filepath.Join(dir, "audio.flv")
	audioArgs := []string{"-f", "lavfi", "-i", fmt.Sprintf("anullsrc=r=%d:cl=stereo", cfg.SampleRate), "-t", "2"}
	if music {
		audioArgs = []string{"-protocol_whitelist", "file,pipe", "-format_whitelist", "mp3,wav", "-i", filepath.Join(dir, "music"), "-t", "601"}
	}
	audioArgs = append(audioArgs, "-vn", "-af", fmt.Sprintf("volume=%.2f", float64(volume)/100), "-ac", "2", "-ar", fmt.Sprint(cfg.SampleRate), "-c:a", "aac", "-b:a", "128k", "-threads", "2", "-fs", fmt.Sprint(maxMediaBytes), "-f", "flv", audioPath)
	if err := run(audioArgs...); err != nil {
		return nil, err
	}
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
	if len(v.frames) != cfg.FPS*2 {
		return nil, errors.New("BRB video preparation was incomplete")
	}
	return &brbMedia{video: v, audio: a}, nil
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

func defaultBRBImage(path string) error {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for y := 0; y < 720; y++ {
		for x := 0; x < 1280; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(14 + y/90), uint8(22 + y/70), uint8(34 + y/45), 255})
		}
	}
	// A small built-in bitmap alphabet keeps default artwork and native builds
	// independent of system fonts or external image-generation services.
	glyphs := map[rune][]string{
		'B': {"11110", "10001", "10001", "11110", "10001", "10001", "11110"},
		'E': {"11111", "10000", "10000", "11110", "10000", "10000", "11111"},
		'R': {"11110", "10001", "10001", "11110", "10100", "10010", "10001"},
		'I': {"11111", "00100", "00100", "00100", "00100", "00100", "11111"},
		'G': {"01111", "10000", "10000", "10111", "10001", "10001", "01111"},
		'H': {"10001", "10001", "10001", "11111", "10001", "10001", "10001"},
		'T': {"11111", "00100", "00100", "00100", "00100", "00100", "00100"},
		'A': {"01110", "10001", "10001", "11111", "10001", "10001", "10001"},
		'C': {"01111", "10000", "10000", "10000", "10000", "10000", "01111"},
		'K': {"10001", "10010", "10100", "11000", "10100", "10010", "10001"},
	}
	text := "BE RIGHT BACK"
	size := 12
	x0 := (1280 - (len(text)*6-1)*size) / 2
	for i, ch := range text {
		for y, row := range glyphs[ch] {
			for x, v := range row {
				if v == '1' {
					for dy := 0; dy < size; dy++ {
						for dx := 0; dx < size; dx++ {
							img.SetRGBA(x0+i*6*size+x*size+dx, 318+y*size+dy, color.RGBA{220, 241, 234, 255})
						}
					}
				}
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
