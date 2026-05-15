// feivpnctl — bootstrap + ops CLI for the FeiVPN daemon.
//
// Command surface (all subcommands print a single-line JSON document to
// stdout, consumed by Cursor / Claude skills, plus a human summary to
// stderr):
//
// Account
//
//	feivpnctl getid           Anonymous device bootstrap (auto-run on first use)
//	feivpnctl register        Bind device to a new email; persists token
//	feivpnctl login           Email + password login; persists token
//	feivpnctl logout          Drop named-account session back to anonymous
//	feivpnctl whoami          Prints email / expiry / balance (refreshes from server)
//	feivpnctl change-password Rotate account password
//
// Billing
//
//	feivpnctl plans           Lists available plans
//	feivpnctl recharge        Opens recharge URL with token, or prints it
//
// Connection
//
//	feivpnctl ensure-ready    Install + configure + start + verify
//	feivpnctl connect         Alias for ensure-ready
//	feivpnctl disconnect      Alias for stop
//	feivpnctl stop            Stop daemon, restore network
//	feivpnctl restart         Stop → ensure-ready
//	feivpnctl upgrade         Re-verify pinned binary, restart daemon
//	feivpnctl check-upgrade   Compare local pin with /version/check (read-only)
//	feivpnctl status          Read-only health and state inspection
//
// Diagnostics
//
//	feivpnctl test            Egress IP, latency, DNS, reachability
//
// All actions live in internal/action; this file is just argument
// plumbing.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

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
	jsonOnly     bool
}

var gf globalFlags

func main() {
	root := &cobra.Command{
		Use:           "feivpnctl",
		Short:         "FeiVPN bootstrap + ops CLI",
		Long:          "feivpnctl installs, configures, supervises, upgrades, and authenticates against the FeiVPN backend on the current host.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&gf.configPath, "config", "", "path to feivpnctl profile (default: $FEIVPNCTL_CONFIG or /etc/feivpn/feivpnctl.json)")
	root.PersistentFlags().StringVar(&gf.manifestPath, "manifest", "", "path to binaries.manifest.json (default: /opt/feivpn/manifest.json)")
	root.PersistentFlags().StringVar(&gf.logLevel, "log-level", "info", "log verbosity: debug | info | warn | error")
	root.PersistentFlags().BoolVar(&gf.jsonOnly, "json", false, "only print machine-readable JSON to stdout")

	root.AddCommand(
		// Connection
		newEnsureReadyCmd(),
		newConnectCmd(),
		newDisconnectCmd(),
		newStatusCmd(),
		newStopCmd(),
		newRestartCmd(),
		newUpgradeCmd(),
		newCheckUpgradeCmd(),
		newCountriesCmd(),
		// Account
		newGetidCmd(),
		newRegisterCmd(),
		newLoginCmd(),
		newLogoutCmd(),
		newWhoamiCmd(),
		newChangePasswordCmd(),
		// Billing
		newPlansCmd(),
		newRechargeCmd(),
		// Diagnostics
		newTestCmd(),
	)

	if err := root.Execute(); err != nil {
		emitError(err)
		os.Exit(1)
	}
}

// ----- Connection subcommands -----

func newEnsureReadyCmd() *cobra.Command {
	var (
		country      string
		mode         string
		noRouting    bool
		probeTarget  string
		probeTimeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "ensure-ready",
		Short: "Install + configure + start + verify (the main entry point)",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner(country, mode)
			if err != nil {
				return err
			}
			r.SkipRouting = noRouting
			r.ProbeTarget = probeTarget
			r.ProbeTimeout = probeTimeout
			result, err := r.EnsureReady()
			emit("ensure_ready", result)
			return err
		},
	}
	cmd.Flags().StringVar(&country, "country", "", "preferred egress country, ISO alpha-2 (e.g. HK, JP, US); list options with: feivpnctl countries")
	cmd.Flags().StringVar(&mode, "mode", "", "routing mode (default: global)")
	cmd.Flags().BoolVar(&noRouting, "no-routing", false,
		"start daemon + router but do NOT hijack the system route table / DNS "+
			"(safe mode for remote testing; SSH stays on the original gateway)")
	cmd.Flags().StringVar(&probeTarget, "probe-target", "",
		"host:port the post-configureRouting tunnel verifier dials "+
			"(default 1.1.1.1:443; pick something reachable through your tunnel)")
	cmd.Flags().DurationVar(&probeTimeout, "probe-timeout", 0,
		"how long the tunnel verifier waits before rolling back routing (default 6s)")
	return cmd
}

func newConnectCmd() *cobra.Command {
	cmd := newEnsureReadyCmd()
	cmd.Use = "connect"
	cmd.Short = "Alias for ensure-ready"
	return cmd
}

func newDisconnectCmd() *cobra.Command {
	cmd := newStopCmd()
	cmd.Use = "disconnect"
	cmd.Short = "Alias for stop"
	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print current daemon and network state (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner("", "")
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
			r, err := buildRunner("", "")
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
			r, err := buildRunner("", "")
			if err != nil {
				return err
			}
			result, err := r.Restart()
			emit("restart", result)
			return err
		},
	}
}

func newCountriesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "countries",
		Short: "List ISO country codes available in your subscription",
		Long: "Fetches the current subscription, classifies each node by detected\n" +
			"egress country, and prints the ISO 3166-1 alpha-2 codes you can pass\n" +
			"to `--country` (or pin under `preferred_country` in the profile).",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner("", "")
			if err != nil {
				return err
			}
			result, err := r.Countries()
			emit("countries", result)
			return err
		},
	}
}

func newUpgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Re-verify the pinned binaries and restart the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner("", "")
			if err != nil {
				return err
			}
			result, err := r.Upgrade()
			emit("upgrade", result)
			return err
		},
	}
}

func newCheckUpgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check-upgrade",
		Short: "Compare the bundled daemon version with /version/check (no side-effects)",
		Long: "check-upgrade auto-detects the host OS / arch (Linux x86_64 or ARM64,\n" +
			"macOS Apple Silicon or Intel) and queries the FeiVPN backend for the\n" +
			"latest released daemon version.\n\n" +
			"It writes JSON to stdout and never installs, downloads, or restarts\n" +
			"anything. To act on a positive result, run `feivpnctl upgrade` (end\n" +
			"user) or `make sync-bins && git commit` (maintainer).",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner("", "")
			if err != nil {
				return err
			}
			result, err := r.CheckUpgrade()
			emit("check-upgrade", result)
			return err
		},
	}
}

// ----- Account subcommands -----

func newRegisterCmd() *cobra.Command {
	var email, password, passwordFile string
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Bind device to a new email account; persists token",
		RunE: func(cmd *cobra.Command, args []string) error {
			pw, err := resolvePassword(password, passwordFile, "Choose password: ")
			if err != nil {
				return err
			}
			r, err := buildRunner("", "")
			if err != nil {
				return err
			}
			result, err := r.Register(email, pw)
			emit("register", result)
			return err
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Account email (required)")
	cmd.Flags().StringVar(&password, "password", "", "Account password (will prompt if empty)")
	cmd.Flags().StringVar(&passwordFile, "password-file", "", "Read password from file instead of prompting")
	_ = cmd.MarkFlagRequired("email")
	return cmd
}

func newLoginCmd() *cobra.Command {
	var email, password, passwordFile string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Email + password login; persists token",
		RunE: func(cmd *cobra.Command, args []string) error {
			pw, err := resolvePassword(password, passwordFile, "Password: ")
			if err != nil {
				return err
			}
			r, err := buildRunner("", "")
			if err != nil {
				return err
			}
			result, err := r.Login(email, pw)
			emit("login", result)
			return err
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Account email (required)")
	cmd.Flags().StringVar(&password, "password", "", "Account password (will prompt if empty)")
	cmd.Flags().StringVar(&passwordFile, "password-file", "", "Read password from file instead of prompting")
	_ = cmd.MarkFlagRequired("email")
	return cmd
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Drop the named-account session back to anonymous (re-runs getid)",
		Long: "logout does NOT delete account.json. It calls /getid again with the\n" +
			"current device id, which replaces the named-account fields\n" +
			"(auth_data, user_email) with the anonymous baseline. The same uuid\n" +
			"and a fresh subscribe_url remain available for trial usage.",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner("", "")
			if err != nil {
				return err
			}
			result, err := r.Logout()
			emit("logout", result)
			return err
		},
	}
}

func newGetidCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "getid",
		Short: "Anonymous device bootstrap (refreshes uuid + subscribe_url)",
		Long: "getid signs the host's persistent device identity (machine-id on\n" +
			"Linux, IOPlatformUUID on macOS) and exchanges it with the backend\n" +
			"for an anonymous user record (uuid + token + subscribe_url).\n" +
			"Safe to re-run; the server returns the same uuid for the same\n" +
			"device. Other commands invoke this automatically when account.json\n" +
			"does not yet exist.",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner("", "")
			if err != nil {
				return err
			}
			result, err := r.Getid()
			emit("getid", result)
			return err
		},
	}
}

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show current account (refreshes from /user/info or /getid)",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner("", "")
			if err != nil {
				return err
			}
			result, err := r.Whoami()
			emit("whoami", result)
			return err
		},
	}
}

func newChangePasswordCmd() *cobra.Command {
	var newPassword, passwordFile string
	cmd := &cobra.Command{
		Use:   "change-password",
		Short: "Rotate the account password (requires existing login)",
		RunE: func(cmd *cobra.Command, args []string) error {
			pw, err := resolvePassword(newPassword, passwordFile, "New password: ")
			if err != nil {
				return err
			}
			r, err := buildRunner("", "")
			if err != nil {
				return err
			}
			result, err := r.ChangePassword(pw)
			emit("change_password", result)
			return err
		},
	}
	cmd.Flags().StringVar(&newPassword, "new-password", "", "New password (will prompt if empty)")
	cmd.Flags().StringVar(&passwordFile, "password-file", "", "Read new password from file instead of prompting")
	return cmd
}

// ----- Billing subcommands -----

func newPlansCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plans",
		Short: "List subscription plans (uses cached token if logged in)",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner("", "")
			if err != nil {
				return err
			}
			result, err := r.Plans()
			emit("plans", result)
			return err
		},
	}
}

func newRechargeCmd() *cobra.Command {
	var planID string
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "recharge",
		Short: "Open the recharge URL with your token (web payment)",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner("", "")
			if err != nil {
				return err
			}
			result, err := r.Recharge(action.RechargeOptions{PlanID: planID, NoBrowser: noBrowser})
			emit("recharge", result)
			return err
		},
	}
	cmd.Flags().StringVar(&planID, "plan", "", "Plan ID to pre-select on the recharge page")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the URL without spawning a browser")
	return cmd
}

// ----- Diagnostics subcommands -----

func newTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Egress IP, latency, DNS, and reachability checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner("", "")
			if err != nil {
				return err
			}
			result, err := r.Test()
			emit("test", result)
			return err
		},
	}
}

// ----- shared helpers -----

func buildRunner(country, mode string) (*action.Runner, error) {
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
	if country != "" {
		prof.PreferredCountry = country
	}
	if mode != "" {
		prof.Mode = mode
	}
	return action.NewRunner(prof, gf.manifestPath)
}

// resolvePassword fetches a password either from the explicit flag, a
// file, or an interactive prompt (via golang.org/x/term so input does
// not echo). When stdin is not a TTY and neither --password nor
// --password-file is given we return an actionable error rather than
// hanging.
func resolvePassword(flag, file, prompt string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("PASSWORD_FILE_UNREADABLE: %w", err)
		}
		return strings.TrimRight(strings.TrimRight(string(raw), "\n"), "\r"), nil
	}
	if !term.IsTerminal(int(syscall.Stdin)) {
		return "", errors.New("INVALID_ARGUMENT: stdin is not a TTY; pass --password or --password-file")
	}
	fmt.Fprint(os.Stderr, prompt)
	pw, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("PASSWORD_READ_FAILED: %w", err)
	}
	if len(pw) == 0 {
		return "", errors.New("INVALID_ARGUMENT: empty password")
	}
	return string(pw), nil
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

func summarize(act string, raw []byte) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return string(raw)
	}
	headline := act + " complete"
	switch {
	case m["status"] != nil:
		if s, ok := m["status"].(string); ok {
			headline = act + " → " + s
		}
	case m["running"] != nil:
		if s, _ := m["running"].(bool); s {
			headline = "daemon is running"
		} else {
			headline = "daemon is not running"
		}
	case m["stopped"] != nil:
		if s, _ := m["stopped"].(bool); s {
			headline = "daemon stopped"
		}
	}
	if notice, ok := m["notice"].(string); ok && notice != "" {
		headline += "\n  notice: " + notice
	}
	if needs, ok := m["needs_upgrade"].(bool); ok {
		cur, _ := m["current_version"].(string)
		rem, _ := m["remote_version"].(string)
		hostLbl, _ := m["host"].(string)
		if needs {
			headline += fmt.Sprintf("\n  upgrade: %s → %s available for %s", cur, rem, hostLbl)
		} else if rem != "" {
			headline += fmt.Sprintf("\n  up to date (%s) for %s", cur, hostLbl)
		}
		if instr, ok := m["instruction"].(string); ok && instr != "" && needs {
			headline += "\n  instruction: " + instr
		}
	}
	if act == "countries" {
		if buckets, ok := m["countries"].([]any); ok {
			total, _ := m["total"].(float64)
			classified, _ := m["classified"].(float64)
			age, _ := m["age_seconds"].(float64)
			source, _ := m["status"].(string)
			headline += fmt.Sprintf("\n  total: %d nodes (%d classified, source=%s, age=%ds)", int(total), int(classified), source, int(age))
			for _, b := range buckets {
				bm, ok := b.(map[string]any)
				if !ok {
					continue
				}
				code, _ := bm["code"].(string)
				name, _ := bm["display_name"].(string)
				count, _ := bm["count"].(float64)
				headline += fmt.Sprintf("\n  %s  %-10s  %d", code, name, int(count))
			}
			if unknown, ok := m["unknown"].([]any); ok && len(unknown) > 0 {
				headline += fmt.Sprintf("\n  ??  unclassified  %d", len(unknown))
			}
		}
	}
	return headline
}
