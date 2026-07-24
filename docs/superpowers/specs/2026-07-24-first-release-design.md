# Wktbox v0.1.0 Release Design

## Objective

Publish the first official Wktbox release from the current `main` branch as
`v0.1.0`, including the cross-box connection functionality currently listed
under `Unreleased`.

## Approaches Considered

1. Consolidate `Unreleased` into `0.1.0` and tag the current release commit.
   This keeps the first published version and its notes aligned with the code.
2. Publish the current code as `v0.2.0`. This preserves the existing changelog
   split but skips an official `v0.1.0` release.
3. Tag the current commit as `v0.1.0` without changing the changelog. This is
   the smallest operational change, but the generated release notes would omit
   features included in the binaries.

Approach 1 is selected.

## Release Flow

1. Move the current `Unreleased` entries into the existing `0.1.0` section,
   retaining an empty `Unreleased` heading for future changes and setting the
   release date to 2026-07-24.
2. Commit and push the changelog update directly to `main`.
3. Confirm that the pushed commit is the remote `main` head and that its CI
   succeeds.
4. Create an annotated `v0.1.0` tag at that exact commit and push the tag.
5. Monitor the `Release` workflow until it completes.
6. Verify that the final, non-prerelease GitHub Release exists and exposes the
   expected archives, checksums, installers, and container-image publication.

## Failure Handling

Do not create the tag if the changelog commit is not the remote `main` head or
if its CI fails. If the release workflow fails after the tag is pushed, preserve
the tag for diagnosis and report the failing job instead of silently moving or
replacing it.

## Success Criteria

- `v0.1.0` points to the approved release commit on `main`.
- The `Release` workflow completes successfully.
- GitHub shows `v0.1.0` as the latest, final release.
- Release notes include every feature shipped in the tagged source.
