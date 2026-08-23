# Issue #353 — centralize restore secret classification

Issue: https://github.com/Hikyo-Org/Hikyo/issues/353 (parent #326; audit
finding `F-S06-1`).

**State: implemented.** Restore staging now persists the secret-classification
decision once; impact preview reads that persisted decision through the pending
change selected by `resolveVersions`.

## Contract

- `PendingChange.Secret` owns the restore-specific sticky-secret decision.
- Impact preview combines the persisted pending-change flag with the current key
  classification, preserving protection after either historical secret material
  or a current reclassification.
- Restore retry resets no longer maintain a parallel `stickySecret` shadow map.
- Contract or migration decisions: none. Generated outputs: none.

## Coverage

- `live_sticky_secret_restore_previews_as_secret` restores a live value
  occurrence published as secret after its key is reclassified to config.
- The preview must still report `secret` and expose neither `Before` nor `After`.
- Existing restore formula, payload collection, superseded-secret, pending-draft,
  and server wire tests remain the named regression set.
- Red-before-green is not available at the public behavior seam: the removed
  shadow map was byte-equivalent to `PendingChange.Secret`, so old code produces
  the same preview. The scenario instead fails if the persisted flag stops
  carrying a reclassified secret occurrence into preview.

## Validation

- Draft PR #387 previously passed build, test, race, fuzz, web, generated, and
  analysis jobs at `a4fcd1c`; only DCO failed because the bot commit lacked a
  sign-off.
- Corrected sticky-occurrence SQLite scenario: 2 passed.
- Named restore regression set: 6 passed in `internal/conformance`.
- `TestImpactPreviewWirePreservesProtectedState`: passed in `internal/server`.
- `go test -count=1 ./...`: 3,543 passed in 61 packages.
- Local PostgreSQL leg: not run because `HIKYO_TEST_POSTGRES_DSN` was unset;
  trusted CI owns the mandatory SQLite + PostgreSQL conformance run.
- Standards and spec review: CLEAN in round 2/3 after target timing was fixed.
- Exact-head CI: pending.
