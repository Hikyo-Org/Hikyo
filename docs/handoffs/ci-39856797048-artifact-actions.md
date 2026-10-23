# CI artifact action runtime upgrade

## Problem and scope

The successful `apple-sign` job in [run 39856797048](https://github.com/Hikyo-Org/Hikyo/actions/runs/39856797048/job/120119025776)
warned that its artifact actions targeted deprecated Node 20. Upload also emitted
`punycode` and `url.parse()` deprecations from bundled dependencies.

Hikyo already selects Node 26.7.0 through `.nvmrc`. JavaScript actions declare
their own runtime independently; these upstream releases use `node24`.

## Changes

- Pin every workflow upload to `actions/upload-artifact` 7.0.2
  (`cf430e030ddbb5b0abf93d22962f4752f3646cd9`).
- Pin every workflow download to `actions/download-artifact` 8.0.2
  (`9000827ccba6bdab643e8b6fd33ac0654aef8333`).
- Update matching transfer fixtures and contract expectations.
- Accept only these exact upgrades when comparing CI jobs with the frozen
  coverage inventory. Preserve the inventory and fingerprint all other step
  configuration. Six mutation cases reject mutable/previous pins, fail-open
  uploads and ignored missing files.

Artifact names, paths, permissions, retention and conditions stay the same.
Release downloads already merge into their target directory. Uploads retain
upstream's default zipped archive format. The updated downloader fails on digest
mismatches by default.

## Validation and remaining proof

Local validation passed on the implementation before the PR:

- `FAST_FEEDBACK_BASE=HEAD scripts/ci/fast-feedback.sh --working-tree`:
  formatting, compilation, ShellCheck and workflow linting.
- `go test -p 2 -parallel 2 ./scripts/ci ./internal/adapter/githubactions -count=1`.
- `bash scripts/ci/check-build-artifact-reuse_test.sh`.
- The upstream upload bundle's no-files path completed under Node 24.21.0 and
  Node 26.7.0 without deprecation warnings.

The smoke does not transfer artifacts. Hosted CI and the next Apple release job
must establish real transfer behavior and whether all transfer-time deprecation
warnings disappear. A local pass is not release, notarization or deployment proof.

## Upstream references

- [Upload 7.0.2](https://github.com/actions/upload-artifact/releases/tag/v7.0.2)
- [Download 8.0.2](https://github.com/actions/download-artifact/releases/tag/v8.0.2)
- [Action runtime metadata](https://docs.github.com/en/actions/reference/workflows-and-actions/metadata-syntax#runsusing-for-javascript-actions)
