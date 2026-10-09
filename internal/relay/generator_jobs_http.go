package relay

import (
	"errors"
	"net/http"
	"os"
)

func (s *Server) generatorJobsHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	g := s.generator
	if r.Method == "GET" {
		generatorJSON(w, 200, g.jobList())
		return
	}
	var input struct {
		DesignID string `json:"design_id"`
		Version  int    `json:"version"`
	}
	if !generatorDecode(w, r, &input) {
		return
	}
	// Lock the acknowledged draft only long enough to capture and validate it.
	g.mu.Lock()
	draft, err := g.read(input.DesignID)
	g.mu.Unlock()
	if errors.Is(err, os.ErrNotExist) {
		http.Error(w, "Design not found", 404)
		return
	}
	if err != nil {
		http.Error(w, "Cannot read saved design; check storage", 503)
		return
	}
	if draft.Version != input.Version {
		http.Error(w, "The saved draft changed. Reload or save your edits before generating.", 409)
		return
	}
	s.library.mu.Lock()
	profile := s.library.profile
	s.library.mu.Unlock()
	if issues := generationIssues(draft, profile); len(issues) > 0 {
		generatorJSON(w, 422, map[string]any{"error": "Resolve design validation before generating.", "issues": issues})
		return
	}
	job, err := newGenerationJob(draft, profile)
	if err != nil {
		http.Error(w, "Cannot create generation identity", 503)
		return
	}
	g.jobsMu.Lock()
	defer g.jobsMu.Unlock()
	for _, j := range g.jobs {
		if j.State == "queued" || j.State == "running" || j.State == "cancelling" {
			http.Error(w, "Generation is busy. Wait for the current job or cancel it.", 409)
			return
		}
	}
	if len(g.jobs) >= maxGeneratorJobs {
		http.Error(w, "Generation history limit reached (200 jobs)", 409)
		return
	}
	if g.saveJob(&job) != nil {
		http.Error(w, "Cannot save generation inputs; check storage", 507)
		return
	}
	g.jobs[job.ID] = &job
	select {
	case g.wake <- struct{}{}:
	default:
	}
	generatorJSON(w, 202, job)
}
func (s *Server) generatorJobHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	g := s.generator
	g.jobsMu.Lock()
	defer g.jobsMu.Unlock()
	j := g.jobs[r.PathValue("id")]
	if j == nil {
		http.Error(w, "Generation job not found", 404)
		return
	}
	generatorJSON(w, 200, j)
}
func (s *Server) generatorCancelHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var body struct{}
	if !generatorDecode(w, r, &body) {
		return
	}
	g := s.generator
	g.jobsMu.Lock()
	defer g.jobsMu.Unlock()
	j := g.jobs[r.PathValue("id")]
	if j == nil {
		http.Error(w, "Generation job not found", 404)
		return
	}
	previous := *j
	switch j.State {
	case "queued":
		j.State = "cancelled"
		j.Error = "Generation cancelled; previous media is unchanged."
	case "running":
		j.State = "cancelling"
	case "cancelling", "cancelled":
		generatorJSON(w, 200, j)
		return
	default:
		http.Error(w, "Only queued or running generation can be cancelled", 409)
		return
	}
	if g.saveJob(j) != nil {
		*j = previous
		http.Error(w, "Cancellation not saved; check storage and retry", 507)
		return
	}
	if j.State == "cancelling" && g.cancel != nil {
		g.cancel()
	}
	generatorJSON(w, 202, j)
}
