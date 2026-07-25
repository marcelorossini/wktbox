# Changelog

All notable changes to Wktbox are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.3.1] - 2026-07-25

### Fixed

- Fixed automatic Webtop localhost forwarding for inner Docker TCP
  publications bound only to the DinD loopback, including remapped ports such
  as `127.0.0.1:5174:5173`.

## [0.3.0] - 2026-07-24

### Added

- Added persistent `wktbox port import` mappings that expose real-host TCP
  services as `localhost` in every workload container.
- Added loopback-only `wktbox port publish` mappings from real-host ports to
  selected DinD-published box ports.
- Added atomic multi-service batches, conflict rejection, removal, lifecycle
  restoration, human/JSON status, and an authenticated per-box host relay.
- Added an always-running graphical Chromium in every ready Webtop, with
  automatic reopening, a persistent profile, and discoverable `browserCdp`
  status fields on a box-specific host loopback port.
- Added the complete Wktbox branding package and a theme-aware logo to the
  project README.

### Security

- Protected host imports with per-box random 256-bit relay tokens and kept the
  host Docker socket and relay credentials out of workload containers.
- Bound the unauthenticated Chromium CDP endpoint to host loopback and
  documented that it grants full control over the browser profile.

## [0.2.0] - 2026-07-24

### Added

- Accepted any existing directory as a workspace, with automatic optional Git
  detection and exact `--path` mounting.
- Added non-Git workspace diagnostics, agent guidance, scorer coverage, and a
  real Docker E2E that verifies Wktbox never initializes Git implicitly.

## [0.1.1] - 2026-07-24

### Fixed

- Made the Unix installer conform to POSIX `sh`, matching every documented
  installation and update command.

## [0.1.0] - 2026-07-24

### Added

- Persistent `connect`, `connections`, and `disconnect` commands for explicit
  bidirectional networking between two or more boxes.
- Stable `<box-id>.wktbox` discovery inside workload containers, with lifecycle
  reconciliation and human/JSON topology output.
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

- Documented that connected boxes share a trusted development network while
  retaining separate DinD daemons and storage.
- Documented the trusted-code boundary: Wktbox is operational isolation, not a
  sandbox for hostile code.
- Kept the host Docker socket out of boxes and protected the internal DinD API
  with container-local TLS.
- Bound host-facing services to loopback and stored generated state with
  restrictive permissions.

[Unreleased]: https://github.com/marcelorossini/wktbox/compare/v0.3.1...HEAD
[0.3.1]: https://github.com/marcelorossini/wktbox/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/marcelorossini/wktbox/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/marcelorossini/wktbox/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/marcelorossini/wktbox/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/marcelorossini/wktbox/releases/tag/v0.1.0
