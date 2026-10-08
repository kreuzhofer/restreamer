package relay

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

const defaultBRBText = "BE RIGHT BACK"

// Generated from the same pixel font and palette as the Canvas artwork.
//
//go:embed artwork/text.json
var brbTextData []byte

type brbFontDefinition struct {
	Glyphs     map[string][]string `json:"glyphs"`
	Background string              `json:"background"`
	Foreground string              `json:"foreground"`
	Shadow     string              `json:"shadow"`
}

var brbFont = func() brbFontDefinition {
	var font brbFontDefinition
	if err := json.Unmarshal(brbTextData, &font); err != nil {
		panic("invalid bundled BRB font")
	}
	return font
}()

func normalizeBRBText(value string) (string, error) {
	// Reject controls before trimming so a newline cannot silently disappear.
	for _, ch := range value {
		if ch < 32 || ch == 127 {
			return "", errors.New("arcade message must be a single line without control characters")
		}
	}
	value = strings.ToUpper(strings.TrimSpace(value))
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 40 {
		return "", errors.New("arcade message must contain 1–40 characters")
	}
	for _, ch := range value {
		if _, ok := brbFont.Glyphs[string(ch)]; !ok {
			return "", errors.New("arcade message supports A–Z, 0–9, ÄÖÜß, spaces and . , ! ? : ' - / ( ) + &")
		}
	}
	return value, nil
}

func brbColor(hex string) color.RGBA {
	n, _ := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 24)
	return color.RGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 255}
}

// The 44-pixel centre band is reserved for text in every animation scene.
// Render text to pixels, never interpolate user input into FFmpeg arguments.
func brbTextImage(text string) (*image.RGBA, error) {
	text, err := normalizeBRBText(text)
	if err != nil {
		return nil, err
	}
	size := min(3, 296/(utf8.RuneCountInString(text)*6-1))
	img := image.NewRGBA(image.Rect(0, 0, 320, 44))
	draw.Draw(img, img.Bounds(), image.NewUniform(brbColor(brbFont.Background)), image.Point{}, draw.Src)
	top := (44 - 7*size) / 2
	x0 := (320 - (utf8.RuneCountInString(text)*6-1)*size) / 2
	for i, ch := range []rune(text) {
		for y, row := range brbFont.Glyphs[string(ch)] {
			for x, pixel := range row {
				if pixel != '1' {
					continue
				}
				xp, yp := x0+(i*6+x)*size, top+y*size
				draw.Draw(img, image.Rect(xp+1, yp+1, xp+size+1, yp+size+1), image.NewUniform(brbColor(brbFont.Shadow)), image.Point{}, draw.Src)
				draw.Draw(img, image.Rect(xp, yp, xp+size, yp+size), image.NewUniform(brbColor(brbFont.Foreground)), image.Point{}, draw.Src)
			}
		}
	}
	return img, nil
}

func writeBRBPNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = png.Encode(f, img)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func defaultBRBImage(path, text string) error {
	band, err := brbTextImage(text)
	if err != nil {
		return err
	}
	base, err := png.Decode(bytes.NewReader(defaultBRBPoster))
	if err != nil {
		return err
	}
	img := image.NewRGBA(base.Bounds())
	draw.Draw(img, img.Bounds(), base, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 70, 320, 114), band, image.Point{}, draw.Src)
	return writeBRBPNG(path, img)
}
