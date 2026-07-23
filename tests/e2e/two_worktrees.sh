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

mkdir -p "$repository" "$(dirname "$worktree_a")"
cp -R "$project_root/tests/fixtures/compose-project/." "$repository/"
rm -f "$repository/project.env"
printf '%s\n' \
  'version: 1' \
  'gateway:' \
  '  enabled: true' \
  '  routes:' \
  '    frontend:' \
  '      port: 5173' \
  '    api:' \
  '      port: 8000' >"$repository/.wktbox.yml"

git -C "$repository" init -b main >/dev/null
git -C "$repository" config user.name "Wktbox E2E"
git -C "$repository" config user.email "wktbox-e2e@example.invalid"
git -C "$repository" add .
git -C "$repository" commit -m "fixture" >/dev/null
git -C "$repository" worktree add -b feature/a "$worktree_a" >/dev/null
git -C "$repository" worktree add -b feature/b "$worktree_b" >/dev/null

printf 'WKTBOX_TEST_MARKER=box-a\n' >"$env_a"
printf 'WKTBOX_TEST_MARKER=box-b\n' >"$env_b"
export WKTBOX_STATE_HOME="$state_root"

make -C "$project_root" build >/dev/null
docker build -q -t wktbox/webtop:dev "$project_root/images/webtop" >/dev/null
docker build -q -t wktbox/gateway:dev "$project_root/images/gateway" >/dev/null

cold_started="$SECONDS"
"$wktbox" --path "$worktree_a" --env-file "$env_a" up \
  >"$test_root/up-a.out" 2>"$test_root/up-a.err" &
up_a_pid="$!"
"$wktbox" --path "$worktree_b" --env-file "$env_b" up \
  >"$test_root/up-b.out" 2>"$test_root/up-b.err" &
up_b_pid="$!"
if ! wait "$up_a_pid"; then
  cat "$test_root/up-a.err" >&2
  exit 1
fi
box_a_created=true
if ! wait "$up_b_pid"; then
  cat "$test_root/up-b.err" >&2
  exit 1
fi
box_b_created=true
cold_seconds="$((SECONDS - cold_started))"

status_a="$("$wktbox" --path "$worktree_a" --json status)"
status_b="$("$wktbox" --path "$worktree_b" --json status)"
assert_json_status "$status_a" ready
assert_json_status "$status_b" ready
id_a="$(json_value "$status_a" id)"
id_b="$(json_value "$status_b" id)"
port_a="$(json_value "$status_a" ports.webtopHttp)"
port_b="$(json_value "$status_b" ports.webtopHttp)"
gateway_port_a="$(json_value "$status_a" ports.gateway)"
assert_nonempty_distinct "$id_a" "$id_b" "box IDs"
assert_nonempty_distinct "$port_a" "$port_b" "port blocks"

"$wktbox" --path "$worktree_a" --env-file "$env_a" run -- \
  docker compose up -d --wait
"$wktbox" --path "$worktree_b" --env-file "$env_b" run -- \
  docker compose up -d --wait
"$wktbox" --path "$worktree_a" exec -- \
  docker compose --profile test run --rm e2e
"$wktbox" --path "$worktree_b" exec -- \
  docker compose --profile test run --rm e2e

ps_a="$("$wktbox" --path "$worktree_a" exec -- \
  docker ps --format '{{.ID}}' | sort)"
ps_b="$("$wktbox" --path "$worktree_b" exec -- \
  docker ps --format '{{.ID}}' | sort)"
assert_nonempty_distinct "$ps_a" "$ps_b" "inner container sets"

marker_a="$("$wktbox" --path "$worktree_a" exec -- \
  docker compose exec -T database cat /data/marker)"
marker_b="$("$wktbox" --path "$worktree_b" exec -- \
  docker compose exec -T database cat /data/marker)"
[[ "$marker_a" == "box-a" ]]
[[ "$marker_b" == "box-b" ]]
"$wktbox" --path "$worktree_a" exec -- \
  sh -c 'test "$(cat /workspace/.env)" = "WKTBOX_TEST_MARKER=box-a"'
"$wktbox" --path "$worktree_b" exec -- \
  sh -c 'test "$(cat /workspace/.env)" = "WKTBOX_TEST_MARKER=box-b"'
if "$wktbox" --path "$worktree_a" exec -- \
  sh -c 'printf "MUTATED=true\n" >>/workspace/.env' 2>/dev/null; then
  printf 'project env unexpectedly writable inside box A\n' >&2
  exit 1
fi
[[ "$(cat "$env_a")" == "WKTBOX_TEST_MARKER=box-a" ]]

"$wktbox" --path "$worktree_a" exec -- \
  sh -c 'test "$1" = "value with spaces" && test "$2" = "$3"' \
  sh 'value with spaces' '$(not-expanded)' '$(not-expanded)'
set +e
"$wktbox" --path "$worktree_a" exec -- sh -c 'exit 23'
child_exit="$?"
set -e
[[ "$child_exit" -eq 23 ]]

gateway_body="$(curl --fail --silent --show-error \
  -H "Host: frontend.$id_a.localhost" \
  "http://127.0.0.1:$gateway_port_a/public/index.html")"
[[ "$gateway_body" == *"wktbox fixture"* ]]

doctor_a="$("$wktbox" --path "$worktree_a" --json doctor)"
assert_doctor_check "$doctor_a" dind_tls pass

rm -f "$state_root/state.json"
recovered="$("$wktbox" --json list)"
recovered_count="$(printf '%s' "$recovered" | python3 -c \
  'import json,sys; print(len(json.load(sys.stdin)))')"
[[ "$recovered_count" -eq 2 ]]
assert_json_status "$("$wktbox" --path "$worktree_a" --json status)" ready
assert_json_status "$("$wktbox" --path "$worktree_b" --json status)" ready

"$wktbox" --path "$worktree_a" stop
assert_json_status "$("$wktbox" --path "$worktree_a" --json status)" stopped
set +e
stopped_error="$("$wktbox" --path "$worktree_a" exec -- pwd 2>&1)"
stopped_exit="$?"
set -e
[[ "$stopped_exit" -ne 0 ]]
[[ "$stopped_error" == *"wktbox up"* ]]
[[ "$stopped_error" == *"wktbox run --"* ]]

warm_started="$SECONDS"
"$wktbox" --path "$worktree_a" --env-file "$env_a" up
"$wktbox" --path "$worktree_a" --env-file "$env_a" run -- \
  docker compose up -d --wait
warm_seconds="$((SECONDS - warm_started))"
[[ "$("$wktbox" --path "$worktree_a" exec -- \
  docker compose exec -T database cat /data/marker)" == "box-a" ]]

docker_bytes_a="$(docker exec "wktbox-$id_a-docker-1" \
  du -sb /var/lib/docker | awk '{print $1}')"
docker_bytes_b="$(docker exec "wktbox-$id_b-docker-1" \
  du -sb /var/lib/docker | awk '{print $1}')"
stats_a="$(docker stats --no-stream \
  --format '{{.CPUPerc}}|{{.MemUsage}}' "wktbox-$id_a-webtop-1")"
stats_b="$(docker stats --no-stream \
  --format '{{.CPUPerc}}|{{.MemUsage}}' "wktbox-$id_b-webtop-1")"
metrics_file="${WKTBOX_METRICS_FILE:-$test_root/metrics.json}"
python3 - "$metrics_file" \
  "$cold_seconds" "$warm_seconds" \
  "$docker_bytes_a" "$docker_bytes_b" \
  "$stats_a" "$stats_b" <<'PY'
import json
import sys

(
    destination,
    cold_seconds,
    warm_seconds,
    docker_bytes_a,
    docker_bytes_b,
    stats_a,
    stats_b,
) = sys.argv[1:]
metrics = {
    "coldStartupSeconds": int(cold_seconds),
    "warmStartupSeconds": int(warm_seconds),
    "boxes": [
        {
            "name": "a",
            "dockerDataBytes": int(docker_bytes_a),
            "cpu": stats_a.split("|", 1)[0],
            "memory": stats_a.split("|", 1)[1],
        },
        {
            "name": "b",
            "dockerDataBytes": int(docker_bytes_b),
            "cpu": stats_b.split("|", 1)[0],
            "memory": stats_b.split("|", 1)[1],
        },
    ],
}
with open(destination, "w", encoding="utf-8") as output:
    json.dump(metrics, output, indent=2)
    output.write("\n")
PY

"$wktbox" --path "$worktree_a" destroy --force
box_a_created=false
assert_json_status "$("$wktbox" --path "$worktree_b" --json status)" ready
"$wktbox" --path "$worktree_b" exec -- \
  docker compose --profile test run --rm e2e
"$wktbox" --path "$worktree_b" destroy --force
box_b_created=false

printf 'wktbox E2E passed: two linked worktrees remained isolated\n'
