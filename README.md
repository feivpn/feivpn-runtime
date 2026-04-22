# feivpn-runtime

> **Bootstrap CLI + Cursor/Claude skill for the FeiVPN daemon on Linux & macOS.**
> Turns the agent's *current* machine into a fully-running FeiVPN endpoint
> in a single `feivpnctl ensure-ready` call.

[![License](https://img.shields.io/badge/license-Apache_2.0-blue)](LICENSE)
![status](https://img.shields.io/badge/status-MVP-yellow)
![platforms](https://img.shields.io/badge/platforms-linux%2Famd64%20%7C%20linux%2Farm64%20%7C%20darwin%2Farm64-informational)

## What it is

`feivpn-runtime` is the *agent-friendly* counterpart to the FeiVPN
daemon (which lives in [`feivpn/feivpn-apps`](https://github.com/feivpn/feivpn-apps)).
It does three things:

1. Ships pre-built `feivpn` (the user-level Go daemon), `feivpn-router`
   (the privileged C++ route/DNS controller), and `feiapi` (the API client)
   binaries, pinned by SHA256 in [`manifest/binaries.manifest.json`](manifest/binaries.manifest.json).
2. Provides a small Go CLI — **`feivpnctl`** — that orchestrates them
   via the local service manager (`systemd` on Linux, `launchd` on macOS).
3. Exposes the same orchestration as a [Cursor / Claude skill](SKILL.md),
   so an AI agent can bring up a VPN on the host it's running on with a
   single tool call.

## Architecture (3 layers, 3 binaries)

```
┌──────────────────────────────────────────────────────────────────┐
│  Layer 1 — Skill / CLI    feivpnctl  (this repo)                 │
│  ensure-ready · status · stop · restart · upgrade · check-upgrade│
└──────────────────────────────┬───────────────────────────────────┘
                               │ exec & supervise (systemd / launchd)
   ┌───────────────────────────┼────────────────────────────┐
   ▼                           ▼                            ▼
┌────────────────────┐ ┌─────────────────────────┐ ┌──────────────────┐
│ Layer 2a — Daemon  │ │ Layer 2b — Router (root)│ │ feiapi (upstream)│
│ feivpn  (Go, user) │ │ feivpn-router (C++)     │ │ HTTP API client  │
│ TUN + tun2socks    │◀┤ route + DNS mutations   │ │ getid / login /  │
│ data plane         │ │ exposes RPC socket      │ │ getconfig / ...  │
└──────────┬─────────┘ └────────────┬────────────┘ └──────────────────┘
           │                        │
           └─────── RPC ────────────┘
              unix:/var/run/feivpn_controller   (Linux)
              tcp:127.0.0.1:38964               (macOS)

┌──────────────────────────────────────────────────────────────────┐
│ Layer 3 — Host adapter   internal/platform/                      │
│ systemd units (Linux) · LaunchDaemon plists (macOS)              │
│ feivpn-router.service starts BEFORE feivpn.service and STOPS     │
│ AFTER it, on every code path (manual + reboot replay).           │
└──────────────────────────────────────────────────────────────────┘
```

**Why two daemons?** Route table mutation and DNS rewrite require root,
but the tun2socks data plane should not run as root. The upstream
project already split the responsibilities into a privileged C++
controller (`feivpn-router`) and a user-level Go daemon (`feivpn`)
that talk over a local socket — this repo just wires both into the
host's service manager and ships the bytes so a fresh box only needs
one `curl | bash` to be fully self-contained.

## Quick start

```bash
# 1. one-shot install (downloads + verifies + lays down /opt/feivpn/)
curl -fsSL https://raw.githubusercontent.com/feivpn/feivpn-runtime/main/scripts/install.sh \
  | sudo bash

# 2a. either pre-fill /etc/feivpn/feivpnctl.json with a subscription URL
sudo cp /opt/feivpn/templates/config/feivpnctl.example.json /etc/feivpn/feivpnctl.json
sudo $EDITOR /etc/feivpn/feivpnctl.json

# 2b. ...or log in / register from the host (recommended for ops humans)
sudo feivpnctl register --email me@example.com         # prompts for password
# or, scriptable:
echo "$PW" > /tmp/pw && sudo feivpnctl login --email me@example.com --password-file /tmp/pw

# 3. light it up (ensure-ready picks up the subscribe_url from the store)
sudo feivpnctl ensure-ready --json
# {"status":"ready","platform":"linux-amd64","version":"1.0.0","pid":4831,"tun":"fei0","checks":{...}}

# 4. inspect / verify
sudo feivpnctl status
sudo feivpnctl test --json   # egress IP, DNS, TUN, reachability

# 5. account / billing housekeeping
sudo feivpnctl whoami        # refreshes expiry from the server
sudo feivpnctl plans
sudo feivpnctl recharge      # opens the web payment page

# 6. tear down + restore network
sudo feivpnctl disconnect    # alias for `stop`
```

## Repository layout

## Command surface (`feivpnctl --help`)

```text
Account
  getid, register, login, logout, whoami, change-password
Billing
  plans, recharge
Connection
  ensure-ready, connect (alias), disconnect (alias), stop, restart,
  upgrade, check-upgrade, status
Diagnostics
  test
```

All commands accept `--json` and emit a single JSON document on stdout
(human summary on stderr). See [SKILL.md](SKILL.md) for the agent
contract and [`internal/action/types.go`](internal/action/types.go) for
exact response shapes.

### Local store (single file)

`feivpnctl` keeps **one** file outside the daemon's own `state.json`:

| File                                                            | Purpose                                                                                         | Mode |
| --------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- | ---- |
| `$FEIVPN_ACCOUNT_FILE` (default `/var/lib/feivpn/account.json`) | mirror of the server's UserData (uuid, token, auth_data, subscribe_url, expired_at, user_email) | 0600 |

The file is created on the first identity call (`getid` / `login` /
`register`) and is **never deleted by the CLI**. `logout` re-runs
`getid` and overwrites the named-account fields with the anonymous
baseline; the device-bound `uuid` and a fresh `subscribe_url` remain.

The persistent **device identity** is read from the OS on every call
(no caching by us): `/etc/machine-id` on Linux, `IOPlatformUUID` on
macOS. Missing → `DEVICE_ID_UNAVAILABLE` (the CLI never substitutes a
self-generated random UUID).

`auth_data` non-empty ⇒ logged in. After `login` / `register`,
`ensure-ready` is zero-arg: it pulls the latest `subscribe_url` from
the server, falls back to the cached one, and feeds the result to the
daemon. No subscription / node-list cache is persisted — every
`ensure-ready` re-fetches the latest node list.

## Repository layout

```
feivpn-runtime/
├── cmd/feivpnctl/                 main.go (cobra) — 15 subcommands + 2 aliases
├── internal/
│   ├── action/                    ensure_ready, status, stop, restart, upgrade,
│   │                              check_upgrade, getid, login, register, logout, whoami,
│   │                              change_password, plans, recharge, test
│   ├── binmgr/                    locator + SHA verifier + spawn helpers
│   ├── config/                    feivpnctl profile schema
│   ├── daemon/                    thin wrapper around `feivpn`
│   ├── router/                    thin wrapper around `feivpn-router` (root C++ controller)
│   ├── device/                    OS-issued device id (machine-id / IOPlatformUUID)
│   ├── feiapi/                    thin wrapper around `feiapi`
│   ├── host/                      OS / arch detection + skill-upgrade tag mapping
│   ├── platform/                  systemd (linux) + launchd (darwin) adapters
│   │                              — installs both feivpn-router AND feivpn units
│   ├── state/                     mirrors daemon's state.json
│   ├── store/                     single-file account store (account.json)
│   └── logging/                   slog wrapper
├── bin/                           PINNED BINARIES (committed; see bin/README.md)
├── manifest/                      binaries.manifest.json (SHA256 source of truth)
├── templates/                     reference systemd/launchd templates + example profile
├── examples/                      agent request/response samples
├── scripts/
│   ├── install.sh                 curl|bash one-shot installer
│   ├── sync-bins.sh               maintainer: pull bin/* from upstream Releases
│   └── verify-bins.sh             CI: re-hash bin/* against manifest
├── docs/                          architecture · runbook · contributing
├── .github/workflows/             CI + release pipeline
├── .goreleaser.yml
├── Makefile
├── SKILL.md                       agent contract (Cursor / Claude)
└── README.md
```

## Development

```bash
make build         # compile feivpnctl for the current host
make test          # go test ./...
make verify-bins   # re-hash bin/* against manifest/
make tarball       # produce dist/feivpn-runtime-<os>-<arch>.tar.gz
```

To bump the pinned daemon / API binaries:

1. Edit `manifest/binaries.manifest.json` with the new tag + SHA256.
2. `make sync-bins` (downloads + verifies into `bin/`).
3. Commit `manifest/` and `bin/` together.

## License

Apache-2.0 © 2026 The FeiVPN Authors. See [LICENSE](LICENSE).
