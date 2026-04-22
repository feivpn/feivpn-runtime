//go:build !linux

package platform

// NewLinux returns a stub adapter on non-Linux builds. Detect() never
// calls this on Linux at runtime, but the symbol must exist so the
// platform.go switch compiles.
func NewLinux() Adapter { return &stub{name: "systemd-stub"} }
