# Cross-box Connections Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add persistent `connect`, `connections`, and `disconnect` commands so two or more isolated Wktbox boxes can resolve stable aliases and reach each other's published workload ports.

**Architecture:** Keep one DinD per box and create one labeled host-Docker bridge per connection. Attach current DinD and Webtop containers at runtime, forward workload DNS through a required sidecar in the DinD namespace, and store only desired topology plus observed status in `state.json`.

**Tech Stack:** Go 1.26.5, Cobra, Docker CLI/Compose v2, standard-library networking and JSON, shell E2E tests.

## Global Constraints

- Keep each box's DinD daemon, volumes, images, and project-internal networks isolated.
- Do not mount the host Docker socket inside any Wktbox service.
- Do not rewrite the user's Compose files.
- Only ports published by the target DinD are part of the connection contract.
- Use `<full-box-id>.wktbox` as the stable, collision-free alias.
- Preserve `state.json` schema version 1 and load absent `connections` as an empty map.
- Preserve existing human and JSON contracts for `list` and `status`.
- All generated files remain deterministic and protected.
- Commit messages are in Portuguese and start with `feat:` or `fix:`.

---

### Task 1: Connection identity and persistent state

**Files:**
- Create: `internal/connection/identity.go`
- Create: `internal/connection/identity_test.go`
- Modify: `internal/state/model.go`
- Modify: `internal/state/store.go`
- Modify: `internal/state/store_test.go`

**Interfaces:**
- Produces: `connection.ID(memberIDs []string) (string, error)`
- Produces: `connection.Alias(boxID string) string`
- Produces: `state.ConnectionRecord`, `state.ConnectionStatus`, and `State.Connections`
- Consumes: existing 12-character lowercase hexadecimal box IDs

- [ ] **Step 1: Write failing identity and state compatibility tests**

```go
func TestIDIsStableForSortedUniqueMembers(t *testing.T) {
	first, err := connection.ID([]string{"dfe31c662a91", "a4f8c9137d2b"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := connection.ID([]string{"a4f8c9137d2b", "dfe31c662a91"})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) != 12 {
		t.Fatalf("ids = %q and %q", first, second)
	}
}

func TestLoadVersionOneWithoutConnectionsInitializesMap(t *testing.T) {
	root := t.TempDir()
	writeStateFile(t, root, `{"version":1,"boxes":{}}`)
	got, err := state.NewStore(root).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Connections == nil || len(got.Connections) != 0 {
		t.Fatalf("connections = %#v", got.Connections)
	}
}
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
rtk go test ./internal/connection ./internal/state -run 'Test(ID|LoadVersionOneWithoutConnections)' -count=1
```

Expected: compile failure because `internal/connection`, `State.Connections`,
and the connection record types do not exist.

- [ ] **Step 3: Implement deterministic identity and state types**

```go
package connection

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
)

func ID(memberIDs []string) (string, error) {
	if len(memberIDs) < 2 {
		return "", errors.New("a connection requires at least two boxes")
	}
	members := append([]string(nil), memberIDs...)
	sort.Strings(members)
	for index := range members {
		if index > 0 && members[index] == members[index-1] {
			return "", errors.New("connection members must be distinct")
		}
	}
	sum := sha256.Sum256([]byte(joinMembers(members)))
	return hex.EncodeToString(sum[:])[:12], nil
}

func Alias(boxID string) string {
	return boxID + ".wktbox"
}
```

```go
type ConnectionStatus string

const (
	ConnectionReady    ConnectionStatus = "ready"
	ConnectionDegraded ConnectionStatus = "degraded"
	ConnectionError    ConnectionStatus = "error"
)

type ConnectionRecord struct {
	ID           string           `json:"id"`
	Name         string           `json:"name,omitempty"`
	Network      string           `json:"network"`
	Members      []string         `json:"members"`
	Status       ConnectionStatus `json:"status"`
	Error        string           `json:"error,omitempty"`
	CreatedAt    time.Time        `json:"createdAt"`
	ReconciledAt time.Time        `json:"reconciledAt,omitempty"`
}

type State struct {
	Version     int                         `json:"version"`
	Boxes       map[string]BoxRecord        `json:"boxes"`
	Connections map[string]ConnectionRecord `json:"connections,omitempty"`
}
```

Initialize `Connections` in `Empty`, `Load`, and `Save` exactly as `Boxes` is
initialized.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run:

```bash
rtk go test ./internal/connection ./internal/state -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the state foundation**

```bash
rtk git add internal/connection internal/state
rtk git commit -m "feat: modelar conexoes persistentes entre boxes"
```

---

### Task 2: Docker network backend

**Files:**
- Modify: `internal/compose/client.go`
- Modify: `internal/compose/client_test.go`

**Interfaces:**
- Produces: `compose.ConnectionNetwork`, `compose.ConnectionEndpoint`
- Produces: `Backend.EnsureConnectionNetwork`, `InspectConnectionNetwork`,
  `ConnectConnectionEndpoint`, `DisconnectConnectionEndpoint`,
  `RemoveConnectionNetwork`
- Consumes: host-Docker labels `io.wktbox.managed`,
  `io.wktbox.connection-id`, `io.wktbox.connection-name`

- [ ] **Step 1: Write failing command-contract tests**

```go
func TestEnsureConnectionNetworkCreatesLabeledPrivateBridge(t *testing.T) {
	runner := &recordingRunner{
		results: []process.Result{{Stdout: "[]"}, {Stdout: "network-id\n"}},
	}
	client := compose.NewClient(runner)
	network := compose.ConnectionNetwork{
		ID: "64f420a77f31", Name: "dev-stack",
		DockerName: "wktbox-connect-64f420a77f31", Version: "dev",
	}
	if err := client.EnsureConnectionNetwork(context.Background(), network); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.calls[1], " ")
	for _, want := range []string{
		"docker network create",
		"--driver bridge",
		"--label io.wktbox.managed=true",
		"--label io.wktbox.connection-id=64f420a77f31",
		"wktbox-connect-64f420a77f31",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("call %q missing %q", got, want)
		}
	}
}

func TestConnectEndpointUsesCurrentServiceContainerAndAlias(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{{
		Stdout: "container-id\n",
	}}}
	client := compose.NewClient(runner)
	err := client.ConnectConnectionEndpoint(context.Background(),
		compose.ConnectionEndpoint{
			Network: "wktbox-connect-64f420a77f31",
			BoxID: "a4f8c9137d2b", Service: "docker",
			Alias: "a4f8c9137d2b.wktbox",
		})
	if err != nil {
		t.Fatal(err)
	}
	want := "docker network connect --alias a4f8c9137d2b.wktbox wktbox-connect-64f420a77f31 container-id"
	if got := strings.Join(runner.calls[1], " "); got != want {
		t.Fatalf("call = %q", got)
	}
}
```

- [ ] **Step 2: Run the backend tests and verify RED**

Run:

```bash
rtk go test ./internal/compose -run 'Test(EnsureConnectionNetwork|ConnectEndpoint)' -count=1
```

Expected: compile failure because connection network APIs do not exist.

- [ ] **Step 3: Implement typed network operations**

```go
type ConnectionNetwork struct {
	ID         string
	Name       string
	DockerName string
	Version    string
}

type ConnectionEndpoint struct {
	Network string
	BoxID   string
	Service string
	Alias   string
}

type ConnectionNetworkStatus struct {
	Exists    bool
	Endpoints map[string][]string
}
```

Use `docker network inspect --format '{{json .}}' <name>` for inspection.
Resolve each current service container with:

```text
docker ps --filter label=io.wktbox.box-id=<id>
          --filter label=com.docker.compose.service=<service>
          --filter status=running --format {{.ID}}
```

Normalize Docker errors containing `already exists`, `already connected`,
`is not connected`, or `No such network` only when that text proves the
requested end state. Keep all other stderr through `commandError`.

- [ ] **Step 4: Verify create, inspect, attach, detach, and remove**

Run:

```bash
rtk go test ./internal/compose -count=1
```

Expected: PASS with exact Docker argv and error normalization covered.

- [ ] **Step 5: Commit the Docker backend**

```bash
rtk git add internal/compose
rtk git commit -m "feat: gerenciar redes compartilhadas no docker"
```

---

### Task 3: Connection manager and lifecycle reconciliation

**Files:**
- Create: `internal/sandbox/connections.go`
- Create: `internal/sandbox/connections_test.go`
- Modify: `internal/sandbox/manager.go`
- Modify: `internal/sandbox/manager_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Produces: `Manager.Connect(context.Context, []string, string, string) (state.ConnectionRecord, error)`
- Produces: `Manager.Connections(context.Context, string) ([]state.ConnectionRecord, error)`
- Produces: `Manager.Disconnect(context.Context, string) (state.ConnectionRecord, error)`
- Produces: application pass-through methods with the same public records
- Consumes: connection identity and compose backend from Tasks 1 and 2

- [ ] **Step 1: Write failing selector and orchestration tests**

```go
func TestConnectResolvesPrefixesPersistsAndAttachesBothServices(t *testing.T) {
	manager, backend, store := connectionManagerFixture(t,
		box("a4f8c9137d2b", "frontend", state.Ready),
		box("dfe31c662a91", "api", state.Ready),
	)
	got, err := manager.Connect(
		context.Background(),
		[]string{"a4f", "dfe"},
		"dev-stack",
		"dev",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "dev-stack" || len(got.Members) != 2 {
		t.Fatalf("connection = %#v", got)
	}
	current, _ := store.Load(context.Background())
	if current.Connections[got.ID].ID != got.ID {
		t.Fatalf("state = %#v", current.Connections)
	}
	if len(backend.connectCalls) != 4 {
		t.Fatalf("endpoint calls = %#v", backend.connectCalls)
	}
}

func TestDestroyRemovesMemberAndDeletesConnectionBelowTwo(t *testing.T) {
	manager, backend, _ := connectedManagerFixture(t)
	if err := manager.Destroy(context.Background(), "a4f8c9137d2b"); err != nil {
		t.Fatal(err)
	}
	if len(backend.removeNetworkCalls) != 1 {
		t.Fatalf("network removals = %#v", backend.removeNetworkCalls)
	}
}
```

- [ ] **Step 2: Run manager tests and verify RED**

Run:

```bash
rtk go test ./internal/sandbox ./internal/app -run 'Test(Connect|DestroyRemovesMember)' -count=1
```

Expected: compile failure because manager and app connection methods are absent.

- [ ] **Step 3: Implement selector resolution and connection orchestration**

```go
var (
	ErrConnectionNotFound = errors.New("connection not found")
	ErrSelectorNotFound   = errors.New("selector not found")
	ErrSelectorAmbiguous  = errors.New("selector is ambiguous")
)

func (manager Manager) Connect(
	ctx context.Context,
	selectors []string,
	name string,
	version string,
) (state.ConnectionRecord, error)

func (manager Manager) Connections(
	ctx context.Context,
	selector string,
) ([]state.ConnectionRecord, error)

func (manager Manager) Disconnect(
	ctx context.Context,
	selector string,
) (state.ConnectionRecord, error)
```

Resolve all selectors under the global lock, sort full IDs, reject duplicates,
derive the ID, persist desired state, then call one `reconcileConnection`
helper. That helper ensures the network, attaches `docker` with the stable
alias, attaches `webtop` without an alias, inspects endpoint membership, and
sets ready/degraded/error.

Refactor `ensure`, `Restart`, and `Destroy` to invoke focused helpers:

```go
func (manager Manager) reconcileConnectionsForBox(
	ctx context.Context,
	current *state.State,
	boxID string,
) error

func (manager Manager) removeBoxFromConnections(
	ctx context.Context,
	current *state.State,
	boxID string,
) error
```

Call reconciliation after a box becomes ready and after restart. Call removal
before deleting the box record. Preserve connections on stop.

- [ ] **Step 4: Run manager and app tests and verify GREEN**

Run:

```bash
rtk go test ./internal/sandbox ./internal/app -count=1
```

Expected: PASS, including ambiguous selectors, not-ready boxes, idempotent
retry, error persistence, restart repair, destroy cleanup, and prune cleanup.

- [ ] **Step 5: Commit orchestration**

```bash
rtk git add internal/sandbox internal/app
rtk git commit -m "feat: reconciliar conexoes no ciclo de vida das boxes"
```

---

### Task 4: CLI commands and topology rendering

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`
- Modify: `internal/output/output.go`
- Modify: `internal/output/output_test.go`

**Interfaces:**
- Produces: Cobra commands `connect`, `connections`, and `disconnect`
- Produces: `Renderer.Connection(state.ConnectionRecord, map[string]state.BoxRecord) error`
- Produces: `Renderer.Connections([]state.ConnectionRecord, map[string]state.BoxRecord) error`
- Consumes: application methods from Task 3

- [ ] **Step 1: Write failing CLI and output tests**

```go
func TestConnectAcceptsTwoOrMoreSelectorsAndName(t *testing.T) {
	fake := newFakeService()
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{"connect", "a4f", "dfe", "ghi", "--name", "dev-stack"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, fake.calls, "Connect:a4f|dfe|ghi:dev-stack")
}

func TestConnectionsHumanOutputShowsAdjacency(t *testing.T) {
	out := &bytes.Buffer{}
	boxes := map[string]state.BoxRecord{
		"a4f8c9137d2b": {
			ID: "a4f8c9137d2b", Name: "frontend", Status: state.Ready,
		},
		"dfe31c662a91": {
			ID: "dfe31c662a91", Name: "api", Status: state.Ready,
		},
	}
	record := state.ConnectionRecord{
		ID: "64f420a77f31", Name: "dev-stack",
		Network: "wktbox-connect-64f420a77f31",
		Members: []string{"a4f8c9137d2b", "dfe31c662a91"},
		Status: state.ConnectionReady,
	}
	err := output.New(output.Options{Out: out}).Connections(
		[]state.ConnectionRecord{record},
		boxes,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Connection dev-stack (64f420a77f31) is ready",
		"Network: wktbox-connect-64f420a77f31",
		"frontend (a4f8c9137d2b) -> a4f8c9137d2b.wktbox [ready]",
		"api (dfe31c662a91) -> dfe31c662a91.wktbox [ready]",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestConnectionsJSONUsesStablePublicShape(t *testing.T) {
	out := &bytes.Buffer{}
	boxes := map[string]state.BoxRecord{
		"a4f8c9137d2b": {
			ID: "a4f8c9137d2b", Name: "frontend", Status: state.Ready,
		},
	}
	record := state.ConnectionRecord{
		ID: "64f420a77f31", Name: "dev-stack",
		Network: "wktbox-connect-64f420a77f31",
		Members: []string{"a4f8c9137d2b"},
		Status: state.ConnectionDegraded,
	}
	err := output.New(output.Options{JSON: true, Out: out}).Connections(
		[]state.ConnectionRecord{record},
		boxes,
	)
	if err != nil {
		t.Fatal(err)
	}
	var got []output.ConnectionData
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 ||
		got[0].ID != record.ID ||
		got[0].State != state.ConnectionDegraded ||
		got[0].Members[0].Alias != "a4f8c9137d2b.wktbox" {
		t.Fatalf("data = %#v", got)
	}
}
```

- [ ] **Step 2: Run CLI/output tests and verify RED**

Run:

```bash
rtk go test ./internal/cli ./internal/output -run 'Test(Connect|Connections|Disconnect)' -count=1
```

Expected: compile failure because the service methods, commands, and renderers
are absent.

- [ ] **Step 3: Implement commands and stable output DTOs**

```go
type ConnectionMemberData struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Status state.Status `json:"status"`
	Alias  string       `json:"alias"`
}

type ConnectionData struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Network      string                 `json:"network"`
	State        state.ConnectionStatus `json:"state"`
	Members      []ConnectionMemberData `json:"members"`
	CreatedAt    time.Time              `json:"createdAt"`
	ReconciledAt *time.Time             `json:"reconciledAt,omitempty"`
	Error        string                 `json:"error,omitempty"`
}
```

Define Cobra contracts:

```go
Use:  "connect <box> <box> [<box>...]"
Args: cobra.MinimumNArgs(2)

Use:  "connections [connection]"
Args: cobra.MaximumNArgs(1)

Use:  "disconnect <connection>"
Args: cobra.ExactArgs(1)
```

`--name` belongs only to `connect`. Global `--json` and `--quiet` retain their
existing behavior. Extend `ErrorCode` with selector and connection codes.

- [ ] **Step 4: Run CLI/output tests and verify GREEN**

Run:

```bash
rtk go test ./internal/cli ./internal/output -count=1
```

Expected: PASS with help, argument validation, human output, JSON output, quiet
mode, and structured errors covered.

- [ ] **Step 5: Commit the CLI contract**

```bash
rtk git add internal/cli internal/output
rtk git commit -m "feat: expor conexoes e topologia na cli"
```

---

### Task 5: Workload DNS forwarding and mandatory sidecar

**Files:**
- Create: `internal/interconnect/dns.go`
- Create: `internal/interconnect/dns_test.go`
- Modify: `cmd/wktbox-loopback/main.go`
- Modify: `cmd/wktbox-loopback/main_test.go`
- Modify: `assets/sandbox.compose.yml`
- Modify: `internal/compose/client.go`
- Modify: `internal/compose/client_test.go`
- Modify: `internal/sandbox/render_test.go`
- Modify: `images/webtop/Dockerfile`

**Interfaces:**
- Produces: `interconnect.ServeDNS(context.Context, DNSOptions) error`
- Produces: `wktbox-loopback dns-serve` and `wktbox-loopback dns-probe`
- Consumes: the address selected by the box's default route on port `53` and
  upstream Docker DNS `127.0.0.11:53`

- [ ] **Step 1: Write failing UDP/TCP forwarding and Compose tests**

```go
func TestServeDNSForwardsUDPResponse(t *testing.T) {
	upstream := startUDPFixture(t, []byte{0x12, 0x34, 0x81, 0x80})
	listen := freeUDPAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go interconnect.ServeDNS(ctx, interconnect.DNSOptions{
		ListenAddress: listen,
		UpstreamAddress: upstream,
	})
	got := exchangeUDP(t, listen, []byte{0x12, 0x34, 0x01, 0x00})
	if !bytes.Equal(got, []byte{0x12, 0x34, 0x81, 0x80}) {
		t.Fatalf("response = %x", got)
	}
}

func TestRenderIncludesInterconnectDNSWithoutHostSocket(t *testing.T) {
	files, err := sandbox.Render(t.TempDir(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	body := readFile(t, files.ComposePath)
	for _, want := range []string{
		`ip -4 route get 192.0.2.1`,
		`/usr/local/bin/dockerd-entrypoint.sh`,
		`interconnect:`,
		`network_mode: service:docker`,
		`wktbox-loopback dns-serve`,
		`wktbox-loopback dns-probe`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("compose missing %q", want)
		}
	}
	if strings.Contains(body, "/var/run/docker.sock") {
		t.Fatal("host Docker socket leaked")
	}
}
```

- [ ] **Step 2: Run DNS and render tests and verify RED**

Run:

```bash
rtk go test ./internal/interconnect ./cmd/wktbox-loopback ./internal/sandbox ./internal/compose -run 'Test(ServeDNS|RenderIncludesInterconnect|InspectRequiresHealthyInterconnect)' -count=1
```

Expected: compile/test failure because DNS commands and the interconnect service
are absent.

- [ ] **Step 3: Implement cancellable DNS forwarding**

```go
type DNSOptions struct {
	ListenAddress   string
	UpstreamAddress string
	Timeout         time.Duration
}

func ServeDNS(ctx context.Context, options DNSOptions) error

func ProbeDNS(ctx context.Context, server string) error
```

For UDP, preserve each DNS datagram and response transaction without parsing or
rewriting it. For TCP, accept the DNS length-prefixed stream and proxy one
connection to the upstream with `io.Copy`. Close listeners when context is
cancelled and use deadlines so shutdown cannot hang.

Extend `wktbox-loopback`:

```text
wktbox-loopback dns-serve
wktbox-loopback dns-probe
```

Default `dns-serve` to the private IP selected by the box's default route on
port `53`, forwarding to `127.0.0.11:53`. Do not assume a fixed inner bridge
subnet. The probe sends a minimal A query for `localhost.` and accepts any
well-formed response with the same transaction ID.

Add the `interconnect` Compose service sharing the `docker` namespace and make
box readiness require it to be healthy. Update `ListManaged` health recovery
and Webtop image build inputs for `internal/interconnect`.

- [ ] **Step 4: Run DNS, image-contract, and sandbox tests**

Run:

```bash
rtk go test ./internal/interconnect ./cmd/wktbox-loopback ./internal/sandbox ./internal/compose -count=1
rtk bash tests/images/labels_test.sh
```

Expected: PASS.

- [ ] **Step 5: Commit DNS transport**

```bash
rtk git add internal/interconnect cmd/wktbox-loopback assets/sandbox.compose.yml internal/compose internal/sandbox images/webtop/Dockerfile
rtk git commit -m "feat: resolver aliases de boxes nos containers internos"
```

---

### Task 6: Documentation and real runtime acceptance

**Files:**
- Create: `tests/e2e/cross_box_connections.sh`
- Modify: `Makefile`
- Modify: `.github/workflows/ci.yml`
- Modify: `README.md`
- Modify: `README.pt-BR.md`
- Modify: `docs/configuration.md`
- Modify: `docs/pt-BR/configuration.md`
- Modify: `docs/security.md`
- Modify: `docs/pt-BR/security.md`
- Modify: `docs/troubleshooting.md`
- Modify: `CHANGELOG.md`
- Modify: `tests/docs_test.go`

**Interfaces:**
- Produces: user-facing command examples and security boundary
- Produces: Linux runtime proof of Webtop and inner-container bidirectional
  connectivity
- Consumes: released CLI/image behavior from Tasks 1–5

- [ ] **Step 1: Write failing documentation and E2E contract tests**

```go
func TestDocsExplainCrossBoxConnections(t *testing.T) {
	readme := readFile(t, "../README.md")
	for _, want := range []string{
		"wktbox connect",
		"wktbox connections",
		"wktbox disconnect",
		".wktbox",
		"published ports",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing %q", want)
		}
	}
}
```

The shell test must assert:

```bash
wktbox connect "$box_a_prefix" "$box_b_prefix" --name e2e-pair
wktbox --json connections e2e-pair
wktbox --path "$worktree_a" exec -- curl -fsS "http://${box_b_id}.wktbox:8000"
wktbox --path "$worktree_a" exec -- docker run --rm curlimages/curl:latest \
  -fsS "http://${box_b_id}.wktbox:8000"
wktbox disconnect e2e-pair
```

It must also test the reverse direction, same published port in both DinDs,
alias failure before/after the connection, and intact local payload/data after
disconnect.

- [ ] **Step 2: Run docs test and verify RED**

Run:

```bash
rtk go test ./tests -run TestDocsExplainCrossBoxConnections -count=1
```

Expected: FAIL because the commands are not documented.

- [ ] **Step 3: Document the command, topology, lifecycle, and security model**

Add concise English canonical documentation and matching Portuguese entry
points. State explicitly that all member processes can reach published ports,
that application authentication is unchanged, that loopback-only bindings are
excluded, and that disconnect does not remove workloads or data.

Add `tests/e2e/cross_box_connections.sh` to `make e2e` and the runtime CI job
after the existing two-worktree suite.

- [ ] **Step 4: Run acceptance and full quality gates**

Run:

```bash
rtk go test ./...
rtk go test -race ./...
rtk go vet ./...
rtk bash tests/images/labels_test.sh
rtk bash tests/e2e/cross_box_connections.sh
rtk git diff --check
```

Expected: every command exits 0; the runtime test proves bidirectional Webtop
and inner-container connectivity and cleanup.

- [ ] **Step 5: Commit documentation and acceptance coverage**

```bash
rtk git add Makefile .github/workflows/ci.yml README.md README.pt-BR.md CHANGELOG.md docs tests
rtk git commit -m "feat: documentar e validar conexoes entre boxes"
```

---

## Completion audit

- [ ] Verify each of the seven acceptance items in the design against a named
  automated test or fresh runtime command.
- [ ] Inspect `git diff origin/main...HEAD` for unrelated changes and secret
  material.
- [ ] Confirm the pre-existing untracked plan
  `docs/superpowers/plans/2026-07-23-professional-release-agent-integration.md`
  remains untouched and uncommitted.
- [ ] Request code review and resolve every critical or important finding.
- [ ] Record one consolidated Graphiti memory describing the durable CLI,
  state, Compose, DNS, and lifecycle relationship.
