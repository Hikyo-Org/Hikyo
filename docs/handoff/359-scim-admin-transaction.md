# Issue 359 — SCIM administration transaction owner

## Outcome

Binding-scoped SCIM administration now uses one `adminTx` owner for actor
resolution, authorization, optional per-binding serialization, binding load,
and audit insertion. `MintCredential` remains hand-rolled because its verified
reauthentication evidence must be consumed after caller resolution and before
authorization.

## Contract

- Mutations take the binding-row lock and emit the existing
  `wire-enter:<binding>` / `wire-exit:<binding>` observer pair.
- Reads share authorization, binding-load, and audit behavior without taking
  the reconciliation lock.
- Empty event sets are valid, preserving idempotent credential revocation
  without inventing an audit transition.
- No wire, audit, storage, migration, generated, or API contract changed.

## Validation

- `TestSCIMAdminMutationsMarkSerializedPhaseSQLite`: passed.
- Named phase, serialization, and credential set: 3 passed; PostgreSQL variants
  skipped locally because `HIKYO_TEST_POSTGRES_DSN` was unset.
- All `TestSCIM*` isolation tests: 66 passed.
- `internal/service`: 242 passed.
- Full repository suite, review, exact-head CI, and merge: pending.
