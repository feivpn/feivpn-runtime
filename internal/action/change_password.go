package action

import (
	"fmt"

	"github.com/feivpn/feivpn-runtime/internal/logging"
)

// ChangePassword rotates the operator's account password. Requires an
// existing logged-in session (we use auth_data from the local store).
func (r *Runner) ChangePassword(newPassword string) (*SimpleResult, error) {
	if newPassword == "" {
		return nil, fmt.Errorf("INVALID_ARGUMENT: new password is required")
	}
	acc, err := r.AssertLoggedIn()
	if err != nil {
		return nil, err
	}

	if err := r.Feiapi.ChangePassword(acc.AuthData, newPassword); err != nil {
		return nil, fmt.Errorf("CHANGE_PASSWORD_FAILED: %w", err)
	}

	logging.Info("change_password: success", "email", acc.UserEmail)
	return &SimpleResult{Status: "ok"}, nil
}
