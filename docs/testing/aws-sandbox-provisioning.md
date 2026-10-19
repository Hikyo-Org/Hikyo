# AWS sandbox account and fixture provisioning plan

Provisioning and execution contract for #158. On October 18 the operator
verified the dedicated Free account and root MFA, selected `eu-central-1`, and
approved the bounded fixture proposal: one KMS key, eight fixture roles, at most
one OIDC provider, a protected execution environment and non-root setup
identity as needed, 32 cumulative secret-create attempts, USD 1 gross usage,
exact synthetic cleanup and a seven-day KMS deletion window. Paid upgrades
and unrelated resources remain excluded. Concrete account and billing metadata
belong in private run inputs and evidence.

Provisioning and execution follow successful local checks and source review.
No resource ARN, credential delivery or successful fixture execution is asserted
here. The runtime contract and policy templates are in
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

The October 18 authorization covers these bounds, including the non-root setup
identity and protected execution route. The two setup managed policies below
implement that authorized identity safely; they do not add fixture roles, keys
or workloads. Review the exact substituted change set before use.

| Resource | Approved bound and name | Purpose / end state |
|---|---|---|
| Customer KMS key and alias | One symmetric encrypt/decrypt, single-region key; alias `alias/hikyo-158-sandbox` | Owner MFA console creates the key and alias with the sole account-root IAM-delegation key-policy statement. No extra key administrators/users, rotation, replica or custom key store. After exact synthetic cleanup, remove the alias and schedule the approved seven-day deletion. |
| Fixture IAM roles | Eight: `hikyo-158-static`, `hikyo-158-ambient`, `hikyo-158-assume`, `hikyo-158-web`, `hikyo-158-kms-denied`, `hikyo-158-source`, `hikyo-158-oracle`, `hikyo-158-cleanup` | Five writers, separate AssumeRole source, separate synthetic reader and separate cleanup. No broad shared test admin. Remove fixture-specific policies/trusts after evidence/cleanup. |
| OIDC provider | At most one `token.actions.githubusercontent.com` provider | Seven fixture roles trust the exact approved immutable GitHub repository/environment subject and `sts.amazonaws.com` audience. The eighth, `hikyo-158-assume`, trusts only the source role and exact external ID. No wildcard subject or human setup-user trust. |
| Setup identity | One `hikyo-158-setup` IAM user, console login and its own MFA device | No access keys, groups or additional policies. Official same-device CLI sign-in supplies temporary setup credentials privately. Retire the login, MFA and user after cleanup. |
| Setup managed policies | One setup-user policy and one immutable fixture-role permissions boundary | Owner creates both before enabling setup. Attach the setup policy only to the setup user; require the exact boundary on all eight fixture roles. Setup cannot version, replace, detach or delete either policy. Remove the unused policies only after their principals are retired. |
| Secret namespace | One random `hikyo-e2e-<run-id>/` prefix | 31 nominal creation attempts before replacement retries; 32 total attempts per approved run, including failures and retries. The compiled suite shares a cumulative reservation counter and single-use marker; controlled guard tests passed, while real AWS proof remains pending. |
| Compute/network/storage | Zero AWS compute, VPC endpoints, NAT gateways, clusters, buckets or deployments | Run Hikyo and both databases on the allocated existing local/runner resources. |
| Billing observation | Free-plan balance/expiry and existing billing views | Approved USD 1 gross-usage ceiling, including credit-funded usage; monitor separately. No claim of an instantaneous billing hard cap. |

The suite uses one entry point and a shared 32-attempt creation
counter, including failed calls, retries, replacements, and cleanup seeding.
A persistent single-use namespace marker prevents a fresh process in the same
protected fixture directory from resetting that budget. This is not a
cross-directory or AWS-enforced cap. Independently verify the local guards and
reconcile the inventory before real execution. Account signup alone grants no fixture authority; use the separately approved
October 18 bounds.

Tag supported resources with `Project=Hikyo`, `Purpose=AdapterAcceptance`,
`Issue=158`, `Environment=Nonprod` and the approved run/reference. Names and tags
are metadata only. Keep all synthetic payloads and actor credentials out of them.

## Ordered provisioning and retirement

1. Parent verifies the existing account, root MFA and Free-plan metadata; do not
   create another account. Bind account, `eu-central-1`, exact names, run and
   cleanup windows, cost and protected execution route to the approved bounds.
   Stop on an account mismatch, unexpected existing resource or expanded scope.
2. The owner uses the authenticated MFA console to create exactly one
   `SYMMETRIC_DEFAULT`, `ENCRYPT_DECRYPT`, `AWS_KMS`, single-region key and the
   approved alias. Keep only the default account-root IAM-delegation statement;
   add no key administrators, users or grants. Preserve lockout safety checks.
   Record the key ARN and compare the returned policy to the reviewed default.
   No root credentials or access keys enter local tools or the fixture.
3. Resolve that exact key ARN, account, region, external ID, immutable OIDC
   subject and absolute UTC expiries in the reviewed setup and boundary
   policies. Check final hashes, no unresolved placeholders and managed-policy
   size limits. Owner installs the immutable boundary and setup managed policy,
   then creates the setup user with only its exact policy and console password.
   Do not give the non-root setup user `CreateKey`, `PutKeyPolicy`, alias writes,
   role assumption or policy/boundary administration.
4. Enroll setup MFA through the exact self-enrollment permissions or the owner
   console, keeping password and MFA material private. Sign in freshly with MFA
   and use the official same-device `aws login` flow. Verify the resolved user,
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
   revoke fixture trusts and retire roles, dedicated CI access and any provider
   created by this run. Schedule deletion of the exact key for seven days under
   the approved cleanup authority. Owner removes the alias, setup login/MFA/user
   and unused managed policies; never remove a boundary still attached to a role.
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
| Account plan | The operator verified a Free plan. Current credits, expiry and restrictions belong in private setup evidence; that observation alone is not resource or spend authorization; the separate October 18 proposal supplies the bounded authority. |
| Region | Owner explicitly selected eu-central-1 for all AWS connections on October 18. Resource/spend/deletion authority remains separate. |
| Setup authority | A scoped non-root setup identity is still absent. Keep the concrete IAM inventory in private setup evidence. Do not put administrator or root credentials into any test descriptor. |
| Fixture scope | One KMS key, eight isolated principals, at most one OIDC provider, zero AWS compute/network/storage; 31 nominal secret creations with the unchanged 32-attempt ceiling. Approved on October 18; execution still requires the local-check and review gates. |
| Credential custody | The separately reviewed trusted runner provides private OIDC projection and temporary sessions. Protected environment/issuer setup and actual execution remain pending. There is no local fixture-injection fallback; browser signup does not populate test credentials. |
| Destructive cases | The October 18 scope includes exact-ARN synthetic replacement and final force deletion, retained and scheduled-delete canaries, failed/ambiguous create reconciliation and the bounded cleanup window. No prefix-wide sweep. |
| Runtime | Provider checks passed 57 top-level tests; focused backend checks passed 37 top-level tests with PostgreSQL 18 and TLS Moto. Canonical witness migration 78 bindings and compatibility metadata were regenerated against October 19 integrated main, preserving its migration 77 indexes. See the issue handoff for current broader checks; local evidence does not establish real AWS acceptance. |

Setup remains assigned to the parent operator, not the #158 feature test owner. Parent and #79 consume the recorded reservation and conditional
release status; neither the migration rename nor source review promotes the
feature or creates another cloud test owner.

## Bounded setup authority and activation checks

The earlier proposal permitting non-root KMS creation/policy editing, human
fixture-role assumption and optional permissions boundaries is superseded and
must not be executed. It was rejected during setup-policy review. The operative
route is the owner-created key/default policy, immutable fixture boundary and
constrained setup managed policy described above. The October 18 authorization
for non-root setup covers these controls; no insecure manual fallback remains.

| Principal and phase | Exact authority | Required constraint |
|---|---|---|
| Owner MFA console bootstrap | Create one key/alias, two managed policies and one setup user/login | Review the default key policy, final policy JSON and exact resource inventory before creation; no root programmatic credentials. |
| Setup self-service | Own password change, own virtual MFA creation/enrollment/resync and MFA inventory; official same-device sign-in | Exact setup user/device ARNs where supported, absolute setup expiry; these grants do not authorize fixture mutations without MFA. |
| Setup inventory | Read the eight exact roles and their inline/attached policy metadata; list roles/providers; inspect exact key | Fresh MFA and successful MFA-gated read-only probe; inspect effective grants and boundaries, not only inline policies. |
| Setup role creation/configuration | Create and configure only the eight exact fixture roles | CreateRole requires the owner-installed exact boundary ARN. No PassRole, managed-policy attachment, user/key creation or boundary/policy-version modification. |
| Setup issuer | Create/read/tag the exact GitHub OIDC provider; delete only if owned by this run | Exact approved audience and immutable subject in all seven workload trusts; independently compare returned trust documents because the boundary cannot constrain their JSON text. |
| Setup KMS | DescribeKey, GetKeyPolicy, GetKeyRotationStatus; ScheduleKeyDeletion | Exact owner-created key in eu-central-1, MFA, setup expiry, deletion window exactly seven days. No CreateKey, PutKeyPolicy, aliases, grants or cryptographic use. |
| Runtime session issuance | Seven OIDC roles; source assumes only the eighth writer role | Exact provider/audience/immutable subject and expiry; source-to-writer external ID. Setup is explicitly denied role assumption. |
| Retirement | Delete the exact fixture role policies/roles and run-created issuer; owner retires setup identity and unused policies/alias | Only after reconciled synthetic cleanup, within approved expiry; do not delete pre-existing resources or detach the boundary from a live role. |

The boundary permits only the approved namespace, exact KMS key and
source-to-writer role transition. Per-role policies narrow that ceiling; only
the oracle may read synthetic values and only cleanup may force-delete. The
setup user cannot alter the boundary or KMS policy to escape those limits.
Resource/session grants still require independent inspection. See
[AWS boundary semantics](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_boundaries.html)
and [default key policy](https://docs.aws.amazon.com/kms/latest/developerguide/key-policy-default.html).

Resolve and validate every final JSON artifact before activation. Setup expiry
is at most four hours from activation; runtime mutation lasts at most two hours,
with cleanup at most two hours beyond that mutation deadline. Check all eight
role names are absent or already match the approved ownership, boundary, policy
and trust; inspect the key policy and alias separately. Record final hashes and
returned nonsecret metadata. The repository's runtime templates are insufficient
to reconstruct missing private setup grants: if the reviewed concrete setup
artifacts are unavailable, stop and recover them instead of inventing grants.

The reviewed setup policy supports own-user MFA enrollment, but actual enrollment
and the resulting CLI MFA context remain execution checks. A successful
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
