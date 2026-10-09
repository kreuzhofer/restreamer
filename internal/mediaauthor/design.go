// Package mediaauthor defines authored media independently of broadcast playback.
package mediaauthor

import (
	"fmt"
	"math"
)

// ThemeRef pins appearance independently of scene content and timing.
type ThemeRef struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
}

// ContentRegion remains centered; percentages adjust its size without pixel positioning.
// Zero values inherit the theme default.
type ContentRegion struct {
	WidthPercent  float64 `json:"width_percent,omitempty"`
	HeightPercent float64 `json:"height_percent,omitempty"`
}

const MaxScenes = 20
const MaxListItems = 20
const MaxSceneTextBytes = 4096

type AssetRef struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
}

type VideoScene struct {
	Asset              AssetRef `json:"asset"`
	TrimStartSeconds   float64  `json:"trim_start_seconds,omitempty"`
	TrimEndSeconds     float64  `json:"trim_end_seconds,omitempty"`
	Repeat             bool     `json:"repeat,omitempty"`
	AudioEnabled       bool     `json:"audio_enabled,omitempty"`
	AudioVolumePercent *float64 `json:"audio_volume_percent,omitempty"`
}

func (s Scene) IsVideo() bool { return s.Layout == "media" && s.MediaKind == "video" }
func (v VideoScene) Volume() float64 {
	if v.AudioVolumePercent == nil {
		return 100
	}
	return *v.AudioVolumePercent
}

type Scene struct {
	MediaKind       string        `json:"media_kind,omitempty"`
	Video           *VideoScene   `json:"video,omitempty"`
	Image           *AssetRef     `json:"image,omitempty"`
	ID              string        `json:"id"`
	Layout          string        `json:"layout"`
	Text            string        `json:"text"`
	Items           []string      `json:"items,omitempty"`
	ContentRegion   ContentRegion `json:"content_region,omitempty"`
	Alignment       string        `json:"alignment,omitempty"`
	DurationSeconds float64       `json:"duration_seconds"`
	// Empty font and zero font size inherit the selected theme.
	Font     string  `json:"font,omitempty"`
	FontSize float64 `json:"font_size,omitempty"`
}

type Soundtrack struct {
	Asset          AssetRef `json:"asset"`
	Mode           string   `json:"mode"`
	VolumePercent  *float64 `json:"volume_percent,omitempty"`
	FadeInSeconds  float64  `json:"fade_in_seconds,omitempty"`
	FadeOutSeconds float64  `json:"fade_out_seconds,omitempty"`
}

func (m Soundtrack) Volume() float64 {
	if m.VolumePercent == nil {
		return 100
	}
	return *m.VolumePercent
}

type Design struct {
	Soundtrack *Soundtrack `json:"soundtrack,omitempty"`
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Stage      string      `json:"stage"`
	Version    int         `json:"version"`
	Theme      ThemeRef    `json:"theme"`
	Scenes     []Scene     `json:"scenes"`
	UpdatedAt  string      `json:"updated_at"`
}

type Issue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// CutDuration rounds every scene independently to the nearest video frame.
func CutDuration(d Design, fps int) float64 {
	if fps <= 0 {
		return 0
	}
	frames := 0.0
	for _, scene := range d.Scenes {
		if scene.DurationSeconds > 0 && !math.IsNaN(scene.DurationSeconds) && !math.IsInf(scene.DurationSeconds, 0) {
			frames += math.Round(scene.DurationSeconds * float64(fps))
		}
	}
	return frames / float64(fps)
}

// ValidateCutTiming keeps displayed timing and generation admission consistent.
func ValidateCutTiming(d Design, fps int) []Issue {
	issues := make([]Issue, 0)
	for i, scene := range d.Scenes {
		if scene.DurationSeconds > 600 || (scene.DurationSeconds > 0 && math.Round(scene.DurationSeconds*float64(fps)) < 1) {
			issues = append(issues, Issue{fmt.Sprintf("scenes.%d.duration_seconds", i), "Use at least one video frame and at most 600 seconds."})
		}
	}
	if CutDuration(d, fps) > 600 {
		issues = append(issues, Issue{"scenes", "The complete sequence must be at most 600 seconds after rounding each scene to video frames."})
	}
	return issues
}

// DesignAssetRefs retains references even when a draft temporarily changes layout.
func DesignAssetRefs(d Design) []AssetRef {
	refs := make([]AssetRef, 0)
	seen := make(map[AssetRef]bool)
	if d.Soundtrack != nil {
		refs = append(refs, d.Soundtrack.Asset)
		seen[d.Soundtrack.Asset] = true
	}
	for _, scene := range d.Scenes {
		if scene.Video != nil && !seen[scene.Video.Asset] {
			refs = append(refs, scene.Video.Asset)
			seen[scene.Video.Asset] = true
		}
		if scene.Image != nil && !seen[*scene.Image] {
			refs = append(refs, *scene.Image)
			seen[*scene.Image] = true
		}
	}
	return refs
}
