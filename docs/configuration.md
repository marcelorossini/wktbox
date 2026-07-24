# Configuration

Wktbox uses schema `version: 1`. Precedence from lowest to highest is:

```text
defaults < .wktbox.yml < .wktbox.local.yml < WKTBOX_* < flags
```

An explicit `--config` replaces both automatically discovered files. Relative
paths resolve from the selected workspace. Keep `.wktbox.local.yml` untracked.

## Complete example

```yaml
version: 1

workspace:
  target: /workspace

environment:
  file: .env
  target: /workspace/.env
  readOnly: true

runtime:
  dindImage: docker:29.5.0-dind
  webtopImage: wktbox/webtop:dev
  gatewayImage: wktbox/gateway:dev

webtop:
  enabled: true
  shmSize: 2gb
  ssh:
    enabled: false

git:
  mode: host

resources:
  cpus: 4
  memory: 8gb
  pids: 2048

gateway:
  enabled: true
  routes:
    frontend:
      port: 5173

commands:
  e2e:
    command: [go, test, ./...]

lifecycle:
  idleTimeout: 0
  removeOnWorktreeDelete: false
```

## Sections

`workspace.target` is fixed at `/workspace` in schema v1. The same path in
Webtop and DinD keeps relative project bind mounts consistent.

`environment.file` selects a project environment file. When present, it is
mounted at `environment.target` in Webtop and DinD. `environment.readOnly` must
remain `true`; Wktbox records the path, not file contents.

`runtime` selects external images. Development binaries default to
`docker:29.5.0-dind`, `wktbox/webtop:dev`, and `wktbox/gateway:dev`. A semantic
release binary defaults to immutable matching tags such as
`ghcr.io/marcelorossini/wktbox/webtop:0.1.0` and
`ghcr.io/marcelorossini/wktbox/gateway:0.1.0`. Explicit configuration always
wins.

`webtop.enabled` must be true. `webtop.shmSize` controls shared desktop memory.
SSH enablement is reserved and rejected because Wktbox does not configure an
SSH server.

### Graphical Chromium and CDP

The graphical Chromium is always running in every ready Webtop. XFCE starts a
session supervisor that reopens the browser when it exits. Chromium uses the
persistent profile `/config/.config/wktbox-chromium` and listens for CDP on
container port `9222`.

The host port uses offset `+4` of the box's ten-port block and binds only to
`127.0.0.1`. For a block starting at `23000`, Webtop HTTP is `23000` and
Browser CDP is `23004`. Use `urls.browserCdp` or `ports.browserCdp` from
`wktbox --json status` instead of deriving the port in integrations.

`git.mode` accepts:

- `host` (default): Git remains on the host; only the workspace is mounted.
- `mounted`: writable shared Git metadata is mounted into Webtop and validated.
  It is not mounted into DinD. This mode is skipped with a doctor warning when
  the workspace has no Git metadata or is below a Git root. Use it only with
  trusted repositories and hooks.

`resources` is reserved. Schema v1 parses `cpus`, `memory`, and `pids` but does
not enforce them; they are not security controls.

`gateway.enabled` enables the optional external HTTP gateway. Each
`gateway.routes` entry names a lowercase host label and target port. It is
independent from automatic localhost inside Webtop.

`commands` and `lifecycle` are reserved. Named commands are not executed
automatically. `idleTimeout` and `removeOnWorktreeDelete` do not trigger
automatic cleanup; use explicit CLI lifecycle commands and `wktbox prune`.

## Automatic Webtop loopback

Automatic loopback has no configuration section. Project Compose `ports:` are
the source of truth, and the sidecar reads DinD
`NetworkSettings.Ports`. Only published TCP bindings with a host port become
routes; `EXPOSE` alone does not.

For `8000:3000`, Webtop receives `localhost:8000 -> docker:8000`, preserving the
published port. UDP is reported as a warning.

Webtop reserves internal ports 61000, 61001, and 61002 for HTTP, HTTPS, and
WebSocket transport. A project publication that collides with one is reported
as a conflict without disabling unrelated routes.

Container events update routes. Successful `wktbox run` and `wktbox compose`
commands also request synchronization. Inspect the result with:

```bash
wktbox status
wktbox --json status
```

Cross-box connections also require no configuration section. They are explicit
persistent resources managed with `wktbox connect`, `wktbox connections`, and
`wktbox disconnect`; see [Cross-box connections](connections.md).

## Environment variables and flags

| Variable | Effect |
|---|---|
| `WKTBOX_ENV_FILE` | project environment file |
| `WKTBOX_ENV_TARGET` | project environment mount target |
| `WKTBOX_DIND_IMAGE` | DinD image |
| `WKTBOX_WEBTOP_IMAGE` | Webtop image |
| `WKTBOX_GATEWAY_IMAGE` | gateway image |
| `WKTBOX_STATE_HOME` | state, generated files, and lock root |

`--env-file` and `--env-target` override corresponding variables. `--path`
selects the exact workspace directory; Wktbox does not replace it with a
detected Git root. Repeatable `--env KEY=VALUE` affects only the child process
and is redacted from structured diagnostics.

Resolution is declarative on each invocation. A prior `--env-file` does not
become persistent configuration; repeat it or record `environment.file` in
`.wktbox.local.yml`. With no source, the intentional result is no project
environment mount.

The `--profile` flag is reserved and any named value is rejected in schema v1.
