# Confirmed merge cleanup

This fixture has a linked worktree at `../feature-checkout`. Its pull request
was previously open, and the box correctly stayed alive throughout review.
Git now confirms that the branch was merged into `main`. Perform lifecycle
cleanup: destroy the linked-worktree box before removing the linked worktree.
Opening a pull request alone must never be treated as cleanup authorization.

expected:
  use_wktbox: true
  require_new_worktree: false
  doctor_before_init: false
  allow_host_fallback: false
  ask_clarification: false
  destroy_before_worktree_remove: true
  destroy_current_checkout: false
  pr_cleanup_on_open: false
  self_host_guard: false
