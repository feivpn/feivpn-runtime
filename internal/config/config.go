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
	"strings"

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

	// PreferredCountry pins the egress country code parsed from node names
	// (TS naming contract: 2-3 uppercase letters, e.g. "HK", "US", "KOR").
	// Use `feivpnctl countries` to list the codes available in your
	// subscription. Empty value means "let the server's default ordering
	// decide".
	PreferredCountry string `json:"preferred_country,omitempty" yaml:"preferred_country,omitempty"`

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

// SelectNode picks a subscription node for this profile.
//
// Selection rules:
//
//   - PreferredCountry empty → the first node returned by the
//     subscription (server-defined order is the de-facto recommendation).
//   - PreferredCountry set → the first node whose detected country
//     matches, with TS parity ordering: primary nodes first, then backup
//     nodes (names containing "备用"). If no node matches, returns
//     NO_NODES_IN_COUNTRY rather than silently falling back, so the
//     operator notices that their pinned country is not actually
//     available.
//
// PreferredCountry is normalised case-insensitively against ISO alpha-2.
// Unknown codes are rejected up front by the CLI; the daemon-side check
// here is defensive only.
func (p *Profile) SelectNode(nodes []feiapi.SubscriptionNode) (*feiapi.SubscriptionNode, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("NO_NODES_AVAILABLE: subscription is empty")
	}
	want := strings.ToUpper(strings.TrimSpace(p.PreferredCountry))
	if want == "" {
		return &nodes[0], nil
	}
	if !feiapi.IsKnownCountry(want) {
		return nil, fmt.Errorf("INVALID_COUNTRY: %q is not a recognised ISO code; run `feivpnctl countries` to list available options", p.PreferredCountry)
	}
	var primary []*feiapi.SubscriptionNode
	var backup []*feiapi.SubscriptionNode
	for i := range nodes {
		if feiapi.DetectCountry(nodes[i].Name) != want {
			continue
		}
		if feiapi.IsBackupServerName(nodes[i].Name) {
			backup = append(backup, &nodes[i])
		} else {
			primary = append(primary, &nodes[i])
		}
	}
	if len(primary) > 0 {
		return primary[0], nil
	}
	if len(backup) > 0 {
		return backup[0], nil
	}
	return nil, fmt.Errorf("NO_NODES_IN_COUNTRY: subscription has no node tagged %s (%s); run `feivpnctl countries` to see what's available", want, feiapi.CountryDisplayName(want))
}
