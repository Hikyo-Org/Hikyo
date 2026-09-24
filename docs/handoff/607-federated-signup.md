# 607: Federated sign-up on the OIDC kind

Ticket T3 of `docs/spec/social-signin.md` (section 13), issue #607, stacked on
#606 (registration policy) and #605 (migration 00057). An unknown federated
identity becomes an account only when its `login` transaction was started
with intent `sign-up` and the addressed scope's registration policy admits it;
everything else about OIDC login is unchanged.

## What landed

**Migration 00058** (`00058_registration_switch.sql`, both engines; the spec's
00043). In-flight `oidc_transactions` are deleted (minutes-lived, and a login
row from the previous binary carries a NULL intent), and the login arm of the
exhaustive purpose CHECK gains `intent IS NOT NULL`. sqlite rebuilds the table
in its 00057 column order; postgres replaces `oidc_transactions_purpose_shape`
in place. The drill fixture's post-legacy list and `buildcompat` pin include
it. Asserted at the end of `TestSocialSigninMigration{SQLite,Postgres}`.

**Start** (`OIDCStart`, `POST /auth/oidc/{provider}/start`). `intent`
(`sign-in | sign-up`, absent stored as `sign-in`) and `signup_org` ride the
request and the transaction. Intent on any purpose but `login`, an unknown
intent, and a `signup_org` without `sign-up` refuse in the `ErrBadPurpose`
shape (uniform 401) before any row. The start reads no policy. A `reauth` on a
provider row with a NULL assurance policy is now a named 409 whose detail is
the remedy ("enrol WebAuthn or TOTP"), still before discovery and before any
row (#588 d4); the web reauth ceremony renders that remedy.

**Callback** (`completeLogin` in `internal/service/oidc_flow.go`, the leg in
`internal/service/registration_signup.go`). Order, per spec section 4 as
reordered by 14.2: provider revalidation, epoch, the intent-blind
`(oidc, issuer, subject)` resolution; a known identity signs in under either
intent, uncharged; an unknown identity under `sign-in` refuses
`auth.oidc_refused {cause: unknown-identity, intent}` with no registration
event and no charge; under `sign-up`: the admission gate (policy present,
active by #606's `evaluatePolicy` re-check, this provider row an entry;
refusals `closed | authority-lost | precondition | predicate`, uncharged), the
`signup` budget through #606's `chargeSignup` (once per callback across
transaction retries; overflow is the shared 429 with a `budget` refusal
committed), the verified-email assertion from the signed token's raw claims
(`email` a non-empty string beside `email_verified` or `xms_edov` as JSON
`true`; any recognised claim present and not `true` refuses; refusal
`no-verified-email`, `verified_by: none`), the one-claim allowlist
(`predicate`), the fresh-org cap counted live (`cap`), then the write.

The write runs under the policy's authority principal as the caller for the
landing (`authz.Identity{Principal: authority}`, the `audit.go` shape): the new
principal and account (`username = oidc-<account id>`, display name from a
valid `name` claim, `accounts.email` NULL), the identity row, then for
`fresh-org` the org (`org-<org id>`, `origin = registration`,
`registration_policy_id`) under `Authorize(org.create)` and the admin template
under `Authorize(template)` at the new org; for `org-template` the policy's
template under `Authorize(template.apply-org)`; `none` grants nothing. Template
grants carry grant origin `registration` with the authority as subject. Events:
`registration.signup_admitted` (unauthenticated actor, address and
`verified_by`), `settings.org_created {origin: registration, policy_id}`
(actor = authority), `registration.signup_completed {account_id, landing,
org_id?}` (actor = new principal), then `auth.oidc_login {intent}`. A UNIQUE
race on the identity insert rolls the whole attempt back (no account, no org)
and commits `identity-exists` in a fresh transaction.

**Wire and registries.** `OidcStartRequest.intent|signup_org`; `oidcStart`
409; `Org.origin` (required) and `registration_policy_id`; `GET /orgs?origin=`
(+400); `AuthMethodProvider.brand` (`google | microsoft`, derived from the
pinned issuer, presentation only); `AuthMethods.signup_landing` (open door's
landing kind). Audit registry: `registration.signup_admitted | signup_refused
| signup_completed` (instance trail, security), `intent`/`signup_org` on
`auth.oidc_login|oidc_refused`, `origin`/`policy_id` on `settings.org_created`
(operator creates now record `origin: manual`); the callback wire entry
declares the new events; `runRegistrationLifecycle` emits all of them.
`OriginRegistration` joined the human release gate (`mintableOrigins`), so an
administrator's revoke releases it like a manual origin.

**oidcrp.** The Hikyo issuer belt and `ErrIssuer` are deleted: go-oidc's
unconditional check (with its Google-only bare `accounts.google.com`
tolerance) is the ID-token issuer check, and `Claims.Issuer` is the pinned
issuer. `Claims.Raw` carries every signed claim. Discovery surfaces
`IssuerMismatchError{Discovered}`; `Providers.Put` answers a 400 whose detail
names the document's issuer (the Entra `common` / domain-name remedy).
`PairwiseSubjectsOnly()` reads `subject_types_supported`; `Providers.Put`
refuses a `client_id` change (`ErrPairwiseClientID`, 400 naming `client_id`)
when the document is pairwise-only and identities are linked under the issuer.
`CreateExternalIdentity` folds the identity-key UNIQUE violation onto
`domain.ErrConflict` on both engines (SCIM's race path relied on this already).

**Web.** The atoms are #800's (the `[UI]` slice, handoff
`607-staged-login-entry.md`): `ui/auth/LoginForm` is the staged entry of
prototype iteration 2 and `ui/auth/ProviderButton` carries the brand rules.
This half feeds them the wire: `routes/Login` passes each provider's `brand`
(`google` / `microsoft` from `GET /auth/methods`; `github` stays story-side
until #609), `signup` from `signup_open` + `signup_methods` (admitted OIDC
providers matched by `{kind, slug}`) with the landing worded from
`signup_landing`, `initialIntent` for `/signup`, and sends `intent` (plus
`signup_org`) on the OIDC start. A sign-up start is always OIDC, even when a
SAML row shares the slug. The brand rules (Google's standard G with
"Continue with Google" / "Sign up with Google"; the Microsoft logo with "Sign
in with Microsoft · <display name>" on every surface, one row per Entra tenant
row, "Microsoft: work or school account" beneath; generic "Continue with
<display name>"). The door appears only while the addressed scope is open;
each admitted provider is followed by the confirmation step (#586 d6 copy and
the landing line) whose button alone sends `sign-up`. New public surface
`signup` (`/signup`, `/signup?org=<id>`), registered in the flow registry
under `login.spec.ts`. The instance org list shows an origin badge
(`manual` / `self-serve`) and a filter using `?origin=`.

## Tests

- Both engines (`internal/isolation/federated_signup_e2e_test.go`):
  `TestFederatedSignup` (start refusals and stored intent; closed and
  unknown-org doors; sign-in misses with no registration event; 20
  unadmitted-provider refusals leaving the whole budget; Google- and
  Entra-shaped admits; eight verified-email fixtures; allowlist after
  verified-email; known identities uncharged under every intent; exact budget
  exhaustion; the identity race), `TestFederatedSignupLandings` (org template
  with origin registration and its revoke; authority lost; fresh org under
  the authority with the widened `settings.org_created`; single-factor founder
  refused manage-members until an adequate session; cap and the operator
  delete freeing the slot; precondition), `TestFederatedSignupOneQueryPath`
  (the third member's fixture pin: known and fresh legs share the ordered
  query trace up to the resolution), `TestOIDCProviderIssuerDepartures`,
  `TestOrgRenameFormula`.
- `internal/oidcrp/issuer_test.go` (bare Google `iss` accepted only for the
  pinned Google issuer; mismatch names the document issuer; pairwise),
  `internal/service/registration_signup_test.go`,
  `internal/server/signup_contract_test.go`.
- Web: `Login.oidc.test.tsx` (staged entry, door, confirmation, `?org=`,
  brand rules), `values.oidc.test.ts`, stories; Playwright `login.spec.ts`
  gains the sign-up door round-trip on real server state (see below).

## Departures and decisions

- **WriteSerialized for every sign-up-intent callback.** The spec puts only
  the fresh-org branch under `WriteSerialized`; the landing is known only after
  the policy read inside the transaction, so the callback chooses by the
  recorded intent. Sign-in callbacks keep `tx.Write`.
- **Sign-up outcome events land on the instance trail** (the pre-auth plane
  `auth.*` refusals share) with `scope` and `policy_id` in the payload. A
  tenant copy of `signup_completed` was tried and dropped: a tenant-trail
  write needs a proof whose operation declares the event, and no registry
  operation is "a sign-up into this org" (the template proof may not emit it;
  a new operation would be a registry-shape change). The org's own trail
  still shows who joined: the template grant lines (`grant.created`,
  `grant.template_applied`, `origin_kind: registration`, the new principal).
- **`authority-unassigned` refuses as `authority-lost`** (the closed cause
  enum has no unassigned member; nothing writes such a row since #617).
- **Brand and landing are server-derived additions to `/auth/methods`**
  (`brand`, `signup_landing`): the prototype needs both and the public list
  carried neither. Brand keys on the issuer for presentation only.
- **The local entry is not rendered on the door yet**: its request endpoint is
  #608's. `signup_methods: "local"` is ignored by the page until then.
- **`auth.oidc_refused` cause `identity-exists`** is left to #610 (its only
  OIDC emitter is claim); a federated sign-up race records
  `registration.signup_refused {identity-exists}`.
- **"Reaches manage-members only after a local factor"**: the founder's
  single-factor session is refused and an adequately assured session is
  admitted; enrolling the local factor on a social-only account is #611's
  `establish` purpose. **Deferred assertion for #611, verbatim:** the first
  administrator of a fresh org, signed up through a policy-less OIDC
  provider, is refused `manage-members` at that org, enrols a local
  possession factor through the `establish` purpose, and is then admitted to
  `manage-members` at the same org in a session carrying that factor.
- **`mintableOrigins` belt.** `registration` joined the human release gate so
  an administrator's revoke releases it like a manual origin
  (permission-model 2026-09-03 (b)). The grant API still takes no origin
  parameter, so nothing outside the sign-up transaction can mint one; the
  set is also `AddGrantOrigin`'s write gate, which is why the sign-up needs it
  there. `TestSCIMOriginKindsAreNotHumanReleasable` pins it on the human side
  and off the SCIM side.
- **Charge per attempt.** The `signup` charge carries its refund; a sign-up
  attempt that rolls back (a serialization retry, or a transaction failing
  outright) refunds before anything else, so the budget counts committed
  sign-ups only (`TestFederatedSignupRetryRefundsCharge`). `EnableSignup`
  wires the policy and the budget together and refuses a missing budget; a
  nil budget refuses a charge instead of admitting one.
- **Display name.** A valid `name` claim becomes the display name, else the
  handle; `signup_completed.display_name_from` records which.
- **Google bare `iss`** is pinned at the `oidcrp` layer: an e2e provider row
  cannot carry Google's issuer (discovery would fetch the real document).
- The staged entry adds a click to every password sign-in (#587 d1's accepted
  cost); every Playwright password sign-in clicks the Password row by
  `/^Password\b/` (#800): the "Last used" badge joins the row's name.

- **Own accounts, own authenticators in e2e.** Flows that prove repeatedly
  (the sign-up door) no longer draw the shared administrator's TOTP ledger,
  which every project draws at once. The `shell.spec.ts` sign-out flows keep
  #800's answer instead: the administrator's challenge is met with the shared
  passkey, so no TOTP step is spent there at all. The door creates throwaway
  accounts (`e2e/fixtures/accounts.ts`: `enrolledAccount`,
  `TotpLedger`). A fresh enrolment treats the step before its creation as
  spent, so a new account presents two codes per step without waiting. An
  instance operator needs three (enrol, step-up, proof), so the sign-up door
  enrols its opener and its closer up front; only the opener's proof may wait
  for one step boundary, and the test budget is the default plus one step
  (`TOTP_STEP_MS`), not a padded constant. Both ran `--repeat-each=3` on
  desktop and mobile.
- **Entra authorities.** The `common` / `organizations` documents publish the
  literal `{tenantid}` placeholder; a domain-name authority's document carries
  the tenant GUID. Both refusals name the document's issuer (fixtures for
  both in `issuer_test.go` and `TestOIDCProviderIssuerDepartures`).
- **Guard.** `TestNothingRelaxesTheLibraryIssuerCheck` fails if code outside a
  test sets `SkipIssuerCheck` or calls `InsecureIssuerURLContext`. Discovery
  failures log their class of cause; go-oidc's own error is not wrapped
  because it quotes the provider's response body
  (`TestAllOIDCLegsUseBoundedClient` forbids that).
- **CLI.** `hikyo org list --origin manual|registration` with an `ORIGIN`
  column (api-cli-spellings section 8, amended).
- `GET /orgs` gained the shared `BadRequest` response (for `?origin=`); the
  generated TS client therefore repeats that response's existing description,
  which carries an em-dash. No new prose here contains one.

No spec-versus-resolution contradiction was found.

## For the next tickets

- **#608** (local sign-up): render the local row on the door (the page ignores
  `"local"` today), add check-your-mail under the staged door, and set
  `accounts.email` + `email_verified_at` with the unverified-holder clear
  (#605's handoff). The budget, the gate order and `refusal()` shape here are
  the pattern; `signup_refused`'s cause enum already carries the token causes.
- **#609** (OAuth2): `completeLogin`'s sibling reuses `oidcSignup`'s order
  with `/user/emails` for the assertion (`verified_by: github-primary`), a
  `github` brand, and the entry match on `oauth2` rows (`resolveEntryProvider`
  and `evaluatePolicy`'s kind branch). The login page filters the door to
  `oidc` today.
- **#612** (login handoff): the handoff page must hide the door and send
  `sign-in`; `LoginForm` takes `signup={null}` for that.
- **#610/#611**: `claim` and `establish` purposes; `auth.oidc_refused`
  `identity-exists` for claim.

## How to test

```sh
docker run -d --rm --name hikyo-pg -e POSTGRES_USER=hikyo -e POSTGRES_PASSWORD=hikyo \
  -e POSTGRES_DB=hikyo_test -p 127.0.0.1:55607:5432 postgres:18-alpine
export HIKYO_TEST_POSTGRES_DSN='postgres://hikyo:hikyo@127.0.0.1:55607/hikyo_test?sslmode=disable'
go test ./internal/store/migrate/ -run TestSocialSignin
go test ./internal/isolation/ -run 'TestFederated|TestOIDC|TestAudit|TestRegistration|TestOrgRename|TestInvariant'
go test -timeout 90m ./internal/... ./api/...
docker rm -f hikyo-pg

cd web && node --run typecheck && node --run lint && node --run test && node --run design:check
pnpm run build
HIKYO_E2E_PORT=46071 HIKYO_E2E_PORT_B=46072 HIKYO_E2E_PORT_TLS=46073 \
  HIKYO_E2E_PORT_OPERATIONAL=46074 HIKYO_E2E_PORT_OPERATIONAL_B=46075 \
  pnpm exec playwright test --project=desktop e2e/flows/login.spec.ts
# and --project=mobile; the password step touches members, shell, workspace
# and instance-admin specs too.
```
