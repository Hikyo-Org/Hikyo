# #589: synthesis of social sign-in and open registration (handoff)

Issue: https://github.com/Hikyo-Org/Hikyo/issues/589 (wayfinder task, the destination of map #578). Authored by Claude Fable 5.1 in one autonomous session on 2026-09-03; review routing: Codex `gpt-5.6-sol` high, rounds capped at 3 (reviewed by Codex R1-R3; findings fixed before merge). Stacked on PR #603 (`proto/587-social-signin-surfaces`, the locked prototype), branch `synth/589-social-signin`.

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
