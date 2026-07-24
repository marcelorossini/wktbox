#!/bin/sh
set -eu

repository="marcelorossini/wktbox"
default_base_url="https://github.com/$repository/releases"
default_api_url="https://api.github.com/repos/$repository/releases/latest"

version=""
install_dir="${HOME}/.local/bin"
base_url="$default_base_url"
custom_base_url=false

usage() {
  printf '%s\n' \
    'Usage: install.sh [--version X.Y.Z] [--install-dir PATH] [--base-url URL]'
}

require_value() {
  if [ -z "${2:-}" ]; then
    printf '%s requires a value\n' "$1" >&2
    usage >&2
    exit 2
  fi
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      require_value "$1" "${2:-}"
      version="$2"
      shift 2
      ;;
    --install-dir)
      require_value "$1" "${2:-}"
      install_dir="$2"
      shift 2
      ;;
    --base-url)
      require_value "$1" "${2:-}"
      base_url="${2%/}"
      custom_base_url=true
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      printf 'unknown option: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

normalize_version() {
  normalized_candidate="${1#v}"
  if ! printf '%s\n' "$normalized_candidate" |
    grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$'; then
    printf 'invalid version "%s"; expected X.Y.Z or vX.Y.Z\n' "$1" >&2
    return 1
  fi
  printf '%s\n' "$normalized_candidate"
}

download() {
  curl --fail --location --retry 3 --output "$2" "$1"
}

temporary="$(mktemp -d)"
candidate=""
cleanup() {
  rm -rf "$temporary"
  if [ -n "$candidate" ]; then
    rm -f "$candidate"
  fi
}
trap cleanup 0

if [ -z "$version" ]; then
  api_url="$default_api_url"
  if [ "$custom_base_url" = true ]; then
    api_url="$base_url/latest"
  fi
  latest_json="$temporary/latest.json"
  download "$api_url" "$latest_json"
  version="$(
    sed -n \
      's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
      "$latest_json" |
      head -n 1
  )"
  if [ -z "$version" ]; then
    printf 'latest release response does not contain tag_name\n' >&2
    exit 1
  fi
fi
version="$(normalize_version "$version")"

case "$(uname -s)" in
  Linux) operating_system="linux" ;;
  Darwin) operating_system="darwin" ;;
  *)
    printf 'unsupported operating system: %s\n' "$(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64|amd64) architecture="amd64" ;;
  aarch64|arm64) architecture="arm64" ;;
  *)
    printf 'unsupported architecture: %s\n' "$(uname -m)" >&2
    exit 1
    ;;
esac

archive="wktbox_${version}_${operating_system}_${architecture}.tar.gz"
release_url="$base_url/download/v${version}"
archive_path="$temporary/$archive"
checksums_path="$temporary/checksums.txt"

download "$release_url/$archive" "$archive_path"
download "$release_url/checksums.txt" "$checksums_path"

expected_digest=""
while read -r digest file; do
  if [ "${file##*/}" = "$archive" ]; then
    expected_digest="$digest"
    break
  fi
done <"$checksums_path"
if [ -z "$expected_digest" ]; then
  printf 'checksum for %s is missing from checksums.txt\n' "$archive" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual_digest="$(sha256sum "$archive_path" | cut -d' ' -f1)"
elif command -v shasum >/dev/null 2>&1; then
  actual_digest="$(shasum -a 256 "$archive_path" | cut -d' ' -f1)"
else
  printf 'checksum verification requires sha256sum or shasum\n' >&2
  exit 1
fi
actual_digest="$(
  printf '%s\n' "$actual_digest" | tr '[:upper:]' '[:lower:]'
)"
expected_digest="$(
  printf '%s\n' "$expected_digest" | tr '[:upper:]' '[:lower:]'
)"
if [ "$actual_digest" != "$expected_digest" ]; then
  printf 'checksum verification failed for %s\n' "$archive" >&2
  exit 1
fi

extract_dir="$temporary/extract"
mkdir -p "$extract_dir"
tar -xzf "$archive_path" -C "$extract_dir"
if [ ! -f "$extract_dir/wktbox" ]; then
  printf 'archive %s does not contain wktbox\n' "$archive" >&2
  exit 1
fi

mkdir -p "$install_dir"
candidate="$(mktemp "$install_dir/.wktbox.XXXXXX")"
install -m 0755 "$extract_dir/wktbox" "$candidate"
mv -f "$candidate" "$install_dir/wktbox"
candidate=""

printf 'Installed Wktbox %s at %s\n' "$version" "$install_dir/wktbox"
case ":${PATH:-}:" in
  *":$install_dir:"*) ;;
  *)
    printf 'Add Wktbox to PATH for this shell:\n'
    printf '  export PATH="%s:$PATH"\n' "$install_dir"
    ;;
esac
