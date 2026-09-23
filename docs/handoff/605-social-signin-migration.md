# 605: Social sign-in migration and domain types

Ticket T1 of `docs/spec/social-signin.md` (section 2): the additive schema for
social sign-in and open registration, on both engines, with nothing
user-visible except the profile email becoming read-only. Issue #605.

## Migration numbers

The spec calls these 00042 and 00043; MCP had already taken both numbers.
This ticket is **00057_social_signin.sql** (sqlite and postgres). **00058 is
reserved for #607** (the registration switch: `intent` required on
`oidc_transactions`, live-transaction purge). Do not reuse 00058 elsewhere.

## What landed

- New tables, with their `hikyo:table` directives on both engines:
  `registration_policies`, `registration_policy_domains`,
  `registration_policy_entries`, `registration_policy_entry_values`,
  `registration_signups`, `oauth2_providers`, `oauth2_transactions`.
- Widened: `oidc_transactions` (purposes `establish`/`claim`, `intent`,
  `signup_scope_org_id`, `authority_id`, one exhaustive per-purpose CHECK;
  in-flight rows purged), `external_identities` (kind `oauth2`), `sessions`
  (`oauth2_provider_id`, three-way one-provider CHECK; every open reauth
  window closed, sessions and CLI handoffs survive), `credential_authorities`
  (`established_credential_kind` admits `oidc`/`oauth2`; recovery stays
  password-only), `grant_origins` (`registration`), `orgs` (`origin`,
  `registration_policy_id`), `accounts.email` (below).
- Go: `crypto.ArtifactSignup` (`su`, grammar and redaction pinned beside
  `hs`/`hc`), `domain.OriginRegistration`, `domain.CanonicalEmail` (spec 2.5),
  `domain.ProviderRef` with `ParseProviderRef`/`ResolveProviderRef` (spec 2.6),
  `domain.ProviderOAuth2` beside the existing `domain.ProviderKind` values,
  which are now the single source of `service.OIDCKind`/`SAMLKind`/`OAuth2Kind`.
- Registries: `oauth2_providers.client_secret` is in the instance reencrypt
  walk (queries, repo, store ops, dryness gate); new blob columns are
  classified in `reencrypt_coverage.go`; annotated-query pins and
  `internal/buildcompat/development.json` regenerated; the legacy upgrade
  drill fixture reverses 00057 on both engines.
- OIDC start refuses `environment_id` on any purpose other than `reauth` with a
  400 naming the field (the new CHECK would refuse the row). It depends on the
  body alone, so it is the one start refusal outside the uniform 401. The web
  client and CLI never send it on login or link.
- Release note: `docs/site/src/content/docs/docs/upgrades.mdx`, "Social
  sign-in schema upgrade".

## Owner decision: accounts.email repurposed, verified values only

00049 had added `accounts.email TEXT NOT NULL DEFAULT ''` as user-editable
contact metadata. The owner chose to **repurpose that column** as the login
email rather than add a new one, and then ruled that the legacy values must
neither take over nor block anyone: they were never verified, so anyone could
have typed someone else's address. The design:

- 00057 makes `email` nullable (no default, `''` refused by CHECK) and adds
  `accounts.email_verified_at` (nullable; `TIMESTAMPTZ` on postgres, `TEXT`
  on sqlite, like every other timestamp) with CHECK
  `email_verified_at IS NULL OR email IS NOT NULL`.
- The partial unique index `accounts_email` covers verified values only:
  `UNIQUE (email) WHERE email_verified_at IS NOT NULL`, over the canonical form
  (domain lowercased, local part preserved), so two verified holders of one
  address in different domain case are refused.
- Existing values: kept in canonical form, **unverified**
  (`email_verified_at = NULL`), when plain SQL can prove them a bare ASCII
  dot-atom addr-spec of at most 254 bytes; everything else becomes NULL.
  Duplicates among kept values stay (they are contact data and block nobody),
  so the former "null every duplicate-group member" step is gone. Migrations
  are SQL-only, so the count of cleared values is not reported. The sqlite
  GLOBs and postgres regex accept the same set, pinned by a both-engine parity
  test that also checks agreement with `domain.CanonicalEmail`.
- Semantics: **only a verified email is a login identifier, and only a
  verified email counts as an "existing address" for sign-up.** An unverified
  value is display-only contact data and never an authentication, linking or
  uniqueness key.
- The profile can no longer set email (owner decision: read-only).
  `service.ProfileUpdate` carries no email, `UpdateAccountProfileRequest` has
  no `email` field (sending one is a 400). The profile read reports
  `email_verified` (`AccountProfile` in Go, OpenAPI and the TS client); the web
  form shows a verified value as "Sign-in email, used to sign in" and an
  unverified one as "Contact email, from before sign-in email existed, not
  used to sign in". Privacy erasure writes NULL to both columns.

### Requirements for #608 (verified local sign-up)

- Sign-up and sign-in look addresses up with `email_verified_at IS NOT NULL`
  only; an unverified holder never refuses a sign-up and never matches a
  sign-in.
- Writing a verified address sets `email` (canonical form) and
  `email_verified_at` together.
- **In the same transaction that verifies an address, clear (`email = NULL`)
  every other account holding that address unverified**
  (`UPDATE accounts SET email = NULL WHERE email = $1 AND email_verified_at IS
  NULL AND id <> $2`). Not added in #605: with no caller it would be a dead
  proof-free writer needing a `ResolutionSurfaceWriters` entry, parameter
  aliases and an annotated-query pin; #608 adds it with its caller.

## Departures from the spec DDL

- **Postgres alters in place instead of rebuilding.** The spec's twin
  directives `sessions_rebuilt` and `credential_authorities_new` already exist
  (00020, 00006) and the directive lint refuses duplicates, so its DDL is not
  implementable as written; a `sessions` rebuild would also have to drop and
  restore the `reauth_windows` and `cli_reauth_handoffs` foreign keys. The
  ALTERs give the same constraints.
- **sqlite rebuilds with foreign keys off** (the 00025/00054 shape), or
  dropping `sessions` would cascade into live CLI handoffs.
- **Column order:** rebuilt sqlite tables keep their current column order and
  append the new columns, matching what the postgres ALTERs produce.
- **`sessions.enrolment_required` (00056) is carried forward**; the spec DDL
  predates it.
- **`jit_policy`** is gone since 00044; nothing references it.
- **Canonicalizer is stricter than the spec text:** it also refuses quoted
  local parts (net/mail unquotes them) and domain literals.
- **Extra CHECKs** `email IS NULL OR email <> ''` and
  `email_verified_at IS NULL OR email IS NOT NULL` on `accounts`, and the
  added `email_verified_at` column (the spec has none).
- **Restore epoch scan:** a restore bumps the credential epoch against the
  archive's own schema before rolling forward, so the two post-legacy epoch
  tables (`oauth2_transactions`, `registration_signups`) cannot join the frozen
  `MaxKnownCredentialEpoch`. `PostLegacyMaxCredentialEpoch` scans them when the
  restored catalog has both tables (tables only on both engines; a schema with
  one of them is refused). The coverage and forged-archive tests read both.
  A later epoch-stamped table joins the post-legacy query.
- The `registration_policies` instance singleton index uses `((org_id IS
  NULL))` on both engines.

## Deferred to later tickets

- Privacy erasure of `oauth2_transactions` rows: #609 (first writer).
- Privacy erasure of `registration_signups.email`: #608.
- Wire enums (`IdentityProviderKind`, OIDC purposes, grant origins) stay as
  they are, per the 2026-09-05 banner.

## Running the both-engine tests

Postgres legs need `HIKYO_TEST_POSTGRES_DSN` (they skip locally without it and
fail in CI). A throwaway server:

```sh
docker run -d --rm --name hikyo-pg -e POSTGRES_USER=hikyo -e POSTGRES_PASSWORD=hikyo \
  -e POSTGRES_DB=hikyo_test -p 127.0.0.1:55605:5432 postgres:18-alpine
export HIKYO_TEST_POSTGRES_DSN='postgres://hikyo:hikyo@127.0.0.1:55605/hikyo_test?sslmode=disable'
go test ./internal/store/migrate/ -run TestSocialSignin
go test ./internal/store/... ./internal/lint/... ./internal/domain/... ./internal/crypto/...
go test ./internal/app/...
go test -timeout 90m ./internal/isolation/...
docker rm -f hikyo-pg
```

After changing any migration, regenerate the development declaration against
an empty scratch database (see `docs/handoff/744-adapter-origin-conflicts.md`,
"Migration fixture note"), then `go tool sqlc generate` and the annotated-query
pins.
