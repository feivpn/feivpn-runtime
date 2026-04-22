package action

import (
	"github.com/feivpn/feivpn-runtime/internal/logging"
)

// Logout drops the current named-account session by re-running /getid.
// We do NOT delete account.json: the device-bound uuid + subscribe_url
// are still useful (anonymous trial mode), and the account file always
// exists by invariant. After logout, IsLoggedIn() returns false and the
// operator can `feivpnctl login` (or `register`) again.
func (r *Runner) Logout() (*AccountResult, error) {
	acc, err := r.bootstrapAnonymous()
	if err != nil {
		return nil, err
	}
	logging.Info("logout: dropped to anonymous", "uuid", acc.UUID)
	return accountResultFrom(acc, nil, "logged_out"), nil
}
