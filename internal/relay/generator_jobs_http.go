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
	defer g.mu.Unlock()
	draft, err := g.read(input.DesignID)
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
	theme, _ := g.resolveTheme(draft.Theme)
	issues := generationIssues(draft, profile, theme)
	issues = append(issues, g.assetIssues(draft)...)
	issues = append(issues, g.videoIssues(draft, profile.FPS)...)
	issues = append(issues, g.musicIssues(draft, profile.FPS, profile.SampleRate)...)
	issues = append(issues, g.themeIssues(draft.Theme)...)
	if len(issues) > 0 {
		generatorJSON(w, 422, map[string]any{"error": "Resolve design validation before generating.", "issues": issues})
		return
	}
	job, err := newGenerationJob(draft, profile, theme)
	if err != nil {
		http.Error(w, "Cannot create generation identity", 503)
		return
	}
	g.jobsMu.Lock()
	defer g.jobsMu.Unlock()
	g.admitJob(w, job)
}

// admitJob owns durable admission under jobsMu. A failed write consumes no slot.
func (g *generatorStore) admitJob(w http.ResponseWriter, job GenerationJob) {
	outstanding := 0
	for _, j := range g.jobs {
		if j.State == "queued" || j.State == "running" || j.State == "cancelling" {
			outstanding++
		}
	}
	if outstanding >= maxGeneratorOutstanding {
		http.Error(w, "Generation queue is full (8 outstanding jobs). Wait for completion or cancel queued work; your draft is saved.", 409)
		return
	}
	if len(g.jobs) >= maxGeneratorJobs {
		http.Error(w, "Generation history limit reached (200 jobs)", 409)
		return
	}
	job.Sequence = g.sequence + 1
	if g.saveJob(&job) != nil {
		http.Error(w, "Cannot save generation inputs; check storage", 507)
		return
	}
	g.sequence = job.Sequence
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
	view := *j
	if j.State == "queued" {
		for _, other := range g.jobs {
			if other.State == "queued" && other.Sequence <= j.Sequence {
				view.QueuePosition++
			}
		}
	}
	generatorJSON(w, 200, view)
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

func (s *Server) generatorRetryHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.generatorAvailable(w) {
		return
	}
	var body struct{}
	if !generatorDecode(w, r, &body) {
		return
	}
	g := s.generator
	g.mu.Lock()
	defer g.mu.Unlock()
	g.jobsMu.Lock()
	defer g.jobsMu.Unlock()
	previous := g.jobs[r.PathValue("id")]
	if previous == nil {
		http.Error(w, "Generation job not found", 404)
		return
	}
	if previous.State != "failed" && previous.State != "interrupted" && previous.State != "cancelled" {
		http.Error(w, "Only failed, interrupted or cancelled jobs can be retried", 409)
		return
	}
	if previous.Renderer != generatorRenderer {
		http.Error(w, "The captured renderer is unavailable after a server update. This exact retry cannot run. Generate saved draft creates a new job and may include newer edits.", 409)
		return
	}
	if issues := append(g.assetIssues(previous.Design), g.themeIssues(previous.Design.Theme)...); len(issues) > 0 {
		generatorJSON(w, 422, map[string]any{"issues": issues})
		return
	}
	identity, err := newGenerationJob(previous.Design, previous.Profile)
	if err != nil {
		http.Error(w, "Cannot create retry identity", 503)
		return
	}
	// Preserve every captured field, including renderer version and exact duration.
	// Retry does not read the current draft or silently substitute the active profile.
	job := *previous
	job.ID = identity.ID
	job.CreatedAt = identity.CreatedAt
	job.Sequence = 0
	job.State = "queued"
	job.Progress = 0
	job.Error = ""
	job.Message = ""
	job.MediaRevision = ""
	job.MixGain = nil
	job.QueuePosition = 0
	job.RetryOf = previous.ID
	g.admitJob(w, job)
}
