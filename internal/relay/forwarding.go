package relay

// Master forwarding deliberately has no on-disk representation. Every new
// process starts closed, including when saved target switches are enabled.
func (s *Server) setForwarding(enabled bool) {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if s.forwarding.Swap(enabled) == enabled {
		return
	}
	for _, o := range s.outputs {
		o.mu.Lock()
		o.blocked = !enabled
		o.failed = false
		o.status.RetryAt = 0
		switch o.status.State {
		case "idle", "disabled", "paused":
			o.status.State = o.settledState()
		default:
			if !enabled {
				o.status.State = "stopping"
			}
		}
		o.mu.Unlock()
		select {
		case o.changed <- struct{}{}:
		default:
		}
	}
	s.log.Info("master forwarding changed", "enabled", enabled)
}
