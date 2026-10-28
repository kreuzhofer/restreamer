package relay

import "net/http"

// Names describe a retained video; they never change captured inputs or media.
func (s *Server) generatorJobName(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !generatorDecode(w, r, &input) {
		return
	}
	if !validAssetName(input.Name) {
		http.Error(w, "Choose a video name up to 180 bytes", 422)
		return
	}
	g := s.generator
	g.jobsMu.Lock()
	defer g.jobsMu.Unlock()
	job := g.jobs[r.PathValue("id")]
	if job == nil {
		http.Error(w, "Video not found", 404)
		return
	}
	next := *job
	next.Name = input.Name
	if g.saveJob(&next) != nil {
		http.Error(w, "Cannot save video name", 507)
		return
	}
	job.Name = next.Name
	generatorJSON(w, 200, job)
}
