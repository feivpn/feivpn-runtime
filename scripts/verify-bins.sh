#!/usr/bin/env bash
# Re-hashes each entry in manifest/binaries.manifest.json against the actual
# bytes under bin/ and exits non-zero if any drift is detected. Run by CI
# on every push.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MANIFEST="$ROOT/manifest/binaries.manifest.json"

if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq is required" >&2
  exit 1
fi

fail=0

verify_one() {
  local component="$1"
  local platform="$2"
  local path sha
  path=$(jq -r ".\"$component\".binaries.\"$platform\".path" "$MANIFEST")
  sha=$(jq -r ".\"$component\".binaries.\"$platform\".sha256" "$MANIFEST")

  if [[ "$path" == "null" ]]; then
    return
  fi

  local full="$ROOT/$path"

  if [[ "$sha" == "0000000000000000000000000000000000000000000000000000000000000000" ]]; then
    if [[ -f "$full" ]]; then
      echo "WARN: $path present but manifest sha is placeholder"
    fi
    echo "skip: $component/$platform (placeholder manifest entry)"
    return
  fi

  if [[ ! -f "$full" ]]; then
    echo "MISSING: $path (manifest expects sha $sha)"
    fail=1
    return
  fi

  local actual
  actual=$(shasum -a 256 "$full" | awk '{print $1}')
  if [[ "$actual" != "$sha" ]]; then
    echo "DRIFT: $path"
    echo "  manifest: $sha"
    echo "  actual:   $actual"
    fail=1
    return
  fi
  echo "ok:    $path  $sha"
}

for component in feivpn feiapi; do
  for platform in linux-amd64 linux-arm64 darwin-arm64; do
    verify_one "$component" "$platform"
  done
done

exit "$fail"
