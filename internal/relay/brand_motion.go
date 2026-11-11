package relay

import (
	"errors"
	"io"
	"math"
	"os"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func (g *generatorStore) checkBrandMotion(style mediaauthor.Style) error {
	if style.Motion == nil {
		return nil
	}
	m := style.Motion
	meta, ok := g.assetRevision(m.Asset)
	if !ok || g.assets[m.Asset.ID].Kind != "video" || meta.Bytes > 64<<20 || math.Abs(meta.DurationSeconds-float64(m.CycleSeconds)) > 0.04 {
		return errors.New("Motion requires an available video up to 64 MiB matching its whole-second cycle")
	}
	return nil
}

func (s *Server) preparedThemeArtwork(style mediaauthor.Style, stage string) (preparedArtwork, bool) {
	if style.Motion == nil {
		return themeArtworkForStage(style.Artwork, stage)
	}
	g := s.generator
	g.mu.Lock()
	if g.checkBrandMotion(style) != nil {
		g.mu.Unlock()
		return preparedArtwork{}, false
	}
	ref := style.Motion.Asset
	meta, _ := g.assetRevision(ref)
	f, err := os.Open(g.assetPath(ref))
	g.mu.Unlock()
	if err != nil {
		return preparedArtwork{}, false
	}
	defer f.Close()
	if verifyVideoRevision(f, meta) != nil {
		return preparedArtwork{}, false
	}
	data, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
	if err != nil || len(data) > 64<<20 {
		return preparedArtwork{}, false
	}
	// Source audio is deliberately excluded by the renderer's video-only map.
	return preparedArtwork{loop: data, seconds: style.Motion.CycleSeconds}, true
}
