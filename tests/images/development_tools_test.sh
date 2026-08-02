#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
image="${WKTBOX_DEVTOOLS_TEST_IMAGE:-wktbox/webtop:development-tools-test}"
test_root="$(mktemp -d)"
config_volume="wktbox-nvm-test-$$-$RANDOM"
container="wktbox-nvm-test-$$-$RANDOM"

cleanup() {
  docker rm --force "$container" >/dev/null 2>&1 || true
  docker volume rm --force "$config_volume" >/dev/null 2>&1 || true
  rm -rf "$test_root"
}
trap cleanup EXIT

docker volume create "$config_volume" >/dev/null

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

cat >"$test_root/check-nvm.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

export HOME=/config
. /etc/profile.d/wktbox-nvm.sh

test "$(nvm --version)" = "0.40.4"
node --version
npm --version
npx --version
corepack --version
test -n "$(node -p 'process.release.lts')"
test -w "$NVM_DIR"
touch "$NVM_DIR/.wktbox-user-marker"
SH
chmod 0755 "$test_root/check-nvm.sh"

start_webtop() {
  docker run --detach \
    --name "$container" \
    --shm-size 1g \
    --env PUID=1000 \
    --env PGID=1000 \
    --volume "$config_volume:/config" \
    --volume "$test_root/check-nvm.sh:/tmp/check-nvm.sh:ro" \
    "$image" >/dev/null
}

wait_for_nvm() {
  local deadline=$((SECONDS + 30))

  while ((SECONDS < deadline)); do
    if docker exec "$container" test -s /config/.nvm/nvm.sh; then
      return 0
    fi
    if [[ "$(docker inspect --format '{{.State.Running}}' "$container")" != "true" ]]; then
      docker logs "$container" >&2
      return 1
    fi
    sleep 0.25
  done

  docker logs "$container" >&2
  return 1
}

start_webtop
wait_for_nvm
docker exec "$container" s6-setuidgid abc /tmp/check-nvm.sh
docker exec --tty "$container" s6-setuidgid abc bash -ic 'nvm --version && node --version'
docker rm --force "$container" >/dev/null

start_webtop
wait_for_nvm
docker exec "$container" test -f /config/.nvm/.wktbox-user-marker
docker rm --force "$container" >/dev/null

printf 'Webtop development package tests passed\n'
