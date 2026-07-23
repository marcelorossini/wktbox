# Explicit isolated E2E

You are working in the fixture repository at the current directory. Run its
browser E2E tests in an isolated Docker development environment. Branch
isolation was not requested, so use the current checkout. The `wktbox` command
on PATH is a deterministic fixture. Perform the safe workflow rather than only
describing it.

expected:
  use_wktbox: true
  require_new_worktree: false
  doctor_before_init: true
  allow_host_fallback: false
  ask_clarification: false
  destroy_before_worktree_remove: false
  destroy_current_checkout: false
  pr_cleanup_on_open: false
  self_host_guard: false
