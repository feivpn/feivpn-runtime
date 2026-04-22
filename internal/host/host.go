// Package host fingerprints the current machine's OS + architecture and
// translates between the three naming systems we have to talk to:
//
//  1. Go's own runtime.GOOS / runtime.GOARCH ("linux", "darwin", "amd64", "arm64").
//  2. The manifest key used inside manifest/binaries.manifest.json
//     ("linux-amd64", "darwin-arm64", ...). This is also what
//     binmgr.PlatformKey() returns; we re-derive it here so callers don't
//     need to import binmgr just to read host info.
//  3. The skill-specific platform tag we pass on
//     /api/v1/version/check?platform= so the server returns the
//     **bundled-binary** version line (feivpn / feiapi / feivpn-router
//     pinned together inside this skill release), NOT the desktop
//     Electron client's version. The desktop client uses bare
//     "linux" / "mac" / "win" / "ios" / "android" / "web"; reusing those
//     would conflate two completely independent release trains, so the
//     skill carves out its own namespace.
//
// Detection is fully automatic — no flags, no env vars. Callers that
// genuinely want to query a different target (e.g. a CI box on linux/amd64
// asking about darwin/arm64) pass an explicit override at the call site.
package host

import (
	"fmt"
	"runtime"
)

// Reserved skill-side platform tags for /api/v1/version/check. Server
// MUST register these as separate release channels — they share schema
// with the desktop client tags but are otherwise independent.
const (
	SkillUpgradeTagLinux = "feivpn-runtime-linux"
	SkillUpgradeTagMac   = "feivpn-runtime-mac"
)

// Info describes the host the skill is currently running on.
type Info struct {
	// GoOS is runtime.GOOS verbatim ("linux", "darwin").
	GoOS string `json:"goos"`
	// GoArch is runtime.GOARCH verbatim ("amd64", "arm64").
	GoArch string `json:"goarch"`
	// SkillUpgradeTag is the platform string the skill sends to
	// /api/v1/version/check to pull the version of THIS skill release
	// (feivpn / feiapi / feivpn-router pinned in bin/). It is
	// deliberately distinct from the desktop client's tags
	// ("linux"/"mac") so the server can dispatch the two release
	// trains independently. Empty if the OS isn't supported.
	SkillUpgradeTag string `json:"skill_upgrade_tag"`
	// ManifestKey is the lookup key inside manifest/binaries.manifest.json
	// (".binaries[<key>]"). E.g. "linux-amd64", "darwin-arm64".
	ManifestKey string `json:"manifest_key"`
	// Friendly is a human label, e.g. "macOS Apple Silicon" or
	// "Linux x86_64". For stderr summaries only.
	Friendly string `json:"friendly"`
}

// Detect returns the live host info for the current process. It never
// returns an error — for unsupported OS/arch combinations the unsupported
// fields are left empty and the caller is expected to validate by
// calling Info.Supported().
func Detect() Info {
	return describe(runtime.GOOS, runtime.GOARCH)
}

// Supported is true when the host is one of the architectures the skill
// ships binaries for AND the OS has a service-manager adapter.
func (i Info) Supported() bool {
	return i.SkillUpgradeTag != "" && i.ManifestKey != "" && supportedKey(i.ManifestKey)
}

// AssertSupported returns nil when Supported() is true, else a wrapped
// UNSUPPORTED_PLATFORM error suitable for surfacing through the CLI.
func (i Info) AssertSupported() error {
	if i.Supported() {
		return nil
	}
	return fmt.Errorf("UNSUPPORTED_PLATFORM: %s/%s is not bundled in this skill release", i.GoOS, i.GoArch)
}

// describe is the pure mapping function so it can be unit-tested without
// poking runtime.GOOS.
func describe(goos, goarch string) Info {
	info := Info{GoOS: goos, GoArch: goarch}

	switch goos {
	case "linux":
		info.SkillUpgradeTag = SkillUpgradeTagLinux
	case "darwin":
		info.SkillUpgradeTag = SkillUpgradeTagMac
	}

	if info.SkillUpgradeTag != "" {
		info.ManifestKey = goos + "-" + goarch
	}

	info.Friendly = friendly(goos, goarch)
	return info
}

func friendly(goos, goarch string) string {
	osPart := goos
	switch goos {
	case "darwin":
		osPart = "macOS"
	case "linux":
		osPart = "Linux"
	}

	archPart := goarch
	switch {
	case goos == "darwin" && goarch == "arm64":
		archPart = "Apple Silicon"
	case goos == "darwin" && goarch == "amd64":
		archPart = "Intel"
	case goarch == "amd64":
		archPart = "x86_64"
	case goarch == "arm64":
		archPart = "ARM64"
	}

	return osPart + " " + archPart
}

// supportedKey is the allow-list of (goos, goarch) pairs the skill ships
// pre-built binaries for. Mirror manifest/binaries.manifest.json.
//
// darwin-amd64 (Intel Mac) is supported via per-arch Go binaries
// (feivpn / feiapi) plus a single Universal Binary for the C++ router
// (feivpn-router-darwin-universal) — see manifest comment on
// feivpn_router for the shipping model.
func supportedKey(k string) bool {
	switch k {
	case "linux-amd64", "linux-arm64", "darwin-arm64", "darwin-amd64":
		return true
	default:
		return false
	}
}
