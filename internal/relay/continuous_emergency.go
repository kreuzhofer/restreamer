package relay

import (
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Independent of uploaded/prepared media and FFmpeg decoders. A small grayscale
// slate expands exactly into the fixed YUV output profile, with silent PCM.
func continuousEmergencyFrame() *programFrame {
	small := image.NewGray(image.Rect(0, 0, 480, 270))
	draw.Draw(small, small.Bounds(), image.NewUniform(color.Gray{Y: 24}), image.Point{}, draw.Src)
	text := func(y int, value string) {
		drawer := font.Drawer{Dst: small, Src: image.NewUniform(color.Gray{Y: 220}), Face: basicfont.Face7x13}
		drawer.Dot = fixed.P((480-drawer.MeasureString(value).Ceil())/2, y)
		drawer.DrawString(value)
	}
	text(125, "BE RIGHT BACK")
	text(150, "Waiting for the broadcast source")
	frame := make([]byte, continuousFrameBytes)
	for y := 0; y < 1080; y++ {
		for x := 0; x < 1920; x++ {
			frame[y*1920+x] = 16 + uint8(int(small.GrayAt(x/4, y/4).Y)*219/255)
		}
	}
	for i := 1920 * 1080; i < len(frame); i++ {
		frame[i] = 128
	}
	return &programFrame{video: frame, audio: make([]byte, continuousAudioBytes)}
}
