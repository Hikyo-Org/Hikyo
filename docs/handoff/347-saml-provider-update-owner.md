# Handoff: #347 SAML provider update owner

Issue: https://github.com/Hikyo-Org/Hikyo/issues/347 (parent #326; audit finding
`F-S09-1`). Implementation base:
`b891007b0d8a6ec8f318af506172cc217640089e`.

## Contract

- `applyProviderUpdate` owns the complete provider compare-and-swap, persisted
  row reload, and security-sensitive session invalidation for PUT, PATCH, and
  metadata refresh mutations.
- `samlSessionsInvalidated` is the single directional predicate. Disabling a
  provider invalidates sessions; enabling one does not. Assurance policy,
  pinned signing certificates, SSO URL, and email NameID policy changes also
  invalidate sessions.
- Every successful create or update response is projected from the persisted
  row. Storage-owned row versions and timestamps can no longer diverge from
  the returned provider.
- Wire shapes, audit payloads, database schema, and generated artifacts are
  unchanged. Generated outputs: none.

## Coverage

- The predicate table covers disable/enable direction, assurance-policy and
  NameID policy changes, rotated/identical certificates, SSO URL changes, and
  display-only changes.
- The SQLite SAML flow proves display-only and identical-certificate updates
  preserve live sessions; disabling, policy changes, and certificate rotation
  sweep them. It also compares PATCH and refresh responses with persisted
  timestamps.
- PostgreSQL coverage uses the same isolation fixture in CI; local execution
  skips when `HIKYO_TEST_POSTGRES_DSN` is unset.

## Validation

```text
go test -count=1 ./internal/service                          251 passed
go test -count=1 -run 'SAML' ./internal/isolation             6 passed
go test -count=1 -run 'SAML' ./internal/server                4 passed
go test -count=1 ./...                          3,539 passed / 61 packages
go vet ./internal/service ./internal/isolation ./internal/server      passed
gofmt -w <changed Go files>                                   clean
git diff --check                                               clean
```
