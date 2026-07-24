# Automatic Workspace Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every existing directory a valid Wktbox workspace while detecting Git metadata automatically and preserving existing Git-root box identities.

**Architecture:** Discovery validates the selected path first and records Git metadata as optional enrichment. Git-root and linked-worktree paths retain the existing identity algorithm; plain directories and Git subdirectories receive namespaced path identities. Application, doctor, agent guidance, and E2E behavior consume the same optional metadata without making Git a prerequisite.

**Tech Stack:** Go 1.26, Bash, Docker Compose, Python 3 agent scorer, Markdown, GitHub Actions.

## Global Constraints

- `--path` mounts exactly the selected existing directory at `/workspace`.
- Omitting `--path` selects the current directory.
- Git detection is automatic, optional, and never runs `git init`.
- Missing paths, regular files, and Docker bind-mount failures remain blocking.
- Existing Git-root and linked-worktree IDs remain byte-for-byte compatible.
- Persisted `worktree` JSON fields and Compose labels remain compatible.
- Inapplicable mounted Git bridging is skipped with a non-blocking doctor warning.
- The completed capability is released as `v0.2.0`.

---

### Task 1: Discover plain and Git-backed workspaces

**Files:**
- Modify: `internal/discovery/worktree.go`
- Modify: `internal/discovery/worktree_test.go`
- Modify: `internal/identity/identity.go`
- Modify: `internal/identity/identity_test.go`

**Interfaces:**
- Produces: `discovery.Worktree` with authoritative `Path`, optional `GitRoot`,
  `CommonDir`, `GitDir`, `Branch`, and `GitProbeError`.
- Produces: `HasGit() bool`, `IsGitRoot() bool`, and
  `identity.ForPath(string, Platform) BoxIdentity`.

- [ ] **Step 1: Add failing discovery tests**

Add tests that require:

```go
func TestDiscoverAcceptsDirectoryOutsideGit(t *testing.T)
func TestDiscoverRejectsMissingPath(t *testing.T)
func TestDiscoverRejectsRegularFile(t *testing.T)
func TestDiscoverKeepsRequestedSubdirectoryInsideGit(t *testing.T)
```

The plain-directory result must keep the canonical requested path, leave Git
fields empty, retain the Git diagnostic, and return no error. The Git
subdirectory result must keep the selected subdirectory in `Path` and record
the repository root separately in `GitRoot`.

- [ ] **Step 2: Verify discovery RED**

Run:

```bash
rtk make test
```

Expected: discovery tests fail because non-Git paths still return
`ErrNotWorktree` and `GitRoot` does not exist.

- [ ] **Step 3: Implement workspace-first discovery**

Validate `requestedPath` with `filepath.Abs`, `filepath.EvalSymlinks`, and
`os.Stat`. Return a wrapped `ErrInvalidWorkspace` for missing paths or files.
Run Git probes only after directory validation. On any Git-probe error, return
a valid path-only result with the diagnostic. On success, keep `Path` equal to
the selected path and populate:

```go
type Worktree struct {
    Path          string
    GitRoot       string
    CommonDir     string
    GitDir        string
    Branch        string
    DisplayName   string
    GitProbeError string
}

func (worktree Worktree) HasGit() bool
func (worktree Worktree) IsGitRoot() bool
```

- [ ] **Step 4: Add and test path identities**

Implement:

```go
func ForPath(workspacePath string, platform Platform) BoxIdentity
```

Hash `"workspace-path\n" + canonical(workspacePath, platform)`. Test stable
canonicalization, different paths, and separation from `ForWorktree`.

- [ ] **Step 5: Verify Task 1 GREEN**

Run:

```bash
rtk make test
```

Expected: all Go tests pass.

- [ ] **Step 6: Commit Task 1**

```bash
rtk git add internal/discovery internal/identity
rtk git commit -m "feat: detectar workspaces sem exigir git"
```

### Task 2: Resolve and diagnose optional Git metadata

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/gitbridge/gitbridge.go`
- Modify: `internal/gitbridge/gitbridge_test.go`
- Modify: `internal/doctor/doctor.go`
- Modify: `internal/doctor/host.go`
- Modify: `internal/doctor/host_test.go`

**Interfaces:**
- Consumes: optional discovery metadata from Task 1.
- Produces: path identity for plain/subdirectory workspaces, legacy identity
  for Git roots, and `gitbridge.Bridge.SkippedReason`.

- [ ] **Step 1: Add failing app and Git bridge tests**

Require app resolution of a plain directory to:

```go
if resolution.Worktree.Path != plainDirectory {
    t.Fatalf("workspace path = %q", resolution.Worktree.Path)
}
if resolution.Worktree.HasGit() {
    t.Fatalf("unexpected Git metadata: %#v", resolution.Worktree)
}
if resolution.Spec.ID != identity.ForPath(plainDirectory, identity.Unix).ID {
    t.Fatalf("workspace ID = %q", resolution.Spec.ID)
}
```

Require `gitbridge.Prepare` to return a bridge with `SkippedReason` and no
mounts for `git.mode: mounted` when Git metadata is absent or the selected path
is below `GitRoot`.

- [ ] **Step 2: Add failing doctor tests**

Require a valid plain directory to produce:

```go
CheckWorktree: Status Warn, message containing "Workspace" and "without Git"
CheckBindMount: Status Pass
CheckGitBridge: Status Warn, message containing "disabled"
Report.OK: true
```

Keep the `worktree` check ID stable but change human labels to `Workspace path`
and `Workspace bind mount`.

- [ ] **Step 3: Verify Task 2 RED**

Run:

```bash
rtk make test
```

Expected: compilation or assertions fail because application and doctor still
require Git metadata.

- [ ] **Step 4: Implement optional Git consumption**

In application resolution, use `identity.ForWorktree` only when
`worktree.IsGitRoot()`; otherwise use `identity.ForPath`.

Extend `gitbridge.Bridge`:

```go
type Bridge struct {
    Mounts             []Mount
    Environment        map[string]string
    RequiresValidation bool
    SkippedReason      string
}
```

Return a skipped bridge rather than an error when mounted mode is inapplicable.
Map `ErrInvalidWorkspace` to CLI code `invalid_workspace`.

- [ ] **Step 5: Update doctor behavior**

Use workspace terminology in messages. A valid non-Git path and skipped bridge
are warnings. Only invalid paths and failed bind mounts block the report.

- [ ] **Step 6: Verify Task 2 GREEN**

Run:

```bash
rtk make test
rtk git diff --check
```

Expected: all tests pass and no whitespace errors exist.

- [ ] **Step 7: Commit Task 2**

```bash
rtk git add internal/app internal/cli internal/gitbridge internal/doctor
rtk git commit -m "feat: executar workspaces com git opcional"
```

### Task 3: Teach agents and users the path-first contract

**Files:**
- Modify: `internal/agentintegration/assets/wktbox-isolated-development/SKILL.md`
- Modify: `tests/agents/validate_skill_test.go`
- Create: `tests/agents/scenarios/plain-directory.md`
- Modify: `tests/agents/evaluate.sh`
- Modify: `tests/agents/score.py`
- Modify: `tests/agents/scorer_test.py`
- Modify: `README.md`
- Modify: `docs/quickstart.md`
- Modify: `docs/worktrees.md`
- Modify: `docs/agents.md`
- Modify: `docs/troubleshooting.md`
- Modify: `docs/pt-BR/configuration.md`
- Modify: `tests/docs_test.go`

**Interfaces:**
- Consumes: successful non-Git doctor behavior from Task 2.
- Produces: agent instructions that use the current path without Git and a
  scorer expectation named `initialize_git`.

- [ ] **Step 1: Add failing skill and scorer tests**

The skill validator must require the phrases:

```text
The selected directory does not need to be a Git repository
Never run git init only to satisfy Wktbox
```

Add scorer logic that records commands beginning with `git init` and compares
them to an `initialize_git` expectation. Add a self-test proving
`initialize_git: false` rejects a logged `git init`.

- [ ] **Step 2: Add the plain-directory agent scenario**

Create a scenario that explicitly requests isolation in a non-Git current
directory and expects:

```yaml
use_wktbox: true
require_new_worktree: false
doctor_before_init: true
allow_host_fallback: false
ask_clarification: false
initialize_git: false
destroy_current_checkout: false
```

Make `evaluate.sh` skip repository initialization only for this scenario and
include it in the scenario list.

- [ ] **Step 3: Verify agent RED**

Run:

```bash
rtk make agent-skill-test
rtk make agent-scorer-test
```

Expected: tests fail until skill wording and scorer behavior are implemented.

- [ ] **Step 4: Update skill and documentation**

State consistently that any existing directory is a workspace, Git metadata is
optional, `--path` is exact, linked worktrees remain optional, and the agent
must not suggest `git init`.

- [ ] **Step 5: Verify Task 3 GREEN**

Run:

```bash
rtk make agent-skill-test
rtk make agent-scorer-test
rtk make docs-check
```

Expected: skill, scorer, harness, and documentation tests pass.

- [ ] **Step 6: Commit Task 3**

```bash
rtk git add internal/agentintegration tests/agents README.md docs tests/docs_test.go
rtk git commit -m "feat: orientar agentes para qualquer diretorio"
```

### Task 4: Prove a real non-Git Docker workspace

**Files:**
- Create: `tests/e2e/plain_directory.sh`
- Modify: `.github/workflows/ci.yml`
- Modify: `Makefile`

**Interfaces:**
- Consumes: path-first discovery and optional Git behavior.
- Produces: `make e2e-plain-directory` and a CI step exercising real Docker.

- [ ] **Step 1: Write the failing E2E**

Create a temporary directory without `.git`, build Wktbox and runtime images,
then execute:

```bash
"$wktbox" --path "$workspace" doctor
"$wktbox" --path "$workspace" up
"$wktbox" --path "$workspace" run -- test -f /workspace/marker.txt
"$wktbox" --path "$workspace" destroy --force
```

Assert doctor completes without blocking failures, the box reports ready, the
marker is visible, and no `.git` is created.

- [ ] **Step 2: Verify E2E RED**

Run:

```bash
rtk bash tests/e2e/plain_directory.sh
```

Expected: failure before box creation because discovery requires Git.

- [ ] **Step 3: Wire the E2E gate**

Add `e2e-plain-directory` to `Makefile` and a dedicated CI step in the existing
Docker isolation job.

- [ ] **Step 4: Verify E2E GREEN**

Run:

```bash
rtk bash tests/e2e/plain_directory.sh
```

Expected: PASS and cleanup removes the managed box.

- [ ] **Step 5: Commit Task 4**

```bash
rtk git add tests/e2e/plain_directory.sh .github/workflows/ci.yml Makefile
rtk git commit -m "feat: validar workspace sem git no docker"
```

### Task 5: Release and verify v0.2.0

**Files:**
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: all completed tasks.
- Produces: successful CI for `main`, annotated tag `v0.2.0`, final GitHub
  Release, and verified public binary/skill behavior.

- [ ] **Step 1: Prepare changelog**

Move the automatic workspace detection feature into:

```markdown
## [0.2.0] - 2026-07-24

### Added

- Accepted any existing directory as a workspace, with automatic optional Git
  detection and exact `--path` mounting.
```

Update comparison links from `v0.1.1` to `v0.2.0`.

- [ ] **Step 2: Run final local gates**

```bash
rtk git diff --check
rtk make test
rtk make test-race
rtk make vet
rtk make install-test
rtk make agent-skill-test
rtk make agent-scorer-test
rtk make docs-check
rtk make release-dry-run VERSION=0.2.0
rtk bash tests/e2e/plain_directory.sh
```

Expected: every command passes.

- [ ] **Step 3: Commit and push main**

```bash
rtk git add CHANGELOG.md
rtk git commit -m "feat: preparar release v0.2.0"
rtk git push origin main
```

Wait for the exact `main` CI SHA to complete successfully before tagging.

- [ ] **Step 4: Tag and publish**

Confirm no local or remote `v0.2.0` exists, then:

```bash
release_sha="$(rtk git rev-parse HEAD)"
rtk git tag -a v0.2.0 "$release_sha" -m "Wktbox v0.2.0"
rtk git push origin refs/tags/v0.2.0
```

Monitor the `Release` workflow to terminal success.

- [ ] **Step 5: Verify the public release**

Confirm nine release assets, six checksum entries, final/latest metadata,
multiarch Webtop and gateway images, and tag-to-main SHA equality.

Download the public `v0.2.0` binary to a temporary directory and prove:

```bash
release_workspace="$(mktemp -d)"
printf 'release marker\n' >"$release_workspace/marker.txt"
wktbox doctor --path "$release_workspace"
wktbox up --path "$release_workspace"
wktbox run --path "$release_workspace" -- test -f /workspace/marker.txt
wktbox destroy --path "$release_workspace" --force
```

Expected: doctor has no blocking failures, the command runs inside the box, no
`.git` is created, and cleanup succeeds.
