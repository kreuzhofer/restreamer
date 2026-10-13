package relay

import (
	"bytes"
	"embed"
	"image"
	"image/draw"
	"image/png"
	"net/http"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	xdraw "golang.org/x/image/draw"
)

// Each frame is independently designed with a transparent, irregular opening.
// These are not cutouts from the scene backgrounds.
//
//go:embed artwork/obs-frames/*.png
var obsFrames embed.FS

func (s *Server) generatorThemePreviewHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var input struct {
		Style mediaauthor.Style `json:"style"`
		View  string            `json:"view"`
	}
	if !generatorDecode(w, r, &input) {
		return
	}
	if input.View != "border" && input.View != "prestream" && input.View != "agenda" && input.View != "brb" && input.View != "ending" {
		http.Error(w, "Choose a scene sample or OBS frame", 400)
		return
	}
	g := s.generator
	if !g.previewMu.TryLock() {
		http.Error(w, "Another preview is rendering; retry shortly", 429)
		return
	}
	defer g.previewMu.Unlock()
	g.mu.Lock()
	issues := append(mediaauthor.ValidateStyle(input.Style), g.themeStyleAssetIssues(input.Style)...)
	g.mu.Unlock()
	if len(issues) > 0 {
		generatorJSON(w, 422, map[string]any{"issues": issues})
		return
	}
	const width, height = 1920, 1080
	var img image.Image
	if input.View == "border" {
		// ValidateStyle restricts Artwork to known identifiers; embed also bounds IO.
		data, err := obsFrames.ReadFile("artwork/obs-frames/" + input.Style.Artwork + ".png")
		if err != nil {
			http.Error(w, "Choose built-in artwork to download its purpose-designed OBS frame", 422)
			return
		}
		src, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			http.Error(w, "Cannot decode OBS frame", 500)
			return
		}
		dst := image.NewNRGBA(image.Rect(0, 0, width, height))
		xdraw.NearestNeighbor.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
		// Generated PNGs can contain invisible 1/255 alpha speckles in the empty
		// canvas. Quantize only that noise to clean transparency for compositing.
		for i := 0; i < len(dst.Pix); i += 4 {
			if dst.Pix[i+3] <= 1 {
				clear(dst.Pix[i : i+4])
			}
		}
		img = dst
	} else {
		stage := input.View
		scene := mediaauthor.Scene{Layout: "title", Text: "STARTING SOON", DurationSeconds: 10}
		switch input.View {
		case "agenda":
			stage = "prestream"
			scene.Layout = "list"
			scene.Text = "TODAY"
			scene.Items = []string{"CREATE", "EXPLORE"}
		case "brb":
			scene.Text = "BE RIGHT BACK"
		case "ending":
			scene.Text = "THANK YOU"
		}
		inputs, err := s.loadThemeInputs(mediaauthor.Theme{Style: input.Style}, width, height, stage)
		if err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
		img, err = mediaauthor.RenderSceneStyled(scene, width, height, input.Style, inputs, 0, 30)
		if err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		http.Error(w, "Cannot encode theme preview", 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", `attachment; filename="`+input.View+`.png"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data.Bytes())
}
