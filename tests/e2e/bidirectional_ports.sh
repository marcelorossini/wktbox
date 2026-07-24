#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
test_root="$(mktemp -d)"
workspace_a="$test_root/workspace a"
workspace_b="$test_root/workspace b"
state_root="$test_root/wktbox state"
wktbox="$project_root/bin/wktbox"
box_a_created=false
box_b_created=false
id_a=""
id_b=""
server_pids=()

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
  local pid
  for pid in "${server_pids[@]}"; do
    kill "$pid" >/dev/null 2>&1 || true
    wait "$pid" >/dev/null 2>&1 || true
  done
  if [[ "$box_a_created" == true && -d "$workspace_a" ]]; then
    "$wktbox" --path "$workspace_a" destroy --force >/dev/null 2>&1 || true
  fi
  if [[ "$box_b_created" == true && -d "$workspace_b" ]]; then
    "$wktbox" --path "$workspace_b" destroy --force >/dev/null 2>&1 || true
  fi
  cleanup_managed_for_path "$workspace_a"
  cleanup_managed_for_path "$workspace_b"
  rm -rf "$test_root"
}
trap cleanup EXIT

fail_with_diagnostics() {
  local description="$1"
  printf 'bidirectional port E2E failed: %s\n' "$description" >&2
  if [[ -n "$id_a" ]]; then
    "$wktbox" --path "$workspace_a" --json status >&2 || true
    docker logs "wktbox-$id_a-docker-1" >&2 || true
    docker logs "wktbox-$id_a-loopback-1" >&2 || true
    docker logs "wktbox-$id_a-portbridge-1" >&2 || true
  fi
  if [[ -n "$id_b" ]]; then
    "$wktbox" --path "$workspace_b" --json status >&2 || true
  fi
  exit 1
}

start_host_http() {
  local port="$1"
  local marker="$2"
  local directory="$test_root/host-http-$port"
  mkdir -p "$directory"
  printf '%s\n' "$marker" >"$directory/marker.txt"
  python3 -m http.server "$port" \
    --bind 127.0.0.1 \
    --directory "$directory" \
    >"$test_root/host-http-$port.log" 2>&1 &
  server_pids+=("$!")
}

start_host_raw_echo() {
  local port="$1"
  python3 -c '
import socket
import sys

server = socket.socket()
server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
server.bind(("127.0.0.1", int(sys.argv[1])))
server.listen()
while True:
    connection, _ = server.accept()
    with connection:
        data = connection.recv(65536)
        connection.sendall(b"host-raw:" + data)
' "$port" >"$test_root/host-raw-$port.log" 2>&1 &
  server_pids+=("$!")
}

wait_host_http() {
  local port="$1"
  local expected="$2"
  local deadline=$((SECONDS + 30))
  while ((SECONDS < deadline)); do
    local body
    body="$(curl --fail --silent --max-time 2 \
      "http://127.0.0.1:$port/marker.txt" 2>/dev/null || true)"
    if [[ "$body" == *"$expected"* ]]; then
      return 0
    fi
    sleep 1
  done
  fail_with_diagnostics "host HTTP $port did not return $expected"
}

inner_http_body() {
  local workspace="$1"
  local container="$2"
  local port="$3"
  "$wktbox" --path "$workspace" exec -- \
    docker exec "$container" python -c \
    'import sys,urllib.request; print(urllib.request.urlopen("http://localhost:"+sys.argv[1]+"/marker.txt", timeout=2).read().decode())' \
    "$port"
}

wait_inner_http() {
  local workspace="$1"
  local container="$2"
  local port="$3"
  local expected="$4"
  local deadline=$((SECONDS + 60))
  while ((SECONDS < deadline)); do
    local body
    body="$(inner_http_body "$workspace" "$container" "$port" 2>/dev/null || true)"
    if [[ "$body" == *"$expected"* ]]; then
      return 0
    fi
    sleep 1
  done
  fail_with_diagnostics "$container localhost:$port did not return $expected"
}

inner_raw_body() {
  local workspace="$1"
  local container="$2"
  local port="$3"
  "$wktbox" --path "$workspace" exec -- \
    docker exec "$container" python -c '
import socket
import sys

connection = socket.create_connection(("localhost", int(sys.argv[1])), timeout=2)
connection.sendall(b"ping")
connection.shutdown(socket.SHUT_WR)
print(connection.recv(65536).decode())
' "$port"
}

wait_inner_raw() {
  local workspace="$1"
  local container="$2"
  local port="$3"
  local deadline=$((SECONDS + 60))
  while ((SECONDS < deadline)); do
    if [[ "$(inner_raw_body "$workspace" "$container" "$port" 2>/dev/null || true)" == "host-raw:ping" ]]; then
      return 0
    fi
    sleep 1
  done
  fail_with_diagnostics "$container localhost:$port did not relay raw TCP"
}

host_raw_body() {
  local port="$1"
  python3 -c '
import socket
import sys

connection = socket.create_connection(("127.0.0.1", int(sys.argv[1])), timeout=2)
connection.sendall(b"ping")
connection.shutdown(socket.SHUT_WR)
print(connection.recv(65536).decode())
' "$port"
}

wait_published_http() {
  local port="$1"
  local expected="$2"
  local deadline=$((SECONDS + 60))
  while ((SECONDS < deadline)); do
    local body
    body="$(curl --fail --silent --max-time 2 \
      "http://127.0.0.1:$port/marker.txt" 2>/dev/null || true)"
    if [[ "$body" == *"$expected"* ]]; then
      return 0
    fi
    sleep 1
  done
  fail_with_diagnostics "published host port $port did not return $expected"
}

wait_published_raw() {
  local port="$1"
  local expected="$2"
  local deadline=$((SECONDS + 60))
  while ((SECONDS < deadline)); do
    if [[ "$(host_raw_body "$port" 2>/dev/null || true)" == "$expected" ]]; then
      return 0
    fi
    sleep 1
  done
  fail_with_diagnostics "published raw port $port did not return $expected"
}

canonical_json() {
  python3 -c '
import json
import sys
print(json.dumps(json.load(sys.stdin), sort_keys=True, separators=(",", ":")))
'
}

json_has_mapping() {
  local document="$1"
  local name="$2"
  printf '%s' "$document" | python3 -c '
import json
import sys

status = json.load(sys.stdin)
mappings = status if isinstance(status, list) else status.get("portMappings", [])
name = sys.argv[1]
if not any(mapping.get("name") == name for mapping in mappings):
    raise SystemExit(1)
' "$name"
}

status_has_warning() {
  local document="$1"
  local code="$2"
  local source="$3"
  printf '%s' "$document" | python3 -c '
import json
import sys

status = json.load(sys.stdin)
code, source = sys.argv[1:]
warnings = status.get("loopback", {}).get("warnings", [])
if not any(
    warning.get("code") == code and warning.get("source") == source
    for warning in warnings
):
    raise SystemExit(1)
' "$code" "$source"
}

assert_inner_port_closed() {
  local workspace="$1"
  local container="$2"
  local port="$3"
  if "$wktbox" --path "$workspace" exec -- \
    docker exec "$container" python -c '
import socket
import sys

connection = socket.create_connection(("localhost", int(sys.argv[1])), timeout=1)
connection.close()
' "$port" >/dev/null 2>&1; then
    fail_with_diagnostics "$container localhost:$port remained open"
  fi
}

wait_host_port_open() {
  local port="$1"
  local deadline=$((SECONDS + 30))
  while ((SECONDS < deadline)); do
    if python3 -c '
import socket
import sys

connection = socket.create_connection(("127.0.0.1", int(sys.argv[1])), timeout=1)
connection.close()
' "$port" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  fail_with_diagnostics "host port $port did not open"
}

wait_host_port_closed() {
  local port="$1"
  local deadline=$((SECONDS + 30))
  while ((SECONDS < deadline)); do
    if ! python3 -c '
import socket
import sys

connection = socket.create_connection(("127.0.0.1", int(sys.argv[1])), timeout=1)
connection.close()
' "$port" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  fail_with_diagnostics "host port $port remained open"
}

read -r \
  host_http \
  host_raw \
  host_same \
  host_conflict \
  host_batch_free \
  host_temp \
  publish_http \
  publish_raw \
  publish_conflict \
  publish_batch_free \
  publish_temp \
  publish_http_b < <(python3 -c '
import socket

sockets = []
for _ in range(12):
    listener = socket.socket()
    listener.bind(("127.0.0.1", 0))
    sockets.append(listener)
print(" ".join(str(listener.getsockname()[1]) for listener in sockets))
')

mkdir -p "$workspace_a" "$workspace_b"
printf 'box-a\n' >"$workspace_a/marker.txt"
printf 'box-b\n' >"$workspace_b/marker.txt"
export WKTBOX_STATE_HOME="$state_root"

make -C "$project_root" build >/dev/null
docker build -q -f "$project_root/images/webtop/Dockerfile" \
  -t wktbox/webtop:dev "$project_root" >/dev/null

start_host_http "$host_http" "host-http"
start_host_raw_echo "$host_raw"
start_host_http "$host_same" "host-same-port"
start_host_http "$host_conflict" "host-conflict"
start_host_http "$host_batch_free" "host-batch-free"
start_host_http "$host_temp" "host-temp"
wait_host_http "$host_http" "host-http"
wait_host_http "$host_same" "host-same-port"
wait_host_http "$host_conflict" "host-conflict"

"$wktbox" --path "$workspace_a" up >/dev/null
box_a_created=true
status_a="$("$wktbox" --path "$workspace_a" --json status)"
id_a="$(printf '%s' "$status_a" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"

"$wktbox" --path "$workspace_a" exec -- \
  docker run -d --restart unless-stopped --name workload-a \
  python:3.14-alpine sleep 900 >/dev/null
"$wktbox" --path "$workspace_a" exec -- \
  docker run -d --restart unless-stopped --name workload-b \
  python:3.14-alpine sleep 900 >/dev/null

# One atomic command imports multiple host services with remapped localhost ports.
"$wktbox" --path "$workspace_a" port import \
  --map "host-http=127.0.0.1:$host_http:31081" \
  --map "host-raw=127.0.0.1:$host_raw:31082" >/dev/null
wait_inner_http "$workspace_a" workload-a 31081 host-http
wait_inner_http "$workspace_a" workload-b 31081 host-http
wait_inner_raw "$workspace_a" workload-a 31082
wait_inner_raw "$workspace_a" workload-b 31082

# Positional syntax preserves the same port number from host to every workload.
"$wktbox" --path "$workspace_a" port import "$host_same" >/dev/null
wait_inner_http "$workspace_a" workload-a "$host_same" host-same-port
wait_inner_http "$workspace_a" workload-b "$host_same" host-same-port

# An existing workload listener rejects the entire import batch without residue.
"$wktbox" --path "$workspace_a" exec -- \
  docker run -d --name existing-conflict python:3.14-alpine \
  python -m http.server 31083 --bind 127.0.0.1 >/dev/null
before_imports="$("$wktbox" --path "$workspace_a" --json port list | canonical_json)"
if "$wktbox" --path "$workspace_a" port import \
  --map "batch-free=127.0.0.1:$host_batch_free:31084" \
  --map "batch-conflict=127.0.0.1:$host_conflict:31083" \
  >"$test_root/import-conflict.out" 2>"$test_root/import-conflict.err"; then
  fail_with_diagnostics "existing workload conflict unexpectedly succeeded"
fi
grep -q 'existing-conflict' "$test_root/import-conflict.err" ||
  fail_with_diagnostics "import conflict did not identify the workload"
after_imports="$("$wktbox" --path "$workspace_a" --json port list | canonical_json)"
[[ "$before_imports" == "$after_imports" ]] ||
  fail_with_diagnostics "failed import batch changed desired state"
assert_inner_port_closed "$workspace_a" workload-a 31084
"$wktbox" --path "$workspace_a" exec -- \
  docker rm -f existing-conflict >/dev/null

# A temporary import can be removed and closes every workload listener.
docker exec "wktbox-$id_a-loopback-1" sh -c \
  'flock -x /run/wktbox-port-transaction/transaction.lock sh -c "echo locked; sleep 3"' \
  >"$test_root/transaction-lock.out" &
transaction_holder_pid="$!"
server_pids+=("$transaction_holder_pid")
transaction_lock_deadline=$((SECONDS + 10))
while ((SECONDS < transaction_lock_deadline)); do
  if grep -q locked "$test_root/transaction-lock.out"; then
    break
  fi
  sleep 1
done
grep -q locked "$test_root/transaction-lock.out" ||
  fail_with_diagnostics "sidecar could not acquire the shared transaction lock"
transaction_started=$SECONDS
"$wktbox" --path "$workspace_a" port import \
  --map "temporary=127.0.0.1:$host_temp:31085" >/dev/null
wait "$transaction_holder_pid"
transaction_elapsed=$((SECONDS - transaction_started))
((transaction_elapsed >= 2)) ||
  fail_with_diagnostics "host transaction ignored the sidecar-held lock"
wait_inner_http "$workspace_a" workload-a 31085 host-temp
"$wktbox" --path "$workspace_a" port remove temporary >/dev/null
assert_inner_port_closed "$workspace_a" workload-a 31085

# A live Docker start event detects a future workload conflict, stops the
# workload, and leaves a durable structured warning.
docker pause "wktbox-$id_a-loopback-1" >/dev/null
docker exec "wktbox-$id_a-webtop-1" \
  docker run -d --name future-conflict python:3.14-alpine \
  python -u -c '
import socket

server = socket.socket()
server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
server.bind(("127.0.0.1", 31081))
server.listen()
print("READY", flush=True)
while True:
    connection, _ = server.accept()
    connection.close()
' >/dev/null
future_ready_deadline=$((SECONDS + 30))
future_bound=false
while ((SECONDS < future_ready_deadline)); do
  future_running="$(docker exec "wktbox-$id_a-webtop-1" \
    docker inspect future-conflict --format '{{.State.Running}}' 2>/dev/null || true)"
  if [[ "$future_running" == "true" ]] &&
    docker exec "wktbox-$id_a-webtop-1" \
      docker logs future-conflict 2>/dev/null | grep -q READY; then
    future_bound=true
    break
  fi
  sleep 1
done
if [[ "$future_bound" != true ]]; then
  docker unpause "wktbox-$id_a-loopback-1" >/dev/null 2>&1 || true
  fail_with_diagnostics "future conflict did not occupy localhost:31081"
fi
docker unpause "wktbox-$id_a-loopback-1" >/dev/null
future_deadline=$((SECONDS + 60))
future_stopped=false
while ((SECONDS < future_deadline)); do
  future_running="$(docker exec "wktbox-$id_a-webtop-1" \
    docker inspect future-conflict --format '{{.State.Running}}' 2>/dev/null || true)"
  status_a="$("$wktbox" --path "$workspace_a" --json status 2>/dev/null || true)"
  if [[ "$future_running" == "false" ]] &&
    [[ -n "$status_a" ]] &&
    status_has_warning "$status_a" port_import_conflict future-conflict 2>/dev/null; then
    future_stopped=true
    break
  fi
  sleep 1
done
[[ "$future_stopped" == true ]] ||
  fail_with_diagnostics "future conflicting workload was not stopped and reported"
docker exec "wktbox-$id_a-webtop-1" \
  docker rm future-conflict >/dev/null
wait_inner_http "$workspace_a" workload-a 31081 host-http

# Inner DinD publications remain private until explicitly published.
"$wktbox" --path "$workspace_a" exec -- \
  docker run -d --restart unless-stopped --name publish-http \
  --publish 28080:8080 \
  python:3.14-alpine sh -c \
  'mkdir -p /site; printf "box-a\n" >/site/marker.txt; exec python -m http.server 8080 --bind 0.0.0.0 --directory /site' \
  >/dev/null
"$wktbox" --path "$workspace_a" exec -- \
  docker run -d --restart unless-stopped --name publish-raw \
  --publish 29090:9090 \
  python:3.14-alpine python -c '
import socket

server = socket.socket()
server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
server.bind(("0.0.0.0", 9090))
server.listen()
while True:
    connection, _ = server.accept()
    with connection:
        data = connection.recv(65536)
        connection.sendall(b"box-a:" + data)
' >/dev/null

"$wktbox" --path "$workspace_a" port publish \
  --map "published-http=127.0.0.1:$publish_http:28080" \
  --map "published-raw=127.0.0.1:$publish_raw:29090" >/dev/null
wait_published_http "$publish_http" box-a
wait_published_raw "$publish_raw" box-a:ping

# A host bind conflict rejects the whole publication batch.
start_host_http "$publish_conflict" "host-blocker"
wait_host_http "$publish_conflict" host-blocker
before_publications="$("$wktbox" --path "$workspace_a" --json port list | canonical_json)"
if "$wktbox" --path "$workspace_a" port publish \
  --map "publish-free=127.0.0.1:$publish_batch_free:28080" \
  --map "publish-conflict=127.0.0.1:$publish_conflict:29090" \
  >"$test_root/publish-conflict.out" 2>"$test_root/publish-conflict.err"; then
  fail_with_diagnostics "host publication conflict unexpectedly succeeded"
fi
grep -q 'publish-conflict' "$test_root/publish-conflict.err" ||
  fail_with_diagnostics "publication conflict did not identify the mapping"
after_publications="$("$wktbox" --path "$workspace_a" --json port list | canonical_json)"
[[ "$before_publications" == "$after_publications" ]] ||
  fail_with_diagnostics "failed publication batch changed desired state"
if curl --fail --silent --max-time 1 \
  "http://127.0.0.1:$publish_batch_free/marker.txt" >/dev/null 2>&1; then
  fail_with_diagnostics "failed publication batch left a free listener"
fi

# Removing one publication closes only that listener.
"$wktbox" --path "$workspace_a" port publish \
  --map "publish-temporary=127.0.0.1:$publish_temp:28080" >/dev/null
wait_published_http "$publish_temp" box-a
"$wktbox" --path "$workspace_a" port remove publish-temporary >/dev/null
if curl --fail --silent --max-time 1 \
  "http://127.0.0.1:$publish_temp/marker.txt" >/dev/null 2>&1; then
  fail_with_diagnostics "removed publication still accepted connections"
fi
wait_published_http "$publish_http" box-a

# Stop closes runtime listeners but preserves desired state; restart restores
# both directions.
relay_port="$(printf '%s' "$status_a" | python3 -c '
import json
import sys

print(json.load(sys.stdin)["ports"]["webtopHttp"] + 4)
')"
wait_host_port_open "$relay_port"
"$wktbox" --path "$workspace_a" stop >/dev/null
if curl --fail --silent --max-time 1 \
  "http://127.0.0.1:$publish_http/marker.txt" >/dev/null 2>&1; then
  fail_with_diagnostics "publication remained open after stop"
fi
wait_host_port_closed "$relay_port"
stopped_status="$("$wktbox" --path "$workspace_a" --json status)"
for mapping in host-http host-raw "import-$host_same" published-http published-raw; do
  json_has_mapping "$stopped_status" "$mapping" ||
    fail_with_diagnostics "stop lost desired mapping $mapping"
done
if ! "$wktbox" --path "$workspace_a" restart >/dev/null; then
  fail_with_diagnostics "box A restart failed"
fi
wait_inner_http "$workspace_a" workload-a 31081 host-http
wait_inner_raw "$workspace_a" workload-b 31082
wait_published_http "$publish_http" box-a
wait_published_raw "$publish_raw" box-a:ping

# A second box may reuse the same workload-local and DinD ports without
# cross-talk.
"$wktbox" --path "$workspace_b" up >/dev/null
box_b_created=true
status_b="$("$wktbox" --path "$workspace_b" --json status)"
id_b="$(printf '%s' "$status_b" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
"$wktbox" --path "$workspace_b" exec -- \
  docker run -d --name workload-b python:3.14-alpine sleep 900 >/dev/null
"$wktbox" --path "$workspace_b" port import \
  --map "host-http=127.0.0.1:$host_http:31081" >/dev/null
wait_inner_http "$workspace_b" workload-b 31081 host-http
"$wktbox" --path "$workspace_b" exec -- \
  docker run -d --name publish-http --publish 28080:8080 \
  python:3.14-alpine sh -c \
  'mkdir -p /site; printf "box-b\n" >/site/marker.txt; exec python -m http.server 8080 --bind 0.0.0.0 --directory /site' \
  >/dev/null
"$wktbox" --path "$workspace_b" port publish \
  --map "published-http=127.0.0.1:$publish_http_b:28080" >/dev/null
wait_published_http "$publish_http_b" box-b
wait_published_http "$publish_http" box-a

"$wktbox" --path "$workspace_a" port remove host-http >/dev/null
assert_inner_port_closed "$workspace_a" workload-a 31081
wait_inner_http "$workspace_b" workload-b 31081 host-http

# No unrequested DinD publication appears on the real host, and credentials do
# not leak into workload mounts or public JSON.
outer_ports="$(docker ps \
  --filter "label=io.wktbox.box-id=$id_a" \
  --format '{{.Ports}}')"
for private_port in 28080 29090; do
  if [[ "$outer_ports" == *":$private_port->"* ]]; then
    fail_with_diagnostics "inner DinD port $private_port leaked to the host"
  fi
done
workload_mounts="$("$wktbox" --path "$workspace_a" exec -- \
  docker inspect workload-a --format '{{json .Mounts}}')"
[[ "$workload_mounts" != *"port-relay"* ]] ||
  fail_with_diagnostics "relay credentials leaked into a workload"
public_json="$("$wktbox" --path "$workspace_a" --json port list)"
for secret_field in token pid relay; do
  if [[ "${public_json,,}" == *"$secret_field"* ]]; then
    fail_with_diagnostics "public JSON leaked $secret_field"
  fi
done

printf 'wktbox bidirectional port E2E passed: imports, publications, conflicts, batches, lifecycle, and isolation\n'
