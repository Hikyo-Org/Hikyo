# Issue 611: local credentials for social accounts

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/611>.
Assessment base: `c12e7d39c9f227b0cb884ba69e4f1bd77c895f76`.
Implementation and review: delegated to Codex `gpt-6.1-sol`, high effort.

## Necessity

The issue remains necessary. The merged OAuth2 work supplied establishment
transactions and a session evidence carrier, but its handoff explicitly left
local enrollment consumers, provider selection and native CLI step-up to 611.
The invitation-claim handoff also left OIDC establishment to 611. At the
assessment base there was no password set/change endpoint, and identity linking
and unlinking still required a password.

Sources: [609 handoff](609-github-oauth2.md),
[social sign-in specification](../spec/social-signin.md), and the final
[#586 resolution and clarification](https://github.com/Hikyo-Org/Hikyo/issues/586).
The locked UI reference is `docs/site/public/prototypes/social-signin/2/index.html`.

## Owner decision

The original acceptance criterion required changing an existing password with
TOTP proof, contradicting the owning 586 resolution and operative human-auth
clause (k). Marc explicitly selected option A on 2026-10-15: preserve
password-first. The existing password is required whenever one exists;
confirmed TOTP can authorize setting a first password when none exists.

The implementation already follows that choice. Issue 611's acceptance clause
has been corrected, and the
[owner ruling is recorded on #589](https://github.com/Hikyo-Org/Hikyo/issues/589#issuecomment-6199131864).
No human-auth policy amendment is needed. Issue 589 remains open for the
separate 612 amendment and must not be closed as a side effect of completing 611.

## Implementation scope

- OIDC and OAuth2 establishment prove the acting account's existing identity,
  while it has no local proof credential. The account-security stamp is bound
  to the principal and session, expires after five unused minutes, and is
  consumed by the first completed credential mutation's session reissue.
- Password set, TOTP enrollment, passkey enrollment and recovery-code generation
  consume this evidence. It cannot authorize disclosure, linking or unlinking.
  Establishment is an audit proof class, never an authentication factor.
- `PUT /api/v1/auth/password` validates the password, verifies the selected
  proof, writes atomically, revokes sessions and reissues the acting session.
  The API, generated clients, server seam, parity and native CLI are connected.
- Identity link/unlink uses the documented password/TOTP selection. The unlink
  guard checks whether a usable login credential would remain, including the
  complete passkey-only invariant. TOTP alone is not a login credential.
- The Security UI provides Set/Change password, linked-provider verification,
  truthful stamp status, a provider picker keyed by kind and slug, and refusal
  guidance. CLI commands refuse impossible browser-only ceremonies before
  prompting or opening the browser.
- API revision checks prevent new clients from interpreting missing credential
  inventory on an older server as an empty account.

The wire/probe registry owns authentication endpoint classification and audit
closure, matching existing factor endpoints. No unused unauthenticated entry
is added to the separate proof/formula registry. This follows the two-registry
distinction in [audit-model](../adr/audit-model.md), completeness linkage.

The API revision is 11. The CLI checks the revision before using the new
password operation or credential inventory, so missing fields on an older
server cannot be mistaken for an account with no credentials.

## Preserved boundary

The operative human-auth ADR explicitly records that passkeys are not yet
accepted as account-security mutation proof. This work does not introduce a
new passkey-proof ceremony. A passkey-only account without password/TOTP gets a
truthful refusal rather than a promise that its passkey can authorize these
mutations. Existing passkey/recovery operations retain possession-first proof;
the password/link selection remains separately pinned by 586.

## Validation and delivery

The owner decision above resolves the policy conflict. Local implementation and
validation are complete. Results below are not remote CI or production-provider
evidence. The owner subsequently authorized signed commit, PR creation, CI and
review repairs, and merge after the required gates pass. Remote delivery is
tracked by the PR; release and production deployment remain outside this task.

| Scope | Evidence so far |
| --- | --- |
| API, server, CLI | Complete package tests passed; generated contract and CLI revision/refusal tests passed. |
| TypeScript client | Typecheck and 37 tests passed. |
| Web | Final pre-push typecheck and all 1,714 unit tests passed (198 files, 12.81 seconds). The final loading-state, provider-copy and proof-field clearing changes also passed 23 targeted account/sensitivity tests plus lint and production build. |
| Database lifecycle | All 11 new `TestCredentialEstablish*` families passed on SQLite and a dedicated PostgreSQL 18.6 container in 54.348 seconds. A final cross-provider provenance and CLI refusal rerun passed in 10.953 seconds. |
| Approved password-first policy | The expanded `TestCredentialEstablishTOTPAndExpiry` passed for OIDC and OAuth2 on both engines in 6.467 seconds. It sets the first password with TOTP, refuses a fresh valid TOTP for replacement, proves that same code still succeeds for recovery generation, then changes the password with current-password proof and checks session revocation and audit evidence. |
| Existing lifecycle regression | OAuth2 identity/epoch-fence/audit coverage passed in 71.664 seconds. Scoped OIDC, SAML replay, passkey, TOTP, recovery and replaced-password-proof tests passed on both engines in 35.263 seconds. |
| Real application browser | All 12 desktop account tests passed in 4.1 minutes and all 12 mobile account tests passed in 3.7 minutes, including establish, link refusal, first-password set, password change, persistence after signing back in, passkey enrollment/removal, recovery replacement and dark/light accessibility assertions. |
| Independent review | Sol 6.1 high found restored-credential, SAML proof-selection, CLI eligibility and live proof-field secret-retention defects. Repairs were rechecked with no remaining source defect found in the reviewed deltas; this is a source-review result, not a runtime verdict. |

The service, authn, audit, conformance and authz package checks passed. SQL and
authorization forwarders were regenerated, `graphify update .` completed, and
`git diff --check` passed. Browser stories passed in both dark and light themes
(35 in each); the later provider-copy change passed all 10 AccountSecurity
browser stories. The parent visually inspected the desktop establish chooser and
verified status against prototype iteration 2 at 1280 by 800. Mobile chooser,
status and password form were inspected at the Pixel 5 fixture's 393 by 727 CSS
viewport, with the prototype resized to match. Existing app navigation differs
from the older prototype, so no pixel-identical claim is made.

The final pre-push unit run includes all UI fixes. The desktop browser bundle
preceded the loading-state copy fix; the rebuilt mobile bundle includes it. The final
provider-verification lede correction and proof-field remount passed targeted
regressions, typecheck, lint and build after the browser bundles were loaded.
Their screenshots therefore precede those two final refinements. The reviewer
rechecked these narrow UI refinements after the initial
source-review snapshot. The remount regression types a password before the
credential inventory switches to TOTP, then verifies that the new visible code
field is empty and only the new OTP is submitted.

The browser workflow uses the real Go server, Chromium, a local fake identity
provider and a CDP virtual passkey authenticator. It does not verify a production
Google, Microsoft or GitHub integration or physical passkey hardware. The
existing required-MFA gate correctly appears after a password-proved change
when no second factor is enrolled; tests must not preserve old IdP assurance
to bypass that gate.

Failed attempts retained for final accounting:

- The initial full web run exposed an Undici fixture incompatibility. It was
  reproduced with Node 26.7 and repaired by matching the fixture's fetch and
  dispatcher implementations, without changing application transport.
- The first real browser flow reached the provider callback, then failed because
  `OIDCDone` did not recognize `establish`. The parser and continuation were
  corrected and callback regression coverage was added.
- Overlapping isolation test processes collided in their shared derived
  PostgreSQL database. Subsequent isolation runs use that database sequentially;
  those failures are not counted as passing lifecycle evidence.
- A desktop browser attempt expected Security after reload, but the required-MFA
  policy correctly redirected the password-proved session to enrollment. The
  test now checks that gate and then signs in through the existing provider to
  verify the persisted password state.
- A later browser attempt timed out waiting for its operational health endpoint
  despite server-ready logs. Its log is retained and the fixture now records
  the actual HTTP status or fetch cause to diagnose any recurrence. Source
  inspection found that a synchronous sibling build could consume the first
  instance's health deadline. The fixture now starts sequentially and reuses the
  identical binary. Subsequent startup passed; the exact original timing cause
  was not instrumented, so it remains a source-supported diagnosis.
- Expanded lifecycle tests caught an expired-stamp refusal leaking
  `domain.ErrNotFound`; the consumer now returns `ErrNoProofCredential` and the
  expiry cases pass.

## PR 925 review repairs

Qodo's five findings on `b308a44530af` were verified and repaired by Sol 6.1
high workers, with parent review. Auto-merge was disabled while repairing them.

- Password mismatch now uses shared Input/Field errors associated with both
  controls. The server refusal remains a form-level alert. The sensitive-state
  ownership and clearing behavior were reviewed before updating its source pin.
- Password mutation is registered as an expected account-session rotation.
  A regression using the real hook and AuthProvider reproduced the old epoch
  advance and lost success callback for both first-set and replacement, then
  passed after the fix. Previous browser assertions observed persisted state
  and dialog closure, which also occurred during the erroneous session reset.
  The browser flow now checks success feedback and immediate policy navigation.
- Unlink and both passkey invariant paths share a usable external-identity
  predicate. Disabled, missing, mismatched or epoch-inert identities cannot
  satisfy the remaining-login floor. SAML retains its supported same-entity
  provider recreation behavior and checks metadata expiry.
- An independent second review found that SAML email-NameID opt-out also makes
  a retained identity unusable. A real signed link followed by the actual
  provider policy toggle reproduced this for unlink and passkey removal on both
  engines. The fix decodes the existing stored representation through its
  samlsp owner and reapplies the callback's existing NameID policy, requiring
  exact re-encoding. Decoder tests cover presence, arbitrary bytes, truncation,
  lengths and trailing data. The reviewer rechecked the repair without finding
  another defect in this delta.
- Password audit `first` now reflects the current credential epoch, separately
  from the physical row's INSERT/CAS choice. Regression coverage establishes
  over an inert restored row, then changes the resulting current password,
  checking both audit values and physical row behavior.
- Credential races map to the uniform HTTP 409 conflict. The initial regression
  reproduced HTTP 500; changing only the mapping exposed the missing OpenAPI
  response. Password and four verified sibling operation contracts now declare
  409; recovery regeneration already did. Generated clients were refreshed.
  Seven HTTP contract cases cover plain/wrapped password errors and siblings.

Repair validation: final web typecheck, lint, production build and all 1,718
unit tests passed. API/server package tests passed (1.677/65.166 seconds), as did
client typecheck and all 37 client tests. Core backend repair cases passed on
SQLite and PostgreSQL 18.6 in 22.007 seconds; broader establishment, SAML
recreation, passkey-floor and audit regressions passed in 142.733 seconds. The
final SAML policy and provider eligibility cases passed on both engines in
15.775 seconds. Final complete service/samlsp package checks passed in
51.599/0.695 seconds. The strengthened social-password browser flow passed on
desktop (2.9 minutes including setup) and mobile (3.0 minutes), with both
mismatch screenshots visually inspected. Those OIDC browser runs preceded the
last SAML-only refinement, which has the separate database and package evidence
above. Go import formatting, the final AST graph refresh and diff checks passed;
the graph refresh retains its existing partial-extraction warnings for 17 Astro
documentation files. These are local results; the repaired head still requires
fresh hosted feedback and immutable merge-group validation.

## CodeRabbit review follow-through

The full included CodeRabbit review of `c46430b164f7` completed with five
comments. Sol 6.1 high workers checked each against source, accepted decisions
and regression evidence; the parent reviewed the resulting changes.

- Password and identity-unlink wire declarations now include their conditional
  developer-credential revocation event. The generator owner was corrected,
  output regenerated, and the declaration regression failed before the fix.
  Actual emission follows the existing `revokeDeveloperScope` call paths.
- CLI `set` and `change` remain aliases, as the operative api-cli-surface
  amendment's single set-or-change verb and api-cli-spellings section 8 specify.
  Help and canonical spelling documentation now say this explicitly. Both
  spellings are tested with both first-password TOTP and existing-password
  proof, including replacement-session persistence and secret redaction.
- Session liveness assertions now reject every error, and assurance assertions
  check the factor count before indexing. The affected lifecycle families pass
  on both engines.
- SAML identity inventory resolves a missing historical provider row to the
  configured provider with the same entityID. Both-engine recreation tests
  reproduced the empty presentation fields, then passed after repair. Disabled
  providers retain their display metadata, consistent with the inventory's
  existing behavior. Listing never changes historical identity provenance;
  login eligibility and callback rebinding remain separate checks.
- The reported empty local-proof submission is already prevented by
  ProofDialog's disabled button and guarded submit handler; explicit password
  and TOTP regressions confirm that. A nearby delayed-timer case did reproduce:
  a rendered establishment Continue action could outlive its stamp. All four
  creation continuations now recheck current eligibility and wall-clock expiry
  before proceeding. Expiry keeps the dialog open and refreshes provider
  verification. Tests advance the clock without delivering the expiry timer,
  and also cover credential-inventory changes and valid establishment.

This pass passed all 1,728 web tests, typecheck, lint and production build;
complete CLI/authz/generator/API packages; and the affected SQLite/PostgreSQL
lifecycle families in 18.088 seconds. The complete service package passed in
77.963 seconds. It changes no visual layout. The earlier
desktop/mobile runtime evidence remains separate from these new delayed-timer
and inventory regressions.

## Integration with current main

After PR 926 merged, a fresh fetch found main at
`69475a27e85cd4e2ad823a557ebf9a5507bdd5d7`, while this PR remained at
`b6958b831ae5d9d17bd45d2feb0d3c69bfe3e3c1`. GitHub still displayed the older
base SHA, but its conflict status was accurate: `git merge-tree` reproduced one
content conflict in `web/e2e/fixtures/instance-fetch.ts`. The common ancestor
was `c12e7d39c9f227b0cb884ba69e4f1bd77c895f76`.

Both changes repaired the same installed Undici dispatcher versus Node bundled
fetch mismatch. The integration uses PR 926's single installed Undici transport
for ordinary and serving hosts, with request and response types inferred from
that fetch. Fixture cookie handling now uses the matching Undici Response type;
the former mixed-response union is unnecessary. The real-HTTP regression covers
ordinary-host response identity, explicit origin/cookie headers, Set-Cookie
visibility and a subsequent bearer peer request without cookie carryover.
Existing serving-host anonymous, bearer, revocation and origin/header tests stay
intact. The automatically merged Matrix implementation and regression match
current main exactly; both Matrix and account-security sensitivity pins remain.

Local integration checks on pinned Node 26.7.0 passed: 38 focused tests,
all 1,730 web tests across 199 files, typecheck, lint and the production build.
Full test output retains happy-dom teardown AbortError and unmocked HTTP 404
diagnostics; Vitest exited successfully without an unhandled-error summary.
No backend or browser suite was rerun for this fixture integration. The earlier
database and desktop/mobile browser evidence remains separate. The merge was
prepared without rewriting the three reviewed feature commits; signing, range
checks, push and fresh hosted validation remained delivery steps at that
integration checkpoint. The subsequent explicit rebase is recorded below.
The AST graph refresh passed with 55,550 nodes and retains the 17 Astro
partial-extraction warnings noted above. Staged and unstaged diff checks passed.

## Explicit rebase onto main

The user subsequently requested a rebase. Fresh fetches of `origin/main` and
`hikyo-upstream/main` both resolved to
`69475a27e85cd4e2ad823a557ebf9a5507bdd5d7`. Since the integration merge already
contained main, the three feature commits were explicitly replayed to produce
linear history rather than accept an up-to-date no-op. The sole conflict was
again the fixture transport; the reviewed installed Undici resolution and its
matching Response annotations were preserved.

Commit mapping:

- `b308a44530af` to `1896d66891e7`.
- `c46430b164f7` to `b84f9eace0db`.
- `b6958b831ae5` to `6fc8ccb0674b`.

The retained local backup `backup/issue-611-pre-rebase-7113bb5` points to
`7113bb580a2738d160c9dd621af0f916ed5998c9`. The regression and handoff changes
introduced only by that merge were restored as a separate preservation commit.
Before this rebase note was added, the complete staged tree exactly matched the
backup tree, `b040400f35304d55ad951eae509c0543fc2d3880`. Final application and test
content therefore remains identical to the previously validated integration;
only this handoff's topology explanation differs. Fresh typecheck and lint
passed on Node 26.7.0. The 1,730-test suite, build, real-HTTP and earlier runtime
results above apply to that identical code content; they were not rerun merely
for rewritten commit identities. The complete four-commit rewritten range
passed DCO and local cryptographic signature verification before delivery.
