# #617 pre-freeze retirement and compatibility handoff

Implemented on `feat/one-zero-pre-freeze`, based on `e114b56e2423a0996fa266fafbb59b561712d35d`. Parent owns combined review, signing, PR, preview and merge on green. This handoff does not claim those gates passed.

## Result

- OIDC login refuses every unknown identity with `unknown-identity`; no account, principal, external identity or session is created. Existing linked identities, linking, reauthentication, provider change/delete races and SCIM login remain supported.
- New dual-engine migration `00044_retire_oidc_provisioning.sql` drops the old provider policy. Historical migration 00007 stays unchanged. Existing accounts, links, providers and assurance policy survive. The migration is roll-forward-only per ops-spec; restoring a pre-upgrade backup is the downgrade path. No Down section is provided.
- `org.rename` now requires `manage-members@org`, with instance inheritance. Bare `instance-config` does not suffice; `org.delete` still requires instance config. Registry, formula pin, OpenAPI description and web refusal text agree.
- Four existing wire enums are open: `IdentityProviderKind`, `OidcStartRequest.purpose`, `SamlStartRequest.purpose`, `GrantOrigin.kind`. No new values added. Generated server/runtime validator, real Go HTTP client and TS/Zod preserve unknown values. Account Security disables linking through unknown provider protocols while retaining their display.
- `EstablishCredentialRequest` has no issuer discriminator. Its description now lists the existing server metadata values including recovery; no field invented. Removed the dead invitation-claim error seam.

## Decisions

See [HTML report](../reports/1.0/pre-freeze.html) for questions, options, recommendations, evidence and revisit points. Historical locked prototypes, ADR rationale and migrations are preserved; active contract, editor and spec are amended. The old provisioning symbol spelling necessarily occurs in migration history and its explicit data-preservation regression fixture.

## #607 follow-up to publish with parent PR

Pre-freeze issue #617 removes the provider policy column before registration ships. Section 2.3 is a plain no-op: do not fold legacy policy rows, query/drop the retired column, remove the old audit event again, or repeat the organisation rename formula migration. Existing user accounts and linked identities survive. MCP already consumed planned migration numbers 00042/00043; allocate fresh versions at implementation. Social sign-in spec carries the same operative amendment.

## Validation

- Web typecheck and complete Vitest suite passed: 82 files, 666 tests, including the unknown-provider DOM fixture.
- Generated TS client regeneration and typecheck passed; all 17 tests passed, including one unknown-value fixture per newly open enum.
- `api/pre_freeze_test.go`: runtime schema acceptance and generated Go model round trip, one fixture per enum.
- `internal/cli/pre_freeze_test.go`: actual Go client's response decoder accepts each enum fixture.
- `internal/isolation/oidc_e2e_test.go`: unknown identity uniform refusal with unchanged account/identity/principal/session counts; prior automatically provisioned test setup replaced by invitation or explicit linking.
- `internal/isolation/pre_freeze_e2e_test.go`: scoped rename succeeds, bare instance config and cross-org rename refuse, org-admin delete refuses.
- `internal/store/migrate/pre_freeze_migration_test.go`: pre-retirement provider, principal, account, external identity and session rows retain every surviving column on SQLite/PostgreSQL. Both engines export the upgraded database and restore through native store backup paths; PostgreSQL restores into a fresh canonical schema to exercise positional COPY compatibility.
- Full affected Go suite uses `HIKYO_TEST_POSTGRES_DSN=postgres://hikyo:hikyo@127.0.0.1:5432/hikyo_pre_freeze_617?sslmode=disable GOMAXPROCS=2 go test -p 1 ./internal/store/migrate ./internal/isolation ./api ./internal/audit ./internal/authz ./internal/server ./internal/cli`. Dedicated scratch DB only. Full migration/API/audit/authz/server/CLI packages passed. The 478.6-second complete dual-engine isolation run reported only annotated-query pin drift. Reviewed and updated seven OIDC provider query hashes per engine; no names, annotations or scopes changed. Final all-invariants rerun passed in 10.870 seconds. The OIDC/rename rerun had no lifecycle failures; its only failure read the prior pin before refresh. Final service/store tests passed. Logs `/tmp/pre-freeze-full.log`, `/tmp/pre-freeze-web.log`.

## Native review round 1 fixes

All five findings addressed: removed Down migrations; stabilized closed `ScimAttention.state` constants through source `x-enum-varnames` (no generated hand edits); removed the empty provider-editor field; strengthened both-engine migration/backup preservation coverage; added recovery to the endpoint credential-authority description as well as its component. The existing MCP audit-origin rollback test now explicitly starts and reapplies schema 42, matching its targeted migration proof instead of crossing future roll-forward-only migrations.

Post-fix checks: full migration package with both engines passed in 4.982s (`/tmp/hikyo-617-r1-migrations.log`); full API and CLI packages passed (`/tmp/hikyo-617-r1-api-cli.log`); regenerated TS client typecheck and all 17 tests passed; web typecheck and Account Security DOM fixture (2 tests) passed (`/tmp/hikyo-617-r1-web.log`); `git diff --check` passed. Earlier full-suite evidence above remains applicable to unchanged lifecycle behavior. No unrelated ScimAttentionState constant changes remain in the generated diff.

## Parent verification

Review full diff against the base, run required combined checks, verify preview org rename and provider editor, post the #607 note, then commit with configured cryptographic signing and DCO. Verify every pushed commit through signature script and GitHub Verified. Merge only after exact-head green CI and clean review.

Native Codex R2 verified every R1 fix and returned CLEAN, no remaining blockers. Parent independently reran both-engine retirement migration/backup, scoped rename and invariants plus API/CLI focused tests. Browser provider-editor and rename preview is the final local gate before signed delivery.

Parent embedded preview passed on desktop (2/2) and mobile (2/2): configure/advertise/disable/delete provider and organization rename through the actual SPA and Go API. Logs /tmp/hikyo-617-browser-desktop.log and /tmp/hikyo-617-browser-mobile.log. CLI focused filter selected no tests; the agent full CLI suite and actual Go-client decoder fixtures provide CLI evidence, not that empty parent filter.
