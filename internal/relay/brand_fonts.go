package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"time"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"golang.org/x/image/font/opentype"
)

const maxBrandFontBytes = 4 << 20

func stageBrandFont(r io.Reader, path string) (AssetRevision, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBrandFontBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxBrandFontBytes {
		return AssetRevision{}, errors.New("Font must be an OpenType or TrueType file up to 4 MiB")
	}
	if _, err = opentype.Parse(data); err != nil {
		return AssetRevision{}, errors.New("Cannot parse the supplied OpenType or TrueType font")
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		return AssetRevision{}, err
	}
	sum := sha256.Sum256(data)
	return AssetRevision{Bytes: int64(len(data)), Digest: hex.EncodeToString(sum[:]), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
}

// Caller holds the generator lock. Fonts are parsed per operation, not placed
// in a global registry that could retain deleted or cross-instance brand data.
func (g *generatorStore) resolveBrandFont(style *mediaauthor.Style) error {
	if style.FontAsset == nil {
		return nil
	}
	ref := *style.FontAsset
	meta, ok := g.assetRevision(ref)
	if !ok || g.assets[ref.ID].Kind != "font" || meta.Bytes <= 0 || meta.Bytes > maxBrandFontBytes {
		return errors.New("The selected brand font is unavailable")
	}
	f, err := os.Open(g.assetPath(ref))
	if err != nil {
		return errors.New("Cannot read the selected brand font")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBrandFontBytes+1))
	sum := sha256.Sum256(data)
	if err != nil || int64(len(data)) != meta.Bytes || hex.EncodeToString(sum[:]) != meta.Digest {
		return errors.New("The selected brand font changed; import a new package version")
	}
	style.ResolvedFont, err = opentype.Parse(data)
	if err != nil {
		return errors.New("Cannot parse the selected brand font")
	}
	return nil
}
