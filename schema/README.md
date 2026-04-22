# `schema/`

Intentionally minimal. **Contracts live in source code**, not in
hand-maintained schema files:

| What                                          | Source of truth                                                |
| --------------------------------------------- | -------------------------------------------------------------- |
| `feivpnctl` stdout payloads                   | `internal/action/types.go` (Go structs, JSON-encoded)          |
| `/etc/feivpn/feivpnctl.json` profile          | `internal/config/config.go` (`Profile` struct)                 |
| daemon `state.json`                           | `internal/state/state.go` here, mirrored from upstream         |
| daemon `--health` output                      | `internal/daemon/daemon.go` here, mirrored from upstream       |

We deliberately do not ship JSON Schema files: this is a single-maintainer
project and parallel schemas drift away from the code within weeks. If
you need to validate a payload, JSON-decode it into the matching Go
struct — that's the contract.
