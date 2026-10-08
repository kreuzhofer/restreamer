package relay

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

type savedState struct {
	Version int             `json:"version"`
	Targets map[string]bool `json:"targets"`
}

func (s *Server) initialize() error {
	s.initOnce.Do(func() {
		s.controlMu.Lock()
		defer s.controlMu.Unlock()
		if err := s.initializeBRB(); err != nil {
			s.initErr = err
			return
		}
		if s.cfg.StateFile == "" {
			return
		}
		f, err := os.Open(s.cfg.StateFile)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			s.initErr = errors.New("cannot read target state file")
			return
		}
		defer f.Close()
		var state savedState
		d := json.NewDecoder(io.LimitReader(f, 64<<10))
		d.DisallowUnknownFields()
		if d.Decode(&state) != nil || d.Decode(new(any)) != io.EOF || state.Version != 1 || state.Targets == nil {
			s.initErr = errors.New("invalid target state file")
			return
		}
		for _, o := range s.outputs {
			if enabled, ok := state.Targets[o.config.Name]; ok {
				o.setEnabled(enabled && o.snapshot().CanEnable)
			}
		}
	})
	return s.initErr
}

var errUnknownTarget = errors.New("unknown target")
var errTargetUnavailable = errors.New("target needs a valid server URL and stream key")
var errSaveState = errors.New("cannot save target state; switch unchanged")

func (s *Server) setTarget(name string, enabled bool) error {
	if err := s.initialize(); err != nil {
		return err
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	var target *output
	state := savedState{Version: 1, Targets: make(map[string]bool)}
	for _, o := range s.outputs {
		state.Targets[o.config.Name] = o.snapshot().Enabled
		if o.config.Name == name {
			target = o
		}
	}
	if target == nil {
		return errUnknownTarget
	}
	if enabled && !target.snapshot().CanEnable {
		return errTargetUnavailable
	}
	state.Targets[name] = enabled
	if s.cfg.StateFile != "" {
		if err := writeState(s.cfg.StateFile, state); err != nil {
			return errSaveState
		}
	}
	if target.snapshot().Enabled != enabled {
		target.setEnabled(enabled)
		s.log.Info("target switch changed", "target", name, "enabled", enabled)
	}
	return nil
}

// Write in the same directory and rename atomically so interrupted saves never
// leave a partially written JSON file. No secrets or metrics are persisted.
func writeState(path string, state any) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".targets-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = json.NewEncoder(f).Encode(state); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
