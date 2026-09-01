# Issue #515: CORS allowlist fast path

Issue: https://github.com/Hikyo-Org/Hikyo/issues/515. Base:
`a84fd620cedb068988b4d996a5610d12638172b1`.

## Contract

- `PublicOptions.ExternalOrigin` carries the config-validated
  `HIKYO_EXTERNAL_ORIGIN` into the public router. Same-origin comparison never
  trusts the request `Host` header.
- `crossOriginAllowed` canonicalizes the request `Origin`. An origin matching
  the instance origin bypasses `Workspace.OriginAllowed`, emits no CORS grant,
  and retains `Vary: Origin`.
- `Workspace.PrimeOriginAllowlist` loads a negative-only snapshot before the
  public listener serves. An empty allowlist refuses every foreign origin
  without a request-path transaction.
- A non-empty snapshot still performs the live membership read. Runtime read
  errors therefore remain fail closed, and existing cross-origin grant/header
  behavior remains unchanged.
- `AddOrigin` and `RemoveOrigin` update the snapshot under the same lock as the
  write transaction, so the empty/non-empty fast path changes atomically with
  successful local writes.

## Tests

- `TestCORSSameOriginMutationSkipsAllowlistConsult` drives the real public
  router and proves a canonical same-origin POST never reaches the workspace
  allowlist service.
- `TestCORSRequestsIssueNoAllowlistQueriesAfterSnapshot` uses a counting SQLite
  driver through the isolation harness. Same-origin mutation and hostile
  cross-origin traffic against an empty allowlist both execute zero datastore
  queries.
- The existing remote lifecycle primes the snapshot before adding and removing
  origins, covering cache coherence on SQLite and PostgreSQL conformance legs.

## Verification

- `go build ./...`: clean.
- `go vet ./...`: clean.
- `go test -count=1 ./...`: 4,039 passed across 69 packages.
- `go test ./internal/server ./internal/app -count=1`: 314 passed.
- `go test ./internal/conformance -count=1`: 78 passed (SQLite; PostgreSQL runs
  in CI with `HIKYO_TEST_POSTGRES_DSN`).
- `go test ./internal/isolation -run 'TestCORSRequestsIssueNoAllowlistQueriesAfterSnapshot|TestRemoteLifecycleSQLite' -count=1`:
  4 passed.
- Two-axis review: Standards CLEAN; Spec CLEAN after three rounds.
