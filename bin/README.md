# `bin/` — Pinned upstream binaries

This directory is **checked in to git** and contains the binaries that
`feivpnctl` spawns at runtime:

| File                                    | Built from                                                          | Privilege   |
| --------------------------------------- | ------------------------------------------------------------------- | ----------- |
| `feivpn-linux-amd64`                    | `feivpn/feivpn-apps` → `client/go/outline/electron`                | user (CAP_NET_ADMIN) |
| `feivpn-linux-arm64`                    | `feivpn/feivpn-apps` → `client/go/outline/electron`                | user (CAP_NET_ADMIN) |
| `feivpn-darwin-arm64`                   | `feivpn/feivpn-apps` → `client/go/outline/electron`                | user        |
| `feivpn-darwin-amd64`                   | `feivpn/feivpn-apps` → `client/go/outline/electron`                | user        |
| `feiapi-linux-amd64`                    | `feivpn/feivpn-apps` → `client/go/api/cmd/feiapi`                  | user        |
| `feiapi-linux-arm64`                    | `feivpn/feivpn-apps` → `client/go/api/cmd/feiapi`                  | user        |
| `feiapi-darwin-arm64`                   | `feivpn/feivpn-apps` → `client/go/api/cmd/feiapi`                  | user        |
| `feiapi-darwin-amd64`                   | `feivpn/feivpn-apps` → `client/go/api/cmd/feiapi`                  | user        |
| `feivpn-router-linux-amd64`             | `feivpn/feivpn-apps` → `client/electron/linux_proxy_controller`    | **root**    |
| `feivpn-router-linux-arm64`             | `feivpn/feivpn-apps` → `client/electron/linux_proxy_controller`    | **root**    |
| `feivpn-router-darwin-universal`        | `feivpn/feivpn-apps` → `client/electron/macos_proxy_controller`    | **root**    |

The `feivpn-router-*` binaries are the C++ `FeiVPNProxyController` daemon
that owns route + DNS mutations. The `feivpn` Go daemon talks to it over
a local socket (Unix on Linux, TCP `127.0.0.1:38964` on macOS); see the
upstream READMEs in `client/electron/{linux,macos}_proxy_controller/`.

### macOS shipping model

Two flavors live side by side on darwin, on purpose:

- **C++ router** (`feivpn-router`): single **Universal Binary**
  (`lipo arm64 + x86_64`), exactly mirroring the upstream Electron
  client (`electron-builder.json` → `mac.target.arch: ["x64", "arm64"]`,
  `asarUnpack: ["…/macos_proxy_controller/dist"]`). The manifest declares
  **both** `darwin-arm64` and `darwin-amd64` keys pointing at
  `bin/feivpn-router-darwin-universal`; `install.sh` symlinks
  `bin/feivpn-router → feivpn-router-darwin-universal` on darwin.
- **Go binaries** (`feivpn`, `feiapi`): **per-arch** (`feivpn-darwin-arm64`
  + `feivpn-darwin-amd64`, same for `feiapi`). Cross-compiling Go is
  cheap enough that lipo'ing buys nothing here, and `install.sh`
  symlinks the right per-arch file to the stable `feivpn` / `feiapi`
  name based on `uname -m`.

Intel Mac (`darwin-amd64`) is fully supported end-to-end; the upstream
release matrix is wired in `feivpn-apps/.github/workflows/release-{daemon,feiapi}.yml`.

### Service ordering

Managed by `feivpnctl`: `feivpn-router.service` (root) starts **before**
`feivpn.service` (user-level) and is stopped **after** it.

## Why ship binaries in git?

- `feivpnctl` is a **bootstrap** tool — its job is to be the *first* thing a
  fresh machine downloads. Requiring it to fan out to multiple Releases
  pages makes the cold-start brittle.
- Pinning bytes in this repo makes every install reproducible from a
  single git revision and protects against silent upstream tag mutations.
- Source-of-truth for verification is `manifest/binaries.manifest.json`.
  CI fails the build if the on-disk SHA256 differs from the manifest.

## How to update

```sh
# 1. Edit manifest/binaries.manifest.json (bump tag + version + sha256)
# 2. Pull the new bytes into bin/
make sync-bins
# 3. Verify locally before committing
make verify-bins
# 4. Commit manifest + bin/ together in one PR
git add manifest/binaries.manifest.json bin/feivpn-* bin/feiapi-*
git commit -s -m "bin: bump feivpn to vX.Y.Z and feiapi to vA.B.C"
```

## Why aren't there Windows binaries?

The MVP scope (per `SKILL.md`) is `linux/amd64`, `linux/arm64`,
`darwin/arm64`, and `darwin/amd64`. Windows comes later once we have a
host adapter for TUN-Windows + WinTUN.
