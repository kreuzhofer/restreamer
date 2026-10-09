// Package mediaauthor defines authored media independently of broadcast playback.
package mediaauthor

// ThemeRef pins appearance independently of scene content and timing.
type ThemeRef struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
}

type Scene struct {
	ID              string  `json:"id"`
	Layout          string  `json:"layout"`
	Text            string  `json:"text"`
	DurationSeconds float64 `json:"duration_seconds"`
	// Empty font and zero font size inherit the selected theme.
	Font     string  `json:"font,omitempty"`
	FontSize float64 `json:"font_size,omitempty"`
}

type Design struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Stage     string   `json:"stage"`
	Version   int      `json:"version"`
	Theme     ThemeRef `json:"theme"`
	Scenes    []Scene  `json:"scenes"`
	UpdatedAt string   `json:"updated_at"`
}

type Issue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}
