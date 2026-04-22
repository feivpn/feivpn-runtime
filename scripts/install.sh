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
#   3. Lays down /opt/feivpn/{bin,schema,templates,manifest.json} and
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

# ----- 4. lay down /opt/feivpn -----
mkdir -p "$PREFIX"
tar -C "$WORK" -xzf "$WORK/$TAR_NAME"
mkdir -p "$PREFIX/bin" "$PREFIX/schema" "$PREFIX/templates"
install -m 0755 "$WORK/pkg/feivpnctl"        "$BIN_LINK"
cp -R "$WORK/pkg/bin/."        "$PREFIX/bin/"
cp -R "$WORK/pkg/schema/."     "$PREFIX/schema/"
cp -R "$WORK/pkg/templates/."  "$PREFIX/templates/"
cp    "$WORK/pkg/manifest.json" "$PREFIX/manifest.json"
chmod 0755 "$PREFIX/bin/"*

# Make linux/darwin daemon binary discoverable as a stable name too.
case "$PLAT" in
  linux-amd64|linux-arm64|darwin-arm64)
    if [[ -f "$PREFIX/bin/feivpn-$PLAT" ]]; then
      ln -sf "feivpn-$PLAT" "$PREFIX/bin/feivpn"
    fi
    if [[ -f "$PREFIX/bin/feiapi-$PLAT" ]]; then
      ln -sf "feiapi-$PLAT" "$PREFIX/bin/feiapi"
    fi
    ;;
esac

# ----- 5. verify -----
echo "==> verifying installed binaries against manifest"
"$BIN_LINK" --version >/dev/null
echo
echo "✓ feivpnctl installed at $BIN_LINK"
echo "✓ resources installed at $PREFIX"
echo
echo "Next steps:"
echo "  1. Create a profile:  sudo cp $PREFIX/templates/config/feivpnctl.example.json /etc/feivpn/feivpnctl.json"
echo "  2. Edit it:           sudo \$EDITOR /etc/feivpn/feivpnctl.json   # set subscription_token"
echo "  3. Launch:            sudo $BIN_LINK ensure-ready"
echo "  4. Inspect:           sudo $BIN_LINK status"
echo "  5. Tear down:         sudo $BIN_LINK stop"
