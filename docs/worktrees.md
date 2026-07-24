# Optional worktrees and lifecycle

Wktbox works in the current checkout. A linked worktree is optional and useful
when you want a second checkout for a branch at the same time.

## Create a branch checkout

```bash
git worktree add ../feature-example -b feature/example
wktbox doctor --path ../feature-example
wktbox up --path ../feature-example
wktbox run --path ../feature-example -- go test ./...
```

Paths may be absolute or relative to the command's current directory. Quote
paths containing spaces.

Every checkout receives a stable identity derived from its Git common
directory and normalized checkout path. Consequently, two worktrees may run
the same Compose project and publish the same internal ports without sharing a
daemon, network, image cache, volumes, or Webtop loopback.

## Reuse during review

Keep the worktree and its box while a pull request is open or review changes may
still arrive. Opening or closing a pull request alone is not deletion
authorization. Use `wktbox stop --path <checkout>` if you want to release CPU
and memory while preserving state.

## Cleanup after merge

First confirm that the branch is merged into `main`. Then destroy the box before
removing the linked worktree:

```bash
wktbox destroy --path ../feature-example --force
git worktree remove ../feature-example
```

This order lets Wktbox resolve the checkout and remove the correct Docker
project while the path still exists.

## Stale records

If a checkout was removed outside this lifecycle, discover stale boxes:

```bash
wktbox prune
wktbox --json prune
```

The default is a dry run. Only a recorded path for which the operating system
returns “not found” becomes a candidate. Permission errors, empty paths, and
other inspection failures are warnings and are never treated as proof of
deletion.

After reviewing the candidates:

```bash
wktbox prune --force
```

This destroys only the reported missing-checkout boxes. It does not remove Git
worktrees, and it does not make a broad Docker cleanup call.

See [Current-checkout quickstart](quickstart.md) and
[Security model](security.md).
