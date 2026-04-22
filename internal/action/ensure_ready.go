package action

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/feivpn/feivpn-runtime/internal/binmgr"
	"github.com/feivpn/feivpn-runtime/internal/feiapi"
	"github.com/feivpn/feivpn-runtime/internal/logging"
	"github.com/feivpn/feivpn-runtime/internal/platform"
	"github.com/feivpn/feivpn-runtime/internal/state"
	"github.com/feivpn/feivpn-runtime/internal/store"
)

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
	configPath, err := r.renderDaemonConfig()
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
	installOpts := platform.InstallOptions{
		BinPath:    binPath,
		ConfigPath: configPath,
		WorkingDir: r.Paths.WorkingDir,
		LogFile:    r.Paths.LogFile,
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

	// Step 6: wait + health-check (with bounded retries)
	health, err := r.waitForHealth(20*time.Second, 1*time.Second)
	if err != nil {
		return appendErr(res, err)
	}

	// Step 7: assemble report
	res.Checks = CheckReport{
		Process:      health.Running,
		Tun:          health.Tun.Up,
		Route:        health.Route.HijackedByTun,
		DNS:          health.DNS.Hijacked,
		Connectivity: health.Connectivity.Reach,
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
// the selected SubscriptionNode.
func (r *Runner) renderDaemonConfig() (string, error) {
	subscribeURL := r.Profile.SubscriptionToken
	if subscribeURL != "" {
		logging.Info("ensure_ready: using subscribe_url from --token / profile override")
	} else {
		acc, err := r.refreshAccountForEnsureReady()
		if err != nil {
			return "", err
		}
		subscribeURL = acc.SubscribeURL
		if subscribeURL == "" {
			return "", fmt.Errorf("CONFIG_INCOMPLETE: server returned no subscribe_url — try `feivpnctl whoami` or pass --token <subscribe_url>")
		}
		logging.Info("ensure_ready: using subscribe_url from store", "uuid", acc.UUID, "logged_in", acc.IsLoggedIn())
	}

	tz, _ := time.Now().Zone()
	nodes, err := r.Feiapi.GetConfig(subscribeURL, tz)
	if err != nil {
		return "", fmt.Errorf("SUBSCRIPTION_FETCH_FAILED: %w", err)
	}
	node, err := r.Profile.SelectNode(nodes)
	if err != nil {
		return "", err
	}

	cfg := map[string]any{
		"schema_version": 1,
		"mode":           orDefault(r.Profile.Mode, "global"),
		"server":         node.Server,
		"port":           node.Port,
		"protocol":       node.Protocol,
		"token":          node.Token,
		"method":         node.Method,
		"name":           node.Name,
		"tun_name":       orDefault(r.Profile.TunName, defaultTunName()),
		"tun_addr":       orDefault(r.Profile.TunAddr, "10.111.222.1/24"),
		"log_level":      orDefault(r.Profile.LogLevel, "info"),
	}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.MkdirAll(filepath.Dir(r.Paths.ConfigFile), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(r.Paths.ConfigFile, raw, 0o600); err != nil {
		return "", err
	}
	return r.Paths.ConfigFile, nil
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
			if last.Running && last.Tun.Up && last.Connectivity.Reach {
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
