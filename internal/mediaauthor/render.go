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

// Validate uses exactly the glyph metrics and content regions used by RenderScene.
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
	if len(d.Scenes) == 0 || len(d.Scenes) > MaxScenes {
		issues = append(issues, Issue{"scenes", "Use between 1 and 20 scenes."})
	}
	ids := make(map[string]bool)
	for i, scene := range d.Scenes {
		prefix := fmt.Sprintf("scenes.%d.", i)
		if scene.ID == "" || ids[scene.ID] {
			issues = append(issues, Issue{prefix + "id", "Each scene needs its own nonempty identity."})
		}
		ids[scene.ID] = true
		if math.IsNaN(scene.DurationSeconds) || math.IsInf(scene.DurationSeconds, 0) || scene.DurationSeconds <= 0 || scene.DurationSeconds > 3600 {
			issues = append(issues, Issue{prefix + "duration_seconds", "Use a finite duration greater than zero and at most 3600 seconds."})
		}
		layout, field, message := layoutScene(scene, width, height)
		if layout.face != nil {
			layout.face.Close()
		}
		if message != "" {
			issues = append(issues, Issue{prefix + field, message})
		}
	}
	return issues
}

type sceneLine struct {
	text      string
	y, indent fixed.Int26_6
	bullet    bool
}
type sceneLayout struct {
	face      font.Face
	lines     []sceneLine
	region    image.Rectangle
	height    fixed.Int26_6
	alignment string
}

func layoutScene(scene Scene, width, height int) (result sceneLayout, field, message string) {
	fail := func(field, message string) (sceneLayout, string, string) { return result, field, message }
	if width < 320 || height < 180 || width > 3840 || height > 2160 {
		return fail("text", "Unsupported streaming profile.")
	}
	if scene.Layout != "title" && scene.Layout != "list" {
		return fail("layout", "Choose a title or list layout.")
	}
	if scene.Alignment != "" && scene.Alignment != "left" && scene.Alignment != "center" && scene.Alignment != "right" {
		return fail("alignment", "Choose left, center or right alignment.")
	}
	result.alignment = scene.Alignment
	if result.alignment == "" {
		result.alignment = "center"
		if scene.Layout == "list" {
			result.alignment = "left"
		}
	}
	rw, rh := scene.ContentRegion.WidthPercent, scene.ContentRegion.HeightPercent
	if rw == 0 {
		rw = 80
	}
	if rh == 0 {
		rh = 80
	}
	if math.IsNaN(rw) || math.IsInf(rw, 0) || math.IsNaN(rh) || math.IsInf(rh, 0) || rw < 30 || rw > 90 || rh < 30 || rh > 90 {
		return fail("content_region", "Content width and height must be between 30% and 90% (blank inherits 80%).")
	}
	regionWidth, regionHeight := int(float64(width)*rw/100), int(float64(height)*rh/100)
	result.region = image.Rect((width-regionWidth)/2, (height-regionHeight)/2, (width+regionWidth)/2, (height+regionHeight)/2)
	f := sansFont
	switch scene.Font {
	case "", "go-sans":
	case "go-mono":
		f = monoFont
	default:
		return fail("font", "Choose Go Sans or Go Mono.")
	}
	size := scene.FontSize
	if size == 0 {
		size = 64
	}
	if math.IsNaN(size) || math.IsInf(size, 0) || size < 24 || size > 120 {
		return fail("font_size", "Use a font size from 24 to 120 (at 1080p).")
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size * float64(height) / 1080, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return fail("font", "Cannot load the selected font.")
	}
	result.face = face
	metrics := face.Metrics()
	lineHeight := metrics.Height * 6 / 5
	nextY := fixed.Int26_6(0)
	appendText := func(text, field string, indent fixed.Int26_6, bullet bool) (string, string) {
		for _, r := range text {
			if r == '\n' {
				continue
			}
			idx, err := f.GlyphIndex(nil, r)
			if err != nil || idx == 0 || unicode.IsControl(r) {
				return field, fmt.Sprintf("Unsupported character U+%04X. Use a supported glyph; no substitution is applied.", r)
			}
		}
		maxWidth := fixed.I(regionWidth) - indent
		lines := make([]string, 0)
		for _, paragraph := range strings.Split(text, "\n") {
			line := ""
			for _, word := range strings.SplitAfter(paragraph, " ") {
				if font.MeasureString(face, strings.TrimRight(word, " ")) > maxWidth {
					return field, "A word exceeds the content region. Add a space or line break, widen the region, or choose a smaller readable font size."
				}
				if font.MeasureString(face, line+word) > maxWidth && line != "" {
					lines = append(lines, strings.TrimRight(line, " "))
					line = word
				} else {
					line += word
				}
			}
			lines = append(lines, strings.TrimRight(line, " "))
		}
		for i, line := range lines {
			bounds, _ := font.BoundString(face, line)
			if bounds.Max.X-bounds.Min.X > maxWidth {
				return field, "Text extends beyond the content region."
			}
			if nextY+metrics.Ascent+metrics.Descent > fixed.I(regionHeight) {
				return field, "Text overflows the content region. Shorten it, enlarge the region or choose a smaller readable font size; text is never truncated or automatically shrunk."
			}
			result.lines = append(result.lines, sceneLine{line, nextY, indent, bullet && i == 0})
			nextY += lineHeight
		}
		return "", ""
	}
	if scene.Layout == "title" || scene.Text != "" {
		if field, message := appendText(scene.Text, "text", 0, false); message != "" {
			return fail(field, message)
		}
		if scene.Layout == "list" {
			nextY += lineHeight / 3
		}
	}
	if scene.Layout == "list" {
		if len(scene.Items) == 0 || len(scene.Items) > MaxListItems {
			return fail("items", "Use between 1 and 20 list items.")
		}
		for i, item := range scene.Items {
			field := fmt.Sprintf("items.%d", i)
			if strings.TrimSpace(item) == "" {
				return fail(field, "Enter text for this list item or remove it.")
			}
			if field, message := appendText(item, field, font.MeasureString(face, "• "), true); message != "" {
				return fail(field, message)
			}
			if i < len(scene.Items)-1 {
				nextY += lineHeight / 4
			}
		}
	}
	if len(result.lines) > 0 {
		result.height = result.lines[len(result.lines)-1].y + metrics.Ascent + metrics.Descent
	}
	return result, "", ""
}

// RenderScene creates the same raster for quick previews and prepared output.
// Font size is defined at 1080p; no system fonts or automatic shrinking are used.
func RenderScene(scene Scene, width, height int) (*image.RGBA, error) {
	layout, _, message := layoutScene(scene, width, height)
	if layout.face != nil {
		defer layout.face.Close()
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
	baseline := fixed.I(layout.region.Min.Y) + (fixed.I(layout.region.Dy())-layout.height)/2 + layout.face.Metrics().Ascent
	drawer := font.Drawer{Dst: img, Src: image.NewUniform(color.RGBA{238, 232, 255, 255}), Face: layout.face}
	for _, line := range layout.lines {
		x := fixed.I(layout.region.Min.X) + line.indent
		spare := fixed.I(layout.region.Dx()) - line.indent - font.MeasureString(layout.face, line.text)
		switch layout.alignment {
		case "center":
			x += spare / 2
		case "right":
			x += spare
		}
		if line.bullet {
			drawer.Dot = fixed.Point26_6{X: x - line.indent, Y: baseline + line.y}
			drawer.DrawString("•")
		}
		drawer.Dot = fixed.Point26_6{X: x, Y: baseline + line.y}
		drawer.DrawString(line.text)
	}
	return img, nil
}
