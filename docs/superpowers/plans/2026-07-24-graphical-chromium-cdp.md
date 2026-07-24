# Graphical Chromium CDP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep the Webtop's graphical Chromium open, restart it after closure, and expose its CDP endpoint on a deterministic high loopback port for every box.

**Architecture:** The Webtop image owns a Chromium wrapper plus an XFCE-autostarted session supervisor, so the controlled browser is the visible desktop browser and uses one persistent non-default profile. Chromium listens on loopback port `9222`; an s6-supervised `socat` relay exposes container port `9223`, which external Compose publishes at port-block offset `+4`. Status output and health-aware readiness make the endpoint discoverable and reliable.

**Tech Stack:** Go 1.26, Docker Compose v2, LinuxServer Webtop/Ubuntu XFCE, Chromium CDP, Bash, XDG autostart, Go and shell E2E tests.

## Global Constraints

- The browser is the graphical Chromium in the Webtop, not a separate headless instance.
- Chromium starts with the XFCE session and is relaunched after the last window closes or the process crashes.
- Container CDP port is exactly `9222`.
- Container CDP relay port is exactly `9223`.
- Host CDP port is exactly `ports.Block.Start + 4`.
- Host publication binds only to `127.0.0.1`.
- Chromium profile is `/config/.config/wktbox-chromium` and persists in `webtop-config`.
- A ready box must have a healthy `/json/version` CDP endpoint.
- The schema v1 has no browser-disable option.
- Do not mount the host Docker socket or publish the DinD API.
- Commit messages are Portuguese and start with `feat:` or `fix:`.

---

### Task 1: Public port and status contract

**Files:**
- Modify: `internal/ports/allocator.go`
- Modify: `internal/ports/allocator_test.go`
- Modify: `internal/output/output.go`
- Modify: `internal/output/output_test.go`

**Interfaces:**
- Produces: `func (Block) BrowserCDP() int`
- Produces: `func BrowserCDPURL(state.BoxRecord) string`
- Produces: JSON fields `urls.browserCdp` and `ports.browserCdp`
- Consumes: existing `ports.Block.Start`, `ports.Block.Size`, and `state.BoxRecord.Ports`

- [x] **Step 1: Write failing port and output tests**

Extend `TestReserveReturnsFirstAvailableBlock`:

```go
if block.HTTP() != 23000 || block.HTTPS() != 23001 ||
	block.SSH() != 23002 || block.Gateway() != 23003 ||
	block.BrowserCDP() != 23004 {
	t.Fatalf("service ports = %#v", block)
}
```

Extend `TestWriteBoxJSONUsesStablePublicContract`:

```go
if urls["browserCdp"] != "http://localhost:23004" {
	t.Fatalf("browser CDP URL = %#v", urls["browserCdp"])
}
portsData, ok := got["ports"].(map[string]any)
if !ok || portsData["browserCdp"] != float64(23004) {
	t.Fatalf("ports = %#v", got["ports"])
}
```

Extend `TestHumanBoxNeverPrintsInternalGeneratedPaths`:

```go
if !strings.Contains(got, "Browser CDP: http://localhost:23004") {
	t.Fatalf("human output missing browser CDP URL: %s", got)
}
```

- [x] **Step 2: Run targeted tests and verify RED**

Run:

```bash
docker run --rm --user "$(id -u):$(id -g)" \
  -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod \
  -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/ports ./internal/output -count=1
```

Expected: compilation fails because `BrowserCDP` does not exist, followed by
missing JSON/human assertions after the method is introduced.

- [x] **Step 3: Implement the public contract**

Add to `internal/ports/allocator.go`:

```go
func (block Block) BrowserCDP() int {
	return block.Start + 4
}
```

Extend the output types:

```go
type URLs struct {
	Webtop    string `json:"webtop,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
	BrowserCDP string `json:"browserCdp,omitempty"`
}

type PortSet struct {
	WebtopHTTP  int `json:"webtopHttp,omitempty"`
	WebtopHTTPS int `json:"webtopHttps,omitempty"`
	SSH         int `json:"ssh,omitempty"`
	Gateway     int `json:"gateway,omitempty"`
	BrowserCDP  int `json:"browserCdp,omitempty"`
}
```

Add:

```go
func BrowserCDPURL(box state.BoxRecord) string {
	if box.Ports.Size < 5 {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", box.Ports.BrowserCDP())
}
```

Populate `URLs.BrowserCDP` and `PortSet.BrowserCDP` in `boxData`, then print:

```go
if url := BrowserCDPURL(box); url != "" {
	fmt.Fprintf(&result, "Browser CDP: %s\n", url)
}
```

immediately after the Webtop URL in `humanBox`.

- [x] **Step 4: Run targeted tests and verify GREEN**

Run the command from Step 2.

Expected: `ok` for `wktbox/internal/ports` and `wktbox/internal/output`.

- [x] **Step 5: Commit**

```bash
git add internal/ports/allocator.go internal/ports/allocator_test.go \
  internal/output/output.go internal/output/output_test.go
git commit -m "feat: expor porta CDP no status"
```

---

### Task 2: Compose publication and health-aware readiness

**Files:**
- Modify: `assets/sandbox.compose.yml`
- Modify: `internal/sandbox/render.go`
- Modify: `internal/sandbox/render_test.go`
- Modify: `internal/compose/client.go`
- Modify: `internal/compose/client_test.go`
- Modify: `internal/sandbox/manager_test.go`

**Interfaces:**
- Consumes: `ports.Block.BrowserCDP() int` from Task 1
- Produces: sandbox env key `PORT_BROWSER_CDP`
- Produces: host mapping `127.0.0.1:${PORT_BROWSER_CDP}:9223`
- Produces: Webtop health contract `curl ... http://127.0.0.1:9223/json/version`
- Produces: `compose.Status.Ready()` requiring healthy Webtop

- [x] **Step 1: Write failing render and validation tests**

In `TestRenderIncludesRequiredLoopbackSidecarAndHighWebtopPorts`, require:

```go
if !reflect.DeepEqual(webtop.Ports, []string{
	"127.0.0.1:${PORT_HTTP}:61000",
	"127.0.0.1:${PORT_HTTPS}:61001",
	"127.0.0.1:${PORT_BROWSER_CDP}:9223",
}) {
	t.Fatalf("webtop ports = %#v", webtop.Ports)
}
for _, required := range []string{
	"curl --fail --silent --show-error http://127.0.0.1:9223/json/version",
	`PORT_BROWSER_CDP="23004"`,
} {
	if !strings.Contains(readFile(t, files.ComposePath)+readFile(t, files.SandboxEnvPath), required) {
		t.Fatalf("rendered files missing %q", required)
	}
}
```

Add a validation test:

```go
func TestRenderRejectsPortBlockWithoutBrowserOffset(t *testing.T) {
	spec := testSpec()
	spec.Ports.Size = 4
	_, err := sandbox.Render(t.TempDir(), spec)
	if err == nil || !strings.Contains(err.Error(), "invalid port block") {
		t.Fatalf("error = %v", err)
	}
}
```

- [x] **Step 2: Write failing readiness tests**

Change ready fixtures to use `"Health":"healthy"` for Webtop. Add:

```go
func TestInspectRequiresHealthyWebtopBrowser(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{{Stdout: `[
		{"Name":"wktbox-a-docker-1","Service":"docker","State":"running","Health":"healthy"},
		{"Name":"wktbox-a-webtop-1","Service":"webtop","State":"running","Health":"starting"},
		{"Name":"wktbox-a-loopback-1","Service":"loopback","State":"running","Health":"healthy"},
		{"Name":"wktbox-a-interconnect-1","Service":"interconnect","State":"running","Health":"healthy"}
	]`}}}
	got, err := compose.NewClient(runner).Inspect(context.Background(), testProject())
	if err != nil {
		t.Fatal(err)
	}
	if got.Ready() {
		t.Fatalf("status unexpectedly ready: %#v", got)
	}
}
```

Update managed-container fixtures so the Webtop status contains `(healthy)`,
and add a counterpart with `(unhealthy)` that asserts `ManagedProject.Healthy`
is false.

- [x] **Step 3: Run targeted tests and verify RED**

Run:

```bash
docker run --rm --user "$(id -u):$(id -g)" \
  -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod \
  -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/sandbox ./internal/compose -count=1
```

Expected: failures for the missing CDP mapping/env/healthcheck and Webtop
readiness still accepting `starting`.

- [x] **Step 4: Implement render and readiness**

Change port-block validation to:

```go
if spec.Ports.Size < 5 || spec.Ports.Start < 1 || spec.Ports.End() > 65535 {
	return fmt.Errorf("invalid port block %#v", spec.Ports)
}
```

Add to the sandbox env map:

```go
"PORT_BROWSER_CDP": strconv.Itoa(spec.Ports.BrowserCDP()),
```

Add to the Webtop service in `assets/sandbox.compose.yml`:

```yaml
    ports:
      - 127.0.0.1:${PORT_HTTP}:61000
      - 127.0.0.1:${PORT_HTTPS}:61001
      - 127.0.0.1:${PORT_BROWSER_CDP}:9223
    healthcheck:
      test:
        - CMD-SHELL
        - curl --fail --silent --show-error http://127.0.0.1:9223/json/version >/dev/null
      interval: 3s
      timeout: 3s
      retries: 30
      start_period: 15s
```

Make `Status.Ready` require:

```go
webtopReady = container.State == "running" && container.Health == "healthy"
```

Replace `webtopRunning` with `webtopHealthy` in the managed accumulator and
calculate it from the `(healthy)` Docker status text. Include it in the final
`ManagedProject.Healthy` conjunction.

Update `readyComposeStatus()` and all explicit ready Webtop fixtures to
`Health: "healthy"`.

- [x] **Step 5: Run targeted tests and full Go tests**

Run:

```bash
docker run --rm --user "$(id -u):$(id -g)" \
  -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod \
  -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./internal/sandbox ./internal/compose ./internal/app ./internal/cli -count=1
make test
```

Expected: all commands exit `0`.

- [x] **Step 6: Commit**

```bash
git add assets/sandbox.compose.yml internal/sandbox/render.go \
  internal/sandbox/render_test.go internal/compose/client.go \
  internal/compose/client_test.go internal/sandbox/manager_test.go
git commit -m "feat: publicar CDP e exigir navegador saudavel"
```

---

### Task 3: Graphical Chromium lifecycle in the Webtop image

**Files:**
- Create: `images/webtop/wrapped-chromium`
- Create: `images/webtop/chromium-supervisor`
- Create: `images/webtop/wktbox-chromium.desktop`
- Create: `images/webtop/s6-rc.d/svc-wktbox-cdp-relay/run`
- Create: `images/webtop/s6-rc.d/svc-wktbox-cdp-relay/type`
- Create: `images/webtop/s6-rc.d/svc-wktbox-cdp-relay/dependencies.d/init-services`
- Create: `images/webtop/s6-rc.d/user/contents.d/svc-wktbox-cdp-relay`
- Modify: `images/webtop/Dockerfile`
- Create: `tests/images/chromium_test.sh`
- Modify: `Makefile`

**Interfaces:**
- Produces: `/usr/local/bin/wrapped-chromium`
- Produces: `/usr/local/bin/wktbox-chromium-supervisor`
- Produces: `/etc/xdg/autostart/wktbox-chromium.desktop`
- Produces: s6-supervised `0.0.0.0:9223 -> 127.0.0.1:9222` relay
- Consumes: XFCE session environment, `/config`, container port `9222`
- Test-only overrides: `WKTBOX_CHROMIUM_BIN`, `WKTBOX_CHROMIUM_PROFILE`,
  `WKTBOX_CHROMIUM_WRAPPER`, `WKTBOX_CHROMIUM_RESTART_DELAY`

- [x] **Step 1: Write the failing image behavior test**

Create `tests/images/chromium_test.sh` with a temporary fake Chromium binary
that records every argument. Execute `images/webtop/wrapped-chromium
https://example.test`, then require these exact arguments in the recording:

```text
--remote-debugging-port=9222
--user-data-dir=<temporary-profile>
https://example.test
```

Create a fake wrapper that increments a counter and exits. Run
`images/webtop/chromium-supervisor` with restart delay `0.05`, wait until the
counter reaches `2`, terminate the supervisor, and require that it exits and no
fake child remains.

Finally require the desktop file to contain:

```text
Type=Application
Exec=/usr/local/bin/wktbox-chromium-supervisor
OnlyShowIn=XFCE;
X-GNOME-Autostart-enabled=true
```

- [x] **Step 2: Run the image test and verify RED**

Run:

```bash
bash tests/images/chromium_test.sh
```

Expected: failure because the three image assets do not exist.

- [x] **Step 3: Implement the wrapper**

Create a Bash wrapper which:

```bash
browser_bin="${WKTBOX_CHROMIUM_BIN:-/usr/bin/chromium}"
profile="${WKTBOX_CHROMIUM_PROFILE:-/config/.config/wktbox-chromium}"

if ! pgrep -x chromium >/dev/null 2>&1; then
  rm -f "$profile"/Singleton*
fi

browser_args=(
  --password-store=basic
  --remote-debugging-port=9222
  "--user-data-dir=$profile"
)
if ! grep -q 'Seccomp:.0' /proc/1/status; then
  browser_args+=(--no-sandbox --test-type)
fi
exec "$browser_bin" "${browser_args[@]}" "$@"
```

Use `mkdir -p "$profile"` before cleanup and quote every path.

- [x] **Step 4: Implement the session supervisor and autostart**

The supervisor must launch the wrapper as a child, trap `TERM`, `INT`, and
`HUP`, forward termination to the current child, wait for it, then sleep for
`WKTBOX_CHROMIUM_RESTART_DELAY` (default `2`) before relaunching.

Create the desktop entry:

```ini
[Desktop Entry]
Type=Application
Name=Wktbox Chromium
Comment=Graphical Chromium with Chrome DevTools Protocol
Exec=/usr/local/bin/wktbox-chromium-supervisor
OnlyShowIn=XFCE;
X-GNOME-Autostart-enabled=true
Terminal=false
StartupNotify=false
```

- [x] **Step 5: Install assets in the image**

Add Dockerfile copies:

```dockerfile
COPY --chmod=0755 images/webtop/wrapped-chromium /usr/local/bin/wrapped-chromium
COPY --chmod=0755 images/webtop/chromium-supervisor /usr/local/bin/wktbox-chromium-supervisor
COPY --chmod=0644 images/webtop/wktbox-chromium.desktop /etc/xdg/autostart/wktbox-chromium.desktop
```

Extend the Dockerfile validation with:

```dockerfile
RUN test -x /usr/local/bin/wrapped-chromium \
    && test -x /usr/local/bin/wktbox-chromium-supervisor \
    && test -r /etc/xdg/autostart/wktbox-chromium.desktop
```

Update `images-test`:

```make
images-test:
	bash tests/images/chromium_test.sh
	bash tests/images/labels_test.sh
```

- [x] **Step 6: Run tests and build the image**

Run:

```bash
bash tests/images/chromium_test.sh
docker build -f images/webtop/Dockerfile -t wktbox/webtop:dev .
make images-test
```

Expected: the behavior test passes, the Webtop image builds, and OCI label
tests pass.

- [x] **Step 7: Commit**

```bash
git add images/webtop/wrapped-chromium images/webtop/chromium-supervisor \
  images/webtop/wktbox-chromium.desktop images/webtop/Dockerfile \
  tests/images/chromium_test.sh Makefile
git commit -m "feat: manter Chromium grafico supervisionado"
```

---

### Task 4: User and security documentation

**Files:**
- Modify: `README.md`
- Modify: `README.pt-BR.md`
- Modify: `docs/configuration.md`
- Modify: `docs/pt-BR/configuration.md`
- Modify: `docs/security.md`
- Modify: `docs/pt-BR/security.md`
- Modify: `tests/docs_test.go`

**Interfaces:**
- Consumes: public fields `browserCdp`, port offset `+4`, container port `9222`
- Produces: English canonical guidance and Portuguese entry-point guidance

- [x] **Step 1: Write a failing documentation contract test**

Add a docs test that requires canonical English docs to contain:

```go
required := map[string][]string{
	"README.md": {"Browser CDP", "browserCdp", "23004"},
	"docs/configuration.md": {"9222", "offset `+4`", "always running"},
	"docs/security.md": {"CDP", "cookies", "127.0.0.1"},
	"README.pt-BR.md": {"CDP do navegador", "browserCdp"},
	"docs/pt-BR/configuration.md": {"9222", "offset `+4`"},
	"docs/pt-BR/security.md": {"CDP", "cookies", "127.0.0.1"},
}
```

Normalize whitespace with `strings.Join(strings.Fields(body), " ")`, then fail
for every missing phrase.

- [x] **Step 2: Run the docs test and verify RED**

Run:

```bash
docker run --rm --user "$(id -u):$(id -g)" \
  -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod \
  -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./tests -run BrowserCDP -count=1
```

Expected: failures listing all undocumented phrases.

- [x] **Step 3: Document usage and security**

Document this discovery flow:

```bash
wktbox --json status
npx -y chrome-devtools-mcp@latest \
  --browser-url=http://localhost:23004
```

Explain that the real port comes from `urls.browserCdp`, that Chromium is
visible and automatically relaunched, and that the profile persists.

Document the fixed container port, high host offset, loopback-only publication,
and the fact that any local process reaching CDP can read/control pages,
cookies, and browser storage.

- [x] **Step 4: Run documentation tests**

Run:

```bash
make docs-check
```

Expected: all documentation tests pass.

- [x] **Step 5: Commit**

```bash
git add README.md README.pt-BR.md docs/configuration.md \
  docs/pt-BR/configuration.md docs/security.md docs/pt-BR/security.md \
  tests/docs_test.go
git commit -m "feat: documentar acesso CDP por box"
```

---

### Task 5: Runtime proof with two isolated graphical browsers

**Files:**
- Modify: `tests/e2e/two_worktrees.sh`
- Modify: `tests/e2e/assert-isolation.sh`

**Interfaces:**
- Consumes: `ports.browserCdp`, `urls.browserCdp`, graphical supervisor, CDP
  `/json/version` and `/json/new`
- Proves: distinct ports, host usability, visible page creation, restart after
  closure, and loopback-only binding

- [x] **Step 1: Add reusable CDP polling**

Add to `tests/e2e/assert-isolation.sh`:

```bash
wait_for_cdp() {
  local url="$1"
  local deadline=$((SECONDS + 90))
  while ((SECONDS < deadline)); do
    if curl --fail --silent --show-error \
      "$url/json/version" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  printf 'CDP endpoint did not become ready: %s\n' "$url" >&2
  return 1
}
```

- [x] **Step 2: Extend two-worktree assertions**

After reading both statuses, extract and assert:

```bash
browser_port_a="$(json_value "$status_a" ports.browserCdp)"
browser_port_b="$(json_value "$status_b" ports.browserCdp)"
browser_url_a="$(json_value "$status_a" urls.browserCdp)"
browser_url_b="$(json_value "$status_b" urls.browserCdp)"
assert_nonempty_distinct "$browser_port_a" "$browser_port_b" "browser CDP ports"
wait_for_cdp "$browser_url_a"
wait_for_cdp "$browser_url_b"
```

Check Docker publication for each Webtop:

```bash
test "$(docker port "wktbox-$id_a-webtop-1" 9223/tcp)" = \
  "127.0.0.1:$browser_port_a"
test "$(docker port "wktbox-$id_b-webtop-1" 9223/tcp)" = \
  "127.0.0.1:$browser_port_b"
```

After the inner frontend starts, open a real graphical tab:

```bash
curl --fail --silent --show-error --request PUT \
  "$browser_url_a/json/new?http://localhost:5173/public/index.html" \
  >"$test_root/browser-tab.json"
curl --fail --silent --show-error "$browser_url_a/json/list" |
  python3 -c '
import json, sys
pages = json.load(sys.stdin)
if not any(page.get("type") == "page" and
           page.get("url", "").endswith("/public/index.html")
           for page in pages):
    raise SystemExit("graphical Chromium tab not found")
'
```

- [x] **Step 3: Prove automatic relaunch**

Read the browser PID from the Webtop by matching
`--remote-debugging-port=9222`, terminate it, wait for CDP, then require a
different PID:

```bash
old_browser_pid="$(docker exec "wktbox-$id_a-webtop-1" \
  pgrep -o -f '/usr/lib/chromium/chromium.*--remote-debugging-port=9222')"
docker exec "wktbox-$id_a-webtop-1" kill "$old_browser_pid"
wait_for_cdp "$browser_url_a"
new_browser_pid="$(docker exec "wktbox-$id_a-webtop-1" \
  pgrep -o -f '/usr/lib/chromium/chromium.*--remote-debugging-port=9222')"
test -n "$old_browser_pid"
test -n "$new_browser_pid"
test "$old_browser_pid" != "$new_browser_pid"
```

Verify the box returns to `ready` and box B remains ready.

- [x] **Step 4: Run RED against pre-feature behavior**

Before production implementation is complete, run:

```bash
bash tests/e2e/two_worktrees.sh
```

Expected: failure because `ports.browserCdp` and the CDP publication do not
exist. If Tasks 1–3 are already implemented in sequence, confirm the new
assertions would have failed on commit `3122936` with
`git show 3122936:tests/e2e/two_worktrees.sh`.

- [x] **Step 5: Run the completed E2E and full verification**

Run:

```bash
bash tests/e2e/two_worktrees.sh
make test
make vet
make docs-check
bash tests/images/chromium_test.sh
git diff --check
```

Expected: every command exits `0`; E2E prints
`wktbox E2E passed: two linked worktrees remained isolated`.

- [x] **Step 6: Commit**

```bash
git add tests/e2e/two_worktrees.sh tests/e2e/assert-isolation.sh
git commit -m "feat: validar Chromium CDP em duas boxes"
```

---

## Completion audit

- [x] `git status --short` is clean.
- [x] `git log --oneline main..HEAD` contains the design, implementation plan,
  and implementation commits.
- [x] `main` does not contain any of those commits.
- [x] The worktree remains present for user review.
- [x] The branch is not merged into `main`.
- [x] Graphiti contains the durable mapping between Webtop Chromium, CDP 9222,
  host offset `+4`, status fields, and lifecycle supervision.
