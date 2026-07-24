# Cross-box connections

Boxes are isolated by default. Use an explicit connection when workloads in
two or more ready boxes need to communicate while keeping their Docker daemons,
images, volumes, and internal project networks separate.

## Connect boxes

List the boxes, then select each member by its full ID, a unique ID prefix of at
least three characters, or its unique name:

```bash
wktbox list
wktbox connect a4f dfe --name dev-stack
```

`connect` is the command name because the operation creates bidirectional
network reachability. `bind` usually suggests a port, address, mount, or
one-way association and would make the contract less clear.

The definition is persistent. `wktbox stop` preserves it, while `wktbox up` and
`wktbox restart` reconcile the runtime network and container attachments.

## Reach a workload

Every member gets a stable alias based on its full box ID:

```text
<box-id>.wktbox
```

Use the published port from the target project's Compose file. For example, if
box `dfe31c662a91` contains:

```yaml
services:
  api:
    ports:
      - "8000:3000"
```

a workload container in another member can reach it at:

```text
http://dfe31c662a91.wktbox:8000
```

The port on the left of `ports:` is the reachable published port. `EXPOSE`
alone and unpublished container ports are not made reachable by Wktbox. The
project Compose file is not rewritten.

Aliases deliberately use immutable full IDs instead of names, which can change
or collide. The shared bridge connects each member's outer DinD endpoint; DNS
inside workload containers resolves the alias through a small sidecar in that
box. The host Docker socket is never mounted.

## Inspect and remove topology

```bash
wktbox connections
wktbox connections dev-stack
wktbox --json connections dev-stack
wktbox disconnect dev-stack
```

The topology reports `ready`, `degraded`, or `error`. A stopped or missing
member makes a connection degraded. Bring the member back with `wktbox up`;
Wktbox reattaches it automatically.

`disconnect` removes only the shared bridge and its attachments. It does not
stop or destroy member boxes. Destroying one member removes a two-member
connection; larger connections keep their remaining members.

## Trust boundary

Connect only trusted development boxes. Members intentionally share a network,
so a process can attempt to reach listeners on peer endpoints, including
published application ports. The peer DinD API remains protected by its own
TLS authority, and boxes still have independent daemons and storage, but a
connection is not a tenant or hostile-code boundary. See
[Security](security.md).

