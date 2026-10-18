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
`eu-central-1`, prefix `hikyo-e2e-20261018a/`, one customer KMS key and the eight
exact fixture roles. Effective role grants and absolute policy deadlines must
match the approved policy templates. No root or setup identity enters the
runner or tests. The complete suite shares one ceiling of 32 CreateSecret
reservations, including retries and failed attempts, and the approved gross
usage target is USD 1. The recorded budget is not an instantaneous billing cap.

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

Configure these eight variables on the protected environment. They must match
the reviewed candidate and effective AWS policies. Never store credentials,
JWTs or private descriptors in them.

| Variable | Required value |
| --- | --- |
| `AWS_ACCEPTANCE_APPROVED_WORKFLOW_SHA` | Full reviewed `main` commit that supplies the workflow and runner. |
| `AWS_ACCEPTANCE_APPROVED_SHA` | Full independently reviewed candidate commit. |
| `AWS_ACCEPTANCE_APPROVED_RUN_ID` | Exact pending workflow run ID, set before human environment approval. |
| `AWS_ACCEPTANCE_AUTHORIZATION_ID` | Approved scope reference, 1 to 128 characters from letters, digits and `._:/-`. |
| `AWS_ACCEPTANCE_AUTH_EXPIRES_AT` | UTC ISO timestamp matching mutation policies; 55 to 120 minutes after startup. |
| `AWS_ACCEPTANCE_CLEANUP_EXPIRES_AT` | UTC ISO timestamp matching cleanup policies, from mutation expiry to at most two hours later. |
| `AWS_ACCEPTANCE_KMS_KEY_ARN` | Exact approved single-region customer key ARN in the sandbox account and region. |
| `AWS_ACCEPTANCE_EXTERNAL_ID` | Exact external ID shared by the source policy, AssumeRole trust and descriptor. |

Choose imminent activation times with room for candidate compilation, genuine
session expiry preparation and the 30-minute test deadline. Recheck the exact
policy deadlines and matching variables immediately before approval. An
exhausted window is a refusal, not a reason to extend authority automatically.

## Human approval is the final execution step

After local validation, independent security review and effective fixture
policy verification pass, dispatch `aws-acceptance` on `main` with its required
`reviewed_sha` input set to the exact reviewed candidate commit. The job must
wait for its environment reviewer. Inspect the pending run, set its exact
`AWS_ACCEPTANCE_APPROVED_RUN_ID`, and verify the two SHA pins, fixture scope,
deadlines and environment protections. The human owner then approves that
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
synthetic creation reservations, validated complete ARNs, verified absences and
bounded unresolved names. Success requires every expected case, both engines,
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

After reconciliation, the owner verifies exact synthetic secret absence,
retires the fixture IAM roles/trusts, removes the dedicated CI access and the
created OIDC provider, and schedules the dedicated KMS key's deletion with a
seven-day waiting period. Pending KMS deletion must be reported as pending.
These operator tasks are outside this runner.

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
