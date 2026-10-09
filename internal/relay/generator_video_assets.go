package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const maxVideoAssetBytes int64 = 512 << 20

func stageGeneratorVideo(ctx context.Context, src io.Reader, path string) (AssetRevision, error) {
	f, err := os.Create(path)
	if err != nil {
		return AssetRevision{}, errors.New("Cannot save video upload; check storage.")
	}
	n, err := io.Copy(f, io.LimitReader(src, maxVideoAssetBytes+1))
	closeErr := f.Close()
	if err != nil || closeErr != nil || n == 0 || n > maxVideoAssetBytes {
		return AssetRevision{}, errors.New("Choose an MP4 up to 512 MiB.")
	}
	args := []string{"-v", "error", "-max_alloc", "268435456", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,mp4,m4a,3gp,3g2,mj2", "-enable_drefs", "0", "-use_absolute_path", "0", "-show_entries", "stream=codec_type,codec_name,width,height,r_frame_rate,duration,sample_rate,channels:format=duration", "-of", "json", path}
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	var output boundedProbe
	cmd.Stdout = &output
	if cmd.Run() != nil {
		return AssetRevision{}, errors.New("Cannot read MP4 video; check the file and FFprobe installation.")
	}
	var data struct {
		Streams []struct {
			Type          string `json:"codec_type"`
			Codec         string `json:"codec_name"`
			Width, Height int
			Rate          string `json:"r_frame_rate"`
			Duration      string `json:"duration"`
			SampleRate    string `json:"sample_rate"`
			Channels      int
		}
		Format struct {
			Duration string `json:"duration"`
		}
	}
	if json.Unmarshal(output.data, &data) != nil {
		return AssetRevision{}, errors.New("Cannot read video metadata.")
	}
	meta := AssetRevision{Bytes: n, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	videos, audios := 0, 0
	for _, stream := range data.Streams {
		switch stream.Type {
		case "video":
			videos++
			meta.Width, meta.Height = stream.Width, stream.Height
			parts := strings.Split(stream.Rate, "/")
			if len(parts) == 2 {
				a, _ := strconv.ParseFloat(parts[0], 64)
				b, _ := strconv.ParseFloat(parts[1], 64)
				meta.FPS = a / b
			}
			duration := stream.Duration
			if duration == "" || duration == "N/A" {
				duration = data.Format.Duration
			}
			meta.DurationSeconds, _ = strconv.ParseFloat(duration, 64)
			if stream.Codec != "h264" {
				return AssetRevision{}, errors.New("Choose MP4 video encoded as H.264, with optional AAC audio.")
			}
		case "audio":
			audios++
			rate, _ := strconv.Atoi(stream.SampleRate)
			if stream.Codec != "aac" || stream.Channels < 1 || stream.Channels > 2 || rate < 8000 || rate > 48000 {
				return AssetRevision{}, errors.New("Video audio must be mono or stereo AAC at 8–48 kHz.")
			}
			meta.HasAudio = true
		default:
			return AssetRevision{}, errors.New("Choose an MP4 containing only one video track and optional audio; remove extra data or subtitle tracks.")
		}
	}
	if videos != 1 || audios > 1 || !validVideoMetadata(meta) {
		return AssetRevision{}, errors.New("Video requires one H.264 track, up to 1920 × 1080 and 60 fps, between 0.04 and 600 seconds.")
	}
	// Decode within the preparation slot before acknowledging the immutable asset.
	args = []string{"-hide_banner", "-v", "error", "-nostdin", "-xerror", "-max_alloc", "268435456", "-threads", "2", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,mp4,m4a,3gp,3g2,mj2", "-enable_drefs", "0", "-use_absolute_path", "0", "-i", path, "-map", "0:v:0", "-map", "0:a:0?", "-t", "601", "-threads", "2", "-f", "null", "-"}
	if exec.CommandContext(ctx, "ffmpeg", args...).Run() != nil {
		return AssetRevision{}, errors.New("Video decoding failed or preparation timed out; use a valid H.264/AAC MP4.")
	}
	meta.Digest, err = mediaDigest(path)
	if err != nil {
		return AssetRevision{}, errors.New("Cannot verify video upload; check storage.")
	}
	return meta, nil
}
func validVideoMetadata(m AssetRevision) bool {
	return m.Bytes > 0 && m.Bytes <= maxVideoAssetBytes && m.Width > 0 && m.Width <= 1920 && m.Height > 0 && m.Height <= 1080 && m.FPS > 0 && m.FPS <= 60 && !math.IsInf(m.FPS, 0) && !math.IsNaN(m.FPS) && m.DurationSeconds >= .04 && m.DurationSeconds <= 600 && !math.IsInf(m.DurationSeconds, 0) && !math.IsNaN(m.DurationSeconds)
}
func verifyVideoRevision(f *os.File, m AssetRevision) error {
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != m.Bytes || !validVideoMetadata(m) {
		return errors.New("The video revision is unavailable or changed.")
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, maxVideoAssetBytes+1))
	if err != nil || n != m.Bytes || hex.EncodeToString(hash.Sum(nil)) != m.Digest {
		return errors.New("The video revision changed; upload and explicitly adopt a new revision.")
	}
	_, err = f.Seek(0, io.SeekStart)
	return err
}
