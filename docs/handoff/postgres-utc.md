# PostgreSQL UTC read normalization

## Scope and cause

The frozen generated-client harness rejected PostgreSQL `whoami` session timestamps with offsets. The store already promises UTC. Pinned pgx v5.10.0 binary timestamptz scanning defaults to Go `time.Local`; text scanning follows the server session offset. Changing server TimeZone alone does not address the binary protocol.

`internal/store/store.go` now registers pgx's UTC timestamptz codec in `AfterConnect`, plus the matching array codec element reference, for every physical connection. Instants and microsecond precision are unchanged. No handler-specific changes or relaxed frozen validators.

## Regression

`internal/store/timestamps_postgres_test.go` launches an isolated non-UTC process, uses real PostgreSQL with a different non-UTC session timezone, and holds two pool connections. Eight cases cover binary and text protocols, winter/summer and date-crossing instants. Assertions cover direct time values, generated query wrapper types, nullable values, arrays, exact instant equality and canonical UTC JSON.

The regression failed before the production change and passed after it. Uses the existing store scratch-database helper; CI without its required PostgreSQL DSN fails explicitly. Local dedicated base database: `hikyo_utc_store`; individual test databases are uniquely named and removed by the fixture.

## Validation

- Targeted real PostgreSQL regression passed (2.004 seconds).
- `GOMAXPROCS=2 go test -p 1 ./internal/store/...` passed with the dedicated PostgreSQL DSN: store 7.985 seconds, migrate 3.251 seconds, tx 1.078 seconds.
- `GOMAXPROCS=2 go test -race -p 1 ./internal/store -run ^TestPostgresTimestampsAreUTC$ -count=3` passed with the dedicated PostgreSQL DSN (7.671 seconds).

## Delivery

Worktree `/tmp/hikyo-postgres-utc`, branch `codex/1.0-postgres-utc`, base `944d23e5`. Parent received `/tmp/hikyo-postgres-utc-code.patch` for immediate frozen-client integration testing. No commit or push by this agent. Report: `docs/reports/1.0/postgres-utc.html`.
