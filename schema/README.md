# `schema/` — JSON Schema for feivpnctl I/O

These schemas mirror (or directly reference) the daemon-side contracts
maintained in `feivpn/feivpn-apps/client/protocol/`:

| File                          | Mirrors / depends on                                                                       |
| ----------------------------- | ------------------------------------------------------------------------------------------ |
| `feivpnctl-output.schema.json`| Single-line JSON documents that `feivpnctl <action>` prints to stdout                       |
| `feivpnctl-config.schema.json`| The user-facing profile under `/etc/feivpn/feivpnctl.json` (mirrors `internal/config/`)     |
| `daemon-state.schema.json`    | Verbatim copy of `client/protocol/ipc/daemon-state.schema.json` (vendored for offline use)  |
| `daemon-health.schema.json`   | Verbatim copy of `client/protocol/ipc/daemon-health.schema.json` (vendored for offline use) |

> Why vendor copies?  feivpn-runtime must be installable on a fresh
> machine that has no access to the upstream repo, so the schemas are
> shipped inside the release tarball and the ones bundled here are the
> authoritative copy at the pinned upstream revision.
>
> When you bump `manifest/binaries.manifest.json` you SHOULD also
> re-sync the `daemon-*.schema.json` files; CI fails the release if
> the schema versions in the manifest don't match the file headers.
