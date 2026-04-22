//go:build !darwin

package platform

// NewDarwin returns a stub adapter on non-Darwin builds. See linux_stub.go.
func NewDarwin() Adapter { return &stub{name: "launchd-stub"} }
