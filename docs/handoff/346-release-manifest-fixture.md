# Handoff: #346 canonical release-manifest fixture

Issue: https://github.com/Hikyo-Org/Hikyo/issues/346 (parent #326; audit
finding `F-S36-2`). Fixed point before this work:
`b891007b0d8a6ec8f318af506172cc217640089e`.

## Contract

- `scripts/release/create-manifest.sh` now owns the v1 fixture manifest shape,
  artifact classification, artifact hashes, image tag, and chart app version.
- `scripts/release/test-fixtures.sh` supplies only classifiable release
  artifacts and canonical candidate inputs before invoking that producer.
- Intentional v2 and negative-fixture mutations remain local `jq` patches;
  they model contradictory or historical bundles rather than a second valid
  manifest producer.
- `scripts/release/create-manifest_test.sh` passes producer output through the
  complete `verify-bundle.sh` contract with only cosign cryptography stubbed.
- Runtime release bytes, schema, ordering, and generated outputs are unchanged.

## Validation

- `git diff --check`: passed before the initial push.
- `scripts/release/create-manifest_test.sh`: passed; producer output completed
  verifier checks with only cosign cryptography stubbed.
- `scripts/release/test-fixtures.sh`: local start blocked because `cosign` is
  not installed; CI installs its pinned cosign version before this fixture.
- PR #400 exact-head CI is the full release-fixture gate.

## Review

- Spec round 1: `CLEAN`.
- Standards round 1 found this missing handoff and a duplicated SHA helper;
  both were added or replaced with the release library owner.
- Standards and spec round 2: `CLEAN`.
