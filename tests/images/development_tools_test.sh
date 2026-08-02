#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
image="${WKTBOX_DEVTOOLS_TEST_IMAGE:-wktbox/webtop:development-tools-test}"

docker build \
  --file "$project_root/images/webtop/Dockerfile" \
  --tag "$image" \
  "$project_root"

docker run --rm --entrypoint bash "$image" -lc '
  set -euo pipefail

  python --version
  python3 --version
  pip --version
  pipx --version
  code --version --no-sandbox --user-data-dir=/tmp/wktbox-code-test
  command -v dbeaver

  for command_name in cc make jq rg unzip zip; do
    command -v "$command_name"
  done

  dpkg-query --show \
    python3 python3-pip python3-venv python-is-python3 pipx \
    code dbeaver-ce build-essential jq ripgrep unzip zip
'

printf 'Webtop development package tests passed\n'
