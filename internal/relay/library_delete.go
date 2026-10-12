package relay

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Library originals have their own explicit deletion boundary. Generated results
// and authoring uploads retain their existing ownership and cleanup paths.
func (s *Server) libraryDelete(w http.ResponseWriter, r *http.Request) {
	l := s.library
	if l == nil {
		http.Error(w, "Enable BRB to manage the video library", 409)
		return
	}
	var input struct {
		Version string `json:"version"`
	}
	if !generatorDecode(w, r, &input) {
		return
	}
	if !l.uploadMu.TryLock() {
		http.Error(w, "Wait for the current upload before deleting", 409)
		return
	}
	defer l.uploadMu.Unlock()
	l.scanMu.Lock()
	defer l.scanMu.Unlock()
	if s.generator != nil {
		s.generator.jobsMu.Lock()
		defer s.generator.jobsMu.Unlock()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	id := r.PathValue("id")
	entry := l.entries[id]
	if entry == nil {
		http.Error(w, "Library upload not found", 404)
		return
	}
	if input.Version != clipHash(entry.stamp) {
		http.Error(w, "Upload changed; refresh Media before deleting", 409)
		return
	}
	if l.jobID == id || entry.State == "preparing" {
		http.Error(w, "Wait for preparation to finish before deleting", 409)
		return
	}
	revisions := []string{}
	for revision, media := range l.revisions {
		if media.LibraryID != id && revision != entry.Revision {
			continue
		}
		if reason := s.mediaDeletionBlockLocked(revision); reason != "" {
			http.Error(w, reason, 409)
			return
		}
		for otherID, other := range l.entries {
			if otherID != id && other.Revision == revision {
				http.Error(w, "Prepared media is shared with another library upload", 409)
				return
			}
		}
		if s.generator != nil {
			for _, job := range s.generator.jobs {
				if job.MediaRevision == revision {
					http.Error(w, "Prepared media is referenced by generation history", 409)
					return
				}
			}
		}
		revisions = append(revisions, revision)
	}
	paths := []string{"originals/" + entry.Name}
	files, err := os.ReadDir(filepath.Join(l.root, "prepared"))
	if err != nil {
		http.Error(w, "Cannot inspect prepared files; check storage", 507)
		return
	}
	for _, file := range files {
		if strings.HasPrefix(file.Name(), id+"-") && strings.HasSuffix(file.Name(), ".flv") {
			paths = append(paths, "prepared/"+file.Name())
		}
	}
	for _, revision := range revisions {
		paths = append(paths, "revisions/"+revision+".json", "revisions/"+revision+".flv")
	}
	committed, err := deleteMediaFiles(l.root, paths)
	if committed {
		delete(l.entries, id)
		for _, revision := range revisions {
			delete(l.revisions, revision)
		}
		s.broadcast.mu.Lock()
		s.broadcast.control.version++
		s.broadcast.mu.Unlock()
	}
	if err != nil {
		http.Error(w, "Deletion storage error; refresh Media to check the result. Restart retries interrupted cleanup.", 507)
		return
	}
	generatorJSON(w, 200, map[string]any{"deleted": true, "deleted_media": len(revisions)})
}
