# Upgrade integration operator custody review

## Findings addressed

- R1 P1: recovery and restore committed using a separate PostgreSQL transaction after only checking the migration session's in-memory active flag. Backend termination could drop migration exclusion before commit.
- R1 P2: abandoned runtime-DB PostgreSQL restore entrypoints retained native transactions without destination capabilities.

## Final boundaries

- `upgrade.WithLock` retains the existing goose exclusive session lock. It also drains an independent PostgreSQL advisory barrier exclusively, then retains its shared side for the callback.
- Recovery and destination imports take a shared transaction-scoped barrier before domain access, then verify original physical owner connection and goose lock ownership. Another migration owner must acquire the barrier exclusively before its callback, including after the previous backend dies.
- Commit checks the original connection and lock again. If the owner dies after that check, the transaction's own shared barrier still prevents the next owner entering until commit/rollback completes. This closes the time-of-check/time-of-use gap without relying on a successful probe alone.
- Original `sql.Conn` is never replaced or reconnected. Context bounds PostgreSQL checks. SQLite keeps its existing host-lock custody.
- Removed `store.RestorePostgres`, `store.RestoreUpgradePostgres`, `tx.RestorePostgres`, `tx.RestoreUpgradePostgres`, and unused `store.PostgresIsEmpty`. The private importer requires a verified plan and both admission/owner checks.

## Validation

Sanitized runner: `/tmp/hikyo-runtime-fence-evidence/custody_review.py`.
Evidence: `/tmp/hikyo-runtime-fence-evidence/custody-review-race.jsonl` and `custody-review-race-summary.json`.

Actual isolated PostgreSQL under `GOMAXPROCS=2 go test -p 1 -race`: six passed cases, zero skips/failures. Upgrade package 2.256s; app package 11.436s. Covers next-owner drain after termination, real recovery rollback, real archive-import rollback, and nested goose lock ownership for both engines. Parent owns combined final validation. The final private importer nil-authority refusal and test error-path cleanup were added after this focused run and await parent validation.

## Decision

Question: Is a live-owner query immediately before commit sufficient? Options: query only; retain database-enforced transaction exclusion plus query. Choice: retained shared transaction barrier, because backend loss can occur between any query and COMMIT.

No commit or push from this worker.
