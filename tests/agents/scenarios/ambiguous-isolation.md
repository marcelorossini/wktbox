# Ambiguous isolation intent

Run the integration tests. Docker isolation might be useful, but I have not
decided whether I want an isolated environment or ordinary host execution.
Do not infer that choice for me.

expected:
  use_wktbox: false
  require_new_worktree: false
  doctor_before_init: false
  allow_host_fallback: false
  ask_clarification: true
  destroy_before_worktree_remove: false
  destroy_current_checkout: false
  pr_cleanup_on_open: false
  self_host_guard: false
