# Wktbox v0.1.0 Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish the current `main` branch as the first official Wktbox release, `v0.1.0`.

**Architecture:** Prepare one release commit that makes `CHANGELOG.md` describe every feature already present in the tagged source. Push and validate that exact commit before creating an immutable annotated tag, then let the tag-triggered GitHub Actions workflow build, attest, publish, and create the final GitHub Release.

**Tech Stack:** Git, GitHub Actions, GitHub REST API, `jq`, Markdown, RTK command wrapper.

## Global Constraints

- `v0.1.0` must point to the approved release commit on `main`.
- Release notes must include every feature shipped in the tagged source.
- Do not create the tag unless the release commit is the remote `main` head and its CI succeeds.
- Do not move or replace a pushed release tag if the release workflow fails.
- Preserve unrelated user-owned working-tree files.

---

### Task 1: Align the changelog with the first release

**Files:**
- Modify: `CHANGELOG.md:7`
- Reference: `docs/superpowers/specs/2026-07-24-first-release-design.md`

**Interfaces:**
- Consumes: The current `Unreleased` and `0.1.0` changelog entries.
- Produces: One complete `0.1.0` section dated `2026-07-24` and an empty `Unreleased` section.

- [ ] **Step 1: Replace the release section**

Make the beginning of `CHANGELOG.md` read exactly:

```markdown
## [Unreleased]

## [0.1.0] - 2026-07-24

### Added

- Persistent `connect`, `connections`, and `disconnect` commands for explicit
  bidirectional networking between two or more boxes.
- Stable `<box-id>.wktbox` discovery inside workload containers, with lifecycle
  reconciliation and human/JSON topology output.
- Current-checkout and optional linked-worktree Docker isolation.
- A dedicated privileged DinD daemon, Webtop, volumes, networks, images, and
  port namespace for every box.
- Automatic TCP loopback routes inside Webtop based on project Compose
  publications, plus an optional host-facing HTTP gateway.
- Host prerequisite diagnostics, structured JSON output, lifecycle commands,
  and safe stale-box pruning.
- Atomic Codex and Claude skill installation with conflict detection.
- Reproducible archives and checksum-verified installers for Linux, macOS, and
  Windows on amd64 and arm64.
- Versioned Webtop and gateway images for linux/amd64 and linux/arm64.

### Security

- Documented that connected boxes share a trusted development network while
  retaining separate DinD daemons and storage.
- Documented the trusted-code boundary: Wktbox is operational isolation, not a
  sandbox for hostile code.
- Kept the host Docker socket out of boxes and protected the internal DinD API
  with container-local TLS.
- Bound host-facing services to loopback and stored generated state with
  restrictive permissions.
```

Keep the existing `[Unreleased]` and `[0.1.0]` comparison links unchanged.

- [ ] **Step 2: Validate the changelog diff**

Run:

```bash
rtk git diff --check -- CHANGELOG.md
rtk rg -n "^## \\[Unreleased\\]|^## \\[0\\.1\\.0\\]|connect.*connections.*disconnect|trusted development network" CHANGELOG.md
```

Expected: no whitespace errors; one `Unreleased` heading; one `0.1.0` heading
dated `2026-07-24`; both newly released feature groups inside `0.1.0`.

- [ ] **Step 3: Commit the changelog**

Run:

```bash
rtk git add CHANGELOG.md
rtk git commit -m "docs: preparar changelog para v0.1.0"
```

Expected: one commit containing only `CHANGELOG.md`.

### Task 2: Publish and validate the release commit

**Files:**
- Verify: `.github/workflows/ci.yml`
- Verify: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: The local release commit from Task 1.
- Produces: A successful CI run for the exact commit at `origin/main`.

- [ ] **Step 1: Verify the commit and working tree**

Run:

```bash
rtk git status --short
rtk git show --stat --oneline HEAD
```

Expected: only pre-existing user-owned untracked files remain; the release
commit contains only `CHANGELOG.md`.

- [ ] **Step 2: Push the release commit**

Run:

```bash
rtk git push origin main
```

Expected: `origin/main` advances to the local `HEAD`.

- [ ] **Step 3: Confirm the remote head**

Run:

```bash
rtk git rev-parse HEAD
rtk git ls-remote origin refs/heads/main
```

Expected: both commands report the same commit SHA.

- [ ] **Step 4: Monitor CI for the release commit**

Query the GitHub Actions API for the `CI` workflow whose `head_sha` is the
release SHA until its status is `completed`.

Expected: the run concludes `success`, including `Docker isolation E2E`.
If it fails, stop before Task 3 and report the failing job.

### Task 3: Create the immutable release tag

**Files:**
- Verify: `.github/workflows/release.yml:3`

**Interfaces:**
- Consumes: The successful release SHA from Task 2.
- Produces: Remote annotated tag `refs/tags/v0.1.0` pointing to that SHA.

- [ ] **Step 1: Confirm the tag does not exist**

Run:

```bash
rtk git tag --list v0.1.0
rtk git ls-remote --tags origin refs/tags/v0.1.0 refs/tags/v0.1.0^{}
```

Expected: both outputs are empty. If either tag exists, stop and inspect it.

- [ ] **Step 2: Create and verify the annotated tag**

Run:

```bash
release_sha="$(rtk git rev-parse HEAD)"
rtk git tag -a v0.1.0 "$release_sha" -m "Wktbox v0.1.0"
rtk git rev-list -n 1 v0.1.0
```

Expected: `git rev-list` prints the same release SHA.

- [ ] **Step 3: Push only the release tag**

Run:

```bash
rtk git push origin refs/tags/v0.1.0
```

Expected: Git reports a new tag `v0.1.0`; this triggers the `Release` workflow.

### Task 4: Verify the published GitHub Release

**Files:**
- Verify: `.github/workflows/release.yml`
- Verify: `docs/release-verification.md`

**Interfaces:**
- Consumes: The tag-triggered `Release` workflow.
- Produces: A final GitHub Release and its public distribution artifacts.

- [ ] **Step 1: Monitor the release workflow**

Query the GitHub Actions API for workflow `Release` and tag branch `v0.1.0`
until its status is `completed`.

Expected: every job concludes `success`. If a job fails, preserve the tag and
report its exact failed step and log excerpt.

- [ ] **Step 2: Verify release metadata**

Run:

```bash
rtk run 'curl -fsSL "https://api.github.com/repos/marcelorossini/wktbox/releases/tags/v0.1.0" | jq -r "[.tag_name,.name,.draft,.prerelease,.target_commitish,.html_url] | @tsv"'
```

Expected: tag `v0.1.0`, title `Wktbox 0.1.0`, `draft=false`, and
`prerelease=false`.

- [ ] **Step 3: Verify release assets**

Run:

```bash
rtk run 'curl -fsSL "https://api.github.com/repos/marcelorossini/wktbox/releases/tags/v0.1.0" | jq -r ".assets[].name" | sort'
```

Expected: six platform archives, `checksums.txt`, `install.sh`, and
`install.ps1`.

- [ ] **Step 4: Confirm the release is latest**

Run:

```bash
rtk run 'curl -fsSL "https://api.github.com/repos/marcelorossini/wktbox/releases/latest" | jq -r ".tag_name"'
```

Expected: `v0.1.0`.
