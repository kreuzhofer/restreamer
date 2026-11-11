package relay

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
	"golang.org/x/image/font/opentype"
)

type preparedBrand struct {
	pkg       brandPackage
	root      string
	assets    []GeneratorAsset
	themes    []mediaauthor.Theme
	templates []ContentTemplate
}

func brandObjectID(p brandPackage, kind, id string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("brand:%s:%d:%s:%s", p.ID, p.Version, kind, id)))
	return hex.EncodeToString(sum[:16])
}

func prepareBrand(ctx context.Context, archive, root string, p brandPackage) (*preparedBrand, error) {
	result := &preparedBrand{pkg: p, root: root}
	result.pkg.Assets = append([]brandAsset{}, p.Assets...)
	result.pkg.Themes = append([]brandTheme{}, p.Themes...)
	result.pkg.Templates = append([]brandTemplate{}, p.Templates...)
	z, err := zip.OpenReader(archive)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	files := map[string]*zip.File{}
	for _, f := range z.File {
		files[f.Name] = f
	}
	refs := map[string]mediaauthor.AssetRef{}
	var normalizedBytes int64
	for i, a := range p.Assets {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		id := brandObjectID(p, "asset", a.ID)
		refs[a.ID] = mediaauthor.AssetRef{ID: id, Revision: 1}
		dir := filepath.Join(root, id)
		if err = os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
		if err = brandStorageCapacity(root, files[a.Path].UncompressedSize64+(32<<20)); err != nil {
			return nil, err
		}
		source, err := files[a.Path].Open()
		if err != nil {
			return nil, errors.New("Cannot read asset")
		}
		hash := sha256.New()
		reader := io.TeeReader(io.LimitReader(source, maxBrandArchiveBytes+1), hash)
		var meta AssetRevision
		if a.Kind == "font" {
			meta, err = stageBrandFont(reader, filepath.Join(dir, "1.otf"))
		} else if a.Kind == "video" {
			meta, err = stageGeneratorVideo(ctx, reader, filepath.Join(dir, "1.mp4"))
		} else if a.Kind == "audio" {
			meta, err = stageGeneratorMusic(ctx, reader, filepath.Join(dir, "1.wav"))
		} else {
			meta, err = normalizeGeneratorImage(reader, filepath.Join(dir, "1.png"))
		}
		source.Close()
		if err != nil {
			return nil, err
		}
		if hex.EncodeToString(hash.Sum(nil)) != a.Digest {
			return nil, errors.New("Asset checksum does not match manifest")
		}
		normalizedBytes += meta.Bytes
		if normalizedBytes > maxBrandArchiveBytes {
			return nil, errors.New("Normalized package exceeds 512 MiB")
		}
		meta.Revision = 1
		asset := GeneratorAsset{ID: id, Name: a.Name, Kind: a.Kind, Revision: 1, Revisions: []AssetRevision{meta}}
		if err = writeState(filepath.Join(dir, "asset.json"), asset); err != nil {
			return nil, err
		}
		result.assets = append(result.assets, asset)
		result.pkg.Assets[i].ID = id
	}
	remap := func(ref **mediaauthor.AssetRef) error {
		if *ref == nil {
			return nil
		}
		v, ok := refs[(*ref).ID]
		if !ok || (*ref).Revision != 1 {
			return errors.New("Reference must name a package asset at revision 1")
		}
		*ref = &v
		return nil
	}
	themes := map[string]mediaauthor.ThemeRef{}
	for i, t := range p.Themes {
		id := brandObjectID(p, "theme", t.ID)
		themes[t.ID] = mediaauthor.ThemeRef{ID: id, Revision: 1}
		if err = remap(&t.Style.Background); err != nil {
			return nil, err
		}
		if err = remap(&t.Style.Logo); err != nil {
			return nil, err
		}
		if err = remap(&t.Style.OBSBorder); err != nil {
			return nil, err
		}
		if t.Style.Motion != nil {
			motion := *t.Style.Motion
			t.Style.Motion = &motion
			ref := &motion.Asset
			if err = remap(&ref); err != nil {
				return nil, err
			}
			motion.Asset = *ref
			found := false
			for _, a := range result.assets {
				if a.ID == ref.ID {
					found = a.Kind == "video" && a.Revisions[0].Bytes <= 64<<20 && math.Abs(a.Revisions[0].DurationSeconds-float64(motion.CycleSeconds)) <= 0.04
				}
			}
			if !found {
				return nil, errors.New("Motion requires a video up to 64 MiB matching its whole-second cycle")
			}
		}
		if err = remap(&t.Style.FontAsset); err != nil {
			return nil, err
		}
		if t.Style.FontAsset != nil {
			data, e := os.ReadFile(filepath.Join(root, t.Style.FontAsset.ID, "1.otf"))
			if e != nil {
				return nil, e
			}
			t.Style.ResolvedFont, e = opentype.Parse(data)
			if e != nil {
				return nil, e
			}
		}
		theme := mediaauthor.Theme{ID: id, Name: t.Name, Revision: 1, Style: t.Style}
		if err = writeState(filepath.Join(root, id+"-1.json"), theme); err != nil {
			return nil, err
		}
		result.themes = append(result.themes, theme)
		result.pkg.Themes[i] = brandTheme{ID: id, Name: t.Name, Style: t.Style}
	}
	for i, t := range p.Templates {
		id := brandObjectID(p, "template", t.ID)
		// Decode-owned scene slices are copied before reference resolution.
		d := t.Content
		d.Scenes = append([]mediaauthor.Scene{}, d.Scenes...)
		for n := range d.Scenes {
			if err = remap(&d.Scenes[n].Image); err != nil {
				return nil, err
			}
			if d.Scenes[n].Video != nil {
				v := *d.Scenes[n].Video
				d.Scenes[n].Video = &v
				ref := &v.Asset
				if err = remap(&ref); err != nil {
					return nil, err
				}
				v.Asset = *ref
			}
		}
		if d.Soundtrack != nil {
			m := *d.Soundtrack
			d.Soundtrack = &m
			if m.Playlist != nil {
				return nil, errors.New("Package playlists are not supported")
			}
			ref := &m.Asset
			if err = remap(&ref); err != nil {
				return nil, err
			}
			m.Asset = *ref
		}
		item := ContentTemplate{ID: id, Name: t.Name, Version: 1, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), Content: templateContent(d)}
		if !templateBounds(item) {
			return nil, errors.New("Invalid template content")
		}
		validation := d
		validation.Name = t.Name
		if validation.Stage == "brb" {
			validation.Stage = "prestream"
		}
		validation.Theme = themes[t.Theme]
		for _, theme := range result.themes {
			if theme.ID == validation.Theme.ID {
				if issues := mediaauthor.ValidateWithTheme(validation, theme, 1920, 1080); len(issues) > 0 {
					return nil, fmt.Errorf("Template %s: %s", t.Name, issues[0].Message)
				}
			}
		}
		if err = writeState(filepath.Join(root, id+".json"), item); err != nil {
			return nil, err
		}
		result.templates = append(result.templates, item)
		result.pkg.Templates[i] = brandTemplate{ID: id, Name: t.Name, Theme: themes[t.Theme].ID, Content: item.Content}
	}
	shadow := &generatorStore{assetsRoot: root, assets: map[string]GeneratorAsset{}, themes: map[mediaauthor.ThemeRef]mediaauthor.Theme{}}
	for _, a := range result.assets {
		shadow.assets[a.ID] = a
	}
	for _, t := range result.themes {
		shadow.themes[mediaauthor.ThemeRef{ID: t.ID, Revision: 1}] = t
		if issues := shadow.themeStyleAssetIssues(t.Style); len(issues) > 0 {
			return nil, errors.New(issues[0].Message)
		}
	}
	for _, t := range result.templates {
		if issues := shadow.musicIssues(t.Content, 30, 48000); len(issues) > 0 {
			return nil, errors.New(issues[0].Message)
		}
		if issues := shadow.assetIssues(t.Content); len(issues) > 0 {
			return nil, errors.New(issues[0].Message)
		}
		if issues := shadow.videoIssues(t.Content, 30); len(issues) > 0 {
			return nil, errors.New(issues[0].Message)
		}
	}
	return result, nil
}

func (p brandPackage) filename() string { return p.ID + "-" + strconv.Itoa(p.Version) + ".json" }

// The journal records only newly owned identities. Recovery runs before readers
// load assets or themes; the package manifest is the publication commit marker.
func (g *generatorStore) brandPaths(p brandPackage) []string {
	var out []string
	for _, a := range p.Assets {
		out = append(out, filepath.Join(g.assetsRoot, a.ID))
	}
	for _, t := range p.Themes {
		out = append(out, filepath.Join(g.themesRoot, t.ID+"-1.json"))
	}
	for _, t := range p.Templates {
		out = append(out, filepath.Join(g.templateRoot, t.ID+".json"))
	}
	return out
}
func (g *generatorStore) publishBrand(b *preparedBrand) error {
	templates, err := g.listTemplates()
	if err != nil {
		return err
	}
	revisions := 0
	for _, a := range g.assets {
		revisions += len(a.Revisions)
	}
	if revisions+len(b.assets) > maxAssetRevisions || len(g.themes)+len(b.themes) > maxThemeRevisions+len(mediaauthor.BuiltinThemes())+len(mediaauthor.ArchivedThemes()) || len(templates)+len(b.templates) > maxTemplates+len(starterTemplates()) {
		return errors.New("asset, theme or template limit reached")
	}
	paths := g.brandPaths(b.pkg)
	for _, p := range paths {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			return errors.New("package object identity already exists")
		}
	}
	if err = os.MkdirAll(g.packagesRoot(), 0700); err != nil {
		return err
	}
	journal := filepath.Join(g.packagesRoot(), ".pending-"+b.pkg.filename())
	if err = writeState(journal, b.pkg); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			clean := true
			for _, p := range paths {
				if os.RemoveAll(p) != nil {
					clean = false
				}
			}
			if clean {
				os.Remove(journal)
			}
		}
	}()
	for _, dst := range paths {
		if err = os.Rename(filepath.Join(b.root, filepath.Base(dst)), dst); err != nil {
			return err
		}
	}
	b.pkg.ImportedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err = writeState(filepath.Join(g.packagesRoot(), b.pkg.filename()), b.pkg); err != nil {
		return err
	}
	committed = true
	for _, a := range b.assets {
		g.assets[a.ID] = a
	}
	for _, t := range b.themes {
		g.themes[mediaauthor.ThemeRef{ID: t.ID, Revision: 1}] = t
	}
	// A committed package remains successful if cleanup is deferred to restart.
	_ = os.Remove(journal)
	return nil
}

func (g *generatorStore) recoverBrandImports() error {
	files, err := os.ReadDir(g.packagesRoot())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, f := range files {
		if !strings.HasPrefix(f.Name(), ".pending-") {
			continue
		}
		file := filepath.Join(g.packagesRoot(), f.Name())
		var p brandPackage
		if readBrandJSON(file, &p) != nil || !brandNamePattern.MatchString(p.ID) || p.Version < 1 || f.Name() != ".pending-"+p.filename() {
			return errors.New("invalid package recovery journal")
		}
		for _, a := range p.Assets {
			if !validDesignID(a.ID) {
				return errors.New("invalid recovery asset identity")
			}
		}
		for _, t := range p.Themes {
			if !validDesignID(t.ID) {
				return errors.New("invalid recovery theme identity")
			}
		}
		for _, t := range p.Templates {
			if !validDesignID(t.ID) {
				return errors.New("invalid recovery template identity")
			}
		}
		if _, err = os.Stat(filepath.Join(g.packagesRoot(), p.filename())); os.IsNotExist(err) {
			for _, target := range g.brandPaths(p) {
				if err = os.RemoveAll(target); err != nil {
					return errors.New("cannot recover interrupted package import")
				}
			}
		} else if err != nil {
			return err
		}
		if err = os.Remove(file); err != nil {
			return err
		}
	}
	return nil
}

func readBrandJSON(path string, dst any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 512<<10))
	d.DisallowUnknownFields()
	if err = d.Decode(dst); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("invalid brand package state")
	}
	return nil
}
