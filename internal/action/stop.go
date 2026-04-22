package action

import (
	"github.com/feivpn/feivpn-runtime/internal/logging"
)

// Stop performs the graceful shutdown sequence:
//
//   1. Ask the platform service manager to stop the daemon
//      (systemctl stop / launchctl bootout). The daemon should clean
//      up its own routes/DNS via SIGTERM in the happy path.
//   2. Run `feivpn --recover` to forcibly restore the original default
//      route + DNS settings recorded in state.json. This is the safety
//      net for the case where the daemon crashed and could not clean
//      up after itself.
//
// Both steps are best-effort; the result reports per-step status.
func (r *Runner) Stop() (*StopResult, error) {
	res := &StopResult{}

	if err := r.Platform.Stop(); err != nil {
		res.Errors = append(res.Errors, "service stop: "+err.Error())
		logging.Warn("stop: service stop failed", "err", err)
	} else {
		res.Stopped = true
	}

	if err := r.Daemon.Recover(); err != nil {
		// If the binary can't be located we treat that as a soft failure
		// — the network is already restored if the daemon exited cleanly.
		res.Errors = append(res.Errors, "feivpn --recover: "+err.Error())
		logging.Warn("stop: recover failed", "err", err)
	} else {
		res.Recovery = true
	}

	return res, nil
}

// Restart is Stop + EnsureReady in sequence. We deliberately reuse
// EnsureReady (rather than just `systemctl restart`) so that an upgraded
// manifest, profile change, or new subscription node is picked up.
func (r *Runner) Restart() (*EnsureReadyResult, error) {
	if _, err := r.Stop(); err != nil {
		logging.Warn("restart: stop phase reported errors", "err", err)
	}
	return r.EnsureReady()
}
