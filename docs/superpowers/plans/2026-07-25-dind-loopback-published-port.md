# DinD Loopback-Published Port Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Webtop `localhost` reach inner Docker TCP publications bound only to the DinD loopback, including `127.0.0.1:5174:5173`.

**Architecture:** Preserve Docker `HostIP` values as private upstream candidates and create upstream sockets inside the DinD network namespace already exposed to the loopback sidecar through `pid: service:docker`. Keep Webtop listeners loopback-only, keep `docker:HostPort` as the public logical target, and read current candidates for every accepted connection.

**Tech Stack:** Go 1.26.5, Moby Engine API, Linux `setns(2)`, Docker-in-Docker, Bash E2E, Docker Compose.

## Global Constraints

- Do not rewrite the user's Compose file.
- Do not expose inner application ports on the real host.
- Do not involve explicit port imports or host services.
- `listening` continues to mean that both Webtop loopback listeners are bound.
- All new production behavior must be preceded by a failing regression test.
- The exact `127.0.0.1:5174 -> frontend:5173` route must pass end to end.
- Existing default, remapped, IPv4, IPv6, HTTP, WebSocket, raw TCP, and isolation behavior must remain green.

---

### Task 1: Add the exact failing E2E and preserve Docker binding addresses

**Files:**
- Modify: `tests/fixtures/compose-project/compose.yml`
- Modify: `tests/e2e/automatic_loopback.sh`
- Modify: `internal/loopback/model.go`
- Modify: `internal/loopback/docker.go`
- Modify: `internal/loopback/docker_test.go`
- Modify: `internal/loopback/discovery.go`
- Modify: `internal/loopback/discovery_test.go`

**Interfaces:**
- Consumes: Moby `network.PortBinding.HostIP` and `HostPort`.
- Produces: `PortBinding.HostIP string` and `Publication.Upstreams []string`, where upstreams are sorted numeric `host:port` candidates in the DinD namespace.

- [ ] **Step 1: Add the loopback-only fixture and failing acceptance assertions**

Add this service to `tests/fixtures/compose-project/compose.yml`:

```yaml
  loopback_frontend:
    image: python:3.14-alpine
    working_dir: /app
    command: ["python", "service.py", "http", "5173"]
    environment:
      BOX_MARKER: ${WKTBOX_TEST_MARKER:?WKTBOX_TEST_MARKER is required}
    volumes:
      - .:/app:ro
    ports:
      - "127.0.0.1:5174:5173"
    healthcheck:
      test:
        - CMD
        - python
        - -c
        - import urllib.request; urllib.request.urlopen("http://localhost:5173")
      interval: 2s
      timeout: 2s
      retries: 30
```

After the existing inner-publication inspection in
`tests/e2e/automatic_loopback.sh`, assert the exact binding, prove direct
`docker:5174` access fails, wait for a `listening` route, and prove both boxes
reach their own marker through Webtop:

```bash
loopback_ports="$("$wktbox" --path "$worktree_a" exec -- \
  docker inspect wktbox-fixture-loopback_frontend-1 \
  --format '{{json .NetworkSettings.Ports}}')"
[[ "$loopback_ports" == *'"HostIp":"127.0.0.1","HostPort":"5174"'* ]] ||
  fail_with_diagnostics "scenario 2: loopback-only 5174 publication missing"
if "$wktbox" --path "$worktree_a" exec -- \
  curl --fail --silent --connect-timeout 1 http://docker:5174 >/dev/null 2>&1; then
  fail_with_diagnostics "scenario 2: docker:5174 unexpectedly accepts traffic"
fi
wait_for_route "$worktree_a" 5174 listening
wait_for_http "$worktree_a" "http://localhost:5174" "box-a"
wait_for_http "$worktree_b" "http://localhost:5174" "box-b"
```

Add `5174` to the outer-host leak assertion list.

- [ ] **Step 2: Run the exact E2E to verify RED**

Run:

```bash
rtk bash tests/e2e/automatic_loopback.sh
```

Expected: FAIL with `http://localhost:5174 did not return box-a`; diagnostics
show the route as `listening` and Docker inspect shows
`HostIp=127.0.0.1`.

- [ ] **Step 3: Add failing unit tests for address preservation and normalization**

Extend `TestContainerFromInspectConvertsPublishedPortMap` with:

```go
network.MustParsePort("5173/tcp"): {
    {HostIP: "127.0.0.1", HostPort: "5174"},
},
```

and require:

```go
PortBinding{
    ContainerPort: 5173,
    HostIP: "127.0.0.1",
    HostPort: 5174,
    Protocol: "tcp",
    Published: true,
},
```

Add a discovery test containing `127.0.0.1`, `0.0.0.0`, `::1`, `::`, a
specific address, and duplicates, then require:

```go
want := loopback.Publication{
    Port: 5174,
    Target: "docker:5174",
    Sources: []string{"frontend"},
    Upstreams: []string{
        "127.0.0.1:5174",
        "172.24.0.2:5174",
        "[::1]:5174",
    },
}
```

- [ ] **Step 4: Run the focused unit tests to verify RED**

Run:

```bash
rtk docker run --rm --user "$(id -u):$(id -g)" \
  -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod \
  -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/loopback \
  -run 'Test(ContainerFromInspect|Discover)' -count=1
```

Expected: FAIL because `PortBinding.HostIP` and `Publication.Upstreams` do not
exist.

- [ ] **Step 5: Implement binding preservation and deterministic upstreams**

Add to `PortBinding`:

```go
HostIP string
```

Add to `Publication`:

```go
Upstreams []string `json:"-"`
```

Copy `binding.HostIP` in `containerFromInspect`.

In `discovery.go`, aggregate an upstream set per host port and normalize each
valid numeric Docker host IP with:

```go
func publicationUpstream(hostIP string, hostPort uint16) string {
    parsed := net.ParseIP(strings.TrimSpace(hostIP))
    if parsed == nil {
        return ""
    }
    host := parsed.String()
    if parsed.IsUnspecified() {
        if parsed.To4() != nil {
            host = "127.0.0.1"
        } else {
            host = "::1"
        }
    }
    return net.JoinHostPort(host, strconv.Itoa(int(hostPort)))
}
```

Sort candidates before assigning them to `Publication.Upstreams`.

- [ ] **Step 6: Run focused and full unit tests to verify GREEN**

Run:

```bash
rtk docker run --rm --user "$(id -u):$(id -g)" \
  -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod \
  -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/loopback \
  -run 'Test(ContainerFromInspect|Discover)' -count=1
rtk make test
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
rtk git add tests/fixtures/compose-project/compose.yml \
  tests/e2e/automatic_loopback.sh internal/loopback/model.go \
  internal/loopback/docker.go internal/loopback/docker_test.go \
  internal/loopback/discovery.go internal/loopback/discovery_test.go
rtk git commit -m "fix: preservar enderecos publicados no dind"
```

### Task 2: Dial upstreams from the DinD network namespace

**Files:**
- Modify: `internal/loopback/netns_linux.go`
- Modify: `internal/loopback/netns_unsupported.go`
- Create: `internal/loopback/netns_test.go`
- Modify: `internal/loopback/proxy.go`
- Modify: `internal/loopback/reconciler.go`
- Modify: `internal/loopback/proxy_test.go`

**Interfaces:**
- Consumes: `Publication.Upstreams []string` from Task 1 and PID 1 from
  `pid: service:docker`.
- Produces: `dialNamespaceContext(context.Context, int, string, string) (net.Conn, error)` and `ReconcilerOptions.DialNamespaceContext`.

- [ ] **Step 1: Add failing tests for namespaced candidates and live metadata**

Add a proxy test whose publication has:

```go
Upstreams: []string{"127.0.0.1:5174"},
```

Inject `DialNamespaceContext`, connect through the Webtop listener, and assert
that the injected function receives `127.0.0.1:5174`.

Apply the same port again with:

```go
Upstreams: []string{"127.0.0.1:5175"},
```

Connect again and assert the existing listener uses the new candidate without
another listen call.

Add `netns_test.go` tests requiring errors from:

```go
dialNamespaceContext(context.Background(), 0, "tcp", "127.0.0.1:5174")
dialNamespaceContext(context.Background(), 1, "tcp", "docker:5174")
```

- [ ] **Step 2: Run focused tests to verify RED**

Run:

```bash
rtk docker run --rm --user "$(id -u):$(id -g)" \
  -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod \
  -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/loopback \
  -run 'Test(ReconcilerUsesDinDNamespace|DialNamespace)' -count=1
```

Expected: FAIL because the namespaced dial option and helper do not exist.

- [ ] **Step 3: Implement the Linux namespace dial helper**

In `netns_linux.go`, implement `dialNamespaceContext` using
`runtime.LockOSThread`, `/proc/thread-self/ns/net`, `/proc/<pid>/ns/net`, and
`unix.Setns`. Validate that the host portion of `address` is numeric before
entering the namespace. Use a `net.Dialer` for the connection and always
restore the original namespace. Close the new socket and join errors if
restoration fails.

In `netns_unsupported.go`, return:

```go
errors.New("DinD published-port dialing requires Linux containers")
```

- [ ] **Step 4: Route accepted connections through current publication metadata**

Add to `ReconcilerOptions` and `Reconciler`:

```go
DialNamespaceContext func(context.Context, int, string, string) (net.Conn, error)
```

Default it to `dialNamespaceContext`. Change `serve` to receive the published
port, look up and clone the active publication after `Accept`, and pass that
publication to `proxyConnection`.

Change `proxyConnection` to:

```go
func proxyConnection(
    ctx context.Context,
    downstream net.Conn,
    publication Publication,
    dialContext func(context.Context, string, string) (net.Conn, error),
    dialNamespaceContext func(context.Context, int, string, string) (net.Conn, error),
)
```

Within the existing three-second deadline, use the ordinary dialer only when
`publication.Upstreams` is empty. Otherwise try every upstream through PID 1,
joining failures and stopping at the first connection.

Ensure `clonePublication` clones `Upstreams`.

- [ ] **Step 5: Run focused tests and exact E2E to verify GREEN**

Run:

```bash
rtk docker run --rm --user "$(id -u):$(id -g)" \
  -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod \
  -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/loopback -count=1
rtk bash tests/e2e/automatic_loopback.sh
```

Expected: unit tests PASS and E2E ends with
`wktbox automatic loopback E2E passed`.

- [ ] **Step 6: Commit**

```bash
rtk git add internal/loopback/netns_linux.go \
  internal/loopback/netns_unsupported.go internal/loopback/netns_test.go \
  internal/loopback/proxy.go internal/loopback/reconciler.go \
  internal/loopback/proxy_test.go
rtk git commit -m "fix: acessar publicacoes pelo namespace do dind"
```

### Task 3: Document the corrected route and prepare patch release

**Files:**
- Modify: `README.md`
- Modify: `README.pt-BR.md`
- Modify: `docs/troubleshooting.md`
- Modify: `CHANGELOG.md`
- Modify: `tests/docs_test.go`

**Interfaces:**
- Consumes: the corrected automatic-loopback behavior from Tasks 1 and 2.
- Produces: user-facing documentation and `v0.3.1` changelog metadata.

- [ ] **Step 1: Add failing documentation contract assertions**

Extend `tests/docs_test.go` to require the canonical documentation to contain:

```text
127.0.0.1:5174:5173
DinD network namespace
listening
```

and the Portuguese README to contain:

```text
127.0.0.1:5174:5173
namespace de rede do DinD
```

- [ ] **Step 2: Run docs tests to verify RED**

Run:

```bash
rtk make docs-check
```

Expected: FAIL because the loopback-only binding behavior is not documented.

- [ ] **Step 3: Update documentation and changelog**

Document in both READMEs that:

```yaml
ports:
  - "127.0.0.1:5174:5173"
```

is reached as Webtop `localhost:5174` by creating the upstream socket in the
DinD network namespace, without broadening the publication to another
interface.

Document in troubleshooting that `listening` reports the Webtop listener and
that end-to-end failures should be diagnosed from the Docker `HostIp`,
`HostPort`, and service health.

Move the fix into:

```markdown
## [0.3.1] - 2026-07-25

### Fixed

- Fixed automatic Webtop localhost forwarding for inner Docker TCP
  publications bound only to the DinD loopback, including remapped ports such
  as `127.0.0.1:5174:5173`.
```

Update comparison links so `[Unreleased]` starts at `v0.3.1` and `[0.3.1]`
compares `v0.3.0...v0.3.1`.

- [ ] **Step 4: Run docs and full tests to verify GREEN**

Run:

```bash
rtk make docs-check
rtk make test
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add README.md README.pt-BR.md docs/troubleshooting.md \
  CHANGELOG.md tests/docs_test.go
rtk git commit -m "fix: documentar publicacoes restritas ao loopback do dind"
```

### Task 4: Verify, review, merge, push, and tag

**Files:**
- Verify all files changed since `main`.
- Do not modify unrelated files in the primary checkout.

**Interfaces:**
- Consumes: all implementation and documentation commits.
- Produces: merged `main`, pushed `main`, and annotated tag `v0.3.1`.

- [ ] **Step 1: Format and inspect the complete diff**

Run:

```bash
rtk make fmt
rtk git diff --check main...HEAD
rtk git diff --stat main...HEAD
rtk git status --short
```

Expected: no formatting or whitespace errors and only planned files changed.
Commit formatting changes if `make fmt` produces any.

- [ ] **Step 2: Run the complete pre-merge verification**

Run:

```bash
rtk make test
rtk make test-race
rtk make vet
rtk make docs-check
rtk make images
rtk make images-test
rtk bash tests/e2e/automatic_loopback.sh
rtk bash tests/release/build_test.sh
rtk bash tests/install/unix_test.sh
```

Expected: every command exits 0.

- [ ] **Step 3: Perform a focused self-review**

Compare `main...HEAD` against the design and verify:

```text
HostIP is preserved.
Loopback-only candidates are dialed inside PID 1's network namespace.
The caller's namespace is restored on every return path.
New connections use current metadata.
No real-host application port is added.
No import path is involved.
The exact 5174-to-5173 E2E passes.
```

Fix every critical or important finding and rerun Step 2. A reviewer subagent
is intentionally not used because this session prohibits subagent dispatch
unless explicitly requested.

- [ ] **Step 4: Merge into updated main**

From the primary checkout:

```bash
rtk git fetch origin
rtk git checkout main
rtk git pull --ff-only origin main
rtk git merge --no-ff fix/dind-loopback-published-port \
  -m "fix: encaminhar portas publicadas no loopback do dind"
```

Expected: merge succeeds without touching the pre-existing untracked plan.

- [ ] **Step 5: Verify the merged result**

Run from `main`:

```bash
rtk make test
rtk make test-race
rtk make vet
rtk bash tests/e2e/automatic_loopback.sh
rtk bash tests/release/build_test.sh
```

Expected: every command exits 0 from the exact merge commit.

- [ ] **Step 6: Push main and create the patch tag**

First confirm that `v0.3.1` does not exist locally or remotely:

```bash
rtk git tag --list v0.3.1
rtk git ls-remote --tags origin refs/tags/v0.3.1
```

Then:

```bash
rtk git push origin main
rtk git tag -a v0.3.1 -m "Wktbox v0.3.1"
rtk git push origin v0.3.1
```

Expected: `main` and the annotated tag are accepted by `origin`.

- [ ] **Step 7: Verify remote references and clean the owned worktree**

Run:

```bash
rtk git ls-remote origin refs/heads/main refs/tags/v0.3.1 \
  refs/tags/v0.3.1^{}
rtk git worktree remove \
  .worktrees/dind-loopback-published-port
rtk git worktree prune
rtk git branch -d fix/dind-loopback-published-port
```

Expected: remote `main` identifies the merge commit, the peeled tag identifies
the same commit, and the owned worktree and merged feature branch are removed.
