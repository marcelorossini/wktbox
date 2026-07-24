# Automatic Workspace Detection Design

## Goal

Allow Wktbox to use any existing directory as a workspace without requiring a
Git repository, linked worktree, flag, or configuration. Git becomes an
automatically detected optional capability.

## User Contract

- `--path` identifies the directory mounted at `/workspace`.
- The selected path is authoritative. Wktbox canonicalizes it to an absolute
  path but does not replace it with a Git repository root.
- Omitting `--path` selects the current directory.
- A regular directory, normal Git checkout, linked Git worktree, or directory
  below a Git checkout is valid.
- Wktbox never runs `git init` and the agent skill never asks the user to create
  a repository merely to use isolation.
- Wktbox fails only when the selected path does not exist, is not a directory,
  cannot be resolved safely, or cannot be bind-mounted by Docker.

## Detection Model

Discovery first validates and canonicalizes the requested directory. It then
probes Git metadata without making the probe a prerequisite.

When Git detection succeeds, the workspace records the repository root, common
directory, administrative directory, and branch. Existing repository-root and
linked-worktree identities remain byte-for-byte compatible with current box
IDs.

When Git detection fails, the workspace remains valid and records the Git
diagnostic only for doctor output. Its stable box ID is derived from a
`workspace-path` namespace plus the canonical path, preventing collisions with
Git-derived identities.

When the selected path is below a Git root, Wktbox mounts and configures the
selected path exactly. It uses the selected path, not the repository root, in
the identity so independently selected subdirectories do not share a box.

## Git Integration

The default workflow needs no Git configuration.

- With usable Git metadata, existing `git.mode` behavior remains available.
- Without Git metadata, Git bridging is disabled automatically.
- If the selected path is below rather than equal to the Git worktree root,
  mounted Git bridging is also disabled because the complete worktree is not
  mounted.
- An inapplicable `git.mode: mounted` does not block workspace startup. Doctor
  reports a non-blocking warning explaining that Git integration was skipped.

This keeps every directory runnable while avoiding implicit filesystem changes
or hidden mounts outside the requested path.

## Component Changes

### Discovery and identity

`internal/discovery` represents a workspace with an authoritative path and
optional Git metadata. Directory validation errors are distinct from an
optional Git-probe failure.

`internal/identity` keeps the existing Git identity calculation when the
selected path is the Git worktree root. Path-only and Git-subdirectory
workspaces receive deterministic, namespaced identities.

### Application and sandbox

`internal/app` resolves configuration and project environment files relative to
the selected workspace path. `internal/sandbox` continues persisting the legacy
`worktree` state field and Compose label for compatibility, while treating the
value as the workspace path.

The workspace path remains the only bind mount source and the host working
directory mapper continues mapping descendants into `/workspace`.

### Doctor

The existing check IDs remain stable for JSON consumers, but human labels and
messages use “Workspace” instead of treating Git as mandatory.

- Workspace path and bind mount checks pass for a valid non-Git directory.
- Git detection and disabled bridging produce warnings, not failures.
- Invalid or unmountable paths remain blocking failures.

### Agent skill and documentation

The installed skill explicitly states that the current directory does not need
Git, uses the absolute current path directly, and must not suggest `git init`
after an optional Git-probe warning.

README, quickstart, worktree documentation, troubleshooting, and Portuguese
documentation distinguish an always-valid workspace from optional Git checkout
metadata.

## Compatibility

- Existing `--path` commands and defaults remain unchanged.
- Existing boxes created from Git repository roots and linked worktrees retain
  their IDs and state records.
- Persisted JSON fields and Compose labels named `worktree` remain readable.
- Existing linked-worktree lifecycle and prune behavior remain unchanged.
- No repository or project file is created or modified by detection.

## Verification

Automated coverage includes:

1. discovery of a plain directory when Git returns “not a repository”;
2. rejection of missing paths and regular files;
3. preservation of current Git-root and linked-worktree metadata and IDs;
4. exact-path behavior for a subdirectory inside a Git repository;
5. path-only identity stability and separation from Git identities;
6. no-op Git bridge behavior and doctor warnings without Git;
7. successful app resolution, sandbox rendering, and command mapping for a
   plain directory;
8. an agent scenario proving it uses a non-Git current directory without asking
   for `git init`;
9. end-to-end `doctor`, `up`, `run`, and `destroy` for a temporary non-Git
   directory.

The change is an additive workspace capability and will be published as the
next minor release, `v0.2.0`.
