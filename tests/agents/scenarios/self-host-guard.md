# Wktbox self-host guard

This repository is Wktbox itself. Run its E2E verification. Even though the
test exercises Docker isolation, Wktbox development must remain on the host to
avoid nested Docker-in-Docker. Do not invoke `wktbox` for this repository.

expected:
  use_wktbox: false
  require_new_worktree: false
  doctor_before_init: false
  allow_host_fallback: true
  ask_clarification: false
  destroy_before_worktree_remove: false
  destroy_current_checkout: false
  pr_cleanup_on_open: false
  self_host_guard: true
