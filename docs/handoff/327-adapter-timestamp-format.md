# Issue #327 — SQLite adapter timestamp format

Issue: https://github.com/Hikyo-Org/Hikyo/issues/327 (parent #326; audit
finding `F-S04-2` §1).

**State: implemented.** SQLite adapter repositories and the adapter runtime
now write one fixed-width, microsecond UTC timestamp representation wherever
timestamps are compared lexically.

## Contract

- `adapterTimestamp` owns adapter timestamp conversion for both dialects.
- SQLite uses `2006-01-02T15:04:05.000000Z`; PostgreSQL continues to pass a
  canonical `time.Time` to native `TIMESTAMPTZ` columns.
- Repository adapter jobs, runtime adapter jobs, and provider-lease comparisons
  use the same SQLite representation. Whole-second and sub-second timestamps
  therefore sort in wall-clock order.
- Existing wire and audit timestamp contracts remain unchanged.

## Migration

Migration `00034_adapter_timestamp_format.sql` normalizes SQLite
`adapter_targets.provider_lease_expires_at` and
`adapter_outbox.next_attempt_at`/`lease_expires_at` to six fractional digits.
The repository currently has no historical deployment data, but the one-shot
migration keeps upgrades deterministic. `CanonTime` already limits stored
precision to microseconds, so normalization only pads known RFC3339Nano forms
and loses no precision. PostgreSQL needs no migration.

Generated outputs: none.

## Validation

```text
TestAdapterStampsCompareLexicallyAcrossBridges                 passed
TestAdapterClaimDueReplaysExpiredLeaseOnly                     passed
TestAdapterEnqueueClearsExpiredProviderWriteFence              passed
TestSQLiteAdapterTimestampMigrationNormalizesLexicalColumns    passed
go test -count=1 ./internal/store                               47 passed
go test -count=1 ./internal/store/migrate                       10 passed
go test -count=1 ./...                                          3456 passed in 61 packages
git diff --cached --check                                       passed
```

Two-axis review found one standards issue (this handoff was missing), now
fixed. Specification review returned `CLEAN`.
