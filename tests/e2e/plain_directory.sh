#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
source "$project_root/tests/e2e/assert-isolation.sh"

test_root="$(mktemp -d)"
workspace="$test_root/plain workspace"
state_root="$test_root/wktbox state"
wktbox="$project_root/bin/wktbox"
box_created=false

cleanup_managed_for_path() {
  local selected_path="$1"
  local projects
  projects="$(docker ps -a \
    --filter 'label=io.wktbox.managed=true' \
    --filter "label=io.wktbox.worktree=$selected_path" \
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
  if [[ "$box_created" == true && -d "$workspace" ]]; then
    "$wktbox" --path "$workspace" destroy --force >/dev/null 2>&1 || true
  fi
  cleanup_managed_for_path "$workspace"
  rm -rf "$test_root"
}
trap cleanup EXIT

mkdir -p "$workspace"
printf 'plain-directory-e2e\n' >"$workspace/marker.txt"
printf '%s\n' \
  'version: 1' \
  'git:' \
  '  mode: mounted' >"$workspace/.wktbox.yml"
if [[ -e "$workspace/.git" ]]; then
  printf 'plain-directory fixture unexpectedly contains .git\n' >&2
  exit 1
fi

export WKTBOX_STATE_HOME="$state_root"

make -C "$project_root" build >/dev/null
docker build -q -f "$project_root/images/webtop/Dockerfile" \
  -t wktbox/webtop:dev "$project_root" >/dev/null

doctor="$("$wktbox" --path "$workspace" --json doctor)"
[[ "$(json_value "$doctor" ok)" == "true" ]]
assert_doctor_check "$doctor" bind_mount pass
assert_doctor_check "$doctor" worktree warn
assert_doctor_check "$doctor" git_bridge warn

"$wktbox" --path "$workspace" up >/dev/null
box_created=true
assert_json_status "$("$wktbox" --path "$workspace" --json status)" ready
"$wktbox" --path "$workspace" run -- \
  test -f /workspace/marker.txt
"$wktbox" --path "$workspace" exec -- \
  grep -Fx plain-directory-e2e /workspace/marker.txt >/dev/null

if [[ -e "$workspace/.git" ]]; then
  printf 'Wktbox unexpectedly initialized Git in the selected workspace\n' >&2
  exit 1
fi

"$wktbox" --path "$workspace" destroy --force >/dev/null
box_created=false

printf 'wktbox E2E passed: plain directory ran without Git initialization\n'
