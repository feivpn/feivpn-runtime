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

1. Ships pre-built `feivpn` (the daemon) and `feiapi` (the API client)
   binaries, pinned by SHA256 in [`manifest/binaries.manifest.json`](manifest/binaries.manifest.json).
2. Provides a small Go CLI — **`feivpnctl`** — that orchestrates them
   via the local service manager (`systemd` on Linux, `launchd` on macOS).
3. Exposes the same orchestration as a [Cursor / Claude skill](SKILL.md),
   so an AI agent can bring up a VPN on the host it's running on with a
   single tool call.

## Architecture (3 layers)

```
┌─────────────────────────────────────────────────────────────┐
│  Layer 1 — Skill / CLI    feivpnctl  (this repo)            │
│  ensure-ready · status · stop · restart · upgrade           │
└──────────────────────────────┬──────────────────────────────┘
                               │ exec
       ┌───────────────────────┴───────────────────────┐
       ▼                                               ▼
┌────────────────────┐                       ┌──────────────────┐
│ Layer 2 — Daemon   │                       │ feiapi  (upstream)│
│  feivpn (upstream) │  TUN / route / DNS    │  HTTP API client  │
└──────────┬─────────┘                       └──────────────────┘
           │ uses
           ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 3 — Host adapter   internal/platform/                │
│  systemd unit (Linux) · LaunchDaemon plist (macOS)          │
└─────────────────────────────────────────────────────────────┘
```

## Quick start

```bash
# 1. one-shot install (downloads + verifies + lays down /opt/feivpn/)
curl -fsSL https://raw.githubusercontent.com/feivpn/feivpn-runtime/main/scripts/install.sh \
  | sudo bash

# 2. drop in a profile
sudo cp /opt/feivpn/templates/config/feivpnctl.example.json /etc/feivpn/feivpnctl.json
sudo $EDITOR /etc/feivpn/feivpnctl.json   # set subscription_token

# 3. light it up
sudo feivpnctl ensure-ready --json
# {"status":"ready","platform":"linux-amd64","version":"1.0.0","pid":4831,"tun":"fei0","checks":{...}}

# 4. inspect
sudo feivpnctl status

# 5. tear down + restore network
sudo feivpnctl stop
```

## Repository layout

```
feivpn-runtime/
├── cmd/feivpnctl/                 main.go (cobra) — five subcommands
├── internal/
│   ├── action/                    EnsureReady · Status · Stop · Restart · Upgrade
│   ├── binmgr/                    locator + SHA verifier + spawn helpers
│   ├── config/                    feivpnctl profile schema
│   ├── daemon/                    thin wrapper around `feivpn`
│   ├── feiapi/                    thin wrapper around `feiapi`
│   ├── platform/                  systemd (linux) + launchd (darwin) adapters
│   ├── state/                     mirrors daemon's state.json
│   └── logging/                   slog wrapper
├── bin/                           PINNED BINARIES (committed; see bin/README.md)
├── manifest/                      binaries.manifest.json (SHA256 source of truth)
├── schema/                        JSON Schema for stdout / config / state
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
