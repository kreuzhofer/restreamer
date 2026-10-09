package relay

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

const maxTemplates = 200
const maxTemplateBytes = maxDesignBytes + 1024

// ContentTemplate stores the entire supported composition, without a draft's
// identity or chosen theme. Copying always chooses appearance separately.
type ContentTemplate struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Version   int                `json:"version"`
	UpdatedAt string             `json:"updated_at"`
	Builtin   bool               `json:"builtin,omitempty"`
	Content   mediaauthor.Design `json:"content"`
}

func templateContent(d mediaauthor.Design) mediaauthor.Design {
	d.ID, d.Name, d.UpdatedAt = "", "", ""
	d.Version = 0
	d.Theme = mediaauthor.ThemeRef{}
	return d
}

func starterTemplates() []ContentTemplate {
	return []ContentTemplate{
		{ID: "starter-prestream", Name: "Welcome, topics and links", Version: 1, Builtin: true, Content: mediaauthor.Design{Stage: "prestream", Scenes: []mediaauthor.Scene{
			{ID: "welcome", Layout: "title", Text: "Welcome to the show", DurationSeconds: 10},
			{ID: "topics", Layout: "list", Text: "Today’s topics", Items: []string{"First topic", "Second topic"}, DurationSeconds: 10},
			{ID: "links", Layout: "list", Text: "Stay connected", Items: []string{"Your website", "Your community link"}, DurationSeconds: 10},
		}}},
		{ID: "starter-ending", Name: "Thanks and follow-up links", Version: 1, Builtin: true, Content: mediaauthor.Design{Stage: "ending", Scenes: []mediaauthor.Scene{
			{ID: "thanks", Layout: "title", Text: "Thanks for watching!", DurationSeconds: 10},
			{ID: "follow-up", Layout: "list", Text: "See you next time", Items: []string{"Your next show", "Your follow-up link"}, DurationSeconds: 10},
		}}},
	}
}

func (g *generatorStore) readTemplate(id string) (ContentTemplate, error) {
	for _, starter := range starterTemplates() {
		if starter.ID == id {
			return starter, nil
		}
	}
	var t ContentTemplate
	if !validDesignID(id) {
		return t, os.ErrNotExist
	}
	f, err := os.Open(filepath.Join(g.templateRoot, id+".json"))
	if err != nil {
		return t, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, maxTemplateBytes+1))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&t); err != nil {
		return t, err
	}
	if dec.Decode(new(any)) != io.EOF || t.ID != id || !templateBounds(t) {
		return t, errors.New("invalid saved template")
	}
	return t, nil
}

func templateBounds(t ContentTemplate) bool {
	return strings.TrimSpace(t.Name) != "" && len(t.Name) <= 180 && utf8.ValidString(t.Name) && designBounds(t.Content)
}

// Caller holds mu, sharing publication/deletion serialization with drafts and assets.
func (g *generatorStore) listTemplates() ([]ContentTemplate, error) {
	files, err := os.ReadDir(g.templateRoot)
	if err != nil {
		return nil, err
	}
	result := make([]ContentTemplate, 0)
	for _, file := range files {
		if filepath.Ext(file.Name()) != ".json" {
			continue
		}
		if len(result) >= maxTemplates {
			return nil, errors.New("too many templates")
		}
		item, err := g.readTemplate(strings.TrimSuffix(file.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return append(starterTemplates(), result...), nil
}

func (g *generatorStore) writeTemplate(t ContentTemplate) error {
	data, err := json.Marshal(t)
	if err != nil || len(data) > maxTemplateBytes {
		return errors.New("template exceeds storage limit")
	}
	f, err := os.CreateTemp(g.templateRoot, ".template-*")
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
	return os.Rename(f.Name(), filepath.Join(g.templateRoot, t.ID+".json"))
}

func (s *Server) generatorTemplatesHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var next ContentTemplate
	if r.Method == "POST" {
		if !generatorDecode(w, r, &next) {
			return
		}
		next.Content = templateContent(next.Content)
		if !templateBounds(next) {
			http.Error(w, "Template requires a name up to 180 bytes and a supported design", 400)
			return
		}
	}
	g := s.generator
	g.mu.Lock()
	defer g.mu.Unlock()
	templates, err := g.listTemplates()
	if err != nil {
		http.Error(w, "Cannot read templates; check generator storage", 503)
		return
	}
	if r.Method == "GET" {
		generatorJSON(w, 200, templates)
		return
	}
	if len(templates) >= maxTemplates+len(starterTemplates()) {
		http.Error(w, "Template limit reached (200 templates)", 409)
		return
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		http.Error(w, "Cannot create template identity", 503)
		return
	}
	next.ID = hex.EncodeToString(id[:])
	next.Version = 1
	next.Builtin = false
	next.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err = g.writeTemplate(next); err != nil {
		http.Error(w, "Template not saved; check generator storage and retry", 507)
		return
	}
	generatorJSON(w, 201, next)
}

func (s *Server) generatorTemplateHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var next ContentTemplate
	if r.Method == "PUT" {
		if !generatorDecode(w, r, &next) {
			return
		}
		next.Content = templateContent(next.Content)
		if !templateBounds(next) {
			http.Error(w, "Invalid template structure or input limits", 400)
			return
		}
	}
	g := s.generator
	g.mu.Lock()
	defer g.mu.Unlock()
	current, err := g.readTemplate(r.PathValue("id"))
	if errors.Is(err, os.ErrNotExist) {
		http.Error(w, "Template not found", 404)
		return
	}
	if err != nil {
		http.Error(w, "Cannot read template; check generator storage", 503)
		return
	}
	if r.Method == "GET" {
		generatorJSON(w, 200, current)
		return
	}
	if current.Builtin {
		http.Error(w, "Built-in starters cannot be overwritten; save an independent template", 409)
		return
	}
	if next.Version != current.Version {
		generatorJSON(w, 409, map[string]any{"error": "A newer template was saved in another tab. Reload it or save your local work as a new template.", "current": current})
		return
	}
	next.ID = current.ID
	next.Version = current.Version + 1
	next.Builtin = false
	next.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err = g.writeTemplate(next); err != nil {
		http.Error(w, "Template not saved; check generator storage and retry", 507)
		return
	}
	generatorJSON(w, 200, next)
}

func (s *Server) generatorTemplateCopyHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var input struct {
		Name    string               `json:"name"`
		Version int                  `json:"version"`
		Theme   mediaauthor.ThemeRef `json:"theme"`
	}
	if !generatorDecode(w, r, &input) {
		return
	}
	// Theme support is deliberately independent of template contents.
	if input.Theme.ID != "retro" || input.Theme.Revision != 1 {
		http.Error(w, "Choose an available theme revision separately from the template", 422)
		return
	}
	g := s.generator
	g.mu.Lock()
	defer g.mu.Unlock()
	template, err := g.readTemplate(r.PathValue("id"))
	if errors.Is(err, os.ErrNotExist) {
		http.Error(w, "Template not found", 404)
		return
	}
	if err != nil {
		http.Error(w, "Cannot read template; check generator storage", 503)
		return
	}
	if input.Version != template.Version {
		generatorJSON(w, 409, map[string]any{"error": "The template changed. Reload it before creating a design.", "current": template})
		return
	}
	draft := template.Content
	draft.Name = input.Name
	draft.Theme = input.Theme
	if !designBounds(draft) {
		http.Error(w, "Invalid design name or content", 400)
		return
	}
	designs, err := g.list()
	if err != nil {
		http.Error(w, "Cannot read drafts; check generator storage", 503)
		return
	}
	if len(designs) >= maxDesigns {
		http.Error(w, "Draft limit reached (200 designs)", 409)
		return
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		http.Error(w, "Cannot create draft identity", 503)
		return
	}
	draft.ID = hex.EncodeToString(id[:])
	draft.Version = 1
	draft.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err = g.write(draft); err != nil {
		http.Error(w, "Draft not saved; check generator storage and retry", 507)
		return
	}
	generatorJSON(w, 201, draft)
}
