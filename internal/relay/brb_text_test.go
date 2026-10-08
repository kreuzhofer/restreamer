package relay

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
)

func TestBRBTextValidationAndLayout(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"  be right back  ", "BE RIGHT BACK"},
		{"bin gleich zurück!", "BIN GLEICH ZURÜCK!"},
		{"pause 2: let's go!", "PAUSE 2: LET'S GO!"},
	} {
		got, err := normalizeBRBText(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("%q: %q %v", tc.input, got, err)
		}
	}
	for _, input := range []string{"", "  ", strings.Repeat("A", 41), "hello\nworld", "hello\x00", "hello 🎮", "%{secret}", "<script>"} {
		if _, err := normalizeBRBText(input); err == nil {
			t.Fatalf("accepted invalid text %q", input)
		}
	}
	for _, input := range []string{"A", "BE RIGHT BACK", strings.Repeat("W", 40), "TAKING A BREAK BACK IN FIVE MINUTES"} {
		img, err := brbTextImage(input)
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 320 || img.Bounds().Dy() != 44 {
			t.Fatal("wrong overlay dimensions")
		}
		// No glyph may touch the band edges or get clipped by the screen.
		background := img.RGBAAt(0, 0)
		for y := 0; y < 44; y++ {
			if img.RGBAAt(0, y) != background || img.RGBAAt(319, y) != background {
				t.Fatal("text clipped horizontally")
			}
		}
		for x := 0; x < 320; x++ {
			if img.RGBAAt(x, 0) != background || img.RGBAAt(x, 43) != background {
				t.Fatal("text clipped vertically")
			}
		}
	}
}

func TestBRBTextAutoSizesOnOneLine(t *testing.T) {
	for _, tc := range []struct{ length, height int }{{1, 21}, {16, 21}, {17, 14}, {24, 14}, {25, 7}, {40, 7}} {
		img, err := brbTextImage(strings.Repeat("W", tc.length))
		if err != nil {
			t.Fatal(err)
		}
		minY, maxY := 44, -1
		for y := 0; y < 44; y++ {
			for x := 0; x < 320; x++ {
				if img.RGBAAt(x, y) == brbColor(brbFont.Foreground) {
					minY = min(minY, y)
					maxY = max(maxY, y)
				}
			}
		}
		if maxY-minY+1 != tc.height {
			t.Fatalf("%d characters: height %d, want one line of %d", tc.length, maxY-minY+1, tc.height)
		}
	}
}

func TestBRBTextIsEncodedIntoVideo(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := encodeBRB(ctx, config.BRBProfile{Width: 320, Height: 180, FPS: 25, SampleRate: 48000}, dir, false, false, 50, "I")
	if err != nil {
		t.Fatal(err)
	}
	data, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-i", filepath.Join(dir, "video.flv"), "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 320*180*3 {
		t.Fatal("wrong decoded frame size")
	}
	expected, err := brbTextImage("I")
	if err != nil {
		t.Fatal(err)
	}
	differences, lit := 0, 0
	for y := 0; y < 44; y++ {
		for x := 0; x < 320; x++ {
			i := ((y+70)*320 + x) * 3
			got := data[i] > 170 && data[i+1] > 170 && data[i+2] > 170
			want := expected.RGBAAt(x, y) == brbColor(brbFont.Foreground)
			if got != want {
				differences++
			}
			if want {
				lit++
			}
		}
	}
	if differences > lit/10 {
		t.Fatalf("decoded message does not match custom text: %d differences for %d pixels", differences, lit)
	}
}

func TestBRBTextPosterUpdatesWithoutChangingAnimationLanes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.png")
	if err := defaultBRBImage(path, "GAME PAUSED!"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	base, err := png.Decode(bytes.NewReader(defaultBRBPoster))
	if err != nil {
		t.Fatal(err)
	}
	changed := 0
	for y := 0; y < 180; y++ {
		for x := 0; x < 320; x++ {
			if got.At(x, y) != base.At(x, y) {
				// Compare colour components because decoded image types may differ.
				r, g, b, a := got.At(x, y).RGBA()
				br, bg, bb, ba := base.At(x, y).RGBA()
				if r != br || g != bg || b != bb || a != ba {
					if y < 70 || y >= 114 {
						t.Fatal("changed animation lane")
					}
					changed++
				}
			}
		}
	}
	if changed == 0 {
		t.Fatal("message was not rendered")
	}
}
