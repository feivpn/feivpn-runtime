# Contributing

Thanks for your interest! A few ground rules.

## Scope

This repo deliberately stays small. Anything that requires touching
TUN devices, packet forwarding, DNS hijacking, or the FeiVPN backend
API belongs in `feivpn/feivpn-apps`, not here. We import the result of
those projects via pinned binaries in `bin/`.

If you find yourself wanting to vendor a kernel-networking library or
parse a Shadowsocks frame, you're in the wrong repo.

## Workflow

1. Fork + branch from `main`.
2. `make build && make test && make verify-bins` must all pass locally.
3. Open a PR. CI runs `go vet`, `go test ./...`, and `make verify-bins`
   on linux-amd64, linux-arm64, darwin-arm64, and darwin-amd64.
4. Squash-merge. We sign commits with `git commit -s` (DCO).

## Bumping pinned binaries

Always go through the official upstream Release first
(`feivpn/feivpn-apps` → `daemon-v*` or `api-v*` tag). Then in this
repo:

```bash
$EDITOR manifest/binaries.manifest.json   # bump tag + SHA256
make sync-bins
make verify-bins
git add manifest/ bin/
git commit -s -m "bin: bump feivpn to vX.Y.Z"
```

The `manifest/` change and the `bin/` change MUST land in the same
commit so CI's drift check is meaningful.

## Adding a new platform

`linux/amd64`, `linux/arm64`, `darwin/arm64` are first-class. Adding
e.g. `linux/riscv64` requires:

1. Cross-compile feivpn + feiapi for the new triple in
   `feivpn/feivpn-apps/.goreleaser.{daemon,feiapi}.yaml`.
2. Add the triple to `manifest/binaries.manifest.json`.
3. Drop the new `bin/feivpn-linux-riscv64` and `bin/feiapi-linux-riscv64`
   into git via `make sync-bins`.
4. Add the triple to the matrix in `.goreleaser.yml` here.

`internal/platform/` does not need changes — both Linux and macOS
adapters are arch-agnostic.

## Coding style

- Plain `gofmt`. No external linter beyond `go vet`.
- Comments explain *intent*, not *what the code does*. If a comment
  starts with "Now we...", delete it.
- Prefer returning structured errors with the canonical codes from
  `SKILL.md` (`UNSUPPORTED_PLATFORM`, `BINARY_MISSING`, ...). The agent
  consuming `feivpnctl` output relies on those prefixes.
