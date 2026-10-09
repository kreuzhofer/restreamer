package relay

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

// applyGeneratorEndingFade operates on the complete mixed PCM before the sole
// AAC encode. The last video frame has exact source silence; lossy AAC may have
// small residuals around the envelope. No samples or delivery delay are added.
func applyGeneratorEndingFade(ctx context.Context, j GenerationJob, sequence string, frames int) (int, error) {
	fade, issues := mediaauthor.EndingFadeFrames(j.Design, j.Profile.FPS, frames)
	if len(issues) > 0 {
		return 0, errors.New(issues[0].Message)
	}
	if fade == 0 {
		return 0, nil
	}
	p := j.Profile
	start := sampleBoundary(frames-fade, p.SampleRate, p.FPS)
	silence := sampleBoundary(frames-1, p.SampleRate, p.FPS)
	count := sampleBoundary(frames, p.SampleRate, p.FPS)
	audio, err := os.OpenFile(sequence, os.O_RDWR, 0)
	if err != nil {
		return 0, errors.New("Cannot open mixed audio for the final fade.")
	}
	defer audio.Close()
	stat, err := audio.Stat()
	if err != nil || stat.Size() != count*4 {
		return 0, errors.New("Ending audio has an incomplete sample count.")
	}
	buf := make([]byte, 32768)
	for offset := start * 4; offset < count*4; offset += int64(len(buf)) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		n := int(min(int64(len(buf)), count*4-offset))
		if _, err := audio.ReadAt(buf[:n], offset); err != nil {
			return 0, errors.New("Cannot read mixed ending audio.")
		}
		for i := 0; i < n; i += 2 {
			sample := (offset + int64(i)) / 4
			gain := float64(max(int64(0), silence-sample)) / float64(silence-start)
			value := float64(int16(binary.LittleEndian.Uint16(buf[i:])))
			binary.LittleEndian.PutUint16(buf[i:], uint16(int16(math.Round(value*gain))))
		}
		if _, err := audio.WriteAt(buf[:n], offset); err != nil {
			return 0, errors.New("Cannot save the final audio fade; check storage.")
		}
	}
	return fade, nil
}
