# Cross-engine query contract lint

## Purpose

Prevent SQLite and PostgreSQL sqlc queries with the same name from silently
drifting into different caller contracts.

## Implementation

- `internal/lint/sqlcontract.go` compares sqlc commands, Hikyo security
  annotations, parameter identities and types, and result field identities,
  order, and dialect-normalized types. Nullable text remains distinct from
  required text; timestamp wrappers cannot expose nullability consistently
  across the two generated drivers.
- Contracts come from sqlc's generated Go APIs. This covers `SELECT *`, CTEs,
  aliases, and dialect-specific projections without reparsing result SQL.
- Generated driver-call arguments are combined with dialect-aware placeholder
  ordinals to compare bind order and reuse. Literal and comment masking handles
  PostgreSQL dollar quotes, escaped strings, nested comments, and JSON `?`
  operators.
- Table-model results compare named fields independent of declaration order.
  Query-specific `Row` results retain projection order because sqlc scans them
  positionally.
- Deliberate audit differences are pinned to exact hashes for both SQL
  statements and both generated APIs. PostgreSQL owns `recorded_at` through its
  deferred appender and returns `commit_seq`; SQLite binds `recorded_at` and
  canonicalizes `seq` as the commit cursor.
- sqlc naming differences are bridged only by query-specific reviewed aliases;
  unrelated identifiers cannot collapse into a generic `ID` or time value.
- SQLite `ClampCredentialExpiry` now names and reuses one `ceiling` parameter,
  matching PostgreSQL instead of requiring callers to bind it twice.

## Verification

- `go tool sqlc generate`
- `go build ./...`
- `go vet ./...`
- `go test -count=1 ./...` (`4515` tests passed across `72` packages)
