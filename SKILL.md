---
name: FeiVPN Bootstrap Skill
description: Convert the current Linux or macOS host into a fully-running FeiVPN endpoint. Installs and supervises the FeiVPN daemon, manages account login + payment, TUN/route/DNS, and reports machine-readable status. Use when the user wants their *current machine* to be on a VPN they control end-to-end.
when_to_use: |
  Use this skill when the user (or another agent) asks any of:
    - "set up a VPN on this machine"
    - "make this server route everything through FeiVPN"
    - "ensure feivpn is running"
    - "log into my feivpn account from this server"
    - "register a new feivpn account"
    - "show my plan / balance / expiry"
    - "open the recharge page"
    - "test the VPN connection"
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
summary to stderr). Always pass `--json` and parse stdout.

## Prerequisites the agent must verify before any action

1. `feivpnctl` is on `$PATH`. If not, run the installer first:
   ```bash
   curl -fsSL https://raw.githubusercontent.com/feivpn/feivpn-runtime/main/scripts/install.sh \
     | sudo bash
   ```
2. The host is Linux (amd64/arm64) or macOS (arm64 Apple Silicon /
   amd64 Intel). The skill is a no-op on other platforms
   (`ensure-ready` will return `"status": "failed"` with
   `UNSUPPORTED_PLATFORM`).
3. The agent has root for any command that touches the daemon
   (`ensure-ready`, `connect`, `disconnect`, `stop`, `restart`,
   `upgrade`, `status`, `test`). Pure account / billing commands
   (`getid`, `login`, `register`, `whoami`, `plans`, `recharge`,
   `change-password`, `logout`) only need write access to
   `$FEIVPN_ACCOUNT_FILE` (default `/var/lib/feivpn/account.json`).

## Command surface

```text
Account
  feivpnctl getid                                  # anonymous bootstrap
  feivpnctl register --email <e> [--password-file <f>]
  feivpnctl login    --email <e> [--password-file <f>]
  feivpnctl logout                                 # → anonymous (re-runs getid)
  feivpnctl whoami
  feivpnctl change-password [--password-file <f>]

Billing
  feivpnctl plans
  feivpnctl recharge [--plan <id>] [--no-browser]

Connection
  feivpnctl ensure-ready                     # main entry point
  feivpnctl connect                          # alias for ensure-ready
  feivpnctl disconnect                       # alias for stop
  feivpnctl stop
  feivpnctl restart
  feivpnctl upgrade
  feivpnctl check-upgrade
  feivpnctl status

Diagnostics
  feivpnctl test
```

## Process model

`feivpnctl` itself never touches the kernel. It orchestrates **two**
long-lived service units, both shipped under `bin/` in this repo and
managed by the platform's native service manager (systemd on Linux,
launchd on macOS):

| Unit            | Binary                          | Privilege | Owns                                              |
| --------------- | ------------------------------- | --------- | ------------------------------------------------- |
| `feivpn-router` | `bin/feivpn-router-<platform>`  | **root**  | route table mutations, `/etc/resolv.conf` (Linux) / `scutil` DNS (macOS), serves an RPC socket |
| `feivpn`        | `bin/feivpn-<platform>`         | user      | TUN device + tun2socks data plane, dials the router for every privileged op |

The router socket is local-only:

| OS    | Endpoint                          |
| ----- | --------------------------------- |
| Linux | `unix:/var/run/feivpn_controller` |
| macOS | `tcp:127.0.0.1:38964`             |

Lifecycle ordering, encoded both in `feivpnctl` and in the systemd /
launchd units (so reboots replay the same sequence even when
`feivpnctl` is not in the loop):

- **start**: router → daemon
- **stop**: daemon (SIGTERM) → `feivpn --recover` (route/DNS rollback over the router socket) → router

Agents do not need to manage the two units separately. `feivpnctl
ensure-ready` / `stop` / `restart` / `upgrade` already cover the
ordering. `feivpnctl status` reports both:

```json
{
  "running": true,
  "service": { "manager": "launchd", "active": true },
  "router":  { "manager": "launchd", "active": true }
}
```

A "router off, daemon on" state is impossible in steady state — the
daemon `Requires=feivpn-router.service` (Linux) and refuses to come
up if the router socket is unreachable. If `status` ever reports it,
treat it as a partial-shutdown bug and run `feivpnctl restart`.

## Local store contract

`feivpnctl` keeps **one** file outside the daemon's own state:

| File                                                            | Purpose                                                           | Mode |
| --------------------------------------------------------------- | ----------------------------------------------------------------- | ---- |
| `$FEIVPN_ACCOUNT_FILE` (default `/var/lib/feivpn/account.json`) | mirror of the server's UserData payload (uuid, token, auth_data, subscribe_url, expired_at, user_email, invite_code, updated_at) | 0600 |

The file is created on the first identity call (`getid` / `login` /
`register`) and is **never deleted** by the CLI. `logout` re-runs
`getid` and overwrites the named-account fields with the anonymous
baseline; the `uuid` and a fresh `subscribe_url` remain.

The persistent **device identity** is read from the OS on every call
(no caching by us):

| Platform | Source                                                   |
| -------- | -------------------------------------------------------- |
| Linux    | `/etc/machine-id` (fallback `/var/lib/dbus/machine-id`)  |
| macOS    | `IOPlatformUUID` from `ioreg -rd1 -c IOPlatformExpertDevice` |

If neither is available the CLI returns `DEVICE_ID_UNAVAILABLE`. We
never substitute a self-generated random UUID — that would let one
machine register as multiple devices on the backend.

**Logged-in semantics**: `auth_data` non-empty ⇒ logged in. This matches
the TS client's auth_manager.ts.

## Refresh policy

| Command                              | Hits the server?                          | Updates `account.json`? |
| ------------------------------------ | ----------------------------------------- | ----------------------- |
| `getid`                              | `/getid`                                  | yes (full)              |
| `login`                              | `/passport/auth/login`                    | yes (full)              |
| `register`                           | `/passport/auth/bind`                     | yes (full)              |
| `logout`                             | `/getid`                                  | yes (anonymizes)        |
| `whoami`                             | `/user/info` (logged in) or `/getid`      | yes (merge)             |
| `ensure-ready` / `connect`           | `/user/info` or `/getid`, then `/getconfig` | yes (merge subscribe_url) |
| `plans`                              | `/user/plan/fetch` or `/guest/...`        | no                      |
| `recharge`                           | `/guest/comm/appConfig`                   | no                      |
| `change-password`                    | `/user/changePassword`                    | no                      |
| `status` / `stop` / `disconnect` / `restart` / `upgrade` / `test` | none for the account | no                      |

Rule of thumb: any command that hits a *user-identity* endpoint also
flushes the result to disk, so the next read sees the latest
`subscribe_url` and `expired_at`. Pure local / daemon commands never
touch the file.

If the file is missing when a command needs it, that command auto-runs
`getid` first (one extra round-trip). After that, every command finds
the file in place.

## Selected actions in detail

Every action accepts `--json`; we only show stdout shapes here.

### `feivpnctl ensure-ready`

Idempotent main entry point. Inputs come from (highest priority first):
`--token` flag → `subscription_token` in
`/etc/feivpn/feivpnctl.json` → `subscribe_url` in the account file.

```json
{
  "status": "ready",
  "platform": "darwin-arm64",
  "version": "1.0.0",
  "pid": 48321,
  "tun": "utun7",
  "checks": { "process": true, "tun": true, "route": true, "dns": true, "connectivity": true }
}
```

| status     | what the agent should do                                              |
| ---------- | --------------------------------------------------------------------- |
| `ready`    | done; report success                                                  |
| `degraded` | daemon is up but at least one check is `false`; run `status` for why  |
| `failed`   | bootstrap aborted; surface `errors[]` to the user                     |

### `feivpnctl getid` / `login` / `register` / `logout` / `whoami`

All five emit the same payload shape (only the `status` field differs):

```json
{
  "status": "ok",
  "uuid": "413852962",
  "email": "u@example.com",
  "subscribe_url": "https://...",
  "token": "raw token",
  "auth_data": "Bearer ...",
  "expired_at": 1801583999,
  "is_new": false,
  "usage_time_balance": 1374360,
  "notice": "New device detected — you have a 30-minute free trial..."
}
```

| status        | meaning                                                            |
| ------------- | ------------------------------------------------------------------ |
| `ok`          | fresh refresh from the server                                      |
| `stale`       | refresh failed, returning the on-disk snapshot                     |
| `logged_out`  | only `logout` returns this; `auth_data` will be empty in the JSON  |

For the anonymous case (`getid`, or `whoami` before login), `email` and
`auth_data` will be empty strings (or omitted). Agents should treat
`auth_data` non-empty as the source of truth for "logged in".

`notice` is populated when the server marks the response with
`is_new=true` AND `auth_data` is empty — i.e. a brand-new anonymous
device. It currently announces the 30-minute free trial. Agents should
surface it verbatim to the user when present; never present.

Device id is obtained automatically from the OS (Linux
`/etc/machine-id`, macOS `IOPlatformUUID`); operators / agents do not
pass it on the command line.

### `feivpnctl plans`

```json
{
  "status": "ok",
  "authenticated": true,
  "count": 3,
  "plans": [ { "id": 1, "name": "Monthly", "month_price": 1000 }, ... ],
  "recharge_url": "https://feivpn.world/recharge"
}
```

### `feivpnctl recharge [--plan ID] [--no-browser]`

Splices the operator's `token` and `email` (if logged in) into the
recharge URL. With `--no-browser`, returns the URL only. Otherwise tries
`open` / `xdg-open` / `rundll32`.

```json
{
  "status": "ok",
  "url": "https://feivpn.world/recharge?token=...&email=...&plan_id=1",
  "opened": true,
  "open_command": "/usr/bin/xdg-open"
}
```

### `feivpnctl test`

Egress IP, DNS, TUN, and reachability checks. Status is `ok` when DNS
resolves AND at least one reachability target succeeds; `partial` when
some targets succeed; `failed` otherwise.

```json
{
  "status": "ok",
  "egress_ip": "203.0.113.5",
  "egress_ip_via": "https://api.ipify.org",
  "egress_latency_ms": 132,
  "dns": { "servers": ["1.1.1.1"], "resolved": "104.16.x.x", "ok": true },
  "tun": { "up": true, "name": "utun7" },
  "reachability": [
    { "target": "https://www.google.com/generate_204", "ok": true, "status": 204 }
  ]
}
```

### `feivpnctl status` / `stop` / `restart` / `upgrade`

Same shapes as previously; see `internal/action/types.go` for the exact
Go structs.

### Server-side `platform` tag policy (applies to ALL endpoints)

`feivpnctl` is one logical client and presents a single, dedicated
platform tag to **every** backend call — `/getid`, `/info`, `/register`,
`/login`, `/version/check`, `/plans`, `/appconfig`, `/getconfig`, etc.:

| Host  | `?platform=` value     |
| ----- | ---------------------- |
| Linux | `feivpn-runtime-linux` |
| macOS | `feivpn-runtime-mac`   |

> **Why not reuse `linux` / `mac`?** The desktop Electron client uses
> bare `linux` / `mac` / `win` / `ios` / `android` / `web`. Reusing
> those values here would conflate two independent products on the
> server — version-check would return whichever release the desktop
> channel advertises, analytics would double-count users, and rate
> limits would tangle. The `feivpn-runtime-{linux,mac}` namespace lets
> the server dispatch the skill (bundled `feivpn` + `feiapi` +
> `feivpn-router` triple) as its own first-class client.
>
> **Server-side requirement**: register `feivpn-runtime-linux` and
> `feivpn-runtime-mac` as valid `platform` enum values across every
> endpoint, and provision a separate `/version/check` release channel
> for them. Until that lands, version-check will fail / fall through.

The tag is auto-injected by the `feiapi` wrapper (see
`internal/feiapi/feiapi.go`); skill subcommands never need to think
about it.

### `feivpnctl check-upgrade`

Read-only diff against `/api/v1/version/check`. **Never** downloads,
swaps, or restarts. Auto-detects the host and uses the
`feivpn-runtime-{linux,mac}` tag described above. No flags — the skill
manages this machine and only this machine.

```json
{
  "status": "ok",
  "component": "feivpn",
  "host": "macOS Apple Silicon",
  "platform": "feivpn-runtime-mac",
  "architecture": "arm64",
  "manifest_key": "darwin-arm64",
  "current_version": "0.1.0",
  "remote_version": "0.2.0",
  "needs_upgrade": true,
  "force_update": false,
  "changelog": "...",
  "update_url": "https://github.com/feivpn/feivpn-apps/releases/...",
  "upgrade": {
    "installer_url": "https://raw.githubusercontent.com/feivpn/feivpn-runtime/main/scripts/install.sh",
    "target_tag": "v0.2.0",
    "command": "curl -fsSL https://raw.githubusercontent.com/feivpn/feivpn-runtime/main/scripts/install.sh | sudo bash -s -- --tag v0.2.0 && sudo feivpnctl upgrade",
    "steps": [
      "curl -fsSL https://raw.githubusercontent.com/feivpn/feivpn-runtime/main/scripts/install.sh | sudo bash -s -- --tag v0.2.0",
      "sudo feivpnctl upgrade"
    ],
    "requires_root": true
  },
  "instruction": "New daemon release available. Execute upgrade.command as root, or run the two upgrade.steps in order."
}
```

Status semantics:

| status   | meaning                                                                |
| -------- | ---------------------------------------------------------------------- |
| `ok`     | server reachable; `needs_upgrade` is the canonical answer              |
| `stale`  | server unreachable; only `current_version` is trustworthy              |
| `failed` | manifest unreadable or platform unmapped; `error` field has the reason |

Agents should treat `force_update=true` as "block VPN start until
upgraded" and `needs_upgrade=true && !force_update` as "remind user but
proceed".

**How agents act on `needs_upgrade=true`**: execute the `upgrade.command`
field verbatim under sudo. Do not reconstruct the URL or tag yourself —
the `installer_url` is canonical and `target_tag` already accounts for
the server's `v`-prefix convention. The `upgrade` block is omitted when
`needs_upgrade=false` or when the remote version is unknown
(`status: "stale"`).

## Error contract

A failed call returns:
```json
{ "status": "failed", "error": "BINARY_MISSING: ..." }
```

The error string starts with one of these stable codes:

| Code                             | Meaning                                                       |
| -------------------------------- | ------------------------------------------------------------- |
| `UNSUPPORTED_PLATFORM`           | OS/arch not in the manifest                                   |
| `BINARY_MISSING`                 | `feivpn`, `feiapi`, or `feivpn-router` not found in any locator path |
| `BINARY_CHECKSUM_MISMATCH`       | on-disk SHA256 differs from the manifest entry                |
| `CONFIG_INCOMPLETE`              | no subscription URL in flag, profile, or store                |
| `SUBSCRIPTION_FETCH_FAILED`      | upstream `feiapi getconfig` failed                            |
| `CHECK_FAILED`                   | `feivpn --check` rejected the rendered config                 |
| `ROUTER_INSTALL_FAILED`          | could not write the router systemd unit / LaunchDaemon plist  |
| `ROUTER_START_FAILED`            | systemctl / launchctl refused to start the router unit        |
| `SERVICE_INSTALL_FAILED`         | systemd / launchd refused the daemon unit                     |
| `SERVICE_START_FAILED`           | systemctl / launchctl failed to start the daemon unit         |
| `HEALTH_TIMEOUT`                 | daemon started but never reported full health                 |
| `RECOVER_FAILED`                 | `feivpn --recover` exited non-zero                            |
| `INVALID_ARGUMENT`               | missing required flag or empty password                       |
| `LOGIN_FAILED` / `REGISTER_FAILED` | backend rejected credentials                                |
| `GETID_FAILED`                   | `/getid` rejected the device (network or signing problem)     |
| `NOT_LOGGED_IN`                  | command requires `auth_data` but the device is anonymous      |
| `CHANGE_PASSWORD_FAILED`         | backend rejected the new password                             |
| `ACCOUNT_PERSIST_FAILED`         | could not write to `$FEIVPN_ACCOUNT_FILE`                     |
| `DEVICE_ID_UNAVAILABLE`          | `/etc/machine-id` (Linux) / `IOPlatformUUID` (macOS) unreadable |
| `PASSWORD_FILE_UNREADABLE`       | `--password-file` path is missing or unreadable               |
| `APPCONFIG_FAILED`               | `feiapi appconfig` failed                                     |
| `API_UNREACHABLE`                | all backend domains unreachable                               |
| `API_AUTH_FAILED`                | API signature / credentials rejected                          |

## Recommended agent flow

```pseudo
# Optional: explicit anonymous bootstrap (otherwise getid is auto-run on
# the first command that needs an account)
sh("sudo feivpnctl getid --json")

# First time on a fresh host (named account)
sh("sudo feivpnctl register --email $EMAIL --password-file $PWFILE --json")

# Or, if the account already exists
sh("sudo feivpnctl login    --email $EMAIL --password-file $PWFILE --json")

# Bring up the daemon (no flag needed; subscribe_url comes from the store)
sh("sudo feivpnctl ensure-ready --json")

# Subsequent runs
sh("sudo feivpnctl status --json")
case status.running:
  false: sh("sudo feivpnctl ensure-ready --json")

# Confirm everything works
result = sh("sudo feivpnctl test --json")
say "VPN egress: " + result.egress_ip

# Tear down
sh("sudo feivpnctl disconnect --json")

# Drop named-account session (file stays, becomes anonymous)
sh("sudo feivpnctl logout --json")
```

## Where to learn more

- `internal/action/types.go` — exact Go structs printed to stdout
- `internal/store/store.go` — `Account` struct and on-disk file layout
- `internal/device/device.go` — how the persistent device id is read
- `internal/config/config.go` — `Profile` struct (the `/etc/feivpn/feivpnctl.json` shape)
- `internal/router/router.go` — router socket addresses + binary lookup
- `internal/platform/{linux_systemd,darwin_launchd}.go` — unit / plist templates for both services
- `manifest/binaries.manifest.json` — pinned upstream daemon + router + API binaries
- `docs/architecture.md` — the 3-layer design (skill / daemon / host adapter)
