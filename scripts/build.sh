#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$project_root"

version="${1:-${WKTBOX_VERSION:-dev}}"
if [[ ! "$version" =~ ^[0-9A-Za-z][0-9A-Za-z._-]*$ ]]; then
  printf 'invalid version %q; use only letters, digits, dot, underscore and hyphen\n' \
    "$version" >&2
  exit 2
fi
targets=(
  "windows/amd64"
  "windows/arm64"
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
)

mkdir -p dist
rm -f dist/wktbox_* dist/checksums.txt

for target in "${targets[@]}"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  extension=""
  if [[ "$goos" == "windows" ]]; then
    extension=".exe"
  fi
  output="dist/wktbox_${version}_${goos}_${goarch}${extension}"
  printf 'building %s -> %s\n' "$target" "$output"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath \
    -ldflags "-s -w -X wktbox/internal/version.value=$version" \
    -o "$output" \
    ./cmd/wktbox
done

sha256sum dist/wktbox_* >dist/checksums.txt
