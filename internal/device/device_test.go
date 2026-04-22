package device

import (
	"runtime"
	"testing"
)

// TestIDReadable just sanity-checks that the host this test runs on
// produces a non-empty device id. CI runs on Linux + macOS, both
// supported. We keep the assertion minimal so the test stays robust
// across environments (containers, ephemeral VMs, etc.).
func TestIDReadable(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skipf("device.ID is only implemented for linux and darwin; got %s", runtime.GOOS)
	}
	id, err := ID()
	if err != nil {
		// Some sandboxes (e.g. minimal Docker images) have neither
		// /etc/machine-id nor /var/lib/dbus/machine-id. Skip rather
		// than fail.
		t.Skipf("device.ID unavailable in this environment: %v", err)
	}
	if id == "" {
		t.Fatal("device.ID returned empty string with no error")
	}
}
