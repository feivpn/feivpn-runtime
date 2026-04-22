package action

import (
	"fmt"

	"github.com/feivpn/feivpn-runtime/internal/logging"
	"github.com/feivpn/feivpn-runtime/internal/store"
)

// Login performs an email + password login against the backend and
// merges the returned UserData into the local account file. Subsequent
// `feivpnctl ensure-ready` invocations can then be called without any
// flag — the subscribe_url comes from the store.
func (r *Runner) Login(email, password string) (*AccountResult, error) {
	if email == "" || password == "" {
		return nil, fmt.Errorf("INVALID_ARGUMENT: email and password are required")
	}

	logging.Info("login: starting", "email", email)
	user, err := r.Feiapi.Login(email, password)
	if err != nil {
		return nil, fmt.Errorf("LOGIN_FAILED: %w", err)
	}
	if user.UUID == "" {
		return nil, fmt.Errorf("LOGIN_FAILED: server returned empty uuid")
	}
	if user.AuthData == "" {
		return nil, fmt.Errorf("LOGIN_FAILED: server returned empty auth_data")
	}

	// Start from whatever's on disk so we keep the device-bound uuid
	// even if the server's login response ever stops echoing it.
	acc, _ := store.Load()
	if acc == nil {
		acc = &store.Account{}
	}
	applyUserData(acc, user)
	// The server doesn't echo the email back on login; fall back to
	// the input the operator supplied.
	if acc.UserEmail == "" {
		acc.UserEmail = email
	}
	if err := store.Save(acc); err != nil {
		return nil, fmt.Errorf("ACCOUNT_PERSIST_FAILED: %w", err)
	}

	logging.Info("login: success", "email", acc.UserEmail, "uuid", acc.UUID)
	return accountResultFrom(acc, user, "ok"), nil
}
