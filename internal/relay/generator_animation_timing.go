package relay

import (
	"fmt"
	"math"
	"slices"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func animationLoopSeconds(theme mediaauthor.Theme, stage string) int {
	art, ok := themeArtworkForStage(theme.Style.Artwork, stage)
	if !ok || art.holdEnding || theme.Style.Effect != art.effect {
		return 0
	}
	return art.seconds
}

// Resolve the saved loop count against its pinned theme. Crossfades are extra
// frames so each scene advances the composition by exactly its requested loops.
func resolveAnimationTiming(d mediaauthor.Design, theme mediaauthor.Theme, fps int) (mediaauthor.Design, []mediaauthor.Issue) {
	d.Scenes = slices.Clone(d.Scenes)
	cycle := animationLoopSeconds(theme, d.Stage)
	issues := make([]mediaauthor.Issue, 0)
	for i := range d.Scenes {
		scene := &d.Scenes[i]
		if scene.AnimationLoops == nil {
			continue
		}
		field := fmt.Sprintf("scenes.%d.animation_loops", i)
		if cycle == 0 || d.Stage == "brb" {
			issues = append(issues, mediaauthor.Issue{Field: field, Message: "This theme does not have a repeating scene animation. Choose seconds."})
			continue
		}
		loops := *scene.AnimationLoops
		if loops < 1 || loops > 3600/cycle {
			issues = append(issues, mediaauthor.Issue{Field: field, Message: "Choose a whole number of animation loops within 3600 seconds."})
			continue
		}
		overlap := scene.Transition
		if i == len(d.Scenes)-1 {
			overlap = nil
			if d.Stage == "prestream" {
				overlap = d.LoopTransition
			}
		}
		seconds := float64(loops * cycle)
		if fps > 0 && overlap != nil && overlap.Kind == "crossfade" && overlap.DurationSeconds > 0 && overlap.DurationSeconds <= 600 {
			seconds += math.Round(overlap.DurationSeconds*float64(fps)) / float64(fps)
		}
		scene.DurationSeconds = seconds
	}
	return d, issues
}
