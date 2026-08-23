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

## Validation

- Draft PR #387 previously passed build, test, race, fuzz, web, generated, and
  analysis jobs at `a4fcd1c`; only DCO failed because the bot commit lacked a
  sign-off.
- Current-main scoped and full local validation: pending lower memory pressure.
- Exact-head CI and review: pending.
