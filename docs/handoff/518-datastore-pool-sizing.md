# Issue #518 - datastore pool sizing

**State: implemented.** Hikyo no longer inherits unbounded SQLite reader
connections or pgx's host-dependent default pool size.

## Delivered behavior

- SQLite keeps the locked engine shape: one writer and four WAL readers.
- PostgreSQL defaults to 10 connections, as fixed by `ops-spec` §10.
- `HIKYO_PG_POOL_MAX` accepts a positive 32-bit integer for larger instances;
  zero, negatives, overflow, malformed values, and use with SQLite refuse boot.
- PostgreSQL's `pool_max_conns` DSN parameter remains an alternative. The
  dedicated environment variable wins when both are configured.
- Server and local-admin startup log the effective pool maximum after the
  datastore has opened and passed its boot checks.

## Decision note

The issue suggested CPU-derived examples (`GOMAXPROCS*4` for PostgreSQL and
`GOMAXPROCS*2` for SQLite). Those examples conflict with the locked operational
values in `docs/adr/ops-spec.md`: PostgreSQL 10 and SQLite 1+4. The locked
values win; PostgreSQL retains explicit instance configuration for larger
hardware.

## Verification

- Focused config/store/startup tests cover validation, typo recognition,
  SQLite caps, logged effective values, and the store-owned reporting boundary.
- PostgreSQL 18 conformance passed 73 tests covering the default, DSN
  parameter, explicit override precedence, and the complete behavior corpus.
- Race-instrumented PostgreSQL 18 isolation passed all 2,200 tests. The same
  package passed all 2,200 tests without race instrumentation and within its
  default timeout.
- `go build ./...`, `go vet ./...`, docs Astro check, and the two-axis
  standards/spec review passed. CI remains the source of truth for the
  repository-wide parallel release gates.
