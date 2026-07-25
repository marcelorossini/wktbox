# DinD Loopback-Published Port Design

## Context

Wktbox discovers TCP ports published by workloads on the inner Docker daemon
and mirrors each published port onto `localhost` inside Webtop. The current
discovery keeps `HostPort` but drops Docker's `HostIP`, then sends every
connection to `docker:HostPort`.

That route fails when an inner Compose project intentionally publishes only on
the DinD loopback:

```yaml
ports:
  - "127.0.0.1:5174:5173"
```

Docker listens on `127.0.0.1:5174` in the DinD network namespace. The `docker`
hostname resolves to the DinD container's bridge address, where port 5174 is
not listening. The Webtop proxy accepts the downstream connection but cannot
open its upstream connection.

## Goal

Make every TCP publication discovered from the inner Docker daemon reachable
on the same Webtop loopback port, including publications bound to
`127.0.0.1`, `::1`, wildcard addresses, or a specific DinD interface, without
rewriting the project Compose file or exposing the port on the real host.

## Architecture

### Preserve the published addresses

`PortBinding` will retain Docker's `HostIP` alongside `HostPort`, protocol, and
container port. Discovery will aggregate deterministic upstream candidates for
each TCP host port while continuing to aggregate and sort source container
names.

Each candidate is a numeric address in the DinD network namespace:

- `127.0.0.1` and `::1` remain unchanged;
- `0.0.0.0` becomes `127.0.0.1`;
- `::` becomes `::1`;
- a specific IPv4 or IPv6 address remains unchanged;
- an empty or invalid Docker address falls back to the existing
  `docker:HostPort` route for compatibility.

Duplicate candidates from dual-stack metadata or repeated bindings are
removed. Candidates are sorted so route behavior and tests remain
deterministic.

The public status target remains `docker:HostPort`. It is the stable logical
name of the inner Docker publication; physical candidate addresses are private
runtime metadata and must not be serialized.

### Dial from the DinD network namespace

The loopback sidecar already declares:

```yaml
pid: service:docker
cap_add:
  - SYS_ADMIN
  - SYS_PTRACE
```

It therefore sees the DinD process as PID 1 and already enters workload
network namespaces for explicit port imports. The automatic proxy will reuse
the same operating-system mechanism:

1. lock the current goroutine to its operating-system thread;
2. open the current and `/proc/1/ns/net` namespace handles;
3. enter the DinD network namespace;
4. create the upstream TCP connection to a numeric candidate address;
5. restore the original namespace before returning the connected socket.

A connected socket remains attached to the namespace in which it was created,
so byte forwarding continues normally after the thread returns to the Webtop
namespace.

All candidates share the existing three-second connection deadline. The proxy
tries them in order and returns the joined error only if every candidate
fails. A publication without candidates uses its logical `docker:HostPort`
target through the ordinary dialer, preserving compatibility for direct unit
fixtures.

### Reconciliation and live metadata

The listener remains bound only to `127.0.0.1` and `::1` inside Webtop.
Connections read the current publication metadata at accept time rather than
capturing the target when the listener is first created. A Docker event that
changes `HostIP` for an existing `HostPort` therefore affects new connections
without closing and rebinding the Webtop listener.

Source-only metadata changes retain the same listener as today.

### Status semantics

`listening` continues to mean that Wktbox successfully opened both Webtop
loopback listeners. It does not perform periodic upstream probes. Probing
arbitrary TCP services would create observable connections to databases,
queues, and other protocols and would still not prove application-level
health.

End-to-end reachability is covered by the regression E2E. A failed connection
does not remove the listener because the inner service may be temporarily
starting or restarting.

## Error handling

- Invalid namespace PID or port values fail before `setns`.
- Failure to open, enter, or restore a namespace is wrapped with the namespace
  PID and operation.
- If restoring the original namespace fails, any newly opened upstream socket
  is closed and the restoration error is returned.
- Candidate connection failures are joined, retaining evidence from every
  attempted binding.
- Downstream behavior remains unchanged: an upstream failure closes only the
  current downstream connection.

## Testing

Unit tests will prove:

1. Docker inspect conversion preserves `HostIP` and still deduplicates exact
   binding duplicates.
2. Discovery normalizes IPv4 and IPv6 wildcard bindings, preserves loopback
   bindings, sorts and deduplicates candidates, and retains `docker:HostPort`
   as the logical status target.
3. The reconciler uses the newest candidates for an existing listener.
4. The namespaced dial helper validates inputs and restores the caller's
   namespace on failure paths that can be exercised without privileges.

The automatic-loopback E2E will add the exact regression:

```text
Webtop localhost:5174
  -> Wktbox listener
  -> DinD namespace 127.0.0.1:5174
  -> frontend:5173
```

It will assert Docker reports `HostIp=127.0.0.1`, the frontend returns HTTP
200 through Webtop localhost, the route remains isolated between two boxes,
and port 5174 is not published by any outer Wktbox container on the real host.

The full Go suite, race suite, vet, image tests, and automatic-loopback E2E
must pass before merge and tagging.

## Non-goals

- Changing explicit host-to-box publications or host-to-workload imports.
- Rewriting user Compose port bindings.
- Publishing inner application ports on the real host.
- Changing `listening` into an application-health state.
- Adding UDP forwarding.
