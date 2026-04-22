#!/usr/bin/env bash
# Maintainer command: pulls feivpn + feiapi binaries from the upstream
# vilizhe/feivpn-apps GitHub Releases according to manifest/binaries.manifest.json
# and lays them out under bin/. Verifies SHA256 along the way.
#
# Usage:
#   ./scripts/sync-bins.sh           # respect current manifest
#   FORCE=1 ./scripts/sync-bins.sh   # re-download even if local file matches

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MANIFEST="$ROOT/manifest/binaries.manifest.json"
BIN_DIR="$ROOT/bin"

if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq is required (brew install jq / apt install jq)" >&2
  exit 1
fi

mkdir -p "$BIN_DIR"

download_one() {
  local component="$1"   # feivpn | feiapi
  local platform="$2"    # linux-amd64, linux-arm64, darwin-arm64
  local path sha url
  path=$(jq -r ".\"$component\".binaries.\"$platform\".path" "$MANIFEST")
  sha=$(jq -r ".\"$component\".binaries.\"$platform\".sha256" "$MANIFEST")
  url=$(jq -r ".\"$component\".binaries.\"$platform\".url" "$MANIFEST")

  if [[ "$path" == "null" || "$sha" == "null" || "$url" == "null" ]]; then
    echo "skip: $component/$platform not in manifest"
    return
  fi
  if [[ "$sha" == "0000000000000000000000000000000000000000000000000000000000000000" ]]; then
    echo "skip: $component/$platform manifest entry is a placeholder; bump it first"
    return
  fi

  local local_path="$ROOT/$path"
  if [[ -z "${FORCE:-}" && -f "$local_path" ]]; then
    local actual
    actual=$(shasum -a 256 "$local_path" | awk '{print $1}')
    if [[ "$actual" == "$sha" ]]; then
      echo "ok:  $component/$platform already up to date ($sha)"
      return
    fi
  fi

  echo "fetching: $url"
  local tmp; tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' RETURN
  curl -fsSL "$url" -o "$tmp/dl.tar.gz"
  tar -xzf "$tmp/dl.tar.gz" -C "$tmp"
  local extracted
  extracted=$(find "$tmp" -type f \( -name "$component" -o -name "$component-*" \) | head -n1)
  if [[ -z "$extracted" ]]; then
    echo "ERROR: could not locate $component binary inside $url" >&2
    exit 1
  fi
  install -m 0755 "$extracted" "$local_path"
  local actual
  actual=$(shasum -a 256 "$local_path" | awk '{print $1}')
  if [[ "$actual" != "$sha" ]]; then
    echo "ERROR: SHA mismatch for $component/$platform" >&2
    echo "  expected: $sha"  >&2
    echo "  actual:   $actual" >&2
    rm -f "$local_path"
    exit 2
  fi
  echo "ok:  $component/$platform → $local_path ($sha)"
}

for component in feivpn feiapi; do
  for platform in linux-amd64 linux-arm64 darwin-arm64; do
    download_one "$component" "$platform"
  done
done

echo
echo "Done. Run 'make verify-bins' to double-check."
