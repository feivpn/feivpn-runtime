// Package action implements the high-level orchestration that the
// FeiVPN Bootstrap Skill exposes:
//
//   ensure_feivpn_ready  → EnsureReady
//   status_feivpn        → Status
//   stop_feivpn          → Stop
//   restart_feivpn       → Restart
//   upgrade_feivpn       → Upgrade
//
// Every action returns a structured result that the CLI prints as JSON
// (machine-readable for the agent) and as a human summary (for humans).
package action

import (
	"github.com/feivpn/feivpn-runtime/internal/binmgr"
	"github.com/feivpn/feivpn-runtime/internal/config"
	"github.com/feivpn/feivpn-runtime/internal/daemon"
	"github.com/feivpn/feivpn-runtime/internal/feiapi"
	"github.com/feivpn/feivpn-runtime/internal/platform"
	"github.com/feivpn/feivpn-runtime/internal/state"
)

// Runner bundles everything an action needs.
//
// Constructed once in cmd/feivpnctl by NewRunner; reused across all
// action calls in a single feivpnctl invocation.
type Runner struct {
	Locator  *binmgr.Locator
	Daemon   *daemon.Client
	Feiapi   *feiapi.Client
	Platform platform.Adapter
	Profile  *config.Profile
	Paths    Paths
}

// Paths captures the canonical install layout. Override via env in tests.
type Paths struct {
	StateFile  string // e.g. /var/lib/feivpn/state.json
	ConfigFile string // path to the daemon's config.json (we render it)
	LogFile    string // path to the daemon's stdout/stderr log
	WorkingDir string // /opt/feivpn
}

// DefaultPaths returns the standard install layout.
func DefaultPaths() Paths {
	return Paths{
		StateFile:  state.DefaultPath(),
		ConfigFile: "/etc/feivpn/config.json",
		LogFile:    "/var/log/feivpn/feivpn.log",
		WorkingDir: "/opt/feivpn",
	}
}

// CheckReport is the per-step diagnostic emitted by ensure_ready.
type CheckReport struct {
	Process      bool `json:"process"`
	Tun          bool `json:"tun"`
	Route        bool `json:"route"`
	DNS          bool `json:"dns"`
	Connectivity bool `json:"connectivity"`
}

// EnsureReadyResult is the contract documented in SKILL.md.
type EnsureReadyResult struct {
	Status   string      `json:"status"` // "ready" | "degraded" | "failed"
	Platform string      `json:"platform"`
	Version  string      `json:"version,omitempty"`
	Pid      int         `json:"pid,omitempty"`
	Tun      string      `json:"tun,omitempty"`
	Checks   CheckReport `json:"checks"`
	Errors   []string    `json:"errors,omitempty"`
}

// StatusResult is the contract for `feivpnctl status` (read-only).
type StatusResult struct {
	Running  bool         `json:"running"`
	Platform string       `json:"platform"`
	Service  ServiceState `json:"service"`
	State    *state.State `json:"state,omitempty"`
	Health   *daemon.HealthReport `json:"health,omitempty"`
}

type ServiceState struct {
	Manager string `json:"manager"` // systemd | launchd
	Active  bool   `json:"active"`
}

// StopResult is the contract for `feivpnctl stop`.
type StopResult struct {
	Stopped  bool     `json:"stopped"`
	Recovery bool     `json:"recovery"`
	Errors   []string `json:"errors,omitempty"`
}
