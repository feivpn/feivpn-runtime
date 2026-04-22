package action

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"

	"github.com/feivpn/feivpn-runtime/internal/logging"
	"github.com/feivpn/feivpn-runtime/internal/store"
)

// RechargeOptions controls how `feivpnctl recharge` behaves.
type RechargeOptions struct {
	// PlanID, if set, is appended as a query param so the recharge page
	// pre-selects the plan.
	PlanID string
	// NoBrowser disables the `open` / `xdg-open` invocation: the URL is
	// printed (and returned) but the operator opens it themselves.
	NoBrowser bool
}

// Recharge fetches the recharge URL from the backend appconfig, splices
// the operator's auth token + optional plan ID into it, and asks the OS
// to open it in a browser. On a headless host (no DISPLAY, no `open`
// binary, --no-browser) the URL is just returned for the operator to
// open manually.
func (r *Runner) Recharge(opts RechargeOptions) (*RechargeResult, error) {
	cfg, err := r.Feiapi.FetchAppConfig()
	if err != nil {
		return nil, fmt.Errorf("APPCONFIG_FAILED: %w", err)
	}
	if cfg.RechargeURL == "" {
		return nil, fmt.Errorf("APPCONFIG_INCOMPLETE: server returned no recharge_url")
	}

	target := cfg.RechargeURL

	if acc, err := store.Load(); err == nil && acc.Token != "" {
		target = appendQuery(target, "token", acc.Token)
		if acc.UserEmail != "" {
			target = appendQuery(target, "email", acc.UserEmail)
		}
	} else if err != nil {
		logging.Warn("recharge: no account on disk; URL will not include token", "err", err)
	}
	if opts.PlanID != "" {
		target = appendQuery(target, "plan_id", opts.PlanID)
	}

	res := &RechargeResult{
		Status: "ok",
		URL:    target,
	}

	if opts.NoBrowser {
		res.Notes = "browser launch suppressed via --no-browser; open the URL manually"
		return res, nil
	}

	cmd, args := openerCommand()
	if cmd == "" {
		res.Notes = "no browser opener available on this host (set DISPLAY or use --no-browser); URL printed above"
		return res, nil
	}

	res.OpenCommand = cmd
	allArgs := append(args, target)
	if err := exec.Command(cmd, allArgs...).Start(); err != nil {
		logging.Warn("recharge: opener failed", "cmd", cmd, "err", err)
		res.Notes = "opener failed: " + err.Error()
		return res, nil
	}
	res.Opened = true
	logging.Info("recharge: opened in browser", "cmd", cmd)
	return res, nil
}

func appendQuery(rawURL, key, value string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}

// openerCommand returns the platform-appropriate URL opener, or "" if
// none can be found.
func openerCommand() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		if path, err := exec.LookPath("open"); err == nil {
			return path, nil
		}
	case "linux":
		for _, candidate := range []string{"xdg-open", "gio", "wslview"} {
			if path, err := exec.LookPath(candidate); err == nil {
				if candidate == "gio" {
					return path, []string{"open"}
				}
				return path, nil
			}
		}
	case "windows":
		if path, err := exec.LookPath("rundll32"); err == nil {
			return path, []string{"url.dll,FileProtocolHandler"}
		}
	}
	return "", nil
}
