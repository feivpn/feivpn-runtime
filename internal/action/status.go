package action

import (
	"github.com/feivpn/feivpn-runtime/internal/binmgr"
)

// Status is read-only: it never modifies the host. It combines:
//   - service-manager view  (systemctl is-active / launchctl print)
//   - state.json view       (what the daemon last persisted)
//   - feivpn --health view  (live introspection if the daemon is up)
func (r *Runner) Status() (*StatusResult, error) {
	res := &StatusResult{
		Platform: binmgr.PlatformKey(),
		Service: ServiceState{
			Manager: r.Platform.Name(),
		},
		Router: ServiceState{
			Manager: r.Platform.Name(),
		},
	}

	active, _ := r.Platform.IsActive()
	res.Service.Active = active

	routerActive, _ := r.Platform.IsRouterActive()
	res.Router.Active = routerActive

	if st, err := r.readState(); err == nil {
		res.State = st
	}

	if active {
		if h, err := r.Daemon.Health(); err == nil {
			res.Health = h
			res.Running = h.Running
		} else {
			res.Running = true
		}
	}
	return res, nil
}
