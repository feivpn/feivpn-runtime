package action

import (
	"github.com/feivpn/feivpn-runtime/internal/binmgr"
	"github.com/feivpn/feivpn-runtime/internal/config"
	"github.com/feivpn/feivpn-runtime/internal/daemon"
	"github.com/feivpn/feivpn-runtime/internal/feiapi"
	"github.com/feivpn/feivpn-runtime/internal/platform"
	"github.com/feivpn/feivpn-runtime/internal/router"
)

// NewRunner wires up all collaborators using sensible defaults.
// Pass a profile loaded from disk; if nil an empty profile is used.
func NewRunner(profile *config.Profile, manifestPath string) (*Runner, error) {
	if profile == nil {
		profile = &config.Profile{}
	}
	loc := binmgr.New(manifestPath)
	plat, err := platform.Detect()
	if err != nil {
		return nil, err
	}
	return &Runner{
		Locator:  loc,
		Daemon:   daemon.New(loc),
		Router:   router.New(loc),
		Feiapi:   feiapi.New(loc),
		Platform: plat,
		Profile:  profile,
		Paths:    DefaultPaths(),
	}, nil
}
