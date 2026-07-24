# Troubleshooting

Start with:

```bash
wktbox doctor --path <checkout>
```

Do not bypass a failed required check by silently running the project command
on the host.

## `wktbox` is not found

Verify the installation directory and `PATH`:

```bash
command -v wktbox
wktbox --version
```

The default Unix directory is `$HOME/.local/bin`. The default Windows directory
is `%LOCALAPPDATA%\Programs\Wktbox\bin`. See
[Installation](installation.md).

## Docker or Compose is unavailable

Confirm both client and daemon:

```bash
docker version
docker compose version
docker info
```

On Docker Desktop, select Linux containers and share the local drive containing
the checkout. Wktbox does not support a remote daemon that cannot bind-mount
the same host paths.

## Privileged DinD is denied

Every box uses a privileged Docker-in-Docker service. Some managed desktops,
rootless configurations, and organization policies prohibit it. Ask the Docker
administrator to allow privileged development containers or use a different
development environment. Wktbox cannot emulate this prerequisite safely.

Read [Security](security.md) before enabling privileged containers.

## A port is busy

Wktbox reserves a unique host block for each box. `doctor` reports unavailable
host ports before initialization. Stop the process using the port or configure
the environment so a free allocation is possible.

Inside Webtop, `wktbox status` may show a loopback route as `conflict`. That
means the project's published TCP port collides with a Webtop internal listener.
Change the project's published port; Wktbox never falls back to a broader bind
address. UDP publications are warnings because automatic loopback forwarding is
TCP-only.

## The box is not ready

Inspect:

```bash
wktbox status
wktbox logs
wktbox doctor
```

Reconcile with `wktbox up`, or use `wktbox run -- <command>`. `wktbox exec`
intentionally fails when the box is not already ready.

## Bind mounts or file watching fail

Use a local checkout path supported by Docker. On Windows, avoid UNC paths.
Docker Desktop file sharing, antivirus software, filesystem case behavior, and
large dependency trees can affect bind mounts and watches. Put databases and
heavy caches in Docker volumes instead of the checkout where practical.

## Git fails inside Webtop

The default `git.mode: host` keeps Git operations on the host. With
`git.mode: mounted`, Wktbox mounts writable shared Git metadata into Webtop and
validates basic Git commands. Repository hooks, symlinks, permissions, and
platform-specific paths can still fail. Return to host mode unless in-box Git
is required and trusted.

## A removed checkout still has a box

Preview stale records:

```bash
wktbox prune
```

Review warnings. A permission failure does not make a box eligible. Destroy
only confirmed candidates:

```bash
wktbox prune --force
```

For a live linked worktree, follow the ordered lifecycle in
[Worktrees](worktrees.md).

## Agent integration reports a conflict

Run:

```bash
wktbox agents status --target all
```

The installed skill has local modifications. Review and preserve them before
using `wktbox agents install --force` or
`wktbox agents uninstall --force`. See [Agent integration](agents.md).
