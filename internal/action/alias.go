package action

import "github.com/feivpn/feivpn-runtime/internal/daemon"

// daemonHealth is an alias for daemon.HealthReport so that
// ensure_ready.go can refer to it without re-importing the package
// at every call site.
type daemonHealth = daemon.HealthReport
