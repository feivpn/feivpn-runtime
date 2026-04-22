// Package config models feivpnctl's *user-facing* config (passed to the
// CLI / written under /etc/feivpn/feivpnctl.yaml) and renders the
// per-launch daemon arguments from it.
//
// We deliberately do not parse the daemon's own config.json here —
// the daemon owns its schema. Our job is to pick a node from the
// subscription returned by `feiapi getconfig` and translate it into
// the right CLI flags.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/feivpn/feivpn-runtime/internal/feiapi"
)

// Profile is the user-facing config describing how feivpnctl should
// keep this machine "ready".
type Profile struct {
	// Channel is one of "stable" / "beta", used purely for telemetry.
	Channel string `json:"channel,omitempty" yaml:"channel"`

	// Mode controls the routing scope. Only "global" is implemented in
	// the MVP; "rule-based" / "tun-only" are reserved.
	Mode string `json:"mode" yaml:"mode"`

	// SubscriptionToken authenticates against the backend API.
	// Sourced from `feiapi getid` -> `info.subscriptionToken`, or
	// supplied directly by the operator.
	SubscriptionToken string `json:"subscription_token" yaml:"subscription_token"`

	// PreferredNode is an optional substring match against
	// SubscriptionNode.Name to pin a specific egress.
	PreferredNode string `json:"preferred_node,omitempty" yaml:"preferred_node,omitempty"`

	// TunName, TunAddr override defaults (utunN on darwin, fei0 on linux).
	TunName string `json:"tun_name,omitempty" yaml:"tun_name,omitempty"`
	TunAddr string `json:"tun_addr,omitempty" yaml:"tun_addr,omitempty"`

	// LogLevel: debug / info / warn / error.
	LogLevel string `json:"log_level,omitempty" yaml:"log_level,omitempty"`
}

// DefaultPath is where feivpnctl looks for the user-facing profile.
func DefaultPath() string {
	if env := os.Getenv("FEIVPNCTL_CONFIG"); env != "" {
		return env
	}
	return "/etc/feivpn/feivpnctl.json"
}

// Load reads + parses the profile. Missing file → returns ({} , nil) so
// that callers can still drive the CLI purely via flags.
func Load(path string) (*Profile, error) {
	if path == "" {
		path = DefaultPath()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Profile{}, nil
		}
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return &p, nil
}

// Save writes the profile to disk atomically.
func Save(path string, p *Profile) error {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SelectNode picks a subscription node for this profile. Selection is
// preferred-name substring → first-available, in that order.
func (p *Profile) SelectNode(nodes []feiapi.SubscriptionNode) (*feiapi.SubscriptionNode, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("NO_NODES_AVAILABLE: subscription is empty")
	}
	if p.PreferredNode != "" {
		for i := range nodes {
			if containsCI(nodes[i].Name, p.PreferredNode) {
				return &nodes[i], nil
			}
		}
	}
	return &nodes[0], nil
}

func containsCI(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(haystack) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		eq := true
		for j := 0; j < len(needle); j++ {
			a, b := haystack[i+j], needle[j]
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if b >= 'A' && b <= 'Z' {
				b += 32
			}
			if a != b {
				eq = false
				break
			}
		}
		if eq {
			return true
		}
	}
	return false
}
