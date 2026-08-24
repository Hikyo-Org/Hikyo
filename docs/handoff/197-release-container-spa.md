# Issue #197: release container SPA proof

## State

Implemented. Release archives and OCI inputs already share the `hikyo`
GoReleaser build, whose `.goreleaser.yaml` flags include `-tags=ui` for both
Linux architectures. This slice closes the remaining packaging gap by booting
the exact candidate image and proving its embedded browser surface.

## Contract

- CI resolves the locally built image to its immutable image ID before any
  assertion.
- The release workflow pulls and runs the published multi-architecture image
  by the digest returned from `docker/build-push-action`.
- `scripts/release/smoke-image-ui.sh` boots that reference with isolated SQLite
  and state paths, update checks disabled, a read-only root filesystem, and a
  private host port.
- The smoke requires `/` to return `200` SPA HTML with `text/html`, extracts a
  content-hashed JavaScript reference, then requires that exact asset to be
  non-empty and served as `text/javascript`.
- Untagged development builds keep their existing API-only `404` contract;
  `TestNoEmbeddedUIStillServesTheAPI` remains the direct public-router proof.

## Files

- `.github/workflows/ci.yml`: immutable local candidate smoke.
- `.github/workflows/release.yml`: published digest smoke before SBOM and chart
  publication.
- `scripts/release/smoke-image-ui.sh`: shared image-level HTTP probe.
- `scripts/ci/check-release-binary-reuse_test.sh`: fail-closed workflow and
  smoke-contract fixture.

## Validation

- `./scripts/ci/check-release-binary-reuse_test.sh`
- `./scripts/release/prepare-image-root_test.sh`
- `shellcheck scripts/release/smoke-image-ui.sh scripts/ci/check-release-binary-reuse_test.sh`
- `./scripts/ci/run-go-tool.sh actionlint`
- `go test -count=1 ./internal/server -run '^TestNoEmbeddedUIStillServesTheAPI$'`
- `go test -count=1 -tags ui ./internal/server/... ./internal/webui/...`
- Local Linux/arm64 `-tags=ui` image booted by immutable image ID; `/` served
  the Hikyo shell and `/assets/index-c5rIZ486.js` as `text/javascript`.
