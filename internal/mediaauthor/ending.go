package mediaauthor

import (
	"fmt"
	"math"
)

// EndingFadeFrames spans the end of the complete, frame-rounded composition.
// It never adds time or silently shortens the requested envelope.
func EndingFadeFrames(d Design, fps, total int) (int, []Issue) {
	if d.Stage != "ending" || fps <= 0 {
		return 0, nil
	}
	seconds := 1.0
	if d.EndingFadeSeconds != nil {
		seconds = *d.EndingFadeSeconds
	}
	if seconds == 0 {
		return 0, nil
	}
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 600 {
		return 0, []Issue{{"ending_fade_seconds", "Use a finite final fade between two video frames and 600 seconds, or disable it with zero."}}
	}
	frames := int(math.Round(seconds * float64(fps)))
	if frames < 2 {
		return 0, []Issue{{"ending_fade_seconds", fmt.Sprintf("Use at least two video frames (%g seconds at %d fps), or disable the final fade.", 2/float64(fps), fps)}}
	}
	if frames > total {
		return 0, []Issue{{"ending_fade_seconds", fmt.Sprintf("This ending is %g seconds after transition overlaps. Shorten the final fade to fit, disable it, or lengthen the sequence.", float64(max(0, total))/float64(fps))}}
	}
	return frames, nil
}
