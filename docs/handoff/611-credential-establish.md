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
