---
name: wktbox-isolated-development
description: Use Wktbox for isolated Docker development in a current checkout or linked Git worktree.
---

# Wktbox Isolated Development

Apply this workflow in order. Treat Wktbox as development convenience
isolation, not as a security boundary for hostile workloads.

1. Detect whether the repository itself is Wktbox from its repository identity,
   module name, or project guidance. If it is, stay on the host. Never run the
   Wktbox repository inside Wktbox because nested Docker-in-Docker is
   unsupported.

2. Detect explicit isolation signals in the user request and repository
   guidance. Docker isolation, independent Compose ports, browser or integration
   tests in containers, Webtop, or an explicit Wktbox request are positive
   signals. An ordinary project command without those signals is not consent to
   initialize isolation.

3. Ask the user when isolation intent is unclear. Do not run project commands,
   initialize Wktbox, or choose host execution until the user resolves the
   ambiguity.

4. Choose the current checkout by default. Create or reuse a linked worktree only
   when branch isolation is also requested or already in use. Do not require a
   new worktree for Wktbox. Resolve one checkout path and use that same absolute
   path throughout the operation.

5. Before the first `up` or `run` for a checkout, run:

   ```text
   wktbox doctor --path <checkout>
   ```

6. Stop on doctor failure and report the failed prerequisite. Do not initialize
   the box, do not run the project command, and do not fall back to host
   execution.

7. Run project commands through:

   ```text
   wktbox run --path <checkout> -- <command> [args...]
   ```

   Let `run` initialize the box when needed. Preserve argument boundaries; do
   not wrap the project command in a shell string.

8. For project Compose operations use:

   ```text
   wktbox compose --path <checkout> -- <compose-args...>
   ```

   For the graphical Webtop use:

   ```text
   wktbox open --path <checkout>
   ```

9. Keep current-checkout boxes until explicit user cleanup. Finishing a task is
   not cleanup authorization, and you must not destroy the current checkout's
   box implicitly.

10. Keep linked-worktree boxes through pull-request review. Opening a pull
    request is not cleanup authorization; preserve the box and worktree while
    review or follow-up changes remain possible.

11. After confirming merge into `main`, destroy the linked-worktree box before
    removing the linked worktree:

    ```text
    wktbox destroy --path <checkout> --force
    git worktree remove <checkout>
    ```

    Do not infer a merge from a closed pull request. Confirm it with repository
    state or the user's explicit statement.

12. Use `wktbox prune` only for discovery. Use `wktbox prune --force` only when
    deletion is explicitly authorized by the lifecycle above. A stat warning is
    not proof that a worktree is gone.

## Command templates

Current checkout:

```text
checkout="$(pwd -P)"
wktbox doctor --path "$checkout"
wktbox run --path "$checkout" -- <project-command> [args...]
```

Existing linked worktree:

```text
git worktree list
checkout="<existing-linked-worktree>"
wktbox doctor --path "$checkout"
wktbox run --path "$checkout" -- <project-command> [args...]
```

When branch isolation is explicitly requested and no suitable linked worktree
exists, create it with normal Git worktree commands, then apply the same doctor
and run sequence. Never silently substitute direct host execution after
selecting Wktbox.
