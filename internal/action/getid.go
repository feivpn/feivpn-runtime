package action

import (
	"github.com/feivpn/feivpn-runtime/internal/logging"
)

// Getid runs an anonymous device-identity bootstrap against the
// backend. It is safe to re-run: the server returns the same UUID for
// a given device id, so calling getid again just refreshes the cached
// subscribe_url / quotas without changing the operator's identity.
//
// This is also the operation that `logout` reuses to drop a logged-in
// session back to the anonymous baseline.
func (r *Runner) Getid() (*AccountResult, error) {
	acc, err := r.bootstrapAnonymous()
	if err != nil {
		return nil, err
	}
	logging.Info("getid: ok", "uuid", acc.UUID, "logged_in", acc.IsLoggedIn())
	return accountResultFrom(acc, nil, "ok"), nil
}
