#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
test_root="$(mktemp -d)"
supervisor_pid=""

cleanup() {
  if [[ -n "$supervisor_pid" ]]; then
    kill "$supervisor_pid" >/dev/null 2>&1 || true
    wait "$supervisor_pid" >/dev/null 2>&1 || true
  fi
  rm -rf "$test_root"
}
trap cleanup EXIT

fake_browser="$test_root/fake-browser"
browser_args="$test_root/browser-args"
profile="$test_root/profile"

cat >"$fake_browser" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$@" >"$WKTBOX_CHROMIUM_ARGS_FILE"
SH
chmod +x "$fake_browser"

WKTBOX_CHROMIUM_BIN="$fake_browser" \
WKTBOX_CHROMIUM_ARGS_FILE="$browser_args" \
WKTBOX_CHROMIUM_PROFILE="$profile" \
  "$project_root/images/webtop/wrapped-chromium" \
  https://example.test

for expected in \
  "--remote-debugging-port=9222" \
  "--user-data-dir=$profile" \
  "https://example.test"; do
  grep -Fx -- "$expected" "$browser_args" >/dev/null
done
test -d "$profile"

fake_wrapper="$test_root/fake-wrapper"
restart_count="$test_root/restart-count"
child_pid_file="$test_root/child-pid"

cat >"$fake_wrapper" <<'SH'
#!/usr/bin/env bash
count=0
if [[ -f "$WKTBOX_RESTART_COUNT" ]]; then
  count="$(cat "$WKTBOX_RESTART_COUNT")"
fi
count="$((count + 1))"
printf '%s\n' "$count" >"$WKTBOX_RESTART_COUNT"
if ((count >= 2)); then
  printf '%s\n' "$$" >"$WKTBOX_CHILD_PID_FILE"
  exec sleep 30
fi
SH
chmod +x "$fake_wrapper"

WKTBOX_CHROMIUM_WRAPPER="$fake_wrapper" \
WKTBOX_CHROMIUM_RESTART_DELAY="0.05" \
WKTBOX_RESTART_COUNT="$restart_count" \
WKTBOX_CHILD_PID_FILE="$child_pid_file" \
  "$project_root/images/webtop/chromium-supervisor" &
supervisor_pid="$!"

deadline=$((SECONDS + 10))
while ((SECONDS < deadline)); do
  if [[ -f "$restart_count" ]] &&
    [[ "$(cat "$restart_count")" -ge 2 ]] &&
    [[ -f "$child_pid_file" ]]; then
    break
  fi
  sleep 0.05
done

test -f "$restart_count"
test "$(cat "$restart_count")" -ge 2
test -f "$child_pid_file"

child_pid="$(cat "$child_pid_file")"
kill "$supervisor_pid"
wait "$supervisor_pid"
supervisor_pid=""

if kill -0 "$child_pid" >/dev/null 2>&1; then
  printf 'supervisor left Chromium child %s running\n' "$child_pid" >&2
  exit 1
fi

desktop_file="$project_root/images/webtop/wktbox-chromium.desktop"
for expected in \
  "Type=Application" \
  "Exec=/usr/local/bin/wktbox-chromium-supervisor" \
  "OnlyShowIn=XFCE;" \
  "X-GNOME-Autostart-enabled=true"; do
  grep -Fx -- "$expected" "$desktop_file" >/dev/null
done

relay_service="$project_root/images/webtop/s6-rc.d/svc-wktbox-cdp-relay"
test -f "$relay_service/type"
test "$(cat "$relay_service/type")" = "longrun"
test -f "$relay_service/dependencies.d/init-services"
test -f \
  "$project_root/images/webtop/s6-rc.d/user/contents.d/svc-wktbox-cdp-relay"
for expected in \
  "TCP-LISTEN:9223,bind=0.0.0.0,reuseaddr,fork" \
  "TCP:127.0.0.1:9222"; do
  grep -F -- "$expected" "$relay_service/run" >/dev/null
done
grep -Eq '^[[:space:]]+socat([[:space:]\\]|$)' \
  "$project_root/images/webtop/Dockerfile"

printf 'Webtop graphical Chromium tests passed\n'
