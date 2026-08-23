# Issue #373 — adapter query ownership

Issue: https://github.com/Hikyo-Org/Hikyo/issues/373 (parent #326; audit
finding `F-S04-2`, after #352).

**State: implemented.** Thirteen hand-twinned SQLite/PostgreSQL repository
methods now delegate to one dialect-aware `adapterQueries` owner. Adapter
runtime reads and transactions use the same SQL and timestamp contract, and
the three copies of the active-ledger orphan query are one function.

## Contract

- `adapterQueries` owns the thirteen repository operations named by #373.
- `adapterDBTX` owns query selection and timestamp conversion for every
  transactional adapter-runtime operation.
- Runtime read paths receive one `adapterDB`; no query-site engine branch remains.
- SQLite integer and PostgreSQL boolean ledger values are still scanned by
  their concrete driver representation before conversion to `bool`.
- PostgreSQL locking and `SKIP LOCKED` clauses, SQLite timestamp bytes, public
  `AdapterRepo`, ordering, authorization, audit, and failure behavior are
  unchanged.

## Migration and generated outputs

No schema migration or generated output is needed.

## Validation

```text
TestAdapterTransactionOwnsDialectSelection passed (red-before-green compile seam)
internal/store adapter tests              30 passed
internal/store + internal/service         317 passed
go test -count=1 ./...                     3605 passed in 61 packages
```

## Review disposition

Two-axis review is clean. Standards review found the shared DB interface still
carried its adoption-only name; it is now `adapterDB`. Spec review found unused
transaction placeholder methods and duplicate auth-token ownership; both were
removed. Final standards and issue-spec rechecks returned `CLEAN` with zero
findings.
