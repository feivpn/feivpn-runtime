// Package daemon is a thin wrapper around the pinned `feivpn` binary
// from vilizhe/feivpn-apps.
//
// We only model the subset of flags that feivpnctl actually uses, all
// of which are documented in client/protocol/ipc/daemon-args.schema.json.
// Anything outside that surface should be added there first, never here.
package daemon

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/feivpn/feivpn-runtime/internal/binmgr"
)

// Client wraps a Locator to invoke `feivpn` in its various modes.
type Client struct {
	loc *binmgr.Locator
}

// New returns a Client backed by the given binary locator.
func New(loc *binmgr.Locator) *Client { return &Client{loc: loc} }

// HealthReport mirrors daemon-health.schema.json. Only the fields
// feivpnctl currently consumes are modelled; unknown fields round-trip
// through json.RawMessage in Extra.
type HealthReport struct {
	Running      bool       `json:"running"`
	Pid          int        `json:"pid,omitempty"`
	Version      string     `json:"version,omitempty"`
	Uptime       int64      `json:"uptime_seconds,omitempty"`
	Tun          TunInfo    `json:"tun"`
	Route        RouteInfo  `json:"route"`
	DNS          DNSInfo    `json:"dns"`
	Connectivity ConnInfo   `json:"connectivity"`
	Errors       []string   `json:"errors,omitempty"`
}

type TunInfo struct {
	Up   bool   `json:"up"`
	Name string `json:"name,omitempty"`
	Addr string `json:"addr,omitempty"`
}

type RouteInfo struct {
	Default       string `json:"default,omitempty"`
	HijackedByTun bool   `json:"hijacked_by_tun"`
}

type DNSInfo struct {
	Servers   []string `json:"servers,omitempty"`
	Hijacked  bool     `json:"hijacked"`
	Interface string   `json:"interface,omitempty"`
}

type ConnInfo struct {
	EgressIP string `json:"egress_ip,omitempty"`
	Reach    bool   `json:"reach"`
}

// Check runs `feivpn --check -c <config>`. A nil error means the config
// is sane and the daemon is launchable; a non-nil error is a code +
// message pulled from the daemon's stdout/stderr.
func (c *Client) Check(configPath string) error {
	bin, err := c.loc.Locate(binmgr.ComponentFeivpn)
	if err != nil {
		return err
	}
	res, err := binmgr.Spawn(bin, []string{"--check", "-c", configPath}, nil)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("CHECK_FAILED: feivpn --check exit=%d", res.ExitCode)
	}
	return nil
}

// Health runs `feivpn --health` and parses the resulting JSON.
func (c *Client) Health() (*HealthReport, error) {
	bin, err := c.loc.Locate(binmgr.ComponentFeivpn)
	if err != nil {
		return nil, err
	}
	res, err := binmgr.Spawn(bin, []string{"--health"}, nil)
	if err != nil {
		return nil, err
	}
	if len(res.Stdout) == 0 {
		return nil, errors.New("daemon returned empty health payload")
	}
	var h HealthReport
	if err := json.Unmarshal(res.Stdout, &h); err != nil {
		return nil, fmt.Errorf("daemon: parse health JSON: %w", err)
	}
	return &h, nil
}

// Recover runs `feivpn --recover`. Used by `feivpnctl stop` after the
// process has exited (or been killed) to restore the original default
// route + DNS settings recorded in state.json.
func (c *Client) Recover() error {
	bin, err := c.loc.Locate(binmgr.ComponentFeivpn)
	if err != nil {
		return err
	}
	res, err := binmgr.Spawn(bin, []string{"--recover"}, nil)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("RECOVER_FAILED: feivpn --recover exit=%d", res.ExitCode)
	}
	return nil
}

// StartArgs is the argument bundle for a foreground/background daemon launch.
type StartArgs struct {
	ConfigPath string
	TunName    string
	TunAddr    string
	KeyID      string
	LogLevel   string
	LogFile    string
	Extra      []string
}

// SpawnDetached starts the daemon as a long-lived background process
// and returns its PID. Callers should still verify readiness via Health().
func (c *Client) SpawnDetached(a StartArgs) (int, error) {
	bin, err := c.loc.Locate(binmgr.ComponentFeivpn)
	if err != nil {
		return 0, err
	}
	args := []string{}
	if a.ConfigPath != "" {
		args = append(args, "-c", a.ConfigPath)
	}
	if a.TunName != "" {
		args = append(args, "--tunName", a.TunName)
	}
	if a.TunAddr != "" {
		args = append(args, "--tunAddr", a.TunAddr)
	}
	if a.KeyID != "" {
		args = append(args, "--keyID", a.KeyID)
	}
	if a.LogLevel != "" {
		args = append(args, "--logLevel", a.LogLevel)
	}
	args = append(args, a.Extra...)

	pid, err := binmgr.SpawnDetached(bin, args, nil, a.LogFile)
	if err != nil {
		return 0, fmt.Errorf("DAEMON_SPAWN_FAILED: %w", err)
	}
	return pid, nil
}

// BinaryPath exposes the on-disk path of the resolved daemon binary
// (after manifest verification). Useful for status/debug output.
func (c *Client) BinaryPath() (string, error) {
	return c.loc.Locate(binmgr.ComponentFeivpn)
}
