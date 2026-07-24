# Bidirectional Port Forwarding Design

## Goal

Add explicit, persistent TCP forwarding in both directions:

- import a service bound on the real host into `localhost` of every workload
  container in one Wktbox;
- publish a TCP port from the box on the real host loopback.

The two directions use different subcommands and preserve the existing rule
that ordinary DinD publications never leak to the real host automatically.

## Terminology

- **real host**: the Windows, Linux, or macOS system running the `wktbox` CLI;
- **box port**: a TCP `HostPort` published inside the box's DinD daemon;
- **workload container**: a running container managed by the inner DinD daemon;
- **import**: real host to workload-container localhost;
- **publication**: box port to real-host loopback.

## CLI

The commands are grouped below `wktbox port`:

```text
wktbox port import <host-port> [<host-port>...]
wktbox port import --map <name=host-address:host-port:container-port> [--map ...]
wktbox port publish --map <name=bind-address:host-port:box-port> [--map ...]
wktbox port list
wktbox port remove <name> [<name>...]
wktbox port remove --all-imports
wktbox port remove --all-publications
```

Short imports preserve the number:

```text
wktbox port import 1234 5432

real host 127.0.0.1:1234 -> every workload localhost:1234
real host 127.0.0.1:5432 -> every workload localhost:5432
```

Named mappings support remapping:

```text
wktbox port import \
  --map postgres=127.0.0.1:5432:5432 \
  --map redis=127.0.0.1:6379:16379

wktbox port publish \
  --map frontend=127.0.0.1:15173:5173 \
  --map api=127.0.0.1:18000:8000
```

Names use lowercase letters, digits, dot, underscore, and dash. Generated names
for positional imports are `import-<port>`. Names are unique within a box,
regardless of direction. TCP is the only protocol in version 1.

`port import` and `port publish` require an existing ready box. They do not
silently create one because forwarding must never hide a missing or stopped
runtime. Errors recommend `wktbox up`.

## Import Semantics

An import preserves `localhost`, not merely a special hostname. After:

```text
wktbox port import 1234
```

each running workload can use:

```text
127.0.0.1:1234
localhost:1234
```

The host-side target remains `127.0.0.1:1234`. A remapped import such as
`api=127.0.0.1:1234:4321` is reached at workload `localhost:4321`.

Before changing state, Wktbox probes every requested container port in every
running workload network namespace. If any listener already owns a requested
port, the whole command fails with the mapping, container name, and port. No
proxy, state entry, configuration file, or partial mapping remains.

The final activation repeats the bind while holding the per-box lock. A bind
race also rolls back every proxy created by that command and leaves the previous
configuration active.

The import reconciler watches inner Docker lifecycle events. When a new
workload starts, it installs every active import before considering that
workload reconciled. If the workload already owns a reserved localhost port,
the reconciler immediately stops that workload, records a structured
`port_import_conflict`, and leaves the desired import active for the other
workloads. This is the only non-atomic case because the conflicting workload did
not exist during the original transaction.

Wktbox-owned helper processes and stopped containers are excluded from workload
selection.

## Publication Semantics

A publication targets the published host-side port of the inner DinD daemon.
For an inner Compose mapping `8000:3000`, `box-port` is `8000`.

```text
wktbox port publish --map api=127.0.0.1:18000:8000
```

creates:

```text
real host 127.0.0.1:18000 -> DinD docker:8000 -> workload port 3000
```

The bind address defaults to and initially only permits `127.0.0.1` or `::1`.
Exposing to LAN interfaces is outside this feature. Wktbox validates the entire
batch for duplicate names, duplicate host listeners, invalid ports, and current
host bind conflicts before applying it. Any failure leaves the previous
publication set intact.

Publications forward arbitrary TCP, including HTTP, HTTPS, WebSocket,
PostgreSQL, and Redis. They do not perform HTTP routing, TLS termination, or
authentication.

## Persistent State

Each `state.BoxRecord` stores a sorted `PortMappings` collection. A mapping has:

```text
name, direction, sourceAddress, sourcePort, targetPort, createdAt
```

Desired mappings survive `stop`, `restart`, process exits, and host reboots.
`stop` tears down active relays while preserving desired state. `up` and
`restart` render configuration, start the required relays, and reconcile every
mapping. `destroy` stops the host relay and removes all per-box mapping files
with the box directory.

Generated files in the protected box directory are:

```text
ports.json
ports.override.yml
port-relay.json
port-relay.pid
port-relay.token
```

The token and configuration files use mode `0600`; the directory remains
`0700`. State and generated configuration are written atomically.

## Runtime Architecture

### Host import relay

A detached mode of the same release binary, hidden from normal CLI help, runs
one host relay per box. It listens on offset 4 of the box's already-reserved
port block and forwards authenticated requests to configured host-loopback
targets. The existing block size of 10 requires no new allocator range.

The relay binds an externally reachable host address because nested workload
namespaces cannot reach real-host loopback directly. Every connection starts
with a versioned preamble containing the mapping name and a random 256-bit
token. Invalid or missing credentials are rejected before a host target is
opened. The token never appears in human or JSON output.

The CLI starts the relay on import/up/restart, validates readiness, and stops it
on stop/destroy or when the last import is removed. Unix and Windows use
platform-specific detached-process setup; the forwarding protocol and config
are platform-neutral.

### Workload-local import proxies

The existing `loopback` sidecar continues observing the inner Docker API. It
also shares the DinD PID namespace and receives only the Linux capabilities
needed to enter workload network namespaces.

For each running workload and import, the reconciler starts a child
`wktbox-loopback import-proxy` process. The child enters the workload's network
namespace, binds `127.0.0.1:<container-port>` and `::1:<container-port>`, then
bridges authenticated TCP streams to the host relay. The parent owns and
terminates children when containers stop, mappings are removed, or the sidecar
shuts down.

The sidecar resolves `host.docker.internal` when Docker provides it and falls
back to the IPv4 default gateway from `/proc/net/route` on native Linux. It
passes the resulting literal address to children. This avoids the invalid
combination of `extra_hosts` with `network_mode: service:webtop`.

The mutable `ports.json` and stable relay token live in one protected host
directory mounted read-only at `/run/wktbox-ports`. Mounting the directory,
rather than the individual config file, makes atomic file replacement visible
inside a running sidecar.

### Host-facing publication bridge

A separate outer `portbridge` service runs
`wktbox-loopback publish-serve`. It reads `ports.json`, listens on container
ports rendered in `ports.override.yml`, and forwards each stream to
`docker:<box-port>`.

The generated override publishes only the explicitly requested listeners on
real-host loopback. Changing publications recreates only `portbridge`; it never
restarts DinD or workload containers. Keeping this service separate prevents
collisions with Webtop's automatic-localhost namespace.

## Reconciliation and Transactions

The manager holds the existing global and per-box locks for every mutation.
Each mutation follows:

1. load and reconcile the current box;
2. parse and validate the complete request;
3. perform non-mutating host and workload preflight probes;
4. render candidate files;
5. activate candidate relays;
6. save desired state only after activation succeeds;
7. on failure, restore the previous files and runtime before returning.

If rollback itself fails, the command returns both errors and marks the box
port status as degraded. A later `up`, `restart`, or port mutation retries the
desired state.

## Status and Output

`wktbox port list` shows desired and observed state:

```text
NAME       DIRECTION  HOST                  BOX/CONTAINERS       STATE
postgres   import     127.0.0.1:5432        localhost:5432       ready
api        publish    127.0.0.1:18000       docker:8000          ready
```

JSON output is a stable array with name, direction, source, target, state, and
optional error. `wktbox status` adds a concise port-forwarding section but
retains the existing automatic-localhost section.

Errors distinguish invalid mapping, duplicate mapping, host bind conflict,
workload localhost conflict, stopped box, relay startup failure, and rollback
failure.

## Security

- Publications bind only real-host loopback.
- The import relay accepts connections only after a random-token handshake.
- Tokens and process metadata are never mounted into workload containers.
- No host Docker socket is mounted.
- Entering workload network namespaces expands the trusted sidecar's
  capabilities and is documented as operational isolation, not a hostile-code
  boundary.
- Imported host services receive traffic with the relay process's host
  identity; users must still configure application authentication.

## Testing

Unit tests cover parsing, validation, sorting, state round trips, candidate
rendering, relay authentication, TCP copying, rollback, CLI output, and
lifecycle calls.

Linux namespace tests verify bind probing and proxying with temporary network
namespaces when capabilities are available; pure unit tests inject namespace
operations elsewhere.

The Docker E2E test creates two workload containers and verifies:

1. one host HTTP service imported at the same localhost port in both workloads;
2. multiple imports applied in one atomic command;
3. remapped localhost ports;
4. an existing workload listener rejects the whole batch without residue;
5. a newly started conflicting workload is stopped and reported;
6. multiple box publications reach HTTP and raw TCP from the real host;
7. a host bind conflict rejects the whole publication batch;
8. removing mappings closes listeners;
9. stop preserves desired state but closes listeners;
10. restart restores both directions;
11. two boxes may reuse container-local and box ports without cross-talk;
12. no unrequested DinD port appears on the real host.

Release verification builds the native CLI and the Webtop image containing the
updated `wktbox-loopback` helper.
