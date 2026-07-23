#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
test_root="$(mktemp -d)"
project_a="wktbox-spike-a"
project_b="wktbox-spike-b"
env_a="$test_root/a.sandbox.env"
env_b="$test_root/b.sandbox.env"
worktree_a="$test_root/worktree a"
worktree_b="$test_root/worktree b"

compose() {
  local project="$1"
  local env_file="$2"
  shift 2
  docker compose \
    -p "$project" \
    --env-file "$env_file" \
    -f "$project_root/assets/sandbox.compose.yml" \
    -f "$project_root/tests/fixtures/project-env.override.yml" \
    "$@"
}

cleanup() {
  compose "$project_a" "$env_a" down --volumes --remove-orphans >/dev/null 2>&1 || true
  compose "$project_b" "$env_b" down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$test_root"
}
trap cleanup EXIT

cp -R "$project_root/tests/fixtures/compose-project" "$worktree_a"
cp -R "$project_root/tests/fixtures/compose-project" "$worktree_b"
printf 'WKTBOX_TEST_MARKER=box-a\n' >"$test_root/project-a.env"
printf 'WKTBOX_TEST_MARKER=box-b\n' >"$test_root/project-b.env"
printf 'server { listen 8080 default_server; return 404; }\n' >"$test_root/gateway.conf"

write_sandbox_env() {
  local destination="$1"
  local worktree="$2"
  local project_env="$3"
  local port_base="$4"

  {
    printf 'WKTBOX_ID=%s\n' "$(basename "$destination" .sandbox.env)"
    printf 'WKTBOX_VERSION=dev\n'
    printf 'WORKTREE_PATH=%s\n' "$worktree"
    printf 'PROJECT_ENV_PATH=%s\n' "$project_env"
    printf 'PORT_HTTP=%s\n' "$port_base"
    printf 'PORT_HTTPS=%s\n' "$((port_base + 1))"
    printf 'PORT_SSH=%s\n' "$((port_base + 2))"
    printf 'PORT_GATEWAY=%s\n' "$((port_base + 3))"
    printf 'GATEWAY_CONFIG_PATH=%s\n' "$test_root/gateway.conf"
    printf 'TZ=UTC\n'
    printf 'PUID=1000\n'
    printf 'PGID=1000\n'
    printf 'SHM_SIZE=1gb\n'
    printf 'WKTBOX_DIND_IMAGE=docker:29.5.0-dind\n'
    printf 'WKTBOX_GATEWAY_IMAGE=wktbox/gateway:dev\n'
    printf 'WKTBOX_WEBTOP_IMAGE=wktbox/webtop:dev\n'
  } >"$destination"
}

write_sandbox_env "$env_a" "$worktree_a" "$test_root/project-a.env" 23100
write_sandbox_env "$env_b" "$worktree_b" "$test_root/project-b.env" 23110

docker build -f "$project_root/images/webtop/Dockerfile" \
  -t wktbox/webtop:dev "$project_root"

startup_started="$SECONDS"
compose "$project_a" "$env_a" up -d --wait
compose "$project_b" "$env_b" up -d --wait

inside() {
  local project="$1"
  local env_file="$2"
  shift 2
  compose "$project" "$env_file" exec -T webtop "$@"
}

inside "$project_a" "$env_a" docker compose up -d --wait
inside "$project_b" "$env_b" docker compose up -d --wait
inside "$project_a" "$env_a" docker compose run --rm e2e
inside "$project_b" "$env_b" docker compose run --rm e2e
startup_seconds="$((SECONDS - startup_started))"

ps_a="$(inside "$project_a" "$env_a" docker ps --format '{{.ID}}' | sort)"
ps_b="$(inside "$project_b" "$env_b" docker ps --format '{{.ID}}' | sort)"
test -n "$ps_a"
test -n "$ps_b"
test "$ps_a" != "$ps_b"
test "$(inside "$project_a" "$env_a" docker compose exec -T database cat /data/marker)" = "box-a"
test "$(inside "$project_b" "$env_b" docker compose exec -T database cat /data/marker)" = "box-b"
inside "$project_a" "$env_a" sh -c 'test "$(cat /workspace/.env)" = "WKTBOX_TEST_MARKER=box-a"'
inside "$project_b" "$env_b" sh -c 'test "$(cat /workspace/.env)" = "WKTBOX_TEST_MARKER=box-b"'
compose "$project_a" "$env_a" exec -T docker sh -c 'test "$(cat /workspace/.env)" = "WKTBOX_TEST_MARKER=box-a"'
compose "$project_b" "$env_b" exec -T docker sh -c 'test "$(cat /workspace/.env)" = "WKTBOX_TEST_MARKER=box-b"'
if inside "$project_a" "$env_a" sh -c 'printf "MUTATED=true\n" >>/workspace/.env' 2>/dev/null; then
  printf 'project env unexpectedly writable inside box-a\n' >&2
  exit 1
fi
test "$(cat "$test_root/project-a.env")" = "WKTBOX_TEST_MARKER=box-a"

docker_data_a_bytes="$(compose "$project_a" "$env_a" exec -T docker du -sb /var/lib/docker | awk '{print $1}')"
docker_data_b_bytes="$(compose "$project_b" "$env_b" exec -T docker du -sb /var/lib/docker | awk '{print $1}')"
printf 'metrics startup_seconds=%s docker_data_a_bytes=%s docker_data_b_bytes=%s\n' \
  "$startup_seconds" "$docker_data_a_bytes" "$docker_data_b_bytes"
docker stats --no-stream \
  --format 'metrics container={{.Name}} cpu={{.CPUPerc}} memory={{.MemUsage}}' \
  "$(compose "$project_a" "$env_a" ps -q docker)" \
  "$(compose "$project_a" "$env_a" ps -q webtop)" \
  "$(compose "$project_b" "$env_b" ps -q docker)" \
  "$(compose "$project_b" "$env_b" ps -q webtop)"

compose "$project_a" "$env_a" stop
compose "$project_a" "$env_a" start --wait
inside "$project_a" "$env_a" docker compose up -d --wait
test "$(inside "$project_a" "$env_a" docker compose exec -T database cat /data/marker)" = "box-a"

compose "$project_a" "$env_a" down --volumes --remove-orphans
inside "$project_b" "$env_b" docker compose run --rm e2e

printf 'spike passed: two isolated DinD boxes with identical internal ports\n'
