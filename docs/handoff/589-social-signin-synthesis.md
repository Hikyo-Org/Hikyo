# #589: synthesis of social sign-in and open registration (handoff)

## Current acceptance record (2026-10-16)

The synthesis deliverables landed in [PR #614](https://github.com/Hikyo-Org/Hikyo/pull/614),
merge `fc165413e5923f9cadaada75f6ed910342b86cf4`. This record reconciles
those planning deliverables with the final decisions and implemented source.
The original September report below is historical, including its proposed
1.0 promotion, then-unbuilt features, model routing and review residuals.
The [owner's September 3 ruling](https://github.com/Hikyo-Org/Hikyo/issues/589#issuecomment-5531905677)
selected post-1.0, first. This reconciliation completes the requested
editorial move in mvp-boundary: amendment 4 records the disposition, A7 leaves
the 1.0 inventory for section 4.3, closed admission is uncharged, and the
obsolete JIT-fold fixture follows #617's already-operative supersession. No release or production deployment is claimed here.

### Synthesis acceptance matrix

| Required deliverable | Accepted decision and current evidence | State |
| --- | --- | --- |
| Human-auth amendment | `docs/adr/human-auth.md`, September 3 clauses (a)-(m): registration policy, OAuth2 identity kind, invitation claim, local account email, linking and credential establishment. Decisions #579, #580, #582, #584, #586, #588, #596, #598 and #604 were read through their dated corrections. | Present on main |
| Tenant-isolation amendment | `docs/adr/tenant-isolation.md`, September 3 banner names registration sign-up as the third resolution member, preserving one identity query and the engine-microtiming residual. #585/#604 define its intent and write order. | Present on main |
| Audit-model amendment | `docs/adr/audit-model.md`, September 3 banner enumerates policy/sign-up/verification/claim/OAuth2 changes and explicitly admits mail intent/outcome. The historical body is not the operative limit for this added member. | Present on main |
| Permission-model amendment | `docs/adr/permission-model.md`, September 3 banner carries standing delegation, authority principal, registration grant origin, rename authority and live fresh-org cap. | Present on main |
| Other named ADRs | September 3 banners in threat-model, ops-spec and api-cli-surface. This change completes the already-approved mvp-boundary amendment 4/A7 editorial relocation, retaining the CLI and mailer preconditions and all other A7 criteria. Synchronous mail requires no system-architecture outbox amendment. | A7 reconciliation requires this merge; #612 review procedure remains below |
| Both-engine data model | `docs/spec/social-signin.md` section 2 plus actual SQLite/PostgreSQL migrations `00057_social_signin.sql`, `00058_registration_switch.sql`, and `00075_cli_login.sql`. #617 retired JIT in 00044, so no legacy policy fold remains to implement. | Delivered |
| API/CLI contract and ceremonies | Spec sections 3-5 and `docs/spec/api-cli-spellings.md` section 8; `api/openapi.yaml`, `api/parity.yaml`, the shared auth implementation and source regression coverage. September 23's accepted derived `AuthMethodProvider.brand` addition is reconciled in section 3.1. | Delivered; spec omission corrected by this reconciliation |
| UI references | Spec section 6 and `docs/spec/ui-spec.md` reference locked prototype iteration 2 from #587/#603. The final #613 browser closure covers registered desktop/mobile claims. | Delivered |
| Mailer configuration and pitfalls | Spec sections 7-9, `docs/spec/ops-catalogue.md`, provider/mailer research and the managed-mail amendment. TLS, no boot dial, admission-before-charge and uniform refusal remain explicit. | Delivered |
| Sequenced implementation tickets and routing | Spec section 13 preserves the original T1-T9 dependencies and historical model labels. All #605-#613 are closed; [#615 closeout](https://github.com/Hikyo-Org/Hikyo/issues/615#issuecomment-6216613758) separately verifies implementation. Historical labels do not authorize unavailable reviewers. | Delivered |

### Resolved password conflict

The [October 15 owner ruling](https://github.com/Hikyo-Org/Hikyo/issues/589#issuecomment-6199131864)
preserves password-first for #611: confirmed TOTP can set a first password,
while changing an existing password requires that password. No new human-auth
amendment is required. PR [#925](https://github.com/Hikyo-Org/Hikyo/pull/925)
merged as `648a5a52b7abb403b69186bc496ce367bf117582` on October 16.

`internal/service/password.go` calls the password-first selector in
`internal/service/reauth.go`. `TestCredentialEstablishTOTPAndExpiry` in
`internal/isolation/credential_establish_e2e_test.go` covers OIDC and OAuth2 on
both engines: first-password TOTP succeeds; a valid fresh TOTP cannot replace
it; the same rejected code succeeds at the TOTP-first recovery operation;
current-password replacement succeeds and revokes the prior session. These
are inspected regression assertions, not a new local test run by this task.

### Separate RFC 8628 amendment obligation

The [approved October 15 ruling](https://github.com/Hikyo-Org/Hikyo/issues/589#issuecomment-6197746788)
requires five seconds added for every `slow_down`, with no 30-second cap;
connection-timeout backoff is separate. PR
[#924](https://github.com/Hikyo-Org/Hikyo/pull/924) merged as
`ad4c7b124addae47d79bc715f70d51ba326cf271` on October 15. The dated ops-spec
banner preserves and supersedes the historical capped-doubling row.
`internal/service/cli_login.go` persists the increment, and
`internal/cli/login_transport.go` applies it to subsequent polling.
`TestCLILoginDevice` exercises intervals 10 through 40 seconds on both engines.

**Still open:** #32 owns completion of the locked amendment procedure in
`docs/adr/oss-mechanics.md`, Governance. The #612 handoff explicitly records
cross-provider review as unavailable, not clean. General merge authorization
and exceptions for other issues do not satisfy this gate. Record #32's
qualifying review or explicitly scoped owner disposition and verified merged
closeout of the #612 amendment before closing #589. The shared #32 issue may
remain open for unrelated amendments; only its #612 obligation blocks #589. This reconciliation does not change ops-spec or
claim the review was performed.

### Verification boundary

Live host inspection confirmed PRs #614, #924, #925 and #928 merged and all
four merge commits are ancestors of the assessed main
`c4c626a105d0139fcc8a6f5444e585e7a4e0a761`.
[Full candidate run 38881592174](https://github.com/Hikyo-Org/Hikyo/actions/runs/38881592174)
succeeded on final #928 head `4151c291577ed3491a97a80f4329574362915e87`.
[Full main run 38887776523](https://github.com/Hikyo-Org/Hikyo/actions/runs/38887776523)
succeeded with all 48 jobs on merged #928 commit
`4af540bf377cc1e2a34163d4de150d395bbea5a9`, including all three isolation
shards, eight desktop/mobile browser jobs, flow closure, core/conformance and
the required floor. The application trees under `internal/service`,
`internal/isolation`, `internal/cli`, `internal/authz`, `api` and `web` are
unchanged between that tested main and the assessed main.
[Docs deployment 38890803193](https://github.com/Hikyo-Org/Hikyo/actions/runs/38890803193)
also succeeded on the merged #928 commit. The
[#613 closeout](https://github.com/Hikyo-Org/Hikyo/issues/613#issuecomment-6215549123)
records the detailed acceptance and retained failed attempts.

This assessment is source and live-host inspection. It ran no new application
tests, builds or browser sessions. Existing fixture evidence does not prove
production Google/Entra/GitHub authorization, a real external SMTP inbox,
physical passkey hardware, a release or production deployment. #589 closure
requires the remaining #32 procedure, review of this documentation change including the approved A7 correction,
required CI and a verified merge of the reconciliation. A globally closed #32
is not required when other amendments still belong to that shared issue.

## Historical synthesis report (2026-09-03)

Issue: https://github.com/Hikyo-Org/Hikyo/issues/589 (wayfinder task, the destination of map #578). Authored by Claude Fable 5.1 in one autonomous session on 2026-09-03; review routing: Codex `gpt-5.6-sol` high, rounds capped at 3 (reviewed by Codex R1-R3; findings fixed or dispositioned before merge, two left for human disposition per section 14.13). Stacked on PR #603 (`proto/587-social-signin-surfaces`, the locked prototype), branch `synth/589-social-signin`.

## What this PR is

Planning output only. No Go, TypeScript, SQL or OpenAPI changed. It lands:

- **Amendment banners** (dated 2026-09-03, per the oss-mechanics procedure) in `docs/adr/human-auth.md` (clauses a to m), `tenant-isolation.md` (third resolution member), `audit-model.md` (`registration.*`, `auth.oauth2_*`, field widenings, JIT removal), `permission-model.md` (registration standing delegation, `registration` origin, org rename and delete formulas, single-factor first admin, fresh-org cap), `threat-model.md` (SMTP relay outbound class, fragment reading of no-secrets-in-URLs), `ops-spec.md` (`signup` budget, token and window values, mailer config, provider runbooks, inventory row 21, the reset-lifetime discrepancy), `mvp-boundary.md` (declared amendment 4, criterion A7), `api-cli-surface.md` (exception-class additions, verb joins).
- **Handoff spec** `docs/spec/social-signin.md`: decision index, DDL for both engines, wire deltas and pins, ceremony order, audit deltas, UI references, mailer config, ops values, pitfalls, fog dispositions, residuals, preconditions, nine sequenced implementation tickets with model labels.
- **Spec-layer updates**: `docs/spec/README.md` (document map row), `ui-spec.md` (sixth locked family), `ops-catalogue.md` (values), `api-cli-spellings.md` (§ 8), `domain-model.md` (vocabulary, origins, entity rows).

## Decisions the synthesis itself made (for human disposition)

1. **1.0 membership.** mvp-boundary leaned on "locked no-self-registration" twice; the map amends that rule, so the contradiction had to be dispositioned. Recorded as a **promotion into 1.0** (declared amendment 4, criterion A7, phase 8), because 1.0 has not shipped (#79 open). The alternative, a §4.3 out-row with trigger "post-1.0, first", is an editorial swap; the banner says so.
2. **Fog: SCIM interplay** discharged from the locked SCIM text (attach on existing key; deprovision releases only SCIM origins). **Fog: self-service for self-served accounts** ruled out of scope on the map with a reopen path. Both reversible.
3. **Issuer-mismatch cause name**: an ID-token issuer mismatch now audits under the callback's default token-validation cause `signature` (#588 d3 left the choice); no new cause invented.
4. **Mailer variable names** fixed in ops-catalogue (`HIKYO_MAIL_ADDR|TLS|CA_FILE|USER|PASSWORD_FILE|PASSWORD|FROM|EHLO|ALLOWED_CIDRS`); #584 delegated the exact names here.
5. **Wire spellings** in api-cli-spellings § 8 (registration policy under `access registration`, OAuth2 providers under `instance-config oauth2-provider`, `account password set|change`, `org rename`, `instance-config mail test`).
6. **Round-1 Codex fixes that reshape ticket text** (spec section 14): sign-up scope carried on the start and the request; admission gate before the `signup` charge (reorders #604 d3); consumed sign-up tokens read `unknown` (#584 d7 narrowed); recovery-issued authorities never claim (#582 d4 narrowed); durable `registration.mail_intent` / `mail_outcome` around every SMTP dial; email canonical form; `{kind, slug}` provider references; grant-origin subject = authority principal; migrations split into additive 00042 and switch 00043; JIT fold covers disabled providers; `mail-test` budget 5/h per principal, 1 concurrent. Each is posted as a dated amendment line on the owning ticket.
7. **Model labels** inferred from the label set and prior handoffs (no written routing doc exists): auth core to `model:fable-5`, feature and closure work to `model:opus-4.8`.

## Residuals (recorded in the spec § 11, not fixed)

Possession-first proof discrepancy; ops-spec reset-lifetime row (1 h) versus code (24 h); `EstablishCredentialRequest` issuer list omits `recovery`; Google non-authoritative email caveat; Entra redirect-URI cap; engine microtiming.

## How to pick this up

Read `docs/spec/social-signin.md` § 13 and the nine `ready-for-agent` tickets are #605 to #613 (T1 to T9). Each implementation ticket links its owning resolutions; never restate a decision, link it. Every ADR change after this point reopens the owning ticket per oss-mechanics.

## Verification performed

- Every ticket resolution on the map read in full through its dated amendment lines (final state bound, not first state).
- Every quoted ADR clause and every DDL/registry/contract fact extracted from the files at HEAD (`docs/adr/*`, `internal/store/migrations/*`, `internal/audit/registry.go`, `api/openapi.yaml`, `api/parity.yaml`, `internal/authz/registry.go`, `internal/config/config.go`).
- No em-dash in new text (AGENTS.md rule); existing corpus text untouched.
- Codex cross-model review (`gpt-5.6-sol`, high, cap 3): R1 OBJECTIONS (24 findings, 5 blocking) all fixed or dispositioned; R2 verified 21, left 3; R3 after fixes: exact migration SQL closed one, two remain for human disposition (spec section 14.13): the audit-model body's intent-outcome licence is widened by banner only (corpus rule: banner wins), and the policy entry values-iff-claim rule is writer-enforced with a conformance pin rather than a database guard.
