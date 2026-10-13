package mediaauthor

import "testing"

func TestPacmanMazeIsSelectableAndFitsItsContainedSign(t *testing.T) {
	var theme Theme
	for _, candidate := range BuiltinThemes() {
		if candidate.ID == "pacman-maze" {
			theme = candidate
		}
	}
	if theme.ID == "" {
		t.Fatal("Pac-Man Maze is not selectable")
	}
	if issues := ValidateStyle(theme.Style); len(issues) != 0 {
		t.Fatal(issues)
	}
	for _, size := range [][2]int{{320, 180}, {640, 360}, {1920, 1080}, {640, 480}} {
		scene := Scene{Layout: "title", Text: "BE RIGHT BACK"}
		layout, _, message := layoutSceneStyled(scene, size[0], size[1], theme.Style)
		if message != "" {
			t.Fatalf("%v: %s", size, message)
		}
		layout.face.Close()
		if layout.region.Dx() > size[0]*40/100 {
			t.Fatalf("text spills beyond the maze sign: %v", layout.region)
		}
	}
}
