# Automatic Webtop Loopback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every TCP port actually published by containers in a box's
private DinD reachable at the same `localhost:<published-port>` inside that
box's Webtop, without gateway configuration or CLI flags.

**Architecture:** A required `loopback` sidecar shares the Webtop network
namespace and runs a Go daemon that watches the inner Docker API over TLS. The
daemon reconciles IPv4 and IPv6 loopback listeners, forwards raw TCP to
`docker:<published-port>`, and exposes sync/status operations through a private
Unix socket; the host CLI invokes those operations through external Compose.

**Tech Stack:** Go 1.26, Moby API v1.55.0, Moby client v0.5.0, Docker Engine
29/DinD TLS, Docker Compose, LinuxServer Webtop, shell E2E, Chromium headless.

## Global Constraints

- Discover only running inner containers and only entries present in
  `NetworkSettings.Ports` with a non-empty `HostPort`.
- Treat the published `HostPort`, not the container port, as the Webtop
  localhost port; deduplicate equal TCP host ports and retain all origins.
- Bind every active route atomically on `127.0.0.1:PORT` and `[::1]:PORT`.
- Forward TCP bytes to `docker:PORT`, resolve `docker` on every new connection,
  and use a 3-second upstream dial timeout.
- Ignore UDP publications for forwarding and expose a structured warning.
- Debounce Docker events for 100 milliseconds; reconnect from 250 milliseconds
  to 5 seconds with exponential backoff and ±20% jitter; resync every 30
  seconds.
- Limit repeated logs to one emission per error class every 30 seconds.
- Graceful shutdown has a 10-second ceiling.
- Keep route conflicts visible without making the whole box unready.
- Require Docker healthy, Webtop running, and loopback healthy for box
  readiness.
- Move Webtop HTTP, HTTPS, and websocket internals to 61000, 61001, and 61002;
  do not publish the reserved SSH offset.
- Keep dynamic loopback status out of `state.json`.
- Preserve child stdout and stderr; route summaries after successful `run` and
  `compose` go to stderr and obey `--json` and `--quiet`.
- Keep the optional gateway profile independent and backward compatible.
- Use the repository root as the Webtop Docker build context.

---

## File Structure

New files:

- `internal/loopback/model.go`: stable status, route, warning, publication, and
  container snapshot types.
- `internal/loopback/discovery.go`: conversion from inner container snapshots to
  the desired TCP route set.
- `internal/loopback/discovery_test.go`: published-port, deduplication, stopped,
  unpublished, UDP, and sorting tests.
- `internal/loopback/proxy.go`: dual-stack listener lifecycle and transparent
  TCP connection forwarding.
- `internal/loopback/proxy_test.go`: real IPv4/IPv6 forwarding and atomic
  conflict tests.
- `internal/loopback/reconciler.go`: desired/actual listener reconciliation and
  immutable status snapshots.
- `internal/loopback/reconciler_test.go`: add/remove/conflict/source/status
  reconciliation tests.
- `internal/loopback/source.go`: source and event subscription interfaces.
- `internal/loopback/docker.go`: Moby client adapter over DinD TLS.
- `internal/loopback/docker_test.go`: conversion tests independent of a live
  daemon.
- `internal/loopback/daemon.go`: initial, event-driven, periodic, and reconnect
  synchronization loop.
- `internal/loopback/daemon_test.go`: fake-source tests for debounce, reconnect,
  periodic sync, and stream state.
- `internal/loopback/control.go`: private Unix control server and client.
- `internal/loopback/control_test.go`: `sync`, `status`, invalid command, modes,
  and cancellation tests.
- `cmd/wktbox-loopback/main.go`: `serve`, `sync --json`, and `status --json`
  executable entry point.
- `cmd/wktbox-loopback/main_test.go`: argument and exit-status tests.
- `tests/e2e/automatic_loopback.sh`: all automatic-loopback acceptance
  scenarios.
- `tests/fixtures/compose-project/public/loopback.html`: Chromium HTTP and
  WebSocket probe page.

Modified files:

- `go.mod`, `go.sum`: exact Moby API/client dependencies.
- `assets/sandbox.compose.yml`: required loopback sidecar, high Webtop internal
  ports, healthcheck, and removed SSH publication.
- `images/webtop/Dockerfile`: root-context multi-stage build that embeds
  `wktbox-loopback`.
- `internal/compose/client.go`, `internal/compose/client_test.go`: readiness,
  recovery, and sidecar control calls.
- `internal/state/model.go`, `internal/state/store_test.go`: transient loopback
  field and persistence guard.
- `internal/sandbox/manager.go`, `internal/sandbox/manager_test.go`: attach
  dynamic status and expose explicit sync.
- `internal/app/app.go`, `internal/app/app_test.go`: service-level loopback sync.
- `internal/output/output.go`, `internal/output/output_test.go`: status and
  stderr route summaries.
- `internal/cli/root.go`, `internal/cli/root_test.go`: post-child sync for
  `run`/`compose`, not `exec`.
- `internal/sandbox/render_test.go`: generated Compose contract.
- `tests/fixtures/compose-project/compose.yml`,
  `tests/fixtures/compose-project/service.py`: differing host/container ports
  and WebSocket echo.
- `tests/e2e/two_worktrees.sh`: root-context Webtop build and compatibility with
  the new required service.
- `Makefile`, `.github/workflows/ci.yml`, `tests/integration/spike.sh`: root
  build context and loopback acceptance target.
- `README.md`, `docs/configuration.md`, `docs/security.md`, `docs/windows.md`:
  user-facing automatic localhost behavior, diagnostics, limitations, and
  build commands.

### Task 1: Published-Port Discovery Model

**Files:**

- Create: `internal/loopback/model.go`
- Create: `internal/loopback/discovery.go`
- Create: `internal/loopback/discovery_test.go`

**Interfaces:**

- Consumes: standard library only.
- Produces:
  `func Discover(containers []Container) Desired`,
  `type Desired struct { Publications []Publication; Warnings []Warning }`,
  and the stable `Status`, `Route`, `Warning`, `Container`, `PortBinding`, and
  `Publication` types plus `func Unavailable(error) Status`, used by every later
  task.

- [x] **Step 1: Write failing table tests for discovery**

```go
func TestDiscoverUsesPublishedHostPortAndDeduplicatesOrigins(t *testing.T) {
    got := loopback.Discover([]loopback.Container{
        {ID: "a", Name: "api", Running: true, Ports: []loopback.PortBinding{
            {ContainerPort: 3000, HostPort: 8000, Protocol: "tcp", Published: true},
        }},
        {ID: "b", Name: "worker", Running: true, Ports: []loopback.PortBinding{
            {ContainerPort: 9000, HostPort: 8000, Protocol: "tcp", Published: true},
        }},
    })
    want := []loopback.Publication{{
        Port: 8000, Target: "docker:8000", Sources: []string{"api", "worker"},
    }}
    if !reflect.DeepEqual(got.Publications, want) {
        t.Fatalf("publications = %#v; want %#v", got.Publications, want)
    }
}

func TestDiscoverRejectsStoppedUnpublishedAndUDP(t *testing.T) {
    got := loopback.Discover([]loopback.Container{
        {Name: "stopped", Running: false, Ports: []loopback.PortBinding{{
            HostPort: 5173, Protocol: "tcp", Published: true,
        }}},
        {Name: "exposed", Running: true, Ports: []loopback.PortBinding{{
            ContainerPort: 8080, Protocol: "tcp",
        }}},
        {Name: "dns", Running: true, Ports: []loopback.PortBinding{{
            HostPort: 5353, Protocol: "udp", Published: true,
        }}},
    })
    if len(got.Publications) != 0 {
        t.Fatalf("publications = %#v", got.Publications)
    }
    if len(got.Warnings) != 1 || got.Warnings[0].Code != "udp_unsupported" {
        t.Fatalf("warnings = %#v", got.Warnings)
    }
}
```

- [x] **Step 2: Run discovery tests and verify RED**

Run:

```bash
rtk docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/loopback -run TestDiscover -count=1
```

Expected: FAIL because `wktbox/internal/loopback` and its public types do not
exist.

- [x] **Step 3: Implement the exact stable model and deterministic discovery**

```go
type PortBinding struct {
    ContainerPort uint16
    HostPort      uint16
    Protocol      string
    Published     bool
}

type Container struct {
    ID      string
    Name    string
    Running bool
    Ports   []PortBinding
}

type Publication struct {
    Port    uint16   `json:"port"`
    Target  string   `json:"target"`
    Sources []string `json:"sources"`
}

type Warning struct {
    Code    string `json:"code"`
    Port    uint16 `json:"port,omitempty"`
    Source  string `json:"source,omitempty"`
    Message string `json:"message"`
}

type Route struct {
    Port    uint16   `json:"port"`
    Target  string   `json:"target"`
    Sources []string `json:"sources"`
    Protocol string  `json:"protocol"`
    State   string   `json:"state"`
    Error   string   `json:"error,omitempty"`
}

type Status struct {
    EventStream string    `json:"eventStream"`
    UpdatedAt   time.Time `json:"updatedAt"`
    Routes      []Route   `json:"routes"`
    Warnings    []Warning `json:"warnings"`
}

func Unavailable(err error) Status {
    message := ""
    if err != nil {
        message = err.Error()
    }
    return Status{
        EventStream: "unavailable",
        UpdatedAt: time.Now().UTC(),
        Warnings: []Warning{{
            Code: "loopback_unavailable", Message: message,
        }},
    }
}
```

`Discover` must aggregate TCP entries in `map[uint16]map[string]struct{}`,
ignore invalid zero ports, produce one warning per UDP source/port pair, and
sort publications by port, sources lexicographically, and warnings by
port/source.

- [x] **Step 4: Run package tests and verify GREEN**

Run:

```bash
rtk docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/loopback -count=1
```

Expected: PASS.

- [x] **Step 5: Commit the discovery slice**

```bash
rtk git add internal/loopback
rtk git commit -m "feat: descobre portas publicadas no dind"
```

### Task 2: Dual-Stack TCP Proxy and Reconciler

**Files:**

- Create: `internal/loopback/proxy.go`
- Create: `internal/loopback/proxy_test.go`
- Create: `internal/loopback/reconciler.go`
- Create: `internal/loopback/reconciler_test.go`

**Interfaces:**

- Consumes: `Desired`, `Publication`, `Route`, and `Status` from Task 1.
- Produces:
  `func NewReconciler(options ReconcilerOptions) *Reconciler`,
  `func (*Reconciler) Apply(context.Context, Desired) Status`,
  `func (*Reconciler) Status() Status`, and
  `func (*Reconciler) Close(context.Context) error`.

- [x] **Step 1: Write failing real-proxy and reconciliation tests**

```go
func TestReconcilerForwardsIPv4AndIPv6ToFreshDockerResolution(t *testing.T) {
    upstream := listenEcho(t)
    dialed := make(chan string, 4)
    reconciler := loopback.NewReconciler(loopback.ReconcilerOptions{
        Listen: net.Listen,
        DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
            dialed <- address
            return (&net.Dialer{}).DialContext(ctx, network, upstream.Addr().String())
        },
    })
    port := freeDualStackPort(t)
    status := reconciler.Apply(context.Background(), loopback.Desired{
        Publications: []loopback.Publication{{
            Port: port, Target: fmt.Sprintf("docker:%d", port), Sources: []string{"frontend"},
        }},
    })
    assertEcho(t, fmt.Sprintf("127.0.0.1:%d", port))
    assertEcho(t, fmt.Sprintf("[::1]:%d", port))
    if status.Routes[0].State != "listening" {
        t.Fatalf("status = %#v", status)
    }
}

func TestReconcilerKeepsOtherRoutesWhenOnePortConflicts(t *testing.T) {
    occupied := listenDualStackPair(t)
    defer occupied.Close()
    free := freeDualStackPort(t)
    got := reconciler.Apply(context.Background(), desired(occupied.Port, free))
    assertRouteState(t, got, occupied.Port, "conflict")
    assertRouteState(t, got, free, "listening")
}
```

The suite must also assert removal closes both listeners, equal desired state
reuses listeners, target source changes update status without rebinding, and
`Close` drains connections for at most 10 seconds.

- [x] **Step 2: Run the proxy/reconciler tests and verify RED**

Run:

```bash
rtk docker run --rm --network host -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/loopback -run 'TestReconciler' -count=1
```

Expected: FAIL with undefined `NewReconciler` and `ReconcilerOptions`.

- [x] **Step 3: Implement atomic listeners and transparent copying**

```go
type ReconcilerOptions struct {
    Listen      func(network, address string) (net.Listener, error)
    DialContext func(context.Context, string, string) (net.Conn, error)
    Now         func() time.Time
}

type listenerPair struct {
    ipv4 net.Listener
    ipv6 net.Listener
    stop chan struct{}
}

func bindPair(listen func(string, string) (net.Listener, error), port uint16) (*listenerPair, error) {
    ipv4, err := listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
    if err != nil {
        return nil, err
    }
    ipv6, err := listen("tcp6", net.JoinHostPort("::1", strconv.Itoa(int(port))))
    if err != nil {
        _ = ipv4.Close()
        return nil, err
    }
    return &listenerPair{ipv4: ipv4, ipv6: ipv6, stop: make(chan struct{})}, nil
}
```

Each accept loop launches one goroutine per connection. That goroutine creates
`context.WithTimeout(ctx, 3*time.Second)`, dials the current
`docker:<port>` target, performs two `io.Copy` calls, propagates `CloseWrite`
when implemented, and closes both connections. `Apply` computes removals,
retentions, and additions under a mutex; a failed pair is fully closed and
recorded as `conflict`, never partially installed.

- [x] **Step 4: Run proxy tests plus race detector and verify GREEN**

Run:

```bash
rtk docker run --rm --network host -v "$PWD:/src" -w /src golang:1.26.5 \
  go test -race ./internal/loopback -run 'TestReconciler' -count=1
```

Expected: PASS with no race report.

- [x] **Step 5: Commit the proxy slice**

```bash
rtk git add internal/loopback
rtk git commit -m "feat: reconcilia proxy tcp no loopback"
```

### Task 3: Docker TLS Source, Events, and Daemon Lifecycle

**Files:**

- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/loopback/source.go`
- Create: `internal/loopback/docker.go`
- Create: `internal/loopback/docker_test.go`
- Create: `internal/loopback/daemon.go`
- Create: `internal/loopback/daemon_test.go`

**Interfaces:**

- Consumes: Task 2 `Reconciler`.
- Produces:
  `type Source interface`,
  `func NewDockerSourceFromEnv() (*DockerSource, error)`,
  `func NewDaemon(DaemonOptions) *Daemon`,
  `func (*Daemon) Run(context.Context) error`,
  `func (*Daemon) Sync(context.Context) (Status, error)`, and
  `func (*Daemon) Status() Status`.

- [x] **Step 1: Add exact Moby modules and write failing adapter/daemon tests**

Add direct requirements:

```go
github.com/moby/moby/api v1.55.0
github.com/moby/moby/client v0.5.0
```

Define fake source expectations:

```go
type Source interface {
    Snapshot(context.Context) ([]Container, error)
    Subscribe(context.Context) (Subscription, error)
    Close() error
}

type Event struct {
    Action string
}

type Subscription struct {
    Events <-chan Event
    Errors <-chan error
}

type Backoff struct {
    Initial time.Duration
    Maximum time.Duration
    Jitter  float64
}

func TestDaemonInitialSyncDebouncesEventsAndReconnects(t *testing.T) {
    source := newScriptedSource()
    daemon := loopback.NewDaemon(loopback.DaemonOptions{
        Source: source, Reconciler: reconciler,
        Debounce: 100 * time.Millisecond,
        ResyncInterval: 30 * time.Second,
        Backoff: loopback.Backoff{Initial: 250 * time.Millisecond, Maximum: 5 * time.Second, Jitter: .20},
        After: fakeClock.After,
    })
    // Start, deliver start/die/rename in one 100 ms window, and assert one
    // Snapshot call. Break the stream, advance 250 ms, reconnect, and assert a
    // mandatory full Snapshot before eventStream becomes "connected".
}
```

Docker conversion tests construct Moby inspect responses containing
`3000/tcp -> HostPort 8000`, `5173/tcp -> HostPort 5173`, an empty binding, and
`5353/udp`, then assert the neutral `Container` representation.

- [x] **Step 2: Run daemon tests and verify RED**

Run:

```bash
rtk docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 \
  sh -c 'go mod tidy && go test ./internal/loopback -run "Test(Docker|Daemon)" -count=1'
```

Expected: FAIL because source and daemon implementations are absent.

- [x] **Step 3: Implement the Moby adapter and synchronization loop**

Create the client with:

```go
dockerClient, err := client.New(
    client.FromEnv,
    client.WithAPIVersionNegotiation(),
)
```

`Snapshot` lists running containers, inspects each one, strips the leading `/`
from names, and converts only `NetworkSettings.Ports` bindings with a parseable
host port. `Subscribe` filters `type=container` and maps only `start`, `die`,
`stop`, `destroy`, and `rename`.

The daemon algorithm is:

```go
func (daemon *Daemon) Sync(ctx context.Context) (Status, error) {
    containers, err := daemon.source.Snapshot(ctx)
    if err != nil {
        return daemon.Status(), fmt.Errorf("snapshot inner Docker containers: %w", err)
    }
    status := daemon.reconciler.Apply(ctx, Discover(containers))
    status.EventStream = daemon.streamState()
    daemon.setStatus(status)
    return status, nil
}
```

`Run` performs an initial sync, opens a subscription, coalesces relevant events
with the exact 100 ms debounce, resyncs on the exact 30 s ticker, marks the
stream `disconnected` on errors, and reconnects with jittered exponential
backoff. Cancellation closes the source and calls reconciler shutdown with a
10-second context. A keyed log limiter records the last emission time per error
class and suppresses repeats for exactly 30 seconds.

- [x] **Step 4: Run loopback tests and verify GREEN**

Run:

```bash
rtk docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 \
  go test -race ./internal/loopback -count=1
```

Expected: PASS with no race report.

- [x] **Step 5: Commit the daemon slice**

```bash
rtk git add go.mod go.sum internal/loopback
rtk git commit -m "feat: observa eventos do docker interno"
```

### Task 4: Private Control Socket and Sidecar Executable

**Files:**

- Create: `internal/loopback/control.go`
- Create: `internal/loopback/control_test.go`
- Create: `cmd/wktbox-loopback/main.go`
- Create: `cmd/wktbox-loopback/main_test.go`

**Interfaces:**

- Consumes: Task 3 daemon methods.
- Produces:
  `func ServeControl(context.Context, string, Controller) error`,
  `func Request(context.Context, string, string) (Status, error)`, and a static
  Linux executable with `serve`, `sync --json`, `status --json`.

The consumed control interface is exact:

```go
type Controller interface {
    Sync(context.Context) (Status, error)
    Status() Status
}
```

- [x] **Step 1: Write failing socket protocol and command tests**

```go
func TestControlSyncAndStatusUsePrivateSocket(t *testing.T) {
    path := filepath.Join(t.TempDir(), "control.sock")
    controller := &fakeController{status: loopback.Status{EventStream: "connected"}}
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    go func() { _ = loopback.ServeControl(ctx, path, controller) }()
    waitForSocket(t, path)
    got, err := loopback.Request(context.Background(), path, "sync")
    if err != nil || controller.syncCalls != 1 || got.EventStream != "connected" {
        t.Fatalf("got=%#v calls=%d err=%v", got, controller.syncCalls, err)
    }
    info, err := os.Stat(path)
    if err != nil || info.Mode().Perm() != 0o600 {
        t.Fatalf("socket mode=%v err=%v", info.Mode().Perm(), err)
    }
}
```

Also assert newline-delimited `{"command":"..."}` requests, one JSON response,
unknown-command errors, status without sync, socket removal on cancellation,
and command exit 1 when `eventStream != connected`.

- [x] **Step 2: Run control and command tests and verify RED**

Run:

```bash
rtk docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/loopback ./cmd/wktbox-loopback \
  -run 'Test(Control|Command)' -count=1
```

Expected: FAIL with undefined control functions and absent command package.

- [x] **Step 3: Implement control protocol and executable**

The wire request is:

```go
type controlRequest struct {
    Command string `json:"command"`
}
```

The server creates `/run/wktbox-loopback` as `0700`, removes a stale socket,
listens on Unix, applies `0600`, and serves `status` via `Controller.Status()`
or `sync` via `Controller.Sync(ctx)`. The response is either `Status` or
`{"error":"message"}`.

`serve` builds the Docker source, reconciler, and daemon, starts the control
server at `/run/wktbox-loopback/control.sock`, writes the latest status
atomically to `/run/wktbox-loopback/status.json` with `0644`, handles
`SIGTERM`/`SIGINT`, and exits after the 10-second shutdown. `sync` and `status`
call `Request`; `--json` emits one JSON document, while human mode prints
routes and warnings.

- [x] **Step 4: Run command tests and a static build**

Run:

```bash
rtk docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 \
  sh -c 'go test -race ./internal/loopback ./cmd/wktbox-loopback &&
         CGO_ENABLED=0 go build -o /tmp/wktbox-loopback ./cmd/wktbox-loopback'
```

Expected: PASS and `/tmp/wktbox-loopback` created inside the build container.

- [x] **Step 5: Commit the control-plane slice**

```bash
rtk git add internal/loopback cmd/wktbox-loopback
rtk git commit -m "feat: expoe controle privado do loopback"
```

### Task 5: Webtop Image and Required Compose Sidecar

**Files:**

- Modify: `images/webtop/Dockerfile`
- Modify: `assets/sandbox.compose.yml`
- Modify: `internal/sandbox/render_test.go`
- Modify: `Makefile`
- Modify: `.github/workflows/ci.yml`
- Modify: `tests/integration/spike.sh`
- Modify: `tests/e2e/two_worktrees.sh`

**Interfaces:**

- Consumes: Task 4 executable.
- Produces: one Webtop image containing `/usr/local/bin/wktbox-loopback` and a
  generated external Compose project whose required sidecar is healthchecked.

- [x] **Step 1: Write failing generated-Compose assertions**

```go
func TestRenderIncludesRequiredLoopbackSidecarAndHighWebtopPorts(t *testing.T) {
    files, err := sandbox.Render(t.TempDir(), testSpec())
    if err != nil { t.Fatal(err) }
    document := readCompose(t, files.ComposePath)
    webtop := document.Services["webtop"]
    loopback := document.Services["loopback"]
    assertEnvironment(t, webtop, "CUSTOM_PORT", "61000")
    assertEnvironment(t, webtop, "CUSTOM_HTTPS_PORT", "61001")
    assertEnvironment(t, webtop, "CUSTOM_WS_PORT", "61002")
    assertPublishedPort(t, webtop, "${PORT_HTTP}", "61000")
    assertPublishedPort(t, webtop, "${PORT_HTTPS}", "61001")
    assertNoPublishedTarget(t, webtop, "22")
    assertStringField(t, loopback, "network_mode", "service:webtop")
    assertCommand(t, loopback, "wktbox-loopback", "serve")
    assertNoProfiles(t, loopback)
}
```

Extend Compose client tests in Task 6 to require loopback health for readiness.

- [x] **Step 2: Run render test and verify RED**

Run:

```bash
rtk docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/sandbox -run TestRenderIncludesRequiredLoopback -count=1
```

Expected: FAIL because the sidecar and high internal ports are absent.

- [x] **Step 3: Implement image and Compose integration**

The Dockerfile starts with:

```dockerfile
FROM golang:1.26.5 AS loopback-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal/loopback ./internal/loopback
COPY cmd/wktbox-loopback ./cmd/wktbox-loopback
RUN CGO_ENABLED=0 go build -trimpath -o /out/wktbox-loopback ./cmd/wktbox-loopback
```

The final image copies the binary and copies the entrypoint from the root
context:

```dockerfile
COPY --from=loopback-build /out/wktbox-loopback /usr/local/bin/wktbox-loopback
COPY --chmod=0755 images/webtop/entrypoint.sh /usr/local/bin/wktbox-entrypoint
```

The Compose service is:

```yaml
  loopback:
    image: ${WKTBOX_WEBTOP_IMAGE}
    command: ["wktbox-loopback", "serve"]
    network_mode: service:webtop
    depends_on:
      docker:
        condition: service_healthy
      webtop:
        condition: service_started
    environment:
      DOCKER_HOST: tcp://docker:2376
      DOCKER_TLS_VERIFY: "1"
      DOCKER_CERT_PATH: /certs/client
    volumes:
      - docker-certs:/certs:ro
    healthcheck:
      test: ["CMD", "wktbox-loopback", "status", "--json"]
      interval: 3s
      timeout: 3s
      retries: 30
```

Set Webtop `CUSTOM_PORT=61000`, `CUSTOM_HTTPS_PORT=61001`,
`CUSTOM_WS_PORT=61002`; map host HTTP/HTTPS to 61000/61001; delete the SSH
mapping. Change every Webtop image build to:

```bash
docker build -f images/webtop/Dockerfile -t wktbox/webtop:dev .
```

- [x] **Step 4: Run render tests and validate/build Compose**

Run:

```bash
rtk docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/sandbox -count=1
rtk docker build -f images/webtop/Dockerfile -t wktbox/webtop:dev .
```

Expected: tests PASS and image build succeeds with the embedded binary.

- [x] **Step 5: Commit the runtime slice**

```bash
rtk git add images/webtop/Dockerfile assets/sandbox.compose.yml \
  internal/sandbox/render_test.go Makefile .github/workflows/ci.yml \
  tests/integration/spike.sh tests/e2e/two_worktrees.sh
rtk git commit -m "feat: adiciona sidecar de loopback ao webtop"
```

### Task 6: Host Compose Control and Transient Box Status

**Files:**

- Modify: `internal/compose/client.go`
- Modify: `internal/compose/client_test.go`
- Modify: `internal/state/model.go`
- Modify: `internal/state/store_test.go`
- Modify: `internal/sandbox/manager.go`
- Modify: `internal/sandbox/manager_test.go`

**Interfaces:**

- Consumes: `loopback.Status` and sidecar control commands.
- Produces:
  `Backend.LoopbackStatus(context.Context, Project) (loopback.Status, error)`,
  `Backend.LoopbackSync(context.Context, Project) (loopback.Status, error)`,
  `Manager.SyncLoopback(context.Context, string) (loopback.Status, error)`, and
  transient `BoxRecord.Loopback loopback.Status` tagged `json:"-"`.

- [x] **Step 1: Write failing readiness, command, and persistence tests**

```go
func TestInspectRequiresHealthyLoopback(t *testing.T) {
    runner := &recordingRunner{results: []process.Result{{Stdout: `[
      {"Service":"docker","State":"running","Health":"healthy"},
      {"Service":"webtop","State":"running","Health":""},
      {"Service":"loopback","State":"running","Health":"starting"}
    ]`}}}
    got, err := compose.NewClient(runner).Inspect(context.Background(), testProject())
    if err != nil { t.Fatal(err) }
    if got.Ready() { t.Fatalf("status unexpectedly ready: %#v", got) }
}

func TestLoopbackSyncExecutesPrivateSidecarCommand(t *testing.T) {
    runner := &recordingRunner{results: []process.Result{{Stdout:
      `{"eventStream":"connected","routes":[{"port":5173,"state":"listening"}]}`}}}
    got, err := compose.NewClient(runner).LoopbackSync(context.Background(), testProject())
    if err != nil || got.Routes[0].Port != 5173 { t.Fatalf("got=%#v err=%v", got, err) }
    assertSuffix(t, runner.calls[0], "exec", "-T", "loopback",
      "wktbox-loopback", "sync", "--json")
}

func TestStoreDoesNotPersistTransientLoopbackStatus(t *testing.T) {
    record := existingRecord()
    record.Loopback = loopback.Status{EventStream: "connected"}
    // Save, read state.json, and assert it contains neither "loopback" nor
    // "eventStream".
}
```

Manager tests must assert `Ensure` and `Inspect` attach live status,
`SyncLoopback` delegates with the selected project, and unavailable sidecar
status becomes `eventStream: "unavailable"` on inspection without changing box
readiness to an error.

- [x] **Step 2: Run focused tests and verify RED**

Run:

```bash
rtk docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/compose ./internal/state ./internal/sandbox \
  -run 'Test(InspectRequiresHealthyLoopback|Loopback|StoreDoesNotPersist)' -count=1
```

Expected: compilation fails on missing methods and field.

- [x] **Step 3: Implement sidecar Compose calls and manager plumbing**

Readiness adds:

```go
case "loopback":
    loopbackReady = container.State == "running" && container.Health == "healthy"
```

`Ready` returns `dockerReady && webtopReady && loopbackReady`; managed recovery
uses the same three facts. Both control methods append:

```go
[]string{"exec", "-T", "loopback", "wktbox-loopback", command, "--json"}
```

and decode exactly one `loopback.Status`.

Add:

```go
type BoxRecord struct {
    // persisted fields unchanged
    Loopback loopback.Status `json:"-"`
}
```

`Manager.Inspect` queries Compose status first, derives the box status, and then
queries `LoopbackStatus` only while Compose is running. A query error installs
`loopback.Unavailable(err)` in the returned transient field. `Ensure` performs
the same attachment after readiness; `SyncLoopback` is explicit and returns an
error because successful post-command synchronization is required.

- [x] **Step 4: Run Compose/state/sandbox tests and verify GREEN**

Run:

```bash
rtk docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 \
  go test -race ./internal/compose ./internal/state ./internal/sandbox -count=1
```

Expected: PASS with no race report.

- [x] **Step 5: Commit host control plumbing**

```bash
rtk git add internal/compose internal/state internal/sandbox
rtk git commit -m "feat: integra status do loopback a box"
```

### Task 7: CLI Route Status and Post-Command Sync

**Files:**

- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`
- Modify: `internal/output/output.go`
- Modify: `internal/output/output_test.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`

**Interfaces:**

- Consumes: `Manager.SyncLoopback` and transient box status from Task 6.
- Produces:
  `App.SyncLoopback(context.Context, state.BoxRecord)`,
  `Renderer.LoopbackSummary(loopback.Status)`, status JSON field `loopback`, and
  post-success sync behavior.

- [x] **Step 1: Write failing output and command-behavior tests**

```go
func TestRunSyncsLoopbackAfterChildAndPreservesStdout(t *testing.T) {
    service := &fakeService{runCode: 0, syncStatus: loopback.Status{
        EventStream: "connected",
        Routes: []loopback.Route{{Port: 5173, Target: "docker:5173", State: "listening"}},
    }}
    stdout, stderr := executeCLI(t, service, "run", "--", "printf", "child-output")
    if stdout != "child-output" { t.Fatalf("stdout=%q", stdout) }
    if service.syncCalls != 1 { t.Fatalf("sync calls=%d", service.syncCalls) }
    if !strings.Contains(stderr, "http://localhost:5173") { t.Fatalf("stderr=%q", stderr) }
}

func TestExecDoesNotSyncLoopback(t *testing.T) {
    service := &fakeService{box: readyBox()}
    executeCLI(t, service, "exec", "--", "true")
    if service.syncCalls != 0 { t.Fatalf("sync calls=%d", service.syncCalls) }
}

func TestFailedChildDoesNotReplaceItsExitCodeWithSyncError(t *testing.T) {
    service := &fakeService{runCode: 23}
    err := executeCLIError(t, service, "compose", "up", "-d")
    assertExitCode(t, err, 23)
    if service.syncCalls != 0 { t.Fatalf("sync calls=%d", service.syncCalls) }
}
```

Output tests cover JSON status, conflict rows, UDP warnings, HTTP/HTTPS/tcp
classification, JSON stderr, and quiet suppression.

- [x] **Step 2: Run app/output/CLI tests and verify RED**

Run:

```bash
rtk docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/app ./internal/output ./internal/cli \
  -run 'Test(RunSyncsLoopback|ExecDoesNotSync|FailedChild|Loopback)' -count=1
```

Expected: compilation fails on missing sync and renderer APIs.

- [x] **Step 3: Implement service and output behavior**

Extend interfaces with:

```go
SyncLoopback(context.Context, state.BoxRecord) (loopback.Status, error)
```

Make `child` accept a `syncLoopback bool`: `run` and `compose` pass true;
`exec` passes false. After child code zero:

```go
status, err := commands.service.SyncLoopback(ctx, box)
if err != nil { return err }
if err := commands.renderer().LoopbackSummary(status); err != nil { return err }
return nil
```

`LoopbackSummary` writes only to the renderer's stderr. Human lines use HTTP
for `80,3000,4173,5000,5173,8000,8080`, HTTPS for `443,8443`, and
`localhost:PORT -> docker:PORT` otherwise. JSON emits:

```json
{"loopback":{"eventStream":"connected","routes":[{"port":5173,"state":"listening"}]}}
```

`BoxData` includes the same stable `loopback` object. `--quiet` suppresses only
the diagnostic summary, never child streams.

- [x] **Step 4: Run app/output/CLI tests and verify GREEN**

Run:

```bash
rtk docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 \
  go test -race ./internal/app ./internal/output ./internal/cli -count=1
```

Expected: PASS with no race report.

- [x] **Step 5: Commit CLI behavior**

```bash
rtk git add internal/app internal/output internal/cli
rtk git commit -m "feat: mostra rotas automaticas na cli"
```

### Task 8: Mandatory Loopback E2E Fixture

**Files:**

- Modify: `tests/fixtures/compose-project/compose.yml`
- Modify: `tests/fixtures/compose-project/service.py`
- Create: `tests/fixtures/compose-project/public/loopback.html`
- Create: `tests/e2e/automatic_loopback.sh`
- Modify: `Makefile`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**

- Consumes: built `wktbox`, Webtop image, loopback status JSON.
- Produces: repeatable black-box acceptance covering every scenario in design
  section 13.3.

- [x] **Step 1: Change the fixture and write the failing E2E script**

Use differing backend ports:

```yaml
  backend:
    command: ["python", "service.py", "http", "3000"]
    ports: ["8000:3000"]
```

Add a WebSocket echo mode to `service.py` that performs the RFC 6455 handshake,
unmasks one client text frame, and sends the same payload back. The browser
fixture opens `ws://localhost:5173/ws`, writes `wktbox-websocket-ok` into
`#websocket-result`, and serves normal HTTP content from the same container.

The E2E script must implement named assertions for:

1. no `.wktbox.yml` gateway configuration;
2. `5173:5173` reachable at Webtop `localhost:5173`;
3. `8000:3000` reachable at Webtop `localhost:8000`;
4. raw TCP `5432:5432`;
5. Chromium headless HTTP and WebSocket success;
6. late container publication added without sidecar restart;
7. stopped/removed publication disappears;
8. sidecar restart rehydrates all routes;
9. inner DinD container recreation changes its IP and new connections recover;
10. two boxes expose the same ports without cross-talk;
11. port 61000 appears as `conflict` while other routes remain listening;
12. clean shutdown leaves no listener/process;
13. outer `docker ps` shows no application host publications and no host Docker
    socket mount.

Every polling loop has a 60-second deadline and prints loopback status plus
sidecar logs before failing.

- [x] **Step 2: Run the E2E and verify RED before the full integration is fixed**

Run:

```bash
rtk make build images
rtk bash tests/e2e/automatic_loopback.sh
```

Expected before Tasks 5–7 are green: failure at the first automatic localhost
assertion or missing loopback status.

- [x] **Step 3: Complete the E2E implementation and CI target**

Use commands inside Webtop such as:

```bash
wktbox --path "$worktree_a" exec -- curl --fail --silent \
  http://localhost:8000
wktbox --path "$worktree_a" exec -- \
  chromium --headless --no-sandbox --disable-gpu \
  --virtual-time-budget=5000 --dump-dom http://localhost:5173/public/loopback.html
```

Create/remove late services through inner `docker run --publish`, restart only
`wktbox-$id-loopback-1`, recreate only `wktbox-$id-docker-1`, and query
`wktbox --json status` after each transition. Inspect external container mounts
and ports with `docker inspect`, comparing exact box labels.

Make `make e2e` run both `automatic_loopback.sh` and the existing isolation
suite. Add the automatic suite to Linux CI because privileged DinD is required.

- [x] **Step 4: Run the mandatory E2E twice**

Run:

```bash
rtk bash tests/e2e/automatic_loopback.sh
rtk bash tests/e2e/automatic_loopback.sh
```

Expected: both runs end with `wktbox automatic loopback E2E passed`.

- [x] **Step 5: Commit acceptance coverage**

```bash
rtk git add tests/fixtures/compose-project tests/e2e/automatic_loopback.sh \
  Makefile .github/workflows/ci.yml
rtk git commit -m "feat: valida loopback automatico ponta a ponta"
```

### Task 9: Documentation and Full Verification

**Files:**

- Modify: `README.md`
- Modify: `docs/configuration.md`
- Modify: `docs/security.md`
- Modify: `docs/windows.md`
- Modify: `docs/superpowers/plans/2026-07-23-automatic-webtop-loopback.md`

**Interfaces:**

- Consumes: completed runtime behavior.
- Produces: accurate user documentation and a checked implementation plan.

- [x] **Step 1: Write documentation assertions before prose changes**

Add/adjust `tests/docs_test.go` expectations for these exact concepts:

```go
[]string{
    "localhost:5173",
    "published port",
    "wktbox status",
    "conflict",
    "UDP",
    "docker build -f images/webtop/Dockerfile",
}
```

- [x] **Step 2: Run documentation tests and verify RED**

Run:

```bash
rtk docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./tests -run TestDocumentation -count=1
```

Expected: FAIL on missing automatic loopback documentation.

- [x] **Step 3: Document behavior, diagnostics, security, and platforms**

Document that inner Compose `ports:` is the source of truth; the published host
port is reused in Webtop; additions/removals are automatic; `wktbox status`
shows listening/conflict/UDP state; only loopback is bound; DinD remains
isolated behind TLS; no host Docker socket is mounted; the gateway remains an
optional host-facing convenience. State explicitly that the sidecar requires
Linux network namespaces and Docker Desktop's Linux-container backend on
Windows/macOS.

- [x] **Step 4: Run the complete verification matrix**

Run:

```bash
rtk make fmt
rtk git diff --check
rtk make test
rtk make test-race
rtk make vet
rtk make build
rtk make images
rtk bash tests/integration/spike.sh
rtk bash tests/e2e/automatic_loopback.sh
rtk bash tests/e2e/two_worktrees.sh
```

Expected: every command exits zero, race output contains no warnings, and both
E2E scripts print their success line.

- [x] **Step 5: Audit spec coverage and commit documentation**

Confirm the 13 E2E names appear in `automatic_loopback.sh`, scan for
placeholders, inspect image/Compose output, and verify `state.json` has no
loopback field:

```bash
rtk rg -n 'TBD|TODO|FIXME|implement later|appropriate|as needed' \
  docs/superpowers/plans/2026-07-23-automatic-webtop-loopback.md \
  internal cmd tests
rtk git diff --check
rtk git status --short
```

Then commit:

```bash
rtk git add README.md docs tests/docs_test.go \
  docs/superpowers/plans/2026-07-23-automatic-webtop-loopback.md
rtk git commit -m "feat: documenta loopback automatico"
```

### Task 10: Final Repository Audit

**Files:**

- Review: all files changed by Tasks 1–9.

**Interfaces:**

- Consumes: all completed slices.
- Produces: a clean, reproducible, release-ready branch.

- [x] **Step 1: Inspect commits and worktree**

```bash
rtk git log --oneline --decorate -12
rtk git status --short --branch
rtk git diff main...HEAD --stat
```

Expected: only scoped commits exist and the worktree is clean.

- [x] **Step 2: Re-run the fast release gate from a clean state**

```bash
rtk make test
rtk make vet
rtk make build
rtk git diff --check
```

Expected: all commands exit zero.

- [x] **Step 3: Inspect final runtime artifacts**

```bash
rtk docker run --rm wktbox/webtop:dev wktbox-loopback --help
rtk docker image inspect wktbox/webtop:dev \
  --format '{{json .Config.Entrypoint}} {{json .Config.Labels}}'
```

Expected: the helper is executable and the original Webtop entrypoint/labels
remain present.

- [x] **Step 4: Record durable architecture memory**

Add one consolidated Graphiti memory for project `wktbox` describing the
required sidecar, shared Webtop namespace, DinD TLS discovery, published-host
port rule, control socket, CLI sync points, transient status, high Webtop
internal ports, readiness rule, and relevant files.

- [ ] **Step 5: Hand off the completed implementation**

Report the concrete outcome, final commit, verification commands and results,
E2E scenarios, and any external prerequisite that remains for the user's local
test. Do not claim completion unless the verification evidence is fresh.
