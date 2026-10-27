# AWS acceptance runner bootstrap

This manual runner executes the reviewed #158 real-provider fixture. It does
not provision IAM, OIDC, KMS or billing resources. Local checks, security review,
fixture provisioning, human environment approval and real-provider results are
separate gates. A source review or successful offline test is not AWS acceptance.

## Bootstrap the trusted workflow

1. Prepare a narrow, reviewed bootstrap PR containing
   `.github/workflows/aws-acceptance.yml` and this directory. Use the repository's
   signed and DCO-signed commit workflow. Merge that PR through the authorized
   human merge gate. GitHub requires the dispatch workflow to exist on the
   default branch before it can run. The bootstrap merge does not close #158.
2. Review and validate the exact candidate commit containing the real fixture.
   Its full SHA is independent of the trusted workflow's current `main` SHA.
   The workflow checks out that exact candidate into `acceptance-candidate`.
   Candidate execution remains held until the approved fixture is ready.
3. Create the dedicated environment `hikyo-158-aws-sandbox` with a required
   human reviewer. For the approved owner-operated workflow, use
   `prevent_self_review: false` so the legitimate owner can approve a run they
   dispatched. Disable administrator bypass and permit deployment only from
   the `main` branch. Verify the actual resulting protections before enabling
   federation. No preexisting environment protection is weakened by this setup.

The workflow has no PR trigger. It checks the repository, manual event, `main`
ref, trusted workflow identity, hosted runner and first run attempt. The
protected environment independently pins the workflow SHA, candidate SHA and
single run ID. A rerun or a new dispatch cannot reuse that run authorization.

GitHub's default-branch dispatch requirement is documented in
[Manually running a workflow](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow).
The environment approval controls are documented in
[Deployment protection rules](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments#deployment-protection-rules).

## Verify the bounded AWS fixture

Use only the separately approved sandbox account `099256073061`, region
`eu-central-1`, a separately approved fresh run prefix, one customer KMS key and
the eight exact fixture roles. Set `AWS_ACCEPTANCE_RUN_PREFIX` on the protected
environment to a whole value matching `hikyo-e2e-[a-z0-9]{8,32}/`. There is no
default or fallback namespace. Effective role grants and absolute policy
deadlines must match that exact prefix and the approved policy templates.
No root or setup identity enters the runner or tests. The complete suite shares
one ceiling of 32 CreateSecret
reservations, including retries and failed attempts, and the approved gross
usage target is USD 1. The recorded budget is not an instantaneous billing cap.

The earlier two runs consumed 21 and 27 conservative creation reservations,
48 combined. Their additional one-run authorization is consumed; unused room
under its historical ceiling of 53 does not authorize another run. The
`hikyo-e2e-20261018b/` namespace still has a secret PendingDeletion and must not
be reused. A configurable prefix is a runner capability, not new numeric,
resource, billing or execution authority. Every future run requires the owner's
explicit additional allowance, a new unused namespace, matching effective
policies and approval of its exact protected job. No recurring run, automatic
retry or paid upgrade is authorized. Actual gross billing remains unverified.

Keep the cumulative creation ledger across all namespaces. Changing the prefix
never resets or refunds earlier reservations. The runner enforces 32 reservations
within one run; the operator ledger enforces the separately approved lifetime
allowance. Verify that a prefix has never been consumed before provisioning or
approval, including failed, interrupted and ambiguous attempts. The candidate
keys its exclusive single-use marker by account, region and the approved prefix;
the runner preserves that marker after its suite exits. A fresh hosted runner
does not replace the operator's cross-run namespace and cumulative-ledger checks.

Use GitHub issuer `https://token.actions.githubusercontent.com`, audience
`sts.amazonaws.com` and only this approved immutable subject:

```text
repo:Hikyo-Org@316726515/Hikyo@1316165429:environment:hikyo-158-aws-sandbox
```

Seven roles require OIDC trust for that exact issuer, audience and subject:
`hikyo-158-static`, `hikyo-158-ambient`, `hikyo-158-web`,
`hikyo-158-kms-denied`, `hikyo-158-source`, `hikyo-158-oracle` and
`hikyo-158-cleanup`. The remaining role, `hikyo-158-assume`, trusts only the
exact source role and external ID. Do not add a legacy-subject fallback or
change the repository's OIDC defaults.

Allow 3600-second fresh sessions for static, source, oracle and KMS-denied
actors. Set the cleanup role's `MaxSessionDuration` to 14400 seconds so its
temporary session can cover the separately approved cleanup deadline. The
runner requests only the duration needed for that deadline plus 90 seconds,
bounded by four hours. IAM resource permissions still expire at their absolute
approved deadlines. Ambient and explicit WebIdentity descriptors request
900-second sessions; the AssumeRole descriptor also requests 900 seconds.

Before mutation, the runner authenticates genuine static/source sessions and
ambient/WebIdentity tokens, records their provenance privately, and waits past
their natural expiry. It then creates fresh actors and continuously projects
fresh workload tokens. AWS STS validates the signed tokens and role trust; the
runner's decoded-claim checks are preliminary refusal checks only. No JWT is
fabricated, re-signed or edited. See
[GitHub OIDC claims](https://docs.github.com/en/actions/reference/security/oidc)
and [AWS STS WebIdentity sessions](https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRoleWithWebIdentity.html).

## Set exact nonsecret environment variables

Configure these nine required variables on the protected environment. The
workflow pins historical cleanup to a literal empty value. Leave its environment
variable unset; organization variables cannot enable it. Values must match the
reviewed candidate and effective AWS policies. Never store credentials, JWTs or
private descriptors in them.

| Variable | Required value |
| --- | --- |
| `AWS_ACCEPTANCE_APPROVED_WORKFLOW_SHA` | Full reviewed `main` commit that supplies the workflow and runner. |
| `AWS_ACCEPTANCE_APPROVED_SHA` | Full independently reviewed candidate commit. |
| `AWS_ACCEPTANCE_APPROVED_RUN_ID` | Exact pending workflow run ID, set before human environment approval. |
| `AWS_ACCEPTANCE_AUTHORIZATION_ID` | Approved scope reference, 1 to 128 characters from letters, digits and `._:/-`. |
| `AWS_ACCEPTANCE_RUN_PREFIX` | Exact fresh namespace matching `hikyo-e2e-[a-z0-9]{8,32}/`, also bound in the effective policies and cumulative operator ledger. Required, with no default. |
| `AWS_ACCEPTANCE_AUTH_EXPIRES_AT` | UTC ISO timestamp matching mutation policies; 55 to 120 minutes after startup. |
| `AWS_ACCEPTANCE_CLEANUP_EXPIRES_AT` | UTC ISO timestamp matching cleanup policies, from mutation expiry to at most two hours later. |
| `AWS_ACCEPTANCE_KMS_KEY_ARN` | Exact approved single-region customer key ARN in the sandbox account and region. |
| `AWS_ACCEPTANCE_EXTERNAL_ID` | Exact external ID shared by the source policy, AssumeRole trust and descriptor. |
| `AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN` | Do not configure. The workflow supplies a literal empty string and skips historical cleanup. |

Choose imminent activation times with room for candidate compilation, genuine
session expiry preparation and the 30-minute test deadline. Recheck the exact
policy deadlines and matching variables immediately before approval. An
exhausted window is a refusal, not a reason to extend authority automatically.

## Retained historical-cleanup helper contract

The helper retains the original exact historical-secret contract below. The
current workflow always supplies an empty request and cannot enable this path
through repository, environment or organization variables. Re-enabling it would
require a separately reviewed workflow change, explicit owner approval and
matching cleanup-role permissions. This retained contract grants no live
execution or additional creation allowance:

```text
arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261018b/web-identity/postgres/role-document-vmN1Vj
```

If separately re-enabled, the operator must durably claim the historical
operation in the parent ledger, bind it to the exact pending GitHub run ID before human approval, and clear the
variable for subsequent runs. The runner does not enforce a global once token
across different hosted runners. There are no arbitrary ARN/prefix inputs or
cleanup CLI arguments.

After fresh authentication and genuine expiry preparation, this phase uses only
the already verified `hikyo-158-cleanup` role session. DescribeSecret must return
the exact ARN and name, a scheduled-deletion timestamp, and KMS key
`10908b8d-fe54-40e3-9210-a3eefea709b6` as that exact ID or its full regional/account
ARN. Strict ResourceNotFound from this exact read instead proves already absent,
with zero DeleteSecret calls. AccessDenied, unknown error formats, malformed
metadata and foreign resource/key fields all refuse.

If the scheduled object exists, an exclusive nonsecret intent tied to the run ID
and the separate evidence state are both fsynced, including their directories,
before one DeleteSecret with ForceDeleteWithoutRecovery. AWS CLI maximum
attempts is one. The delete response must return the exact ARN. Only then may
up to six read-only DescribeSecret calls, separated by two-second waits, verify
strict ResourceNotFound. Each call has at most a ten-second timeout and respects
cancellation and the earlier of the fresh mutation/cleanup deadlines. A timeout,
ambiguous delete, mismatched response or other refusal holds immediately,
without delete retransmission or post-ambiguity polling. The new suite never
starts on incomplete historical cleanup.

Known plain and `aws: [ERROR]:` CLI service-error formats are accepted only with
return code 254, empty stdout, the exact ResourceNotFoundException and
DescribeSecret operation, at most the literal zero-retry suffix, and at most
one LF or CRLF before and after the diagnostic. AWS CLI 2.38.1
[`write_error`](https://github.com/aws/aws-cli/blob/10b7b71c678dd01942a88071be7cddb683daa802/awscli/errorformat.py#L37-L41)
emits this framing; offline execution of its official `ClientErrorHandler`
confirmed exit 254, empty stdout and the framed exact diagnostic without an
AWS client/session or provider call. Repeated framing lines and other
formats refuse; raw stderr, response bodies and credential values are withheld.
The helper has no GetSecretValue, restore or KMS operation.

Evidence records this phase under `historical_cleanup`, separate from all 46
new-suite results, new reservations and new ARNs. `delete_attempts` is a durable
claim count, not proof that AWS accepted a request; `verified_absent` is required
for a requested cleanup before the new suite can pass. The current workflow
records this phase as not requested. The earlier failed artifact remains
immutable.
Neither historical deletion nor a fresh prefix refunds previous reservations.
The owner still reconciles the cumulative ledger and billing independently.

## Human approval is the final execution step

After local validation, independent security review and effective fixture
policy verification pass, dispatch `aws-acceptance` on `main` with its required
`reviewed_sha` input set to the exact reviewed candidate commit. The job must
wait for its environment reviewer. Inspect the pending run, set its exact
`AWS_ACCEPTANCE_APPROVED_RUN_ID`, and verify the two SHA pins, fresh run prefix,
matching effective resource scope, cumulative reservations, deadlines and
environment protections. The human owner then approves that
concrete pending job. Dispatching alone is not approval to execute the fixture.

The job starts isolated PostgreSQL 18 and compiles the candidate with private
0700 build-home, workspace, module cache, build cache and temporary directories.
Go processes receive explicit paths, `GOENV=off` and `-modcacherw` so their
private module caches can be removed afterward; they inherit neither host
credential stores nor Actions/AWS bearer material. The official AWS CLI receives
tokens by protected file reference. Private 0600 descriptors and tokens stay
outside the checkout; raw AWS and test output is not printed or uploaded.

Only `aws-acceptance-evidence/evidence.json` beneath the runner's temporary
directory is uploaded. It contains allowlisted test results, cumulative
synthetic creation reservations, validated complete ARNs, verified absences,
bounded unresolved names and structured diagnostics. Each `diagnostics` entry
contains only `test`, `operation` and `code`. The runner accepts only output
events from an exact expected test, with the anchored
`aws_real_diagnostics_test.go:<line>: synthetic AWS diagnostic: <operation> <code>`
marker and closed operation/code enums. It retains at most 128 entries and sets
`diagnostics_truncated` when another valid entry exceeds that cap. Unknown
tests, source files, operations, codes and extra text are discarded. Raw
provider messages, response bodies and credential values remain excluded;
existing per-line and total-output limits still refuse oversized output.
Diagnostics describe observed categories and do not establish causality for
other failures. Success requires all 46 expected cases, both engines,
the creation ledger and exact recorded-resource cleanup. All raw test2json
payloads are discarded after in-memory filtering.

The runner owns the entire suite process group immediately after creation.
Every later setup or evidence failure terminates and waits for that group and
closes its output pipe before releasing private files. Signals request
cancellation without throwing across process-handle assignment. Selector
cleanup cannot prevent child termination. If termination cannot be confirmed,
the run fails and private custody is retained for reconciliation.

On failure, interruption or ambiguous creation, retain this evidence and the
approved namespace for owner reconciliation. Do not redispatch, change the run
ID authorization or remove the single-use marker to reset the creation budget.
The runner never performs a prefix force-delete sweep. A provider write accepted
before its exact ARN was recorded requires independent provenance checks.

After each run, the owner reconciles exact synthetic secret absence and the
cumulative ledger. Retaining fixture access while #158 remains open must follow
the separately approved lifecycle and absolute deadlines; this runner cannot
extend them. Final retirement removes the fixture IAM roles/trusts, dedicated
CI access and created OIDC provider, and schedules the dedicated KMS key's
deletion with a seven-day waiting period. Pending deletion remains pending;
already scheduled keys are not restored by changing namespaces. These operator
tasks are outside this runner.

## Offline checks

From the repository root:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts/ci/aws-acceptance -p 'test_*.py' -v
actionlint .github/workflows/aws-acceptance.yml
```

The capability regression uses the installed Go compiler to build a tiny
temporary module with fresh private caches and network module resolution
disabled. Child-ownership regressions inject post-spawn setup failures against
harmless local sleeping processes, then verify group termination, wait and pipe
closure. The remaining checks use synthetic inputs and mocked provider calls.
None of these checks contacts AWS or establishes real-provider acceptance.

## Assertion diagnostics and schema compatibility

Runner evidence uses schema
`hikyo.aws-acceptance/v2`. All v1 fields and acceptance predicates remain.
The source-only hosted-result consumer validates v1 with its original exact
field set and v2 with exactly two additional required fields:

- `failure_assertions`: at most 128 exact `{test, assertion}` objects. Test
  names belong to the existing 46-test inventory; identifiers are the closed
  `assertion-001` through `assertion-230` mapping pinned to the fixture candidate.
- `failure_assertions_truncated`: an exact JSON boolean. Truncation indicates
  omitted categories and grants no success, cleanup or execution authority.

The runner holds only a closed identifier in bounded per-test memory until an
exact Go failure footer and terminal fail event confirm its framing. Split
multiline output, unknown framing, another log line, incomplete output and
interrupted streams discard the pending identifier. No raw message is saved.
This deliberately omits assertions followed by additional cleanup log lines.
An identifier is an observed category, not proof of its source or root cause.
An empty list does not prove that no assertion occurred; dynamic messages and
discarded child output remain unobservable. The static map does not recover
the four failures from the prior AWS run.

The v2 producer and consumer must be adopted together after the required source
disposition. The existing workflow uploads the same single `evidence.json` file.
Schema compatibility grants no AWS execution or permission to change a closed claim.

Identifiers 225 through 230 distinguish a nonconverged target's existing closed
error classes: auth, network, conflict, provider limit, provider ambiguity and
refused. The fixture selects a literal message from this finite vocabulary;
unknown or empty classes retain generic assertion 066. No target record, error
message, credential, new evidence field or additional execution authority is
included. Identifiers 001 through 224 keep their original meanings. The runner
and its source-only consumer must be published together before using these new
categories in a protected AWS attempt.
