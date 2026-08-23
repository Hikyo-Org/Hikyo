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
- Focused and full local checks are deferred until host memory pressure drops.
- PR #400 exact-head CI started at commit `1b186e3`.

## Review

- Spec round 1: `CLEAN`.
- Standards round 1 found this missing handoff and a duplicated SHA helper;
  both were added or replaced with the release library owner.
