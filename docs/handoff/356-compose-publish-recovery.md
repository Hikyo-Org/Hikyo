# Handoff: #356 phase-derived Compose publish recovery

Issue: https://github.com/Hikyo-Org/Hikyo/issues/356 (parent #326; audit
finding `F-S21-2`). Pull request: https://github.com/Hikyo-Org/Hikyo/pull/407.
Implementation base: `9a60a39055b4cb6f3b2c5460f70369d346acc130`.

## Contract

- `PublishResult.Stamps` is the sole candidate-stamp source; the duplicate
  `CandidateStamps` map is removed.
- `PublishResult.ActiveStamps` directly reports the stamp-file selection. A nil
  map means the selection could not be inspected after an uncertain switch
  failure; the contradictory `ActiveKnown` flag is removed.
- `NeedsCleanup()` derives entirely from `Phase != PublishPhaseComplete`.
  (Since #619 it lives in the package's test file: no production caller.)
  `CandidateActive()` remains phase-derived, and the redundant `GCComplete()`
  method is removed.
- Materialization, atomic stamp switching, post-commit recovery, and bounded GC
  retain their existing ordering and failure behavior. Production CLI consumers
  continue using only candidate stamps, materialization facts, and
  `CandidateActive()`.

Generated outputs: none. Database migrations: none. Wire/audit bytes: unchanged.

## Regression evidence

- The all-phase cleanup table was red before implementation because
  `PublishResult.NeedsCleanup()` did not exist; it now covers materializing,
  switching, collecting, and complete.
- Existing publish success, materialization-failure, stamp-switch-failure,
  post-commit-failure, GC-failure, partial-GC, and torn-generation tests use the
  canonical `Phase`, `Stamps`, and `ActiveStamps` facts.
- Focused `go test -count=1 ./internal/compose` passed 139 tests.
- Full `go test -count=1 ./...` passed 3,547 tests in 61 packages.

## Review

- Standards round 1 found redundant direct complete-phase checks beside
  `NeedsCleanup()`. Crash-seam tests now assert their exact phase separately
  from derived behavior; round 2 returned `CLEAN`.
- Spec round 1 returned `CLEAN`; round 2 returned `SOUND` after the test cleanup.
