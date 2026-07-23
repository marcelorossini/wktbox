#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$project_root"

# shellcheck source=scripts/release_lib.sh
source "$project_root/scripts/release_lib.sh"

requested_version="${1:-${WKTBOX_VERSION:-dev}}"
case "$requested_version" in
  dev|ci)
    build_development "$requested_version"
    ;;
  *)
    version="$(normalize_version "$requested_version")" || {
      printf 'invalid release version %q; expected X.Y.Z or vX.Y.Z\n' \
        "$requested_version" >&2
      exit 2
    }
    build_release "$version"
    ;;
esac
