# Ticket 609: GitHub OAuth2

Entry point: [ticket 609](https://github.com/Hikyo-Org/Hikyo/issues/609).
Canonical contract: [social sign-in specification](../spec/social-signin.md), [owning OAuth2 decision 580](https://github.com/Hikyo-Org/Hikyo/issues/580) and [accepted resolution 589](https://github.com/Hikyo-Org/Hikyo/issues/589).

## Delivered behavior

- Independent `internal/oauth2rp` adapter pins GitHub's canonical origin and endpoints, the `user:email` scope, PKCE S256, JSON token negotiation and an explicit redirect URI. Numeric REST user IDs are the sole subject. Access tokens remain callback-local; refresh tokens and upstream two-factor metadata are discarded.
- Purpose-bound, single-use OAuth2 start/callback transactions support login, link, claim and establishment. Provider, binding, purpose and credential epoch are checked before credential writes. Claims lock the target principal; provider guards serialize same-issuer writes. OAuth2 disclosure reauthentication returns a local-authenticator/passkey remedy.
- Unknown identities register only through an admitted signup policy. Only unknown signup fetches primary verified email. No verified email emits `registration.signup_refused`; an email network failure emits `auth.oauth2_refused` with `userinfo-error` and the signup context. One admitted budget reservation survives the network leg.
- API, CLI and instance-admin WebUI support provider creation, read, list, reconfiguration and deletion. Secrets are sealed and write-only. Duplicate enabled origins return a conflict. Reconfiguration/disable/delete ends provider sessions. GitHub branding, profile and OAuth2 identity kinds reach login, account security and CLI reauthentication guidance.
- Both database engines carry short-lived establishment evidence attached to the resulting session. Migration 71 adds this carrier and updates development compatibility identity. The historical archive fixture refuses to discard nonempty establishment evidence.

## Ownership boundaries

Ticket 610 owns credential claim UI and its broader two-protocol experience. Ticket 611 owns local enrollment consumers of the establishment carrier, enrollment provider selection and native CLI step-up. This change does not claim those successor workflows are complete.

## Review

Ordinary Standards and Spec reviews completed three rounds. Epoch fencing, mixup classification, refusal context, claim serialization and signup reservation findings were corrected; final ordinary passes reported no new critical findings. The user explicitly skipped the ticket's pinned native cross-model review. That pass was not run and is not reported as clean.

## Preview

Local preview: <http://127.0.0.1:30609/instance#instance-oauth2>.
The shared T3 browser exercised desktop 1280x800 and mobile 390x844 login branding and provider administration. Mobile editing had no horizontal overflow; immutable slug and write-only blank secret were verified. Creating, editing, disabling/re-enabling and typed-confirmation deletion succeeded. Saving cleared the editor; disabling removed GitHub from auth methods, and re-enabling restored it. The preview uses a local fixture client, not a real GitHub OAuth application, so no live GitHub authorization is claimed.

## Validation

Initial full-suite failures included stale surface pins, API parity entries and shared proof-gate/registration expectations, which were corrected, plus PostgreSQL tmpfs OOM/recovery failures. Reverification uses disk-backed PostgreSQL 18. Broad fixture concurrency exhausted PostgreSQL connection and lock capacity; failed groups are rerun with two concurrent fixtures on an isolated server with increased lock capacity. An application fixture build and static AST checks also overlapped source edits; their exact failed tests passed after source stabilization.

No push, remote CI, PR, merge or deployment is part of this local implementation endpoint.

Final local evidence:

- Full Go suite attempted: `go test -timeout 90m ./internal/... ./api/... ./scripts/...`. Its failed packages were rerun on disk-backed PostgreSQL. The failed-package run took 29 minutes in the isolation package; all remaining failed groups passed after the two shared test expectation corrections and isolated, bounded reruns (`-parallel 2`). Source and fixture-resource failures are preserved above rather than calling the original run green.
- Both-engine final OAuth2 checks: `go test ./internal/isolation -run '^TestOAuth2' -count=1` passed in 12.164 seconds. This includes numeric identity, wrong PKCE, actual OIDC code/state mixup, intent/email sinks, exact refusal audits, epoch fences, reveal refusal, provider origin/profile/uniqueness/sealing/readback/CRUD and provider audit kind. `TestAuditCore` also passed on both engines with login/link/claim/establish emitter closure.
- WebUI: typecheck, all 150 unit files / 1,327 tests, lint, design checks and production build passed. Generated TypeScript client: typecheck and all 21 tests passed.
- Core Go adapter/server/API/authz/CLI tests passed. Go vet, import formatting and current-toolchain formatting passed. The API freeze guard is dormant because the freeze tag does not exist.
- All 130 generated Go/TypeScript files matched their recorded hashes after fresh pinned regeneration. Signature and DCO verification follow the normal signed commit.
