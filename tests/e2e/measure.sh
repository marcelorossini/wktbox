#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
metrics_file="$(mktemp)"
cleanup() {
  rm -f "$metrics_file"
}
trap cleanup EXIT

WKTBOX_METRICS_FILE="$metrics_file" \
  bash "$project_root/tests/e2e/two_worktrees.sh" >&2
cat "$metrics_file"
