package mediaauthor

import (
	"fmt"
	"math"
)

// SceneTiming uses output frame coordinates. Incoming and outgoing overlap
// regions never intersect, so composition needs at most two scene inputs.
type SceneTiming struct{ Start, Frames, Incoming, Outgoing int }
type SequenceTiming struct {
	Scenes []SceneTiming
	Frames int
}

func PlanTiming(d Design, fps int) (SequenceTiming, []Issue) {
	plan := SequenceTiming{Scenes: make([]SceneTiming, len(d.Scenes))}
	issues := make([]Issue, 0)
	if fps <= 0 {
		return plan, issues
	}
	raw := 0
	for i, scene := range d.Scenes {
		if math.IsNaN(scene.DurationSeconds) || math.IsInf(scene.DurationSeconds, 0) || scene.DurationSeconds <= 0 || scene.DurationSeconds > 600 || math.Round(scene.DurationSeconds*float64(fps)) < 1 {
			issues = append(issues, Issue{fmt.Sprintf("scenes.%d.duration_seconds", i), "Use at least one video frame and at most 600 seconds."})
		} else {
			plan.Scenes[i].Frames = int(math.Round(scene.DurationSeconds * float64(fps)))
			raw += plan.Scenes[i].Frames
		}
		if tr := scene.Transition; tr != nil {
			field := fmt.Sprintf("scenes.%d.transition", i)
			switch tr.Kind {
			case "cut":
				if tr.DurationSeconds != 0 {
					issues = append(issues, Issue{field + ".duration_seconds", "A cut has no overlap duration."})
				}
			case "crossfade":
				if math.IsNaN(tr.DurationSeconds) || math.IsInf(tr.DurationSeconds, 0) || tr.DurationSeconds <= 0 || tr.DurationSeconds > 600 || math.Round(tr.DurationSeconds*float64(fps)) < 1 {
					issues = append(issues, Issue{field + ".duration_seconds", "Use a finite crossfade of at least one video frame and at most 600 seconds."})
				} else if i+1 < len(d.Scenes) {
					plan.Scenes[i].Outgoing = int(math.Round(tr.DurationSeconds * float64(fps)))
				}
			default:
				issues = append(issues, Issue{field + ".kind", "Choose cut or crossfade."})
			}
		}
	}
	for i := range plan.Scenes {
		scene := &plan.Scenes[i]
		if i > 0 {
			previous := plan.Scenes[i-1]
			scene.Incoming = previous.Outgoing
			scene.Start = previous.Start + previous.Frames - previous.Outgoing
		}
		if scene.Incoming+scene.Outgoing > scene.Frames {
			if scene.Incoming > 0 {
				issues = append(issues, Issue{fmt.Sprintf("scenes.%d.transition.duration_seconds", i-1), "Incoming and outgoing crossfades must fit their shared scene duration."})
			}
			if scene.Outgoing > 0 {
				issues = append(issues, Issue{fmt.Sprintf("scenes.%d.transition.duration_seconds", i), "Incoming and outgoing crossfades must fit their shared scene duration."})
			}
		}
		plan.Frames += scene.Frames - scene.Outgoing
	}
	if raw > 600*fps {
		issues = append(issues, Issue{"scenes", "The scenes before overlaps must total at most 600 seconds after rounding to video frames."})
	}
	return plan, issues
}

func SequenceDuration(d Design, fps int) float64 {
	if fps <= 0 {
		return 0
	}
	p, _ := PlanTiming(d, fps)
	return float64(max(0, p.Frames)) / float64(fps)
}
func ValidateTiming(d Design, fps int) []Issue {
	plan, issues := PlanTiming(d, fps)
	_, finishIssues := EndingFadeFrames(d, fps, plan.Frames)
	return append(issues, finishIssues...)
}
