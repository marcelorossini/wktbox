#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

fake_bin="$temporary/bin"
mkdir -p "$fake_bin"
build_log="$temporary/go-build.log"

cat >"$fake_bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf 'CGO_ENABLED=%s GOOS=%s GOARCH=%s %s\n' \
  "${CGO_ENABLED:-}" "${GOOS:-}" "${GOARCH:-}" "$*" >>"$BUILD_LOG"

output=""
while (($#)); do
  if [[ "$1" == "-o" ]]; then
    output="$2"
    break
  fi
  shift
done
if [[ -z "$output" ]]; then
  printf 'fake go expected an output path\n' >&2
  exit 2
fi
printf 'fake wktbox %s/%s\n' "$GOOS" "$GOARCH" >"$output"
chmod 0755 "$output"
EOF
chmod 0755 "$fake_bin/go"

export BUILD_LOG="$build_log"
export PATH="$fake_bin:$PATH"
export SOURCE_DATE_EPOCH=1700000000

cd "$project_root"
rm -rf dist

bash scripts/build.sh v0.1.0

archives=(
  wktbox_0.1.0_linux_amd64.tar.gz
  wktbox_0.1.0_linux_arm64.tar.gz
  wktbox_0.1.0_darwin_amd64.tar.gz
  wktbox_0.1.0_darwin_arm64.tar.gz
  wktbox_0.1.0_windows_amd64.zip
  wktbox_0.1.0_windows_arm64.zip
)

for archive in "${archives[@]}"; do
  test -f "dist/$archive"
done
test "$(find dist -maxdepth 1 -type f -name 'wktbox_*' | wc -l)" -eq 6

sha256sum --check dist/checksums.txt
test "$(wc -l <dist/checksums.txt)" -eq 6
for archive in "${archives[@]}"; do
  grep -Eq "  dist/${archive}$" dist/checksums.txt
done

expected_unix=$'LICENSE\nREADME.md\nwktbox'
actual_unix="$(
  tar -tzf dist/wktbox_0.1.0_linux_amd64.tar.gz | sort
)"
test "$actual_unix" = "$expected_unix"

expected_windows=$'LICENSE\nREADME.md\nwktbox.exe'
actual_windows="$(
  python3 - dist/wktbox_0.1.0_windows_amd64.zip <<'PY'
import sys
import zipfile

with zipfile.ZipFile(sys.argv[1]) as archive:
    print("\n".join(sorted(archive.namelist())))
PY
)"
test "$actual_windows" = "$expected_windows"

test "$(grep -c 'CGO_ENABLED=0' "$build_log")" -eq 6
test "$(grep -c -- '-trimpath' "$build_log")" -eq 6
test "$(grep -c -- '-X wktbox/internal/version.value=0.1.0' "$build_log")" -eq 6

cp dist/checksums.txt "$temporary/first-checksums.txt"
bash scripts/build.sh 0.1.0
cmp "$temporary/first-checksums.txt" dist/checksums.txt

if bash scripts/build.sh release-candidate >"$temporary/invalid.out" 2>&1; then
  printf 'invalid semantic version was accepted\n' >&2
  exit 1
fi
grep -q 'invalid release version' "$temporary/invalid.out"

printf 'release build acceptance tests passed\n'
