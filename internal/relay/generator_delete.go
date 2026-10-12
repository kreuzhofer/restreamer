package relay

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

func (s *Server) generatorTemplateDelete(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var input struct {
		Version int `json:"version"`
	}
	if !generatorDecode(w, r, &input) {
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
		http.Error(w, "Cannot read template storage", 503)
		return
	}
	if template.Builtin {
		http.Error(w, "Built-in starters cannot be deleted", 409)
		return
	}
	if template.Version != input.Version {
		http.Error(w, "Template changed; reload before deleting", 409)
		return
	}
	_, err = deleteMediaFiles(s.library.root, []string{"generator/templates/" + template.ID + ".json"})
	if err != nil {
		http.Error(w, "Deletion storage error; reload to check the result. Restart retries interrupted cleanup.", 507)
		return
	}
	generatorJSON(w, 200, map[string]bool{"deleted": true})
}

func (s *Server) generatorThemeDelete(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var input struct {
		Revision int `json:"revision"`
	}
	if !generatorDecode(w, r, &input) {
		return
	}
	id := r.PathValue("id")
	if mediaauthor.IsBuiltinTheme(id) {
		http.Error(w, "Built-in themes cannot be deleted", 409)
		return
	}
	if !s.mediaMu.TryLock() {
		http.Error(w, "Wait for BRB preparation before deleting a theme", 409)
		return
	}
	defer s.mediaMu.Unlock()
	g := s.generator
	g.mu.Lock()
	defer g.mu.Unlock()
	paths := []string{}
	latest := 0
	for ref := range g.themes {
		if ref.ID == id {
			latest = max(latest, ref.Revision)
			paths = append(paths, fmt.Sprintf("generator/themes/%s-%d.json", id, ref.Revision))
		}
	}
	if latest == 0 {
		http.Error(w, "Theme not found", 404)
		return
	}
	if latest != input.Revision {
		http.Error(w, "Theme changed; reload its latest revision before deleting", 409)
		return
	}
	designs, err := g.list()
	if err != nil {
		http.Error(w, "Cannot check saved designs; deletion blocked until storage is readable", 503)
		return
	}
	for _, d := range designs {
		if d.Theme.ID == id {
			http.Error(w, "Theme is used by design “"+d.Name+"”. Change or delete that design first.", 409)
			return
		}
	}
	g.jobsMu.Lock()
	defer g.jobsMu.Unlock()
	for _, job := range g.jobs {
		if job.Design.Theme.ID == id || job.ThemeSnapshot != nil && job.ThemeSnapshot.ID == id {
			http.Error(w, "Theme is used by generation history. Delete those results first.", 409)
			return
		}
	}
	s.broadcast.mu.Lock()
	active := s.broadcast.media != nil && s.broadcast.media.settings.Theme != nil && s.broadcast.media.settings.Theme.ID == id
	s.broadcast.mu.Unlock()
	s.brbCandidateMu.Lock()
	candidate := s.brbCandidate != nil && s.brbCandidate.Settings.Theme != nil && s.brbCandidate.Settings.Theme.ID == id
	s.brbCandidateMu.Unlock()
	if active || candidate {
		http.Error(w, "Theme is used by current or prepared BRB media. Replace or discard it first.", 409)
		return
	}
	committed, err := deleteMediaFiles(s.library.root, paths)
	if committed {
		for ref := range g.themes {
			if ref.ID == id {
				delete(g.themes, ref)
			}
		}
	}
	if err != nil {
		http.Error(w, "Deletion storage error; reload to check the result. Restart retries interrupted cleanup.", 507)
		return
	}
	generatorJSON(w, 200, map[string]bool{"deleted": true})
}

func (s *Server) generatorJobDelete(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var input struct{}
	if !generatorDecode(w, r, &input) {
		return
	}
	g := s.generator
	g.mu.Lock()
	defer g.mu.Unlock()
	g.jobsMu.Lock()
	defer g.jobsMu.Unlock()
	id := r.PathValue("id")
	if g.jobs[id] == nil {
		http.Error(w, "Generation result not found", 404)
		return
	}
	s.deleteGenerationLocked(w, map[string]bool{id: true}, nil)
}

func (s *Server) generatorDesignDelete(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var input struct {
		Version int `json:"version"`
	}
	if !generatorDecode(w, r, &input) {
		return
	}
	g := s.generator
	g.mu.Lock()
	defer g.mu.Unlock()
	d, err := g.read(r.PathValue("id"))
	if errors.Is(err, os.ErrNotExist) {
		http.Error(w, "Design not found", 404)
		return
	}
	if err != nil {
		http.Error(w, "Cannot read design; deletion blocked until storage is readable", 503)
		return
	}
	if input.Version != d.Version {
		http.Error(w, "Design changed; reload it before deleting", 409)
		return
	}
	g.jobsMu.Lock()
	defer g.jobsMu.Unlock()
	jobs := map[string]bool{}
	for id, job := range g.jobs {
		if job.Design.ID == d.ID {
			jobs[id] = true
		}
	}
	s.deleteGenerationLocked(w, jobs, []string{"generator/designs/" + d.ID + ".json"})
}

// Caller holds g.mu and jobsMu. Library locking protects references and file
// opens; command serialization plus a new context invalidates pre-opened media.
func (s *Server) deleteGenerationLocked(w http.ResponseWriter, jobs map[string]bool, paths []string) {
	g, l := s.generator, s.library
	for _, job := range g.jobs {
		if job.State == "running" || job.State == "cancelling" {
			http.Error(w, "Wait for running generation to finish or cancel it before deleting", 409)
			return
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	revisions := map[string]bool{}
	for id := range jobs {
		paths = append(paths, "generator/jobs/"+id+".json")
		if revision := g.jobs[id].MediaRevision; revision != "" {
			revisions[revision] = true
		}
		// Recover output retained just before an interrupted job's final save.
		for revision, media := range l.revisions {
			if media.LibraryID == "generator-"+id {
				revisions[revision] = true
			}
		}
	}
	retained := []string{}
	for revision := range revisions {
		shared := false
		for id, job := range g.jobs {
			if !jobs[id] && job.MediaRevision == revision {
				shared = true
			}
		}
		for _, entry := range l.entries {
			if entry.Revision == revision {
				shared = true
			}
		}
		if shared {
			delete(revisions, revision)
			retained = append(retained, revision)
			continue
		}
		if reason := s.mediaDeletionBlockLocked(revision); reason != "" {
			http.Error(w, reason, 409)
			return
		}
		paths = append(paths, "revisions/"+revision+".json", "revisions/"+revision+".flv")
	}
	committed, err := deleteMediaFiles(l.root, paths)
	if committed {
		for id := range jobs {
			delete(g.jobs, id)
		}
		for id := range revisions {
			delete(l.revisions, id)
		}
		s.broadcast.mu.Lock()
		s.broadcast.control.version++
		s.broadcast.mu.Unlock()
	}
	if err != nil {
		http.Error(w, "Deletion storage error; reload to check the result. Restart retries interrupted cleanup.", 507)
		return
	}
	generatorJSON(w, 200, map[string]any{"deleted_jobs": len(jobs), "deleted_media": len(revisions), "retained_shared_media": retained})
}

// Caller holds library.mu and controlMu, preventing new selections/commands.
func (s *Server) mediaDeletionBlockLocked(id string) string {
	l := s.library
	if l.selected.Prestream == id || l.selected.Ending == id {
		return "Media is selected for PRESTREAM or ENDING. Clear or replace the selection in live control before deleting."
	}
	for _, shortcut := range l.selected.Shortcuts {
		if shortcut.Revision == id {
			return "Media is used by a clip shortcut. Remove that shortcut in live control before deleting."
		}
	}
	b := s.broadcast
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.clip != nil && b.clip.Revision == id {
		return "Media is playing or retained by the current stage. Stop or replace it before deleting."
	}
	for f := b.control.returnTo; f != nil; f = f.returnTo {
		if f.clip != nil && f.clip.Revision == id {
			return "Media is retained for return to a previous stage. Leave that sequence before deleting."
		}
	}
	return ""
}
