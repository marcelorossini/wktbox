# Bidirectional port forwarding

Wktbox keeps ordinary project ports private to each box. Use the explicit
`wktbox port` commands when traffic must cross the real-host boundary. These
commands require an existing ready box; run `wktbox up` first.

Forwarding is TCP only. It preserves HTTP, HTTPS, WebSocket, PostgreSQL, Redis,
and other TCP protocols without interpreting them.

## Import a host service as container localhost

Normally, `localhost` inside a container refers to that container, not to the
real host. An explicit import changes that for the selected ports:

```bash
wktbox port import 1234
```

The route is:

```text
real host 127.0.0.1:1234 -> every workload container localhost:1234
```

After the command, both `127.0.0.1:1234` and `localhost:1234` work inside every
running workload container. Future workloads receive the same import
automatically.

Use `--map` to change the port seen by the containers:

```bash
wktbox port import \
  --map api=127.0.0.1:1234:4321
```

That makes the host service at `127.0.0.1:1234` available to each workload at
`localhost:4321`.

## Import multiple services in one command

Positional imports preserve each number:

```bash
wktbox port import 1234 5432 6379
```

Named mappings support multiple services and remapping:

```bash
wktbox port import \
  --map api=127.0.0.1:1234:4321 \
  --map postgres=127.0.0.1:5432:5432 \
  --map redis=127.0.0.1:6379:16379
```

Do not mix positional ports and `--map` in the same command.

Before activation, Wktbox checks every requested localhost port in every
running workload. If one container already listens on any requested port, the
entire batch fails and no mapping, proxy, configuration, or state change is
kept. If a future container starts with a conflicting reserved port, that
container is stopped and `wktbox status` reports `port_import_conflict`; the
desired import remains active for the other workloads.

## Publish a box port on the real host

A publication goes in the opposite direction. The box port is the left side of
an inner Compose publication. For an inner mapping `8000:3000`:

```bash
wktbox port publish \
  --map api=127.0.0.1:18000:8000
```

The route is:

```text
real host 127.0.0.1:18000 -> box docker:8000 -> workload port 3000
```

Publish multiple services by repeating `--map`:

```bash
wktbox port publish \
  --map frontend=127.0.0.1:15173:5173 \
  --map api=127.0.0.1:18000:8000 \
  --map database=127.0.0.1:15432:5432
```

Publications accept only `127.0.0.1` or `::1`; Wktbox never broadens them to a
LAN interface. It checks every new host listener before changing runtime
state. A conflict rejects the entire batch. The target box port must already
be published by the inner Docker daemon for connections to succeed.

## Inspect and remove mappings

```bash
wktbox port list
wktbox --json port list

wktbox port remove api database
wktbox port remove --all-imports
wktbox port remove --all-publications
```

The JSON output contains only the public name, direction, source, target,
state, and optional error. Relay tokens, process IDs, and generated file paths
are never included.

`wktbox stop` closes active listeners but preserves desired mappings.
`wktbox up` and `wktbox restart` restore both directions. `wktbox destroy`
removes the mappings with the selected box. Inner workloads still follow
their own Docker restart policy; use a policy such as `unless-stopped` when a
service must resume automatically with the box.

## Names and transaction behavior

Names use lowercase letters, digits, dots, underscores, and dashes. A name is
unique within one box regardless of direction. Imports cannot share the same
container-local port, and publications cannot share the same host listener.

Every add or remove command is transactional. Parse errors, duplicate names,
host bind conflicts, workload localhost conflicts, relay failures, Compose
failures, and persistence failures restore the previous files, runtime, and
desired state before the command returns.

See [Security](security.md) for the relay trust model and
[Troubleshooting](troubleshooting.md) for conflict diagnosis.
