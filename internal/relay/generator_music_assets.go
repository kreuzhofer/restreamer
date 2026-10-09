package relay

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const maxMusicUploadBytes int64 = 32 << 20
const musicSampleRate = 48000
const maxMusicSamples int64 = 600 * musicSampleRate
const maxMusicAssetBytes int64 = 44 + maxMusicSamples*4

func stageGeneratorMusic(ctx context.Context, src io.Reader, path string) (AssetRevision, error) {
	input := path + ".upload"
	defer os.Remove(input)
	f, err := os.Create(input)
	if err != nil {
		return AssetRevision{}, errors.New("Cannot stage music; check storage.")
	}
	n, err := io.Copy(f, io.LimitReader(src, maxMusicUploadBytes+1))
	closed := f.Close()
	if err != nil || closed != nil || n == 0 || n > maxMusicUploadBytes {
		return AssetRevision{}, errors.New("Choose MP3 or WAV audio up to 32 MiB.")
	}
	args := []string{"-v", "error", "-max_alloc", "268435456", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mp3,wav", "-show_entries", "stream=codec_type,codec_name,sample_rate,channels", "-of", "json", input}
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	var probe boundedProbe
	cmd.Stdout = &probe
	if cmd.Run() != nil {
		return AssetRevision{}, errors.New("Cannot read music. Choose a valid MP3 or PCM WAV.")
	}
	var info struct {
		Streams []struct {
			Type     string `json:"codec_type"`
			Codec    string `json:"codec_name"`
			Rate     string `json:"sample_rate"`
			Channels int    `json:"channels"`
		}
	}
	if json.Unmarshal(probe.data, &info) != nil || len(info.Streams) != 1 {
		return AssetRevision{}, errors.New("Choose one audio track without additional video or data tracks.")
	}
	track := info.Streams[0]
	rate, _ := strconv.Atoi(track.Rate)
	if track.Type != "audio" || (track.Codec != "mp3" && !strings.HasPrefix(track.Codec, "pcm_")) || rate < 8000 || rate > 192000 || track.Channels < 1 || track.Channels > 2 {
		return AssetRevision{}, errors.New("Music must be mono or stereo MP3 or PCM WAV at 8–192 kHz.")
	}
	pcm := path + ".pcm"
	defer os.Remove(pcm)
	args = generatorBaseArgs()
	args = append(args, "-xerror", "-threads", "2", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mp3,wav", "-i", input, "-map", "0:a:0", "-vn", "-t", "601", "-ac", "2", "-ar", "48000", "-c:a", "pcm_s16le", "-threads", "2", "-fs", strconv.FormatInt(maxMusicSamples*4+4, 10), "-f", "s16le", pcm)
	if exec.CommandContext(ctx, "ffmpeg", args...).Run() != nil {
		return AssetRevision{}, errors.New("Music decoding failed or timed out. Export a valid MP3 or PCM WAV.")
	}
	raw, err := os.Open(pcm)
	if err != nil {
		return AssetRevision{}, errors.New("Cannot read normalized music.")
	}
	defer raw.Close()
	stat, err := raw.Stat()
	if err != nil || stat.Size()%4 != 0 || stat.Size() < 1920*4 || stat.Size() > maxMusicSamples*4 {
		return AssetRevision{}, errors.New("Music must contain 0.04 to 600 seconds of decoded audio.")
	}
	out, err := os.Create(path)
	if err != nil {
		return AssetRevision{}, errors.New("Cannot save normalized music.")
	}
	header := make([]byte, 44)
	copy(header, "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(stat.Size()+36))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 2)
	binary.LittleEndian.PutUint32(header[24:], 48000)
	binary.LittleEndian.PutUint32(header[28:], 192000)
	binary.LittleEndian.PutUint16(header[32:], 4)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(stat.Size()))
	_, err = out.Write(header)
	if err == nil {
		_, err = io.Copy(out, raw)
	}
	syncErr := out.Sync()
	closeErr := out.Close()
	if err != nil || syncErr != nil || closeErr != nil {
		return AssetRevision{}, errors.New("Cannot save normalized music; check storage.")
	}
	digest, err := mediaDigest(path)
	if err != nil {
		return AssetRevision{}, errors.New("Cannot verify normalized music.")
	}
	return AssetRevision{Digest: digest, Bytes: 44 + stat.Size(), Samples: stat.Size() / 4, SampleRate: 48000, HasAudio: true, DurationSeconds: float64(stat.Size()/4) / 48000, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
}
func validMusicMetadata(m AssetRevision) bool {
	return m.SampleRate == 48000 && m.Samples >= 1920 && m.Samples <= maxMusicSamples && m.Bytes == 44+m.Samples*4 && m.DurationSeconds == float64(m.Samples)/48000
}
func verifyMusicRevision(f *os.File, m AssetRevision) error {
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != m.Bytes || !validMusicMetadata(m) {
		return errors.New("The captured music revision is unavailable or changed.")
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, maxMusicAssetBytes+1))
	if err != nil || n != m.Bytes || hex.EncodeToString(hash.Sum(nil)) != m.Digest {
		return errors.New("The music revision changed. Upload and explicitly adopt a new revision.")
	}
	_, err = f.Seek(0, io.SeekStart)
	return err
}
