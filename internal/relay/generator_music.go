package relay

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func (g *generatorStore) musicIssues(d mediaauthor.Design, fps, rate int) []mediaauthor.Issue {
	m := d.Soundtrack
	if m == nil {
		return nil
	}
	var issues []mediaauthor.Issue
	add := func(field, message string) {
		issues = append(issues, mediaauthor.Issue{Field: "soundtrack." + field, Message: message})
	}
	if m.Mode != "end" && m.Mode != "repeat" {
		add("mode", "Choose End at track end or Repeat to design end.")
	}
	if m.Volume() < 0 || m.Volume() > 100 || math.IsNaN(m.Volume()) || math.IsInf(m.Volume(), 0) {
		add("volume_percent", "Music volume must be between 0 and 100%.")
	}
	meta, ok := g.assetRevision(m.Asset)
	length := sampleBoundary(int(math.Round(mediaauthor.SequenceDuration(d, fps)*float64(fps))), rate, fps)
	if ok && m.Mode == "end" {
		length = min(length, int64(math.Round(float64(meta.Samples)*float64(rate)/48000)))
	}
	fadeSum := int64(0)
	for field, value := range map[string]float64{"fade_in_seconds": m.FadeInSeconds, "fade_out_seconds": m.FadeOutSeconds} {
		if value < 0 || value > 600 || math.IsNaN(value) || math.IsInf(value, 0) {
			add(field, "Fades must be between 0 and 600 seconds.")
			continue
		}
		n := int64(math.Round(value * float64(rate)))
		fadeSum += n
		if value > 0 && n < 2 {
			add(field, "Use a fade of at least two audio samples, or zero to disable it.")
		}
	}
	if fadeSum > length {
		add("fade_out_seconds", "Combined fades must fit the audible music duration (the shorter track/design in End mode).")
	}
	return issues
}

// mixGeneratorMusic runs after complete scene PCM assembly and before the sole
// AAC encode. Two bounded PCM passes measure peak, then apply one fixed gain.
// The caller already holds preparation admission.
func (s *Server) mixGeneratorMusic(ctx context.Context, j GenerationJob, sequence, dir string, budget int64) (float64, error) {
	m := j.Design.Soundtrack
	p := j.Profile
	count := sampleBoundary(int(math.Round(j.Duration*float64(p.FPS))), p.SampleRate, p.FPS)
	if count*4 >= budget {
		return 0, errors.New("Music mixing exceeds the 512 MiB workspace limit. Shorten the design.")
	}
	g := s.generator
	g.mu.Lock()
	meta, ok := g.assetRevision(m.Asset)
	kind := g.assets[m.Asset.ID].Kind
	source, err := os.Open(g.assetPath(m.Asset))
	g.mu.Unlock()
	if !ok || kind != "audio" || err != nil {
		if source != nil {
			source.Close()
		}
		return 0, errors.New("The captured music revision is unavailable.")
	}
	defer source.Close()
	if err = verifyMusicRevision(source, meta); err != nil {
		return 0, err
	}
	musicPath := filepath.Join(dir, "soundtrack.pcm")
	defer os.Remove(musicPath)
	args := generatorBaseArgs()
	if m.Mode == "repeat" {
		args = append(args, "-stream_loop", "-1")
	}
	args = append(args, "-threads", "2", "-protocol_whitelist", "file,pipe", "-format_whitelist", "wav", "-i", source.Name(), "-map", "0:a:0", "-af", fmt.Sprintf("aresample=%d,apad,atrim=end_sample=%d", p.SampleRate, count), "-ac", "2", "-ar", strconv.Itoa(p.SampleRate), "-c:a", "pcm_s16le", "-threads", "2", "-fs", strconv.FormatInt(count*4+1, 10), "-f", "s16le", musicPath)
	if exec.CommandContext(ctx, "ffmpeg", args...).Run() != nil {
		return 0, errors.New("Cannot prepare the soundtrack; check audio and available storage.")
	}
	music, err := os.Open(musicPath)
	if err != nil {
		return 0, errors.New("Cannot read prepared soundtrack.")
	}
	defer music.Close()
	audio, err := os.OpenFile(sequence, os.O_RDWR, 0)
	if err != nil {
		return 0, errors.New("Cannot open scene audio for mixing.")
	}
	defer audio.Close()
	for _, f := range []*os.File{music, audio} {
		info, err := f.Stat()
		if err != nil || info.Size() != count*4 {
			return 0, errors.New("Prepared audio has an incomplete sample count.")
		}
	}
	length := count
	if m.Mode == "end" {
		length = min(length, int64(math.Round(float64(meta.Samples)*float64(p.SampleRate)/48000)))
	}
	fadeIn := int64(math.Round(m.FadeInSeconds * float64(p.SampleRate)))
	fadeOut := int64(math.Round(m.FadeOutSeconds * float64(p.SampleRate)))
	left, right := make([]byte, 32768), make([]byte, 32768)
	gain, peak := 1.0, 1.0
	for pass := 0; pass < 2; pass++ {
		for offset := int64(0); offset < count*4; offset += int64(len(left)) {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			n := int(min(int64(len(left)), count*4-offset))
			if _, err := audio.ReadAt(left[:n], offset); err != nil {
				return 0, errors.New("Cannot read scene audio.")
			}
			if _, err := music.ReadAt(right[:n], offset); err != nil {
				return 0, errors.New("Cannot read soundtrack audio.")
			}
			for i := 0; i < n; i += 2 {
				frame := (offset + int64(i)) / 4
				envelope := m.Volume() / 100
				if frame >= length {
					envelope = 0
				} else {
					if fadeIn > 1 && frame < fadeIn {
						envelope *= float64(frame) / float64(fadeIn-1)
					}
					if fadeOut > 1 && frame >= length-fadeOut {
						envelope *= float64(length-1-frame) / float64(fadeOut-1)
					}
				}
				sum := float64(int16(binary.LittleEndian.Uint16(left[i:]))) + envelope*float64(int16(binary.LittleEndian.Uint16(right[i:])))
				if pass == 0 {
					if sum >= 0 {
						peak = max(peak, sum/32767)
					} else {
						peak = max(peak, -sum/32768)
					}
				} else {
					binary.LittleEndian.PutUint16(left[i:], uint16(int16(math.Round(sum*gain))))
				}
			}
			if pass == 1 {
				if _, err := audio.WriteAt(left[:n], offset); err != nil {
					return 0, errors.New("Cannot save mixed audio; check storage.")
				}
			}
		}
		if pass == 0 {
			gain = 1 / peak
		}
	}
	// A changed backing file must not publish a result from different bytes.
	opened, openErr := source.Stat()
	current, currentErr := os.Stat(source.Name())
	if openErr != nil || currentErr != nil || !os.SameFile(opened, current) {
		return 0, errors.New("Captured music changed during preparation.")
	}
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		return 0, errors.New("Cannot verify captured music.")
	}
	if err = verifyMusicRevision(source, meta); err != nil {
		return 0, err
	}
	return gain, nil
}
