#!/usr/bin/env bash
set -euo pipefail

# Exercises the workflow of a coding agent driving a box from the host:
# command identity and toolchain, host file ownership, /artifacts, host
# imports inside Webtop, resource limits, and the CDP browser.

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
source "$project_root/tests/e2e/assert-isolation.sh"

test_root="$(mktemp -d)"
workspace="$test_root/agent workspace"
state_root="$test_root/wktbox state"
wktbox="$project_root/bin/wktbox"
box_created=false
host_server_pid=""

cleanup() {
  if [[ -n "$host_server_pid" ]]; then
    kill "$host_server_pid" >/dev/null 2>&1 || true
  fi
  if [[ "$box_created" == true ]]; then
    "$wktbox" --path "$workspace" destroy --force >/dev/null 2>&1 || true
  fi
  rm -rf "$test_root"
}
trap cleanup EXIT

fail() {
  printf 'agent workflow E2E failed: %s\n' "$*" >&2
  "$wktbox" --path "$workspace" --json status >&2 || true
  exit 1
}

free_port() {
  python3 -c '
import socket

with socket.socket() as listener:
    listener.bind(("127.0.0.1", 0))
    print(listener.getsockname()[1])
'
}

mkdir -p "$workspace/site"
printf 'agent-site\n' >"$workspace/site/index.html"
printf '%s\n' \
  'version: 1' \
  'resources:' \
  '  cpus: 2' \
  '  memory: 2g' \
  '  pids: 4096' >"$workspace/.wktbox.yml"
cat >"$workspace/compose.yml" <<'YAML'
services:
  web:
    image: python:3.14-alpine
    command: ["python", "-m", "http.server", "5173", "--directory", "/site"]
    volumes:
      - ./site:/site
    ports:
      - "5173:5173"
  writer:
    image: alpine:3.22
    command:
      - sh
      - -c
      - mkdir -p /out/generated && date >/out/generated/by-root.txt && date >/artifacts/report.txt
    volumes:
      - ./:/out
      - /artifacts:/artifacts
YAML

export WKTBOX_STATE_HOME="$state_root"

make -C "$project_root" build >/dev/null
docker build -q -f "$project_root/images/webtop/Dockerfile" \
  -t wktbox/webtop:dev "$project_root" >/dev/null

"$wktbox" --path "$workspace" up >/dev/null
box_created=true
status="$("$wktbox" --path "$workspace" --json status)"
assert_json_status "$status" ready
box_id="$(json_value "$status" id)"

# Commands run as the host user with the NVM toolchain on PATH.
identity="$("$wktbox" --path "$workspace" run -- sh -c 'id -u; id -g' 2>/dev/null)"
[[ "$identity" == "$(id -u)"$'\n'"$(id -g)" ]] ||
  fail "run identity was $identity"
"$wktbox" --path "$workspace" run -- node --version >/dev/null 2>&1 ||
  fail "node is not on PATH for wktbox run"
"$wktbox" --path "$workspace" run -- docker version >/dev/null 2>&1 ||
  fail "the workspace user cannot reach the inner Docker daemon"
[[ "$("$wktbox" --path "$workspace" run --root -- id -u 2>/dev/null)" == 0 ]] ||
  fail "--root did not run as root"
set +e
"$wktbox" --path "$workspace" run -- sh -c 'exit 7' >/dev/null 2>&1
exit_code=$?
set -e
[[ "$exit_code" == 7 ]] || fail "child exit code was $exit_code"

# Files from commands and root containers stay owned by the host user.
"$wktbox" --path "$workspace" run -- mkdir -p build/out >/dev/null 2>&1
"$wktbox" --path "$workspace" run --root -- touch build/root-file >/dev/null 2>&1
"$wktbox" --path "$workspace" compose -- run --rm writer >/dev/null 2>&1 ||
  fail "writer container failed"
foreign="$(find "$workspace" ! -user "$(id -u)" -print)"
[[ -z "$foreign" ]] || fail "workspace entries not owned by host user: $foreign"
rm -rf "$workspace/build" "$workspace/generated" ||
  fail "host user cannot remove files created in the box"

# /artifacts is shared with workload containers and writable in Webtop.
"$wktbox" --path "$workspace" exec -- test -s /artifacts/report.txt ||
  fail "workload output did not reach Webtop /artifacts"
"$wktbox" --path "$workspace" exec -- rm /artifacts/report.txt ||
  fail "the workspace user cannot manage /artifacts"
"$wktbox" --path "$workspace" exec -- touch /artifacts/from-webtop ||
  fail "the workspace user cannot write /artifacts"

# Resource limits bound the DinD service.
limits="$(docker inspect "wktbox-$box_id-docker-1" \
  --format '{{.HostConfig.Memory}} {{.HostConfig.NanoCpus}} {{.HostConfig.PidsLimit}}')"
[[ "$limits" == "2147483648 2000000000 4096" ]] || fail "DinD limits were $limits"

# Published project ports answer on Webtop localhost and in Chromium.
"$wktbox" --path "$workspace" compose -- up -d web >/dev/null 2>&1
deadline=$((SECONDS + 30))
until "$wktbox" --path "$workspace" exec -- \
  curl --fail --silent localhost:5173/index.html 2>/dev/null | grep -qx agent-site; do
  ((SECONDS < deadline)) || fail "localhost:5173 did not answer in Webtop"
  sleep 1
done
cdp="$(json_value "$("$wktbox" --path "$workspace" --json status)" urls.browserCdp)"
wait_for_cdp "$cdp"
curl --fail --silent -X PUT "$cdp/json/new?http://localhost:5173/index.html" >/dev/null ||
  fail "CDP could not open a tab"
deadline=$((SECONDS + 30))
until curl --fail --silent "$cdp/json/list" | grep -q 'localhost:5173'; do
  ((SECONDS < deadline)) || fail "CDP tab did not load the box URL"
  sleep 1
done

# Desktop applications start from their menu entries inside Webtop.
webtop="wktbox-$box_id-webtop-1"
docker exec -d -u abc -e DISPLAY=:1 -e HOME=/config "$webtop" \
  gio launch /usr/share/applications/com.microsoft.VSCode.desktop /workspace
deadline=$((SECONDS + 60))
until docker exec -u abc -e DISPLAY=:1 "$webtop" \
  xdotool search --onlyvisible --name 'Visual Studio Code' >/dev/null 2>&1; do
  ((SECONDS < deadline)) || fail "VS Code did not open a window in Webtop"
  sleep 2
done

# Host imports answer inside Webtop as well as in workloads.
host_port="$(free_port)"
mkdir -p "$test_root/host-site"
printf 'host-service\n' >"$test_root/host-site/index.html"
python3 -m http.server "$host_port" --bind 127.0.0.1 \
  --directory "$test_root/host-site" >/dev/null 2>&1 &
host_server_pid=$!
"$wktbox" --path "$workspace" port import "$host_port" >/dev/null
deadline=$((SECONDS + 30))
until "$wktbox" --path "$workspace" exec -- \
  curl --fail --silent "localhost:$host_port/index.html" 2>/dev/null | grep -qx host-service; do
  ((SECONDS < deadline)) || fail "host import did not answer in Webtop"
  sleep 1
done

"$wktbox" --path "$workspace" destroy --force >/dev/null
box_created=false

printf 'wktbox agent workflow E2E passed: identity, ownership, artifacts, limits, CDP, desktop apps, and imports\n'
