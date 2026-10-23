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

## Ops-floor repair found during PR validation

The native amd64 and arm64 jobs on the initial PR head failed before running
doctor: the tagged fixture configured identical public and operational listener
addresses. Local reproduction confirmed the same boot refusal. Advancing through
the real CLI flow exposed missing audit-retention defaults, missing same-user
socket trust and obsolete synthetic backup health state.

The fixture now uses distinct ephemeral listener addresses, the existing private
CLI socket helper and matching trust entry, explicit audit-retention defaults,
and a real recorded encrypted backup archive. Its operational timestamps remain
synthetic; backup retention can verify the actual archive bytes. JSON failures
and unexpected health findings include sanitized diagnostics.

The doctor and storage-refusal tests pass locally with the source-built CLI,
including all eight doctor states. The existing listener acceptance/refusal
tests also pass. The local datastore volume is above its critical threshold,
which the test preserves as an error; this is functional evidence, not healthy
native capacity acceptance. Fresh hosted amd64/arm64 evidence is still required.

The ops-floor path filters now include the application bootstrap, CLI,
configuration and backup service owners so changes to those contracts trigger
the native fixture. The new artifact upload action successfully retained the
original failure evidence without deprecation warnings in
[the amd64 job](https://github.com/Hikyo-Org/Hikyo/actions/runs/39911360688/job/120274876810).
