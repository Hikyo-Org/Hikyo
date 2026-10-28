# AWS sandbox account and fixture provisioning plan

Source-only plan for #158. Signup is owned by the parent. Account creation does
not authorize fixture provisioning, paid upgrades, or test writes. Parent has
reported account 099256073061 and a later lost console session. These are parent
observations, not independent verification or an approved test destination.
No credential, resource ARN or successful fixture provisioning is asserted here.
The execution contract and policy templates are in [aws-sandbox.md](aws-sandbox.md).

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
- Choose one normal commercial region. Proposed: `eu-west-1`, subject to the
  owner's scope and actual availability. Do not enable additional regions or
  provision workloads as part of signup.

These identity recommendations follow
[AWS root security guidance](https://docs.aws.amazon.com/IAM/latest/UserGuide/root-user-best-practices.html)
and [IAM temporary-credential guidance](https://docs.aws.amazon.com/IAM/latest/UserGuide/best-practices.html).

## Proposed resource bill of materials

All entries below require the subsequent concrete fixture authorization.

| Resource | Proposed bound and name | Purpose / end state |
|---|---|---|
| Customer KMS key | One symmetric encrypt/decrypt, single-region key; alias `alias/hikyo-158-sandbox` | Required customer-key positive/negative tests. No rotation, replica or custom key store. Schedule deletion only after all synthetic secrets are gone and deletion is approved. |
| Fixture IAM roles | Eight: `hikyo-158-static`, `hikyo-158-ambient`, `hikyo-158-assume`, `hikyo-158-web`, `hikyo-158-kms-denied`, `hikyo-158-source`, `hikyo-158-oracle`, `hikyo-158-cleanup` | Five writers, separate AssumeRole source, separate synthetic reader and separate cleanup. No broad shared test admin. Remove fixture-specific policies/trusts after evidence/cleanup. |
| OIDC provider | At most one, only if the approved execution path needs it | Proposed GitHub Actions issuer; exact repository/environment subject and audience, never a repository wildcard. No EKS cluster or new identity service. |
| Secret namespace | One random `hikyo-e2e-<run-id>/` prefix | Current drafts intend 31 nominal creation attempts before replacement retries. Proposed full-acceptance ceiling: 32 total creations per approved run, including replacements and retries. The draft shares a cumulative reservation counter across the full suite; compile and verify it before execution. |
| Compute/network/storage | Zero AWS compute, VPC endpoints, NAT gateways, clusters, buckets or deployments | Run Hikyo and both databases on the allocated existing local/runner resources. |
| Billing observation | Free-plan balance/expiry and existing billing views | Proposed USD 1 gross-usage ceiling, including credit-funded usage; monitor separately. No claim of an instantaneous billing hard cap. |

The current draft uses one suite entry point and a shared 32-attempt creation
counter, including failed calls, retries, replacements, and cleanup seeding.
A persistent single-use namespace marker prevents a fresh process in the same
protected fixture directory from resetting that budget. This is not a
cross-directory or AWS-enforced cap. Independently verify the local guards and
reconcile the inventory before relying on this proposed limit. Account signup
grants no approval for any number of these resources.

Tag supported resources with `Project=Hikyo`, `Purpose=AdapterAcceptance`,
`Issue=158`, `Environment=Nonprod` and the approved run/reference. Names and tags
are metadata only. Keep all synthetic payloads and actor credentials out of them.

## Ordered provisioning proposal

1. Parent completes signup/security setup and supplies nonsecret account/plan
   metadata. The #158 owner independently binds the proposed manifest to that
   account. Stop on an account mismatch or incomplete authority.
2. Present the final account, region, resource names/count, run and cleanup
   windows, expected cost, secure execution route, and deletion scope to the
   parent for the missing resource/spend authorization. No application or test
   write occurs during that decision.
3. Authorized setup identity creates the single KMS key, constrained policies,
   exact role trusts and optional OIDC provider. Resolve the existing JSON
   templates against the new account. Do not replace an account-wide key policy
   or attach administrator policies to test actors. Review the final substituted
   JSON and effective permissions before assuming any fixture role.
4. Populate static-mode descriptors with temporary STS role sessions, not IAM
   access keys. The static product auth mode can consume temporary credentials.
   Give AssumeRole its distinct source role and exact external ID. Ambient and
   explicit WebIdentity require authentic projected tokens and distinct roles.
5. Verify all eight identities, effective scopes and KMS restrictions using the
   approved inspection path. Local tests must compile/pass first. Only after
   the parent releases the real-write hold should the suite create synthetic
   secrets. Keep the writer, reader and cleanup identities separate throughout.
6. Verify exact-ARN cleanup, independently reconcile any ambiguous creates,
   then revoke trust/session issuance and remove fixture IAM resources as
   authorized. Never sweep a prefix whose ownership was not established.

The template set covers **runtime actor** permissions, not unrestricted account
provisioning permissions. The setup identity is separately authorized and is
never passed into the adapter. IAM resource creation and KMS key creation are
account writes even when they do not themselves consume billable usage.

## Secure token delivery options to resolve

Preferred proposal for authentic WebIdentity is a reviewed, manually dispatched
GitHub Actions job on an exact approved commit, protected by a dedicated
sandbox environment. Trust `token.actions.githubusercontent.com` with audience
`sts.amazonaws.com` and the exact environment subject. Restrict the environment
to the intended protected branch and approvals. Do not run untrusted PR code
with `id-token: write`; avoid `pull_request_target` as a delivery shortcut.
This workflow/environment/provider is not yet authorized or created.
[GitHub's AWS OIDC guidance](https://docs.github.com/en/actions/how-tos/secure-your-work/security-harden-deployments/oidc-in-aws)
describes the issuer, audience and subject constraints.

Each externally obtained session/token must stay within the trusted runner and
protected files. A fresh OIDC token must be available when a new client retrieves
credentials; a single captured short-lived JWT may expire during this suite.
Plan an approved token-refresh projection or bounded fresh-token test segments.
Do not send JWTs through chat, artifacts, workflow outputs, or CLI arguments.
No additional AWS compute is needed for this path.

A local run instead requires a supported, approved local federation/token
projection path. The advanced signup guide documents `aws login` for AWS CLI
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
period. Propose seven days for this disposable key only, subject to explicit
deletion authorization; scheduled deletion is not immediate deletion. AWS says
keys scheduled for deletion are not charged, but cancelling deletion restores
charges as though it had not been scheduled. Capture the key's pending-deletion
state and date, then arrange final verification without maintaining an owner
polling loop. Sources: [KMS deletion](https://docs.aws.amazon.com/kms/latest/developerguide/deleting-keys.html),
[KMS pricing](https://aws.amazon.com/kms/pricing/).

The account may remain available for subsequent explicitly scoped acceptance
runs. Account closure, paid upgrades, extra regions, extra services, and new
resource batches remain separate decisions. #158's real-provider proof and
formal-review obligations remain unchanged; #79 does not duplicate AWS tests.


## Current concrete setup handoff, source only

| Item | Known state and next evidence |
|---|---|
| Signup/account | Parent reported 099256073061 under the owner-authorized AWS signup. Recover the existing account; do not sign up again. |
| Root MFA/session | Parent reported session lost and private MFA enrollment still pending. Root owns ordinary-browser links and private enrollment. No root access keys are requested. |
| Account plan | Actual Free/Paid plan, eligibility, credit/expiry and upgrade restrictions remain unverified. Read these through the root-owned setup flow before pricing or resource authorization. |
| Region | Console default eu-north-1 was observed; it is not fixture approval. Choose and record one region explicitly before substituting policies. |
| Setup authority | Non-root MFA-protected setup identity and its scope are not established. Do not put administrator or root credentials into any test descriptor. |
| Fixture scope | One KMS key, eight isolated principals, at most one OIDC provider, zero AWS compute/network/storage; 31 nominal secret creations with the unchanged 32-attempt ceiling. Still a proposal, not authorization. |
| Credential custody | No supported local private injection/projection bridge has been established. Do not request secrets until that path is concrete. Root/browser signup does not populate local test credentials. |
| Destructive cases | Future approval must expressly include exact-ARN synthetic replacement and final force deletion, retained and scheduled-delete canaries, failed/ambiguous create reconciliation and bounded cleanup window. No prefix-wide sweep. |
| Runtime | The provider-only micro passed 56 tests; no full-suite lane is currently granted. Canonical generation waits for the signed migration chain and a separate lane. No capacity or cloud-write grant is implied. |

No current setup action is assigned to the #158 browser: root retains that
ownership. Parent and #79 consume the recorded reservation and conditional
release status; neither the migration rename nor source review promotes the
feature or creates another cloud test owner.

## Bounded setup authority and retirement proposal

The runtime templates do not grant account setup. The concrete setup change set
must substitute the approved account, region, eight role names, issuer, trust
principals, expiry, exact final JSON hashes and one key-creation request. A
non-root MFA-protected setup identity is still missing. MFA enforcement must fit
its actual session/federation mechanism; do not assume every federated session
carries `aws:MultiFactorAuthPresent`. No setup credentials enter the fixture.

| Phase | Proposed narrow authority | Explicit constraint |
|---|---|---|
| Read planned roles | `iam:GetRole`, `iam:GetRolePolicy`, `iam:ListRolePolicies` | Only the eight exact proposed role ARNs. Inspect attachments/boundaries and effective policy separately before runtime; do not infer absence from inline policies. |
| Create/configure roles | `iam:CreateRole`, `iam:UpdateAssumeRolePolicy`, `iam:PutRolePolicy`, `iam:TagRole` | Only those exact ARNs, reviewed trust/inline-policy JSON and short setup expiry. No `iam:PassRole`, broad managed-policy attachment, user/access-key creation or service-linked roles. |
| Create one key | One separately controlled `kms:CreateKey` request and required tagging authority | Approved region, `SYMMETRIC_DEFAULT`, `ENCRYPT_DECRYPT`, `AWS_KMS`, MultiRegion=false, approved tags, lockout safety check retained. Record the returned ARN and revoke creation authority immediately. |
| Inspect/configure that key | `kms:DescribeKey`, `kms:GetKeyPolicy`, `kms:PutKeyPolicy`, and optional `kms:CreateAlias` | Exact recorded key and approved alias only; independently review the key policy and exact runtime role/ViaService/context grants. No replica/custom-store/grant-management operations. |
| OIDC, only if chosen | Create/read one exact approved issuer provider with exact audience | The workflow/projection route and issuer must be approved first; no EKS or additional identity deployment. Record provider ARN for later exact removal. |
| Issue temporary actors | `sts:AssumeRole` to static/source/oracle/cleanup/KMS-denied roles | Exact non-root human trust and short session duration; the AssumeRole writer trusts only its source and external ID; workload roles trust only their exact OIDC audience/subject. |
| Retire | `iam:DeleteRolePolicy`, `iam:DeleteRole`, optional exact OIDC removal; `kms:ScheduleKeyDeletion` | Separate short-lived retirement approval, exact recorded ARNs, after verified synthetic cleanup. Key deletion requires its own seven-day waiting-period approval. No account closure or region sweep. |

IAM role names and expiry alone cannot constrain the content of PutRolePolicy
or trust changes. That setup authority remains escalation-capable. The minimal
manual proposal keeps those mutations with the independently reviewed non-root
setup operator and revokes them before runtime. An automated delegated setup
would instead need independently installed per-actor permissions boundaries
that it cannot change, with role creation conditioned on exact boundary ARNs.
Those extra managed policies are not hidden in the eight-role bill of materials:
they require a revised explicit resource inventory and approval if that route is
chosen. Boundaries constrain identity permissions but do not replace inspection
of direct resource/session grants. See [AWS boundary semantics](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_boundaries.html).

CreateKey cannot be constrained to its not-yet-existing key ARN or enforce a
one-call quota by itself. A scoped setup operator must execute the one approved
request and retire that permission; region/type/tag restrictions are defense in
depth, not a resource-count cap. See [CreateKey](https://docs.aws.amazon.com/kms/latest/APIReference/API_CreateKey.html)
and [KMS permission conditions](https://docs.aws.amazon.com/service-authorization/latest/reference/list_kms.html).
The final grant JSON cannot be truthfully instantiated before the actual setup
identity and approved destination exist. This table defines the required actions
and their limits; it grants none of them.

Secure delivery must supply fresh inputs plus the four expired sidecars and their
prior same-role/issuer provenance. A reviewed trusted-runner projection keeps all
material in private files, never artifacts/chat/argv, and refreshes projected
tokens as needed. A local private injection bridge is still unestablished.
Neither a newly created account nor an opaque secret reference resolves that
bridge. No credentials are requested by this plan.
