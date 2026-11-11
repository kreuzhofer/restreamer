package relay

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

type brandRemovalItem struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}
type brandRemoval struct {
	Remove   []brandRemovalItem `json:"remove"`
	Preserve []brandRemovalItem `json:"preserve"`
	paths    []string
}

// Caller holds mediaMu and generator.mu. The existing transactional deletion
// journal provides rollback and startup recovery for files and directories.
func (s *Server) planBrandRemoval(p brandPackage) (brandRemoval, error) {
	g := s.generator
	plan := brandRemoval{Remove: []brandRemovalItem{}, Preserve: []brandRemovalItem{}}
	removeThemes, removeTemplates := map[string]bool{}, map[string]bool{}
	designs, err := g.list()
	if err != nil {
		return plan, err
	}
	usedThemes := map[string]bool{}
	for _, d := range designs {
		usedThemes[d.Theme.ID] = true
	}
	g.jobsMu.Lock()
	for _, j := range g.jobs {
		usedThemes[j.Design.Theme.ID] = true
		if j.ThemeSnapshot != nil {
			usedThemes[j.ThemeSnapshot.ID] = true
		}
	}
	g.jobsMu.Unlock()
	s.broadcast.mu.Lock()
	if s.broadcast.media != nil && s.broadcast.media.settings.Theme != nil {
		usedThemes[s.broadcast.media.settings.Theme.ID] = true
	}
	s.broadcast.mu.Unlock()
	s.brbCandidateMu.Lock()
	if s.brbCandidate != nil && s.brbCandidate.Settings.Theme != nil {
		usedThemes[s.brbCandidate.Settings.Theme.ID] = true
	}
	s.brbCandidateMu.Unlock()
	for _, t := range p.Templates {
		current, err := g.readTemplate(t.ID)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return plan, err
		}
		item := brandRemovalItem{t.ID, "template", t.Name}
		if current.Version != 1 {
			plan.Preserve = append(plan.Preserve, item)
			continue
		}
		removeTemplates[t.ID] = true
		plan.Remove = append(plan.Remove, item)
		plan.paths = append(plan.paths, "generator/templates/"+t.ID+".json")
	}
	for _, t := range p.Themes {
		latest := 0
		for ref := range g.themes {
			if ref.ID == t.ID {
				latest = max(latest, ref.Revision)
			}
		}
		if latest == 0 {
			continue
		}
		item := brandRemovalItem{t.ID, "theme", t.Name}
		if latest != 1 || usedThemes[t.ID] {
			plan.Preserve = append(plan.Preserve, item)
			continue
		}
		removeThemes[t.ID] = true
		plan.Remove = append(plan.Remove, item)
		plan.paths = append(plan.paths, "generator/themes/"+t.ID+"-1.json")
	}
	uses, err := s.assetUsesLocked("")
	if err != nil {
		return plan, err
	}
	for _, a := range p.Assets {
		current, ok := g.assets[a.ID]
		if !ok {
			continue
		}
		preserve := current.Revision != 1
		for _, u := range uses {
			if u.Kind == "package" && u.ID == p.filename() {
				continue
			}
			if u.AssetID != a.ID {
				continue
			}
			if u.Kind == "theme" && removeThemes[strings.Split(u.ID, ":")[0]] {
				continue
			}
			if u.Kind == "template" && removeTemplates[u.ID] {
				continue
			}
			preserve = true
		}
		item := brandRemovalItem{a.ID, "asset", a.Name}
		if preserve {
			plan.Preserve = append(plan.Preserve, item)
		} else {
			plan.Remove = append(plan.Remove, item)
			plan.paths = append(plan.paths, "generator/assets/"+a.ID)
		}
	}
	plan.paths = append(plan.paths, "generator/packages/"+p.filename())
	return plan, nil
}

func (s *Server) brandPackageRemovalHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	if r.Method == "DELETE" {
		var body struct{}
		if !generatorDecode(w, r, &body) {
			return
		}
	}
	version, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || version < 1 || !brandNamePattern.MatchString(r.PathValue("id")) {
		http.Error(w, "Package not found", 404)
		return
	}
	if !s.mediaMu.TryLock() {
		http.Error(w, "Wait for media preparation before removing a package", 409)
		return
	}
	defer s.mediaMu.Unlock()
	g := s.generator
	g.mu.Lock()
	defer g.mu.Unlock()
	packages, err := g.listBrandPackages()
	if err != nil {
		http.Error(w, "Cannot inspect packages", 503)
		return
	}
	var selected *brandPackage
	for _, p := range packages {
		if p.ID == r.PathValue("id") && p.Version == version {
			selected = &p
			break
		}
	}
	if selected == nil {
		http.Error(w, "Package not found", 404)
		return
	}
	plan, err := s.planBrandRemoval(*selected)
	if err != nil {
		http.Error(w, "Cannot inspect references; removal blocked until storage is readable", 503)
		return
	}
	if r.Method == "GET" {
		generatorJSON(w, 200, plan)
		return
	}
	committed, err := deleteMediaFiles(s.library.root, plan.paths)
	if committed {
		for _, item := range plan.Remove {
			switch item.Kind {
			case "asset":
				delete(g.assets, item.ID)
			case "theme":
				delete(g.themes, mediaauthor.ThemeRef{ID: item.ID, Revision: 1})
			}
		}
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("Removal storage error (committed: %t); reload to check the result. Restart retries cleanup.", committed), 507)
		return
	}
	generatorJSON(w, 200, plan)
}
