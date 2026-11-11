package relay

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/kreuzhofer/restreamer/internal/config"
)

func (s *Server) eventOperatorHTTP(w http.ResponseWriter, r *http.Request) {
	e := s.event(r.PathValue("id"))
	if e == nil {
		http.NotFound(w, r)
		return
	}
	e.mu.Lock()
	if e.State == "deleted" || e.unavailable {
		e.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	resource := r.PathValue("resource")
	if resource == "" {
		e.mu.Unlock()
		data, _ := dashboardFiles.ReadFile("web/event.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
		return
	}
	if resource == "status" {
		view := e.view()
		view["published"] = len(e.Segments) > 0
		view["state"] = "ended"
		view["preview"] = true
		e.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(view)
		return
	}
	s.eventMediaHTTP(w, r, e, resource, true)
}

func (s *Server) eventDeleteHTTP(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version   uint64 `json:"version"`
		Confirmed bool   `json:"confirmed"`
	}
	if !eventJSON(w, r, &input) {
		return
	}
	if !input.Confirmed {
		http.Error(w, "Confirm deleting the event, its recording, and its public URL", 409)
		return
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	e := s.event(r.PathValue("id"))
	if e == nil {
		http.NotFound(w, r)
		return
	}
	e.mu.Lock()
	if e.Version != input.Version {
		e.mu.Unlock()
		http.Error(w, "Event changed; refresh before deleting", 409)
		return
	}
	if e.State == "live" || e.State == "waiting" && e.begun.Load() {
		e.mu.Unlock()
		http.Error(w, "Finish the event before deleting it", 409)
		return
	}
	oldState, oldPublished := e.State, e.Published
	e.State, e.Published = "deleted", false
	e.Version++
	if err := e.save(); err != nil {
		e.State, e.Published = oldState, oldPublished
		e.Version--
		e.mu.Unlock()
		http.Error(w, "Cannot revoke event; nothing was deleted", 500)
		return
	}
	e.mu.Unlock()
	// A persisted tombstone revokes access before removing references or files.
	// Any interrupted cleanup remains fail-closed and can be retried.
	s.broadcast.mu.Lock()
	broadcastLocked := true
	defer func() {
		if broadcastLocked {
			s.broadcast.mu.Unlock()
		}
	}()
	next := destinationState{Version: s.destinations.Version + 1, Targets: []config.Target{}}
	kept := []*output{}
	for _, o := range s.outputSnapshot() {
		if o.config.EventID == e.ID {
			continue
		}
		kept = append(kept, o)
		target := o.config
		if o.snapshot().Enabled {
			target.Enabled = "true"
		} else {
			target.Enabled = "false"
		}
		next.Targets = append(next.Targets, target)
	}
	if err := writeState(s.destinationsPath(), next); err != nil {
		http.Error(w, "Event revoked; destination cleanup failed. Retry Delete event", 500)
		return
	}
	for _, o := range s.outputSnapshot() {
		if o.config.EventID == e.ID {
			o.setEnabled(false)
			if cancel := s.outputCancels[o]; cancel != nil {
				cancel()
				s.broadcast.mu.Unlock()
				<-s.outputDone[o]
				s.broadcast.mu.Lock()
				delete(s.outputCancels, o)
				delete(s.outputDone, o)
			}
		}
	}
	s.destinations = next
	s.destinationsManaged = true
	s.outputsMu.Lock()
	s.outputs = kept
	s.outputsMu.Unlock()
	s.broadcast.hub.mu.Lock()
	s.broadcast.hub.outputs = s.pathOutputs("passthrough")
	s.broadcast.hub.mu.Unlock()
	s.broadcast.continuous.hub.mu.Lock()
	s.broadcast.continuous.hub.outputs = s.pathOutputs("continuous")
	s.broadcast.continuous.hub.mu.Unlock()
	s.broadcast.control.version++
	s.broadcast.mu.Unlock()
	broadcastLocked = false
	trash := filepath.Join(s.cfg.EventsDirectory, ".deleted-"+e.ID)
	if err := os.Rename(e.dir, trash); err != nil {
		http.Error(w, "Event revoked; recording cleanup failed. Retry Delete event", 500)
		return
	}
	if err := os.RemoveAll(trash); err != nil {
		http.Error(w, "Event revoked; recording cleanup will retry on restart", 500)
		return
	}
	s.eventsMu.Lock()
	delete(s.events, e.ID)
	s.eventsMu.Unlock()
	w.WriteHeader(204)
}
