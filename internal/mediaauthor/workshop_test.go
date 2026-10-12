package mediaauthor

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestWorkshopThemesKeepEditableContentInsideTheSign(t *testing.T) {
	for _, id := range []string{"maker-workshop", "maker-halloween", "maker-christmas"} {
		t.Run(id, func(t *testing.T) {
			var theme Theme
			for _, candidate := range BuiltinThemes() {
				if candidate.ID == id {
					theme = candidate
				}
			}
			if theme.ID == "" {
				t.Fatalf("missing selectable theme %s", id)
			}
			if issues := ValidateStyle(theme.Style); len(issues) != 0 {
				t.Fatal(issues)
			}
			if !theme.Builtin || theme.Revision != 1 || theme.Style.Effect != "workshop-motion" {
				t.Fatal(theme)
			}
			for _, size := range [][2]int{{320, 180}, {640, 360}, {1920, 1080}} {
				for _, stage := range []string{"prestream", "ending"} {
					d := Design{Name: "Workshop show", Stage: stage, Theme: ThemeRef{ID: id, Revision: 1}, Scenes: []Scene{
						{ID: "title", Layout: "title", Text: "STARTING SOON", DurationSeconds: 12},
						{ID: "list", Layout: "list", Text: "IN THE WORKSHOP", Items: []string{"AI experiments", "3D printing"}, DurationSeconds: 12},
					}}
					if stage == "ending" {
						d.Scenes[0].Text = "SEE YOU SOON"
					}
					if issues := ValidateWithTheme(d, theme, size[0], size[1]); len(issues) != 0 {
						t.Fatalf("%s %v: %v", stage, size, issues)
					}
					for _, scene := range d.Scenes {
						layout, _, message := layoutSceneStyled(scene, size[0], size[1], theme.Style)
						if message != "" {
							t.Fatal(message)
						}
						layout.face.Close()
						if float64(layout.region.Min.Y) < float64(size[1])*.075-1 || float64(layout.region.Max.Y) > float64(size[1])*.265+1 {
							t.Fatalf("content leaves the blank sign: %v", layout.region)
						}
					}
				}
			}
		})
	}
}

func TestWorkshopMotionRejectsUnrelatedArtwork(t *testing.T) {
	style := RetroTheme(1).Style
	style.Effect = "workshop-motion"
	issues := ValidateStyle(style)
	for _, issue := range issues {
		if issue.Field == "style.effect" && strings.Contains(issue.Message, "workshop") {
			return
		}
	}
	t.Fatalf("missing actionable artwork/effect mismatch: %v", issues)
}

func TestShortContentRegionsStillRejectOffscreenPlacement(t *testing.T) {
	style := RetroTheme(1).Style
	style.ContentRegion = ContentRegion{58, 16}
	style.ContentOffsetYPercent = -33
	if issues := ValidateStyle(style); len(issues) != 0 {
		t.Fatalf("valid sign region rejected: %v", issues)
	}
	style.ContentOffsetYPercent = -45
	if issues := ValidateStyle(style); len(issues) == 0 {
		t.Fatal("offscreen sign region accepted")
	}
}

func TestWorkshopFrozenBackdropKeepsHardPixelEdges(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 2, 2))
	source.Set(0, 0, color.White)
	source.Set(1, 0, color.Black)
	source.Set(0, 1, color.Black)
	source.Set(1, 1, color.White)
	got := ThemeBackdrop(20, 20, WorkshopTheme("maker-workshop").Style, RenderInputs{Background: source})
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			want := uint8(0)
			if (x < 10) == (y < 10) {
				want = 255
			}
			if pixel := got.RGBAAt(x, y); pixel.R != want || pixel.G != want || pixel.B != want {
				t.Fatalf("blurred pixel at %d,%d: %v", x, y, pixel)
			}
		}
	}
}

func TestWorkshopSignTracksContainedArtwork(t *testing.T) {
	style := WorkshopTheme("maker-workshop").Style
	scene := Scene{Layout: "title", Text: "STARTING SOON"}
	reference, _, message := layoutSceneStyled(scene, 640, 360, style)
	if message != "" {
		t.Fatal(message)
	}
	defer reference.face.Close()
	for _, profile := range []struct {
		width, height int
		offset        image.Point
	}{{640, 480, image.Pt(0, 60)}, {960, 360, image.Pt(160, 0)}} {
		got, _, message := layoutSceneStyled(scene, profile.width, profile.height, style)
		if message != "" {
			t.Fatal(message)
		}
		defer got.face.Close()
		if got.region != reference.region.Add(profile.offset) || got.face.Metrics() != reference.face.Metrics() {
			t.Fatalf("sign moved away from contained artwork at %dx%d: %v, want %v", profile.width, profile.height, got.region, reference.region.Add(profile.offset))
		}
	}
}
