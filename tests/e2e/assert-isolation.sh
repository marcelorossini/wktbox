#!/usr/bin/env bash

json_value() {
  local document="$1"
  local path="$2"
  printf '%s' "$document" | python3 -c '
import json
import sys

value = json.load(sys.stdin)
for segment in sys.argv[1].split("."):
    value = value[segment]
if isinstance(value, bool):
    print(str(value).lower())
else:
    print(value)
' "$path"
}

assert_json_status() {
  local document="$1"
  local expected="$2"
  local actual
  actual="$(json_value "$document" status)"
  if [[ "$actual" != "$expected" ]]; then
    printf 'status mismatch: got %s, expected %s\n%s\n' \
      "$actual" "$expected" "$document" >&2
    return 1
  fi
}

assert_doctor_check() {
  local document="$1"
  local check_id="$2"
  local expected="$3"
  printf '%s' "$document" | python3 -c '
import json
import sys

report = json.load(sys.stdin)
check_id, expected = sys.argv[1:]
matches = [check for check in report["checks"] if check["id"] == check_id]
if len(matches) != 1:
    raise SystemExit(f"doctor check {check_id!r} not found exactly once")
actual = matches[0]["status"]
if actual != expected:
    raise SystemExit(
        f"doctor check {check_id!r}: got {actual!r}, expected {expected!r}"
    )
' "$check_id" "$expected"
}

assert_nonempty_distinct() {
  local left="$1"
  local right="$2"
  local description="$3"
  if [[ -z "$left" || -z "$right" || "$left" == "$right" ]]; then
    printf '%s are not non-empty and distinct:\nleft=%s\nright=%s\n' \
      "$description" "$left" "$right" >&2
    return 1
  fi
}
