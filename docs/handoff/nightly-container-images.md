# Nightly container publication

## Result

The nightly workflow publishes `ghcr.io/hikyo-org/hikyo:<nightly-version>`
without the GitHub tag's leading `v`, then promotes the verified digest to
`:nightly`. Both Linux amd64 and arm64 use the already released GoReleaser
binaries and `Dockerfile.release`. Stable tags and `:latest` are untouched.

## Publication and retries

The existing release job exports its durable tag, version and source commit,
including when it skips an already published nightly. A separate container job
downloads and authenticates the complete release inventory with the runtime
verifier, then compares extracted Linux binaries against signed provenance.

`scripts/release/nightly-image.sh` prepares the inputs, distinguishes missing
immutable tags from registry failures, and validates both published platforms.
It checks runtime configuration and exact binary bytes, runs the SPA smoke
test, signs the OCI digest with GitHub OIDC and verifies the workflow identity.
Only then may the newest published nightly move the alias. A failed-job or full
workflow rerun reuses the immutable image instead of overwriting it.

The container job alone receives `packages: write`. The scoped nightly GitHub
App continues to own GitHub tags and release assets. OCI signatures are separate
from the unchanged closed `nightly/v1` archive inventory.

## Validation

- Nightly fixtures cover verifier/source/provenance rejection, immutable lookup
  failures, platform/config/binary mismatch, failed smoke/signature checks,
  successful retries and prevention of older-release promotion.
- Workflow lint, ShellCheck, canonical image-root fixtures and release binary
  reuse fixtures passed.
- Docs typecheck/build and OSS/PWA policy gates passed.
- A real Linux arm64 image built locally from the published
  `v0.0.1-nightly.20260913.39.ga60e8dc3` archive reports the exact version and
  serves SPA HTML and JavaScript with a read-only root filesystem. Extracted
  binary SHA-256 `6c3e36a10458cc149306501476393a22c3b7145b0169489a7c341d9055ece909`
  matched the published binary-provenance entry. This packaging smoke did not
  independently authenticate the complete downloaded release inventory.
- Cross-provider review was skipped because current Claude quota information
  was unavailable after the required three-minute response window. Parent
  ordinary review and local checks do not substitute for that review.

## Activation

No image was published by local verification. Merge the workflow change and
let exact-head main CI pass before dispatching `nightly-release`, or allow its
02:00 UTC schedule to run. Verify the job summary digest, both registry
platforms, signature and anonymous pull access before claiming publication.
Existing registry permissions/visibility could not be inspected locally:
the current GitHub CLI token lacks `read:packages` scope.

User instructions and verification commands live in
[installation](../site/src/content/docs/docs/installation.mdx#nightly-container-images)
and the [signed-nightly runbook](../operations/signed-nightlies.md#container-images).
