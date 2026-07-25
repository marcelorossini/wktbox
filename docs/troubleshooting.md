# Troubleshooting

Start with:

```bash
wktbox doctor --path <workspace>
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

## The directory is not a Git repository

A directory that is not a Git repository is still a valid Wktbox workspace.
Use it directly:

```bash
wktbox --path /absolute/project/path doctor
wktbox --path /absolute/project/path run -- make integration
```

The workspace check reports a non-blocking warning and `git.mode: mounted` is
disabled because no Git metadata exists. Do not run `git init` merely to make
Wktbox accept the directory.

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

## A loopback route is `listening` but connections fail

`listening` confirms that Wktbox bound both loopback listeners inside Webtop.
It is not an application-health or end-to-end probe. Inspect the inner
container's published addresses and service health:

```bash
wktbox exec -- docker inspect <container> \
  --format '{{json .NetworkSettings.Ports}}'
wktbox exec -- docker ps
```

`HostIp` identifies the interface in the DinD network namespace and `HostPort`
identifies the port mirrored onto Webtop localhost. A binding such as
`127.0.0.1:5174:5173` is intentionally unreachable through `docker:5174`;
Wktbox creates its upstream socket inside the DinD namespace to reach it.
Confirm that the service itself listens on the container port and that its
healthcheck is passing.

## The box is not ready

Inspect:

```bash
wktbox status
wktbox logs
wktbox doctor
```

Reconcile with `wktbox up`, or use `wktbox run -- <command>`. `wktbox exec`
intentionally fails when the box is not already ready.

## `wktbox port import` reports a workload conflict

The named workload already owns one requested `localhost` port. Wktbox rejects
the entire batch before changing mappings:

```bash
wktbox status
wktbox --json port list
wktbox exec -- docker ps
```

Change the workload listener or choose a remapped container port, for example
`--map api=127.0.0.1:1234:4321`. If a new workload starts after an import was
created and claims a reserved port, Wktbox stops that workload and reports
`port_import_conflict`; remove the mapping or change the workload before
starting it again.

## `wktbox port publish` reports a host bind conflict

Another host process or box owns the requested `127.0.0.1`/`::1` listener.
Choose a free host port or stop the owning process. The failed batch does not
leave partial publications:

```bash
wktbox port list
wktbox port publish --map api=127.0.0.1:18001:8000
```

The final number is the box's DinD-published port, not necessarily the
workload's internal port. See
[Bidirectional port forwarding](port-forwarding.md).

## A cross-box alias does not resolve

Inspect the desired and observed topology:

```bash
wktbox connections
wktbox --json connections <name-or-id>
```

All members must be ready. Run `wktbox up --path <checkout>` for a stopped
member; the persistent connection is reconciled automatically. Verify that the
target service has a Compose `ports:` publication and use its published
left-hand port with the full `<box-id>.wktbox` alias. `EXPOSE` alone is not
enough. If the connection reports `error`, inspect `wktbox logs interconnect`
for each member and retry `wktbox connect` with the same members.

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
