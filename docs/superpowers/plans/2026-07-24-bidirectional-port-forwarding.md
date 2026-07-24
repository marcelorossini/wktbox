# Bidirectional Port Forwarding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement persistent, atomic `wktbox port import` and `wktbox port publish` commands, preserving real-host ports as `localhost` in every inner workload and exposing requested DinD ports on real-host loopback.

**Architecture:** Store desired mappings on each box, render protected relay configuration and a Compose override, and reconcile runtime state under the existing box lock. A token-authenticated native host relay serves imports; the loopback sidecar enters workload network namespaces for container-local listeners; a separate outer `portbridge` service serves host-facing publications.

**Tech Stack:** Go 1.26, Cobra, Docker Engine API, Docker Compose v2, Linux network namespaces through `golang.org/x/sys/unix`, JSON state/configuration, YAML overrides, shell E2E.

## Global Constraints

- TCP only; UDP mappings return an explicit unsupported error.
- Positional imports preserve the port number and use real-host `127.0.0.1`.
- Publications bind only `127.0.0.1` or `::1`.
- A batch is atomic: any parse, bind, namespace, activation, or persistence failure restores the previous state and runtime.
- Existing workload-local conflicts fail the command before mutation.
- A future conflicting workload is stopped and reported as `port_import_conflict`.
- Ordinary DinD publications remain private to the box.
- No host Docker socket is mounted into a box.
- Native and generated secrets are never emitted by human or JSON output.
- Tests and builds run through the repository Makefile's Docker-based Go toolchain because the repository is Wktbox itself and cannot be nested in Wktbox.

---

### Task 1: Mapping Domain, Parsing, and Persistent State

**Files:**
- Create: `internal/portforward/model.go`
- Create: `internal/portforward/parse.go`
- Test: `internal/portforward/model_test.go`
- Test: `internal/portforward/parse_test.go`
- Modify: `internal/state/model.go`
- Modify: `internal/state/store_test.go`
- Modify: `internal/ports/allocator.go`
- Modify: `internal/ports/allocator_test.go`

**Interfaces:**
- Produces: `portforward.Mapping`, `portforward.Direction`, `portforward.ParseImports`, `portforward.ParsePublications`, `portforward.ValidateSet`, `ports.Block.ImportRelay`.
- Consumes: standard `net/netip`, `strconv`, `regexp`, and the existing version-1 JSON state.

- [ ] **Step 1: Write failing parser and validation tests**

```go
func TestParsePositionalImportsPreservesPorts(t *testing.T) {
    got, err := portforward.ParseImports([]string{"1234", "5432"}, nil)
    if err != nil { t.Fatal(err) }
    want := []portforward.Mapping{
        {Name: "import-1234", Direction: portforward.Import, SourceAddress: "127.0.0.1", SourcePort: 1234, TargetPort: 1234},
        {Name: "import-5432", Direction: portforward.Import, SourceAddress: "127.0.0.1", SourcePort: 5432, TargetPort: 5432},
    }
    if diff := cmp.Diff(want, got); diff != "" { t.Fatal(diff) }
}

func TestParseNamedImportAndPublicationMaps(t *testing.T) {
    imports, err := portforward.ParseImports(nil, []string{"redis=127.0.0.1:6379:16379"})
    if err != nil { t.Fatal(err) }
    publications, err := portforward.ParsePublications([]string{"api=127.0.0.1:18000:8000"})
    if err != nil { t.Fatal(err) }
    if imports[0].TargetPort != 16379 || publications[0].SourcePort != 18000 {
        t.Fatalf("imports=%#v publications=%#v", imports, publications)
    }
}

func TestValidateSetRejectsDuplicateNamesAndPublicationListeners(t *testing.T) {
    mappings := []portforward.Mapping{
        {Name: "api", Direction: portforward.Import, SourceAddress: "127.0.0.1", SourcePort: 3000, TargetPort: 3000},
        {Name: "api", Direction: portforward.Publish, SourceAddress: "127.0.0.1", SourcePort: 18000, TargetPort: 8000},
    }
    if err := portforward.ValidateSet(mappings); err == nil || !strings.Contains(err.Error(), `duplicate mapping name "api"`) {
        t.Fatalf("error=%v", err)
    }
}
```

- [ ] **Step 2: Run the domain tests and verify RED**

Run:

```text
docker run --rm --user $(id -u):$(id -g) -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod -v "$PWD:/src" -w /src golang:1.26.5 go test ./internal/portforward ./internal/state ./internal/ports
```

Expected: FAIL because `internal/portforward` and `Block.ImportRelay` do not exist.

- [ ] **Step 3: Implement the minimal domain**

```go
type Direction string

const (
    Import  Direction = "import"
    Publish Direction = "publish"
)

type Mapping struct {
    Name          string    `json:"name"`
    Direction     Direction `json:"direction"`
    SourceAddress string    `json:"sourceAddress"`
    SourcePort    uint16    `json:"sourcePort"`
    TargetPort    uint16    `json:"targetPort"`
    CreatedAt     time.Time `json:"createdAt"`
}

func (block Block) ImportRelay() int { return block.Start + 4 }
```

Parse `name=address:source:target` with `net.SplitHostPort` applied to
`address:source`, validate all ports in `1..65535`, validate names with
`^[a-z0-9][a-z0-9._-]{0,62}$`, sort by direction then name, and reject
duplicate names or duplicate publication listeners.

Add `PortMappings []portforward.Mapping` to `state.BoxRecord` with
`json:"portMappings,omitempty"` and prove old state JSON still loads with an
empty slice.

- [ ] **Step 4: Run tests and verify GREEN**

Run the Task 1 command again. Expected: PASS for `portforward`, `state`, and
`ports`.

- [ ] **Step 5: Commit**

```text
git add internal/portforward internal/state/model.go internal/state/store_test.go internal/ports
git commit -m "feat: modelar encaminhamentos de portas"
```

---

### Task 2: Protected Runtime Files and Compose Port Bridge

**Files:**
- Create: `internal/portforward/render.go`
- Test: `internal/portforward/render_test.go`
- Modify: `internal/sandbox/spec.go`
- Modify: `internal/sandbox/render.go`
- Modify: `internal/sandbox/render_test.go`
- Modify: `internal/sandbox/manager.go`
- Modify: `internal/compose/client.go`
- Modify: `internal/compose/client_test.go`
- Modify: `assets/sandbox.compose.yml`

**Interfaces:**
- Consumes: sorted `[]portforward.Mapping` and `sandbox.Spec`.
- Produces: `portforward.RenderConfig`, `portforward.RenderComposeOverride`, `sandbox.Files.PortConfigPath`, `sandbox.Files.PortOverridePath`, and `compose.Project.PortMappingsEnabled`.

- [ ] **Step 1: Write failing rendering and Compose argument tests**

```go
func TestRenderComposeOverridePublishesOnlyExplicitLoopbackMappings(t *testing.T) {
    got, err := portforward.RenderComposeOverride([]portforward.Mapping{
        {Name: "api", Direction: portforward.Publish, SourceAddress: "127.0.0.1", SourcePort: 18000, TargetPort: 8000},
        {Name: "postgres", Direction: portforward.Import, SourceAddress: "127.0.0.1", SourcePort: 5432, TargetPort: 5432},
    })
    if err != nil { t.Fatal(err) }
    text := string(got)
    if !strings.Contains(text, `"127.0.0.1:18000:18000"`) || strings.Contains(text, "5432:5432") {
        t.Fatalf("override:\n%s", text)
    }
}

func TestProjectIncludesPortOverrideAndProfile(t *testing.T) {
    runner := &recordingRunner{}
    client := compose.NewClient(runner)
    project := compose.Project{Name: "wktbox-a4f8c9137d2b", Files: []string{"compose.yml", "ports.override.yml"}, EnvFile: "sandbox.env", PortMappingsEnabled: true}
    if err := client.Up(context.Background(), project); err != nil { t.Fatal(err) }
    want := []string{"compose", "-p", "wktbox-a4f8c9137d2b", "--env-file", "sandbox.env", "-f", "compose.yml", "-f", "ports.override.yml", "--profile", "ports", "up", "-d", "--wait"}
    if diff := cmp.Diff(want, runner.arguments); diff != "" { t.Fatal(diff) }
}
```

- [ ] **Step 2: Run targeted tests and verify RED**

Run:

```text
make test
```

Expected: FAIL on missing rendering functions and missing project fields.

- [ ] **Step 3: Implement protected files and the base service**

Render `ports.json` as stable indented JSON plus newline. Render
`ports.override.yml` with:

```yaml
services:
  portbridge:
    ports:
      - "127.0.0.1:18000:18000"
```

Extend `sandbox.Files.ComposeFiles()` and `projectFor` to include the override
only when mappings exist. Write both files through the existing
`writeProtected` atomic helper.

Add this service to `assets/sandbox.compose.yml`:

```yaml
  portbridge:
    image: ${WKTBOX_WEBTOP_IMAGE}
    profiles: ["ports"]
    command: ["wktbox-loopback", "publish-serve"]
    depends_on:
      docker:
        condition: service_healthy
    environment:
      DOCKER_HOST: tcp://docker:2376
      DOCKER_TLS_VERIFY: "1"
      DOCKER_CERT_PATH: /certs/client
      WKTBOX_PORT_CONFIG: /run/wktbox-ports/ports.json
    volumes:
      - docker-certs:/certs:ro
      - type: bind
        source: ${PORT_RUNTIME_PATH}
        target: /run/wktbox-ports
        read_only: true
```

Mount the same config into `loopback`, add `pid: service:docker`,
`cap_add: [SYS_ADMIN, SYS_PTRACE]`, and the sidecar-specific
`security_opt: [apparmor=unconfined]` required for `setns`. Resolve
`host.docker.internal` when present and fall back to the Linux default gateway;
`extra_hosts` cannot be combined with `network_mode: service:webtop`. Update
security render tests to prove the host Docker socket remains absent.

- [ ] **Step 4: Run tests and verify GREEN**

Run `make test`. Expected: every package PASS.

- [ ] **Step 5: Commit**

```text
git add internal/portforward internal/sandbox internal/compose assets/sandbox.compose.yml
git commit -m "feat: renderizar infraestrutura de portas"
```

---

### Task 3: Authenticated Native Host Relay

**Files:**
- Create: `internal/portforward/protocol.go`
- Create: `internal/portforward/relay.go`
- Create: `internal/portforward/relay_process.go`
- Create: `internal/portforward/relay_process_unix.go`
- Create: `internal/portforward/relay_process_windows.go`
- Test: `internal/portforward/protocol_test.go`
- Test: `internal/portforward/relay_test.go`
- Test: `internal/portforward/relay_process_test.go`
- Modify: `cmd/wktbox/main.go`
- Modify: `cmd/wktbox/main_test.go`

**Interfaces:**
- Produces: `portforward.Relay`, `portforward.EnsureRelay`, `portforward.StopRelay`, and hidden `__port-relay` process mode.
- Consumes: `ports.json`, a 256-bit token file, PID path, and `ports.Block.ImportRelay()`.

- [ ] **Step 1: Write failing protocol and relay tests**

```go
func TestRelayAuthenticatesAndCopiesTCP(t *testing.T) {
    target := startEchoServer(t)
    relay := portforward.NewRelay(portforward.RelayOptions{
        Token: []byte("01234567890123456789012345678901"),
        Resolve: func(name string) (string, bool) {
            return target.Addr().String(), name == "echo"
        },
    })
    relayAddress := startRelay(t, relay)
    connection := dialRelay(t, relayAddress, "echo", relay.Token())
    _, _ = connection.Write([]byte("hello"))
    assertRead(t, connection, "hello")
}

func TestRelayRejectsInvalidTokenWithoutOpeningTarget(t *testing.T) {
    opened := false
    relay := portforward.NewRelay(portforward.RelayOptions{
        Token: validToken,
        Dial: func(context.Context, string, string) (net.Conn, error) {
            opened = true
            return nil, errors.New("unexpected")
        },
    })
    relayAddress := startRelay(t, relay)
    connection, err := net.Dial("tcp", relayAddress)
    if err != nil { t.Fatal(err) }
    if _, err := connection.Write(portforward.EncodePreamble("echo", bytes.Repeat([]byte{0xff}, 32))); err != nil { t.Fatal(err) }
    _ = connection.SetReadDeadline(time.Now().Add(time.Second))
    if _, err := io.ReadAll(connection); err == nil { t.Fatal("expected authenticated close") }
    if opened { t.Fatal("target was opened before authentication") }
}
```

- [ ] **Step 2: Run relay tests and verify RED**

Run:

```text
docker run --rm --user $(id -u):$(id -g) -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod -v "$PWD:/src" -w /src golang:1.26.5 go test ./internal/portforward ./cmd/wktbox -run 'Relay|HiddenPort'
```

Expected: FAIL because the relay interfaces do not exist.

- [ ] **Step 3: Implement the protocol and detached process**

Use a bounded binary preamble:

```text
magic "WKTP" | version byte 1 | token[32] | nameLength uint16 | mappingName
```

Reject names longer than 63 bytes and set a five-second preamble deadline.
After authentication, dial the configured real-host loopback target and copy
both directions with two goroutines and half-close when supported.

Before creating the normal application, `cmd/wktbox/main.go` recognizes:

```go
if len(os.Args) > 1 && os.Args[1] == "__port-relay" {
    os.Exit(runPortRelay(os.Args[2:], os.Stderr))
}
```

`EnsureRelay` writes a cryptographically random token with `0600`, starts the
same executable detached, records its PID atomically, and waits for an
authenticated readiness response. Unix uses `Setsid: true`; Windows uses
`CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS`. `StopRelay` verifies the
authenticated relay before killing the recorded PID so a recycled PID is never
terminated blindly.

- [ ] **Step 4: Run tests and verify GREEN**

Run the Task 3 test command, then `make test`. Expected: PASS.

- [ ] **Step 5: Commit**

```text
git add internal/portforward cmd/wktbox
git commit -m "feat: adicionar relay autenticado do host"
```

---

### Task 4: Workload Namespace Import Reconciler

**Files:**
- Create: `internal/loopback/imports.go`
- Create: `internal/loopback/netns_linux.go`
- Create: `internal/loopback/netns_unsupported.go`
- Test: `internal/loopback/imports_test.go`
- Test: `internal/loopback/netns_linux_test.go`
- Modify: `internal/loopback/model.go`
- Modify: `internal/loopback/source.go`
- Modify: `internal/loopback/docker.go`
- Modify: `internal/loopback/docker_test.go`
- Modify: `internal/loopback/daemon.go`
- Modify: `internal/loopback/daemon_test.go`
- Modify: `internal/loopback/control.go`
- Modify: `cmd/wktbox-loopback/main.go`
- Modify: `cmd/wktbox-loopback/main_test.go`

**Interfaces:**
- Consumes: workload ID/name/PID from inner Docker inspect, active import mappings, host relay address/token.
- Produces: `loopback.ImportReconciler.Preflight`, `Apply`, `Status`, and child mode `import-proxy`.

- [ ] **Step 1: Write failing transactional reconciler tests**

```go
func TestImportPreflightRejectsOneConflictWithoutStartingAnyProxy(t *testing.T) {
    namespaces := &fakeNamespaces{conflicts: map[int]map[uint16]bool{42: {1234: true}}}
    reconciler := loopback.NewImportReconciler(loopback.ImportOptions{Namespaces: namespaces})
    workloads := []loopback.Container{{ID: "a", Name: "api", PID: 42, Running: true}}
    err := reconciler.Preflight(context.Background(), workloads, []portforward.Mapping{{
        Name: "host-api", Direction: portforward.Import, SourcePort: 1234, TargetPort: 1234,
    }})
    if err == nil || !strings.Contains(err.Error(), "api") || len(namespaces.started) != 0 {
        t.Fatalf("error=%v starts=%#v", err, namespaces.started)
    }
}

func TestFutureConflictStopsWorkloadAndKeepsDesiredImport(t *testing.T) {
    source := &fakeImportSource{containers: []loopback.Container{{ID: "late", Name: "late", PID: 77, Running: true}}}
    namespaces := &fakeNamespaces{conflicts: map[int]map[uint16]bool{77: {1234: true}}}
    imports := loopback.NewImportReconciler(loopback.ImportOptions{Namespaces: namespaces, Source: source})
    imports.SetDesired([]portforward.Mapping{{Name: "host-api", Direction: portforward.Import, SourcePort: 1234, TargetPort: 1234}})
    if _, err := imports.Apply(context.Background(), source.containers, loopback.ApplyEvent); err != nil { t.Fatal(err) }
    if diff := cmp.Diff([]string{"late"}, source.stopped); diff != "" { t.Fatal(diff) }
    status := imports.Status()
    if len(status.Warnings) != 1 || status.Warnings[0].Code != "port_import_conflict" { t.Fatalf("status=%#v", status) }
    if len(imports.Desired()) != 1 || imports.Desired()[0].Name != "host-api" { t.Fatalf("desired=%#v", imports.Desired()) }
}
```

- [ ] **Step 2: Run loopback tests and verify RED**

Run:

```text
docker run --rm --user $(id -u):$(id -g) -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod -v "$PWD:/src" -w /src golang:1.26.5 go test ./internal/loopback ./cmd/wktbox-loopback -run 'Import|NetNS|FutureConflict'
```

Expected: FAIL on missing import interfaces and `Container.PID`.

- [ ] **Step 3: Implement namespace probes and child proxies**

Extend `loopback.Container` with PID and labels. Extend `Source` with
`Stop(context.Context, id string) error`, backed by Docker
`ContainerStop(..., timeout=10)`.

On Linux, lock the OS thread, open `/proc/<pid>/ns/net`, call
`unix.Setns(fd, unix.CLONE_NEWNET)`, then bind both loopback families. The
long-lived child mode enters once and starts the listeners before reporting
readiness to its parent over a pipe. Unsupported platforms return an explicit
error; the sidecar image itself remains Linux.

The parent keeps one child per `(containerID, mappingName)`, preflights all
workloads before starting any child, starts the full candidate set, and closes
the old set only after the candidate is ready. Event reconciliation excludes
containers labeled `io.wktbox.port-helper=true`.

If a container appearing after activation conflicts, call `Source.Stop`, add:

```go
Warning{
    Code: "port_import_conflict",
    Source: container.Name,
    Port: mapping.TargetPort,
    Message: "workload stopped because localhost port is reserved by import " + mapping.Name,
}
```

and do not remove the mapping from desired state.

- [ ] **Step 4: Run tests and verify GREEN**

Run the Task 4 test command and `make test`. Expected: PASS.

- [ ] **Step 5: Commit**

```text
git add internal/loopback cmd/wktbox-loopback
git commit -m "feat: importar portas no localhost dos workloads"
```

---

### Task 5: Publication Server and Atomic Manager Operations

**Files:**
- Create: `internal/loopback/publish.go`
- Test: `internal/loopback/publish_test.go`
- Create: `internal/sandbox/ports.go`
- Test: `internal/sandbox/ports_test.go`
- Modify: `internal/sandbox/manager.go`
- Modify: `internal/sandbox/manager_test.go`
- Modify: `internal/compose/client.go`
- Modify: `internal/compose/client_test.go`
- Modify: `cmd/wktbox-loopback/main.go`
- Modify: `cmd/wktbox-loopback/main_test.go`

**Interfaces:**
- Produces: `loopback.PublishServer`, manager `ImportPorts`, `PublishPorts`, `PortMappings`, `RemovePortMappings`.
- Consumes: candidate rendered files, relay lifecycle functions, Compose `portbridge`, and loopback import preflight/apply commands.

- [ ] **Step 1: Write failing publication and rollback tests**

```go
func TestPublishServerForwardsConfiguredPortToDinD(t *testing.T) {
    target := startEchoServer(t)
    server := loopback.NewPublishServer([]loopback.PublishRoute{{
        ListenPort: 18000,
        Target: target.Addr().String(),
    }})
    address := startPublishServer(t, server)
    assertEcho(t, address, "published")
}

func TestImportBatchConflictLeavesStateAndFilesUnchanged(t *testing.T) {
    fixture := newPortManagerFixture(t)
    fixture.backend.preflightErr = errors.New("api already uses localhost:1234")
    beforeState := fixture.readState()
    beforeConfig := fixture.readPortConfig()
    _, err := fixture.manager.ImportPorts(context.Background(), fixture.boxID, candidateImports)
    if err == nil { t.Fatal("expected conflict") }
    if diff := cmp.Diff(beforeState, fixture.readState()); diff != "" { t.Fatal(diff) }
    if diff := cmp.Diff(beforeConfig, fixture.readPortConfig()); diff != "" { t.Fatal(diff) }
    if len(fixture.backend.applyCalls) != 0 { t.Fatalf("apply calls=%#v", fixture.backend.applyCalls) }
}

func TestPublicationActivationFailureRollsBackComposeAndState(t *testing.T) {
    fixture := newPortManagerFixture(t)
    fixture.backend.portApplyErr = errors.New("bind address already in use")
    beforeState := fixture.readState()
    beforeOverride := fixture.readPortOverride()
    _, err := fixture.manager.PublishPorts(context.Background(), fixture.boxID, candidatePublications)
    if err == nil { t.Fatal("expected activation error") }
    if diff := cmp.Diff(beforeState, fixture.readState()); diff != "" { t.Fatal(diff) }
    if diff := cmp.Diff(beforeOverride, fixture.readPortOverride()); diff != "" { t.Fatal(diff) }
    if fixture.backend.portApplyCalls != 2 { t.Fatalf("apply calls=%d, want candidate plus rollback", fixture.backend.portApplyCalls) }
}
```

- [ ] **Step 2: Run targeted tests and verify RED**

Run:

```text
docker run --rm --user $(id -u):$(id -g) -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod -v "$PWD:/src" -w /src golang:1.26.5 go test ./internal/loopback ./internal/sandbox ./internal/compose -run 'Publish|Port|Rollback|ImportBatch'
```

Expected: FAIL on missing publication server and manager methods.

- [ ] **Step 3: Implement publication serving and manager transactions**

`publish-serve` reads `WKTBOX_PORT_CONFIG`, listens on each publication's
source port inside `portbridge`, and forwards to `docker:<targetPort>`.

Add backend methods:

```go
PortImportPreflight(context.Context, Project, string) error
PortImportApply(context.Context, Project) (portforward.Status, error)
PortRuntimeApply(context.Context, Project) error
```

Compose preflights host publication listeners with `net.ListenConfig` before
`up`. Manager mutations hold `lockBox`, merge candidate mappings, validate the
whole set, render candidate files, preflight imports and publications, ensure
the host relay, apply Compose, apply imports, then save state.

Capture previous state and file bytes before mutation. On any activation or
save failure, atomically restore those bytes, reapply the previous Compose and
imports, stop an unneeded candidate relay, and return `errors.Join` if rollback
also fails.

`Stop` stops the relay after Compose stops. `Restart` and `Ensure` reconcile
port runtime after readiness. `Destroy` stops the relay before removing the box
directory.

- [ ] **Step 4: Run tests and verify GREEN**

Run the Task 5 test command and `make test`. Expected: PASS.

- [ ] **Step 5: Commit**

```text
git add internal/loopback internal/sandbox internal/compose cmd/wktbox-loopback
git commit -m "feat: publicar portas e garantir transacoes atomicas"
```

---

### Task 6: Application, CLI, Human/JSON Output

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`
- Modify: `internal/output/output.go`
- Modify: `internal/output/output_test.go`
- Modify: `tests/integration/cli_test.go`

**Interfaces:**
- Consumes: manager port operations and parser functions.
- Produces: public CLI commands and stable JSON mapping output.

- [ ] **Step 1: Write failing CLI behavior tests**

```go
func TestPortImportParsesBatchAndRequiresReadyBox(t *testing.T) {
    fake := newFakeService()
    root := cli.New(cli.Dependencies{Service: fake}, testStreams())
    root.SetArgs([]string{"port", "import", "1234", "5432"})
    if err := root.Execute(); err != nil { t.Fatal(err) }
    assertCalls(t, fake.calls, "Resolve:.", "Inspect", "ImportPorts:import-1234|import-5432")
}

func TestPortPublishAcceptsRepeatedMaps(t *testing.T) {
    fake := newFakeService()
    root := cli.New(cli.Dependencies{Service: fake}, testStreams())
    root.SetArgs([]string{
        "port", "publish",
        "--map", "frontend=127.0.0.1:15173:5173",
        "--map", "api=127.0.0.1:18000:8000",
    })
    if err := root.Execute(); err != nil { t.Fatal(err) }
    assertCalls(t, fake.calls, "Resolve:.", "Inspect", "PublishPorts:frontend|api")
}

func TestPortListJSONNeverContainsRelayToken(t *testing.T) {
    fake := newFakeService()
    fake.portMappings = []portforward.ObservedMapping{{
        Mapping: portforward.Mapping{Name: "api", Direction: portforward.Publish, SourceAddress: "127.0.0.1", SourcePort: 18000, TargetPort: 8000},
        State: portforward.Ready,
    }}
    streams := testStreams()
    root := cli.New(cli.Dependencies{Service: fake}, streams)
    root.SetArgs([]string{"--json", "port", "list"})
    if err := root.Execute(); err != nil { t.Fatal(err) }
    output := streams.Out.(*bytes.Buffer).String()
    for _, forbidden := range []string{"token", "pid", "01234567890123456789012345678901"} {
        if strings.Contains(output, forbidden) { t.Fatalf("secret field %q in %s", forbidden, output) }
    }
}
```

- [ ] **Step 2: Run CLI tests and verify RED**

Run:

```text
docker run --rm --user $(id -u):$(id -g) -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod -v "$PWD:/src" -w /src golang:1.26.5 go test ./internal/app ./internal/cli ./internal/output ./tests/integration -run 'Port'
```

Expected: FAIL because the service and commands are absent.

- [ ] **Step 3: Implement app and commands**

Add application methods that take `app.Resolution`, reject any box not
`state.Ready` with an actionable `wktbox up` error, and delegate by box ID.

Register `commands.portCommands()` containing:

```text
port import
port publish
port list
port remove
```

Use repeatable `--map`, reject positional arguments together with `--map`, and
render the manager result after successful mutation. `remove` accepts one or
more names or exactly one all-direction flag.

Human output uses the table in the design. JSON emits:

```json
{
  "name": "api",
  "direction": "publish",
  "source": "127.0.0.1:18000",
  "target": "docker:8000",
  "state": "ready"
}
```

- [ ] **Step 4: Run tests and verify GREEN**

Run the Task 6 command and `make test`. Build with `make build`, then run:

```text
./bin/wktbox port --help
./bin/wktbox port import --help
./bin/wktbox port publish --help
```

Expected: all tests/build PASS and help shows distinct directional commands.

- [ ] **Step 5: Commit**

```text
git add internal/app internal/cli internal/output tests/integration
git commit -m "feat: expor comandos de encaminhamento de portas"
```

---

### Task 7: Docker E2E, Documentation, and Release Gates

**Files:**
- Create: `tests/e2e/bidirectional_ports.sh`
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `README.pt-BR.md`
- Modify: `docs/configuration.md`
- Modify: `docs/pt-BR/configuration.md`
- Modify: `docs/security.md`
- Modify: `docs/pt-BR/security.md`
- Modify: `docs/troubleshooting.md`
- Modify: `CHANGELOG.md`
- Modify: `tests/docs_test.go`
- Modify: `docs/release-verification.md`

**Interfaces:**
- Consumes: built CLI, Webtop image, Docker Engine, and the public commands.
- Produces: executable acceptance evidence for every design invariant.

- [ ] **Step 1: Write the failing E2E and docs assertions**

The script must create a temporary plain-directory project, build the current
Webtop image, start a host HTTP echo on port 18124 and a raw TCP echo on 18125,
then assert:

```bash
"$wktbox" --path "$workspace" port import 18124 18125
"$wktbox" --path "$workspace" exec -- docker compose exec -T app \
  curl --fail http://localhost:18124
"$wktbox" --path "$workspace" port publish \
  --map web=127.0.0.1:19124:8000 \
  --map raw=127.0.0.1:19125:9000
curl --fail http://127.0.0.1:19124
```

Add explicit shell assertions for batch rollback, existing conflict, future
container stop, removal, stop/restart persistence, two-box isolation, and
absence of unrequested outer ports. The cleanup trap removes only temporary
boxes and test containers it created.

Add `docs_test.go` assertions for `port import`, `port publish`, localhost
preservation, TCP-only behavior, and loopback-only host publications.

- [ ] **Step 2: Run E2E/docs tests and verify RED**

Run:

```text
make docs-check
bash tests/e2e/bidirectional_ports.sh
```

Expected: documentation check FAIL before docs edits; E2E FAIL at the first
unimplemented or incorrect runtime assertion.

- [ ] **Step 3: Complete documentation and E2E integration**

Document the exact command grammar, examples for one and many services,
transactional conflict behavior, future-container stop behavior, persistent
lifecycle, JSON output, relay security, and troubleshooting commands.

Add `bash tests/e2e/bidirectional_ports.sh` to `make e2e`. Update release
verification to include the new CLI help and Webtop helper modes.

- [ ] **Step 4: Run the full verification matrix**

Run, in order:

```text
make fmt
make test
make test-race
make vet
make docs-check
make build
make images
make images-test
bash tests/e2e/bidirectional_ports.sh
make e2e
git diff --check
git status --short
```

Expected: every command exits 0; no unexpected worktree files; E2E proves all
twelve design scenarios.

- [ ] **Step 5: Commit**

```text
git add Makefile README.md README.pt-BR.md CHANGELOG.md docs tests
git commit -m "feat: validar encaminhamento bidirecional de portas"
```

---

## Completion Audit

Before declaring completion, compare the implementation and fresh runtime
evidence against every section of
`docs/superpowers/specs/2026-07-24-bidirectional-port-forwarding-design.md`.
For each CLI command, direction, transaction rule, lifecycle event, security
invariant, and E2E scenario, identify the exact test or command output that
proves it. Missing or indirect evidence means the feature is not complete.
