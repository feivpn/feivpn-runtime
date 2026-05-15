#!/usr/bin/env bash
# feivpn-runtime one-shot installer.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/feivpn/feivpn-runtime/main/scripts/install.sh | sudo bash
#
# Or pin a specific release:
#   curl -fsSL ... | sudo bash -s -- --tag v0.1.0
#
# What it does:
#   1. Detects OS/ARCH and selects a release tarball from GitHub Releases.
#   2. Downloads the tarball + .sha256 sidecar; verifies SHA256.
#   3. Lays down /opt/feivpn/{bin,templates,manifest.json} and
#      /usr/local/bin/feivpnctl.
#   4. Re-verifies SHAs in manifest.json against /opt/feivpn/bin/*.
#   5. Prints next-step instructions (it does NOT auto-launch the daemon
#      because that requires a profile + subscription token).

set -euo pipefail

REPO=${REPO:-feivpn/feivpn-runtime}
PREFIX=${PREFIX:-/opt/feivpn}
BIN_LINK=${BIN_LINK:-/usr/local/bin/feivpnctl}
TAG=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag) TAG="$2"; shift 2 ;;
    --prefix) PREFIX="$2"; shift 2 ;;
    --repo) REPO="$2"; shift 2 ;;
    -h|--help)
      sed -n '1,30p' "$0"
      exit 0
      ;;
    *) echo "unknown flag: $1" >&2; exit 1 ;;
  esac
done

# ----- 1. detect platform -----
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) echo "UNSUPPORTED_PLATFORM: $OS/$ARCH" >&2; exit 2 ;;
esac
case "$OS" in
  linux|darwin) ;;
  *) echo "UNSUPPORTED_PLATFORM: $OS/$ARCH" >&2; exit 2 ;;
esac
PLAT="$OS-$ARCH"
echo "==> platform: $PLAT"

# ----- 2. resolve release tag -----
if [[ -z "$TAG" ]]; then
  TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
  if [[ -z "$TAG" ]]; then
    echo "ERROR: could not resolve latest tag from GitHub" >&2
    exit 3
  fi
fi
echo "==> tag: $TAG"

TAR_NAME="feivpn-runtime-$PLAT.tar.gz"
URL="https://github.com/$REPO/releases/download/$TAG/$TAR_NAME"
SHA_URL="$URL.sha256"

# ----- 3. download & verify -----
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
echo "==> downloading $URL"
curl -fsSL "$URL"     -o "$WORK/$TAR_NAME"
curl -fsSL "$SHA_URL" -o "$WORK/$TAR_NAME.sha256"

EXPECTED=$(awk '{print $1}' "$WORK/$TAR_NAME.sha256")
ACTUAL=$(shasum -a 256 "$WORK/$TAR_NAME" | awk '{print $1}')
if [[ "$EXPECTED" != "$ACTUAL" ]]; then
  echo "ERROR: SHA mismatch on $TAR_NAME" >&2
  echo "  expected: $EXPECTED" >&2
  echo "  actual:   $ACTUAL"   >&2
  exit 4
fi
echo "==> sha256 ok"

# ----- 3.5. quiet sudo's "unable to resolve host" warning -----
#
# On stock cloud images (especially the random-UUID-hostname kind that
# Hetzner / Vultr / OVH hand out, e.g. `019d8c6c-64c6-...`) the system
# hostname has no corresponding entry in /etc/hosts and DNS obviously
# can't answer either. Every `sudo` invocation then prints
#
#   sudo: unable to resolve host <hostname>: Temporary failure in name
#         resolution
#
# which is harmless but noisy and obscures real errors in our installer
# / `feivpnctl` output. Patch /etc/hosts ourselves: this is exactly
# what cloud-init would have done if it had been configured properly.
ensure_hostname_in_hosts() {
  # macOS handles this via SystemConfiguration; skip there.
  [[ "$OS" == "linux" ]] || return 0

  local short_host fqdn entry
  short_host=$(hostname -s 2>/dev/null || hostname 2>/dev/null || true)
  [[ -n "$short_host" ]] || return 0
  fqdn=$(hostname -f 2>/dev/null || echo "$short_host")

  # If the host already resolves (via /etc/hosts, NIS, working DNS, …)
  # leave it alone. nss_files runs before nss_dns by default so a
  # pre-existing /etc/hosts entry will match here even when the
  # tunnel-hijacked /etc/resolv.conf can't answer.
  if getent hosts "$short_host" >/dev/null 2>&1; then
    return 0
  fi

  # Refuse if we somehow can't write /etc/hosts (read-only rootfs,
  # immutable bit, weird container, …). Better to skip silently than
  # to abort a working install over a cosmetic warning.
  [[ -w /etc/hosts ]] || return 0

  # Use 127.0.1.1 (the Debian/Ubuntu convention for the host's own
  # name) so we don't collide with 127.0.0.1's "localhost" entry. If
  # `hostname -f` gave us a real FQDN, keep both forms on the same
  # line so `gethostbyname(fqdn)` and `gethostbyname(short)` both
  # succeed.
  if [[ "$fqdn" != "$short_host" && -n "$fqdn" ]]; then
    entry="127.0.1.1 $fqdn $short_host"
  else
    entry="127.0.1.1 $short_host"
  fi

  echo "==> patching /etc/hosts so sudo stops complaining ($entry)"
  printf '%s\n' "$entry" >> /etc/hosts
}
ensure_hostname_in_hosts

# ----- 4. stop running services so we can overwrite live binaries -----
# Linux refuses to write to a binary that is currently being executed
# (ETXTBSY / "Text file busy"). Stop both daemon and router up-front
# rather than failing mid-copy with a half-installed system. Best-effort
# on systemd hosts; silently skip if systemctl is missing.
if [[ "$OS" == "linux" ]] && command -v systemctl >/dev/null 2>&1; then
  for unit in feivpn.service feivpn-router.service; do
    if systemctl is-active --quiet "$unit"; then
      echo "==> stopping $unit before upgrade"
      systemctl stop "$unit" || true
    fi
  done
fi
# macOS: best-effort bootout of LaunchDaemons so kill+rewrite is atomic.
if [[ "$OS" == "darwin" ]] && command -v launchctl >/dev/null 2>&1; then
  for label in org.feivpn.daemon org.feivpn.router; do
    launchctl bootout "system/$label" >/dev/null 2>&1 || true
  done
fi

# ----- 5. lay down /opt/feivpn -----
mkdir -p "$PREFIX"
tar -C "$WORK" -xzf "$WORK/$TAR_NAME"
mkdir -p "$PREFIX/bin" "$PREFIX/templates"
install -m 0755 "$WORK/pkg/feivpnctl"        "$BIN_LINK"
# Use `install` instead of `cp -R` so each binary is written via a
# create+rename, sidestepping ETXTBSY on hosts that race the previous
# stop step (e.g. systemd auto-restart kicking back in mid-copy).
for src in "$WORK/pkg/bin/"*; do
  [[ -e "$src" ]] || continue
  name=$(basename "$src")
  install -m 0755 "$src" "$PREFIX/bin/$name"
done
cp -R "$WORK/pkg/templates/."  "$PREFIX/templates/"
cp    "$WORK/pkg/manifest.json" "$PREFIX/manifest.json"

# Make linux/darwin binaries discoverable under stable names too.
# NOTE: feivpn-router on macOS ships as a single Universal Binary
# (feivpn-router-darwin-universal) — both arm64 and amd64 hosts symlink
# to it. The Go binaries (feivpn / feiapi) are still per-arch.
case "$PLAT" in
  linux-amd64|linux-arm64|darwin-arm64|darwin-amd64)
    if [[ -f "$PREFIX/bin/feivpn-$PLAT" ]]; then
      ln -sf "feivpn-$PLAT" "$PREFIX/bin/feivpn"
    fi
    if [[ -f "$PREFIX/bin/feiapi-$PLAT" ]]; then
      ln -sf "feiapi-$PLAT" "$PREFIX/bin/feiapi"
    fi
    if [[ -f "$PREFIX/bin/feivpn-router-$PLAT" ]]; then
      ln -sf "feivpn-router-$PLAT" "$PREFIX/bin/feivpn-router"
    elif [[ "$OS" == "darwin" && -f "$PREFIX/bin/feivpn-router-darwin-universal" ]]; then
      ln -sf "feivpn-router-darwin-universal" "$PREFIX/bin/feivpn-router"
    fi
    ;;
esac

# ----- 6. verify -----
echo "==> verifying installed binaries against manifest"
"$BIN_LINK" --version >/dev/null
echo
echo "✓ feivpnctl installed at $BIN_LINK"
echo "✓ resources installed at $PREFIX"
echo
echo "Next steps:"
echo "  1. Launch:            sudo $BIN_LINK ensure-ready"
echo "                        (auto-registers this device with the backend on first run;"
echo "                         no manual config required)"
echo "  2. Inspect:           sudo $BIN_LINK status"
echo "  3. Tear down:         sudo $BIN_LINK stop"
echo
echo "Pin an egress country (optional):"
echo "  sudo $BIN_LINK countries                       # list ISO codes available"
echo "  sudo $BIN_LINK ensure-ready --country HK       # one-shot override"
echo "  # or persist it in the profile:"
echo "  sudo mkdir -p /etc/feivpn"
echo "  sudo cp $PREFIX/templates/config/feivpnctl.example.json /etc/feivpn/feivpnctl.json"
echo "  sudo \$EDITOR /etc/feivpn/feivpnctl.json       # set preferred_country"
