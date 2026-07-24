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
connection_created=false
connection_selector=""
connection_network=""

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
  if [[ "$connection_created" == true && -n "$connection_selector" ]]; then
    "$wktbox" disconnect "$connection_selector" >/dev/null 2>&1 || true
  fi
  if [[ "$box_a_created" == true && -d "$worktree_a" ]]; then
    "$wktbox" --path "$worktree_a" destroy --force >/dev/null 2>&1 || true
  fi
  if [[ "$box_b_created" == true && -d "$worktree_b" ]]; then
    "$wktbox" --path "$worktree_b" destroy --force >/dev/null 2>&1 || true
  fi
  if [[ -n "$connection_network" ]]; then
    docker network rm "$connection_network" >/dev/null 2>&1 || true
  fi
  cleanup_managed_for_path "$worktree_a"
  cleanup_managed_for_path "$worktree_b"
  rm -rf "$test_root"
}
trap cleanup EXIT

fail_with_diagnostics() {
  local description="$1"
  printf 'cross-box connection E2E failed: %s\n' "$description" >&2
  "$wktbox" --json connections >&2 || true
  if [[ "$box_a_created" == true ]]; then
    "$wktbox" --path "$worktree_a" logs interconnect >&2 || true
  fi
  if [[ "$box_b_created" == true ]]; then
    "$wktbox" --path "$worktree_b" logs interconnect >&2 || true
  fi
  exit 1
}

peer_http() {
  local source="$1"
  local target_alias="$2"
  local expected="$3"
  local body
  body="$("$wktbox" --path "$source" exec -- \
    docker run --rm python:3.14-alpine \
    python -c \
    'import sys, urllib.request; print(urllib.request.urlopen("http://" + sys.argv[1] + ":8000", timeout=5).read().decode())' \
    "$target_alias")"
  [[ "$body" == "$expected" ]] ||
    fail_with_diagnostics "$source could not reach $target_alias:8000"
}

peer_tcp() {
  local source="$1"
  local target_alias="$2"
  local expected="$3"
  local body
  body="$("$wktbox" --path "$source" exec -- \
    docker run --rm python:3.14-alpine \
    python -c \
    'import socket, sys; connection=socket.create_connection((sys.argv[1],5432),5); print(connection.recv(1024).decode())' \
    "$target_alias")"
  [[ "$body" == "$expected" ]] ||
    fail_with_diagnostics "$source could not reach $target_alias:5432"
}

mkdir -p "$repository" "$(dirname "$worktree_a")"
cp -R "$project_root/tests/fixtures/compose-project/." "$repository/"
rm -f "$repository/project.env" "$repository/.wktbox.yml"

git -C "$repository" init -b main >/dev/null
git -C "$repository" config user.name "Wktbox Connection E2E"
git -C "$repository" config user.email "wktbox-connection-e2e@example.invalid"
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
  up -d --wait >/dev/null
"$wktbox" --path "$worktree_b" --env-file "$env_b" compose -- \
  up -d --wait >/dev/null

status_a="$("$wktbox" --path "$worktree_a" --json status)"
status_b="$("$wktbox" --path "$worktree_b" --json status)"
id_a="$(json_value "$status_a" id)"
id_b="$(json_value "$status_b" id)"
assert_nonempty_distinct "$id_a" "$id_b" "box IDs"
alias_a="$id_a.wktbox"
alias_b="$id_b.wktbox"

connection="$("$wktbox" --json connect \
  "${id_a:0:3}" "${id_b:0:3}" --name dev-stack)"
connection_selector="$(printf '%s' "$connection" | python3 -c \
  'import json,sys; print(json.load(sys.stdin)["id"])')"
connection_network="$(printf '%s' "$connection" | python3 -c \
  'import json,sys; print(json.load(sys.stdin)["network"])')"
connection_created=true

printf '%s' "$connection" | python3 -c '
import json
import sys

document = json.load(sys.stdin)
expected = set(sys.argv[1:])
actual = {member["alias"] for member in document["members"]}
if document["state"] != "ready" or actual != expected:
    raise SystemExit(f"unexpected topology: {document}")
' "$alias_a" "$alias_b"

human_topology="$("$wktbox" connections dev-stack)"
[[ "$human_topology" == *"$alias_a"* && "$human_topology" == *"$alias_b"* ]] ||
  fail_with_diagnostics "human topology omitted a member alias"

peer_http "$worktree_a" "$alias_b" box-b
peer_http "$worktree_b" "$alias_a" box-a
peer_tcp "$worktree_a" "$alias_b" box-b

"$wktbox" --path "$worktree_b" stop >/dev/null
degraded="$("$wktbox" --json connections dev-stack)"
[[ "$(printf '%s' "$degraded" | python3 -c \
  'import json,sys; print(json.load(sys.stdin)["state"])')" == "degraded" ]] ||
  fail_with_diagnostics "stopped member did not degrade the connection"

"$wktbox" --path "$worktree_b" --env-file "$env_b" up >/dev/null
"$wktbox" --path "$worktree_b" --env-file "$env_b" compose -- \
  up -d --wait >/dev/null
peer_http "$worktree_a" "$alias_b" box-b

"$wktbox" disconnect dev-stack >/dev/null
connection_created=false
[[ "$("$wktbox" --json connections)" == "[]" ]] ||
  fail_with_diagnostics "connection remained in persistent state"
if docker network inspect "$connection_network" >/dev/null 2>&1; then
  fail_with_diagnostics "shared Docker bridge remained after disconnect"
fi
if "$wktbox" --path "$worktree_a" exec -- \
  docker run --rm python:3.14-alpine \
  python -c \
  'import socket,sys; socket.create_connection((sys.argv[1],8000),2)' \
  "$alias_b" >/dev/null 2>&1; then
  fail_with_diagnostics "peer alias remained reachable after disconnect"
fi

printf 'wktbox cross-box connection E2E passed: DNS, ports, lifecycle, and cleanup\n'

