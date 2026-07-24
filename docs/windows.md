# Windows and Docker Desktop

Wktbox publishes Windows amd64 and arm64 executables. The runtime requires
Docker Desktop using Linux containers, Docker Compose v2, and a local drive
shared with Docker.

Install with PowerShell as described in [Installation](installation.md), then
check the checkout:

```powershell
wktbox doctor --path "C:\worktrees\feature auth"
```

Local paths containing spaces are preserved. UNC paths such as
`\\server\share` are rejected in schema v1; use a shared local drive letter.

## Runtime networking

Automatic localhost uses the Linux
`network_mode: service:webtop` namespace within Docker Desktop. It does not use
the native Windows network namespace. Inside Webtop, `localhost:5173` therefore
means the box-local Linux loopback. The host browser reaches Webtop through its
assigned host loopback port or the optional gateway.

The Webtop container uses `PUID=1000` and `PGID=1000`. This does not guarantee
ideal ownership for files produced by every project image on NTFS. Prefer
Docker volumes for databases, dependency caches, and other large write-heavy
data.

## Git and filesystems

The default `git.mode: host` avoids mounting shared Git metadata into the Linux
desktop. `git.mode: mounted` is experimental on Windows: validate hooks,
symlinks, case sensitivity, permissions, antivirus behavior, and file watching
before depending on it.

Docker Desktop, NTFS, bind mounts nested through DinD, and large dependency
trees may have different performance and watch behavior from native Linux.

## Doctor failures

- Confirm Docker Desktop is running in Linux containers mode.
- Run `docker version` and `docker compose version`.
- Share the local drive containing the checkout and project environment file.
- Confirm organization policy allows privileged containers.
- Check loopback ports and free disk space.
- Avoid UNC paths and paths inaccessible to Docker Desktop.

The trust model is unchanged on Windows: privileged DinD and a writable
checkout provide operational isolation, not a hostile-code security boundary.
See [Security](security.md) and [Troubleshooting](troubleshooting.md).
