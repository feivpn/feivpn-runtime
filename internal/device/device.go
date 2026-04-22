// Package device exposes the persistent, OS-issued device identifier
// that the FeiVPN backend signs against (the `uuid` parameter on
// /getid and /passport/auth/bind).
//
// We deliberately read the OS value on every call instead of caching
// it ourselves: the backend treats this string as the canonical device
// identity, and storing a *random* UUID would let one physical machine
// register as N devices over time.
//
// Sources:
//
//   - Linux:   /etc/machine-id  (systemd; populated at OS install)
//              fallback /var/lib/dbus/machine-id (older / non-systemd)
//   - macOS:   IOPlatformUUID  via `ioreg -rd1 -c IOPlatformExpertDevice`
//
// Both values are stable across reboots and OS upgrades. They are
// returned verbatim; downstream signing helpers strip hyphens.
package device

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ErrUnavailable is returned when no platform source produced a value.
// Wrap with %w; callers (e.g. action layer) translate this into the
// stable error code DEVICE_ID_UNAVAILABLE.
var ErrUnavailable = errors.New("DEVICE_ID_UNAVAILABLE")

// ID returns the persistent device identifier for the current host.
//
// The returned string is non-empty on success. Callers should not
// modify or normalise it — feed it directly into feiapi.
func ID() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return readLinux()
	case "darwin":
		return readDarwin()
	default:
		return "", fmt.Errorf("%w: unsupported OS %q", ErrUnavailable, runtime.GOOS)
	}
}

func readLinux() (string, error) {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		v := strings.TrimSpace(string(raw))
		if v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("%w: /etc/machine-id and /var/lib/dbus/machine-id are missing or empty", ErrUnavailable)
}

func readDarwin() (string, error) {
	// `ioreg -rd1 -c IOPlatformExpertDevice` prints a property tree;
	// the line we want looks like:
	//   "IOPlatformUUID" = "12345678-ABCD-EFAB-CDEF-1234567890AB"
	out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return "", fmt.Errorf("%w: ioreg failed: %v", ErrUnavailable, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		const key = "\"IOPlatformUUID\" ="
		idx := strings.Index(line, key)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+len(key):])
		rest = strings.TrimPrefix(rest, "\"")
		rest = strings.TrimSuffix(rest, "\"")
		if rest != "" {
			return rest, nil
		}
	}
	return "", fmt.Errorf("%w: IOPlatformUUID not found in ioreg output", ErrUnavailable)
}
