#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
installer="$project_root/scripts/install.sh"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

fake_bin="$temporary/bin"
release_root="$temporary/releases"
release_dir="$release_root/download/v0.1.0"
mkdir -p "$fake_bin" "$release_dir"

cat >"$fake_bin/uname" <<'EOF'
#!/usr/bin/env bash
case "${1:-}" in
  -s) printf '%s\n' "${TEST_UNAME_S:?}" ;;
  -m) printf '%s\n' "${TEST_UNAME_M:?}" ;;
  *) exit 2 ;;
esac
EOF
chmod 0755 "$fake_bin/uname"

create_archive() {
  local os="$1"
  local architecture="$2"
  local staging="$temporary/staging-${os}-${architecture}"
  local archive="$release_dir/wktbox_0.1.0_${os}_${architecture}.tar.gz"

  mkdir -p "$staging"
  cat >"$staging/wktbox" <<EOF
#!/usr/bin/env bash
printf 'wktbox 0.1.0 ${os}/${architecture}\\n'
EOF
  chmod 0755 "$staging/wktbox"
  tar -C "$staging" -czf "$archive" wktbox
}

create_checksums() {
  (
    cd "$release_dir"
    for archive in *.tar.gz; do
      digest="$(sha256sum "$archive" | cut -d' ' -f1)"
      printf '%s  dist/%s\n' "$digest" "$archive"
    done | sort >checksums.txt
  )
}

create_archive linux amd64
create_archive darwin arm64
create_checksums
printf '{"tag_name":"v0.1.0"}\n' >"$release_root/latest"

run_installer() {
  local home="$1"
  local os="$2"
  local architecture="$3"
  shift 3

  HOME="$home" \
    PATH="$fake_bin:/usr/bin:/bin" \
    TEST_UNAME_S="$os" \
    TEST_UNAME_M="$architecture" \
    bash "$installer" "$@"
}

linux_home="$temporary/home-linux"
linux_install="$temporary/install-linux"
linux_output="$(
  run_installer \
    "$linux_home" Linux x86_64 \
    --version 0.1.0 \
    --install-dir "$linux_install" \
    --base-url "file://$release_root" \
    2>&1
)"
test "$("$linux_install/wktbox")" = "wktbox 0.1.0 linux/amd64"
test -x "$linux_install/wktbox"
grep -Fq "export PATH=\"$linux_install:\$PATH\"" <<<"$linux_output"

darwin_home="$temporary/home-darwin"
darwin_install="$temporary/install-darwin"
run_installer \
  "$darwin_home" Darwin arm64 \
  --version v0.1.0 \
  --install-dir "$darwin_install" \
  --base-url "file://$release_root" \
  >"$temporary/darwin.out" 2>&1
test "$("$darwin_install/wktbox")" = "wktbox 0.1.0 darwin/arm64"

if run_installer \
  "$temporary/home-unsupported" Linux s390x \
  --version 0.1.0 \
  --base-url "file://$release_root" \
  >"$temporary/unsupported.out" 2>&1; then
  printf 'unsupported architecture was accepted\n' >&2
  exit 1
fi
grep -qi 'unsupported architecture' "$temporary/unsupported.out"

missing_install="$temporary/install-missing"
mkdir -p "$missing_install"
printf 'original\n' >"$missing_install/wktbox"
chmod 0755 "$missing_install/wktbox"
if run_installer \
  "$temporary/home-missing" Linux x86_64 \
  --version 9.9.9 \
  --install-dir "$missing_install" \
  --base-url "file://$release_root" \
  >"$temporary/missing.out" 2>&1; then
  printf 'missing release asset was accepted\n' >&2
  exit 1
fi
test "$(cat "$missing_install/wktbox")" = "original"

checksum_backup="$temporary/checksums.txt"
cp "$release_dir/checksums.txt" "$checksum_backup"
archive_name="wktbox_0.1.0_linux_amd64.tar.gz"
printf '%064d  dist/%s\n' 0 "$archive_name" >"$release_dir/checksums.txt"
bad_install="$temporary/install-bad-checksum"
mkdir -p "$bad_install"
printf 'original\n' >"$bad_install/wktbox"
chmod 0755 "$bad_install/wktbox"
if run_installer \
  "$temporary/home-bad-checksum" Linux x86_64 \
  --version 0.1.0 \
  --install-dir "$bad_install" \
  --base-url "file://$release_root" \
  >"$temporary/bad-checksum.out" 2>&1; then
  printf 'bad checksum was accepted\n' >&2
  exit 1
fi
grep -qi 'checksum' "$temporary/bad-checksum.out"
test "$(cat "$bad_install/wktbox")" = "original"
cp "$checksum_backup" "$release_dir/checksums.txt"

upgrade_install="$temporary/install-upgrade"
mkdir -p "$upgrade_install"
printf '#!/usr/bin/env bash\nprintf "old version\\n"\n' >"$upgrade_install/wktbox"
chmod 0755 "$upgrade_install/wktbox"
run_installer \
  "$temporary/home-upgrade" Linux amd64 \
  --version 0.1.0 \
  --install-dir "$upgrade_install" \
  --base-url "file://$release_root" \
  >"$temporary/upgrade.out" 2>&1
test "$("$upgrade_install/wktbox")" = "wktbox 0.1.0 linux/amd64"

default_home="$temporary/home-default"
run_installer \
  "$default_home" Linux x86_64 \
  --version 0.1.0 \
  --base-url "file://$release_root" \
  >"$temporary/default.out" 2>&1
test -x "$default_home/.local/bin/wktbox"

latest_install="$temporary/install-latest"
run_installer \
  "$temporary/home-latest" Linux x86_64 \
  --install-dir "$latest_install" \
  --base-url "file://$release_root" \
  >"$temporary/latest.out" 2>&1
test "$("$latest_install/wktbox")" = "wktbox 0.1.0 linux/amd64"

path_install="$temporary/on-path"
path_output="$(
  HOME="$temporary/home-path" \
    PATH="$fake_bin:$path_install:/usr/bin:/bin" \
    TEST_UNAME_S=Linux \
    TEST_UNAME_M=x86_64 \
    bash "$installer" \
      --version 0.1.0 \
      --install-dir "$path_install" \
      --base-url "file://$release_root" \
      2>&1
)"
if grep -q 'export PATH=' <<<"$path_output"; then
  printf 'PATH guidance was printed for a directory already on PATH\n' >&2
  exit 1
fi

printf 'Unix installer acceptance tests passed\n'
