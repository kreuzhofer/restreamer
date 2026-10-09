package relay

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"unicode/utf8"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

const maxDesignBytes = 64 << 10
const maxDesigns = 200

type generatorStore struct {
	mu        sync.Mutex
	previewMu sync.Mutex
	root      string
}

func (s *Server) initializeGenerator() error {
	if s.library == nil {
		return nil
	}
	root := filepath.Join(s.library.root, "generator", "designs")
	if err := os.MkdirAll(root, 0700); err != nil {
		return errors.New("cannot create generator draft storage")
	}
	s.generator = &generatorStore{root: root}
	return nil
}

func designBounds(d mediaauthor.Design) bool {
	if !utf8.ValidString(d.Name) || len(d.Name) > 180 || len(d.Scenes) != 1 {
		return false
	}
	for _, scene := range d.Scenes {
		if len(scene.Text) > 4096 || !utf8.ValidString(scene.Text) || len(scene.ID) > 80 || len(scene.Layout) > 40 || len(scene.Font) > 40 {
			return false
		}
	}
	return len(d.Theme.ID) <= 80 && (d.Stage == "prestream" || d.Stage == "ending")
}

func validDesignID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'f' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func (g *generatorStore) read(id string) (mediaauthor.Design, error) {
	var d mediaauthor.Design
	if _, err := os.Stat(g.root); err != nil {
		return d, errors.New("draft storage unavailable")
	}
	if !validDesignID(id) {
		return d, os.ErrNotExist
	}
	f, err := os.Open(filepath.Join(g.root, id+".json"))
	if err != nil {
		return d, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, maxDesignBytes+1))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&d); err != nil {
		return d, err
	}
	if dec.Decode(new(any)) != io.EOF || !designBounds(d) || d.ID != id {
		return d, errors.New("invalid saved design")
	}
	return d, nil
}

func (g *generatorStore) list() ([]mediaauthor.Design, error) {
	files, err := os.ReadDir(g.root)
	if err != nil {
		return nil, err
	}
	designs := make([]mediaauthor.Design, 0)
	for _, file := range files {
		if filepath.Ext(file.Name()) != ".json" {
			continue
		}
		if len(designs) >= maxDesigns {
			return nil, errors.New("too many saved designs")
		}
		d, err := g.read(file.Name()[:len(file.Name())-5])
		if err != nil {
			return nil, err
		}
		designs = append(designs, d)
	}
	sort.Slice(designs, func(i, j int) bool { return designs[i].UpdatedAt > designs[j].UpdatedAt })
	return designs, nil
}

// write publishes only complete, synced drafts. A failed save never acknowledges
// a new version, so clients retain their local edits and can retry safely.
func (g *generatorStore) write(d mediaauthor.Design) error {
	data, err := json.Marshal(d)
	if err != nil || len(data) > maxDesignBytes {
		return errors.New("invalid draft")
	}
	f, err := os.CreateTemp(g.root, ".draft-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(g.root, d.ID+".json"))
}
