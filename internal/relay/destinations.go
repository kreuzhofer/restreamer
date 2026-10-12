package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kreuzhofer/restreamer/internal/config"
)

type destinationState struct {
	Version uint64          `json:"version"`
	Targets []config.Target `json:"targets"`
}

type destinationView struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	HasKey  bool   `json:"has_key"`
	Enabled bool   `json:"enabled"`
	Ready   bool   `json:"ready"`
	Reason  string `json:"reason,omitempty"`
}

type destinationChange struct {
	Version   uint64  `json:"version"`
	Name      string  `json:"name"`
	URL       string  `json:"url"`
	StreamKey *string `json:"stream_key,omitempty"`
	Confirmed bool    `json:"confirmed"`
	Context   string  `json:"context"`
	ServerID  string  `json:"server_id"`
}

func (s *Server) outputSnapshot() []*output {
	s.outputsMu.RLock()
	defer s.outputsMu.RUnlock()
	return append([]*output{}, s.outputs...)
}

func (s *Server) newOutput(target config.Target) *output {
	status := OutputStatus{Name: target.Name, Enabled: target.IsEnabled(), State: "paused", CanEnable: target.ReadyError() == nil}
	if !status.Enabled {
		status.State = "disabled"
	}
	if err := target.ReadyError(); err != nil {
		status.UnavailableReason = err.Error()
	}
	return &output{config: target, log: s.log, changed: make(chan struct{}, 1), status: status, blocked: true}
}

// Caller holds controlMu. Workers share the bounded broadcast hub but retain
// independent sessions and cancellation; editing one cannot restart its peers.
func (s *Server) startOutputLocked(o *output) {
	if s.outputContext == nil {
		return
	}
	if s.outputCancels == nil {
		s.outputCancels = make(map[*output]context.CancelFunc)
		s.outputDone = make(map[*output]chan struct{})
	}
	ctx, cancel := context.WithCancel(s.outputContext)
	s.outputCancels[o] = cancel
	done := make(chan struct{})
	s.outputDone[o] = done
	s.outputWorkers.Add(1)
	go func() { defer s.outputWorkers.Done(); defer close(done); o.manage(ctx, s.broadcast.hub) }()
}

func (s *Server) destinationsPath() string {
	if s.cfg.DestinationsFile != "" {
		return s.cfg.DestinationsFile
	}
	if s.cfg.StateFile != "" {
		return filepath.Join(filepath.Dir(s.cfg.StateFile), "destinations.json")
	}
	return ""
}

func (s *Server) initializeDestinations() error {
	path := s.destinationsPath()
	if path == "" {
		return nil
	}
	if s.cfg.StateFile != "" && filepath.Clean(path) == filepath.Clean(s.cfg.StateFile) {
		return errors.New("destination credentials and switch state require separate files")
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("cannot read destination credentials")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("destination credentials must be a private file (mode 0600)")
	}
	var saved destinationState
	d := json.NewDecoder(io.LimitReader(f, 128<<10))
	d.DisallowUnknownFields()
	if d.Decode(&saved) != nil || d.Decode(new(any)) != io.EOF || saved.Version == 0 || len(saved.Targets) > 16 {
		return errors.New("invalid destination credentials file")
	}
	names := map[string]bool{}
	for _, target := range saved.Targets {
		if validateDestination(target, !target.IsEnabled()) != nil || names[target.Name] {
			return errors.New("invalid destination credentials file")
		}
		names[target.Name] = true
	}
	s.destinations = saved
	s.destinationsManaged = true
	s.outputs = nil
	for _, target := range saved.Targets {
		s.outputs = append(s.outputs, s.newOutput(target))
	}
	return nil
}

var destinationName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func validateDestination(target config.Target, allowUnavailable bool) error {
	if !destinationName.MatchString(target.Name) {
		return errors.New("Use a unique destination name with 1–64 letters, digits, underscores or hyphens")
	}
	if len(target.URL) > 2048 || len(target.StreamKey) > 2048 || strings.ContainsAny(target.StreamKey, "\r\n\x00") {
		return errors.New("Invalid destination URL or key")
	}
	check := target
	check.StreamKey = "validation"
	if err := check.ReadyError(); err != nil && !allowUnavailable {
		return err
	}
	if target.Enabled != "" && target.Enabled != "true" && target.Enabled != "false" {
		return errors.New("Invalid enabled preference")
	}
	return nil
}

func (s *Server) destinationResponse(w http.ResponseWriter) {
	views := []destinationView{}
	for _, o := range s.outputSnapshot() {
		status := o.snapshot()
		views = append(views, destinationView{Name: o.config.Name, URL: o.config.URL, HasKey: strings.TrimSpace(o.config.StreamKey) != "", Enabled: status.Enabled, Ready: status.CanEnable, Reason: status.UnavailableReason})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Version      uint64            `json:"version"`
		Managed      bool              `json:"managed"`
		Available    bool              `json:"available"`
		Destinations []destinationView `json:"destinations"`
	}{s.destinations.Version, s.destinationsManaged, s.destinationsPath() != "", views})
}

func (s *Server) destinationsHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		s.controlMu.Lock()
		defer s.controlMu.Unlock()
		s.destinationResponse(w)
		return
	}
	typ, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !controlOriginAllowed(r) || typ != "application/json" {
		http.Error(w, "Destination changes require same-origin JSON", 403)
		return
	}
	var change destinationChange
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if d.Decode(&change) != nil || d.Decode(new(any)) != io.EOF {
		http.Error(w, "Expected one destination change", 400)
		return
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	b := s.broadcast
	b.mu.Lock()
	defer b.mu.Unlock()
	if s.destinationsPath() == "" {
		http.Error(w, "Configure persistent destination storage first", 503)
		return
	}
	if change.Version != s.destinations.Version {
		http.Error(w, "Destinations changed; reload before saving", 409)
		return
	}
	if b.control.stage == "ENDING" {
		http.Error(w, "Wait for Ending to finish or confirm Stop now before managing destinations", 409)
		return
	}
	if !s.destinationsManaged && len(s.outputSnapshot()) > 0 && r.URL.Path != "/api/destinations/import" {
		http.Error(w, "Import legacy destinations before editing", 409)
		return
	}
	next := destinationState{Version: s.destinations.Version + 1, Targets: []config.Target{}}
	for _, o := range s.outputSnapshot() {
		target := o.config
		if o.snapshot().Enabled {
			target.Enabled = "true"
		} else {
			target.Enabled = "false"
		}
		next.Targets = append(next.Targets, target)
	}
	changedName := r.PathValue("name")
	if r.URL.Path == "/api/destinations/import" {
		if s.destinationsManaged {
			http.Error(w, "Destinations have already been imported", 409)
			return
		}
		if b.control.stage != "OFF" {
			http.Error(w, "Stop the broadcast or rehearsal before importing destinations", 409)
			return
		}
	} else {
		index := -1
		for i, target := range next.Targets {
			if target.Name == changedName {
				index = i
			}
		}
		if r.Method != "POST" && index < 0 {
			http.Error(w, "Unknown destination", 404)
			return
		}
		if index >= 0 && next.Targets[index].IsEnabled() && b.control.mode == "real" && b.control.stage != "OFF" {
			if !change.Confirmed || change.ServerID != b.control.serverID || change.Context != b.stageContext() {
				http.Error(w, "Confirm this change against the current broadcast: this destination will disconnect; removing the last destination stops delivery while the show and preview continue", 409)
				return
			}
		}
		if r.Method == "DELETE" {
			next.Targets = append(next.Targets[:index], next.Targets[index+1:]...)
		} else {
			target := config.Target{Name: change.Name, URL: change.URL, Enabled: "false"}
			if index >= 0 {
				target.StreamKey = next.Targets[index].StreamKey
				target.Enabled = next.Targets[index].Enabled
			}
			if change.StreamKey != nil {
				target.StreamKey = *change.StreamKey
			}
			if strings.TrimSpace(target.StreamKey) == "" {
				target.Enabled = "false"
			}
			if index < 0 {
				next.Targets = append(next.Targets, target)
			} else {
				next.Targets[index] = target
			}
		}
	}
	if len(next.Targets) > 16 {
		http.Error(w, "At most 16 destinations are supported", 400)
		return
	}
	names := map[string]bool{}
	for _, target := range next.Targets {
		if err := validateDestination(target, !target.IsEnabled() && (r.URL.Path == "/api/destinations/import" || r.Method == "DELETE" || target.Name != change.Name)); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if names[target.Name] {
			http.Error(w, "Destination names must be unique", 400)
			return
		}
		names[target.Name] = true
	}
	if err := writeState(s.destinationsPath(), next); err != nil {
		http.Error(w, "Cannot save destinations; previous configuration retained", 500)
		return
	}
	// Commit after persistence. Reuse every unchanged worker and immediately block
	// admission to a replaced worker before canceling its connection.
	old := s.outputSnapshot()
	outputs := []*output{}
	for _, target := range next.Targets {
		var found *output
		for _, o := range old {
			if o.config.Name == target.Name && o.config.URL == target.URL && o.config.StreamKey == target.StreamKey {
				found = o
				break
			}
		}
		if found == nil {
			found = s.newOutput(target)
			found.blocked = !s.forwarding.Load()
		}
		outputs = append(outputs, found)
	}
	for _, o := range old {
		retained := false
		for _, keep := range outputs {
			if keep == o {
				retained = true
			}
		}
		if !retained {
			o.setEnabled(false)
			if cancel := s.outputCancels[o]; cancel != nil {
				cancel()
				<-s.outputDone[o]
				delete(s.outputCancels, o)
				delete(s.outputDone, o)
			}
		}
	}
	for _, o := range outputs {
		if s.outputCancels[o] == nil {
			s.startOutputLocked(o)
		}
	}
	s.outputsMu.Lock()
	s.outputs = outputs
	s.outputsMu.Unlock()
	b.hub.mu.Lock()
	b.hub.outputs = append([]*output{}, outputs...)
	b.hub.mu.Unlock()
	s.destinations, s.destinationsManaged = next, true
	b.control.version++
	// Completed ending evidence remains historical and is intentionally unchanged.
	s.destinationResponse(w)
}
