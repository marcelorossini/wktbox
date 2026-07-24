# Current-checkout quickstart

The current checkout is Wktbox's default and primary path. A linked worktree is
optional; use one only when you also want branch-level checkout isolation.

## 1. Check prerequisites

From the repository you want to develop:

```bash
wktbox doctor
```

Fix every failed required check before continuing. Wktbox does not silently
fall back to running project commands on the host.

## 2. Start the box

```bash
wktbox up
```

The checkout is available as `/workspace` in Webtop and the isolated DinD
daemon. Repeating `up` reconciles generated configuration and starts an
existing stopped box.

You can skip explicit `up` when immediately running a command because `run`
ensures the box is ready:

```bash
wktbox run -- go test ./...
```

Arguments after `--` are passed without shell reconstruction, and the child
exit code becomes the Wktbox exit code.

## 3. Run project Compose

```bash
wktbox compose -- up --build -d
wktbox compose -- ps
```

The project's original Compose file runs inside the dedicated daemon. Keep its
normal port mappings: a published `5173:5173` becomes `localhost:5173` inside
Webtop, without competing with other boxes.

Open the desktop:

```bash
wktbox open
```

Inspect box state and routes:

```bash
wktbox status
wktbox --json status
```

## 4. Pass environment data intentionally

Mount an external project environment file read-only:

```bash
wktbox --env-file "/secure/project.env" run -- docker compose config
```

Pass a process-only value:

```bash
wktbox --env CI=true run -- go test ./...
```

Flags are resolved on every invocation. Put stable local choices in
`.wktbox.local.yml`, keep that file untracked, and read
[Configuration](configuration.md) for precedence.

## 5. Preserve or clean up

Stop while retaining the daemon's images, volumes, and data:

```bash
wktbox stop
```

Restart:

```bash
wktbox restart
```

Destroy the selected box only when cleanup is intended:

```bash
wktbox destroy --force
```

Current-checkout boxes are not removed automatically when a task finishes.
Stale recorded paths can be reviewed safely with `wktbox prune`; its
destructive form requires explicit `wktbox prune --force`.

Next: [Optional worktrees and lifecycle](worktrees.md).
