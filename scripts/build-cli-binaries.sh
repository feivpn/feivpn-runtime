#!/usr/bin/env bash
# Build the feivpnctl CLI for every platform feivpn-runtime targets, all
# from a single host. The output mirrors what GoReleaser produces during
# a tagged release, but without needing a tag, the GitHub Actions runner,
# or the bundling step (use `goreleaser release --snapshot --clean` if
# you want full tarballs locally).
#
# feivpnctl is pure Go with CGO_ENABLED=0, so cross-compiling all four
# targets works on any host (no Docker, no toolchain dance, no SDK
# tricks). This script exists to keep a one-command parity with
# feivpn-apps/scripts/build-{router,go}-binaries.sh:
#
#   build-router-binaries.sh   → C++ feivpn-router  (root proxy controller)
#   build-go-binaries.sh       → feivpn + feiapi    (daemon + API CLI)
#   build-cli-binaries.sh      → feivpnctl          (this script)
#
# Output layout:
#
#   dist/feivpnctl/feivpnctl-linux-amd64
#   dist/feivpnctl/feivpnctl-linux-arm64
#   dist/feivpnctl/feivpnctl-darwin-amd64
#   dist/feivpnctl/feivpnctl-darwin-arm64
#
# Each build prints size + sha256 and (optionally) copies into a staging
# directory you point at via --stage.
#
# Usage:
#   ./scripts/build-cli-binaries.sh                          # all 4 targets
#   ./scripts/build-cli-binaries.sh --targets darwin-arm64   # one target
#   ./scripts/build-cli-binaries.sh --version 0.2.0          # set -X main.version
#   ./scripts/build-cli-binaries.sh --stage /tmp/install-pkg # copy after build

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$REPO_ROOT/dist/feivpnctl"
PKG="./cmd/feivpnctl"
LDFLAG_KEY="main.version"

ALL_TARGETS=("linux-amd64" "linux-arm64" "darwin-amd64" "darwin-arm64")
TARGETS=("${ALL_TARGETS[@]}")
STAGE=""
VERSION=""

# ---------- arg parsing ----------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --targets)
      IFS=',' read -r -a TARGETS <<< "$2"
      shift 2
      ;;
    --stage)
      STAGE="$2"
      shift 2
      ;;
    --version)
      VERSION="$2"
      shift 2
      ;;
    -h|--help)
      sed -n '/^#!/,/^set -euo/p' "$0" | sed '$d'
      exit 0
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 1
      ;;
  esac
done

# Default version: `git describe --tags --always --dirty`, fall back to
# 0.0.0-dev if not in a git checkout. Matches the Makefile.
if [[ -z "$VERSION" ]]; then
  if VERSION="$(git -C "$REPO_ROOT" describe --tags --always --dirty 2>/dev/null)"; then
    :
  else
    VERSION="0.0.0-dev"
  fi
fi

mkdir -p "$OUT_DIR"

declare -a BUILT=()
declare -a FAILED=()

# ---------- helpers ----------
log()  { printf '\n\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mWARN:\033[0m %s\n' "$*" >&2; }
ok()   { printf '\033[1;32mOK:\033[0m %s\n' "$*"; }
err()  { printf '\033[1;31mERR:\033[0m %s\n' "$*" >&2; }

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

human_size() {
  if [[ "$(uname)" == "Darwin" ]]; then
    stat -f '%z' "$1" | awk '{printf "%.1f MB", $1/1024/1024}'
  else
    stat -c '%s' "$1" | awk '{printf "%.1f MB", $1/1024/1024}'
  fi
}

# ---------- build ----------
build_one() {
  local target="$1"
  local goos="${target%-*}"
  local goarch="${target#*-}"
  local out="$OUT_DIR/feivpnctl-$target"

  log "feivpnctl → $target  (version=$VERSION)"
  if (
    cd "$REPO_ROOT"
    GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
      go build -trimpath \
        -ldflags "-s -w -X $LDFLAG_KEY=$VERSION" \
        -o "$out" \
        "$PKG"
  ); then
    chmod +x "$out"
    BUILT+=("$target|$out")
    ok "$target → $out"
  else
    FAILED+=("$target")
    err "$target build failed"
  fi
}

for tgt in "${TARGETS[@]}"; do
  case "$tgt" in
    linux-amd64|linux-arm64|darwin-amd64|darwin-arm64) ;;
    *) FAILED+=("$tgt (unknown target)"); continue ;;
  esac
  build_one "$tgt"
done

# ---------- summary ----------
log "build summary"

if [[ ${#BUILT[@]} -gt 0 ]]; then
  printf '\n%-22s %-10s %s\n' "binary" "size" "sha256"
  printf '%-22s %-10s %s\n' "------" "----" "------"
  for entry in "${BUILT[@]}"; do
    target="${entry%%|*}"
    path="${entry#*|}"
    name="$(basename "$path")"
    sz="$(human_size "$path")"
    sha="$(sha256_of "$path")"
    printf '%-22s %-10s %s\n' "$name" "$sz" "$sha"
  done
fi

if [[ ${#FAILED[@]} -gt 0 ]]; then
  printf '\n\033[1;31mfailed:\033[0m\n'
  for f in "${FAILED[@]}"; do printf '  - %s\n' "$f"; done
fi

# ---------- optional staging copy ----------
if [[ -n "$STAGE" && ${#BUILT[@]} -gt 0 ]]; then
  if [[ ! -d "$STAGE" ]]; then
    err "stage dir does not exist: $STAGE"
    exit 1
  fi
  log "copying into $STAGE"
  for entry in "${BUILT[@]}"; do
    path="${entry#*|}"
    cp "$path" "$STAGE/"
    ok "→ $STAGE/$(basename "$path")"
  done
fi

# ---------- exit ----------
if [[ ${#FAILED[@]} -gt 0 ]]; then
  exit 1
fi

log "done. binaries in $OUT_DIR"
