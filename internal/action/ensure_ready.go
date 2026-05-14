package action

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/feivpn/feivpn-runtime/internal/binmgr"
	"github.com/feivpn/feivpn-runtime/internal/feiapi"
	"github.com/feivpn/feivpn-runtime/internal/logging"
	"github.com/feivpn/feivpn-runtime/internal/platform"
	"github.com/feivpn/feivpn-runtime/internal/router"
	"github.com/feivpn/feivpn-runtime/internal/state"
	"github.com/feivpn/feivpn-runtime/internal/store"
	"github.com/feivpn/feivpn-runtime/internal/tz"
)

// linuxRouterTunName is the TUN device the C++ feivpn-router creates and
// installs routes against (see outline_proxy_controller.h
// `tunInterfaceName = "feivpn-tun1"` in feivpn-apps). The Go daemon must
// open *this* device, not the default `tun0`, otherwise tun2socks
// runs on a TUN that the router never installed routes against and
// every health check shows tun=false / connectivity=false.
const linuxRouterTunName = "feivpn-tun1"

// EnsureReady is the main bootstrap entry point.
//
// Internal flow (matches the design doc the user wrote):
//
//	1. Platform detection      → already encoded by binmgr.PlatformKey
//	2. Pre-check               → manifest present, binaries verified
//	3. Render daemon config    → fetch subscription via feiapi, pick node
//	4. feivpn --check          → fail fast if config is malformed
//	5. Install & start service → systemd / launchd
//	6. Wait + feivpn --health  → confirm tun/route/dns/connectivity
//	7. Return CheckReport      → green ⇒ "ready", any false ⇒ "degraded"
func (r *Runner) EnsureReady() (*EnsureReadyResult, error) {
	plat := binmgr.PlatformKey()
	logging.Info("ensure_ready: starting", "platform", plat)

	res := &EnsureReadyResult{
		Status:   "failed",
		Platform: plat,
	}

	// Step 1+2: verify manifest + locate both binaries (also performs SHA check).
	// Router is verified first because the daemon will refuse to come
	// up cleanly if the router socket is not reachable.
	routerBin, err := r.Router.BinaryPath()
	if err != nil {
		return appendErr(res, err)
	}
	if _, err := r.Daemon.BinaryPath(); err != nil {
		return appendErr(res, err)
	}

	// Step 3: render daemon config from subscription.
	configPath, node, err := r.renderDaemonConfig()
	if err != nil {
		return appendErr(res, err)
	}

	// Step 4: feivpn --check
	if err := r.Daemon.Check(configPath); err != nil {
		return appendErr(res, err)
	}

	// Step 5a: install + start the privileged router FIRST so its
	// socket exists by the time the daemon tries to dial it.
	routerOpts := platform.RouterInstallOptions{
		BinPath:    routerBin,
		WorkingDir: r.Paths.WorkingDir,
		LogFile:    r.Paths.RouterLogFile,
	}
	// Linux feivpn-router requires an explicit unix socket path.
	if runtime.GOOS == "linux" {
		routerOpts.Args = []string{
			"--socket-filename=/var/run/feivpn_controller",
			"--owning-user-id=-1",
		}
	}
	if err := r.Platform.InstallRouterService(routerOpts); err != nil {
		return appendErr(res, fmt.Errorf("ROUTER_INSTALL_FAILED: %w", err))
	}
	if err := r.Platform.EnableAndStartRouter(); err != nil {
		return appendErr(res, fmt.Errorf("ROUTER_START_FAILED: %w", err))
	}

	// Step 5b: install + start the user-level daemon. The systemd unit
	// declares Requires=feivpn-router.service so a reboot replays this
	// ordering even when feivpnctl is not in the loop.
	binPath, _ := r.Daemon.BinaryPath()
	// Runtime mode must pass the client JSON via -client. The -config flag is
	// check-mode only in the daemon binary and does not boot a live session.
	configRaw, err := os.ReadFile(configPath)
	if err != nil {
		return appendErr(res, fmt.Errorf("CONFIG_READ_FAILED: %w", err))
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, configRaw); err != nil {
		return appendErr(res, fmt.Errorf("CONFIG_COMPACT_FAILED: %w", err))
	}
	clientJSON := strings.TrimSpace(compact.String())
	if clientJSON == "" {
		return appendErr(res, fmt.Errorf("CONFIG_EMPTY: rendered daemon config is empty"))
	}
	installOpts := platform.InstallOptions{
		BinPath:    binPath,
		WorkingDir: r.Paths.WorkingDir,
		LogFile:    r.Paths.LogFile,
		Args:       []string{"-client", clientJSON},
	}
	// On Linux the C++ router creates `feivpn-tun1` at startup; make
	// the Go daemon attach to *that* device instead of creating its
	// own `tun0`. Without this the router and the daemon end up
	// holding two unrelated TUN devices and the route hijack points
	// at a TUN nobody is reading from.
	if runtime.GOOS == "linux" {
		installOpts.Args = append(installOpts.Args, "-tunName", linuxRouterTunName)
	}
	if r.Profile.LogLevel != "" {
		installOpts.Args = append(installOpts.Args, "--logLevel", r.Profile.LogLevel)
	}
	if err := r.Platform.InstallService(installOpts); err != nil {
		return appendErr(res, fmt.Errorf("SERVICE_INSTALL_FAILED: %w", err))
	}
	if err := r.Platform.EnableAndStart(); err != nil {
		return appendErr(res, fmt.Errorf("SERVICE_START_FAILED: %w", err))
	}

	// Step 6: tell the router to hijack routes / DNS toward the proxy.
	// The router sat idle until now (it only listens on its socket),
	// so this is the call that actually flips the system into
	// VPN-routed mode. Best-effort — failures bubble up via Errors so
	// status reports them, but we still wait for health afterwards
	// because the daemon side may still be partially functional.
	if proxyIP, perr := resolveProxyIP(node); perr == nil && proxyIP != "" {
		if err := router.Configure(proxyIP, 10*time.Second); err != nil {
			res.Errors = append(res.Errors, "ROUTER_CONFIGURE_FAILED: "+err.Error())
			logging.Warn("ensure_ready: router.Configure failed", "proxy_ip", proxyIP, "err", err)
		} else {
			logging.Info("ensure_ready: router configured", "proxy_ip", proxyIP)
		}
	} else if perr != nil {
		res.Errors = append(res.Errors, "PROXY_IP_RESOLVE_FAILED: "+perr.Error())
		logging.Warn("ensure_ready: cannot resolve proxy ip for router", "err", perr)
	}

	// Step 7: wait + health-check (with bounded retries)
	health, err := r.waitForHealth(20*time.Second, 1*time.Second)
	if err != nil {
		return appendErr(res, err)
	}

	// Step 8: assemble report
	res.Checks = CheckReport{
		Process:      health.Checks.Process,
		Tun:          health.Checks.TUN,
		Route:        health.Checks.Route,
		DNS:          health.Checks.DNS,
		Connectivity: health.Checks.Connectivity,
	}
	res.Pid = health.Pid
	res.Tun = health.Tun.Name
	res.Version = health.Version

	if allGreen(res.Checks) {
		res.Status = "ready"
	} else {
		res.Status = "degraded"
		res.Errors = append(res.Errors, health.Errors...)
	}
	logging.Info("ensure_ready: done", "status", res.Status, "pid", res.Pid)
	return res, nil
}

// renderDaemonConfig fetches the subscription via feiapi and writes a
// daemon config.json that satisfies the daemon-args.schema.json contract.
//
// Refresh policy: if the profile does not pin a subscription URL we
// always go through the local account store, refreshing it from the
// server first so a stale subscribe_url never blocks ensure-ready. The
// account is auto-bootstrapped via /getid on first run.
//
// In the MVP we generate a minimal Outline-compatible config block with
// the selected SubscriptionNode. The chosen node is returned so callers
// can extract its proxy host/IP for the router IPC handshake.
func (r *Runner) renderDaemonConfig() (string, *feiapi.SubscriptionNode, error) {
	subscribeURL := r.Profile.SubscriptionToken
	if subscribeURL != "" {
		logging.Info("ensure_ready: using subscribe_url from --token / profile override")
	} else {
		acc, err := r.refreshAccountForEnsureReady()
		if err != nil {
			return "", nil, err
		}
		subscribeURL = acc.SubscribeURL
		if subscribeURL == "" {
			return "", nil, fmt.Errorf("CONFIG_INCOMPLETE: server returned no subscribe_url — try `feivpnctl whoami` or pass --token <subscribe_url>")
		}
		logging.Info("ensure_ready: using subscribe_url from store", "uuid", acc.UUID, "logged_in", acc.IsLoggedIn())
	}

	zone := tz.IANA() // IANA name like "Asia/Shanghai"; the server
	// rejects Go's time.Now().Zone() abbreviations
	// ("CST", "PST", …) as a query value.
	nodes, err := r.Feiapi.GetConfig(subscribeURL, zone)
	if err != nil {
		return "", nil, fmt.Errorf("SUBSCRIPTION_FETCH_FAILED: %w", err)
	}
	node, err := r.Profile.SelectNode(nodes)
	if err != nil {
		return "", nil, err
	}

	// Important: feivpn --check/--config expects the same JSON shape as
	// the runtime -client payload consumed by outline.ClientConfig.New(),
	// i.e. a "client config" rooted at transport descriptors.
	//
	// The access key itself (ss:// / trojan:// / vless:// / vmess:// / anytls://)
	// is the canonical transport descriptor in FeiVPN; wrapping it as
	// {"transport":"<uri>"} matches parse.go's legacy URL path and is accepted
	// by both --check and normal startup.
	cfg := map[string]any{
		"transport": node.AccessKey,
	}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.MkdirAll(filepath.Dir(r.Paths.ConfigFile), 0o755); err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(r.Paths.ConfigFile, raw, 0o600); err != nil {
		return "", nil, err
	}
	return r.Paths.ConfigFile, node, nil
}

// resolveProxyIP returns the IP literal the C++ router needs to install
// the /32 priority route to the proxy. The router calls
// `ip route get <addr>` internally and that command does not accept
// hostnames, so we resolve here.
//
// Strategy:
//   - If feiapi already populated node.Server with a literal IP, use it.
//   - Else parse the access key URL host (already done by parseAccessKey)
//     and DNS-resolve it. We pick the first IPv4 result; the router
//     itself is IPv4-only on Linux and tries to disable IPv6 anyway.
//
// Returns ("", nil) if the node is nil — caller treats that as
// "skip router configuration" (e.g. profile-only test mode).
func resolveProxyIP(node *feiapi.SubscriptionNode) (string, error) {
	if node == nil {
		return "", nil
	}
	host := strings.TrimSpace(node.Server)
	if host == "" {
		return "", fmt.Errorf("subscription node has no server host (access_key=%q)", truncateForLog(node.AccessKey, 32))
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String(), nil
	}
	addrs, err := net.LookupIP(host)
	if err != nil {
		return "", fmt.Errorf("dns lookup %q: %w", host, err)
	}
	for _, ip := range addrs {
		if v4 := ip.To4(); v4 != nil {
			return v4.String(), nil
		}
	}
	if len(addrs) > 0 {
		return addrs[0].String(), nil
	}
	return "", fmt.Errorf("no addresses returned for %q", host)
}

func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// waitForHealth polls `feivpn --health` until everything is green or
// timeout fires. The first non-error response is returned even if some
// checks are false; the caller decides how to flag that.
func (r *Runner) waitForHealth(timeout, every time.Duration) (h *daemonHealthAlias, err error) {
	deadline := time.Now().Add(timeout)
	var last *daemonHealthAlias
	for {
		report, healthErr := r.Daemon.Health()
		if healthErr == nil && report != nil {
			last = (*daemonHealthAlias)(report)
			if last.Checks.Process && last.Checks.TUN && last.Checks.Connectivity {
				return last, nil
			}
		}
		if time.Now().After(deadline) {
			if last != nil {
				return last, nil // partial report — caller flags as degraded
			}
			if healthErr != nil {
				return nil, fmt.Errorf("HEALTH_TIMEOUT: %w", healthErr)
			}
			return nil, fmt.Errorf("HEALTH_TIMEOUT: no response from daemon within %s", timeout)
		}
		time.Sleep(every)
	}
}

// daemonHealthAlias avoids a circular import while letting EnsureReady
// reference daemon.HealthReport's shape via type alias.
type daemonHealthAlias = daemonHealth

// daemonHealth is intentionally re-declared as an alias to daemon.HealthReport
// to keep this file decoupled from the daemon package's type identity.
//
// We use a separate file (`alias.go`) to import the daemon package so
// `go vet` doesn't flag the import as unused here.

// ----- helpers -----

func appendErr(r *EnsureReadyResult, err error) (*EnsureReadyResult, error) {
	r.Errors = append(r.Errors, err.Error())
	r.Status = "failed"
	return r, err
}

func allGreen(c CheckReport) bool {
	return c.Process && c.Tun && c.Route && c.DNS && c.Connectivity
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func defaultTunName() string {
	if runtime.GOOS == "darwin" {
		return "" // let the daemon pick utunN
	}
	return "fei0"
}

// readState is a small helper used by other actions in this package.
func (r *Runner) readState() (*state.State, error) {
	return state.Read(r.Paths.StateFile)
}

// refreshAccountForEnsureReady loads (auto-bootstrapping if needed) the
// local account, then asks the server for the latest subscribe_url.
// Returns the (possibly updated) account; persistence is best-effort.
func (r *Runner) refreshAccountForEnsureReady() (*store.Account, error) {
	acc, err := r.loadOrBootstrap()
	if err != nil {
		return nil, err
	}

	id, err := deviceID()
	if err != nil {
		// Without a device id we can't refresh, but we may still have
		// a usable cached subscribe_url.
		logging.Warn("ensure_ready: device id unavailable; using cached subscribe_url", "err", err)
		return acc, nil
	}

	var fresh *feiapi.UserData
	if acc.IsLoggedIn() {
		fresh, err = r.Feiapi.GetInfo(id, acc.AuthData)
	} else {
		fresh, err = r.Feiapi.GetID(id, "")
	}
	if err != nil {
		logging.Warn("ensure_ready: identity refresh failed; using cached subscribe_url", "err", err)
		return acc, nil
	}
	applyUserData(acc, fresh)
	if persistErr := store.Save(acc); persistErr != nil {
		logging.Warn("ensure_ready: persist refreshed account failed", "err", persistErr)
	}
	return acc, nil
}
