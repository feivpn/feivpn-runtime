// Package platform abstracts the per-OS service-manager glue.
//
// On Linux we install a systemd unit and use systemctl.
// On macOS we install a LaunchDaemon and use launchctl.
//
// All implementations satisfy the same Adapter interface so feivpnctl
// never branches on runtime.GOOS outside this package.
package platform

import (
	"fmt"
	"runtime"
)

// Adapter is the platform-agnostic service-manager surface used by the
// `internal/action` package. Each method MUST be idempotent.
//
// Two service units are managed:
//
//   - feivpn        — user-level Go daemon (TUN + tun2socks)
//   - feivpn-router — root-level C++ proxy controller (route + DNS)
//
// The router unit is started before the daemon and stopped after it.
// Adapters MUST encode that ordering at the service-manager layer too
// (e.g. systemd Wants/After, launchd boot order) so a system reboot
// replays the same sequence even when feivpnctl is not in the loop.
type Adapter interface {
	// ----- daemon (feivpn) lifecycle -----

	// InstallService writes the unit/plist file and registers it.
	InstallService(opts InstallOptions) error
	// EnableAndStart starts the service and (where applicable) enables
	// it across reboots.
	EnableAndStart() error
	// Stop stops the running service. Returns nil if it wasn't running.
	Stop() error
	// Disable removes the auto-start hook (called by `feivpnctl uninstall`).
	Disable() error
	// Uninstall removes the unit/plist file.
	Uninstall() error
	// IsActive returns true when the service is currently running
	// according to the OS service manager (orthogonal to state.json).
	IsActive() (bool, error)

	// ----- router (feivpn-router, runs as root) lifecycle -----

	// InstallRouterService writes the privileged router unit/plist.
	// MUST be called before InstallService so the daemon unit can
	// declare an After/Wants relationship to it.
	InstallRouterService(opts RouterInstallOptions) error
	// EnableAndStartRouter starts the router service. Caller MUST call
	// this before EnableAndStart() for the daemon.
	EnableAndStartRouter() error
	// StopRouter stops the router service. Caller MUST call this AFTER
	// the daemon has stopped and run --recover, otherwise routes
	// installed by the router will leak.
	StopRouter() error
	// DisableRouter removes the auto-start hook for the router.
	DisableRouter() error
	// UninstallRouter removes the router unit/plist file.
	UninstallRouter() error
	// IsRouterActive returns true when the router service is running.
	IsRouterActive() (bool, error)

	// Name returns the human-readable adapter name ("systemd", "launchd").
	Name() string
}

// InstallOptions captures everything the daemon unit/plist needs.
type InstallOptions struct {
	BinPath    string // absolute path to the feivpn binary
	ConfigPath string // -c argument
	WorkingDir string // typically /opt/feivpn
	LogFile    string // stdout+stderr destination
	Args       []string
	User       string // optional, defaults to root
}

// RouterInstallOptions captures everything the privileged router
// unit/plist needs. The router always runs as root.
type RouterInstallOptions struct {
	BinPath    string // absolute path to the feivpn-router binary
	WorkingDir string // typically /opt/feivpn
	LogFile    string // stdout+stderr destination (typically /var/log/feivpn/router.log)
	Args       []string
}

// Detect returns the adapter for the current host or a clear error if
// the host is unsupported.
func Detect() (Adapter, error) {
	switch runtime.GOOS {
	case "linux":
		return NewLinux(), nil
	case "darwin":
		return NewDarwin(), nil
	default:
		return nil, fmt.Errorf("UNSUPPORTED_PLATFORM: %s/%s has no service adapter", runtime.GOOS, runtime.GOARCH)
	}
}
