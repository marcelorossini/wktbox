#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
test_root="$(mktemp -d)"
trap 'rm -rf "$test_root"' EXIT
release_root="$test_root/release"
fake_bin="$test_root/bin"
command_log="$test_root/commands.log"
mkdir -p "$release_root" "$fake_bin"
: >"$command_log"

version="0.1.0"
archives=(
  "wktbox_${version}_linux_amd64.tar.gz"
  "wktbox_${version}_linux_arm64.tar.gz"
  "wktbox_${version}_darwin_amd64.tar.gz"
  "wktbox_${version}_darwin_arm64.tar.gz"
  "wktbox_${version}_windows_amd64.zip"
  "wktbox_${version}_windows_arm64.zip"
)
for archive in "${archives[@]}"; do
  printf 'fixture %s\n' "$archive" >"$release_root/$archive"
done
printf '#!/usr/bin/env bash\n' >"$release_root/install.sh"
printf 'Write-Output "fixture"\n' >"$release_root/install.ps1"
(
  cd "$release_root"
  for archive in "${archives[@]}"; do
    digest="$(sha256sum "$archive" | cut -d' ' -f1)"
    printf '%s  dist/%s\n' "$digest" "$archive"
  done >checksums.txt
)

cat >"$fake_bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'gh %s\n' "$*" >>"${COMMAND_LOG:?}"
if [[ "${1:-} ${2:-}" == "release download" ]]; then
  destination=""
  while (($#)); do
    if [[ "$1" == "--dir" ]]; then
      destination="$2"
      break
    fi
    shift
  done
  test -n "$destination"
  cp "${FAKE_RELEASE_ROOT:?}"/* "$destination/"
  exit 0
fi
if [[ "${1:-} ${2:-}" == "attestation verify" ]]; then
  exit 0
fi
printf 'unsupported fake gh command\n' >&2
exit 2
EOF

cat >"$fake_bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'docker %s\n' "$*" >>"${COMMAND_LOG:?}"
case "$*" in
  *'{{json .Manifest}}'*)
    cat <<'JSON'
{"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","manifests":[{"platform":{"os":"linux","architecture":"amd64"}},{"platform":{"os":"linux","architecture":"arm64"}},{"platform":{"os":"unknown","architecture":"unknown"}}]}
JSON
    ;;
  *'{{json (index .Image "linux/amd64")}}'*|\
  *'{{json (index .Image "linux/arm64")}}'*)
    cat <<JSON
{"config":{"Labels":{"org.opencontainers.image.source":"https://github.com/marcelorossini/wktbox","org.opencontainers.image.revision":"0123456789abcdef0123456789abcdef01234567","org.opencontainers.image.version":"${FAKE_VERSION:?}","org.opencontainers.image.licenses":"Apache-2.0","org.opencontainers.image.description":"fixture runtime image"}}}
JSON
    ;;
  *)
    printf 'unsupported fake docker command: %s\n' "$*" >&2
    exit 2
    ;;
esac
EOF
chmod 0755 "$fake_bin/gh" "$fake_bin/docker"

export COMMAND_LOG="$command_log"
export FAKE_RELEASE_ROOT="$release_root"
export FAKE_VERSION="$version"
export PATH="$fake_bin:$PATH"

bash "$project_root/scripts/verify-release.sh" "v$version"

test "$(grep -c '^gh release download ' "$command_log")" -eq 1
test "$(grep -c '^gh attestation verify ' "$command_log")" -eq 11
test "$(grep -c '^docker buildx imagetools inspect ' "$command_log")" -eq 6

printf 'tampered\n' >>"$release_root/${archives[0]}"
if bash "$project_root/scripts/verify-release.sh" "v$version" \
  >"$test_root/tampered.out" 2>&1; then
  printf 'release verifier accepted a checksum mismatch\n' >&2
  exit 1
fi
grep -q 'FAILED' "$test_root/tampered.out"

printf 'release verifier acceptance tests passed\n'
