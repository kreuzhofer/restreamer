package mediaauthor

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"strings"
	"unicode"

	xdraw "golang.org/x/image/draw"
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
	return ValidateWithTheme(d, RetroTheme(1), width, height)
}

func ValidateWithTheme(d Design, theme Theme, width, height int) []Issue {
	issues := make([]Issue, 0)
	if strings.TrimSpace(d.Name) == "" {
		issues = append(issues, Issue{"name", "Name this show design."})
	}
	if d.Stage != "prestream" && d.Stage != "ending" {
		issues = append(issues, Issue{"stage", "Choose prestream or ending."})
	}
	if d.Theme.ID != theme.ID || d.Theme.Revision != theme.Revision {
		issues = append(issues, Issue{"theme", "Choose an available exact theme revision."})
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
		layout, field, message := layoutSceneStyled(scene, width, height, theme.Style)
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
	face        font.Face
	lines       []sceneLine
	region      image.Rectangle
	imageRegion image.Rectangle
	height      fixed.Int26_6
	alignment   string
}

func layoutScene(scene Scene, width, height int) (sceneLayout, string, string) {
	return layoutSceneStyled(scene, width, height, RetroTheme(1).Style)
}

func layoutSceneStyled(scene Scene, width, height int, style Style) (result sceneLayout, field, message string) {
	scene = resolveSceneStyle(scene, style)
	fail := func(field, message string) (sceneLayout, string, string) { return result, field, message }
	if width < 320 || height < 180 || width > 3840 || height > 2160 {
		return fail("text", "Unsupported streaming profile.")
	}
	if scene.Layout != "title" && scene.Layout != "list" && scene.Layout != "text-image" && scene.Layout != "media" {
		return fail("layout", "Choose a title, list, text-image or media layout.")
	}
	if scene.MediaKind != "" && scene.MediaKind != "image" && scene.MediaKind != "video" {
		return fail("media_kind", "Choose image or video media.")
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
		return fail("content_region", "Content width and height must be between 30% and 90% (blank inherits the theme).")
	}
	regionWidth, regionHeight := int(float64(width)*rw/100), int(float64(height)*rh/100)
	offset := 0
	if scene.Layout != "media" {
		offset = int(float64(height) * style.ContentOffsetYPercent / 100)
	}
	result.region = image.Rect((width-regionWidth)/2, (height-regionHeight)/2+offset, (width+regionWidth)/2, (height+regionHeight)/2+offset)
	if !result.region.In(image.Rect(0, 0, width, height)) {
		return fail("content_region", "The content region extends beyond the frame. Reduce its height or vertical offset.")
	}
	if scene.Layout == "text-image" || scene.Layout == "media" {
		if scene.IsVideo() {
			if scene.Video == nil || scene.Video.Asset.ID == "" || scene.Video.Asset.Revision < 1 {
				return fail("video", "Choose an exact video revision.")
			}
		} else if scene.Image == nil || scene.Image.ID == "" || scene.Image.Revision < 1 {
			return fail("image", "Choose an exact image revision for this layout.")
		}
		if scene.Layout == "media" {
			result.imageRegion = result.region
			if scene.ContentRegion.WidthPercent == 0 && scene.ContentRegion.HeightPercent == 0 {
				result.imageRegion = image.Rect(0, 0, width, height)
			}
			return result, "", ""
		}
		result.imageRegion = image.Rect(result.region.Max.X-regionWidth*47/100, result.region.Min.Y, result.region.Max.X, result.region.Max.Y)
		regionWidth = regionWidth * 47 / 100
		result.region.Max.X = result.region.Min.X + regionWidth
	}
	if style.Logo != nil && scene.Layout != "media" && result.region.Overlaps(logoRegion(width, height, style)) {
		return fail("content_region", "The content region overlaps the selected logo band. Reduce its height or choose a smaller logo.")
	}
	f := sansFont
	switch scene.Font {
	case "", "go-sans":
	case "arcade-pixel":
	case "go-mono":
		f = monoFont
	default:
		return fail("font", "Choose Go Sans, Go Mono or Arcade Pixel.")
	}
	size := scene.FontSize
	if size == 0 {
		size = 64
	}
	if math.IsNaN(size) || math.IsInf(size, 0) || size < 24 || size > 120 {
		return fail("font_size", "Use a font size from 24 to 120 (at 1080p).")
	}
	var face font.Face
	var err error
	if scene.Font == "arcade-pixel" {
		face = newPixelFace(size * float64(height) / 1080)
	} else {
		face, err = opentype.NewFace(f, &opentype.FaceOptions{Size: size * float64(height) / 1080, DPI: 72, Hinting: font.HintingNone})
	}
	if err != nil {
		return fail("font", "Cannot load the selected font.")
	}
	result.face = face
	metrics := face.Metrics()
	lineHeight := fixed.Int26_6(float64(metrics.Height) * style.LineSpacingPercent / 100)
	nextY := fixed.Int26_6(0)
	appendText := func(text, field string, indent fixed.Int26_6, bullet bool) (string, string) {
		for _, r := range text {
			if r == '\n' {
				continue
			}
			_, supported := face.GlyphAdvance(r)
			if !supported || unicode.IsControl(r) {
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
				nextY += fixed.Int26_6(float64(lineHeight) * style.ListSpacingPercent / 100)
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
	return RenderSceneWithImage(scene, width, height, nil)
}

// RenderSceneWithImage composes decoded image data, keeping filesystem access
// outside the authoring model. Both preview and generation use this raster.
func RenderSceneWithImage(scene Scene, width, height int, asset image.Image) (*image.RGBA, error) {
	return RenderSceneStyled(scene, width, height, RetroTheme(1).Style, RenderInputs{Image: asset}, 0, 25)
}

func RenderSceneStyled(scene Scene, width, height int, style Style, inputs RenderInputs, frame, fps int) (*image.RGBA, error) {
	layout, _, message := layoutSceneStyled(scene, width, height, style)
	if layout.face != nil {
		defer layout.face.Close()
	}
	if message != "" {
		return nil, fmt.Errorf("%s", message)
	}
	img := ThemeBackdrop(width, height, style, inputs)
	asset := inputs.Image
	if !layout.imageRegion.Empty() {
		if asset == nil || asset.Bounds().Empty() {
			return nil, fmt.Errorf("The selected image revision is unavailable.")
		}
		bounds := asset.Bounds()
		region := layout.imageRegion
		scale := math.Min(float64(region.Dx())/float64(bounds.Dx()), float64(region.Dy())/float64(bounds.Dy()))
		w, h := max(1, int(float64(bounds.Dx())*scale)), max(1, int(float64(bounds.Dy())*scale))
		target := image.Rect(region.Min.X+(region.Dx()-w)/2, region.Min.Y+(region.Dy()-h)/2, region.Min.X+(region.Dx()+w)/2, region.Min.Y+(region.Dy()+h)/2)
		xdraw.ApproxBiLinear.Scale(img, target, asset, bounds, draw.Over, nil)
	}
	if layout.face == nil {
		if inputs.Logo != nil {
			foreground := ThemeForeground(width, height, style, inputs)
			draw.Draw(img, img.Bounds(), foreground, image.Point{}, draw.Over)
		}
		drawEffect(img, style, frame, fps)
		return img, nil
	}
	baseline := fixed.I(layout.region.Min.Y) + (fixed.I(layout.region.Dy())-layout.height)/2 + layout.face.Metrics().Ascent
	textColor, _ := ParseColor(style.TextColor)
	drawer := font.Drawer{Dst: img, Src: image.NewUniform(textColor), Face: layout.face}
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
		dot := fixed.Point26_6{X: x, Y: baseline + line.y}
		if _, pixel := layout.face.(*pixelFace); pixel {
			step := max(1, height/180)
			for _, layer := range []struct {
				offset int
				color  string
			}{{3 * step, "#031632"}, {2 * step, style.AccentColor}, {step, "#c3663c"}} {
				c, _ := ParseColor(layer.color)
				drawer.Src = image.NewUniform(c)
				drawer.Dot = dot.Add(fixed.P(step, layer.offset))
				drawer.DrawString(line.text)
			}
		}
		drawer.Src = image.NewUniform(textColor)
		drawer.Dot = dot
		drawer.DrawString(line.text)
	}
	if inputs.Logo != nil {
		foreground := ThemeForeground(width, height, style, inputs)
		draw.Draw(img, img.Bounds(), foreground, image.Point{}, draw.Over)
	}
	drawEffect(img, style, frame, fps)
	return img, nil
}

// MediaRegion returns the same contained-media region used by image rasterization.
func MediaRegion(scene Scene, width, height int) (image.Rectangle, error) {
	layout, _, message := layoutScene(scene, width, height)
	if layout.face != nil {
		layout.face.Close()
	}
	if message != "" {
		return image.Rectangle{}, fmt.Errorf("%s", message)
	}
	return layout.imageRegion, nil
}
