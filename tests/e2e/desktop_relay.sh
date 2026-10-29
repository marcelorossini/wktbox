#!/usr/bin/env bash
set -euo pipefail

# Emulates the Docker Desktop host import path on a native Linux engine. Docker
# Desktop forwards host.docker.internal to host loopback, so Wktbox binds the
# relay to 127.0.0.1 there. A user-space forwarder on the bridge gateway plays
# the role of Desktop's network proxy without changing host networking.

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
source "$project_root/tests/e2e/assert-isolation.sh"

test_root="$(mktemp -d)"
workspace="$test_root/desktop relay"
state_root="$test_root/wktbox state"
wktbox="$project_root/bin/wktbox"
box_created=false
pids=()

cleanup() {
  for pid in "${pids[@]}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
  if [[ "$box_created" == true ]]; then
    "$wktbox" --path "$workspace" destroy --force >/dev/null 2>&1 || true
  fi
  rm -rf "$test_root"
}
trap cleanup EXIT

fail() {
  printf 'desktop relay E2E failed: %s\n' "$*" >&2
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

listening_on() {
  local address="$1"
  ss -ltnH "sport = :${address##*:}" | awk '{print $4}' | grep -qx "$address"
}

gateway="$(docker network inspect bridge \
  --format '{{range .IPAM.Config}}{{.Gateway}} {{end}}' |
  tr ' ' '\n' | grep -E '^[0-9]+(\.[0-9]+){3}$' | head -n 1)"
[[ -n "$gateway" ]] || fail "default bridge gateway is unavailable"

mkdir -p "$workspace" "$test_root/host-site"
printf 'desktop-host\n' >"$test_root/host-site/index.html"
export WKTBOX_STATE_HOME="$state_root"
export WKTBOX_RELAY_BIND=127.0.0.1

make -C "$project_root" build >/dev/null
docker build -q -f "$project_root/images/webtop/Dockerfile" \
  -t wktbox/webtop:dev "$project_root" >/dev/null

"$wktbox" --path "$workspace" up >/dev/null
box_created=true
status="$("$wktbox" --path "$workspace" --json status)"
relay_port="$(($(json_value "$status" ports.webtopHttp) + 4))"

python3 -I "$project_root/tests/e2e/support/desktop_forwarder.py" \
  "$gateway" "$relay_port" >/dev/null 2>&1 &
forwarder=$!
pids+=("$forwarder")
host_port="$(free_port)"
python3 -m http.server "$host_port" --bind 127.0.0.1 \
  --directory "$test_root/host-site" >/dev/null 2>&1 &
pids+=("$!")
sleep 1

"$wktbox" --path "$workspace" port import "$host_port" >/dev/null ||
  fail "import through Desktop-style forwarding was rejected"
listening_on "127.0.0.1:$relay_port" || fail "relay is not on host loopback"
if ss -ltnH "sport = :$relay_port" | awk '{print $4}' | grep -Eq '^(\*|0\.0\.0\.0):'; then
  fail "relay listens on every interface"
fi
deadline=$((SECONDS + 30))
until "$wktbox" --path "$workspace" exec -- \
  curl --fail --silent "localhost:$host_port/index.html" 2>/dev/null | grep -qx desktop-host; do
  ((SECONDS < deadline)) || fail "host import did not answer in Webtop"
  sleep 1
done

kill "$forwarder"
sleep 1
"$wktbox" --path "$workspace" --json port list | grep -q 'unreachable from the box' ||
  fail "status did not report the unreachable relay"
if "$wktbox" --path "$workspace" restart >/dev/null 2>"$test_root/restart.err"; then
  fail "restart accepted an unreachable relay"
fi
grep -q 'not reachable from the box' "$test_root/restart.err" ||
  fail "restart error was not actionable"
if ss -ltnH "sport = :$relay_port" | awk '{print $4}' | grep -Eq '^(\*|0\.0\.0\.0):'; then
  fail "relay widened its bind after the failure"
fi

"$wktbox" --path "$workspace" destroy --force >/dev/null
box_created=false

printf 'wktbox desktop relay E2E passed: loopback bind, forwarded imports, and safe failure\n'
