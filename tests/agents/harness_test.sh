#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
source "$project_root/tests/agents/shell_environment.sh"

test_root="$(mktemp -d)"
trap 'rm -rf "$test_root"' EXIT
sample_home="$test_root/home"
fixture_bin="$test_root/bin"
mkdir -p "$fixture_bin"
printf '#!/usr/bin/env bash\nexit 0\n' >"$fixture_bin/wktbox"
chmod 0755 "$fixture_bin/wktbox"

configure_agent_login_shell "$sample_home" "$fixture_bin"

actual="$(
  HOME="$sample_home" \
    PATH="/usr/bin:/bin" \
    /bin/bash -lc 'command -v wktbox'
)"
if [[ "$actual" != "$fixture_bin/wktbox" ]]; then
  printf 'login shell resolved wktbox as %q, expected %q\n' \
    "$actual" "$fixture_bin/wktbox" >&2
  exit 1
fi

if ! grep -q -- '--skip-git-repo-check' \
  "$project_root/tests/agents/evaluate.sh"; then
  printf 'Codex evaluation does not support plain directories\n' >&2
  exit 1
fi

printf 'agent harness shell environment: PASS\n'
