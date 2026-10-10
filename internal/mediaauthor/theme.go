package mediaauthor

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
)

type Theme struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Name     string `json:"name"`
	Builtin  bool   `json:"builtin"`
	Style    Style  `json:"style"`
}

// Style contains appearance only. Scene overrides remain in the unchanged draft.
type Style struct {
	Artwork               string        `json:"artwork,omitempty"`
	ContentOffsetYPercent float64       `json:"content_offset_y_percent,omitempty"`
	Font                  string        `json:"font"`
	FontSize              float64       `json:"font_size"`
	BackgroundColor       string        `json:"background_color"`
	TextEdgeColor         string        `json:"text_edge_color,omitempty"`
	TextColor             string        `json:"text_color"`
	AccentColor           string        `json:"accent_color"`
	Background            *AssetRef     `json:"background,omitempty"`
	Logo                  *AssetRef     `json:"logo,omitempty"`
	LogoPosition          string        `json:"logo_position"`
	LogoHeightPercent     float64       `json:"logo_height_percent"`
	ContentRegion         ContentRegion `json:"content_region"`
	LineSpacingPercent    float64       `json:"line_spacing_percent"`
	ListSpacingPercent    float64       `json:"list_spacing_percent"`
	BorderStyle           string        `json:"border_style"`
	BorderWidth           float64       `json:"border_width"`
	Effect                string        `json:"effect"`
	EffectSpeed           int           `json:"effect_speed"`
}

type RenderInputs struct {
	Image, Background, Logo image.Image
	// TransparentBackdrop omits only the background, preserving borders and content.
	TransparentBackdrop bool
}

// Retro revision 1 reproduces the appearance shipped before editable themes.
func RetroTheme(revision int) Theme {
	style := Style{Font: "go-sans", FontSize: 64, BackgroundColor: "#0f1223", TextColor: "#eee8ff", AccentColor: "#3fe0d0", LogoPosition: "top-right", LogoHeightPercent: 6, ContentRegion: ContentRegion{80, 80}, LineSpacingPercent: 120, ListSpacingPercent: 25, BorderStyle: "line", BorderWidth: 6, Effect: "none", EffectSpeed: 8}
	if revision == 2 {
		style.BackgroundColor = "#080e20"
		style.TextColor = "#dcf1ea"
		style.AccentColor = "#ffdb46"
		style.BorderStyle = "pixel"
		style.Effect = "pixel-trail"
	}
	return Theme{ID: "retro", Revision: revision, Name: "Retro", Builtin: true, Style: style}
}

// ArcadeAfterHoursTheme is independently versioned; Retro revisions stay unchanged.
func ArcadeAfterHoursTheme() Theme {
	s := RetroTheme(1).Style
	s.Font, s.FontSize = "arcade-pixel", 120
	s.BackgroundColor, s.TextColor, s.AccentColor = "#02102f", "#fff3cf", "#18abef"
	s.Artwork, s.Effect = "arcade-after-hours", "arcade-palette"
	s.BorderStyle = "none"
	s.ContentRegion = ContentRegion{56, 36}
	s.ContentOffsetYPercent = -15
	return Theme{ID: "arcade-after-hours", Revision: 1, Name: "Arcade After Hours", Builtin: true, Style: s}
}

// NeonNightTheme pins its own city artwork and cool pixel lettering.
func NeonNightTheme() Theme {
	s := ArcadeAfterHoursTheme().Style
	s.BackgroundColor, s.TextColor, s.AccentColor = "#030522", "#c8fff4", "#4815d9"
	s.TextEdgeColor = "#ef38ce"
	s.Artwork, s.Effect = "neon-night", "neon-palette"
	s.ContentRegion = ContentRegion{62, 36}
	s.ContentOffsetYPercent = -17
	return Theme{ID: "neon-night", Revision: 1, Name: "Neon Night", Builtin: true, Style: s}
}
func BuiltinThemes() []Theme {
	return []Theme{RetroTheme(1), RetroTheme(2), ArcadeAfterHoursTheme(), NeonNightTheme()}
}
func IsBuiltinTheme(id string) bool {
	for _, t := range BuiltinThemes() {
		if t.ID == id {
			return true
		}
	}
	return false
}
func ThemeAssetRefs(t Theme) []AssetRef {
	refs := make([]AssetRef, 0, 2)
	if t.Style.Background != nil {
		refs = append(refs, *t.Style.Background)
	}
	if t.Style.Logo != nil && (len(refs) == 0 || refs[0] != *t.Style.Logo) {
		refs = append(refs, *t.Style.Logo)
	}
	return refs
}
func validRange(n, low, high float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= low && n <= high
}
func ParseColor(value string) (color.RGBA, error) {
	if len(value) != 7 || value[0] != '#' {
		return color.RGBA{}, fmt.Errorf("use a six-digit #RRGGBB color")
	}
	n, err := strconv.ParseUint(value[1:], 16, 24)
	if err != nil {
		return color.RGBA{}, fmt.Errorf("use a six-digit #RRGGBB color")
	}
	return color.RGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 255}, nil
}
func ValidateStyle(s Style) []Issue {
	issues := make([]Issue, 0)
	add := func(field, message string) { issues = append(issues, Issue{"style." + field, message}) }
	if s.Artwork != "" && s.Artwork != "arcade-after-hours" && s.Artwork != "neon-night" {
		add("artwork", "Choose an available built-in artwork.")
	}
	if s.Artwork != "" && s.Background != nil {
		add("background", "Choose either built-in artwork or an uploaded background image.")
	}
	if s.Effect == "arcade-palette" && s.Artwork != "arcade-after-hours" {
		add("effect", "Arcade palette animation requires Arcade After Hours artwork.")
	}
	if s.Effect == "neon-palette" && s.Artwork != "neon-night" {
		add("effect", "Neon palette animation requires Neon Night artwork.")
	}
	if s.TextEdgeColor != "" {
		if _, err := ParseColor(s.TextEdgeColor); err != nil {
			add("text_edge_color", "Use a six-digit #RRGGBB color.")
		}
	}
	if !validRange(s.ContentOffsetYPercent, -20, 20) {
		add("content_offset_y_percent", "Use a vertical offset from −20 to 20%.")
	}
	if s.ContentRegion.HeightPercent/2+math.Abs(s.ContentOffsetYPercent) > 50 {
		add("content_region", "The content region must remain within the frame.")
	}
	if s.Font != "go-sans" && s.Font != "go-mono" && s.Font != "arcade-pixel" {
		add("font", "Choose Go Sans, Go Mono or Arcade Pixel.")
	}
	for field, v := range map[string]string{"background_color": s.BackgroundColor, "text_color": s.TextColor, "accent_color": s.AccentColor} {
		if _, err := ParseColor(v); err != nil {
			add(field, "Use a six-digit #RRGGBB color.")
		}
	}
	if !validRange(s.FontSize, 24, 120) {
		add("font_size", "Use 24–120 at 1080p.")
	}
	if !validRange(s.ContentRegion.WidthPercent, 30, 90) || !validRange(s.ContentRegion.HeightPercent, 30, 90) {
		add("content_region", "Content width and height must be 30–90%.")
	}
	if !validRange(s.LineSpacingPercent, 100, 180) {
		add("line_spacing_percent", "Use 100–180% line spacing.")
	}
	if !validRange(s.ListSpacingPercent, 0, 100) {
		add("list_spacing_percent", "Use 0–100% item spacing.")
	}
	if s.BorderStyle != "none" && s.BorderStyle != "line" && s.BorderStyle != "pixel" {
		add("border_style", "Choose none, line or pixel border.")
	}
	if !validRange(s.BorderWidth, 1, 12) {
		add("border_width", "Use 1–12 at 1080p.")
	}
	if s.Effect != "none" && s.Effect != "pixel-trail" && s.Effect != "arcade-palette" && s.Effect != "neon-palette" {
		add("effect", "Choose none, pixel-trail, arcade-palette or neon-palette.")
	}
	if s.EffectSpeed < 1 || s.EffectSpeed > 12 {
		add("effect_speed", "Use 1–12 pixel steps per second.")
	}
	if s.LogoPosition != "top-left" && s.LogoPosition != "top-right" && s.LogoPosition != "bottom-left" && s.LogoPosition != "bottom-right" {
		add("logo_position", "Choose a top or bottom corner.")
	}
	if !validRange(s.LogoHeightPercent, 2, 8) {
		add("logo_height_percent", "Use a logo band height of 2–8%.")
	}
	return issues
}
func resolveSceneStyle(scene Scene, s Style) Scene {
	if scene.Font == "" {
		scene.Font = s.Font
	}
	if scene.FontSize == 0 {
		scene.FontSize = s.FontSize
	}
	// A full-screen media layout remains full-screen unless explicitly inset.
	if scene.Layout != "media" {
		if scene.ContentRegion.WidthPercent == 0 {
			scene.ContentRegion.WidthPercent = s.ContentRegion.WidthPercent
		}
		if scene.ContentRegion.HeightPercent == 0 {
			scene.ContentRegion.HeightPercent = s.ContentRegion.HeightPercent
		}
	}
	return scene
}
func logoRegion(width, height int, s Style) image.Rectangle {
	w, h := width/5, int(float64(height)*s.LogoHeightPercent/100)
	x, y := width/20, height/100
	if strings.HasSuffix(s.LogoPosition, "right") {
		x = width - width/20 - w
	}
	if strings.HasPrefix(s.LogoPosition, "bottom") {
		y = height - height/100 - h
	}
	return image.Rect(x, y, x+w, y+h)
}
func fitImage(dst draw.Image, region image.Rectangle, src image.Image) {
	if src == nil || src.Bounds().Empty() || region.Empty() {
		return
	}
	bounds := src.Bounds()
	scale := math.Min(float64(region.Dx())/float64(bounds.Dx()), float64(region.Dy())/float64(bounds.Dy()))
	w, h := max(1, int(float64(bounds.Dx())*scale)), max(1, int(float64(bounds.Dy())*scale))
	target := image.Rect(region.Min.X+(region.Dx()-w)/2, region.Min.Y+(region.Dy()-h)/2, region.Min.X+(region.Dx()+w)/2, region.Min.Y+(region.Dy()+h)/2)
	xdraw.ApproxBiLinear.Scale(dst, target, src, bounds, draw.Over, nil)
}

// ThemeBackdrop and ThemeForeground also compose normalized video/BRB frames.
func ThemeBackdrop(width, height int, s Style, inputs RenderInputs) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	bg, _ := ParseColor(s.BackgroundColor)
	accent, _ := ParseColor(s.AccentColor)
	if !inputs.TransparentBackdrop {
		draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
		fitImage(img, img.Bounds(), inputs.Background)
	}
	margin := height / 24
	thickness := max(2, int(s.BorderWidth*float64(height)/1080))
	if s.BorderStyle != "none" {
		for _, rect := range []image.Rectangle{image.Rect(margin, margin, width-margin, margin+thickness), image.Rect(margin, height-margin-thickness, width-margin, height-margin), image.Rect(margin, margin, margin+thickness, height-margin), image.Rect(width-margin-thickness, margin, width-margin, height-margin)} {
			draw.Draw(img, rect, image.NewUniform(accent), image.Point{}, draw.Src)
		}
		if s.BorderStyle == "pixel" {
			size := max(4, height/54)
			for _, p := range []image.Point{{margin, margin}, {width - margin - size, margin}, {margin, height - margin - size}, {width - margin - size, height - margin - size}} {
				draw.Draw(img, image.Rect(p.X, p.Y, p.X+size, p.Y+size), image.NewUniform(accent), image.Point{}, draw.Src)
			}
		}
	}
	return img
}
func ThemeForeground(width, height int, s Style, inputs RenderInputs) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	fitImage(img, logoRegion(width, height, s), inputs.Logo)
	return img
}

// EffectSprite and EffectPosition share integer geometry with the FFmpeg overlay.
func EffectSprite(height int, s Style) *image.RGBA {
	size := max(2, height/180)
	img := image.NewRGBA(image.Rect(0, 0, size*11, size*2))
	c, _ := ParseColor(s.AccentColor)
	for i := 0; i < 4; i++ {
		v := color.NRGBA{R: c.R, G: c.G, B: c.B, A: uint8(80 + i*55)}
		draw.Draw(img, image.Rect(i*size*3, 0, i*size*3+size*2, size*2), image.NewUniform(v), image.Point{}, draw.Src)
	}
	return img
}
func EffectGeometry(width, height int) (start, y, step, slots int) {
	step = max(2, height/180)
	start = height / 24
	y = height - height/40 - step*2
	slots = max(1, (width-start*2-step*11)/step+1)
	return
}
func EffectPosition(frame, fps, width, height int, s Style) image.Point {
	start, y, step, slots := EffectGeometry(width, height)
	phase := 0
	if fps > 0 {
		phase = frame * s.EffectSpeed / fps
	}
	return image.Pt(start+(phase%slots)*step, y)
}
func drawEffect(img *image.RGBA, s Style, frame, fps int) {
	if s.Effect != "pixel-trail" || frame < 0 {
		return
	}
	sprite := EffectSprite(img.Bounds().Dy(), s)
	p := EffectPosition(frame, fps, img.Bounds().Dx(), img.Bounds().Dy(), s)
	draw.Draw(img, sprite.Bounds().Add(p), sprite, image.Point{}, draw.Over)
}
