package relay

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

const maxAssetUploadBytes = 10 << 20
const maxAssetPixels = 20_000_000
const maxAssetImageBytes = 32 << 20
const maxAssetRevisions = 200

type AssetRevision struct {
	Revision  int    `json:"revision"`
	Digest    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	CreatedAt string `json:"created_at"`
}
type GeneratorAsset struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Revision  int             `json:"revision"`
	Revisions []AssetRevision `json:"revisions"`
}
type AssetUse struct {
	AssetID  string `json:"asset_id"`
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Revision int    `json:"revision"`
}
type assetResponse struct {
	GeneratorAsset
	Uses []AssetUse `json:"uses"`
}

func (g *generatorStore) assetPath(ref mediaauthor.AssetRef) string {
	return filepath.Join(g.assetsRoot, ref.ID, strconv.Itoa(ref.Revision)+".png")
}
func validAssetName(name string) bool {
	if name == "" || !utf8.ValidString(name) || len(name) > 180 || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func (g *generatorStore) loadAssets() error {
	entries, err := os.ReadDir(g.assetsRoot)
	if err != nil {
		return err
	}
	total := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".upload-") || strings.HasPrefix(entry.Name(), ".deleted-") {
			if err := os.RemoveAll(filepath.Join(g.assetsRoot, entry.Name())); err != nil {
				return errors.New("cannot clean interrupted asset upload")
			}
			continue
		}
		if !entry.IsDir() || !validDesignID(entry.Name()) {
			return errors.New("invalid asset storage entry")
		}
		var a GeneratorAsset
		if readMediaJSON(filepath.Join(g.assetsRoot, entry.Name(), "asset.json"), &a) != nil || a.ID != entry.Name() || !validAssetName(a.Name) || a.Kind != "image" || a.Revision != len(a.Revisions) || a.Revision < 1 {
			return errors.New("invalid image asset metadata")
		}
		for i, r := range a.Revisions {
			if r.Revision != i+1 || !validRevisionID(r.Digest) || r.Bytes < 1 || r.Bytes > maxAssetImageBytes || r.Width < 1 || r.Height < 1 || int64(r.Width)*int64(r.Height) > maxAssetPixels {
				return errors.New("invalid image revision metadata")
			}
		}
		total += len(a.Revisions)
		if total > maxAssetRevisions {
			return errors.New("image revision limit exceeded")
		}
		g.assets[a.ID] = a
	}
	return nil
}
func (g *generatorStore) assetRevision(ref mediaauthor.AssetRef) (AssetRevision, bool) {
	a, ok := g.assets[ref.ID]
	if !ok || ref.Revision < 1 || ref.Revision > len(a.Revisions) {
		return AssetRevision{}, false
	}
	return a.Revisions[ref.Revision-1], true
}

// assetIssues is called with g.mu held, including across draft/job publication.
func (g *generatorStore) assetIssues(d mediaauthor.Design) []mediaauthor.Issue {
	out := make([]mediaauthor.Issue, 0)
	for i, scene := range d.Scenes {
		if scene.Image == nil {
			continue
		}
		ref := *scene.Image
		r, ok := g.assetRevision(ref)
		info, err := os.Lstat(g.assetPath(ref))
		if !ok || err != nil || !info.Mode().IsRegular() || info.Size() != r.Bytes {
			out = append(out, mediaauthor.Issue{Field: fmt.Sprintf("scenes.%d.image", i), Message: "The selected image revision is unavailable. Choose an available image revision explicitly."})
		}
	}
	return out
}

// assetUsesLocked intentionally scans persisted owners rather than maintaining
// a second reference registry. New concrete owners (templates/themes/BRB) extend
// this scan when introduced. Keep lock order g.mu then jobsMu.
func (g *generatorStore) assetUsesLocked(id string) ([]AssetUse, error) {
	out := make([]AssetUse, 0)
	designs, err := g.list()
	if err != nil {
		return nil, err
	}
	add := func(d mediaauthor.Design, kind, owner, name string) {
		refs := mediaauthor.DesignAssetRefs(d)
		if d.Theme.ID != "" {
			theme, err := g.resolveTheme(d.Theme)
			if err == nil {
				refs = append(refs, mediaauthor.ThemeAssetRefs(theme)...)
			}
		}
		for _, ref := range refs {
			if id == "" || ref.ID == id {
				out = append(out, AssetUse{AssetID: ref.ID, Kind: kind, ID: owner, Name: name, Revision: ref.Revision})
			}
		}
	}
	for _, theme := range g.themes {
		for _, ref := range mediaauthor.ThemeAssetRefs(theme) {
			if id == "" || ref.ID == id {
				out = append(out, AssetUse{AssetID: ref.ID, Kind: "theme", ID: fmt.Sprintf("%s:%d", theme.ID, theme.Revision), Name: fmt.Sprintf("%s · revision %d", theme.Name, theme.Revision), Revision: ref.Revision})
			}
		}
	}
	for _, d := range designs {
		add(d, "design", d.ID, d.Name)
	}
	templates, err := g.listTemplates()
	if err != nil {
		return nil, err
	}
	for _, template := range templates {
		add(template.Content, "template", template.ID, template.Name)
	}
	g.jobsMu.Lock()
	defer g.jobsMu.Unlock()
	for _, j := range g.jobs {
		add(j.Design, "job", j.ID, j.Design.Name+" · "+j.State)
		if j.MediaRevision != "" {
			add(j.Design, "media", j.MediaRevision, j.Design.Name)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind+out[i].ID < out[j].Kind+out[j].ID })
	return out, nil
}
func (s *Server) assetUsesLocked(id string) ([]AssetUse, error) {
	uses, err := s.generator.assetUsesLocked(id)
	if err != nil {
		return nil, err
	}
	if s.broadcast == nil {
		return uses, nil
	}
	stage := s.broadcast.stageStatus(time.Now())
	selected := s.library.selections()
	for _, u := range append([]AssetUse{}, uses...) {
		if u.Kind != "media" {
			continue
		}
		if stage.Media.Revision == u.ID {
			uses = append(uses, AssetUse{AssetID: u.AssetID, Kind: "on_air", ID: u.ID, Name: u.Name, Revision: u.Revision})
		}
		if stage.ReturnMedia != nil && stage.ReturnMedia.Revision == u.ID {
			uses = append(uses, AssetUse{AssetID: u.AssetID, Kind: "return", ID: u.ID, Name: u.Name, Revision: u.Revision})
		}
		if selected.Prestream == u.ID || selected.Ending == u.ID {
			uses = append(uses, AssetUse{AssetID: u.AssetID, Kind: "selection", ID: u.ID, Name: u.Name, Revision: u.Revision})
		}
	}
	return uses, nil
}

// Open the immutable file under the reference lock, then decode after releasing
// it. This keeps previews consistent even if an unused asset is deleted.
func (s *Server) loadSceneImage(scene mediaauthor.Scene) (image.Image, error) {
	if scene.Layout != "media" && scene.Layout != "text-image" {
		return nil, nil
	}
	if scene.Image == nil {
		return nil, errors.New("Choose an image revision for this scene.")
	}
	return s.loadAssetImage(*scene.Image)
}

func (s *Server) loadAssetImage(ref mediaauthor.AssetRef) (image.Image, error) {
	g := s.generator
	g.mu.Lock()
	meta, ok := g.assetRevision(ref)
	file, err := os.Open(g.assetPath(ref))
	g.mu.Unlock()
	if !ok || err != nil {
		if file != nil {
			file.Close()
		}
		return nil, errors.New("The captured image revision is unavailable.")
	}
	defer file.Close()
	data, err := readImageRevision(file, meta)
	if err != nil {
		return nil, err
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" || config.Width != meta.Width || config.Height != meta.Height {
		return nil, errors.New("Invalid captured image dimensions.")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("Cannot decode the captured image revision.")
	}
	return img, nil
}

type assetLimitWriter struct {
	io.Writer
	remaining int64
}

func (w *assetLimitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, errors.New("Normalized image exceeds 32 MiB; reduce its dimensions.")
	}
	n, err := w.Writer.Write(p)
	w.remaining -= int64(n)
	return n, err
}
func normalizeGeneratorImage(src io.Reader, path string) (AssetRevision, error) {
	data, err := io.ReadAll(io.LimitReader(src, maxAssetUploadBytes+1))
	if err != nil || len(data) > maxAssetUploadBytes {
		return AssetRevision{}, errors.New("Choose an image up to 10 MiB.")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > maxAssetPixels {
		return AssetRevision{}, errors.New("Choose a PNG or JPEG with at most 20 megapixels.")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return AssetRevision{}, errors.New("Cannot decode this image. Export a valid PNG or JPEG.")
	}
	f, err := os.Create(path)
	if err != nil {
		return AssetRevision{}, errors.New("Cannot save image; check storage.")
	}
	err = png.Encode(&assetLimitWriter{f, maxAssetImageBytes}, img)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err != nil || syncErr != nil || closeErr != nil {
		return AssetRevision{}, errors.New("Cannot normalize image within 32 MiB; reduce dimensions or check storage.")
	}
	info, err := os.Stat(path)
	if err != nil {
		return AssetRevision{}, errors.New("Cannot inspect normalized image; check storage.")
	}
	digest, err := mediaDigest(path)
	if err != nil {
		return AssetRevision{}, errors.New("Cannot verify normalized image; check storage.")
	}
	return AssetRevision{Digest: digest, Bytes: info.Size(), Width: config.Width, Height: config.Height, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
}
func assetIdentity() (string, error) {
	var id [16]byte
	_, err := rand.Read(id[:])
	return hex.EncodeToString(id[:]), err
}

func readImageRevision(file *os.File, meta AssetRevision) ([]byte, error) {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != meta.Bytes {
		return nil, errors.New("The image revision is unavailable or changed.")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxAssetImageBytes+1))
	if err != nil || int64(len(data)) != meta.Bytes {
		return nil, errors.New("Cannot read the captured image revision.")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != meta.Digest {
		return nil, errors.New("The captured image revision changed. Upload a new revision and adopt it explicitly.")
	}
	return data, nil
}
