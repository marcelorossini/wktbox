#!/usr/bin/env bash

normalize_version() {
  local version="${1#v}"
  if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]]; then
    return 1
  fi
  printf '%s\n' "$version"
}

archive_unix() {
  local binary="$1"
  local archive="$2"
  local staging="$3"
  local epoch="$4"

  rm -rf "$staging"
  mkdir -p "$staging"
  install -m 0755 "$binary" "$staging/wktbox"
  install -m 0644 LICENSE "$staging/LICENSE"
  install -m 0644 README.md "$staging/README.md"
  touch -d "@$epoch" "$staging/LICENSE" "$staging/README.md" "$staging/wktbox"
  LC_ALL=C tar \
    --sort=name \
    --mtime="@$epoch" \
    --owner=0 \
    --group=0 \
    --numeric-owner \
    --format=gnu \
    -C "$staging" \
    -czf "$archive" \
    LICENSE README.md wktbox
}

archive_windows() {
  local binary="$1"
  local archive="$2"
  local staging="$3"
  local epoch="$4"

  rm -rf "$staging"
  mkdir -p "$staging"
  install -m 0755 "$binary" "$staging/wktbox.exe"
  install -m 0644 LICENSE "$staging/LICENSE"
  install -m 0644 README.md "$staging/README.md"
  touch -d "@$epoch" \
    "$staging/LICENSE" "$staging/README.md" "$staging/wktbox.exe"
  python3 - "$staging" "$archive" "$epoch" <<'PY'
import datetime
import pathlib
import sys
import zipfile

staging = pathlib.Path(sys.argv[1])
archive = pathlib.Path(sys.argv[2])
epoch = max(int(sys.argv[3]), 315532800)
timestamp = datetime.datetime.fromtimestamp(epoch, datetime.UTC)
date_time = (
    timestamp.year,
    timestamp.month,
    timestamp.day,
    timestamp.hour,
    timestamp.minute,
    timestamp.second,
)

with zipfile.ZipFile(
    archive,
    mode="w",
    compression=zipfile.ZIP_DEFLATED,
    compresslevel=9,
) as output:
    for name, mode in (
        ("LICENSE", 0o100644),
        ("README.md", 0o100644),
        ("wktbox.exe", 0o100755),
    ):
        info = zipfile.ZipInfo(name, date_time)
        info.create_system = 3
        info.compress_type = zipfile.ZIP_DEFLATED
        info.external_attr = mode << 16
        output.writestr(info, (staging / name).read_bytes())
PY
}

write_checksums() {
  local distribution="$1"
  local distribution_name="${distribution##*/}"
  local names=()
  local name path

  for path in "$distribution"/*.tar.gz "$distribution"/*.zip; do
    names+=("${path##*/}")
  done
  printf '%s\n' "${names[@]}" |
    LC_ALL=C sort |
    while IFS= read -r name; do
      sha256sum "$distribution_name/$name"
    done >"$distribution/checksums.txt"
}

build_binary() {
  local version="$1"
  local goos="$2"
  local goarch="$3"
  local output="$4"

  printf 'building %s/%s\n' "$goos" "$goarch"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath \
    -ldflags "-s -w -X wktbox/internal/version.value=$version" \
    -o "$output" \
    ./cmd/wktbox
}

build_development() {
  local version="$1"
  local distribution="$project_root/dist"
  local targets=(
    "windows/amd64"
    "windows/arm64"
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
  )
  local target goos goarch extension output

  mkdir -p "$distribution"
  rm -f "$distribution"/wktbox_* "$distribution/checksums.txt"
  for target in "${targets[@]}"; do
    goos="${target%/*}"
    goarch="${target#*/}"
    extension=""
    if [[ "$goos" == "windows" ]]; then
      extension=".exe"
    fi
    output="$distribution/wktbox_${version}_${goos}_${goarch}${extension}"
    build_binary "$version" "$goos" "$goarch" "$output"
  done
  (
    cd "$distribution"
    sha256sum wktbox_* >checksums.txt
  )
}

build_release() {
  local version="$1"
  local distribution="$project_root/dist"
  local epoch="${SOURCE_DATE_EPOCH:-}"
  local temporary
  local targets=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
    "windows/amd64"
    "windows/arm64"
  )
  local target goos goarch binary archive staging

  if [[ -z "$epoch" ]]; then
    epoch="$(git log -1 --format=%ct)"
  fi
  if [[ ! "$epoch" =~ ^[0-9]+$ ]]; then
    printf 'SOURCE_DATE_EPOCH must be an integer, got %q\n' "$epoch" >&2
    return 2
  fi

  temporary="$(mktemp -d)"
  trap 'rm -rf "$temporary"' RETURN
  rm -rf "$distribution"
  mkdir -p "$distribution"

  for target in "${targets[@]}"; do
    goos="${target%/*}"
    goarch="${target#*/}"
    binary="$temporary/wktbox"
    if [[ "$goos" == "windows" ]]; then
      binary="$temporary/wktbox.exe"
    fi
    build_binary "$version" "$goos" "$goarch" "$binary"

    staging="$temporary/archive-${goos}-${goarch}"
    if [[ "$goos" == "windows" ]]; then
      archive="$distribution/wktbox_${version}_${goos}_${goarch}.zip"
      archive_windows "$binary" "$archive" "$staging" "$epoch"
    else
      archive="$distribution/wktbox_${version}_${goos}_${goarch}.tar.gz"
      archive_unix "$binary" "$archive" "$staging" "$epoch"
    fi
  done

  write_checksums "$distribution"
}
