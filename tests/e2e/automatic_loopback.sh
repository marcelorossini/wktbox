#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
source "$project_root/tests/e2e/assert-isolation.sh"

test_root="$(mktemp -d)"
repository="$test_root/source repository"
worktree_a="$test_root/worktrees/feature a"
worktree_b="$test_root/worktrees/feature b"
state_root="$test_root/wktbox state"
env_a="$test_root/box-a.env"
env_b="$test_root/box-b.env"
wktbox="$project_root/bin/wktbox"
box_a_created=false
box_b_created=false
blocker=""

cleanup_managed_for_path() {
  local worktree="$1"
  local projects
  projects="$(docker ps -a \
    --filter 'label=io.wktbox.managed=true' \
    --filter "label=io.wktbox.worktree=$worktree" \
    --format '{{.Label "com.docker.compose.project"}}' | sort -u)"
  local project
  for project in $projects; do
    local container
    for container in $(docker ps -aq \
      --filter "label=com.docker.compose.project=$project"); do
      docker rm -f "$container" >/dev/null 2>&1 || true
    done
    local volume
    for volume in $(docker volume ls -q \
      --filter "label=com.docker.compose.project=$project"); do
      docker volume rm "$volume" >/dev/null 2>&1 || true
    done
    local network
    for network in $(docker network ls -q \
      --filter "label=com.docker.compose.project=$project"); do
      docker network rm "$network" >/dev/null 2>&1 || true
    done
  done
}

cleanup() {
  if [[ -n "$blocker" ]]; then
    docker rm -f "$blocker" >/dev/null 2>&1 || true
  fi
  if [[ "$box_a_created" == true && -d "$worktree_a" ]]; then
    "$wktbox" --path "$worktree_a" destroy --force >/dev/null 2>&1 || true
  fi
  if [[ "$box_b_created" == true && -d "$worktree_b" ]]; then
    "$wktbox" --path "$worktree_b" destroy --force >/dev/null 2>&1 || true
  fi
  cleanup_managed_for_path "$worktree_a"
  cleanup_managed_for_path "$worktree_b"
  rm -rf "$test_root"
}
trap cleanup EXIT

fail_with_diagnostics() {
  local description="$1"
  printf 'automatic loopback E2E failed: %s\n' "$description" >&2
  if [[ "$box_a_created" == true ]]; then
    "$wktbox" --path "$worktree_a" --json status >&2 || true
    "$wktbox" --path "$worktree_a" logs loopback >&2 || true
  fi
  exit 1
}

route_field() {
  local document="$1"
  local port="$2"
  local field="$3"
  printf '%s' "$document" | python3 -c '
import json
import sys

status = json.load(sys.stdin)
port, field = int(sys.argv[1]), sys.argv[2]
matches = [
    route for route in status.get("loopback", {}).get("routes", [])
    if route.get("port") == port
]
if len(matches) != 1:
    raise SystemExit(1)
value = matches[0].get(field, "")
if isinstance(value, list):
    print(",".join(value))
else:
    print(value)
' "$port" "$field"
}

route_absent() {
  local document="$1"
  local port="$2"
  printf '%s' "$document" | python3 -c '
import json
import sys

status = json.load(sys.stdin)
port = int(sys.argv[1])
if any(
    route.get("port") == port
    for route in status.get("loopback", {}).get("routes", [])
):
    raise SystemExit(1)
' "$port"
}

wait_for_route() {
  local worktree="$1"
  local port="$2"
  local expected="$3"
  local deadline=$((SECONDS + 60))
  while ((SECONDS < deadline)); do
    local status
    status="$("$wktbox" --path "$worktree" --json status 2>/dev/null || true)"
    if [[ -n "$status" ]] &&
      [[ "$(route_field "$status" "$port" state 2>/dev/null || true)" == "$expected" ]]; then
      return 0
    fi
    sleep 1
  done
  fail_with_diagnostics "route $port did not become $expected"
}

wait_for_route_absent() {
  local worktree="$1"
  local port="$2"
  local deadline=$((SECONDS + 60))
  while ((SECONDS < deadline)); do
    local status
    status="$("$wktbox" --path "$worktree" --json status 2>/dev/null || true)"
    if [[ -n "$status" ]] && route_absent "$status" "$port" 2>/dev/null; then
      return 0
    fi
    sleep 1
  done
  fail_with_diagnostics "route $port was not removed"
}

wait_for_http() {
  local worktree="$1"
  local url="$2"
  local expected="$3"
  local deadline=$((SECONDS + 60))
  while ((SECONDS < deadline)); do
    local body
    body="$("$wktbox" --path "$worktree" exec -- \
      curl --fail --silent --show-error --max-time 3 "$url" 2>/dev/null || true)"
    if [[ "$body" == *"$expected"* ]]; then
      return 0
    fi
    sleep 1
  done
  fail_with_diagnostics "$url did not return $expected"
}

mkdir -p "$repository" "$(dirname "$worktree_a")"
cp -R "$project_root/tests/fixtures/compose-project/." "$repository/"
rm -f "$repository/project.env" "$repository/.wktbox.yml"
if [[ -e "$repository/.wktbox.yml" ]]; then
  fail_with_diagnostics "scenario 1: fixture unexpectedly has .wktbox.yml"
fi

git -C "$repository" init -b main >/dev/null
git -C "$repository" config user.name "Wktbox Loopback E2E"
git -C "$repository" config user.email "wktbox-loopback-e2e@example.invalid"
git -C "$repository" add .
git -C "$repository" commit -m "fixture" >/dev/null
git -C "$repository" worktree add -b feature/a "$worktree_a" >/dev/null
git -C "$repository" worktree add -b feature/b "$worktree_b" >/dev/null

printf 'WKTBOX_TEST_MARKER=box-a\n' >"$env_a"
printf 'WKTBOX_TEST_MARKER=box-b\n' >"$env_b"
export WKTBOX_STATE_HOME="$state_root"

make -C "$project_root" build >/dev/null
docker build -q -f "$project_root/images/webtop/Dockerfile" \
  -t wktbox/webtop:dev "$project_root" >/dev/null

"$wktbox" --path "$worktree_a" --env-file "$env_a" up >/dev/null
box_a_created=true
"$wktbox" --path "$worktree_b" --env-file "$env_b" up >/dev/null
box_b_created=true

"$wktbox" --path "$worktree_a" --env-file "$env_a" compose -- \
  up -d --wait >"$test_root/compose-a.out" 2>"$test_root/compose-a.err"
"$wktbox" --path "$worktree_b" --env-file "$env_b" compose -- \
  up -d --wait >"$test_root/compose-b.out" 2>"$test_root/compose-b.err"

status_a="$("$wktbox" --path "$worktree_a" --json status)"
status_b="$("$wktbox" --path "$worktree_b" --json status)"
assert_json_status "$status_a" ready
assert_json_status "$status_b" ready
id_a="$(json_value "$status_a" id)"
id_b="$(json_value "$status_b" id)"
assert_nonempty_distinct "$id_a" "$id_b" "box IDs"

# Scenario 2: the inner daemon really publishes 5173:5173.
inner_ports="$("$wktbox" --path "$worktree_a" exec -- \
  docker inspect wktbox-fixture-frontend-1 \
  --format '{{json .NetworkSettings.Ports}}')"
[[ "$inner_ports" == *'"HostPort":"5173"'* ]] ||
  fail_with_diagnostics "scenario 2: 5173 publication missing"

# Scenario 3: HTTP is automatic on Webtop localhost.
http_headers="$("$wktbox" --path "$worktree_a" exec -- \
  curl --head --fail --silent --show-error http://localhost:5173/public/index.html)"
[[ "$http_headers" == *"200 OK"* ]] ||
  fail_with_diagnostics "scenario 3: localhost:5173 did not return HTTP 200"

# Scenario 4: differing host/container ports use the published host port.
wait_for_route "$worktree_a" 8000 listening
status_a="$("$wktbox" --path "$worktree_a" --json status)"
[[ "$(route_field "$status_a" 8000 target)" == "docker:8000" ]] ||
  fail_with_diagnostics "scenario 4: 8000 target is not docker:8000"
wait_for_http "$worktree_a" "http://localhost:8000" "box-a"
database_body="$("$wktbox" --path "$worktree_a" exec -- \
  curl --silent --max-time 3 telnet://localhost:5432)"
[[ "$database_body" == "box-a" ]] ||
  fail_with_diagnostics "raw TCP localhost:5432 failed"

# Chromium validates both normal HTTP and a bidirectional WebSocket upgrade.
chromium="$("$wktbox" --path "$worktree_a" exec -- \
  sh -c 'command -v chromium || command -v chromium-browser')"
browser_dom="$("$wktbox" --path "$worktree_a" exec -- \
  "$chromium" --headless --no-sandbox --disable-gpu \
  --virtual-time-budget=5000 --dump-dom \
  http://localhost:5173/public/loopback.html 2>/dev/null)"
[[ "$browser_dom" == *"wktbox fixture"* ]] ||
  fail_with_diagnostics "Chromium did not load localhost:5173"
[[ "$browser_dom" == *"wktbox-websocket-ok"* ]] ||
  fail_with_diagnostics "scenario 9: WebSocket echo failed in Chromium"

# Scenario 5: an event adds a route for a late container.
"$wktbox" --path "$worktree_a" exec -- \
  docker run -d --name wktbox-late-loopback \
  --publish 19090:8080 python:3.14-alpine \
  python -m http.server 8080 --bind 0.0.0.0 >/dev/null
wait_for_route "$worktree_a" 19090 listening
wait_for_http "$worktree_a" "http://localhost:19090" "Directory listing"

# Scenario 6: removal closes the route without restarting the sidecar.
"$wktbox" --path "$worktree_a" exec -- \
  docker rm -f wktbox-late-loopback >/dev/null
wait_for_route_absent "$worktree_a" 19090
if "$wktbox" --path "$worktree_a" exec -- \
  curl --fail --silent --connect-timeout 1 http://localhost:19090 >/dev/null 2>&1; then
  fail_with_diagnostics "scenario 6: removed route still accepts connections"
fi

# Scenario 10: sidecar restart performs a complete resync.
docker restart "wktbox-$id_a-loopback-1" >/dev/null
wait_for_route "$worktree_a" 5173 listening
wait_for_http "$worktree_a" "http://localhost:5173/public/index.html" "wktbox fixture"

# Scenario 8: equal localhost ports remain isolated in two Webtop namespaces.
wait_for_http "$worktree_a" "http://localhost:8000" "box-a"
wait_for_http "$worktree_b" "http://localhost:8000" "box-b"

# Scenario 11: Webtop's own high port conflicts without affecting other routes.
"$wktbox" --path "$worktree_a" exec -- \
  docker run -d --name wktbox-port-conflict \
  --publish 61000:8080 python:3.14-alpine \
  python -m http.server 8080 --bind 0.0.0.0 >/dev/null
wait_for_route "$worktree_a" 61000 conflict
wait_for_route "$worktree_a" 5173 listening
"$wktbox" --path "$worktree_a" exec -- \
  docker rm -f wktbox-port-conflict >/dev/null
wait_for_route_absent "$worktree_a" 61000

# Scenario 7: occupy the old DinD IP, recreate it, and prove fresh DNS lookup.
old_ip="$(docker inspect "wktbox-$id_a-docker-1" \
  --format "{{(index .NetworkSettings.Networks \"wktbox-${id_a}_default\").IPAddress}}")"
docker rm -f "wktbox-$id_a-docker-1" >/dev/null
blocker="wktbox-$id_a-ip-blocker"
docker run -d --name "$blocker" \
  --network "wktbox-${id_a}_default" docker:29.5.0-cli \
  sleep 300 >/dev/null
"$wktbox" --path "$worktree_a" --env-file "$env_a" up >/dev/null
new_ip="$(docker inspect "wktbox-$id_a-docker-1" \
  --format "{{(index .NetworkSettings.Networks \"wktbox-${id_a}_default\").IPAddress}}")"
[[ -n "$old_ip" && -n "$new_ip" && "$old_ip" != "$new_ip" ]] ||
  fail_with_diagnostics "scenario 7: DinD IP did not change ($old_ip -> $new_ip)"
docker rm -f "$blocker" >/dev/null
blocker=""
"$wktbox" --path "$worktree_a" --env-file "$env_a" compose -- \
  up -d --wait >/dev/null
wait_for_route "$worktree_a" 8000 listening
wait_for_http "$worktree_a" "http://localhost:8000" "box-a"

# Scenarios 12 and 13: no inner app port and no host Docker socket escape.
outer_ports="$(docker ps \
  --filter "label=io.wktbox.box-id=$id_a" \
  --format '{{.Ports}}')"
for forbidden in 5173 8000 5432 19090; do
  if [[ "$outer_ports" == *"->$forbidden/tcp"* ]]; then
    fail_with_diagnostics "scenario 12: inner port $forbidden leaked to host"
  fi
done
outer_containers="$(docker ps -aq --filter "label=io.wktbox.box-id=$id_a")"
for container in $outer_containers; do
  mounts="$(docker inspect "$container" --format '{{json .Mounts}}')"
  if [[ "$mounts" == *'"/var/run/docker.sock"'* ]]; then
    fail_with_diagnostics "scenario 13: host Docker socket is mounted"
  fi
done

# Ordered shutdown must stop the sidecar process and its shared listeners.
"$wktbox" --path "$worktree_a" stop >/dev/null
loopback_running="$(docker inspect "wktbox-$id_a-loopback-1" \
  --format '{{.State.Running}}')"
[[ "$loopback_running" == "false" ]] ||
  fail_with_diagnostics "loopback sidecar remained running after stop"

printf 'wktbox automatic loopback E2E passed: all 13 scenarios and Chromium\n'
