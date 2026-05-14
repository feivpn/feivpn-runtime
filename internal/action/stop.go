package action

import (
	"time"

	"github.com/feivpn/feivpn-runtime/internal/logging"
	"github.com/feivpn/feivpn-runtime/internal/router"
)

// Stop performs the graceful shutdown sequence. Order matters because
// the privileged router owns the route table — stopping it before
// `resetRouting` would leak hijacked routes (no default gateway, no
// DNS).
//
//  1. Send `resetRouting` IPC to the running router so it restores the
//     original default route, DNS, IPv6 sysctls, and MSS rule. This
//     must happen BEFORE we stop either service so the router process
//     is still alive to handle the request.
//  2. Ask the platform service manager to stop the user-level daemon.
//     The daemon SHOULD clean up its own state via SIGTERM in the
//     happy path.
//  3. Run `feivpn --recover` to belt-and-braces restore from
//     state.json in case the daemon crashed before SIGTERM.
//  4. Stop the privileged router service. After this the network is
//     guaranteed to be back to the pre-feivpn state.
//
// Every step is best-effort; the result reports per-phase status so
// partial failures are diagnosable.
func (r *Runner) Stop() (*StopResult, error) {
	res := &StopResult{}

	// Phase 0 — reset routing (router process must still be alive)
	if err := router.Reset(5 * time.Second); err != nil {
		res.Errors = append(res.Errors, "router reset: "+err.Error())
		logging.Warn("stop: router reset failed", "err", err)
	}

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
