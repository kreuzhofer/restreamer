package mediaauthor

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var sansFont = mustFont(goregular.TTF)
var monoFont = mustFont(gomono.TTF)

func mustFont(data []byte) *opentype.Font {
	f, err := opentype.Parse(data)
	if err != nil {
		panic(err)
	}
	return f
}

// Validate uses the same glyph metrics, wrapping and bounds as RenderScene.
// Font sizes are in a 1080-high design coordinate system, scaled to the profile.
func Validate(d Design, width, height int) []Issue {
	issues := make([]Issue, 0)
	if strings.TrimSpace(d.Name) == "" {
		issues = append(issues, Issue{"name", "Name this show design."})
	}
	if d.Stage != "prestream" && d.Stage != "ending" {
		issues = append(issues, Issue{"stage", "Choose prestream or ending."})
	}
	if d.Theme.ID != "retro" || d.Theme.Revision != 1 {
		issues = append(issues, Issue{"theme", "Choose the built-in retro theme, revision 1."})
	}
	if len(d.Scenes) != 1 {
		issues = append(issues, Issue{"scenes", "This editor requires one title scene."})
	}
	for i, scene := range d.Scenes {
		prefix := fmt.Sprintf("scenes.%d.", i)
		if scene.Layout != "title" {
			issues = append(issues, Issue{prefix + "layout", "Choose the title layout."})
		}
		if math.IsNaN(scene.DurationSeconds) || math.IsInf(scene.DurationSeconds, 0) || scene.DurationSeconds <= 0 || scene.DurationSeconds > 3600 {
			issues = append(issues, Issue{prefix + "duration_seconds", "Use a finite duration greater than zero and at most 3600 seconds."})
		}
		face, lines, field, message := layoutTitle(scene, width, height)
		if face != nil {
			face.Close()
		}
		_ = lines
		if message != "" {
			issues = append(issues, Issue{prefix + field, message})
		}
	}
	return issues
}

func layoutTitle(scene Scene, width, height int) (font.Face, []string, string, string) {
	if width < 320 || height < 180 || width > 3840 || height > 2160 {
		return nil, nil, "text", "Unsupported streaming profile."
	}
	f := sansFont
	switch scene.Font {
	case "", "go-sans":
	case "go-mono":
		f = monoFont
	default:
		return nil, nil, "font", "Choose Go Sans or Go Mono."
	}
	size := scene.FontSize
	if size == 0 {
		size = 64
	}
	if math.IsNaN(size) || math.IsInf(size, 0) || size < 24 || size > 120 {
		return nil, nil, "font_size", "Use a font size from 24 to 120 (at 1080p)."
	}
	for _, r := range scene.Text {
		if r == '\n' {
			continue
		}
		idx, err := f.GlyphIndex(nil, r)
		if err != nil || idx == 0 || unicode.IsControl(r) {
			return nil, nil, "text", fmt.Sprintf("Unsupported character U+%04X. Use a supported glyph; no substitution is applied.", r)
		}
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size * float64(height) / 1080, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, nil, "font", "Cannot load the selected font."
	}
	maxWidth := fixed.I(width * 8 / 10)
	lines := make([]string, 0)
	// Preserve explicit newlines, mixed case and spaces; wrap at word boundaries.
	for _, paragraph := range strings.Split(scene.Text, "\n") {
		line := ""
		for _, word := range strings.SplitAfter(paragraph, " ") {
			if font.MeasureString(face, strings.TrimRight(word, " ")) > maxWidth {
				return face, nil, "text", "A word exceeds the content region. Add a space or line break, or choose a smaller readable font size."
			}
			candidate := line + word
			if font.MeasureString(face, candidate) > maxWidth && line != "" {
				lines = append(lines, strings.TrimRight(line, " "))
				line = word
			} else {
				line = candidate
			}
		}
		lines = append(lines, strings.TrimRight(line, " "))
	}
	metrics := face.Metrics()
	lineHeight := metrics.Height * 6 / 5
	blockHeight := metrics.Ascent + metrics.Descent + fixed.Int26_6(len(lines)-1)*lineHeight
	if blockHeight > fixed.I(height*8/10) {
		return face, nil, "text", "Text overflows the content region. Shorten it or choose a smaller readable font size; text is never truncated or automatically shrunk."
	}
	for _, line := range lines {
		bounds, _ := font.BoundString(face, line)
		if bounds.Max.X-bounds.Min.X > maxWidth {
			return face, nil, "text", "Text extends beyond the content region."
		}
	}
	return face, lines, "", ""
}

// RenderScene creates the exact raster used by both editing preview and media
// generation. It never falls back to system fonts or changes the chosen size.
func RenderScene(scene Scene, width, height int) (*image.RGBA, error) {
	face, lines, _, message := layoutTitle(scene, width, height)
	if face != nil {
		defer face.Close()
	}
	if message != "" {
		return nil, fmt.Errorf("%s", message)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{15, 18, 35, 255}), image.Point{}, draw.Src)
	accent := image.NewUniform(color.RGBA{63, 224, 208, 255})
	margin := height / 24
	thickness := max(2, height/180)
	for _, rect := range []image.Rectangle{image.Rect(margin, margin, width-margin, margin+thickness), image.Rect(margin, height-margin-thickness, width-margin, height-margin), image.Rect(margin, margin, margin+thickness, height-margin), image.Rect(width-margin-thickness, margin, width-margin, height-margin)} {
		draw.Draw(img, rect, accent, image.Point{}, draw.Src)
	}
	metrics := face.Metrics()
	lineHeight := metrics.Height * 6 / 5
	blockHeight := metrics.Ascent + metrics.Descent + fixed.Int26_6(len(lines)-1)*lineHeight
	baseline := (fixed.I(height)-blockHeight)/2 + metrics.Ascent
	drawer := font.Drawer{Dst: img, Src: image.NewUniform(color.RGBA{238, 232, 255, 255}), Face: face}
	for _, line := range lines {
		drawer.Dot = fixed.Point26_6{X: (fixed.I(width) - font.MeasureString(face, line)) / 2, Y: baseline}
		drawer.DrawString(line)
		baseline += lineHeight
	}
	return img, nil
}
