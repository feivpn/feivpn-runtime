package action

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/feivpn/feivpn-runtime/internal/host"
)

// installerURL is the canonical raw URL of the curl|bash installer. It
// lives in this repo's main branch so agents always get the bootstrap
// path that matches the running feivpnctl release line. Override with
// FEIVPN_INSTALLER_URL only for staging / fork deployments.
const installerURL = "https://raw.githubusercontent.com/feivpn/feivpn-runtime/main/scripts/install.sh"

// CheckUpgrade compares the locally pinned `feivpn` daemon version
// against /api/v1/version/check for the host's platform. It NEVER
// downloads, swaps, or restarts anything — call `feivpnctl upgrade` to
// act on the result.
//
// The target platform is fully derived from the host. We pass a
// **skill-specific** platform tag (host.SkillUpgradeTagLinux /
// host.SkillUpgradeTagMac), NOT the desktop client's bare "linux"/"mac"
// tags — the two release trains are independent and the server must
// dispatch them separately. There is no override flag because the
// skill manages THIS machine and nothing else.
func (r *Runner) CheckUpgrade() (*CheckUpgradeResult, error) {
	info := host.Detect()

	res := &CheckUpgradeResult{
		Component:    "feivpn",
		Host:         info.Friendly,
		Platform:     info.SkillUpgradeTag,
		Architecture: info.GoArch,
		ManifestKey:  info.ManifestKey,
	}

	// 1) Local version: read straight from the bundled manifest.
	manifest, err := r.Locator.Manifest()
	if err != nil {
		res.Status = "failed"
		res.Error = err.Error()
		return res, fmt.Errorf("MANIFEST_READ_FAILED: %w", err)
	}
	res.CurrentVersion = manifest.Feivpn.Version

	// 2) Refuse early on hosts the skill can never serve. Without a
	// skill-upgrade tag we have nothing meaningful to ask /version/check.
	if info.SkillUpgradeTag == "" {
		res.Status = "failed"
		res.Error = fmt.Sprintf("UNSUPPORTED_PLATFORM: %s/%s has no FeiVPN skill upgrade tag", info.GoOS, info.GoArch)
		return res, errors.New(res.Error)
	}

	// 3) Remote version via the bundled feiapi binary.
	remote, err := r.Feiapi.GetVersion(info.SkillUpgradeTag)
	if err != nil {
		res.Status = "stale"
		res.Error = err.Error()
		res.Instruction = "Could not reach /version/check; try again later or rely on `feivpnctl upgrade` after manual sync-bins"
		return res, nil
	}
	res.RemoteVersion = remote.Version
	res.ForceUpdate = remote.ForceUpdate
	res.Changelog = remote.Changelog
	res.UpdateURL = remote.UpdateURL

	// 4) Diff. We deliberately use string inequality rather than semver
	// comparison: the manifest version is whatever feivpn-apps tagged,
	// and the server returns the same string. A mismatch in either
	// direction (newer OR older) is an actionable signal.
	res.Status = "ok"
	res.NeedsUpgrade = res.CurrentVersion != res.RemoteVersion

	if !res.Supported() {
		res.Notice = "Host architecture " + info.ManifestKey + " is not in the bundled bin/ manifest. Daemon will fail to launch even if upgrade succeeds."
	}

	if res.NeedsUpgrade {
		res.Upgrade = buildUpgradePlan(res.RemoteVersion)
	}

	switch {
	case res.NeedsUpgrade && res.ForceUpdate:
		res.Instruction = "Force update required. Execute upgrade.command as root, or run the two upgrade.steps in order."
	case res.NeedsUpgrade:
		res.Instruction = "New daemon release available. Execute upgrade.command as root, or run the two upgrade.steps in order."
	default:
		res.Instruction = "Up to date."
	}
	return res, nil
}

// buildUpgradePlan turns a remote version string into the literal
// shell commands a downstream agent / human can execute. The server
// returns version strings without a leading "v" (e.g. "0.2.0") while
// GitHub Release tags conventionally include it (e.g. "v0.2.0"); we
// normalise to the tag form.
func buildUpgradePlan(remoteVersion string) *UpgradePlan {
	url := installerURL
	if env := strings.TrimSpace(os.Getenv("FEIVPN_INSTALLER_URL")); env != "" {
		url = env
	}

	tag := strings.TrimSpace(remoteVersion)
	if tag != "" && !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}

	tagFlag := ""
	if tag != "" {
		tagFlag = " -s -- --tag " + tag
	}
	installLine := "curl -fsSL " + url + " | sudo bash" + tagFlag
	restartLine := "sudo feivpnctl upgrade"

	return &UpgradePlan{
		InstallerURL: url,
		TargetTag:    tag,
		Command:      installLine + " && " + restartLine,
		Steps:        []string{installLine, restartLine},
		RequiresRoot: true,
	}
}


// Supported is a tiny convenience wrapper so the action layer can write
// `res.Supported()` without re-deriving host.Info — the manifest key on
// the result already encodes the answer. Keep this list in lockstep
// with internal/host.supportedKey.
func (r *CheckUpgradeResult) Supported() bool {
	switch r.ManifestKey {
	case "linux-amd64", "linux-arm64", "darwin-arm64", "darwin-amd64":
		return true
	default:
		return false
	}
}
