# Changelog

All notable changes to Wktbox are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-07-23

### Added

- Current-checkout and optional linked-worktree Docker isolation.
- A dedicated privileged DinD daemon, Webtop, volumes, networks, images, and
  port namespace for every box.
- Automatic TCP loopback routes inside Webtop based on project Compose
  publications, plus an optional host-facing HTTP gateway.
- Host prerequisite diagnostics, structured JSON output, lifecycle commands,
  and safe stale-box pruning.
- Atomic Codex and Claude skill installation with conflict detection.
- Reproducible archives and checksum-verified installers for Linux, macOS, and
  Windows on amd64 and arm64.
- Versioned Webtop and gateway images for linux/amd64 and linux/arm64.

### Security

- Documented the trusted-code boundary: Wktbox is operational isolation, not a
  sandbox for hostile code.
- Kept the host Docker socket out of boxes and protected the internal DinD API
  with container-local TLS.
- Bound host-facing services to loopback and stored generated state with
  restrictive permissions.

[Unreleased]: https://github.com/marcelorossini/wktbox/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/marcelorossini/wktbox/releases/tag/v0.1.0
