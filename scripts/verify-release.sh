#!/usr/bin/env bash
set -euo pipefail

repository="marcelorossini/wktbox"
tag="${1:-}"
if [[ $# -ne 1 || ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]]; then
  printf 'usage: verify-release.sh vX.Y.Z\n' >&2
  exit 2
fi
version="${tag#v}"

for command in gh docker sha256sum python3; do
  if ! command -v "$command" >/dev/null 2>&1; then
    printf 'release verification requires %s\n' "$command" >&2
    exit 1
  fi
done

temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
download="$temporary/release"
mkdir -p "$download"

archives=(
  "wktbox_${version}_linux_amd64.tar.gz"
  "wktbox_${version}_linux_arm64.tar.gz"
  "wktbox_${version}_darwin_amd64.tar.gz"
  "wktbox_${version}_darwin_arm64.tar.gz"
  "wktbox_${version}_windows_amd64.zip"
  "wktbox_${version}_windows_arm64.zip"
)
assets=(
  "${archives[@]}"
  checksums.txt
  install.sh
  install.ps1
)

gh release download "$tag" \
  --repo "$repository" \
  --dir "$download"

expected_names="$(printf '%s\n' "${assets[@]}" | LC_ALL=C sort)"
actual_names="$(
  find "$download" -mindepth 1 -maxdepth 1 -type f -printf '%f\n' |
    LC_ALL=C sort
)"
if [[ "$actual_names" != "$expected_names" ]]; then
  printf 'release asset set does not match %s\n' "$tag" >&2
  printf 'expected:\n%s\nactual:\n%s\n' \
    "$expected_names" "$actual_names" >&2
  exit 1
fi

(
  cd "$download"
  sed 's#  dist/#  #' checksums.txt | sha256sum --check -
)

for asset in "${assets[@]}"; do
  gh attestation verify "$download/$asset" --repo "$repository"
done

verify_image_config() {
  local reference="$1"
  local platform="$2"
  local config
  config="$(
    docker buildx imagetools inspect "$reference" \
      --format "{{json (index .Image \"$platform\")}}"
  )"
  python3 - "$reference" "$platform" "$version" "$config" <<'PY'
import json
import re
import sys

reference, platform, version, raw = sys.argv[1:]
image = json.loads(raw)
config = image.get("config", {})
labels = config.get("Labels") or config.get("labels") or {}
expected = {
    "org.opencontainers.image.source": (
        "https://github.com/marcelorossini/wktbox"
    ),
    "org.opencontainers.image.version": version,
    "org.opencontainers.image.licenses": "Apache-2.0",
}
for name, value in expected.items():
    if labels.get(name) != value:
        raise SystemExit(
            f"{reference} {platform} label {name}="
            f"{labels.get(name)!r}, expected {value!r}"
        )
revision = labels.get("org.opencontainers.image.revision", "")
if not re.fullmatch(r"[0-9a-f]{40}", revision):
    raise SystemExit(
        f"{reference} {platform} has invalid revision label {revision!r}"
    )
if not labels.get("org.opencontainers.image.description"):
    raise SystemExit(f"{reference} {platform} lacks image description")
PY
}

for component in webtop gateway; do
  reference="ghcr.io/$repository/$component:$version"
  manifest="$(
    docker buildx imagetools inspect "$reference" \
      --format '{{json .Manifest}}'
  )"
  python3 - "$reference" "$manifest" <<'PY'
import json
import sys

reference, raw = sys.argv[1:]
manifest = json.loads(raw)
platforms = {
    (
        entry.get("platform", {}).get("os"),
        entry.get("platform", {}).get("architecture"),
    )
    for entry in manifest.get("manifests", [])
}
required = {("linux", "amd64"), ("linux", "arm64")}
missing = required - platforms
if missing:
    raise SystemExit(
        f"{reference} is missing platforms: {sorted(missing)}"
    )
PY
  verify_image_config "$reference" linux/amd64
  verify_image_config "$reference" linux/arm64
  gh attestation verify "oci://$reference" --repo "$repository"
done

printf 'Wktbox %s release verification passed\n' "$tag"
