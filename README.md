# Wktbox

Wktbox provides isolated Docker development for the current checkout or an
optional linked Git worktree, without rewriting the project's Compose ports.

[![CI](https://github.com/marcelorossini/wktbox/actions/workflows/ci.yml/badge.svg)](https://github.com/marcelorossini/wktbox/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/marcelorossini/wktbox)](https://github.com/marcelorossini/wktbox/releases/latest)
[![License](https://img.shields.io/github/license/marcelorossini/wktbox)](LICENSE)
[![GHCR](https://img.shields.io/badge/GHCR-runtime_images-blue)](https://github.com/marcelorossini/wktbox/pkgs/container/wktbox%2Fwebtop)

Wktbox runs a small Go CLI on the host. Each box has its own privileged
Docker-in-Docker daemon, Webtop desktop, Docker resources, and optional HTTP
gateway. The checkout is mounted at `/workspace`; services published by the
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

Requirements: Git, Docker Engine or Docker Desktop using Linux containers,
Docker Compose v2, and permission to run privileged containers.

## Current checkout

The current checkout is the primary workflow. A linked worktree is optional.

```bash
wktbox doctor
wktbox up
wktbox run -- go test ./...
```

`doctor` verifies Git, Docker, Compose, bind mounts, free ports and disk,
privileged DinD, internal TLS, and the selected Git bridge. `run` creates or
starts the box when necessary, executes the command with intact argument
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

## Agent integration

Install the bundled autonomous workflow for Codex and Claude:

```bash
wktbox agents install --target all
wktbox agents status --target all
```

The skill selects the current checkout by default, runs `doctor` before first
initialization, refuses host fallback on prerequisite failure, preserves boxes
through their lifecycle, and prevents Wktbox from running itself inside nested
DinD. Installation changes only the documented skill directory and one marked
instruction block. See [Coding-agent integration](docs/agents.md).

## CLI

- `wktbox up` creates, starts, or reconciles a box.
- `wktbox run -- <command>` ensures readiness and runs a command.
- `wktbox exec -- <command>` runs only in an already-ready box.
- `wktbox compose -- <args>` invokes project Compose inside the isolated daemon.
- `wktbox shell` opens Bash, with a `sh` fallback, in `/workspace`.
- `wktbox open` opens the known Webtop URL.
- `wktbox list` lists known boxes.
- `wktbox status` shows box state, ports, URLs, and loopback routes.
- `wktbox logs [service]` reads external box logs; `--follow` streams them.
- `wktbox stop` stops a box while preserving its data.
- `wktbox restart` restarts the external infrastructure.
- `wktbox destroy` removes the selected box; automation should use `--force`.
- `wktbox doctor` checks host and runtime prerequisites.
- `wktbox prune` reports boxes whose recorded checkout no longer exists;
  `--force` destroys only those candidates.
- `wktbox agents` installs, inspects, or removes agent integrations.

Global flags include `--path`, `--config`, `--env-file`, `--env-target`,
repeatable `--env`, `--json`, `--quiet`, `--verbose`, and `--no-color`.
`--json` writes stable data to stdout and structured errors to stderr.

## Security boundary

Wktbox provides operational isolation for trusted development code. It is not a
security boundary: DinD is privileged and the checkout is writable from the
box. Do not use it for hostile workloads or as a virtual-machine substitute.
Read the [security model](docs/security.md) before adopting it.

## Documentation

- [Installation and updates](docs/installation.md)
- [Current-checkout quickstart](docs/quickstart.md)
- [Optional worktrees and lifecycle](docs/worktrees.md)
- [Coding-agent integration](docs/agents.md)
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
