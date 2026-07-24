# Contributing to Wktbox

Thank you for improving Wktbox. Please keep changes small, tested, and explicit
about security and lifecycle behavior.

## Development setup

Wktbox itself must run and test on the host. Do not run this repository inside
Wktbox: nested Docker-in-Docker is unsupported.

The Makefile uses the pinned `golang:1.26.5` image, so a host Go installation is
optional:

```bash
make build
make test
make test-race
make vet
```

Docker is required for containerized Go tooling, image builds, installer
acceptance tests, and E2E tests. See [Troubleshooting](docs/troubleshooting.md)
if `doctor` or Docker-based checks fail.

## Change workflow

1. Add a failing regression or contract test for behavior changes.
2. Implement the smallest coherent change.
3. Run focused tests, then the relevant repository gates.
4. Update canonical English documentation and preserve Portuguese guidance when
   the public contract changes.
5. Keep generated artifacts and evaluation transcripts out of commits.

Commit messages are written in Portuguese and begin with the repository
convention: `feat:`, `fix:`, `docs:`, `test:`, or `ci:`.

## Required checks

Before requesting review, run what is locally available:

```bash
make fmt
make test
make test-race
make vet
make install-test
make agent-skill-test
make agent-scorer-test
make docs-check
```

Changes to runtime images should also run `make images`. Changes to isolation,
networking, Git bridges, or lifecycle behavior should run `make e2e`.

## Releases

Do not edit the compiled version in source. Release builds inject a semantic
version through linker flags. A release tag must be `vX.Y.Z`, match the binary
version, pass every gate, publish both multi-architecture runtime images, and
create the GitHub Release only after artifacts and attestations succeed.

Update [CHANGELOG.md](CHANGELOG.md) and follow
[release verification](docs/release-verification.md) for distribution changes.

## Security reports

Do not open a public issue for a suspected vulnerability. Use the repository's
private GitHub security reporting channel. Remember that privileged DinD and a
writable checkout are documented trust assumptions, not vulnerabilities by
themselves; see the [security model](docs/security.md).
