package action

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/feivpn/feivpn-runtime/internal/binmgr"
	"github.com/feivpn/feivpn-runtime/internal/logging"
	"github.com/feivpn/feivpn-runtime/internal/platform"
	"github.com/feivpn/feivpn-runtime/internal/state"
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

	// Step 1+2: verify manifest + locate binary (also performs SHA check).
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

	// Step 5: install + start service
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
// In the MVP we generate a minimal Outline-compatible config block with
// the selected SubscriptionNode. Future versions will support
// rule-based / split-tunnel configs.
func (r *Runner) renderDaemonConfig() (string, error) {
	if r.Profile.SubscriptionToken == "" {
		return "", fmt.Errorf("CONFIG_INCOMPLETE: profile.subscription_token is empty")
	}

	tz, _ := time.Now().Zone()
	nodes, err := r.Feiapi.GetConfig(r.Profile.SubscriptionToken, tz)
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
