# 760 — Enforce the second factor at sign-in

Server-side enforcement of the sign-in flow PR #755 built in Storybook: a
password login on an account with an enrolled factor presents the factor
before a session mints and cannot skip it; an unenrolled account under a
`required` instance policy is gated into enrolment before anything else is
reachable.

Spec with rationale: `docs/handoff/storybook-ui-consistency.md` §3. ADR:
`docs/adr/human-auth.md` (§ Assurance, § Account-security mutations, § Lockout
and the local floor). Issue #760. SPA wiring (item 6) is explicitly OUT — it
lands with the migration series.

## Design (locked)

**Login outcome by case** (`service.Auth.LocalLogin`, browser artifact):

| Account | Instance policy | Outcome |
|---|---|---|
| factor enrolled (totp or passkey) | any | **202 challenge**, no session, no cookie |
| no factor | `optional` | 200 session (unchanged local floor) |
| no factor | `required` | 200 session flagged `enrolment_required` |

A factor that stands is **never skippable** — the challenge path does not
consult the policy. Policy only decides the unenrolled case. CLI artifact
logins are unchanged (the flow is browser-only; a CLI login of an enrolled
account keeps today's behaviour — see Open question O1).

**Challenge**: a new `login_challenges` row — random id + SHA-256 verifier,
single-use, expiring, bound to `account_id` + `credential_epoch`, carrying the
offered `factors`. Distinct from the WebAuthn ceremony store (purpose `login`
there already means passkey discoverable login — collision). No cookie is set
until a finish op consumes it.

**Finish ops** consume the challenge and mint the session with the factor
recorded in the assurance (`["password","totp"]` or `["password","webauthn"]`):
- `POST /auth/login/challenge/{id}/totp {code}` — verify TOTP (same helper,
  same 401 wrong / 409 replayed-step / 429 as `stepUpTotp`).
- `POST /auth/login/challenge/{id}/webauthn/start` — resolve challenge →
  account, open a WebAuthn step-up ceremony for it.
- `POST /auth/login/challenge/{id}/webauthn/finish` — verify assertion, consume
  the challenge, mint.

**`enrolment_required`**: new boolean column on `sessions`. Stamped in
`createCompletedSession` when an unenrolled account mints under `required`.
Projected into `authz.Identity`. The authorization chokepoint refuses every
operation for such a session **except** the operationId allowlist
{`enrolTotpStart`, `enrolTotpConfirm`, `enrolPasskeyStart`, `enrolPasskeyFinish`,
`regenerateRecoveryCodes`, `whoami`, `logout`}. Deny-by-default at
`internal/authz/artifact_admission.go` (`AdmitOperation`), mirroring the
`runtimeRecoveryOperation` allowlist pattern in
`internal/server/self_config_fence.go:63`. `whoami` exposes the flag so the SPA
(later) renders the gate.

The flag **restricts** a password-assured session; it does not widen it. The
enrolment endpoints already require exactly the authority a just-presented
password gives (ADR § Account-security mutations rule 1: "the password where
[the account] does not yet [have a possession factor]"). "A new credential may
never authorize its own enrolment" is **not** weakened.

**Instance policy** `HIKYO_SECOND_FACTOR ∈ {required, optional}` via
`internal/runtimeconfig` catalogue + `internal/config`:
- fresh install default `required` (greenfield = strict).
- upgrade default `optional` (no posture change on upgrade), via a
  `runtimeconfig.Migration{Default: &optional}` + bumped `MigrationVersion`
  (#780 pattern; existing operator values always win).
- surfaced on the authenticated `CredentialPolicy` (contract) for the operator.
- `service.Auth` gains a policy input. The isolation harness builds
  `service.Auth` directly and defaults it **`optional`** so the ~21 e2e files
  that do unenrolled password logins keep passing unchanged; the three new
  gate tests set `required` explicitly.

## Contract additions (all freeze-additive)

Permitted oasdiff classes: `endpoint-added`, `api-operation-id-added`,
`response-success-status-added`, `response-*-property-added`,
`new-optional-request-property`. Verified against `api/freeze.go`
`PermittedChanges`.

- `localLogin`: **new 202** → `LoginChallenge`. 200/`LoginResult` untouched.
- `LoginChallenge` schema: `{challenge_id: ID, expires_at: Timestamp, factors:
  [FactorClass]}`, `additionalProperties: false`.
- `WhoAmI`: **new optional** `enrolment_required: boolean`.
- (No `CredentialPolicy` change — see O3. The policy lives in `runtimeconfig`.)
- 3 endpoints, each `x-hikyo-class: unauthenticated`,
  `x-hikyo-artifacts: [human-session]`, `x-hikyo-min-revision: 1`,
  `security: []`. operationIds `loginChallengeTotp`,
  `loginChallengeWebauthnStart`, `loginChallengeWebauthnFinish`. TOTP body
  reuses `TotpCodeRequest`; webauthn reuses `WebauthnOptions` / `WebauthnResponse`.

Each new endpoint is registered in: `api/parity.yaml`, `api/noproxy_test.go`,
`internal/server/self_config_fence.go` runtime-recovery switch (login-path ops),
`internal/server/contract_test.go` `stubAuth`, `internal/server/api.go`,
`internal/server/browser.go` response visitor, and the regenerated
`api/apigen/apigen.gen.go` + `clients/ts/src/generated`.

## Phases

1. **Contract** — edit `openapi.yaml`; `go test ./api/...` (freeze/profile/parity);
   `cd clients/ts && pnpm run generate`; `git diff --exit-code clients/ts/src/generated`
   fresh; regen apigen; `go build ./...`.
2. **Migrations** — `login_challenges` table + `sessions.enrolment_required`
   column + webauthn purpose CHECK gains `login-2fa`, all in **both**
   `postgres/` and `sqlite/`; sqlc queries + `pggen`/`sqlitegen` regen.
3. **Config/policy** — `HIKYO_SECOND_FACTOR` catalogue + config field + fresh
   default `required` + upgrade migration default `optional`.
4. **Service** — login-challenge store; `LocalLogin` challenge branch; finish
   ops; `enrolment_required` stamping; policy input on `Auth`.
5. **Authz guard** — `AdmitOperation` enrolment_required allowlist.
6. **Server** — handlers + visitors + registry wiring; `whoami` exposes the flag.
7. **Tests** — isolation e2e `PasswordThenAuthenticator`,
   `UnenrolledIsGatedIntoSetup`, `UnenrolledAllowedByPolicy`; store/service units.
8. **Docs** — ADR amendment; story mock alignment; this handoff.
9. Same-provider adversarial review → CLEAN; CI green.

## Open questions / decisions

- **O1 (artifact scope).** The **202 challenge** (enrolled account) is
  **browser-only**, per spec item 1 (`artifact: browser`). A CLI login of an
  enrolled account keeps today's model: it mints a `[password]`-only session,
  and the authorization chokepoint (`AdequateAssurance`,
  `internal/authz/session.go:111`) already refuses every MFA-mandatory
  capability for it until the caller steps up (`stepUpTotp` /
  `stepUpPasskeyFinish`). The factor is **deferred, not skipped** — the
  security is enforced at the chokepoint, not the login page. The
  **`enrolment_required` gate** (unenrolled + `required`) applies to **both**
  artifacts (spec item 3 names no artifact): a CLI bootstrap admin on a fresh
  `required` instance is likewise gated into enrolment.
- **O2 (upgrade default optional).** #780 introduces per-setting release
  defaults; `second_factor` upgrades to `optional` so an existing instance's
  posture is unchanged until an operator opts in. Fresh installs are `required`.
- **O3 (CredentialPolicy not touched).** `HIKYO_SECOND_FACTOR` is owned by
  `runtimeconfig` (the #780 default/upgrade machinery). Putting it on the
  DB-persisted `CredentialPolicy` (settable via `setCredentialPolicy`) would
  create two owners of one setting. No contract surface is added for the policy
  now — `whoami.enrolment_required` is all the deferred SPA gate needs.
- **O4 (the flag is recomputed live, never inherited).** The gate is stamped in
  `createCompletedSession` from `computeEnrolmentRequired` (live account +
  policy state) on **every** password mint and reissue, so it can never go
  stale: enrolling a factor reissues through `reissueSession` →
  `createCompletedSession`, which recomputes the flag as `false` because a
  factor now stands at the live epoch. `RotateSessionFactors` (the step-up/reauth
  rotation path) additionally clears the column to `false` unconditionally; that
  is safe because no gated session can reach a rotation op (step-up and reauth
  are outside the enrolment allowlist), but it is a blind clear, not a live
  recompute — a future rotation caller reachable by a gated session would need to
  recompute instead. Enrolment only counts a factor at the LIVE credential epoch
  (§ O7), so a restore-superseded factor never lifts the gate.
- **O7 (epoch-bound factor standing).** "A factor stands" means a confirmed TOTP
  or an enabled passkey **at the live credential epoch**. A restore bumps the
  epoch and renders prior-epoch MFA seeds inert (ADR § Restore); `enrolledFactors`
  and `LoginChallengeTOTP` both filter by the live epoch, so a superseded factor
  neither suppresses the enrolment gate nor is presentable against a challenge.
- **O8 (challenge starts are budgeted).** `LoginChallengeWebauthnStart` enters
  the per-IP + instance-wide admission budget (like `PasskeyLoginStart`) before
  opening a ceremony, so a password holder cannot flood `webauthn_ceremonies`.
  Per-IP, not per-account, so a start cannot lock a victim out of their login.
- **O5 (`stepUpFailureText` is a phantom).** The spec names a symbol that does
  not exist. The real refusals to mirror are: bare 401 (`domain.ErrUnauthenticated`)
  for a wrong code, 409 via `totpCodeAlreadyUsedDetail`
  (`internal/service/factors.go:79`) for a replayed time step, 429 via
  `admission.ErrOverloaded` for the factor budget.
- **O6 (webauthn purpose).** The login-2fa webauthn sub-flow uses a new ceremony
  purpose `login-2fa` (purpose `login` already means passkey discoverable
  login — collision). New migration widens the CHECK in both dialects.

## Review status

Cross-provider (Codex) review **skipped — quota out** (user-directed). Same-
provider adversarial review performed; findings looped to CLEAN. Not labelled
CLEAN where a pass was skipped.
