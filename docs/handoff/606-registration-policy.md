# 606: Registration policy (service, API, CLI, Members editor)

Ticket T2 of `docs/spec/social-signin.md` (section 13), issue #606, stacked on
#605 (migration 00057). The registration policy of #579 as amended, final
state: one policy per scope (0..1 per org, 0..1 at instance scope), a standing
delegation re-checked at read and use, write-time preconditions refused by
name, the `signup` budget category, the `registration.policy_*` events, the
public sign-up door on `/auth/methods`, and the Open registration panel and
editor on Members at org and instance scope. No sign-up leg is built here:
federated sign-up is #607, local sign-up and the mailer are #608.

No migration was added. 00057 carried every table this ticket needs; 00058
stays reserved for #607.

## What landed, per layer

**Store** (`internal/store/queries/{sqlite,postgres}/registration.sql`,
`internal/store/authn/registration.go`). The policy tables are class=authn, so
every statement is annotated `hikyo:authn-resolution` and rides the proof-free
Resolver, the provider-administration shape. Writers (all in
`lint.ResolutionSurfaceWriters`): `CreateRegistrationPolicy`,
`ReplaceRegistrationPolicy` (row CAS plus full child replacement),
`writeRegistrationChildren`, `DeleteRegistrationPolicy` (CAS),
`CreateRegistrationSignup`, `DeleteRegistrationSignup`. Reads:
`RegistrationPolicyFor(org)` ("" = instance) with domains, entries and claim
values, `CountRegistrationPolicyOrgs` (the live `n` of `n / cap`),
`RegistrationSignupsForPolicy`. The unique-index violation for a second policy
per scope folds onto `domain.ErrConflict` on both engines. The one cross-table
rule (values iff claim) is writer-enforced (`ErrRegistrationValuesWithoutClaim`)
and pinned on both engines in the isolation suite. `LocalEnabled` joined the
sqlite INTEGER / postgres BOOLEAN list in `internal/lint/sqlcontract.go`.

**Authz** (`internal/authz/registration.go`, `registry.go`, `classify.go`).
Six operations, the member.invite shape: `registration-policy.get-org`,
`.put-org`, `.delete-org` (tenant, level org, `manage-members@org`) and
`.get-instance`, `.put-instance`, `.delete-instance` (instance,
`manage-members@instance`). Six routes classified; wire counts 343 / 240.
`RegistrationAuthorityHolds` is a pure, non-auditing evaluation of a recorded
authority principal's current grants (see decisions). The self-configuration
org's closed operation set was deliberately not widened: registration into
the protected self-config org stays refused (uniform 404).

**Audit** (`internal/audit/registry.go`). `registration.policy_created`,
`policy_updated`, `policy_deleted` (security; tenant or instance trail by
scope; payload `policy_id`, `scope`, `landing`, `authority_principal_id`,
`previous_authority_principal_id?`) and `registration.signup_expired`
(security; `signup_id`, `policy_id`, `cause: expired | policy-deleted`).
`runRegistrationLifecycle` in the audit e2e gives each a real emitter.

**Budget** (`internal/service/budget.go`). `BudgetSignupPerHour = 20`,
`budgetSignup` (instance-wide, rate-only), and the charge seam
`(*Budget).chargeSignup()`. Pinned in `internal/conformance/boundregistry_test.go`
and unit-tested (`TestBudgetSignupInstanceWide`). The six policy operations are
classified exempt under the section 10 authenticated-API budget, beside
`member.invite-*`; the `signup` category has no authz-operation row because
the legs that charge it are pre-auth ceremonies (#607, #608).

**Config** (`internal/config`). `Config.ExternalOriginExplicit`: true when the
operator supplied `HIKYO_EXTERNAL_ORIGIN` (flag, env, or managed value), false
when it fell back to the listen address. `ManagedOwnerValues` now exports the
origin only when explicit, and `ApplyManagedOwnerValues` keeps the node's own
derivation when neither side states one, so a round trip preserves
explicitness. Managed snapshots saved before #606 hold the listen-derived
origin as a value; a managed origin equal to this node's own derivation is
therefore NOT treated as explicit (an operator who means exactly that origin
states it in `HIKYO_EXTERNAL_ORIGIN` on the process). Pinned by
`TestManagedLegacyDerivedOriginIsNotExplicit`.

**Service** (`internal/service/registration.go`). `NewRegistration(
RegistrationConfig{DB, Auth, Now, PublicOriginExplicit, MailConfigured})`
refuses a missing datastore or mailer predicate at construction (the app
refuses to boot). `Get`, `Put` (full replacement, caller becomes authority),
`Delete` and `SignupDoor` take a `RegistrationScope`
(`InstanceRegistrationScope()` / `OrgRegistrationScope(org)`; the zero value
is refused, so "no org" never silently means the instance). Landing kinds,
inactive causes, precondition names and sign-up method kinds are typed
constants. `providerPrecondition` is the one provider-eligibility rule shared
by the write (`resolveEntryProvider`) and every read (`evaluatePolicy`).
`evaluatePolicy` is the read/use re-check. Refusals are
`registrationRefusal` (wraps `domain.ErrInvalid`, `SafeDetail` names the item),
so the 400 body's `detail` is e.g. `provider-disabled: oidc:okta`.

**API** (`api/openapi.yaml`, `internal/server/registration.go`).
`GET|PUT|DELETE /api/v1/orgs/{org}/registration-policy` and
`/api/v1/instance/registration-policy`; `GET` answers 404 when the scope has
no policy (closed). `AuthMethods` gains `signup_open`, `signup_paused`,
`signup_methods` and the `?org=` query. Pins: `api/noproxy_test.go` (+6),
`api/parity.yaml` (`webui: members` / `instance-members`),
`internal/isolation/testdata/operation_formulas.json` (+6),
`annotated_queries.json`, the contract uniformity table (+3 org routes), and
two contract tests (the named 400 on the wire; `?org=` reaching the door).
TS client and `@hikyo/operations` regenerated.

**CLI** (`internal/cli/registration.go`). `hikyo access registration
show|set|delete [--org O | --instance-scope]`; `set --file policy.json` is a
full replacement parsed strictly (unknown members refused, a `proof` in the
file refused); the proof is prompted, never a flag. Help golden and the
auth-artifact rule table updated.

**Web.** `web/src/api/registration.ts` (query, plain async writes, copy and
precondition wording), `web/src/routes/OpenRegistration.tsx` (panel, editor,
the blue `Confirm it's you` proof step), mounted on `Members.tsx` at org and
instance scope (not on the project projection), the "Sign-up is paused." line
on `Login.tsx`, `.btn--reauth` / `.registration*` styles, prototype mock
support, sensitive-inventory re-pin with a review note.

## Decisions and where they are owned

- **Read events.** The audit-model banner lists no `registration.policy_read`,
  and neither read may be audited-none (the formula is `manage-members`, not
  `read`). The reads record `grant.membership_read` (`scope`, `row_count` 0 or
  1): the panel is part of the Members surface that event already covers. Not
  a new event type, so no banner amendment.
- **Authority re-check per landing** (#579 d4, #585 d1): org-template ->
  `template.apply-org` at the org (`manage-members@org`); fresh-org ->
  `org.create` (the admin template on the new org follows by inheritance);
  none -> the policy's own `manage-members@instance` (it grants nothing, so
  the delegation is the policy formula). The check at read and on the public
  door is non-auditing: the principal comes from the row, not the request.
  #607/#608 record their own `authority-lost` refusal at use.
- **Write-time authority for fresh-org.** The editor must hold `org.create`
  (instance-config + manage-members) to write a fresh-org policy; otherwise
  the PUT is refused 403 by `Authorize(org.create)`. A manage-members-only
  instance editor cannot delegate what it does not hold (pinned on both
  engines in `runRegistrationPolicy`).
- **Re-save as authority** is offered for the two authority causes only (the
  prototype's rule): re-saving cannot cure a failing precondition.
- **Cause precedence** at read: `authority-unassigned` (NULL authority), then
  `authority-lost`, then `precondition` (with `inactive_precondition` naming
  it: `no-public-origin`, `mailer-unconfigured`, `provider-disabled: <ref>`,
  `provider-missing-email-scope: <ref>`, `provider-missing: <ref>` for a
  deleted provider row, named by the row id it pointed at, since nothing
  auto-deletes entries (#579 d6), and `provider-kind-unsupported: <ref>`).
- **A disabled authority is a lost authority.** The re-check resolves the
  authority's grants through the same query every authorization uses, which
  returns nothing for a restricted or erased principal; pinned on both
  engines (restricting the authority flips the policy to `authority-lost`).
  The instance path reuses `principalInstanceHolds`, the grant half of
  `HoldsInstanceCapability`.
- **Reauth** (#579 d8): the existing account-security proof primitive
  (`Auth.VerifyReauthProof` before the transaction, `ConsumeReauthEvidence`
  inside it), the `SCIM.MintCredential` shape. The proof rides the request
  body (`proof`, required on PUT and DELETE), DELETE included (the
  `unlinkIdentity` precedent), and the operations are marked
  `x-hikyo-reauth: account-security` (below). No session
  is purged or reissued; the isolation test pins the session count and that
  the acting session still resolves. A spent TOTP step cannot authorize a
  second mutation. A network actor with no `Auth` wired fails closed.
- **Provider references** are `{kind, slug}` on the wire (spec 2.6), stored
  as `(provider_kind, provider_id)`. SAML is not a sign-up kind; `oauth2` is
  refused at write by name (`provider-kind-unsupported: oauth2:<slug>`) until
  #609 adds its table lookup (single seam: `resolveEntryProvider` and the
  kind branch in `evaluatePolicy`).
- **Use-time refusal events land in #607.** The public door is the only use
  site in #606, and it renders `signup_paused` without auditing; the
  `registration.signup_refused {authority-lost | precondition | ...}` event is
  emitted by the first sign-up leg (#607 federated, #608 local).
- **The `none` landing re-checks `manage-members@instance`** (#579 d4: every
  edit and use re-checks the authority's standing grants; a `none` landing
  grants nothing, so the delegation it rests on is the policy's own
  formula).
- **Pending sign-ups on policy delete** (spec section 4): deleted in the same
  transaction, one `registration.signup_expired {cause: policy-deleted}` each,
  on the policy's trail. Orgs the policy minted keep `origin` and the pointer.

## Mailer predicate: wired, not stubbed

`internal/mail` and managed mail configuration already exist, so the static
predicate of mailer-seam 7.3 (clauses 1 and 2) is derivable:
`service.SelfConfigMailConfigured(selfConfig)` captures the active runtime
bundle and reads `bundle.MailConfigured()` (the prepared, never-dialed mail
client). Only the known fenced states read "unconfigured": `Capture` now
returns `ErrSelfConfigFenced` (which wraps `ErrSelfConfigUnavailable`, so
existing callers are unchanged) for a suspended, restoring, other-topology or
unreconciled configuration. Every other capture failure (a failed metadata
read, a runtime that never loaded) propagates: `Put` answers 5xx and the
public door read fails loudly. The predicate is evaluated inside each write
or read transaction, beside the other preconditions. Clause 3 (explicit
public origin) is the separate `no-public-origin` precondition. The seam is
the one `RegistrationConfig.MailConfigured func(ctx) (bool, error)`; a nil
predicate is a construction error. #608 needs no replacement, only the
use-time call.

## Departures from the ticket and spec text, and why

- **No JIT fold, no re-save path for folded rows**: retired by #617
  (migration 00044), per the spec's 2026-09-05 banner. "Re-save as
  authority" is offered for `authority-lost` and `authority-unassigned` only.
- **Spelling additions for a #589 amendment line** (api-cli-spellings
  section 8 is silent on them): `AuthMethods.signup_paused` (the login page
  must tell paused from closed, and `signup_open=false, signup_methods=[]`
  cannot); `RegistrationPolicy.inactive_precondition` (the panel's remedy
  line names the precondition); `RegistrationPolicy.row_version`, `org`,
  `created_at`, `updated_at`; entry `display_name` on responses;
  `RegistrationPolicyPutRequest.proof` and `RegistrationPolicyDeleteRequest.proof`
  (the reauth proof, required). `signup_methods` matches the spelling
  `[{kind, slug} | "local"]` exactly: a `oneOf` of `ProviderRef` and the
  string `local` (`LocalSignupMethod`).
- **`x-hikyo-reauth: account-security`** (spec 3.1) is on the four policy
  mutations and read by `api/spec.go`: the reader refuses a marked operation
  whose required JSON body lacks a string `proof`, and refuses any class but
  `account-security`. The isolation suite drives every marked operation's
  service without proof and asserts the refusal, and asserts every
  proof-gated registration call is marked, so the extension and the gate
  cannot drift. Older reauth-gated operations (SCIM credential mint, recovery
  codes, passkeys) are not yet marked; marking them is a separate sweep.
- **Operation names** carry `-org` / `-instance` (the `member.invite-*` shape);
  spec 3.2 names the verbs only.
- **Event payloads** add `scope` beside the spec's fields.
- **Integers**: `cap`, `fresh_org_count` and `row_version` are plain JSON
  integers (no `int64` format) so the web client keeps numbers, not bigints.
- **Playwright drives real state only.** The write-time 400 names a
  provider really seeded without the email scope (`e2e-reg-noemail` on the
  second fake IdP); authority-lost is a second organisation administrator
  (invited with `admin`, own authenticator) who writes the policy and then
  loses `manage-members`; the paused login page is an instance policy whose
  provider (`e2e-reg-paused`) is then disabled. Every seeded provider and
  policy is deleted afterwards.
- **Shared proof prompt.** `web/src/ui/auth/ProofDialog.tsx` is the one
  "Confirm it's you" prompt; Account & security's account-security prompts
  render it too. The reauth-gated form is a password field
  (`current-password`), never `one-time-code`.
- **E2E fixture**: the instances now start with an explicit
  `HIKYO_EXTERNAL_ORIGIN` (the same value the listener derives), and the
  fixture OIDC provider requests `openid email`, so a policy can name it.

## Spec contradictions found

None. The gaps above are additions, not disagreements with a resolution.

## For #607 and #608

- Use-time check: call `evaluatePolicy` (same package) inside the sign-up
  transaction; when inactive, refuse uniformly and audit
  `registration.signup_refused` by cause. Record the authority re-check with
  the auditing path if a trail entry for it is wanted (the read path is
  deliberately quiet).
- Charge `budgetSignup` through `(*Budget).chargeSignup()` after the
  admission gate and before any write; overflow is the uniform 429.
- The pending-row writer is `CreateRegistrationSignup`; the reaper emits
  `registration.signup_expired {cause: expired}`. Privacy erasure of
  `registration_signups.email` is #608's.
- Deleting an org cascades its policy (FK) but not its pending sign-ups
  (`registration_signups.policy_id` has no FK by design): the #608 reaper
  prunes them at expiry, or the org delete should clear them; decide there.
- The public door is rendered by `SignupDoor`; #607 renders the staged
  "Create an account" door from `signup_methods` and the `/signup?org=<id>`
  route the Members panel already links to.
- #609: add the `oauth2` provider lookup at the two seams named above.
- #613 (A7 closure): bound-registry fixture rows beyond the pinned
  `BudgetSignupPerHour`, the status ledger entry, and operator docs.

## How to test

```sh
docker run -d --rm --name hikyo-pg -e POSTGRES_USER=hikyo -e POSTGRES_PASSWORD=hikyo \
  -e POSTGRES_DB=hikyo_test -p 127.0.0.1:55606:5432 postgres:18-alpine
export HIKYO_TEST_POSTGRES_DSN='postgres://hikyo:hikyo@127.0.0.1:55606/hikyo_test?sslmode=disable'
go test ./internal/isolation/ -run 'TestRegistrationPolicy|TestAudit'
go test -timeout 90m ./internal/... ./api/...
docker rm -f hikyo-pg

go tool sqlc generate && go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml
(cd clients/ts && pnpm install --frozen-lockfile && pnpm run generate)
git status --porcelain internal/store/sqlitegen internal/store/pggen api/apigen clients/ts/src/generated

cd web && node --run typecheck && node --run lint && node --run test && node --run design:check
pnpm run build
# The default ports (28789..28794) suffice when nothing else is listening;
# otherwise move all five: HIKYO_E2E_PORT, _PORT_B, _PORT_TLS,
# _PORT_OPERATIONAL, _PORT_OPERATIONAL_B.
HIKYO_E2E_PORT=45910 pnpm exec playwright test --project=desktop \
  e2e/flows/members.spec.ts e2e/flows/instance-admin.spec.ts e2e/flows/login.spec.ts
HIKYO_E2E_PORT=45910 pnpm exec playwright test --project=mobile \
  e2e/flows/members.spec.ts e2e/flows/instance-admin.spec.ts e2e/flows/login.spec.ts
```
