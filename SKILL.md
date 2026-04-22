---
name: FeiVPN Bootstrap Skill
description: Convert the current Linux or macOS host into a fully-running FeiVPN endpoint. Installs and supervises the FeiVPN daemon, manages TUN/route/DNS, and reports machine-readable status. Use when the user wants their *current machine* to be on a VPN they control end-to-end.
when_to_use: |
  Use this skill when the user (or another agent) asks any of:
    - "set up a VPN on this machine"
    - "make this server route everything through FeiVPN"
    - "ensure feivpn is running"
    - "bring the VPN back up", "restart the VPN", "stop the VPN"
    - "upgrade the FeiVPN daemon"
  Do NOT use this skill to design a new VPN protocol or to manage VPNs
  on hosts other than the one the agent is executing on.
tools_needed: [Shell]
---

# FeiVPN Bootstrap Skill

This skill drives the `feivpnctl` CLI. The agent should treat
`feivpnctl` as a black box: every subcommand prints exactly one
machine-readable JSON document to stdout (and one human-readable
summary to stderr).

## Prerequisites the agent must verify before any action

1. `feivpnctl` is on `$PATH`. If not, run the installer first:
   ```bash
   curl -fsSL https://raw.githubusercontent.com/feivpn/feivpn-runtime/main/scripts/install.sh \
     | sudo bash
   ```
2. The host is Linux (amd64/arm64) or macOS (arm64). The skill is a
   no-op on other platforms (`ensure-ready` will return
   `"status": "failed"` with `UNSUPPORTED_PLATFORM`).
3. The agent has root. All commands below must be run with `sudo`.

## The five actions

Every action accepts `--json` to suppress the human summary on stderr.
The agent should ALWAYS pass `--json` and parse stdout.

### 1. `ensure_feivpn_ready`  →  `feivpnctl ensure-ready`

Main entry point. Idempotent: safe to call repeatedly.

**Inputs (CLI flags or `/etc/feivpn/feivpnctl.json` profile)**
- `--token <subscription_token>` (required if not in profile)
- `--node <substring>` (optional, pin a specific egress)
- `--mode global` (only mode in MVP)

**Output (JSON on stdout)**
```json
{
  "status": "ready",
  "platform": "darwin-arm64",
  "version": "1.0.0",
  "pid": 48321,
  "tun": "utun7",
  "checks": {
    "process": true,
    "tun": true,
    "route": true,
    "dns": true,
    "connectivity": true
  }
}
```

**Status decoding**
| status     | what the agent should do                                              |
| ---------- | --------------------------------------------------------------------- |
| `ready`    | done; report success to user                                          |
| `degraded` | daemon is up but at least one check is `false`; run `status` for why  |
| `failed`   | bootstrap aborted; surface `errors[]` to the user                     |

### 2. `status_feivpn`  →  `feivpnctl status`

Read-only. Combines the OS service-manager view, the daemon's
`state.json`, and a live `feivpn --health` probe.

**Output (JSON on stdout)**
```json
{
  "running": true,
  "platform": "linux-amd64",
  "service": {"manager": "systemd", "active": true},
  "state":   { "...": "see schema/daemon-state.schema.json" },
  "health":  { "...": "see schema/daemon-health.schema.json" }
}
```

### 3. `stop_feivpn`  →  `feivpnctl stop`

Stops the daemon AND restores the original default route + DNS via
`feivpn --recover`. Both steps are best-effort.

**Output**
```json
{ "stopped": true, "recovery": true }
```

### 4. `restart_feivpn`  →  `feivpnctl restart`

Equivalent to `stop` followed by `ensure-ready`. Use this whenever the
profile or the pinned binaries change.

### 5. `upgrade_feivpn`  →  `feivpnctl upgrade`

Re-verifies the bytes in `/opt/feivpn/bin/` against
`/opt/feivpn/manifest.json`, stops the daemon, and runs `ensure-ready`
to bring the new version online. To actually pull a NEW version, the
operator must first re-run the installer (`scripts/install.sh`) for the
desired tag — `feivpnctl upgrade` does NOT perform a network download.

## Error contract

A failed call returns:
```json
{ "status": "failed", "error": "BINARY_MISSING: ..." }
```

The error string starts with one of these stable codes:

| Code                             | Meaning                                                |
| -------------------------------- | ------------------------------------------------------ |
| `UNSUPPORTED_PLATFORM`           | OS/arch not in the manifest                            |
| `BINARY_MISSING`                 | `feivpn` or `feiapi` not found in any locator path     |
| `BINARY_CHECKSUM_MISMATCH`       | on-disk SHA256 differs from the manifest entry         |
| `CONFIG_INCOMPLETE`              | profile is missing `subscription_token` etc.           |
| `SUBSCRIPTION_FETCH_FAILED`      | upstream `feiapi getconfig` failed                     |
| `CHECK_FAILED`                   | `feivpn --check` rejected the rendered config          |
| `SERVICE_INSTALL_FAILED`         | systemd / launchd refused the unit                     |
| `SERVICE_START_FAILED`           | systemctl/launchctl failed                             |
| `HEALTH_TIMEOUT`                 | daemon started but never reported full health          |
| `RECOVER_FAILED`                 | `feivpn --recover` exited non-zero                     |
| `API_NETWORK_FAILURE`            | all backend domains unreachable                        |
| `API_AUTH_REJECTED`              | API signature rejected                                 |
| `API_LOGICAL_ERROR`              | API returned `code != 0`                               |

## Recommended agent flow

```pseudo
result = sh("sudo feivpnctl ensure-ready --json --token $TOKEN")
case result.status:
  "ready":
    say "VPN is up — egress: " + result.connectivity.egress_ip
  "degraded":
    detail = sh("sudo feivpnctl status --json")
    explain_degradation(detail)
  "failed":
    classify_error(result.error)        # surface code + friendly message
```

Always run `sudo feivpnctl stop` at the end of an interactive session
if the user explicitly opted into a *temporary* VPN.

## Where to learn more

- `schema/feivpnctl-output.schema.json` — full machine-readable contract
- `schema/feivpnctl-config.schema.json` — profile shape
- `manifest/binaries.manifest.json` — pinned upstream daemon + API binaries
- `docs/architecture.md` — the 3-layer design (skill / daemon / host adapter)
