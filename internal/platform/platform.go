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
type Adapter interface {
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

	// Name returns the human-readable adapter name ("systemd", "launchd").
	Name() string
}

// InstallOptions captures everything the unit/plist needs.
type InstallOptions struct {
	BinPath    string // absolute path to the feivpn binary
	ConfigPath string // -c argument
	WorkingDir string // typically /opt/feivpn
	LogFile    string // stdout+stderr destination
	Args       []string
	User       string // optional, defaults to root
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
