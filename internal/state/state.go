// Package state mirrors the daemon-state.schema.json contract from
// feivpn/feivpn-apps/client/protocol/ipc/daemon-state.schema.json
//
// The daemon writes this file when it starts and deletes it on graceful
// shutdown; feivpnctl reads it to answer `status` and to drive `stop`.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	defaultLinux  = "/var/lib/feivpn/state.json"
	defaultDarwin = "/var/lib/feivpn/state.json"
)

// DefaultPath returns the canonical state.json location for the current
// host. The caller may override via FEIVPN_STATE_FILE.
func DefaultPath() string {
	if env := os.Getenv("FEIVPN_STATE_FILE"); env != "" {
		return env
	}
	if runtime.GOOS == "darwin" {
		return defaultDarwin
	}
	return defaultLinux
}

// State mirrors daemon-state.schema.json.
type State struct {
	SchemaVersion int            `json:"schema_version"`
	Pid           int            `json:"pid"`
	Version       string         `json:"version"`
	StartedAt     time.Time      `json:"started_at"`
	TunName       string         `json:"tun_name"`
	ConfigPath    string         `json:"config_path,omitempty"`
	KeyID         string         `json:"key_id,omitempty"`
	OriginalRoute *OriginalRoute `json:"original_route,omitempty"`
	OriginalDNS   *OriginalDNS   `json:"original_dns,omitempty"`
}

type OriginalRoute struct {
	Gateway   string `json:"gateway,omitempty"`
	Interface string `json:"interface,omitempty"`
}

type OriginalDNS struct {
	Interface string   `json:"interface,omitempty"`
	Servers   []string `json:"servers,omitempty"`
}

// Read parses the state file. Returns os.ErrNotExist when missing so the
// caller can distinguish "not running" from "broken state".
func Read(path string) (*State, error) {
	if path == "" {
		path = DefaultPath()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("state: parse %s: %w", path, err)
	}
	return &s, nil
}

// Exists is a convenience wrapper around os.Stat.
func Exists(path string) bool {
	if path == "" {
		path = DefaultPath()
	}
	_, err := os.Stat(path)
	return err == nil
}

// Write atomically rewrites the state file. feivpnctl normally does NOT
// write this — the daemon owns it — but the helper is exposed for tests
// and for the upgrade flow where we need to stash state across restarts.
func Write(path string, s *State) error {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// IsAlive returns true iff the recorded PID still corresponds to a live
// process. On Unix we use the kill(0) probe.
func (s *State) IsAlive() bool {
	if s == nil || s.Pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(s.Pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscallZero); err != nil {
		return false
	}
	return true
}

var ErrNotRunning = errors.New("feivpn daemon is not running")
