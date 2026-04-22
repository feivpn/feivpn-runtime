# `bin/` — Pinned upstream binaries

This directory is **checked in to git** and contains the binaries that
`feivpnctl` spawns at runtime:

| File                       | Built from                                                  |
| -------------------------- | ----------------------------------------------------------- |
| `feivpn-linux-amd64`       | `feivpn/feivpn-apps` → `client/go/outline/electron`        |
| `feivpn-linux-arm64`       | `feivpn/feivpn-apps` → `client/go/outline/electron`        |
| `feivpn-darwin-arm64`      | `feivpn/feivpn-apps` → `client/go/outline/electron`        |
| `feiapi-linux-amd64`       | `feivpn/feivpn-apps` → `client/go/api/cmd/feiapi`          |
| `feiapi-linux-arm64`       | `feivpn/feivpn-apps` → `client/go/api/cmd/feiapi`          |
| `feiapi-darwin-arm64`      | `feivpn/feivpn-apps` → `client/go/api/cmd/feiapi`          |

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

The MVP scope (per `SKILL.md`) is `linux/amd64`, `linux/arm64`, and
`darwin/arm64`. Windows comes later once we have a host adapter for
TUN-Windows + WinTUN.
