package relay

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func TestNeonThemeVariantsValidateArtworkAndCanFreezeAnimation(t *testing.T) {
	s := arcadeThemeServer(t)
	create := func(style mediaauthor.Style) (int, []byte) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"name": "Neon variant", "base": mediaauthor.ThemeRef{ID: "neon-night", Revision: 1}, "style": style})
		w := dashboardRequest(s, "POST", "/api/generator/themes", string(body))
		return w.Code, w.Body.Bytes()
	}
	for _, tc := range []struct {
		name, field string
		change      func(*mediaauthor.Style)
	}{
		{"mismatched artwork", "style.effect", func(s *mediaauthor.Style) { s.Artwork = "arcade-after-hours" }},
		{"missing artwork", "style.effect", func(s *mediaauthor.Style) { s.Artwork = "" }},
		{"invalid edge color", "style.text_edge_color", func(s *mediaauthor.Style) { s.TextEdgeColor = "purple" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			style := mediaauthor.NeonNightTheme().Style
			tc.change(&style)
			code, body := create(style)
			if code != 422 || !strings.Contains(string(body), tc.field) {
				t.Fatalf("invalid style accepted: %d %s", code, body)
			}
		})
	}
	style := mediaauthor.NeonNightTheme().Style
	style.Effect = "none"
	code, body := create(style)
	if code != 201 {
		t.Fatalf("create: %d %s", code, body)
	}
	var theme mediaauthor.Theme
	if err := json.Unmarshal(body, &theme); err != nil {
		t.Fatal(err)
	}
	d := generatorDraft(t, s, 16)
	d.Theme = mediaauthor.ThemeRef{ID: theme.ID, Revision: theme.Revision}
	d.Scenes[0].Text = "NEON NIGHT"
	raw, _ := json.Marshal(d)
	a := dashboardRequest(s, "POST", "/api/generator/preview?frame=0", string(raw))
	b := dashboardRequest(s, "POST", "/api/generator/preview?frame=100", string(raw))
	if a.Code != 200 || b.Code != 200 || !bytes.Equal(a.Body.Bytes(), b.Body.Bytes()) {
		t.Fatal("disabled animation did not stay still", a.Code, b.Code)
	}
	style.TextEdgeColor = "#ffbb11"
	code, body = create(style)
	if code != 201 {
		t.Fatalf("edge variant: %d %s", code, body)
	}
	json.Unmarshal(body, &theme)
	d.Theme = mediaauthor.ThemeRef{ID: theme.ID, Revision: theme.Revision}
	raw, _ = json.Marshal(d)
	c := dashboardRequest(s, "POST", "/api/generator/preview", string(raw))
	if c.Code != 200 || bytes.Equal(a.Body.Bytes(), c.Body.Bytes()) {
		t.Fatal("pixel edge color was not applied", c.Code)
	}
}
