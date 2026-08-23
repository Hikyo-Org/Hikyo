# Issue #351 — isolate probe keyring registrations

Issue: https://github.com/Hikyo-Org/Hikyo/issues/351 (parent #326; audit
finding `F-S33-2`).

**State: implemented.** Isolation-test keyrings and retained roots now share one
registration lifecycle and fail closed when no registration exists.

## Contract

- One registry entry owns a datastore's probe keyring and retained root as an
  atomic pair; half-registration is no longer representable.
- `registerKeyring` is the only write path. It clones the retained root and
  evicts the complete entry through `testing.T.Cleanup` before datastore
  cleanup runs.
- `probeRootSource.Current` refuses an unregistered or already-cleaned-up
  datastore instead of returning a nil root with a nil error.
- Probe-created and Compose-server keyrings use the same registration path.
  Existing key material, rotation ordering, audit bytes, and dialect behavior
  are unchanged.
- Contract or migration decisions: none. Generated outputs: none.

## Coverage

- `TestProbeRootSourceRejectsUnregisteredDB` pins the fail-closed lookup.
- `TestProbeKeyringRegistrationEvictsOnCleanup` proves paired visibility and
  cleanup eviction.
- `TestProbeKeyringRegistrationRejectsIncompleteState` proves nil datastore,
  nil keyring, and invalid-length roots cannot form registrations.
- Existing audit-root-rotation, reencrypt, and Compose CLI SQLite tests pass.
  PostgreSQL variants remain covered by CI because no local
  `HIKYO_TEST_POSTGRES_DSN` is configured.

## Validation

- Initial regression tests: 3 passed in `internal/isolation` (including
  subtest); the strict-registration regression was added during review.
- Named reencrypt and Compose CLI SQLite set: 34 passed.
- `TestAuditCoreSQLite`: 15 passed.
- Pre-review `go test -count=1 ./...`: 3,545 passed in 61 packages.
- `gofmt` and `git diff --check`: passed.
