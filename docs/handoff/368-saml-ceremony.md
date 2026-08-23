# Handoff: #368 SAML ceremony tuple

Issue: https://github.com/Hikyo-Org/Hikyo/issues/368 (parent #326; audit finding
`F-S09-2`). Implementation base:
`428dd6a5e347479a7a3697e2953ce10b7543db58`.

## Contract

- `samlCeremony` owns the provider, transaction, and optional validated claims
  used to build every SAML login and reauthentication audit event.
- A zero-value ceremony preserves pre-lookup refusal behavior: login event,
  null transaction object id, and empty provider, entity, purpose, and
  transaction payload fields.
- Post-lookup refusal, success, and actor-attributed paths derive event type,
  object, and payload from the same tuple. Callers cannot reorder adjacent
  provider, entity, purpose, transaction, and claims arguments.
- `samlAuditCause` owns the closed refusal vocabulary through named constants;
  conversion to a payload string occurs only at the audit boundary.
- `stage` is the single transaction-local audit writer. `refuse` stages the
  failure and returns the existing unauthenticated result; `refuseCommitted`
  preserves the standalone invalid-RelayState commit boundary.
- Wire and audit bytes, refusal precedence, transaction ordering, and failure
  results are unchanged. Database migrations, API changes, and generated
  outputs: none.

## Coverage

- `TestSAMLCeremonyAuditDetailsPreserveRefusalContext` table-tests all 66
  refusal causes with exact event type, audit object, and payload across
  pre-lookup, post-lookup/pre-claims, and post-validation/claims phases.
- `TestSAMLAuditPayloadSurfacesExpiredPinnedCertificate` continues to pin the
  optional warning in validated claims.
- `TestSAMLAllPurposesCarryCrossSiteInitiatorBinding` continues to pin the
  cross-site initiator verifier for login, link, and reauthentication.
- Existing SAML service, server, and SQLite/PostgreSQL isolation coverage keeps
  exercising malformed login and reauthentication refusal events through the
  committed audit path. PostgreSQL legs skip locally without
  `HIKYO_TEST_POSTGRES_DSN` and run in CI.

## Regression evidence

Before the tuple existed, the new table test failed to compile at the two
`samlCeremony` construction sites. After implementation, both table rows and
the named SAML regressions pass.

## Validation

```text
go test -count=1 ./internal/service -run 'SAML'                105 passed
go test -count=1 -run 'SAML' ./internal/isolation                6 passed
go test -count=1 -run 'SAML' ./internal/server                   4 passed
go vet ./internal/service ./internal/isolation ./internal/server
                                                                  passed
go test -count=1 ./...                         3,660 passed / 61 packages
gofmt -w internal/service/saml.go internal/service/saml_test.go clean
git diff --check                                                 clean
```

## Review

- Standards R1 found raw refusal-cause strings could drift persisted audit
  vocabulary. Named `samlAuditCause` constants fixed every producer; R2 CLEAN.
- Spec R1 found the refusal-context table covered only two causes. Coverage now
  pins all 66 causes across all three context phases; R2 CLEAN.
