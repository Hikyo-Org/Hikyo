# AWS sandbox account and fixture provisioning plan

Provisioning and execution contract for #158. The dedicated account and MFA
were verified and `eu-central-1` selected. The latest approved proposal permits
one additional manual fixture: one new KMS key, the same eight roles, one
temporary boundary, at most one OIDC provider, and at most 32 new synthetic
creations, reaching 144 conservative lifetime reservations. Retain the existing
setup identity/MFA and protected environment, preserve all three old key-deletion
schedules, and perform exact cleanup with seven-day new-key deletion. The gross
target is below USD 5 where possible, absolute maximum USD 10 before credits;
fresh preparation admits a conservative combined estimate at most USD 3.
No paid upgrade, unrelated resources, recurring run or automatic retry is
authorized. Concrete account and billing metadata belong in private run inputs
and evidence. The [current handoff](../handoff/158-aws-secrets-manager.md#current-status-2026-10-21)
supersedes the older dated setup records.

Provisioning and execution follow successful local checks and source review.
This plan does not assert that a fresh fixture is active or successful.
The runtime contract and policy templates are in
[aws-sandbox.md](aws-sandbox.md).

## Account requirements

Recommend account display name `Hikyo Nonprod Adapter Acceptance`. Use the
owner-supplied signup address exactly; it is recovery identity, not a fixture
credential. Do not infer a legal company name, account type, country, address,
phone number, tax identity, or payment details from this display name. The
parent routes verification/payment fields directly to the user or private tools.

Recommend **Sign up for AWS (advanced)** and the **Free account plan**, if
available to this customer. The signup experience and billing plan are distinct
choices. Select zero-cost support if offered, with no paid support add-on.
AWS now also offers a simplified signup experience with managed
permissions and region selection; its own comparison advises advanced signup
for fine-grained role permissions. The simplified Free service list includes
IAM, STS, Secrets Manager and KMS, so service listing alone does not establish
that its access model fits this fixture. Do not create a duplicate account if
signup has already begun. Record the actual offered experience and limits.
Sources: [signup comparison](https://docs.aws.amazon.com/accounts/latest/reference/sign-up-for-aws.html),
[supported services](https://docs.aws.amazon.com/accounts/latest/reference/supported-services-sign-up-new.html).

The advanced signup guide currently requires a valid payment method; the newer
Free Tier FAQ says most customers in the new experience do not need one.
Treat these as different flows, not a promise of payment-free advanced signup.
Free eligibility depends on AWS's determination of new-customer status, not
just a new email alias. If only Paid is offered, stop at that decision for the
parent/user. Sources: [advanced signup](https://docs.aws.amazon.com/accounts/latest/reference/getting-started.html),
[Free Tier FAQ](https://aws.amazon.com/free/free-tier-faqs/).

The Free account plan ends after six months or credit exhaustion. The documented
advanced plan starts with USD 100 in credits; additional credits require
activities and are not a reason to launch extra services. Do not join AWS
Organizations, set up Control Tower, or select an offering that changes plan
status: AWS documents automatic Paid upgrades for several such actions. Do not
create an organization solely to enable this fixture's login flow.
Source: [billing-plan rules](https://docs.aws.amazon.com/us_en/awsaccountbilling/latest/aboutv2/free-tier-plans.html).

Before handing over the sandbox:

- Confirm recovery email access; keep password/recovery material in the user's
  approved private custody. Enable root MFA promptly, preferably a passkey or
  security key with a recoverable backup arrangement. Do not create root access
  keys or use root credentials in tests.
- Establish a non-root human setup identity with MFA. Its administration route
  must fit the actual account plan; do not silently enable Organizations or
  Identity Center as a bootstrap shortcut.
- Record the 12-digit account ID, actual account plan, credit/expiry information,
  selected region, MFA-enabled status, and setup-identity ARN as nonsecret
  metadata. Do not expose payment/contact details, passwords, tokens or key IDs
  in the handoff.
- Use `eu-central-1`, explicitly selected by the owner on October 18. Do not enable additional regions or
  provision workloads as part of signup.

These identity recommendations follow
[AWS root security guidance](https://docs.aws.amazon.com/IAM/latest/UserGuide/root-user-best-practices.html)
and [IAM temporary-credential guidance](https://docs.aws.amazon.com/IAM/latest/UserGuide/best-practices.html).

## Approved resource bill of materials

The latest approved single-run proposal covers these bounds and retains the
non-root setup identity and protected execution route. The existing setup policy
and new temporary boundary implement that approved route; their exact versions
and lifecycle are specified below. Review the substituted change set before use.

| Resource | Approved bound and name | Purpose / end state |
|---|---|---|
| Customer KMS key | One new symmetric encrypt/decrypt, single-region key; no alias | Owner MFA console creates the key with the sole account-root IAM-delegation key-policy statement. No extra administrators/users, aliases, rotation, replica or custom key store. After exact cleanup, schedule seven-day deletion and preserve all three old schedules. |
| Fixture IAM roles | Eight: `hikyo-158-static`, `hikyo-158-ambient`, `hikyo-158-assume`, `hikyo-158-web`, `hikyo-158-kms-denied`, `hikyo-158-source`, `hikyo-158-oracle`, `hikyo-158-cleanup` | Five writers, separate AssumeRole source, separate synthetic reader and separate cleanup. No broad shared test admin. Remove fixture-specific policies/trusts after evidence/cleanup. |
| OIDC provider | At most one `token.actions.githubusercontent.com` provider | Seven fixture roles trust the exact approved immutable GitHub repository/environment subject and `sts.amazonaws.com` audience. The eighth, `hikyo-158-assume`, trusts only the source role and exact external ID. No wildcard subject or human setup-user trust. |
| Setup identity | Retained `hikyo-158-setup` IAM user, console login and sole named MFA device | No access keys, groups or additional policies. Official same-device CLI sign-in supplies temporary setup credentials privately. Preserve this identity and MFA until #158 is complete and satisfied, as requested. |
| Setup managed policies | Existing setup-user policy and one new immutable fixture-role permissions boundary | Delete only obsolete nondefault setup v1, activate bounded v6, then restore existing self-service v5 after retirement, preserving v2-v6. Require the exact new boundary on all eight roles. Setup cannot administer either policy. Remove only the unused temporary boundary after role cleanup. |
| Secret namespace | One random `hikyo-e2e-<run-id>/` prefix | 31 nominal creation attempts before replacement retries; 32 total attempts per approved run, including failures and retries. The compiled suite shares a cumulative reservation counter and single-use marker; controlled guard tests passed, while real AWS proof remains pending. |
| Compute/network/storage | Zero AWS compute, VPC endpoints, NAT gateways, clusters, buckets or deployments | Run Hikyo and both databases on the allocated existing local/runner resources. |
| Billing observation | Existing plan, credits/expiry and gross billing views | Combined target below USD 5 where possible, absolute USD 10 before credits; fresh admission estimate at most USD 3. Monitor separately. No claim of an instantaneous billing hard cap. |

The suite uses one entry point and a shared 32-attempt creation
counter, including failed calls, retries, replacements, and cleanup seeding.
A persistent single-use namespace marker prevents a fresh process in the same
protected fixture directory from resetting that budget. This is not a
cross-directory or AWS-enforced cap. Independently verify the local guards and
reconcile the inventory before real execution. Account signup alone grants no fixture authority; use the latest separately
approved single-run bounds recorded in the current handoff.

Create the new key and temporary boundary with no tags. Tag each new role and
the run-created OIDC provider with exactly `HikyoIssue=158` and
`Authorization=<bound authorizationId>` from the reviewed run. Do not add
generic tags or alter existing resources. Names and tags are metadata only;
keep all synthetic payloads and actor credentials out of them.

## Ordered provisioning and retirement

1. Parent verifies the existing account, root MFA and Free-plan metadata; do not
   create another account. Bind account, `eu-central-1`, exact names, run and
   cleanup windows, cost and protected execution route to the approved bounds.
   Stop on an account mismatch, unexpected existing resource or expanded scope.
2. The owner uses the authenticated MFA console to create exactly one
   `SYMMETRIC_DEFAULT`, `ENCRYPT_DECRYPT`, `AWS_KMS`, single-region key with no
   alias. Keep only the default account-root IAM-delegation statement;
   add no key administrators, users or grants. Preserve lockout safety checks.
   Record the key ARN and compare the returned policy to the reviewed default.
   No root credentials or access keys enter local tools or the fixture.
3. Resolve that exact key ARN, account, region, external ID, immutable OIDC
   subject and absolute UTC expiries in the reviewed setup and boundary
   policies. Check final hashes, no unresolved placeholders and managed-policy
   size limits. Verify the retained setup identity, MFA, existing PolicyId and
   full v1-v5 history with default v5. Owner deletes only obsolete nondefault
   v1, activates exact bounded v6, and creates the fresh immutable boundary.
   Preserve the retained user, MFA, existing v2-v5 documents and old key schedules.
   Do not give the non-root setup user `CreateKey`, `PutKeyPolicy`, alias writes,
   role assumption or policy/boundary administration.
4. Reuse the existing MFA-backed setup access. If it expires, sign in privately
   and use the official same-device `aws login` flow without recreating the
   identity or MFA. Keep passwords and codes private. Verify the resolved user,
   account and an MFA-gated read-only IAM inventory request before any mutation.
   Missing MFA context must fail closed; do not relax the condition or create
   access keys to bypass a failed probe.
5. Setup inventories the eight names and exact issuer, creates absent roles only
   with the immutable boundary, and installs reviewed per-role policies/trusts.
   Seven roles use the exact approved GitHub OIDC subject and audience; only
   `hikyo-158-assume` uses source-role trust plus the external ID. Inspect returned
   policies, boundary attachment and trust. Stop on unexpected existing objects;
   never repair them blindly or replace a pre-existing provider.
6. Protected execution obtains short-lived sessions through OIDC. Static-mode
   descriptors consume those temporary sessions; AssumeRole receives only its
   distinct source session. Ambient and explicit WebIdentity use authentic fresh
   projected tokens. No setup-user credential or human trust enters a fixture
   descriptor. Inspect all eight effective identities and scopes before release
   of the real-write hold; local checks and final review must already pass.
7. Verify exact-ARN synthetic cleanup and reconcile ambiguous creates, then
   retire the exact fixture role policies/roles and any provider created by this
   run. Schedule deletion of the exact new key for seven days under approved
   cleanup authority. Owner removes only the unused new boundary and restores
   existing self-service v5, preserving setup v2-v6. Retain the setup login/MFA/user,
   account and protected environment. Remove only the consumed execution settings;
   never remove a boundary still attached to a role or cancel an old key schedule.
   Record pending KMS deletion and its date, not immediate key removal.

The runtime JSON templates do not grant setup authority. Owner-console bootstrap
and the constrained non-root setup policy are distinct from fixture execution.
IAM and KMS creation remain account writes; the resource counts and spend ceiling
are enforced by the approved serialized procedure and ledger, not a claim that
IAM provides a one-call or billing cap.

## Approved secure token delivery route

The approved route for authentic WebIdentity is a reviewed, manually dispatched
GitHub Actions job on an exact approved commit, protected by a dedicated
sandbox environment. Trust `token.actions.githubusercontent.com` with audience
`sts.amazonaws.com` and the approved immutable repository/environment subject. Restrict the environment
to the intended protected branch and approvals. Do not run untrusted PR code
with `id-token: write`; avoid `pull_request_target` as a delivery shortcut.
The bounded workflow/environment/provider route is authorized; its reviewed
implementation and actual creation remain separate evidence.
[GitHub's AWS OIDC guidance](https://docs.github.com/en/actions/how-tos/secure-your-work/security-harden-deployments/oidc-in-aws)
describes the issuer, audience and subject constraints.

Each externally obtained session/token must stay within the trusted runner and
protected files. A fresh OIDC token must be available when a new client retrieves
credentials; a single captured short-lived JWT may expire during this suite.
Plan an approved token-refresh projection or bounded fresh-token test segments.
Do not send JWTs through chat, artifacts, workflow outputs, or CLI arguments.
No additional AWS compute is needed for this path.

There is no approved local fixture-credential injection fallback. Human setup
uses the separately constrained CLI sign-in route; that does not project
GitHub workload tokens into a local test run. The advanced signup guide documents `aws login` for AWS CLI
2.32.0 or newer; the previously inventoried CLI 2.37.10 meets that version
threshold. This is a candidate for non-root human setup access, not proof that
the local test files or WebIdentity tokens are securely populated. Do not run
login, export credentials, or start a duplicate browser flow under this hold. The current T3 `request_secret` opaque reference has no
established bridge to the fixture files. Do not request secrets until such a
path is implemented. Root owns private signup fields; signup completion alone
does not establish programmatic test credentials. No duplicate signup, browser
session, credential-refresh attempt, workflow dispatch or AWS request is
performed by this plan.

## Cost and retirement check

AWS currently illustrates Secrets Manager pricing at USD 0.40 per secret-month
and USD 0.05 per 10,000 calls. Customer KMS keys are illustrated at USD 1 per
month, with symmetric-key request examples at USD 0.03 per 10,000 requests;
verify the actual selected region and eligible credits before approval.
These are billable-service rates, not a claim that KMS/Secrets Manager are
unconditionally free. Sources:
[Secrets Manager pricing](https://aws.amazon.com/secrets-manager/pricing/),
[KMS pricing](https://aws.amazon.com/kms/pricing/).

Illustrative 30-day-month estimate: 31 secrets held for two hours, one key held
for one day, 2,000 Secrets Manager calls and 2,000 KMS requests total roughly
USD 0.084 before free allowances/credits/tax. This is an estimate, not an observed
bill or enforced maximum. If 31 secrets and the key remain for a month, the
illustrative storage alone is USD 13.40. Cleanup and expiry are essential even
on a credit-funded account.

After verified secret removal, KMS deletion has a mandatory 7-to-30-day waiting
period. The owner approved seven days for this disposable key only; scheduled deletion is not immediate deletion. AWS says
keys scheduled for deletion are not charged, but cancelling deletion restores
charges as though it had not been scheduled. Capture the key's pending-deletion
state and date, then arrange final verification without maintaining an owner
polling loop. Sources: [KMS deletion](https://docs.aws.amazon.com/kms/latest/developerguide/deleting-keys.html),
[KMS pricing](https://aws.amazon.com/kms/pricing/).

The account may remain available for subsequent explicitly scoped acceptance
runs. Account closure, paid upgrades, extra regions, extra services, and new
resource batches remain separate decisions. #158's real-provider proof and
formal-review obligations remain unchanged; #79 does not duplicate AWS tests.


## Current concrete setup handoff

| Item | Known state and next evidence |
|---|---|
| Signup/account | The operator verified the existing dedicated sandbox account. Recover that account; do not sign up again. Keep its concrete account ID in private run inputs. |
| Root MFA/session | Owner reports signup and MFA complete on October 18; parent read-only browser verification confirmed root MFA enabled and no active root access keys. No root access keys are requested. |
| Account plan | A Free plan was verified in the recorded snapshot. Current plan, credits, expiry and restrictions require fresh private evidence. The latest one-run proposal supplies bounded resource/spend authority; account credit does not expand it. |
| Region | Owner explicitly selected eu-central-1 for all AWS connections on October 18. Resource/spend/deletion authority remains separate. |
| Setup authority | The existing non-root setup identity and sole named MFA are retained. Official CLI identity/MFA were independently verified on October 21; obtain fresh identity, full policy and MFA-context proof before activation. Do not put administrator or root credentials into any test descriptor. |
| Fixture scope | One additional manual run, one new key, eight roles, one temporary boundary, at most one provider and no unrelated compute/network/storage resources. At most 32 new creations, retaining all 112 prior reservations and reaching 144 lifetime. Fresh effective-policy, inventory, cost, CI and review gates remain. |
| Credential custody | The separately reviewed trusted runner supplies private OIDC projection and temporary sessions. The protected environment is retained; new issuer/role provisioning, private projection and execution remain pending. There is no local fixture-injection fallback; browser sign-in does not populate test credentials. |
| Destructive cases | The latest approved single-run scope includes exact-ARN synthetic replacement and final force deletion, retained and scheduled-delete canaries, failed/ambiguous create reconciliation and the bounded cleanup window. No prefix-wide sweep. |
| Runtime | The October 21 integration uses witness migration 80, preserving main's 77 authorization indexes, 78 capacity and 79 pairing. Canonical bindings and fresh SQLite/PostgreSQL compatibility claims pass, with recorded provider, store, offline fixture and authenticated lifecycle checks. Earlier 57/37-test runs remain historical evidence. See current handoff for exact candidate/counts; successful real AWS acceptance remains unproven. |

Setup remains assigned to the parent operator, not the #158 feature test owner. Parent and #79 consume the recorded reservation and conditional
release status; neither the migration rename nor source review promotes the
feature or creates another cloud test owner.

## Bounded setup authority and activation checks

The earlier proposal permitting non-root KMS creation/policy editing, human
fixture-role assumption and optional permissions boundaries is superseded and
must not be executed. It was rejected during setup-policy review. The operative
route is the owner-created key/default policy, immutable fixture boundary and
constrained setup managed policy described above. The latest approved
single-run proposal covers the exact sequence; no insecure manual fallback remains.

| Principal and phase | Exact authority | Required constraint |
|---|---|---|
| Owner MFA console bootstrap | One new key without alias and one new boundary; exact retained-setup v1 deletion/v6 activation/v5 restoration | Verify the retained user/MFA and PolicyId, full preserved version history, default key policy and exact new inventory. Do not recreate the setup identity or use root programmatic credentials. |
| Setup self-service | Own password change, own virtual MFA creation/enrollment/resync and MFA inventory; official same-device sign-in | Exact setup user/device ARNs where supported, absolute setup expiry; these grants do not authorize fixture mutations without MFA. |
| Setup inventory | Read the eight exact roles and their inline/attached policy metadata; list roles/providers; inspect exact key | Fresh MFA and successful MFA-gated read-only probe; inspect effective grants and boundaries, not only inline policies. |
| Setup role creation/configuration | Create and configure only the eight exact fixture roles | CreateRole requires the owner-installed exact boundary ARN. No PassRole, managed-policy attachment, user/key creation or boundary/policy-version modification. |
| Setup issuer | Create/read/tag the exact GitHub OIDC provider; delete only if owned by this run | Exact approved audience and immutable subject in all seven workload trusts; independently compare returned trust documents because the boundary cannot constrain their JSON text. |
| Setup KMS | DescribeKey, GetKeyPolicy, GetKeyRotationStatus; ScheduleKeyDeletion | Exact owner-created key in eu-central-1, MFA, setup expiry, deletion window exactly seven days. No CreateKey, PutKeyPolicy, aliases, grants or cryptographic use. |
| Runtime session issuance | Seven OIDC roles; source assumes only the eighth writer role | Exact provider/audience/immutable subject and expiry; source-to-writer external ID. Setup is explicitly denied role assumption. |
| Retirement | Delete exact fixture role policies/roles and run-created issuer; seven-day new-key deletion; owner deletes unused new boundary and restores existing v5 | Retain setup identity/MFA/account and protected environment. Only after reconciled exact cleanup within approved expiry; do not delete pre-existing resources or detach a live role's boundary. |

The boundary permits only the approved namespace, exact KMS key and
source-to-writer role transition. Per-role policies narrow that ceiling; only
the oracle may read synthetic values and only cleanup may force-delete. The
setup user cannot alter the boundary or KMS policy to escape those limits.
Resource/session grants still require independent inspection. See
[AWS boundary semantics](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_boundaries.html)
and [default key policy](https://docs.aws.amazon.com/kms/latest/developerguide/key-policy-default.html).

Resolve and validate every final JSON artifact before activation. Setup expiry
is at most four hours from activation; runtime mutation lasts at most two hours,
with cleanup at most two hours beyond that mutation deadline. Require all eight
role names and the provider to be absent before initial provisioning. After
creation, verify only the actual new immutable IDs against this run's custody,
boundary, policy and trust; never adopt a matching pre-existing object. Inspect
the key policy and verify no aliases separately. Record final hashes and
returned nonsecret metadata. The repository's runtime templates are insufficient
to reconstruct missing private setup grants: if the reviewed concrete setup
artifacts are unavailable, stop and recover them instead of inventing grants.

Verify the existing sole MFA device and fresh MFA-backed setup CLI context;
do not reenroll, reset or recreate the retained device or user. A successful
GetCallerIdentity alone does not prove `aws:MultiFactorAuthPresent`. The mutation
and inventory guard uses `Bool true`; do not replace it with an absent-key
allowance. Diagnose a failed probe before provisioning. No setup or fixture
credentials belong in chat, arguments, logs or reports.

The protected runner supplies fresh inputs and four naturally expired sidecars
with their same-role/issuer provenance, retains material only in private files,
and refreshes projected tokens as needed. It must preserve reservation counts,
recorded ARNs, unresolved names and independent final absence evidence through
failure and cancellation. Account signup or an opaque secret reference is not
credential delivery. No real AWS result is asserted by this procedure.
