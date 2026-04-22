package action

import (
	"fmt"

	"github.com/feivpn/feivpn-runtime/internal/logging"
	"github.com/feivpn/feivpn-runtime/internal/store"
)

// Whoami refreshes the locally cached account from the server and
// returns it.
//
//   - If no account.json exists yet, we bootstrap via /getid (so the
//     first call ever still produces a useful answer).
//   - If logged in (auth_data present), we call /user/info to refresh
//     subscribe_url / expired_at / email.
//   - If not logged in, we call /getid again to refresh anonymous quota.
//
// On success the local file is rewritten with the merged result.
func (r *Runner) Whoami() (*AccountResult, error) {
	acc, err := r.loadOrBootstrap()
	if err != nil {
		return nil, err
	}

	id, err := deviceID()
	if err != nil {
		return nil, err
	}

	if !acc.IsLoggedIn() {
		logging.Info("whoami: anonymous; refreshing via getid", "uuid", acc.UUID)
		user, err := r.Feiapi.GetID(id, "")
		if err != nil {
			logging.Warn("whoami: getid refresh failed; returning cached account", "err", err)
			return accountResultFrom(acc, nil, "stale"), nil
		}
		applyUserData(acc, user)
		if err := store.Save(acc); err != nil {
			logging.Warn("whoami: persist refresh failed", "err", err)
		}
		return accountResultFrom(acc, user, "ok"), nil
	}

	logging.Info("whoami: logged in; refreshing via getinfo", "email", acc.UserEmail)
	user, err := r.Feiapi.GetInfo(id, acc.AuthData)
	if err != nil {
		logging.Warn("whoami: getinfo failed; returning cached account", "err", err)
		return accountResultFrom(acc, nil, "stale"), nil
	}
	applyUserData(acc, user)
	if err := store.Save(acc); err != nil {
		logging.Warn("whoami: persist refresh failed", "err", err)
	}
	return accountResultFrom(acc, user, "ok"), nil
}

// AssertLoggedIn returns the cached account or an error tagged with the
// stable code NOT_LOGGED_IN. Used by commands (change-password, plans
// in authenticated mode, etc.) that strictly need a session.
func (r *Runner) AssertLoggedIn() (*store.Account, error) {
	acc, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("NOT_LOGGED_IN: %w", err)
	}
	if !acc.IsLoggedIn() {
		return nil, fmt.Errorf("NOT_LOGGED_IN: device is anonymous; run `feivpnctl login` or `register`")
	}
	return acc, nil
}
