package action

import (
	"errors"
	"fmt"
	"time"

	"github.com/feivpn/feivpn-runtime/internal/device"
	"github.com/feivpn/feivpn-runtime/internal/feiapi"
	"github.com/feivpn/feivpn-runtime/internal/logging"
	"github.com/feivpn/feivpn-runtime/internal/store"
)

// deviceID reads the OS-issued device identifier and translates the
// device package's ErrUnavailable into a stable error code that the
// CLI / agent can match on.
func deviceID() (string, error) {
	id, err := device.ID()
	if err != nil {
		if errors.Is(err, device.ErrUnavailable) {
			return "", fmt.Errorf("DEVICE_ID_UNAVAILABLE: %w", err)
		}
		return "", err
	}
	return id, nil
}

// applyUserData merges a fresh UserData payload into an Account. Empty
// fields in UserData do NOT overwrite existing values — that way a
// partial /user/info response does not erase the subscribe_url we got
// from the previous /getid.
func applyUserData(acc *store.Account, u *feiapi.UserData) {
	if u == nil {
		return
	}
	if u.UUID != "" {
		acc.UUID = u.UUID
	}
	if u.Token != "" {
		acc.Token = u.Token
	}
	// AuthData is treated specially: an empty value means "the server
	// is reporting an anonymous account", which is exactly what
	// `logout` wants. We clear AuthData in that case.
	acc.AuthData = u.AuthData
	if u.SubscribeURL != "" {
		acc.SubscribeURL = u.SubscribeURL
	}
	if u.ExpiredAt != nil {
		acc.ExpiredAt = *u.ExpiredAt
	}
	if e := u.ResolvedEmail(); e != "" {
		acc.UserEmail = e
	} else if u.AuthData == "" {
		// Anonymous account → drop any cached email so the file is
		// internally consistent with the "logged-out" state.
		acc.UserEmail = ""
	}
	if u.InviteCode != "" {
		acc.InviteCode = u.InviteCode
	}
	acc.IsAdmin = u.IsAdmin
	acc.UpdatedAt = time.Now().Unix()
}

// loadOrBootstrap returns the current Account, calling /getid first if
// the file does not yet exist. Used by every command that needs an
// Account but does not itself require the user to be logged in.
//
// Logged-in commands should add their own check: `if !acc.IsLoggedIn()`.
func (r *Runner) loadOrBootstrap() (*store.Account, error) {
	if acc, err := store.Load(); err == nil {
		return acc, nil
	} else if !errors.Is(err, store.ErrNoAccount) {
		return nil, err
	}
	logging.Info("account: file missing, bootstrapping via getid")
	return r.bootstrapAnonymous()
}

// bootstrapAnonymous fetches a fresh anonymous identity from /getid and
// writes it to the store. Returns the saved Account.
func (r *Runner) bootstrapAnonymous() (*store.Account, error) {
	id, err := deviceID()
	if err != nil {
		return nil, err
	}
	user, err := r.Feiapi.GetID(id, "")
	if err != nil {
		return nil, fmt.Errorf("GETID_FAILED: %w", err)
	}
	if user.UUID == "" {
		return nil, fmt.Errorf("GETID_FAILED: server returned empty uuid")
	}
	acc := &store.Account{}
	applyUserData(acc, user)
	if err := store.Save(acc); err != nil {
		return nil, fmt.Errorf("ACCOUNT_PERSIST_FAILED: %w", err)
	}
	return acc, nil
}

// accountResultFrom builds the JSON-shaped AccountResult the CLI prints.
// Both the cached Account and the optional fresh UserData (for the
// per-day usage counters) feed in.
func accountResultFrom(acc *store.Account, u *feiapi.UserData, status string) *AccountResult {
	res := &AccountResult{
		Status:       status,
		UUID:         acc.UUID,
		Email:        acc.UserEmail,
		SubscribeURL: acc.SubscribeURL,
		Token:        acc.Token,
		AuthData:     acc.AuthData,
	}
	if acc.ExpiredAt != 0 {
		v := acc.ExpiredAt
		res.ExpiredAt = &v
	}
	if u != nil {
		res.IsNew = u.IsNew
		if u.UsageTimeBalance != nil {
			res.UsageBalance = u.UsageTimeBalance
		}
		// Fire the new-user trial hint only for fresh ANONYMOUS
		// accounts — a brand-new register/login response is also
		// is_new=true but the operator has just claimed a full
		// account, so the "claim full account" hint would be wrong.
		if u.IsNew && u.AuthData == "" {
			res.Notice = NewUserTrialNotice
		}
	}
	return res
}

// NewUserTrialNotice is what we print whenever the server flags a
// fresh-account is_new=true. The 30-minute window is a fixed
// product-side policy of the FeiVPN backend; we hard-code it here so
// the CLI stays self-explanatory without an extra round-trip.
const NewUserTrialNotice = "New device detected — you have a 30-minute free trial. Run `feivpnctl ensure-ready` to start, or `feivpnctl register --email <e>` to claim a full account."
