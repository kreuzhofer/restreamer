package mediaauthor

import (
	_ "embed"
	"encoding/json"
	"image"
	"math"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// The hand-drawn BRB alphabet, with a list bullet and typographic apostrophe.
// Lowercase intentionally uses uppercase arcade glyphs; unsupported text is rejected.
//
//go:embed pixel-font.json
var pixelFontData []byte
var pixelGlyphs = func() map[string][]string {
	var glyphs map[string][]string
	if err := json.Unmarshal(pixelFontData, &glyphs); err != nil {
		panic(err)
	}
	return glyphs
}()

type pixelFace struct {
	step  int
	masks map[rune]*image.Alpha
}

func newPixelFace(size float64) *pixelFace {
	return &pixelFace{max(1, int(math.Floor(size/8))), make(map[rune]*image.Alpha)}
}
func (f *pixelFace) Close() error                 { return nil }
func (f *pixelFace) Kern(_, _ rune) fixed.Int26_6 { return 0 }
func (f *pixelFace) shadowStep() int              { return max(1, f.step*2/5) }
func (f *pixelFace) Metrics() font.Metrics {
	descent := f.step/2 + 3*f.shadowStep()
	return font.Metrics{Height: fixed.I(7*f.step + descent), Ascent: fixed.I(7 * f.step), Descent: fixed.I(descent), XHeight: fixed.I(7 * f.step), CapHeight: fixed.I(7 * f.step)}
}
func pixelRune(r rune) rune {
	if r == 'ß' {
		return r
	}
	return unicode.ToUpper(r)
}
func (f *pixelFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	_, ok := pixelGlyphs[string(pixelRune(r))]
	return fixed.I(6 * f.step), ok
}
func (f *pixelFace) GlyphBounds(r rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	advance, ok := f.GlyphAdvance(r)
	return fixed.Rectangle26_6{Min: fixed.P(0, -7*f.step), Max: fixed.P(5*f.step+f.step/2+f.shadowStep(), f.step/2+3*f.shadowStep())}, advance, ok
}
func (f *pixelFace) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	r = pixelRune(r)
	advance, ok := f.GlyphAdvance(r)
	if !ok {
		return image.Rectangle{}, nil, image.Point{}, advance, false
	}
	mask := f.masks[r]
	if mask == nil {
		mask = image.NewAlpha(image.Rect(0, 0, 5*f.step+f.step/2, 7*f.step+f.step/2))
		for y, row := range pixelGlyphs[string(r)] {
			for x, v := range row {
				if v == '1' {
					for dy := 0; dy < f.step+f.step/2; dy++ {
						for dx := 0; dx < f.step+f.step/2; dx++ {
							mask.Pix[(y*f.step+dy)*mask.Stride+x*f.step+dx] = 255
						}
					}
				}
			}
		}
		f.masks[r] = mask
	}
	rect := mask.Bounds().Add(image.Pt(dot.X.Round(), dot.Y.Round()-7*f.step))
	return rect, mask, image.Point{}, advance, true
}
