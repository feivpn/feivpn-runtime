package action

import (
	"github.com/feivpn/feivpn-runtime/internal/logging"
)

// Stop performs the graceful shutdown sequence in three phases. Order
// matters because the privileged router owns the route table — stopping
// it before --recover would leak hijacked routes.
//
//  1. Ask the platform service manager to stop the user-level daemon
//     (systemctl stop / launchctl bootout). The daemon SHOULD clean
//     up its own routes/DNS via SIGTERM in the happy path.
//  2. Run `feivpn --recover` to forcibly restore the original default
//     route + DNS settings recorded in state.json (via RPC to the
//     still-running router). Safety net for daemon crashes.
//  3. Stop the privileged router service. After this the network is
//     guaranteed to be back to the pre-feivpn state.
//
// Every step is best-effort; the result reports per-phase status so
// partial failures are diagnosable.
func (r *Runner) Stop() (*StopResult, error) {
	res := &StopResult{}

	// Phase 1 — daemon
	if err := r.Platform.Stop(); err != nil {
		res.Errors = append(res.Errors, "service stop: "+err.Error())
		logging.Warn("stop: service stop failed", "err", err)
	} else {
		res.Stopped = true
	}

	// Phase 2 — recover (router still up so RPC can reach it)
	if err := r.Daemon.Recover(); err != nil {
		// If the binary can't be located we treat that as a soft failure
		// — the network is already restored if the daemon exited cleanly.
		res.Errors = append(res.Errors, "feivpn --recover: "+err.Error())
		logging.Warn("stop: recover failed", "err", err)
	} else {
		res.Recovery = true
	}

	// Phase 3 — router (must be last)
	if err := r.Platform.StopRouter(); err != nil {
		res.Errors = append(res.Errors, "router stop: "+err.Error())
		logging.Warn("stop: router stop failed", "err", err)
	} else {
		res.RouterStopped = true
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
