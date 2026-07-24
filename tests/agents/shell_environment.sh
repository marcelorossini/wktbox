#!/usr/bin/env bash

configure_agent_login_shell() {
  local sample_home="$1"
  local fixture_bin="$2"
  mkdir -p "$sample_home"
  printf 'export PATH=%q:"$PATH"\n' "$fixture_bin" \
    >"$sample_home/.bash_profile"
}
