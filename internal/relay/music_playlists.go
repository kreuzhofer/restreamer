package relay

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

const maxPlaylists = 100
const maxPlaylistRevisions = 100
const maxPlaylistTracks = 50
const maxPlaylistSamples int64 = 3600 * musicSampleRate

type MusicPlaylistRevision struct {
	Revision  int                    `json:"revision"`
	Name      string                 `json:"name"`
	Tracks    []mediaauthor.AssetRef `json:"tracks"`
	Samples   int64                  `json:"samples"`
	Duration  float64                `json:"duration"`
	UpdatedAt string                 `json:"updated_at"`
}
type musicPlaylist struct {
	ID        string                  `json:"id"`
	Revisions []MusicPlaylistRevision `json:"revisions"`
}
type musicPlaylistView struct {
	ID string `json:"id"`
	MusicPlaylistRevision
	Uses []AssetUse `json:"uses"`
}

func (g *generatorStore) playlistRoot() string {
	return filepath.Join(filepath.Dir(g.root), "playlists")
}
func (g *generatorStore) playlistPath(id string) string {
	return filepath.Join(g.playlistRoot(), id+".json")
}
func (g *generatorStore) loadPlaylists() error {
	g.playlists = make(map[string]musicPlaylist)
	if err := os.MkdirAll(g.playlistRoot(), 0700); err != nil {
		return err
	}
	files, err := os.ReadDir(g.playlistRoot())
	if err != nil {
		return err
	}
	for _, entry := range files {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if len(g.playlists) >= maxPlaylists {
			return errors.New("playlist catalog exceeds its limit")
		}
		f, err := os.Open(filepath.Join(g.playlistRoot(), entry.Name()))
		if err != nil {
			return err
		}
		var p musicPlaylist
		d := json.NewDecoder(io.LimitReader(f, 512<<10))
		d.DisallowUnknownFields()
		err = d.Decode(&p)
		tail := d.Decode(new(any))
		f.Close()
		if err != nil || tail != io.EOF || !validDesignID(p.ID) || entry.Name() != p.ID+".json" || len(p.Revisions) == 0 || len(p.Revisions) > maxPlaylistRevisions {
			return errors.New("invalid saved playlist")
		}
		for i, r := range p.Revisions {
			if r.Revision != i+1 || !validAssetName(r.Name) || len(r.Tracks) == 0 || len(r.Tracks) > maxPlaylistTracks || r.Samples <= 0 || r.Samples > maxPlaylistSamples || r.Duration != float64(r.Samples)/musicSampleRate {
				return errors.New("invalid saved playlist revision")
			}
			for _, ref := range r.Tracks {
				if !validDesignID(ref.ID) || ref.Revision < 1 || ref.Revision > maxAssetRevisions {
					return errors.New("invalid saved playlist track")
				}
			}
		}
		g.playlists[p.ID] = p
	}
	return nil
}
func (g *generatorStore) playlistTrackSamples(tracks []mediaauthor.AssetRef) (int64, error) {
	if len(tracks) == 0 || len(tracks) > maxPlaylistTracks {
		return 0, errors.New("Choose 1–50 music tracks")
	}
	var samples int64
	for _, ref := range tracks {
		meta, ok := g.assetRevision(ref)
		if !ok || g.assets[ref.ID].Kind != "audio" || !validMusicMetadata(meta) {
			return 0, errors.New("Choose available exact music revisions")
		}
		samples += meta.Samples
	}
	if samples > maxPlaylistSamples {
		return 0, errors.New("A playlist may contain at most one hour of decoded music")
	}
	return samples, nil
}
func (s *Server) musicPlaylistsHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	g := s.generator
	var input struct {
		Name    string                 `json:"name"`
		Version int                    `json:"version"`
		Tracks  []mediaauthor.AssetRef `json:"tracks"`
	}
	if r.Method != "GET" && !generatorDecode(w, r, &input) {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	id := r.PathValue("id")
	p, exists := g.playlists[id]
	if id != "" && !exists {
		http.Error(w, "Playlist not found", 404)
		return
	}
	if r.Method == "GET" {
		if id != "" {
			revision, err := strconv.Atoi(r.PathValue("revision"))
			if err != nil || revision < 1 || revision > len(p.Revisions) {
				http.Error(w, "Playlist revision not found", 404)
				return
			}
			generatorJSON(w, 200, musicPlaylistView{ID: id, MusicPlaylistRevision: p.Revisions[revision-1], Uses: []AssetUse{}})
			return
		}
		out := make([]musicPlaylistView, 0, len(g.playlists))
		for _, p := range g.playlists {
			uses, err := g.playlistUses(p.ID)
			if err != nil {
				http.Error(w, "Cannot inspect playlist uses", 503)
				return
			}
			out = append(out, musicPlaylistView{ID: p.ID, MusicPlaylistRevision: p.Revisions[len(p.Revisions)-1], Uses: uses})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		generatorJSON(w, 200, out)
		return
	}
	if r.Method != "POST" && input.Version != len(p.Revisions) {
		http.Error(w, "Playlist changed; reload before editing or deleting", 409)
		return
	}
	if r.Method == "DELETE" {
		uses, err := g.playlistUses(id)
		if err != nil {
			http.Error(w, "Cannot inspect playlist uses; deletion blocked", 503)
			return
		}
		if len(uses) > 0 {
			generatorJSON(w, 409, map[string]any{"error": "Playlist is referenced; replace or delete these saved selections first", "uses": uses})
			return
		}
		committed, err := deleteMediaFiles(s.library.root, []string{"generator/playlists/" + id + ".json"})
		if committed {
			delete(g.playlists, id)
		}
		if err != nil {
			http.Error(w, "Cannot delete playlist; reload to check storage", 507)
			return
		}
		generatorJSON(w, 200, map[string]bool{"deleted": true})
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if !validAssetName(input.Name) {
		http.Error(w, "Choose a playlist name up to 180 bytes without control characters", 422)
		return
	}
	samples, err := g.playlistTrackSamples(input.Tracks)
	if err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	if r.Method == "POST" {
		if len(g.playlists) >= maxPlaylists {
			http.Error(w, "Playlist limit reached (100)", 409)
			return
		}
		id, err = assetIdentity()
		if err != nil {
			http.Error(w, "Cannot create playlist identity", 503)
			return
		}
		p = musicPlaylist{ID: id}
	}
	if len(p.Revisions) >= maxPlaylistRevisions {
		http.Error(w, "Playlist revision limit reached (100)", 409)
		return
	}
	next := MusicPlaylistRevision{Revision: len(p.Revisions) + 1, Name: input.Name, Tracks: input.Tracks, Samples: samples, Duration: float64(samples) / musicSampleRate, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	p.Revisions = append(append([]MusicPlaylistRevision{}, p.Revisions...), next)
	if err := writeState(g.playlistPath(id), p); err != nil {
		http.Error(w, "Cannot save playlist; previous revision retained", 507)
		return
	}
	g.playlists[id] = p
	status := 200
	if r.Method == "POST" {
		status = 201
	}
	generatorJSON(w, status, musicPlaylistView{ID: id, MusicPlaylistRevision: next, Uses: []AssetUse{}})
}

// Caller holds the generator catalog lock. A selection always resolves an exact
// revision; editing a playlist never adopts it into an existing design or job.
func (g *generatorStore) musicRefs(m *mediaauthor.Soundtrack) ([]mediaauthor.AssetRef, error) {
	if m == nil {
		return nil, nil
	}
	if m.Playlist == nil {
		return []mediaauthor.AssetRef{m.Asset}, nil
	}
	p, ok := g.playlists[m.Playlist.ID]
	if !ok || m.Playlist.Revision < 1 || m.Playlist.Revision > len(p.Revisions) {
		return nil, errors.New("The selected playlist revision is unavailable")
	}
	return append([]mediaauthor.AssetRef{}, p.Revisions[m.Playlist.Revision-1].Tracks...), nil
}
func (g *generatorStore) playlistUses(id string) ([]AssetUse, error) {
	out := []AssetUse{}
	add := func(d mediaauthor.Design, kind, owner, name string) {
		if d.Soundtrack != nil && d.Soundtrack.Playlist != nil && d.Soundtrack.Playlist.ID == id {
			out = append(out, AssetUse{Kind: kind, ID: owner, Name: name, Revision: d.Soundtrack.Playlist.Revision})
		}
	}
	designs, err := g.list()
	if err != nil {
		return nil, err
	}
	for _, d := range designs {
		add(d, "design", d.ID, d.Name)
	}
	templates, err := g.listTemplates()
	if err != nil {
		return nil, err
	}
	for _, t := range templates {
		add(t.Content, "template", t.ID, t.Name)
	}
	g.jobsMu.Lock()
	defer g.jobsMu.Unlock()
	for _, j := range g.jobs {
		add(j.Design, "job", j.ID, j.Design.Name+" · "+j.State)
	}
	return out, nil
}
