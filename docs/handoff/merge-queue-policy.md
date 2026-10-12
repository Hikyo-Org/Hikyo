# Mandatory merge queue policy handoff

This follow-up adopts the protected queue configuration already activated after
bootstrap PR #891. It changes canonical policy, local policy/configuration
fixtures and contributor/operator documentation. It also corrects the queue
publisher's exact recognition of GitHub's observed normalized skipped metadata
job name. Queue publication now also uses bounded per-head writers and complete
read-only maintenance discovery. Application code, the job registry and the
frozen full-suite coverage remain unchanged.

## Contract

- `ci-queue-required` admission binds the exact current PR head to useful hosted
  feedback and current source authority. Admission authorizes enqueueing only.
- The mandatory merge queue requires full current-base group proof across all
  16 domains before merging. Rebuilt or reordered groups need fresh proof.
- Queue parameters are `ALLGREEN`, build concurrency one, merge limits one,
  zero minimum wait, `MERGE`, and a 60-minute check-response timeout. Strict
  branch-up-to-date checking is false only with that mandatory queue/context.
- Signatures, DCO, stale-review dismissal, resolved threads and zero owning
  bypass actors remain required. Fork vouch/run approval remain independent.
- `ci-required-next` and `ci:full` remain the permanent full-only rollback route.
  Queue admission never writes that context, so old admission greens cannot
  satisfy restored strict legacy protection.

## Local configuration

Run `scripts/release/configure-repository.sh --main-only Hikyo-Org/Hikyo` only
with operator authorization and administrator access. It reads the unique
existing main ruleset, validates its queue or legacy contract, preserves every
unrelated live rule/PR parameter, atomically replaces only the queue/status-check
rules, and verifies writable readback, treating absent/null unspecified integration
bindings equivalently while retaining every explicit integer binding. Discovery
validates each 100-result page and refuses before any PUT if still incomplete
after 20 pages. It refuses missing or redacted
actor policy, duplicate/absent owning rulesets, malformed JSON, weakened policy,
API errors and readback drift. It never changes tag/release settings, CodeQL,
Actions permissions or git refs. Broad initial configuration retains those
existing responsibilities and can still create the initial main ruleset.

The canonical legacy JSON is a reference. Actual recovery uses the exact saved
pre-transition legacy writable policy, not a newly synthesized approximation.
The CI guide contains the validated capture, restore and equality commands.

## Local validation

The initial complete CI tooling Go package passed (25.338 seconds). The expanded
policy/configuration regression check passes (4.875 seconds), including 32 actual
queue-policy mutations, five malformed legacy-policy cases and 20 mocked
main-only API scenarios. Vet and ShellCheck pass. The existing CodeQL setup and
nightly/release preservation fixtures pass. Pinned documentation diagnostics
report zero errors, warnings or hints; the site builds successfully and the
actual OSS policy fixture passes its positive controls and refusals.

These checks validate the local follow-up, not hosted populated-group acceptance.
No application-wide suite was repeated locally, and no policy mutation or CI
run was dispatched by this follow-up preparation.

## Review follow-up

Five new source-bound checks fail on original head `3423e328c`: a valid positive,
zero or null integration binding, GitHub's nullable readback representation,
and a later-page main owner. They pass after preserving the binding and fully
validating bounded discovery. Cross-page duplicates, malformed/incomplete pages
and changed concrete bindings remain refused. The API fixture now asserts the
ordered list-page(s), owning GET, atomic PUT and final GET sequence.

Main publication also encountered GitHub's normalized spelling of a single
completed/skipped metadata-only matrix job. Architecture reproduced that exact
job name from run 38233557251, then added only its exact canonical spelling to
the existing ignore list. The regression still refuses unknown names, different
events, unfinished/failed/successful jobs and duplicate metadata jobs; useful
feedback and full group proof remain necessary. Its race test passes (6.087s).

## Evidence and remaining hosted acceptance

Bootstrap main is `21773f333b4c37cac61b650baf2bf2c154263e9a`.
The actual minimal trusted-main workflow-token active-empty-queue probe
[38230781293](https://github.com/Hikyo-Org/Hikyo/actions/runs/38230781293)
passed. Protected owning/effective policy and empty queue were captured before
and after. Success follows the forced active-policy `repairGroups -> liveQueue`
path; an inactive workflow success or administrator API request is not the
same capability proof. Redacted bypass actors are explicitly unobservable to
the workflow token; the administrator separately verified zero live actors.

This legitimate policy/docs PR is the first populated hosted acceptance.
Actual event/ref, group/member mapping, all 16 domains, trusted required proof,
landed tree and post-merge protection still require verification. Empty-queue
capability and local fixtures do not prove those outcomes.

No tests or fast checks are removed. Required benchmark compilation/smoke remains
in the complete graph; optional paid performance measurement has a separate
budget and does not become a required paid job. Local hooks are not substitutes
for remote exact-group proof. Build concurrency one does not reserve hosted
runner capacity or guarantee five-minute feedback, and cold caches or group
rebuilds can still cost time.

## Entry points

- [CI operator guide](../development/ci.md)
- [Contributor guide](../../CONTRIBUTING.md)
- [Canonical queue policy](../../release/repository/main-ci-gate.json)
- [Legacy reference policy](../../release/repository/main-ci-gate-legacy.json)
- [Historical bootstrap handoff](trusted-merge-queue.md)
- [GitHub merge queue semantics](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-a-merge-queue)

## First full group: documentation dependency repair

The first actual queued group `334453f5cc3bfc407116b43794c5cf1bb090323c`
ran in [38239116880](https://github.com/Hikyo-Org/Hikyo/actions/runs/38239116880).
Its docs job failed because the new shared policy validator required `jq`,
which is absent from the pinned Playwright container. The PR was dequeued and
the superseded group cancelled; this is a real dependency failure, not full
suite success. Protections stayed active and no tests were removed.

Both full CI and Pages already call `scripts/ci/verify-docs.sh`. That explicit
bootstrap now provisions missing `jq` through Ubuntu APT only in the recognized
root GitHub Actions Ubuntu 24.04 container, before Corepack and verification.
Local, unprivileged or unsupported setups must install `jq` first and otherwise
fail with the prerequisite. The policy validator remains pure: no package
installation, network access or system changes occur during policy checking.

Twelve permanent source-bound bootstrap cases pass (4.401s). They exercise
preinstalled local tools, recognized provisioning, independent CI/GitHub flags,
root/container/distro boundaries, APT update/install failure, missing install
result, and the pure validator's no-install behavior. Independent mutations
reject removal of the root/container guards and swallowed update failure.

The complete `verify-docs.sh` invocation then passed in a clean container with
exact Playwright digest
`sha256:dcc5531e97840b9b5e794f2814476b21571c5124a3fca2267d73041f56e7580e`,
official checksum-verified Node 26.7.0 and the actual public docs configuration.
APT provided `jq` 1.7.1-3ubuntu0.24.04.2. Diagnostics reported zero errors,
warnings or hints; build, docs/PWA browser checks, PostHog/CSP, OSS mutations,
live-page and fallback-notification fixtures passed. This local container proof
does not replace new-head useful feedback or a fresh full queued-group run.

## Linear-history compatibility refusal

Review finding 4247465002 exposed an incompatible preservation fixture: the
operator path retained `required_linear_history` while adopting the fixed
`MERGE` queue method. Linear history disallows merge commits; see
[GitHub's rule contract](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets).

Three new source-bound regressions reproduce acceptance on original head
`69e8ffd92`: direct queue validation and actual operator reconciliation from
legacy and queue policies containing the rule. Queue-mode validation now
refuses the combination before any PUT. A valid legacy policy with linear
history remains accepted as a source; its attempted conversion refuses without
removing protections or changing the configured merge method. The compatible
preservation-positive fixture instead retains a branch commit-message rule
blocking `[skip ci]`, plus its stricter two-review requirement. Existing
preservation, app-binding, pagination and malformed-policy refusals remain.

This policy-only repair leaves the docs bootstrap and exact-container proof
unchanged. Fresh useful feedback and full queued-group proof remain required.

## Per-commit deadlines and head ownership

The previous queue publisher allowed ten minutes after a four-minute feedback
job, and its repository-wide concurrency could hold a new commit behind a
maintenance sweep. The new targeted publisher job has a one-minute total limit.
Its first step authenticates the current run attempt and exact named job's
server `started_at` before checkout, setup and compilation. Controller work ends
45 seconds after that anchor; an independent reserve ends at the same absolute
60-second deadline for fresh identity checks and a blocking terminal write.
Neither setup nor a later callback starts a new clock inside the same invocation.
Git, policy and GitHub subprocess cancellation kills their process group and
bounds pipe waits, including a child that retained stdout after its parent died.

PR events and useful-feedback callbacks route directly to a writer using the
event's head SHA as a lock hint. Full-group callbacks use their group SHA. Every
writer, including maintenance workers, shares the same repository-ID/head-SHA
concurrency key with cancellation disabled. The event hint grants no authority:
current API evidence must authenticate the head, source attempt, policy and role.
Every check mutation verifies the held head, binding head and selected owned check.
Fresh initial and final snapshots refuse an ambiguous PR/group role on one SHA.
Head movement never authorizes a write to the replacement head. Pure stale work
may end benignly; an error joined with a transport failure remains a failure.

Read-only main/schedule/manual discovery has no check-write permission. It fully
validates up to 2,000 open PRs and 2,000 queue entries before dispatching any work.
It coalesces the union into at most 4,000 distinct heads and batches at most 64
records per fixed default-branch worker invocation. Each POST has a five-second
bound and a typed authenticated receipt. All batches are attempted once despite
an earlier failed or ambiguous receipt, with explicit failed head inventories
and a failing aggregate. An authenticated schedule-time rotation varies the
starting batch; manual target priority retains the remaining group work. These
bounds prevent silent truncation; persistent platform errors still prevent
completion and are reported, without a mutation retry or a fairness guarantee.

Workers can write checks but cannot dispatch workflows. Their fresh head/role
checks use the same common mutation guard as direct writers. An owned incomplete
blocking barrier is reused; identical reassessment does not add a second generic
barrier. Completed proof is never treated as current authority without validation.
Maintenance and full-group jobs retain their separate ten-minute budgets. No
selected feedback command, full-suite test, domain, baseline or registry entry
is removed. Bot review duration is separate from the CI-only eligibility metric.

The four-minute feedback job plus the one-minute targeted publisher allowance
bounds controlled execution, not all GitHub elapsed time. Runner queues, API
outages and time waiting for authority can delay eligibility. Setup/compilation
failure before the controller starts is a native job failure and cannot promise
an owned terminal check publication. A failed terminal API write remains red;
there is no timeout-to-green fallback. Old useful feedback can remain valid
under fresh authority during a later bounded reassessment. Its reported age is
age at that reconciliation, not proof of push-to-first-eligibility latency.

The integrated publisher package and vet pass locally. Permanent tests exercise
server timestamp/provenance refusals, cancellation after actual child readiness,
in-progress feedback expiry, reserve transport failure, head/base/policy/attempt
movement, foreign checks, barrier reuse, a complete 4,000-head union and partial
dispatch failures. Independent source tests also reproduce cancellation and the
real `gh` HTTP/version/structured-payload boundary. The prior pinned-container
docs proof remains valid for its unchanged bootstrap. These are local proofs;
fresh hosted feedback, token dispatch, same-head serialization and the complete
populated queued group still require exact-head acceptance.

All new jobs are disabled until `QUEUE_HEAD_WORKERS_ENABLED` is enabled through
the reviewed cutover. Drain older compiled unleased publishers before enabling
the direct and worker paths. No variable, policy or queue operation is performed
by this source preparation. The legacy full-only publisher remains independent.

- [Direct and discovery publisher](../../.github/workflows/ci-queue-gate.yml)
- [Fixed head worker](../../.github/workflows/ci-queue-worker.yml)
- [GitHub concurrency contract](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency)

## Recurring check history and guarded API lifecycle proof

Review finding 4250549208 reproduces recurring history growth: unchanged PR
reassessment adds a check, group reassessment adds four, and supported histories
at 1,000 PR checks or 100 group checks fail on the next pass. The guarded queue
path now reuses the newest authenticated owned ID for a pending barrier and its
freshly re-proved result. Complete `filter=all` ownership and collision scans,
their existing bounds, head leases and final source/member barriers remain.
Older native same-name collisions remain blocking. Unknown or already excessive
history is refused rather than silently skipped. No check is deleted and no
history limit is increased. The separate legacy producer retains its lifecycle.

Local permanent tests pass more than 1,100 unchanged reassessments for each role
with constant check count, including the existing history boundaries. Pending
feedback and transient fork geometry remain pending. An uncertain reset never
retries its PATCH; a short independent reserve, capped by the original targeted
deadline, attempts one distinct blocking pending barrier and retains the error.
The callback cannot turn cancellation into a green result.

The actual GitHub completed-to-queued reset remains unproved. An authorized
uniquely named non-required local canary request was refused with HTTP 403 because
that token was not a GitHub App; it created no check and proves no lifecycle
constraint. The new fixed [trusted-main lifecycle workflow](../../.github/workflows/ci-check-lifecycle-probe.yml)
uses the actual workflow App token, no caller inputs, and current immutable main.
Its CLI authenticates run/attempt, native job start, main and owning/effective
policy before mutation, including that its fixed canary name is not required.
Normal work ends at job-start plus 120 seconds; cleanup is independently bounded
inside plus 150 seconds, leaving margin within the three-minute native job.

The single diagnostic ID starts completed/success, receives the same production
queued payload builder with conclusion/completed_at omitted rather than null,
and must read back as queued with no success conclusion. Its actual timestamps
are retained in the receipt. The known owned ID is then restored to completed/
neutral and independently read back. Lost or foreign creation identity is not
guessed; uncertain reset writes are not retried. Any reset or cleanup refusal
leaves the hosted acceptance unmet. This non-required canary supplies no test,
admission or merge proof. See the [GitHub Checks API](https://docs.github.com/en/rest/checks/runs#update-a-check-run).

Landing is deliberately staged. `QUEUE_HEAD_WORKERS_ENABLED` remains unset/false
until a successful exact-main lifecycle run, its trusted run-attempt artifact,
and actual same-ID neutral cleanup are verified. Old workflow definitions loading
new code lack an expected-head lease and refuse before API writes; already
compiled old code has no new reset behavior. All new production queue jobs are
skipped while disabled, and the legacy full-only publisher cannot invoke queue
reuse. The temporary publication hold is accepted, with queue protection active
and no parallel legacy queue writer. Drain/empty-queue and current-source checks
remain necessary before enabling. Source tests cannot substitute for this proof.

The whole publisher package passed in 3.348 seconds and vet locally. Independent adversarial
source tests cover canceled reset transport, actual workflow naming, required
canary refusal and historical native collisions. Fresh hosted feedback, all 16
full-group domains, lifecycle acceptance, head-worker activation and timing still
require verification. Observed feedback for earlier c95 passed in 82 seconds,
while its old deployed global publisher took 6m 59s to finish admission. A separate
actual full group ran 40m 39s, exceeding the approximately 35-minute target. Neither
measurement proves the new disabled pipeline meets the requested timing goals.

Race weights are recalibrated from all twelve JSON timing artifacts of successful
full group 38279650054, attempt 1. The twelve-shard count, packing, complete target
discovery and database serialization are unchanged. The source snapshot contains
800 planner units, including whole-package units; this is not a test count.
The measured-cost model maximum improves from 985.78 to 753.51 seconds. This model
is not hosted latency proof. PR #893 bootstrap copies the planner from its
immutable trusted `BASE_SHA`, so it exercises the old placement. The new weights
are exercised after landing; a later legitimate full group must establish the
approximately 35-minute target.
