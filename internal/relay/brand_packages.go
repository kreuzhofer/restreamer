package relay

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxBrandArchiveBytes int64 = 512 << 20
const maxBrandManifestBytes = 256 << 10

var brandNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type brandManifest struct {
	FormatVersion int             `json:"format_version"`
	ID            string          `json:"id"`
	Version       int             `json:"version"`
	Name          string          `json:"name"`
	Notes         string          `json:"notes,omitempty"`
	Assets        []brandAsset    `json:"assets"`
	Themes        []brandTheme    `json:"themes"`
	Templates     []brandTemplate `json:"templates"`
}
type brandAsset struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Digest string `json:"sha256"`
	Source string `json:"source,omitempty"`
}
type brandTheme struct {
	ID    string            `json:"id"`
	Name  string            `json:"name"`
	Style mediaauthor.Style `json:"style"`
}
type brandTemplate struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Theme   string             `json:"theme"`
	Content mediaauthor.Design `json:"content"`
}
type brandPackage struct {
	brandManifest
	Digest     string `json:"sha256"`
	ImportedAt string `json:"imported_at,omitempty"`
}

func (g *generatorStore) packagesRoot() string {
	return filepath.Join(filepath.Dir(g.root), "packages")
}
func (g *generatorStore) listBrandPackages() ([]brandPackage, error) {
	out := make([]brandPackage, 0)
	files, err := os.ReadDir(g.packagesRoot())
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".") || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		var p brandPackage
		if readBrandJSON(filepath.Join(g.packagesRoot(), f.Name()), &p) != nil {
			return nil, errors.New("cannot read saved brand package")
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == out[j].ID {
			return out[i].Version < out[j].Version
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
func readBrandArchive(filename string) (brandPackage, error) {
	var p brandPackage
	z, err := zip.OpenReader(filename)
	if err != nil {
		return p, errors.New("Choose a valid brand package ZIP")
	}
	defer z.Close()
	if len(z.File) > 256 {
		return p, errors.New("Package exceeds 256 files")
	}
	seen := map[string]bool{}
	var total uint64
	var manifest []byte
	for _, f := range z.File {
		if f.Name == "" || strings.Contains(f.Name, "\\") || path.Clean(f.Name) != f.Name || strings.HasPrefix(f.Name, "/") || strings.HasPrefix(f.Name, "../") || strings.Contains(f.Name, ":") || !f.Mode().IsRegular() || seen[f.Name] {
			return p, errors.New("Package contains unsafe or duplicate paths")
		}
		seen[f.Name] = true
		if f.UncompressedSize64 > uint64(maxBrandArchiveBytes) || total > uint64(maxBrandArchiveBytes)-f.UncompressedSize64 {
			return p, errors.New("Expanded package exceeds 512 MiB")
		}
		total += f.UncompressedSize64
		if f.Name != "manifest.json" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return p, errors.New("Cannot read package file")
		}
		manifest, err = io.ReadAll(io.LimitReader(r, maxBrandManifestBytes+1))
		r.Close()
		if err != nil || len(manifest) > maxBrandManifestBytes {
			return p, errors.New("Manifest is corrupt or exceeds 256 KiB")
		}
	}
	dec := json.NewDecoder(bytes.NewReader(manifest))
	dec.DisallowUnknownFields()
	if dec.Decode(&p.brandManifest) != nil || dec.Decode(new(any)) != io.EOF {
		return p, errors.New("Manifest must contain supported JSON fields")
	}
	if p.FormatVersion != 1 || !brandNamePattern.MatchString(p.ID) || p.Version < 1 || p.Version > 1000000 || !validAssetName(p.Name) || len(p.Notes) > 4096 {
		return p, errors.New("Unsupported format or invalid package identity, version or name")
	}
	declared := map[string]bool{"manifest.json": true}
	ids := map[string]bool{}
	for _, a := range p.Assets {
		if !brandNamePattern.MatchString(a.ID) || ids[a.ID] || !validAssetName(a.Name) || len(a.Source) > 2048 || !validRevisionID(a.Digest) || !seen[a.Path] || declared[a.Path] || (a.Kind != "image" && a.Kind != "font" && a.Kind != "video" && a.Kind != "audio") {
			return p, errors.New("Invalid asset identity, checksum, path or kind")
		}
		ids[a.ID] = true
		declared[a.Path] = true
	}
	for name := range seen {
		if !declared[name] {
			return p, errors.New("Package contains an undeclared file")
		}
	}
	ids = map[string]bool{}
	for _, t := range p.Themes {
		if !brandNamePattern.MatchString(t.ID) || ids[t.ID] || !validThemeName(t.Name) || len(mediaauthor.ValidateStyle(t.Style)) > 0 {
			return p, errors.New("Invalid theme identity or style")
		}
		ids[t.ID] = true
	}
	templateIDs := map[string]bool{}
	for _, t := range p.Templates {
		if !brandNamePattern.MatchString(t.ID) || templateIDs[t.ID] || !validAssetName(t.Name) || !ids[t.Theme] {
			return p, errors.New("Invalid template identity or theme")
		}
		templateIDs[t.ID] = true
	}
	canonical, _ := json.Marshal(p.brandManifest)
	digest := sha256.Sum256(canonical)
	p.Digest = hex.EncodeToString(digest[:])
	return p, nil
}
func (s *Server) brandPackagesHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	g := s.generator
	if r.Method == "GET" {
		g.mu.Lock()
		defer g.mu.Unlock()
		p, err := g.listBrandPackages()
		if err != nil {
			http.Error(w, "Cannot read brand packages", 503)
			return
		}
		generatorJSON(w, 200, p)
		return
	}
	typ, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !controlOriginAllowed(r) || typ != "multipart/form-data" {
		http.Error(w, "Package imports require same-origin multipart form data", 403)
		return
	}
	if !g.assetUploadMu.TryLock() {
		http.Error(w, "Another asset or package upload is being prepared", 409)
		return
	}
	defer g.assetUploadMu.Unlock()
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(15 * time.Minute))
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(16 * time.Minute))
	r.Body = http.MaxBytesReader(w, r.Body, maxBrandArchiveBytes+(64<<10))
	defer r.Body.Close()
	parts, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Choose one package ZIP", 400)
		return
	}
	part, err := parts.NextPart()
	if err != nil || part.FormName() != "file" {
		http.Error(w, "Choose one package ZIP", 400)
		return
	}
	required := uint64(maxBrandArchiveBytes)
	if r.ContentLength > 0 && r.ContentLength < maxBrandArchiveBytes {
		required = uint64(r.ContentLength)
	}
	if err := brandStorageCapacity(g.workRoot, required); err != nil {
		http.Error(w, err.Error(), 507)
		return
	}
	f, err := os.CreateTemp(g.workRoot, "brand-*.zip")
	if err != nil {
		http.Error(w, "Cannot stage package", 507)
		return
	}
	defer os.Remove(f.Name())
	n, copyErr := io.Copy(f, io.LimitReader(part, maxBrandArchiveBytes+1))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil || n > maxBrandArchiveBytes {
		http.Error(w, "Cannot retain package within 512 MiB limit", 422)
		return
	}
	if _, err = parts.NextPart(); err != io.EOF {
		http.Error(w, "Choose exactly one package ZIP", 400)
		return
	}
	p, err := readBrandArchive(f.Name())
	if err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	staged, err := os.MkdirTemp(g.workRoot, "brand-stage-")
	if err != nil {
		http.Error(w, "Cannot stage package content", 507)
		return
	}
	defer os.RemoveAll(staged)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	var prepared *preparedBrand
	err = s.preparation.run(ctx, 15*time.Minute, nil, func(attempt context.Context) error {
		var e error
		prepared, e = prepareBrand(attempt, f.Name(), staged, p)
		return e
	})
	if err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	p = prepared.pkg
	g.mu.Lock()
	defer g.mu.Unlock()
	existing, err := g.listBrandPackages()
	if err != nil {
		http.Error(w, "Cannot inspect installed packages", 503)
		return
	}
	for _, old := range existing {
		if old.ID == p.ID && old.Version == p.Version {
			if old.Digest != p.Digest {
				http.Error(w, "This package version is already installed with different contents; use a new version", 409)
				return
			}
			generatorJSON(w, 200, old)
			return
		}
	}
	if strings.HasSuffix(r.URL.Path, "/preview") {
		generatorJSON(w, 200, p)
		return
	}
	if len(existing) >= 200 {
		http.Error(w, "Package limit reached (200 versions)", 409)
		return
	}
	if err = g.publishBrand(prepared); err != nil {
		http.Error(w, "Package not saved: "+err.Error(), 507)
		return
	}
	p = prepared.pkg
	generatorJSON(w, 201, p)
}
