// Package router is a thin wrapper around the pinned `feivpn-router`
// binary (the C++ FeiVPNProxyController from feivpn/feivpn-apps).
//
// Why a separate component: route + DNS mutations require root, but
// running the whole tun2socks data plane as root is overkill and
// dangerous. The upstream client already splits responsibilities into
// two processes and talks to the router over a local socket:
//
//	Linux  → unix:/var/run/feivpn_controller   (managed by systemd)
//	macOS  → tcp:127.0.0.1:38964               (managed by launchd as
//	                                            a LaunchDaemon)
//
// feivpnctl's only jobs here are:
//
//  1. Locate + verify the binary via the manifest (binmgr).
//  2. Tell the platform adapter to install + start it as a privileged
//     service unit *before* the user-level feivpn daemon is launched.
//  3. After the daemon stops, the platform adapter stops this too.
//
// We deliberately do NOT model --health / --recover here yet: the
// upstream router does not expose them today. Once it does we'll add
// thin Health() / Recover() helpers symmetrical to internal/daemon.
package router

import (
	"runtime"

	"github.com/feivpn/feivpn-runtime/internal/binmgr"
)

// Client wraps a Locator to resolve and describe the router binary.
type Client struct {
	loc *binmgr.Locator
}

// New returns a Client backed by the given binary locator.
func New(loc *binmgr.Locator) *Client { return &Client{loc: loc} }

// BinaryPath returns the verified absolute path to the router binary
// for the current platform. Same error codes as daemon.BinaryPath():
// BINARY_MISSING / BINARY_CHECKSUM_MISMATCH / UNSUPPORTED_PLATFORM.
func (c *Client) BinaryPath() (string, error) {
	return c.loc.Locate(binmgr.ComponentFeivpnRouter)
}

// SocketAddress returns the local-IPC endpoint that the user-level
// feivpn daemon will use to talk to this router. The values mirror what
// the upstream router binds to in:
//
//   - feivpn-apps/client/electron/linux_proxy_controller/dist/feivpn_proxy_controller.service
//   - feivpn-apps/client/electron/macos_proxy_controller/dist/com.feivpn.proxy.plist
//
// Returned scheme is one of "unix" or "tcp"; the address is what the
// daemon should pass to net.Dial.
func SocketAddress() (scheme, addr string) {
	switch runtime.GOOS {
	case "darwin":
		return "tcp", "127.0.0.1:38964"
	case "linux":
		return "unix", "/var/run/feivpn_controller"
	default:
		return "", ""
	}
}
