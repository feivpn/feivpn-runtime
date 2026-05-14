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
// feivpnctl's responsibilities here are:
//
//  1. Locate + verify the router binary via the manifest (binmgr).
//  2. Tell the platform adapter to install + start it as a privileged
//     service unit *before* the user-level feivpn daemon is launched.
//  3. After the daemon stops, the platform adapter stops this too.
//  4. Drive the router's IPC contract (configureRouting / resetRouting).
//     This was originally the Electron TypeScript layer's job (see
//     `RoutingDaemon` in feivpn-apps/client/electron/routing_service.ts);
//     in the standalone bootstrap world `feivpnctl ensure-ready` /
//     `feivpnctl stop` own it.
package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"syscall"
	"time"

	"github.com/feivpn/feivpn-runtime/internal/binmgr"
)

// ErrRouterDown is returned by Configure / Reset when the router's IPC
// endpoint is not reachable in a way that's structurally indistinguishable
// from "the router service simply isn't running" (ENOENT on the unix
// socket, ECONNREFUSED on tcp). Callers can errors.Is() against this to
// downgrade their logging from WARN to INFO during normal stop / upgrade
// flows where the router has already been taken down.
var ErrRouterDown = errors.New("router: not running")

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

// ----- IPC: configureRouting / resetRouting -------------------------------
//
// Wire format (mirrors RoutingDaemon in routing_service.ts):
//
//   - Request:  one JSON object, no length prefix. The C++ server reads
//     until it sees a balanced "}" so we MUST emit compact single-line
//     JSON (no pretty-printing).
//   - Response: one JSON object, same framing. Server may push
//     unsolicited `statusChanged` events on the same socket; we drain
//     one extra frame if the first reply isn't ours.
//
// Routing-pollution monitoring (re-hijack after dhclient overwrites the
// default route) on the C++ side only runs while the IPC session is
// OPEN. We connect, command, then immediately disconnect — fine for the
// typical server-side install where the route table doesn't churn. If
// long-lived monitoring matters in the future, the right home is a tiny
// supervisor subprocess managed alongside feivpn.service.

type ipcRequest struct {
	Action     string         `json:"action"`
	Parameters map[string]any `json:"parameters"`
}

type ipcResponse struct {
	Action       string `json:"action"`
	StatusCode   int    `json:"statusCode"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// Configure tells the router to hijack the default route to the proxy
// server reachable at proxyIP. proxyIP MUST be a literal IP — the C++
// side calls `ip route get <ip>` which doesn't accept hostnames.
func Configure(proxyIP string, timeout time.Duration) error {
	if proxyIP == "" {
		return fmt.Errorf("router: proxyIP is required")
	}
	return roundtrip(ipcRequest{
		Action: "configureRouting",
		Parameters: map[string]any{
			"proxyIp":       proxyIP,
			"isAutoConnect": false,
		},
	}, timeout)
}

// Reset asks the router to restore the original default route + DNS.
// Best-effort: callers should still call `feivpn --recover` afterwards
// in case the router was already down.
func Reset(timeout time.Duration) error {
	return roundtrip(ipcRequest{
		Action:     "resetRouting",
		Parameters: map[string]any{},
	}, timeout)
}

func roundtrip(req ipcRequest, timeout time.Duration) error {
	network, addr := SocketAddress()
	if network == "" {
		return fmt.Errorf("router: unsupported OS %s", runtime.GOOS)
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial(network, addr)
	if err != nil {
		if isRouterDown(err) {
			return ErrRouterDown
		}
		return fmt.Errorf("router: dial %s/%s: %w", network, addr, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("router: set deadline: %w", err)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("router: marshal: %w", err)
	}
	if _, err := conn.Write(body); err != nil {
		return fmt.Errorf("router: write %s: %w", req.Action, err)
	}

	dec := json.NewDecoder(conn)
	var resp ipcResponse
	if err := dec.Decode(&resp); err != nil {
		return fmt.Errorf("router: read %s reply: %w", req.Action, err)
	}
	if resp.Action != "" && resp.Action != req.Action {
		// Server pushed a status event before our reply (rare on a
		// fresh connection, but possible). Decode one more frame.
		if err := dec.Decode(&resp); err != nil {
			return fmt.Errorf("router: read %s reply (after event): %w", req.Action, err)
		}
	}
	if resp.StatusCode != 0 {
		msg := resp.ErrorMessage
		if msg == "" {
			msg = "(no error message)"
		}
		return fmt.Errorf("router: %s failed: status=%d %s", req.Action, resp.StatusCode, msg)
	}
	return nil
}

// isRouterDown returns true when the dial error indicates the router
// service is not currently listening — i.e. ENOENT (unix socket file
// missing) or ECONNREFUSED (port closed). These are structurally
// equivalent to "router process is gone" and callers should treat them
// as soft failures rather than alarms.
func isRouterDown(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, os.ErrNotExist) {
		return true
	}
	// net.OpError wraps syscall errors via SyscallError; errors.Is walks
	// the chain so the check above usually suffices, but some net stacks
	// surface only the message — fall back to OpError inspection.
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		var sce *os.SyscallError
		if errors.As(op.Err, &sce) {
			return errors.Is(sce.Err, syscall.ENOENT) || errors.Is(sce.Err, syscall.ECONNREFUSED)
		}
	}
	return false
}
