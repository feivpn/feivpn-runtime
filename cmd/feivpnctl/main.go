// feivpnctl — bootstrap CLI for the FeiVPN daemon.
//
// Five top-level subcommands, each prints a single-line JSON document
// to stdout (consumed by Cursor / Claude skills) plus a human summary
// to stderr.
//
//	feivpnctl ensure-ready   — install + configure + start + verify
//	feivpnctl status         — read-only health and state inspection
//	feivpnctl stop           — stop daemon, restore network
//	feivpnctl restart        — stop → ensure-ready
//	feivpnctl upgrade        — re-verify pinned binary, restart daemon
//
// All actions live in internal/action; this file is just argument
// plumbing.
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/feivpn/feivpn-runtime/internal/action"
	"github.com/feivpn/feivpn-runtime/internal/config"
	"github.com/feivpn/feivpn-runtime/internal/logging"
)

// version is overridden via -ldflags "-X main.version=..." at build time.
var version = "dev"

// Root-level flags shared by every subcommand.
type globalFlags struct {
	configPath   string
	manifestPath string
	logLevel     string
	jsonOnly     bool // suppress human-readable summary on stderr
}

var gf globalFlags

func main() {
	root := &cobra.Command{
		Use:           "feivpnctl",
		Short:         "FeiVPN bootstrap CLI",
		Long:          "feivpnctl installs, configures, supervises, and upgrades the FeiVPN daemon on the current host.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&gf.configPath, "config", "", "path to feivpnctl profile (default: $FEIVPNCTL_CONFIG or /etc/feivpn/feivpnctl.json)")
	root.PersistentFlags().StringVar(&gf.manifestPath, "manifest", "", "path to binaries.manifest.json (default: /opt/feivpn/manifest.json)")
	root.PersistentFlags().StringVar(&gf.logLevel, "log-level", "info", "log verbosity: debug | info | warn | error")
	root.PersistentFlags().BoolVar(&gf.jsonOnly, "json", false, "only print machine-readable JSON to stdout")

	root.AddCommand(
		newEnsureReadyCmd(),
		newStatusCmd(),
		newStopCmd(),
		newRestartCmd(),
		newUpgradeCmd(),
	)

	if err := root.Execute(); err != nil {
		emitError(err)
		os.Exit(1)
	}
}

// ----- subcommands -----

func newEnsureReadyCmd() *cobra.Command {
	var (
		token         string
		preferredNode string
		mode          string
	)
	cmd := &cobra.Command{
		Use:   "ensure-ready",
		Short: "Install + configure + start + verify (the main entry point)",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner(token, preferredNode, mode)
			if err != nil {
				return err
			}
			result, err := r.EnsureReady()
			emit("ensure_ready", result)
			return err
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "subscription token (overrides profile)")
	cmd.Flags().StringVar(&preferredNode, "node", "", "preferred subscription-node name substring")
	cmd.Flags().StringVar(&mode, "mode", "", "routing mode (default: global)")
	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print current daemon and network state (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner("", "", "")
			if err != nil {
				return err
			}
			result, err := r.Status()
			emit("status", result)
			return err
		},
	}
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop daemon and restore the original network configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner("", "", "")
			if err != nil {
				return err
			}
			result, err := r.Stop()
			emit("stop", result)
			return err
		},
	}
}

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Stop and re-run ensure-ready",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner("", "", "")
			if err != nil {
				return err
			}
			result, err := r.Restart()
			emit("restart", result)
			return err
		},
	}
}

func newUpgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Re-verify the pinned binaries and restart the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner("", "", "")
			if err != nil {
				return err
			}
			result, err := r.Upgrade()
			emit("upgrade", result)
			return err
		},
	}
}

// ----- helpers -----

func buildRunner(token, preferredNode, mode string) (*action.Runner, error) {
	switch gf.logLevel {
	case "debug":
		logging.SetLevel(slog.LevelDebug)
	case "warn":
		logging.SetLevel(slog.LevelWarn)
	case "error":
		logging.SetLevel(slog.LevelError)
	default:
		logging.SetLevel(slog.LevelInfo)
	}

	prof, err := config.Load(gf.configPath)
	if err != nil {
		return nil, fmt.Errorf("CONFIG_LOAD_FAILED: %w", err)
	}
	if token != "" {
		prof.SubscriptionToken = token
	}
	if preferredNode != "" {
		prof.PreferredNode = preferredNode
	}
	if mode != "" {
		prof.Mode = mode
	}
	return action.NewRunner(prof, gf.manifestPath)
}

// emit prints the result JSON to stdout. If jsonOnly is false a one-line
// human summary is also written to stderr.
func emit(action string, result any) {
	if result == nil {
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		emitError(err)
		return
	}
	fmt.Fprintln(os.Stdout, string(raw))
	if !gf.jsonOnly {
		fmt.Fprintf(os.Stderr, "[feivpnctl %s] %s\n", action, summarize(action, raw))
	}
}

func emitError(err error) {
	payload := map[string]any{
		"status": "failed",
		"error":  err.Error(),
	}
	raw, _ := json.Marshal(payload)
	fmt.Fprintln(os.Stdout, string(raw))
	if !gf.jsonOnly {
		fmt.Fprintf(os.Stderr, "[feivpnctl error] %s\n", err.Error())
	}
}

func summarize(action string, raw []byte) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return string(raw)
	}
	if s, ok := m["status"].(string); ok {
		return action + " → " + s
	}
	if s, ok := m["running"].(bool); ok {
		if s {
			return "daemon is running"
		}
		return "daemon is not running"
	}
	if s, ok := m["stopped"].(bool); ok && s {
		return "daemon stopped"
	}
	return action + " complete"
}
