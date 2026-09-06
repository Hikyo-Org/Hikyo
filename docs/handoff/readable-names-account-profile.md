# Readable names and account profiles

Request: use human-readable names in the web app and let people set or change
their email address and username.

## Delivered behavior

- Projects show names without a secondary raw ID. Members, audit actors,
  revision publishers, approval requesters and voters show current names.
  Audit and SCIM scope labels resolve the addressed organisation, project and
  environment through existing authorized metadata reads.
- Member and approval-policy selectors present names and submit immutable IDs.
  Duplicate names are disambiguated. Undisclosed principals and provider groups
  retain explicit advanced ID entry; no user directory was introduced.
- Account & security has an editable profile backed by GET/PATCH
  `/api/v1/me/profile`. Email is optional contact metadata, never an identity,
  login, recovery or linking key. The schema migration adds an empty default
  for existing accounts on SQLite and PostgreSQL.
- Actual username changes require existing password/TOTP proof, including a
  repeated comparison under the principal lock. Metadata-only edits require
  the authenticated self session and normal browser CSRF protection. Principal,
  grants and sessions remain unchanged. SCIM-owned names cannot be overwritten;
  synthetic usernames are hidden when managed or no local proof is available.
- Profile updates audit without names, email or proof in the payload. Subject
  export includes email; erasure clears it atomically. Proof uses component-owned
  sensitive state and never enters TanStack mutation variables or retries.

## Validation

- Web typecheck and complete Vitest suite passed: 896 tests in 104 files.
- Generated TypeScript client typecheck and 20 client tests passed.
- Go API, server, service, authz, lint, audit and buildcompat suites passed.
- Profile, membership, approval-policy names and privacy lifecycle isolation
  tests passed on SQLite and PostgreSQL 18. Privacy coverage verifies contact
  export, failed-erasure rollback and successful erasure.
- Development compatibility manifest regenerated and independently checked
  against fresh SQLite and PostgreSQL 18 schemas.
- Desktop and mobile account browser suites: 11 tests each passed, including profile save/reload,
  account-menu refresh, security flows and dark/light accessibility checks.
- Manual browser inspection at 1280px and 390px: profile uses existing design
  tokens, inputs have 44px height, and mobile document width stays 390px.
- Independent profile review: clean after fixing federation-only metadata edits
  and hiding synthetic handles. Sensitivity inventory hashes refreshed after
  reviewing account mutations and history's presentation-only change.

## Delivery state

Delivery authorized: signed commit, pull request, and merge after green checks.
Rebased onto main after #684, preserving its revision diff and adapter findings.
Account profile migration is 49; adapter findings remains migration 48.
Rebase validation: 905 web tests passed; generated clients rebuilt from the combined contract.
Disposable PostgreSQL test containers were removed after verification.
Current actor names are presentation labels, not immutable historical evidence;
audit event IDs and original attribution remain intact.
