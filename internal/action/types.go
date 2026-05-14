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
	"time"

	"github.com/feivpn/feivpn-runtime/internal/binmgr"
	"github.com/feivpn/feivpn-runtime/internal/config"
	"github.com/feivpn/feivpn-runtime/internal/daemon"
	"github.com/feivpn/feivpn-runtime/internal/feiapi"
	"github.com/feivpn/feivpn-runtime/internal/platform"
	"github.com/feivpn/feivpn-runtime/internal/router"
	"github.com/feivpn/feivpn-runtime/internal/state"
)

// feiapiPlan is a re-export so the JSON output of `feivpnctl plans`
// stays decoupled from the internal/feiapi import path.
type feiapiPlan = feiapi.Plan

// Runner bundles everything an action needs.
//
// Constructed once in cmd/feivpnctl by NewRunner; reused across all
// action calls in a single feivpnctl invocation.
type Runner struct {
	Locator  *binmgr.Locator
	Daemon   *daemon.Client
	Router   *router.Client
	Feiapi   *feiapi.Client
	Platform platform.Adapter
	Profile  *config.Profile
	Paths    Paths

	// SkipRouting suppresses the configureRouting IPC call in
	// EnsureReady. The daemon + router services are still started
	// (so `feivpnctl status` can verify they came up cleanly), but
	// the host's routing table and DNS are NOT touched. Use this on
	// remote hosts where you need to safely test the install before
	// flipping the box into VPN mode and risking SSH lockout.
	SkipRouting bool

	// ProbeTarget is the "host:port" the post-configureRouting
	// connectivity probe dials through the just-hijacked TUN. If the
	// dial fails inside ProbeTimeout, EnsureReady automatically calls
	// router.Reset() to restore the original routes BEFORE the user
	// is locked out. Empty value falls back to a sensible public
	// endpoint (1.1.1.1:443).
	ProbeTarget  string
	ProbeTimeout time.Duration
}

// Paths captures the canonical install layout. Override via env in tests.
type Paths struct {
	StateFile     string // e.g. /var/lib/feivpn/state.json
	ConfigFile    string // path to the daemon's config.json (we render it)
	LogFile       string // path to the daemon's stdout/stderr log
	RouterLogFile string // path to the C++ router's stdout/stderr log
	WorkingDir    string // /opt/feivpn
}

// DefaultPaths returns the standard install layout.
func DefaultPaths() Paths {
	return Paths{
		StateFile:     state.DefaultPath(),
		ConfigFile:    "/etc/feivpn/config.json",
		LogFile:       "/var/log/feivpn/feivpn.log",
		RouterLogFile: "/var/log/feivpn/router.log",
		WorkingDir:    "/opt/feivpn",
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
	Running  bool                 `json:"running"`
	Platform string               `json:"platform"`
	Service  ServiceState         `json:"service"`
	Router   ServiceState         `json:"router"`
	State    *state.State         `json:"state,omitempty"`
	Health   *daemon.HealthReport `json:"health,omitempty"`
}

type ServiceState struct {
	Manager string `json:"manager"` // systemd | launchd
	Active  bool   `json:"active"`
}

// StopResult is the contract for `feivpnctl stop`.
//
// The flow is: stop daemon → run --recover (clean routes/DNS) → stop
// router. Each phase reports its own success bit so partial-failure
// scenarios are diagnosable.
type StopResult struct {
	Stopped       bool     `json:"stopped"`        // daemon service stopped
	Recovery      bool     `json:"recovery"`       // feivpn --recover ran ok
	RouterStopped bool     `json:"router_stopped"` // router service stopped
	Errors        []string `json:"errors,omitempty"`
}

// AccountResult is the contract for `feivpnctl getid` / `login` /
// `register` / `logout` / `whoami`. Token + AuthData are the two
// strings the rest of the system needs; everything else is
// informational.
type AccountResult struct {
	Status       string `json:"status"` // "ok" | "logged_out" | "stale"
	Email        string `json:"email,omitempty"`
	UUID         string `json:"uuid,omitempty"`
	IsNew        bool   `json:"is_new,omitempty"`
	SubscribeURL string `json:"subscribe_url,omitempty"`
	Token        string `json:"token,omitempty"`
	AuthData     string `json:"auth_data,omitempty"`
	ExpiredAt    *int64 `json:"expired_at,omitempty"`
	UsageBalance *int64 `json:"usage_time_balance,omitempty"`

	// Notice is a one-line human-readable hint surfaced on stderr by
	// the CLI (and visible to agents in the JSON). Populated for
	// transient events worth flagging — most importantly the new-user
	// trial when IsNew is true.
	Notice string `json:"notice,omitempty"`
}

// SimpleResult is the generic ok / error contract used by stateless
// actions (logout, change-password).
type SimpleResult struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// CheckUpgradeResult is the contract for `feivpnctl check-upgrade`.
//
// It compares the locally pinned daemon version (manifest.feivpn.version)
// against /api/v1/version/check?platform=<skill-upgrade tag>. The
// platform tag is auto-detected from the host and is **distinct from
// the desktop client's namespace**:
//
//   linux  → feivpn-runtime-linux
//   darwin → feivpn-runtime-mac
//
// The desktop client uses bare "linux"/"mac"/"win"/…; reusing those
// would silently return desktop versions whenever the skill polled.
// See internal/host.Info.SkillUpgradeTag for the full rationale.
type CheckUpgradeResult struct {
	Status          string `json:"status"` // "ok" | "stale" | "failed"
	Component       string `json:"component"`        // always "feivpn" today
	Host            string `json:"host"`             // friendly label, e.g. "macOS Apple Silicon"
	Platform        string `json:"platform"`         // skill-upgrade tag, e.g. "feivpn-runtime-linux" | "feivpn-runtime-mac"
	Architecture    string `json:"architecture"`     // amd64 | arm64
	ManifestKey     string `json:"manifest_key"`     // linux-amd64 | darwin-arm64 | ...
	CurrentVersion  string `json:"current_version"`
	RemoteVersion   string `json:"remote_version,omitempty"`
	NeedsUpgrade    bool   `json:"needs_upgrade"`
	ForceUpdate     bool   `json:"force_update,omitempty"`
	Changelog       string `json:"changelog,omitempty"`
	UpdateURL       string `json:"update_url,omitempty"` // upstream URL the server suggests; informational only

	// Upgrade is the machine-readable upgrade plan. Populated whenever
	// NeedsUpgrade is true (and we know what to upgrade to). Agents
	// should execute Upgrade.Command verbatim as root and not try to
	// reconstruct it themselves.
	Upgrade *UpgradePlan `json:"upgrade,omitempty"`

	Instruction string `json:"instruction,omitempty"`
	Notice      string `json:"notice,omitempty"`
	Error       string `json:"error,omitempty"`
}

// UpgradePlan is the actionable subdocument inside CheckUpgradeResult.
//
// Two layers, both included so callers can pick:
//   - Command: a single shell line that does the full end-user upgrade
//     (install + restart). Agents should prefer this.
//   - Steps: the same flow split into install-then-restart for callers
//     that want to drive each phase independently (e.g. install in a
//     maintenance window, restart later).
type UpgradePlan struct {
	InstallerURL string   `json:"installer_url"` // raw.githubusercontent.com URL of install.sh
	TargetTag    string   `json:"target_tag"`    // e.g. "v0.2.0"; matches GitHub Release tag
	Command      string   `json:"command"`       // single-line, root-required
	Steps        []string `json:"steps"`         // the same thing as 2 commands
	RequiresRoot bool     `json:"requires_root"` // always true today
}

// PlansResult is the contract for `feivpnctl plans`.
type PlansResult struct {
	Status        string        `json:"status"`
	Authenticated bool          `json:"authenticated"`
	Count         int           `json:"count"`
	Plans         []feiapiPlan  `json:"plans"`
	RechargeURL   string        `json:"recharge_url,omitempty"`
}

// RechargeResult is the contract for `feivpnctl recharge`.
type RechargeResult struct {
	Status      string `json:"status"`
	URL         string `json:"url"`
	Opened      bool   `json:"opened"`
	OpenCommand string `json:"open_command,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

// TestResult is the contract for `feivpnctl test`.
type TestResult struct {
	Status         string         `json:"status"`
	EgressIP       string         `json:"egress_ip,omitempty"`
	EgressIPVia    string         `json:"egress_ip_via,omitempty"`
	DNS            DNSProbeResult `json:"dns"`
	TUN            TUNProbeResult `json:"tun"`
	Reachability   []ReachProbe   `json:"reachability"`
	LatencyMS      int64          `json:"egress_latency_ms,omitempty"`
	Errors         []string       `json:"errors,omitempty"`
}

type DNSProbeResult struct {
	Servers  []string `json:"servers,omitempty"`
	Resolved string   `json:"resolved,omitempty"`
	OK       bool     `json:"ok"`
}

type TUNProbeResult struct {
	Up   bool   `json:"up"`
	Name string `json:"name,omitempty"`
}

type ReachProbe struct {
	Target string `json:"target"`
	OK     bool   `json:"ok"`
	Status int    `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}
