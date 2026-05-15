# Runbook

A grab-bag of operational recipes for `feivpn-runtime` in production.

## Bootstrap a fresh host

```bash
# 1. install
curl -fsSL https://raw.githubusercontent.com/feivpn/feivpn-runtime/main/scripts/install.sh \
  | sudo bash

# 2. (optional) profile — only needed to override defaults like preferred_country
sudo install -d -m 0755 /etc/feivpn
sudo cp /opt/feivpn/templates/config/feivpnctl.example.json \
        /etc/feivpn/feivpnctl.json
sudo $EDITOR /etc/feivpn/feivpnctl.json

# 3. launch (auto-registers the device on first run; no token required)
sudo feivpnctl ensure-ready --json | tee /tmp/ensure.json
jq -r .status /tmp/ensure.json    # → "ready"
```

## Diagnose a `degraded` state

```bash
sudo feivpnctl status --json | jq .
# Look at .health.errors[] for the daemon's own diagnosis.
# Look at .health.connectivity for egress probe results.
# Look at .state.original_route to confirm we have a path back.
```

Common causes:

| Symptom                         | Likely cause                                       | Fix                                            |
| ------------------------------- | -------------------------------------------------- | ---------------------------------------------- |
| `tun: false`                    | Missing `CAP_NET_ADMIN` (Linux) / SIP block (mac)  | Run as root; on macOS approve the TUN driver   |
| `route: false`                  | `ip route replace default` denied                  | Check `dmesg`, ensure no other VPN is running  |
| `dns: false`                    | `resolvconf`/`scutil` failure                      | Inspect `/etc/resolv.conf` / `scutil --dns`    |
| `connectivity.reach: false`     | Subscription node unreachable                      | Run `feivpnctl countries` then retry with `--country <CC>` |

## Roll back to the previous version

`feivpnctl upgrade` does NOT manage versions itself. To roll back:

```bash
# 1. re-run installer with an older tag
curl -fsSL .../install.sh | sudo bash -s -- --tag v0.0.5

# 2. restart daemon to pick up the rolled-back binaries
sudo feivpnctl restart --json
```

## Tear down completely

```bash
sudo feivpnctl stop
sudo systemctl disable feivpn 2>/dev/null || true   # linux
sudo launchctl bootout system/org.feivpn.daemon 2>/dev/null || true   # darwin
sudo rm -rf /opt/feivpn /etc/feivpn /var/lib/feivpn /var/log/feivpn
sudo rm -f  /usr/local/bin/feivpnctl
```

## Bump the pinned upstream binaries

Maintainer-only:

```bash
# 1. edit manifest with new tag + new SHA256s
$EDITOR manifest/binaries.manifest.json

# 2. download into bin/
make sync-bins

# 3. local sanity check
make verify-bins

# 4. commit (manifest + bin/ together)
git add manifest/ bin/
git commit -s -m "bin: bump feivpn to vX.Y.Z, feiapi to vA.B.C"
```

CI re-runs `make verify-bins` on every push; drift fails the build.
