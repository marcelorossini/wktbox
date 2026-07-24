# Coding-agent integration

Wktbox bundles the `wktbox-isolated-development` skill for Codex and Claude. It
teaches agents when to select isolation, how to preserve the workspace
lifecycle, and when to stop.

## Install

Preview both targets:

```bash
wktbox agents install --target all --dry-run
```

Install:

```bash
wktbox agents install --target all
```

Use `--target codex` or `--target claude` for one provider. The default target
is `--target all`.

Codex receives the skill at
`~/.agents/skills/wktbox-isolated-development` and a managed instruction block
in `$CODEX_HOME/AGENTS.md`, or `~/.codex/AGENTS.md`.

Claude receives the skill under
`$CLAUDE_CONFIG_DIR/skills/wktbox-isolated-development`, or
`~/.claude/skills/wktbox-isolated-development`, and a managed instruction block
in the corresponding `CLAUDE.md`.

Wktbox installs no plugin, hook, daemon, alias, or shell startup mutation. User
text outside the marked block is preserved.

## Status

```bash
wktbox agents status --target all
wktbox --json agents status --target codex
```

Status reports the concrete target, skill and instruction paths, managed block,
binary/skill version, expected and installed digests, and conflict state.

## Update

After updating the Wktbox binary, reinstall the target:

```bash
wktbox agents install --target all
```

An unchanged installation is a no-op. Files are staged and replaced atomically.

## Conflict handling

A conflict means the installed managed skill differs from the skill bundled in
the current Wktbox binary. Normal install and uninstall refuse to overwrite or
remove local skill edits.

Review the reported path and preserve any desired changes. To replace a
modified managed skill with the bundled version:

```bash
wktbox agents install --target codex --force
```

To remove a modified managed skill after explicit review:

```bash
wktbox agents uninstall --target codex --force
```

`--force` applies only to the selected managed skill. It does not overwrite
user-owned instruction text outside the managed instruction block.

## Uninstall

Preview:

```bash
wktbox agents uninstall --target all --dry-run
```

Remove the skill and Wktbox managed instruction block:

```bash
wktbox agents uninstall --target all
```

Provider credentials, sessions, configuration, other skills, and unrelated
instruction text remain untouched.

## Bundled policy

The workflow:

1. keeps Wktbox development itself on the host;
2. uses explicit isolation intent or asks for clarification;
3. defaults to the current directory, accepts paths without Git, and makes
   worktrees optional;
4. runs `wktbox doctor` before first initialization;
5. treats optional Git warnings as non-blocking;
6. stops on a blocking prerequisite failure rather than falling back to the
   host;
7. never initializes Git merely to make a directory acceptable. Never run
   `git init` only to satisfy Wktbox;
8. executes project commands through `wktbox run`;
9. preserves current-directory boxes until explicit cleanup;
10. preserves linked worktrees through pull-request review; and
11. destroys a merged linked-worktree box before removing the checkout.

See [Worktrees and lifecycle](worktrees.md) and
[Security model](security.md).
