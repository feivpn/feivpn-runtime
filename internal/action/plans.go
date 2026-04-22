package action

import (
	"github.com/feivpn/feivpn-runtime/internal/logging"
	"github.com/feivpn/feivpn-runtime/internal/store"
)

// Plans returns the subscription plan list. If the operator is logged in
// we use the authenticated endpoint (so the server can personalize the
// list); otherwise we hit the public default-plans endpoint.
//
// We also pull the recharge URL from `appconfig` so the operator can see
// where `feivpnctl recharge` is going to send them.
func (r *Runner) Plans() (*PlansResult, error) {
	res := &PlansResult{Status: "ok"}

	authData := ""
	if acc, err := store.Load(); err == nil && acc.IsLoggedIn() {
		authData = acc.AuthData
		res.Authenticated = true
	}

	plans, err := r.Feiapi.FetchPlans(authData)
	if err != nil {
		return nil, err
	}
	res.Plans = plans
	res.Count = len(plans)

	if cfg, err := r.Feiapi.FetchAppConfig(); err != nil {
		logging.Warn("plans: appconfig lookup failed; recharge URL omitted", "err", err)
	} else {
		res.RechargeURL = cfg.RechargeURL
	}

	return res, nil
}
