package relay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func (s *Server) generatorAssetsHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	g := s.generator
	if r.Method == "POST" {
		s.generatorAssetUpload(w, r)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]assetResponse, 0, len(g.assets))
	allUses, err := s.assetUsesLocked("")
	if err != nil {
		http.Error(w, "Cannot inspect image uses; check saved designs", 503)
		return
	}
	for _, a := range g.assets {
		uses := make([]AssetUse, 0)
		for _, use := range allUses {
			if use.AssetID == a.ID {
				uses = append(uses, use)
			}
		}
		out = append(out, assetResponse{a, uses})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	generatorJSON(w, 200, out)
}
func (s *Server) generatorAssetUpload(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	g := s.generator
	typ, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !controlOriginAllowed(r) || typ != "multipart/form-data" {
		http.Error(w, "Image uploads require same-origin multipart form data", 403)
		return
	}
	if !g.assetUploadMu.TryLock() {
		http.Error(w, "Another image upload is being prepared", 409)
		return
	}
	defer g.assetUploadMu.Unlock()
	id := r.PathValue("id")
	version := 0
	if id != "" {
		var err error
		version, err = strconv.Atoi(r.URL.Query().Get("version"))
		if err != nil || version < 1 || !validDesignID(id) {
			http.Error(w, "Specify the current asset revision before replacing it", 400)
			return
		}
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(time.Minute))
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute))
	r.Body = http.MaxBytesReader(w, r.Body, maxAssetUploadBytes+(64<<10))
	defer r.Body.Close()
	parts, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Choose one PNG or JPEG image", 400)
		return
	}
	part, err := parts.NextPart()
	if err != nil || part.FormName() != "file" || !validAssetName(part.FileName()) {
		http.Error(w, "Choose one image with a name up to 180 bytes and no control characters", 400)
		return
	}
	name := part.FileName()
	temp, err := os.MkdirTemp(g.assetsRoot, ".upload-")
	if err != nil {
		http.Error(w, "Cannot stage image; check storage", 507)
		return
	}
	defer os.RemoveAll(temp)
	normalized := filepath.Join(temp, "image.png")
	uploadCtx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	release, err := s.preparation.acquire(uploadCtx)
	if err != nil {
		http.Error(w, "Image preparation timed out while waiting; try again after current work finishes", 503)
		return
	}
	meta, err := normalizeGeneratorImage(part, normalized)
	release()
	if err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	if _, err := parts.NextPart(); err != io.EOF {
		http.Error(w, "Upload exactly one image", 400)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	count := 0
	for _, a := range g.assets {
		count += len(a.Revisions)
	}
	if count >= maxAssetRevisions {
		http.Error(w, "Image revision limit reached (200 total revisions)", 409)
		return
	}
	var asset GeneratorAsset
	if id == "" {
		id, err = assetIdentity()
		if err != nil {
			http.Error(w, "Cannot create image identity", 503)
			return
		}
		asset = GeneratorAsset{ID: id, Name: name, Kind: "image"}
	} else {
		var ok bool
		asset, ok = g.assets[id]
		if !ok {
			http.Error(w, "Image asset not found", 404)
			return
		}
		if asset.Revision != version {
			http.Error(w, "Image changed in another tab. Reload before uploading its replacement.", 409)
			return
		}
	}
	// Append to a copy so a failed metadata save cannot modify the stored history.
	asset.Revisions = append([]AssetRevision{}, asset.Revisions...)
	asset.Revision++
	meta.Revision = asset.Revision
	asset.Revisions = append(asset.Revisions, meta)
	target := filepath.Join(g.assetsRoot, id)
	newAsset := version == 0
	if newAsset {
		if os.Rename(normalized, filepath.Join(temp, "1.png")) != nil || writeState(filepath.Join(temp, "asset.json"), asset) != nil || os.Rename(temp, target) != nil {
			http.Error(w, "Cannot save image revision; check storage", 507)
			return
		}
	} else {
		path := g.assetPath(mediaauthor.AssetRef{ID: id, Revision: asset.Revision})
		if os.Rename(normalized, path) != nil {
			http.Error(w, "Cannot save image revision; check storage", 507)
			return
		}
		if writeState(filepath.Join(target, "asset.json"), asset) != nil {
			os.Remove(path)
			http.Error(w, "Cannot save image metadata; previous revision is unchanged", 507)
			return
		}
	}
	g.assets[id] = asset
	generatorJSON(w, 201, assetResponse{asset, []AssetUse{}})
}
func (s *Server) generatorAssetDelete(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var body struct{}
	if !generatorDecode(w, r, &body) {
		return
	}
	g := s.generator
	id := r.PathValue("id")
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.assets[id]; !ok {
		http.Error(w, "Image asset not found", 404)
		return
	}
	uses, err := s.assetUsesLocked(id)
	if err != nil {
		http.Error(w, "Cannot check references; deletion blocked until storage is readable", 503)
		return
	}
	if len(uses) > 0 {
		generatorJSON(w, 409, map[string]any{"error": "This image is referenced. Remove its uses before deleting it.", "uses": uses})
		return
	}
	trash := filepath.Join(g.assetsRoot, ".deleted-"+id)
	if os.Rename(filepath.Join(g.assetsRoot, id), trash) != nil {
		http.Error(w, "Cannot delete image; check storage", 507)
		return
	}
	delete(g.assets, id)
	// Removal is atomic to readers; interrupted cleanup is retried at startup.
	if os.RemoveAll(trash) != nil {
		http.Error(w, "Image removed from the catalog; temporary cleanup will retry at restart", 507)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) generatorAssetImage(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	select {
	case s.previewSlots <- struct{}{}:
		defer func() { <-s.previewSlots }()
	default:
		http.Error(w, "Image viewer limit reached", 429)
		return
	}
	revision, err := strconv.Atoi(r.PathValue("revision"))
	if err != nil {
		http.Error(w, "Image revision not found", 404)
		return
	}
	g := s.generator
	ref := mediaauthor.AssetRef{ID: r.PathValue("id"), Revision: revision}
	g.mu.Lock()
	meta, ok := g.assetRevision(ref)
	var f *os.File
	if ok {
		f, err = os.Open(g.assetPath(ref))
	} else {
		err = os.ErrNotExist
	}
	g.mu.Unlock()
	if !ok || errors.Is(err, os.ErrNotExist) {
		http.Error(w, "Image revision not found", 404)
		return
	}
	if err != nil {
		http.Error(w, "Cannot read image revision", 503)
		return
	}
	defer f.Close()
	data, err := readImageRevision(f, meta)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	http.ServeContent(w, r, "image.png", time.Time{}, bytes.NewReader(data))
}
