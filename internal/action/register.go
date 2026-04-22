package action

import (
	"fmt"

	"github.com/feivpn/feivpn-runtime/internal/logging"
	"github.com/feivpn/feivpn-runtime/internal/store"
)

// Register binds the host's persistent device identity (read from the
// OS — see internal/device) to a new email account. The server "binds"
// the existing anonymous quota to the email, so any usage already
// recorded against this device carries over.
func (r *Runner) Register(email, password string) (*AccountResult, error) {
	if email == "" || password == "" {
		return nil, fmt.Errorf("INVALID_ARGUMENT: email and password are required")
	}

	id, err := deviceID()
	if err != nil {
		return nil, err
	}

	logging.Info("register: starting", "email", email)
	user, err := r.Feiapi.Register(id, email, password)
	if err != nil {
		return nil, fmt.Errorf("REGISTER_FAILED: %w", err)
	}
	if user.UUID == "" {
		return nil, fmt.Errorf("REGISTER_FAILED: server returned empty uuid")
	}

	acc, _ := store.Load()
	if acc == nil {
		acc = &store.Account{}
	}
	applyUserData(acc, user)
	if acc.UserEmail == "" {
		acc.UserEmail = email
	}
	if err := store.Save(acc); err != nil {
		return nil, fmt.Errorf("ACCOUNT_PERSIST_FAILED: %w", err)
	}

	logging.Info("register: success", "email", acc.UserEmail, "uuid", acc.UUID, "is_new", user.IsNew)
	return accountResultFrom(acc, user, "ok"), nil
}
