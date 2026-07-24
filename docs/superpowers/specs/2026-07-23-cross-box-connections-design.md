# Cross-box Connections Design

## Objective

Allow two or more existing Wktbox boxes to communicate through the TCP or UDP
ports published by their internal Compose workloads, without merging Docker
daemons, volumes, images, or project-internal networks.

The feature is controlled by the host CLI and exposes both human-readable and
stable JSON views of every connection.

## CLI vocabulary

The public vocabulary is:

```text
wktbox connect <box> <box> [<box>...] [--name <name>]
wktbox connections [<connection>]
wktbox disconnect <connection>
```

`connect` is preferable to `bind` because Docker already uses “bind” for port
bindings and bind mounts. It is preferable to `link` because Docker links are a
legacy feature with different semantics. A `network` command group was rejected
because Wktbox should expose the user intent—connecting boxes—rather than the
implementation resource.

Box selectors accept a full 12-character box ID, a unique ID prefix of at least
three characters, or a unique box name. Connection selectors accept the
generated connection ID, a unique ID prefix of at least three characters, or
the optional name.

The `connect` command:

- requires at least two distinct boxes;
- requires every selected box to exist and be ready;
- creates one private bridge for the complete member set;
- is idempotent for the same set of members and name;
- generates a stable 12-character connection ID from the sorted member IDs;
- uses the generated ID as the display name when `--name` is omitted;
- rejects a name already assigned to a different member set;
- persists the desired connection before reporting success.

The `connections` command lists all connections when no selector is supplied
and inspects one connection when a selector is supplied. Its human output shows
the connection name and ID, runtime state, network name, and one line per box
with box name, ID, status, and DNS alias.

The `disconnect` command removes the selected connection as one unit. It
disconnects its members, removes its managed network, and only then removes the
desired connection from state. Repeating it after successful removal reports
that the connection does not exist.

## Connectivity contract

Each member is reachable through:

```text
<box-id>.wktbox:<published-port>
```

The alias always uses the full box ID so it is stable and collision-free.
Optional connection and box display names never become DNS names.

Only ports published by the target box's internal Docker daemon are reachable.
For example, an internal Compose mapping `8000:3000` makes
`<target-id>.wktbox:8000` available to the other members. `EXPOSE` alone does
not make a port reachable. Bindings limited to the target DinD loopback, such
as `127.0.0.1:8000:3000`, remain loopback-only and are not part of the
cross-box contract.

Connectivity is available from both:

- the Webtop/runner of every member; and
- workload containers created by every member's DinD daemon.

The feature does not rewrite the user's Compose file. It does not place
workload containers from separate boxes on one Docker network. Instead it
routes member traffic to the target DinD's published ports.

## Runtime architecture

Every connection owns one bridge network on the host Docker daemon:

```text
wktbox-connect-<connection-id>
```

The network has management, connection ID, connection name, and Wktbox version
labels. For each member, Wktbox attaches the external `docker` and `webtop`
containers at runtime. The DinD container receives the alias
`<box-id>.wktbox`. Runtime attachment avoids Compose's automatic `docker`
service alias on the shared network, which would make the private
`DOCKER_HOST=tcp://docker:2376` name ambiguous.

Each box also runs a required `interconnect` sidecar in
`network_mode: service:docker`. It provides a DNS forwarding listener on the
DinD bridge gateway, while Docker's own embedded DNS on `127.0.0.11` remains
the upstream resolver. The DinD daemon uses the gateway listener as the default
DNS server for workload containers. Queries for another member's
`<box-id>.wktbox` alias therefore resolve through the host Docker network; all
other queries continue through the existing Docker DNS path.

The sidecar is present even when a box has no connection so a later `connect`
does not require restarting the DinD daemon. It binds only to the inner bridge
gateway, not to the host or Webtop loopback.

The external Compose project remains private by default. Only an explicit
`connect` operation attaches it to a managed shared bridge.

## State model

`state.json` retains schema version 1 and gains an optional `connections`
object. Older files load with an empty connection map.

Each record contains:

- connection ID;
- optional user name;
- host Docker network name;
- sorted member box IDs;
- creation time;
- last reconciliation time;
- last known runtime state and error.

Runtime state values are `ready`, `degraded`, and `error`:

- `ready`: the network exists and every running member has both managed
  endpoints attached;
- `degraded`: desired state exists but a member is stopped or an endpoint is
  absent;
- `error`: Docker inspection or reconciliation failed.

Connection records store no container IDs or IP addresses because both are
ephemeral. Runtime inspection always resolves current containers through the
existing Wktbox labels.

## Reconciliation and lifecycle

The desired state in `state.json` and the real state in Docker are reconciled
under the global lock.

- `connect` creates or inspects the managed network, attaches current
  containers, verifies all endpoints, and saves the resulting state.
- `connections` is read-only with respect to desired state but refreshes the
  reported runtime status from Docker.
- `up` and `restart` reattach the current `docker` and `webtop` containers for
  every connection containing that box. This repairs Compose recreation.
- `stop` preserves desired connections and the managed network. The runtime
  state becomes degraded until the box runs again.
- `destroy` removes the box from each connection. Connections left with fewer
  than two members are removed; larger connections are reconciled and kept.
- `prune --force` uses the same destroy path and therefore receives the same
  connection cleanup.
- `disconnect` never removes box volumes, internal networks, or workloads.

Managed networks discovered through labels but missing from local state are
reported as warnings and are not adopted or deleted automatically. A
connection record whose network is missing is recreated on reconciliation.

## Failure handling

Operations are ordered to avoid claiming connectivity that does not exist:

1. validate selectors and member states;
2. persist the intended connection;
3. create or inspect the managed network;
4. attach and verify all current endpoints;
5. persist the observed runtime state;
6. render success.

If a Docker operation fails after desired state is saved, the command returns a
runtime error and preserves an `error` connection record for inspection and
retry. Retrying `connect` with the same members reconciles it.

Disconnect follows the inverse order. If endpoint or network removal fails,
the desired record remains with the error so a retry is safe. “Already
connected” and “not connected” responses from Docker are normalized where they
represent the requested end state.

Selector errors distinguish not found from ambiguous. Duplicate member
selectors are rejected before Docker is called.

## Output contract

JSON uses an array for `connections` and a single object for an inspected or
newly created connection. A connection object contains:

```json
{
  "id": "64f420a77f31",
  "name": "dev-stack",
  "network": "wktbox-connect-64f420a77f31",
  "state": "ready",
  "members": [
    {
      "id": "a4f8c9137d2b",
      "name": "frontend",
      "status": "ready",
      "alias": "a4f8c9137d2b.wktbox"
    }
  ],
  "createdAt": "2026-07-23T12:00:00Z",
  "reconciledAt": "2026-07-23T12:00:01Z",
  "error": ""
}
```

Human output is a compact adjacency view:

```text
Connection dev-stack (64f420a77f31) is ready
Network: wktbox-connect-64f420a77f31
Members:
  frontend (a4f8c9137d2b) -> a4f8c9137d2b.wktbox [ready]
  api      (dfe31c662a91) -> dfe31c662a91.wktbox [ready]
```

`wktbox list` and `wktbox status` keep their existing output contracts. The
dedicated `connections` command is the canonical topology view.

## Security

A connection intentionally weakens network isolation only between its explicit
members. It does not share Docker APIs, TLS certificates, volumes, images,
container namespaces, or the host Docker socket.

Any process in a member Webtop or workload container can attempt to reach every
port published by another member's DinD. Wktbox adds no authentication or
application-layer authorization. Human output and documentation must state
this boundary.

The managed bridge is not published to host interfaces. Existing Webtop and
gateway host bindings remain limited to loopback.

## Testing and acceptance

Unit tests cover:

- deterministic IDs and sorted unique membership;
- selector resolution and ambiguity;
- backward-compatible state loading;
- Docker network create/inspect/connect/disconnect/remove command contracts;
- idempotent Docker responses;
- reconciliation after container recreation;
- stop, restart, destroy, and prune behavior;
- CLI argument validation and error codes;
- human and JSON topology output;
- DNS UDP/TCP forwarding and cancellation.

Integration tests use a temporary state root and a recording Docker runner to
exercise the complete CLI orchestration.

A Linux runtime test creates two boxes with separate DinDs and workloads that
publish the same port. Before connection, the remote alias must not resolve.
After `connect`, both Webtops and workload containers must resolve the other
box alias and receive the expected box-specific payload. After `disconnect`,
the alias must stop resolving while each box's own workload and data remain
intact.

The feature is complete only when:

1. two or more boxes can be connected with one CLI command;
2. published ports are reachable bidirectionally by stable aliases;
3. DinD, volume, image, and internal-network isolation remains intact;
4. connections survive stop/start and Compose container recreation;
5. connections are visible in human and JSON output;
6. disconnect and destroy clean up only their owned connection resources; and
7. the full existing test, race, vet, documentation, image, and runtime suites
   remain green.
