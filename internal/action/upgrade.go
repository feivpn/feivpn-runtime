package action

import (
	"fmt"

	"github.com/feivpn/feivpn-runtime/internal/binmgr"
	"github.com/feivpn/feivpn-runtime/internal/logging"
)

// UpgradeResult mirrors the SKILL.md contract for upgrade_feivpn.
type UpgradeResult struct {
	OldVersion string `json:"old_version,omitempty"`
	NewVersion string `json:"new_version,omitempty"`
	Restarted  bool   `json:"restarted"`
	Notes      string `json:"notes,omitempty"`
}

// Upgrade is the in-place upgrade flow:
//   1. Re-read the manifest (the operator is expected to have already
//      bumped manifest/binaries.manifest.json + bin/* via `make sync-bins`
//      and re-deployed feivpnctl).
//   2. Locate (and SHA-verify) the new feivpn binary.
//   3. Stop the running service.
//   4. EnsureReady to bring the new version online.
//
// We don't perform an in-process binary download because:
//   - tarball releases of feivpn-runtime already bundle bin/, so the
//     install pipeline IS the upgrade pipeline.
//   - keeping the upgrade flow declarative (file in place → restart)
//     means rollback is just a `git checkout` of the previous tag.
func (r *Runner) Upgrade() (*UpgradeResult, error) {
	res := &UpgradeResult{}

	manifest, err := r.Locator.Manifest()
	if err != nil {
		return nil, fmt.Errorf("MANIFEST_READ_FAILED: %w", err)
	}
	res.NewVersion = manifest.Feivpn.Version

	if cur, err := r.readState(); err == nil && cur != nil {
		res.OldVersion = cur.Version
	}

	if _, err := r.Locator.Locate(binmgr.ComponentFeivpn); err != nil {
		return nil, err
	}
	// Router is part of the upgrade unit too: a daemon bump that
	// expects a newer router RPC contract MUST refuse to start with a
	// stale router. Locate verifies SHA against the (already bumped)
	// manifest entry.
	if _, err := r.Locator.Locate(binmgr.ComponentFeivpnRouter); err != nil {
		return nil, err
	}

	if _, err := r.Stop(); err != nil {
		logging.Warn("upgrade: stop reported errors", "err", err)
	}

	ready, err := r.EnsureReady()
	if err != nil {
		return res, err
	}
	if ready.Status == "ready" || ready.Status == "degraded" {
		res.Restarted = true
	}
	if ready.Status == "degraded" {
		res.Notes = "daemon restarted but health check is degraded; run `feivpnctl status` for details"
	}
	return res, nil
}
