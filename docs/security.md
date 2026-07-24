# Security model

Wktbox provides operational isolation between boxes running trusted
development code. It is not a security boundary for hostile workloads.

Every box includes privileged Docker-in-Docker and a writable checkout mount.
Those properties are fundamental to the current developer workflow. Do not
treat Wktbox as a virtual machine, tenant boundary, or malware sandbox.

## Isolation properties

- Each box has a separate DinD daemon, containers, networks, images, caches,
  volumes, Webtop, and loopback namespace.
- The host `/var/run/docker.sock` is not mounted into Webtop or DinD.
- The internal DinD API uses TLS at `docker:2376` and is not published to the
  host.
- Host Webtop, Browser CDP, and gateway ports bind to `127.0.0.1`.
- Automatic application routes bind only to `127.0.0.1` and `::1` inside the
  Webtop network namespace.
- The sidecar receives read-only DinD certificates and uses a private Unix
  socket at `/run/wktbox-loopback/control.sock` with mode `0600`.
- Project environment files are mounted read-only.
- Generated state directories use mode `0700`; sensitive files use `0600`.
- State contains paths and metadata, not project environment contents.
- Process environment values supplied through `--env` are redacted from
  structured diagnostics.
- Explicit host publications bind only to real-host loopback.
- Host imports cross an authenticated relay with a random 256-bit token. The
  token is mounted read-only only into the trusted loopback sidecar and is
  never included in human or JSON output.

These controls reduce accidental interference between trusted checkouts. They
do not make privileged containers safe for adversarial code.

## Privileged DinD and writable mounts

The DinD container runs with `privileged: true`. Project processes can control
their dedicated daemon and everything mounted into it. Webtop and DinD can read
and modify the checkout. They can read a mounted project environment file even
when that file is read-only.

A compromised or malicious project may alter source, generated files, Git data
that is deliberately mounted, and credentials exposed to project processes.
Use a dedicated virtual machine or another hardened backend when code,
dependencies, or users are not trusted.

## Networking

A TCP port published in DinD is reachable by processes sharing the Webtop
namespace. Automatic routing adds no application authentication. Bind
conflicts are reported and never fall back to external interfaces. UDP is not
forwarded.

The Webtop and optional gateway listen on host loopback, but Wktbox does not add
its own user authentication. Any local user or process able to connect to the
assigned loopback port may attempt to access them.

The graphical Chromium CDP endpoint is also unauthenticated. Any local process
that can reach its `127.0.0.1` port can inspect and control pages, read or
modify cookies and browser storage, and act with the browser profile's
credentials. Closing Chromium is not a way to revoke access because the Webtop
session supervisor opens it again. Stop the box when the endpoint must be
unavailable.

Internal TLS protects the DinD API from accidental access outside the box
containers. It does not transform a privileged container into a VM.

### Explicit host/container forwarding

`wktbox port publish` deliberately makes a selected box port available to any
local process that can reach its `127.0.0.1` or `::1` listener. Wktbox adds no
application authentication.

`wktbox port import` runs a native host relay because container loopback cannot
reach real-host loopback directly. The relay listens on a reserved box port but
rejects requests without the per-box 256-bit token before opening a host
target. Imported host services still receive traffic with the relay process's
host identity and must enforce their own authentication.

The loopback sidecar receives `SYS_ADMIN` for `setns` and `SYS_PTRACE` for
accessing workload namespace handles. On AppArmor hosts it also runs with
`apparmor=unconfined`, because Docker's default profile blocks cross-container
namespace entry. These permissions are limited to the trusted loopback
sidecar, but materially expand its authority inside the box. It does not mount
the host Docker socket, and tokens are not mounted into workload containers.
Treat workloads as trusted development code, not hostile tenants.

### Connecting boxes

Connecting boxes intentionally adds a shared bridge between the selected DinD
and Webtop endpoints. Workload containers can resolve peer aliases and reach
published ports, while images, volumes, daemons, and project-internal networks
remain independent.

Treat every connected member as part of the same trusted development network.
A process may attempt to reach any listener available on a peer endpoint, not
only the intended application. Each DinD API remains protected by a separate
TLS authority and the host Docker socket is still absent, but the shared network
is not isolation against hostile peer code. Remove unused connections with
`wktbox disconnect`.

## Git modes

The default `git.mode: host` does not mount common Git metadata. In
`git.mode: mounted`, Wktbox mounts that metadata writable into Webtop so in-box
Git can work. Hooks and tools may then change the shared repository. DinD still
does not receive the Git metadata mount.

## Secrets

Prefer one external project environment file per checkout and pass it with
`--env-file`. Do not commit `.wktbox.local.yml`, project environment files, or
generated credentials. Avoid secrets in file names, command arguments, and
application output.

Resource fields in schema v1 are not enforced and must not be treated as
availability or security limits.

See [Configuration](configuration.md) and
[Troubleshooting](troubleshooting.md).
