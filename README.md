# Wktbox

Wktbox provides isolated Docker development for any existing directory,
without rewriting the project's Compose ports. Git is optional; a normal
checkout or linked Git worktree adds metadata but is not a prerequisite.

[![CI](https://github.com/marcelorossini/wktbox/actions/workflows/ci.yml/badge.svg)](https://github.com/marcelorossini/wktbox/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/marcelorossini/wktbox)](https://github.com/marcelorossini/wktbox/releases/latest)
[![License](https://img.shields.io/github/license/marcelorossini/wktbox)](LICENSE)
[![GHCR](https://img.shields.io/badge/GHCR-runtime_images-blue)](https://github.com/marcelorossini/wktbox/pkgs/container/wktbox%2Fwebtop)

Wktbox runs a small Go CLI on the host. Each box has its own privileged
Docker-in-Docker daemon, Webtop desktop, Docker resources, and optional HTTP
gateway. The selected directory is mounted at `/workspace`; services published by the
project appear on the same loopback ports inside Webtop. Boxes therefore keep
containers, networks, volumes, images, caches, and port namespaces independent.

## Two-minute installation

Linux and macOS:

```bash
curl -fsSL \
  https://github.com/marcelorossini/wktbox/releases/latest/download/install.sh \
  | sh
```

Windows PowerShell:

```powershell
irm https://github.com/marcelorossini/wktbox/releases/latest/download/install.ps1 |
  iex
```

The installers select the OS and architecture, verify the release checksum, and
replace the executable atomically. See [Installation](docs/installation.md) for
download-inspect-run commands, fixed versions, PATH setup, updates, manual
verification, and removal.

Requirements: Docker Engine or Docker Desktop using Linux containers, Docker
Compose v2, and permission to run privileged containers.

## Current checkout

The current directory is the primary workflow. It does not need to be a Git
repository, and a linked worktree is optional.

```bash
wktbox doctor
wktbox up
wktbox run -- go test ./...
```

Pass another existing directory when needed:

```bash
wktbox --path /absolute/project/path doctor
wktbox --path /absolute/project/path run -- go test ./...
```

`--path` selects that exact directory, even when it is below a detected Git
root. `doctor` verifies Docker, Compose, bind mounts, free ports and disk,
privileged DinD, internal TLS, and any applicable Git metadata. `run` creates
or starts the box when necessary, executes the command with intact argument
boundaries, and returns the child exit code.

For project Compose operations and the browser desktop:

```bash
wktbox compose -- up --build -d
wktbox compose -- ps
wktbox open
```

Keep the current-checkout box while it remains useful. `wktbox stop` preserves
its data; `wktbox destroy --force` removes only the selected box and its
box-owned Docker volumes.

Read the complete [current-checkout quickstart](docs/quickstart.md).

## Optional linked worktree

Use a linked worktree when branch isolation is useful, not merely because
Wktbox is in use:

```bash
git worktree add ../feature-example -b feature/example
wktbox doctor --path ../feature-example
wktbox up --path ../feature-example
wktbox run --path ../feature-example -- go test ./...
```

Keep the box and worktree throughout pull-request review. After confirming the
branch is merged into `main`, destroy the box before removing the worktree:

```bash
wktbox destroy --path ../feature-example --force
git worktree remove ../feature-example
```

See [Worktrees and lifecycle](docs/worktrees.md).

## Automatic localhost inside Webtop

The project's Compose `ports:` are the source of truth. If a frontend publishes
`5173:5173`, `http://localhost:5173` works inside that box's Webtop. A mapping
such as `8000:3000` preserves the published port on the left and routes
`localhost:8000` to the service.

Each Webtop has a separate network namespace, so two boxes can both use
`localhost:5173`. Container start, stop, die, destroy, and rename events update
routes automatically. `wktbox run` and `wktbox compose` also synchronize after
a successful child command.

Use `wktbox status` to inspect listening routes and their targets. A TCP bind
collision is reported as `conflict` without breaking unrelated routes. UDP is
reported as a warning and is not proxied. The automatic proxy preserves HTTP,
HTTPS, WebSocket, hot reload, and raw TCP protocols.

## Browser CDP

Every ready box keeps its graphical Chromium open inside Webtop and exposes its
Chrome DevTools Protocol on a box-specific host loopback port. Closing the
browser makes the Webtop session supervisor open it again with the same
persistent profile.

Discover the endpoint from the stable JSON contract:

```bash
wktbox --json status
# urls.browserCdp: http://localhost:23004
# ports.browserCdp: 23004
```

A port block starting at `23000` uses `23004` for Browser CDP; subsequent boxes
use `23014`, `23024`, and so on. Point a compatible client at the reported URL:

```bash
npx -y chrome-devtools-mcp@latest \
  --browser-url=http://localhost:23004
```

The port binds only to `127.0.0.1`, but CDP is unauthenticated and grants full
control of the browser profile. See [Security](docs/security.md).

## Agent integration

Install the bundled autonomous workflow for Codex and Claude:

```bash
wktbox agents install --target all
wktbox agents status --target all
```

The skill selects the current directory by default, never initializes Git just
for Wktbox, runs `doctor` before first initialization, refuses host fallback on
blocking prerequisite failure, preserves boxes through their lifecycle, and
prevents Wktbox from running itself inside nested DinD. Installation changes
only the documented skill directory and one marked instruction block. See
[Coding-agent integration](docs/agents.md).

## Connect boxes explicitly

Boxes remain isolated unless you connect them. To let workload containers in
two or more ready boxes reach each other's published ports:

```bash
wktbox connect a4f dfe --name dev-stack
wktbox connections dev-stack
```

If the target box ID is `dfe31c662a91` and its Compose publishes `8000:3000`,
other members use `http://dfe31c662a91.wktbox:8000`. The persistent connection
survives `stop` and is reconciled by `up` or `restart`. Remove it with
`wktbox disconnect dev-stack`. See
[Cross-box connections](docs/connections.md) for selectors, topology, lifecycle,
and the trust boundary.

## CLI

- `wktbox up` creates, starts, or reconciles a box.
- `wktbox run -- <command>` ensures readiness and runs a command.
- `wktbox exec -- <command>` runs only in an already-ready box.
- `wktbox compose -- <args>` invokes project Compose inside the isolated daemon.
- `wktbox shell` opens Bash, with a `sh` fallback, in `/workspace`.
- `wktbox open` opens the known Webtop URL.
- `wktbox list` lists known boxes.
- `wktbox status` shows box state, ports, URLs, and loopback routes.
- `wktbox connect <box> <box> [more...]` creates a persistent shared network.
- `wktbox connections [connection]` shows connection topology and aliases.
- `wktbox disconnect <connection>` removes one shared network.
- `wktbox logs [service]` reads external box logs; `--follow` streams them.
- `wktbox stop` stops a box while preserving its data.
- `wktbox restart` restarts the external infrastructure.
- `wktbox destroy` removes the selected box; automation should use `--force`.
- `wktbox doctor` checks host and runtime prerequisites.
- `wktbox prune` reports boxes whose recorded workspace no longer exists;
  `--force` destroys only those candidates.
- `wktbox agents` installs, inspects, or removes agent integrations.

Global flags include `--path`, `--config`, `--env-file`, `--env-target`,
repeatable `--env`, `--json`, `--quiet`, `--verbose`, and `--no-color`.
`--json` writes stable data to stdout and structured errors to stderr.

## Security boundary

Wktbox provides operational isolation for trusted development code. It is not a
security boundary: DinD is privileged and the workspace is writable from the
box. Do not use it for hostile workloads or as a virtual-machine substitute.
Read the [security model](docs/security.md) before adopting it.

## Documentation

- [Installation and updates](docs/installation.md)
- [Current-checkout quickstart](docs/quickstart.md)
- [Optional worktrees and lifecycle](docs/worktrees.md)
- [Coding-agent integration](docs/agents.md)
- [Cross-box connections](docs/connections.md)
- [Configuration](docs/configuration.md)
- [Windows and Docker Desktop](docs/windows.md)
- [Security model](docs/security.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Release verification](docs/release-verification.md)
- [Technical plan and acceptance record](wktbox-plano-tecnico.md)

## Contributing and license

Development, tests, commit rules, and release expectations are in
[CONTRIBUTING.md](CONTRIBUTING.md). Wktbox is licensed under the
[Apache License 2.0](LICENSE). Release history is recorded in
[CHANGELOG.md](CHANGELOG.md).

Leia a [documentação em Português](README.pt-BR.md).
