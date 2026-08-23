# Issue #352 — adapter dialect ownership

Issue: https://github.com/Hikyo-Org/Hikyo/issues/352 (parent #326; audit
finding `F-S04-1`, after #327).

**State: implemented.** Adapter target configuration now receives one dialect-
owning `adoptDB`; callers can no longer pair a SQLite adapter with PostgreSQL
placeholders or omit PostgreSQL locking through an independent boolean.

## Contract

- `adoptDB` owns query selection, placeholder formatting, and timestamps.
- `targetManifest`, collision checks, target insertion, and target updates take
  no free dialect flag.
- SQLite uses `?` placeholders and fixed-width adapter timestamps. PostgreSQL
  uses numbered `$n` placeholders, native timestamps, and locking query forms.
- `AdapterRepo` and all wire, audit, ordering, and failure contracts are
  unchanged.

## Migration and generated outputs

No schema migration or generated output is needed.

## Validation

```text
TestAdoptDBOwnsDialectPlaceholders                             passed
TestAdapterUpdateTargetRoundTrip/sqlite                       passed
TestAdapterUpdateTargetRoundTrip/postgres                     passed or explicit local skip without HIKYO_TEST_POSTGRES_DSN
TestAdapterCreateAtomicallyBootstrapsCredentialAndFirstTarget passed
TestApplyTargetMutationClassifiesUpdateAndMove                passed
internal/store, internal/service, internal/isolation packages passed
```

The dialect-ownership regression is compile-time: before this change,
`adoptDB.Placeholders` did not exist. The dual-engine round-trip pins retained
public behavior while the contradictory internal state becomes unrepresentable.
