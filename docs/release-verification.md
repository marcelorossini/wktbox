# Release verification

Official releases publish six archives, `checksums.txt`, Unix and PowerShell
installers, build-provenance attestations, and two multi-architecture OCI
images.

## Expected targets

| Target | Archive |
|---|---|
| linux/amd64 | `wktbox_VERSION_linux_amd64.tar.gz` |
| linux/arm64 | `wktbox_VERSION_linux_arm64.tar.gz` |
| darwin/amd64 | `wktbox_VERSION_darwin_amd64.tar.gz` |
| darwin/arm64 | `wktbox_VERSION_darwin_arm64.tar.gz` |
| windows/amd64 | `wktbox_VERSION_windows_amd64.zip` |
| windows/arm64 | `wktbox_VERSION_windows_arm64.zip` |

Each archive contains one `wktbox` or `wktbox.exe` executable, `LICENSE`, and
`README.md`.

## Checksums

Download the release into an empty directory:

```bash
gh release download v0.1.0 \
  --repo marcelorossini/wktbox \
  --pattern 'wktbox_*' \
  --pattern checksums.txt
```

Because checksum entries use the reproducible build path `dist/`, either place
archives under `dist/` or normalize that prefix:

```bash
sed 's#  dist/#  #' checksums.txt | sha256sum --check -
```

On macOS use `shasum -a 256 -c -`. On Windows compare every archive with
`Get-FileHash -Algorithm SHA256`.

The checksum proves that the downloaded bytes match the release manifest; it
does not by itself prove who built them.

## Artifact attestations

With GitHub CLI authenticated:

```bash
gh attestation verify wktbox_0.1.0_linux_amd64.tar.gz \
  --repo marcelorossini/wktbox
gh attestation verify checksums.txt \
  --repo marcelorossini/wktbox
```

Verify the installer scripts the same way before executing them. The
attestation should identify the Wktbox repository and the release workflow at
the expected Git revision.

## Runtime images and OCI metadata

The release publishes:

```text
ghcr.io/marcelorossini/wktbox/webtop:0.1.0
ghcr.io/marcelorossini/wktbox/gateway:0.1.0
```

Inspect their multi-architecture manifests:

```bash
docker buildx imagetools inspect \
  ghcr.io/marcelorossini/wktbox/webtop:0.1.0
docker buildx imagetools inspect \
  ghcr.io/marcelorossini/wktbox/gateway:0.1.0
```

Both must contain linux/amd64 and linux/arm64. OCI annotations or image labels
identify the source URL, exact Git revision, semantic version, Apache-2.0
license, and image description. Verify image attestations by manifest digest,
not only by a movable tag.

For an anonymous pull check, log out of GHCR or use an isolated Docker
configuration:

```bash
anonymous_config="$(mktemp -d)"
DOCKER_CONFIG="$anonymous_config" docker pull \
  ghcr.io/marcelorossini/wktbox/webtop:0.1.0
```

Public runtime images must pull without repository credentials. Remove the
temporary Docker configuration afterward.

See [Installation](installation.md) for platform-specific checksum commands.
