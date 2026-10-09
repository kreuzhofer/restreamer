package relay

import (
	"errors"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
)

// GeneratedTiming records validated video-frame timing independently of AAC
// packet padding. Ordinary imported revisions omit it and retain their contract.
type GeneratedTiming struct {
	Version int `json:"version"`
	Frames  int `json:"frames"`
}

func validateGeneratedTiming(timing *GeneratedTiming, p config.BRBProfile) error {
	if timing == nil {
		return nil
	}
	if timing.Version != 1 || p.FPS <= 0 || timing.Frames < 1 || timing.Frames > 600*p.FPS {
		return errors.New("Generated media timing is invalid; generate a new revision.")
	}
	return nil
}
func applyGeneratedTiming(idx *clipIndex, timing *GeneratedTiming, p config.BRBProfile) error {
	if err := validateGeneratedTiming(timing, p); err != nil {
		return err
	}
	if timing == nil {
		return nil
	}
	duration := time.Duration(timing.Frames) * time.Second / time.Duration(p.FPS)
	last := time.Duration(timing.Frames-1) * time.Second / time.Duration(p.FPS)
	if idx == nil || idx.Frames != timing.Frames || idx.LastVideo < last-time.Millisecond || idx.LastVideo > last+time.Millisecond || idx.LastAudio < 0 || idx.LastAudio >= duration+time.Millisecond || idx.LastAudio+1024*time.Second/time.Duration(p.SampleRate) < duration-time.Millisecond {
		return errors.New("Generated media does not match its saved frame timing; generate a new revision.")
	}
	idx.Duration = duration
	idx.Exact = true
	idx.ExactFPS = p.FPS
	return nil
}

var errGeneratedTimingCollision = errors.New("Identical output already belongs to an ordinary library revision. Change the design or use that existing revision; its legacy timing was preserved.")
