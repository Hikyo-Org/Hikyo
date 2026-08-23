# Handoff: #338 audit registry closed taxonomies

Issue: https://github.com/Hikyo-Org/Hikyo/issues/338 (parent #326; audit
finding `F-S16-1`). Integrated base:
`3b342e5a657f8a9d75cfe5d973a89eb23d28f0a2`.

## Contract

- Sixteen `KindString` audit fields that previously described closed
  taxonomies only in comments now declare their accepted values through
  `FieldSpec.Enum`. Unknown values therefore fail the emitting operation at
  the existing audit validation boundary.
- Current producer values remain valid. `settings.org_read.query` includes the
  emitted `count` variant, `auth.credential_authority_minted.issued_by`
  includes recovery issuance, authority delivery includes `stdout`, and
  `auth.oidc_refused.cause` includes `window-zero`, `no-possession`, and
  `downgrade`.
- Open vocabularies remain open: provider-qualified authentication methods and
  adapter authority principal IDs are unchanged.
- Event bytes, schema versions, ordering, trail selection, and dual-dialect
  behavior are unchanged. Database migrations: none. Generated outputs: none.

## Regression evidence

- The 16-case public `Validate` regression was red before registry closure:
  every field exposed an empty enum and accepted an out-of-taxonomy string.
- Each case now pins the exact accepted taxonomy, proves a current valid event
  still validates, and proves an unknown value is refused with the closed-set
  error.

## Validation

```text
rtk go test -count=1 ./internal/audit                     33 passed
rtk go test -count=1 ./internal/isolation -run '^(TestAuditCore|TestPostgresAuditExport|TestPostgresDurabilityBootRefusal|TestInvariantAudit|TestSCIM)'
                                                             85 passed
rtk go test -count=1 ./...                              3506 passed / 61 packages
rtk go vet ./...                                        passed
rtk gofmt -l <changed-go-files>                         clean
rtk git diff --check                                    clean
```

## Review

- Standards round 1 found a non-copyable wrapped validation command in this
  handoff; fixed. Round 2: `CLEAN`.
- Spec round 1 found the live `stdout` delivery producer was missing and the
  required prose-only enum guard was absent; both fixed. Round 2: `CLEAN`.
