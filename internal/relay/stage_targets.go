package relay

import "errors"

// The shared command handler holds controlMu and broadcast.mu. It validates
// server/context identities before this operation and records its outcome.
func (b *broadcast) applyTargetCommand(cmd StageCommand) (bool, error) {
	if cmd.Enabled == nil {
		return false, errors.New("Choose whether this destination is enabled")
	}
	var target *output
	enabledCount := 0
	for _, candidate := range b.server.outputs {
		status := candidate.snapshot()
		if status.Enabled {
			enabledCount++
		}
		if status.Name == cmd.Target {
			target = candidate
		}
	}
	if target == nil {
		return false, errUnknownTarget
	}
	current := target.snapshot()
	if *cmd.Enabled && !current.CanEnable {
		return false, errTargetUnavailable
	}
	if current.Enabled == *cmd.Enabled {
		return false, nil
	}
	if b.control.stage == "ENDING" && !b.control.failed && *cmd.Enabled {
		return false, errors.New("Ending cannot add destinations; wait for it to finish or confirm Stop now")
	}
	if b.control.stage != "OFF" && b.control.mode == "real" && !cmd.Confirmed {
		if *cmd.Enabled {
			return false, errors.New("Confirm enabling this destination: it immediately joins the current on-air source")
		}
		if enabledCount == 1 {
			return false, errors.New("Confirm disabling the last destination: the show and preview continue, but NO DESTINATIONS — NOT SENDING")
		}
	}
	changed, err := b.server.setTargetLocked(cmd.Target, *cmd.Enabled)
	if err != nil {
		return false, err
	}
	if changed {
		b.control.version++
	}
	return changed, nil
}
