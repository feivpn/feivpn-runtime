// Package store is feivpnctl's on-disk persistence — one tiny JSON
// file that mirrors the server's UserData payload (the same shape
// returned by /getid, /user/info, /passport/auth/login,
// /passport/auth/bind).
//
// Design notes
//
//   - One file: $FEIVPN_ACCOUNT_FILE (default /var/lib/feivpn/account.json),
//     mode 0600.
//   - The file is the *current device session*. It always exists after
//     the first successful `feivpnctl getid` (or login/register). It is
//     never deleted by ordinary commands; `logout` re-runs getid to
//     replace the named-account fields with the anonymous baseline.
//   - There is no separate `fingerprint` file. The persistent device
//     identity comes from the OS itself (see internal/device); we never
//     mint a random one of our own — that would let one machine show up
//     as N devices on the backend.
//   - Logged-in state = `auth_data` non-empty. This matches the TS
//     client's auth_manager.ts.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Account mirrors the server's UserData payload (see
// feivpn-apps/client/go/api/endpoints/types.go). The server reuses the
// same shape across /getid, /user/info, /passport/auth/login, and
// /passport/auth/bind, so we can reuse one struct everywhere.
//
// We only persist long-lived identity fields. Per-day usage counters
// (today_available_time, usage_time_balance, daily_usage_limit,
// sum_usage_seconds) and the transient is_new flag are intentionally
// dropped on save — anything time-sensitive is re-fetched on the next
// `whoami` / `ensure-ready`.
type Account struct {
	UUID         string `json:"uuid"`
	Token        string `json:"token,omitempty"`
	AuthData     string `json:"auth_data,omitempty"`
	SubscribeURL string `json:"subscribe_url,omitempty"`
	ExpiredAt    int64  `json:"expired_at,omitempty"`
	UserEmail    string `json:"user_email,omitempty"`
	InviteCode   string `json:"invite_code,omitempty"`
	IsAdmin      bool   `json:"is_admin,omitempty"`

	// UpdatedAt is the local unix timestamp of the last successful
	// refresh. Useful for status/whoami output and for diagnosing stale
	// caches.
	UpdatedAt int64 `json:"updated_at,omitempty"`
}

// IsLoggedIn returns true when the account holds a usable Authorization
// header value. Anonymous (getid-only) accounts have UUID + Token but
// no AuthData.
func (a *Account) IsLoggedIn() bool { return a != nil && a.AuthData != "" }

// ErrNoAccount is returned by Load when the account file does not yet
// exist. Callers that need an account should bootstrap via getid (or
// surface this to the user as "run feivpnctl getid").
var ErrNoAccount = errors.New("no account on disk: run `feivpnctl getid` (or `login`/`register`) first")

// Path returns the on-disk location of the account file.
func Path() string {
	if env := os.Getenv("FEIVPN_ACCOUNT_FILE"); env != "" {
		return env
	}
	return "/var/lib/feivpn/account.json"
}

// Load returns the persisted account or ErrNoAccount.
func Load() (*Account, error) {
	path := Path()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoAccount
		}
		return nil, fmt.Errorf("store: read %s: %w", path, err)
	}
	var a Account
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("store: parse %s: %w", path, err)
	}
	return &a, nil
}

// Save atomically (over)writes the account file. Pass a *complete*
// Account; partial updates should Load → mutate → Save so callers can
// see exactly what gets written.
func Save(a *Account) error {
	if a == nil {
		return errors.New("store: Save(nil)")
	}
	if a.UUID == "" {
		return errors.New("store: Save called with empty UUID — refusing to write half-formed account")
	}
	path := Path()
	raw, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return writeFile0600(path, raw)
}

// Exists reports whether the account file is present (does NOT
// validate its contents).
func Exists() bool {
	_, err := os.Stat(Path())
	return err == nil
}

func writeFile0600(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	// Some filesystems / umasks drop the explicit mode on the temp
	// file, so re-chmod just before the rename.
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
