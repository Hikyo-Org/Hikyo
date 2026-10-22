# CI guide

Every pushed PR commit receives automatic **ci-feedback**. The required
**ci-queue-required** check admits an exact PR head only after useful feedback,
signatures/DCO, fork vouch where applicable, and current review/workflow authority.
A maintainer then adds the reviewed PR to the mandatory merge queue. Admission
success permits enqueueing; it never replaces full-suite merge proof.

The queue runs all 16 mandatory domains on its immutable current-base merge
group. Trusted publication binds the exact group, current main, ordered members,
workflow, run and attempt before the group can merge. An unchanged PR head can
reuse successful useful feedback; rebuilt or reordered groups need fresh full
proof. The permanent **ci:full / ci-required-next** path is the full-only recovery
route when operators restore strict legacy policy.

## Choose a task

| What you need | Actions workflow | What it does |
| --- | --- | --- |
| Check each pushed commit | `ci-feedback` | Selected formatting, compilation, lint, package tests, workflow and docs checks on the exact PR head. Four-minute runner deadline includes setup; queue time is separate. |
| Refresh the bounded feedback compiler cache | `ci-feedback-backend-cache` | Daily or manual, trusted main only. Compiles application, CLI and CI tooling exports without DWARF in a fresh dedicated directory, checks its size and file count, then publishes the version-2 seed that the application and tooling feedback lanes read. Dispatch it manually after this workflow changes or a `go.mod`/`go.sum` change lands. |
| Refresh the production-fixture compiler cache | `ci-feedback-cache` | Daily or manual, trusted main only. Publishes the version-1 CLI/CI seed with default compiler flags. Only `source-artifact-security` in the full suite reads it; feedback no longer does. |
| Refresh the shared race compiler cache | `ci-race-cache` | Daily or manual, trusted main only. Compiles every race-instrumented package and test dependency without running tests, then publishes the single cache that all merge-group race shards and the reliability race suite restore. Run it manually after a `go.mod`/`go.sum` change lands. |
| Run repository invariants before queueing | `repository-invariants` | Advisory, per PR push touching Go code, workflows, scripts, release policy, or the store, lint fixture and web e2e inputs those suites read. Runs the `internal/boundary`, `internal/lint` and `scripts/ci` suites on the exact PR head. Not a required context; the queue still runs everything. |
| Prove queued changes | `ci-merge-group` | Queue `checks_requested` runs the complete graph, including benchmark smoke and all 16 domains. Trusted `ci-queue-required` publication authorizes only the exact current group. |
| Prove a legacy recovery candidate | `pr-candidate` | Under restored strict legacy policy, a maintainer adds `ci:full`. Trusted `ci-required-next` requires the complete immutable current-main merge. |
| Validate all of main | `ci-main` | Manual full graph, including development snapshot artifacts. |
| Run maintenance checks | `reliability` | Choose `security`, `gitlab`, `reporting`, `race`, `channel`, or `all`. Original schedules run their matching suites. |
| Prove release hardware/restore acceptance | `release-acceptance` | Choose `backup-restore`, `operator`, or `all`. Existing custody secrets stay confined to the backup step. This does not publish a release. |
| Run operations-floor acceptance | `ops-floor` | Native ARM64/AMD64 operator diagnostics; also runs for relevant PR changes. |
| Measure performance | `codspeed` or the PR benchmark control | Measurements preserve the paid-run budget. Required benchmark smoke does not use paid runners. |
| Publish nightly builds | `nightly-release` | Scheduled publication or the repository-dispatch restart path, after full validation of the selected main commit. |
| Publish stable releases | `release-build` | Signed release-tag pipeline; follow the existing release ceremony. |
| Publish documentation | `docs` | Main-push Pages/Storybook build, deployment, and public-site verification. |

New PR heads need fresh admission evidence. Main movement, membership changes,
or queue reordering invalidate old group proof; GitHub may rebuild the group.
The queue removes failed or conflicting entries. Resolve the cause and request
queue admission again; never promote stale green proof. Ordinary pushes do not
request a legacy full candidate. Under legacy rollback, remove and reapply
`ci:full` after either PR head or main changes.

From the repository root with authenticated GitHub CLI access, after the
consolidated maintenance workflows reach main:

```sh
gh workflow run reliability.yml --repo Hikyo-Org/Hikyo --ref main -f suite=security
gh workflow run release-acceptance.yml --repo Hikyo-Org/Hikyo --ref main -f suite=operator
gh workflow run ci-main.yml --repo Hikyo-Org/Hikyo --ref main
```

These commands request runs; they do not prove success. Inspect the jobs and
artifacts. Use the Actions suite selector for other choices. Backup/restore
requires the existing custody secret configuration.

## The complete workflow map

The source contains 34 YAML workflow files. GitHub's sidebar also contains
provider-generated and historical entries; deleting YAML does not erase history.

| Responsibility | Files | Why they remain separate |
| --- | --- | --- |
| Validation | `ci-feedback.yml`, `ci-candidate.yml`, `ci-main.yml`, `ci-merge-group.yml`, `ci.yml` | Fast per-commit feedback and explicitly admitted full candidates have different costs. Event entry points share a complete graph. PR code executes outside privileged controller contexts. |
| Advisory PR checks | `ci-dependency-compatibility.yml`, `ci-invariants.yml` | Read-only early signals for dependency bumps and repository-invariant suites that otherwise fail only in the serial queue. Neither is a required context. |
| Trusted gates | `ci-queue-gate.yml`, `ci-queue-worker.yml`, `ci-pr-gate.yml` | Trusted callbacks publish queue admission/group proof and the separate legacy full-candidate context. Queued checks do not hold a polling runner; only a completed merge group whose GitHub job metadata lags, or whose reads fail transiently, is re-read for a bounded window. |
| Queue lifecycle diagnostic | `ci-check-lifecycle-probe.yml` | Fixed non-required App-token immutable-verdict/neutral-cleanup proof on trusted main, before enabling new queue writers. |
| Retired checks | `ci-check-retirement.yml` | Trusted asynchronous cleanup of authenticated unfinished synthetic records, with the existing writer leases and live-role checks. Never grants merge proof. |
| Superseded runs | `ci-run-retirement.yml` | Bounded asynchronous cancellation of authenticated obsolete merge-group runs. Preserves active PR/group/default-branch roles and verifies terminal readback. |
| Reporting | `dco-report.yml`, `fuzz-report.yml` | Write-capable reports execute trusted code separately from PR validation. |
| Queue diagnostics | `ci-queue-report.yml`, `ci-impact-shadow.yml` | Mirror merge-group progress onto the PR head (title shows failures and progress; the summary links the run), and observe backend impact selection. Neither grants merge authority. |
| Performance | `benchmark-control.yml`, `pr-benchmark.yml`, `codspeed.yml`, `matrix-performance.yml`, `floor-bench.yml` | Budget control, PR measurement, main measurement, browser measurement, and native floor enforcement retain distinct trust/hardware contracts. |
| Maintenance | `reliability.yml` | Original daily security and weekly integration/race/channel checks, plus one manual menu. |
| Compiler cache seeds | `ci-feedback-backend-cache.yml`, `ci-feedback-cache.yml`, `ci-race-cache.yml` | Trusted-main-only producers of Go build entries. PR feedback, source-artifact validation, race shards and the reliability race suite only read them. One shared race entry replaces twelve per-shard entries that could not fit the 10 GB repository cache budget together. |
| Release acceptance | `release-acceptance.yml`, `ops-floor.yml`, `aws-acceptance.yml` | Manual secret-bearing acceptance is separate from PR-capable operations validation. |
| Publication | `nightly.yml`, `nightly-manual.yml`, `release.yml`, `security-release.yml`, `docs.yml` | Nightly, stable tags, and Pages retain separate credentials and gates. |

Files live in [`.github/workflows/`](../../.github/workflows/). Display names may
differ from filenames. `ci.yml` and `matrix-performance.yml` are reusable graphs.

Historical workflow entries can remain in GitHub even after source consolidation.
The permanent legacy full-only publisher is retained for protected rollback.

Scheduled triggers are best effort. Between 12 and 19 October 2026 GitHub
delivered each 15-minute control-plane schedule (`pr-gate`, `queue-gate`,
`check-retirement`) about seven times per 24 hours, and the 02:00 UTC nightly
started between 06:21 UTC
([run 39009622432](https://github.com/Hikyo-Org/Hikyo/actions/runs/39009622432))
and 08:39 UTC
([run 38370804480](https://github.com/Hikyo-Org/Hikyo/actions/runs/38370804480)).
Treat scheduled repair and cache seeding as hours-scale, not minutes-scale;
dispatch the workflow manually when a fresher result matters.

## Coverage and trust rules

Fast feedback selects inexpensive checks and reports a verdict for each distinct
pushed head. It does not run every integration, race, or fuzz test. Go feedback
compiles changed package owners, without a reverse-importer scan; a completely
deleted package can leave broken imports that only the full candidate exposes.
The runner timeout includes checkout, dependency setup, and execution. A timeout
is failed feedback, not proof of a completed check. Shared hosted runner queue
capacity prevents a universal five-minute promise without measured capacity.

Every merge group and legacy full candidate selects all domains from the trusted
[job registry](../../scripts/ci/ci-job-registry.json), using the immutable current
base's [classifier](../../scripts/ci/classify-changed-paths.sh). All previous test,
race, fuzz, web, integration, docs, native-floor, supply-chain, and release
snapshot checks remain mandatory. The frozen conservation baseline rejects
removed or weakened execution; exact additive demo and benchmark steps are
checked separately. Benchmark smoke exercises every benchmark in the same five
packages once, inside `test_core`; a failure fails that required job.

The trusted publisher binds repository, candidate workflow, run, attempt, exact
head, live main, and immutable merge parents. It rejects old head-only/selective
runs and requires the full candidate completion marker. PR-controlled artifacts
are not executable policy. Workflow byte identity normally matches trusted
main; the existing explicit exact-head workflow-change approval policy remains
the sole exception. Revoked or stale approval does not authorize a later head.
A request label or admission result is neither source review nor full merge proof. Fork vouching and
GitHub's fork-run approval remain separate admission boundaries.

GitHub branch rules match a required check name and, when configured, its app;
they do not identify the producer workflow. The generic GitHub Actions app is
shared by workflows. The controller's source/provenance checks do not establish
stronger platform enforcement against another workflow publishing a same-name
check. Maintainers must inspect the exact trusted publisher and full candidate
provenance before merging; this existing platform limit is not solved by the
split. The controller refuses a conflicting native same-name check, leaves it
untouched, and creates its own queued barrier. That barrier is not a guarantee
of GitHub's behavior for ambiguous same-name checks.

Main-push publication and security checks retain their triggers. Full main
validation remains manual and a nightly prerequisite. Stable releases keep their
existing exact-commit validation ceremony. Preserve pins, signatures, cache
ownership, native runners, budget admission, and protected publication.

## Documentation prerequisites

Local documentation verification and the main-policy validator require `jq` in
addition to the pinned Node/pnpm toolchain. Install `jq` with your system package
manager before direct `pnpm --dir docs/site run verify` or local policy commands.
For example, macOS uses `brew install jq`; Ubuntu uses its `jq` package.

The shared `scripts/ci/verify-docs.sh` bootstrap is used by both full CI and
Pages. It provisions missing `jq` through Ubuntu APT only in the recognized
root GitHub Actions Ubuntu 24.04 container, before Corepack or verification.
Outside that context it fails with the local prerequisite instead of escalating
privileges. Installation failure stops verification. Policy validation never
installs packages, accesses the network or changes system settings.

## Queue policy and protected recovery

The canonical [main policy](../../release/repository/main-ci-gate.json) requires
`ci-queue-required`, mandatory `ALLGREEN` queue, build concurrency one, merge
limits one, zero minimum wait, `MERGE`, and a 60-minute check-response timeout.
Strict branch-up-to-date checking is false only alongside mandatory queue and
that distinct context. This delegates current-base integration to the queue;
it does not waive signatures, DCO, stale-review dismissal or resolved threads.
Build concurrency limits group dispatch, not runner capacity or all redundant
reruns. A universal five-minute queue-to-feedback guarantee is not claimed.

The source is bootstrapped on main. Protected activation and the actual minimal
workflow-token empty-queue API probe passed on main `21773f333b4c37cac61b650baf2bf2c154263e9a`
in [run 38230781293](https://github.com/Hikyo-Org/Hikyo/actions/runs/38230781293).
That verifies the forced active-policy queue-read path, not populated membership
or full-group merge acceptance. The legitimate policy/guide follow-up provides
that first hosted acceptance; do not call queue delivery complete before its
actual group, all 16 domains, trusted publication and landed tree are verified.

For an authorized main-only reconciliation with authenticated administrator
access, run from the repository root:

```sh
scripts/release/configure-repository.sh --main-only Hikyo-Org/Hikyo
```

Expected result: `repository policy: mandatory queue main policy verified`.
This path refuses absent/duplicate owning rulesets, redacted actors, weakened
source policy, API errors and readback drift. It preserves unrelated live rules
and PR parameters, including an explicit required-check `integration_id`.
Unspecified absent/null bindings compare equivalently on readback; a changed
or lost concrete binding is refused. A live linear-history requirement is
preserved, but blocks reconciliation to the fixed `MERGE` queue before any PUT.
The tool does not delete that protection or silently switch merge methods.
Discovery validates every 100-result page
and stops without writing if still incomplete after 20 pages. It does not
configure tags, releases, CodeQL, Actions
permissions or git refs. The broad command still configures those settings;
do not use it for an isolated queue transition. Neither command proves hosted
queue acceptance or replaces the actual-token capability staging.

Before a legacy-to-queue activation, save the exact writable live owning
ruleset. Keep this snapshot private and immutable for that transition:

```sh
(
set -eu
gh api repos/Hikyo-Org/Hikyo/rulesets/20539346 |
  jq '{name,target,enforcement,bypass_actors,conditions,rules}' > main-policy-before.json
scripts/release/check-main-policy.sh --legacy main-policy-before.json
)
```

The last command is a prerequisite for an activation snapshot captured in
legacy mode; it must fail if that snapshot is already queue mode. For an
already-active queue reconciliation, use the queue validator without `--legacy`
for the new queue snapshot and retain the earlier legacy restoration snapshot.
The checked-in
[legacy reference](../../release/repository/main-ci-gate-legacy.json) documents
the known original policy. Prefer the exact per-transition snapshot so stricter
or newly added live settings are preserved.

If capability, membership or full proof fails, leave merges blocked or, with
operator authorization, atomically restore the saved **legacy** snapshot:

```sh
(
set -eu
scripts/release/check-main-policy.sh --legacy main-policy-before.json
gh api --method PUT repos/Hikyo-Org/Hikyo/rulesets/20539346 --input main-policy-before.json
gh api repos/Hikyo-Org/Hikyo/rulesets/20539346 > main-policy-after.json
scripts/release/check-main-policy.sh --legacy main-policy-after.json
jq -e --slurpfile expected main-policy-before.json '
  def writable: {name,target,enforcement,bypass_actors,conditions,rules} |
    (.rules[] | select(.type == "required_status_checks").parameters.required_status_checks[]) |=
      (if .integration_id == null then del(.integration_id) else . end) |
    .rules |= sort_by(.type);
  writable == ($expected[0] | writable)
' main-policy-after.json
)
```

Expected result: both validators and equality exit zero. This restores strict
`ci-required-next` and removes mandatory queue in one policy update. Old or late
`ci-queue-required` admission greens cannot satisfy the distinct full-only gate.
Only fresh complete current-main candidates authorize merge under rollback.
Never remove the required context, fabricate success or relax protection to
recover. Re-enable queue only after the actual-token capability and real-group
acceptance sequence passes again.

[GitHub queue semantics](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-a-merge-queue)
explain why the group is a different SHA and why main movement or reordering can
require rebuilds. The trusted publisher retains lifecycle/completion callbacks
and scheduled repair; metadata movement of unrelated open PRs remains pending
rather than making main publication fail.

## Queue publisher head leases

The direct PR/feedback publisher and full-group callbacks share a
repository-ID/head-SHA concurrency key with the fixed maintenance worker.
The head is a routing hint; fresh current metadata and expected-head guards
remain required before every owned check write. Read-only main/schedule/manual
discovery dispatches complete validated head batches and has no check-write
permission. Workers have no workflow-dispatch permission.

Targeted publisher execution is limited to one minute from the authenticated
current job start, including checkout, setup and compilation. Controller work
uses 45 seconds and reserves the remaining absolute budget for a fresh blocking
terminal publication. Feedback retains its four-minute job limit; maintenance
and full-group jobs retain ten minutes. These active execution limits do not
guarantee push-to-eligibility wall time when GitHub queues or APIs are delayed.
Startup or terminal transport failure remains a failure, never a green fallback.
The initial job lookup tolerates GitHub briefly reporting that same executing
job as pending, queued or in progress. One complete bounded Jobs API read must
match its run, attempt, source head, unique job ID and original start timestamp.
The absolute deadline is retained without polling or another execution budget.

Queue publishers retry GitHub reads that fail with HTTP 429, 500, 502, 503 or
504, or with a `gh` failure that reports no status (except authentication and
cancellation): three attempts with 1 and 3 second backoff, never past the
invocation deadline. Writes are never retried. Discovery dispatch, retirement
discovery and the legacy gate keep single-attempt reads.

The full-group callback and the group worker are the only invocations that
wait.
- When a successful merge-group run's proof jobs are still listed as
  unfinished or missing, they poll that run's jobs every 20 seconds, then
  repeat the complete reassessment once.
- When other reads keep failing transiently, the full reassessment repeats
  after 20, 40, 80 and then 120 seconds.
- A rate limit (HTTP 429) ends waiting at once.

If GitHub stays incomplete or unreadable for 6 minutes, the publisher writes
the distinct terminal refusal "Merge group proof metadata unavailable", so the
PR can be re-queued instead of waiting for the queue's 60-minute timeout. The
exception is when no pass ever proved the merge group's identity: then the
record stays pending, as before. See the
[transient read handoff](../handoff/queue-proof-transient-reads.md).

Queue jobs run only when `QUEUE_HEAD_WORKERS_ENABLED` is enabled. Before a
new source generation is enabled, drain old unleased compiled publishers and
verify its actual trusted-main lifecycle using the reviewed cutover. Preserve
mandatory queue protection throughout; enabling a variable does not replace
hosted admission and full-group acceptance.

PR [#898](https://github.com/Hikyo-Org/Hikyo/pull/898) landed as `f1022b9c` after
all 16 domains passed in 24m 51s. Trusted-main version-2 probe
[38371444282](https://github.com/Hikyo-Org/Hikyo/actions/runs/38371444282) passed
independent receipt and both-canary neutral-cleanup verification. Guarded
activation enabled workers at 08:51:48 UTC on 13 October 2026; exact source and
mandatory queue protection were unchanged. Ordinary deployed PR admission
within five minutes and a new complete queued group still require measurement.
See the [policy handoff](../handoff/merge-queue-policy.md) for evidence and limits.

The fixed `check-lifecycle-probe` manual workflow has no caller inputs and must
run from current trusted main before enabling the new publishers. It creates
only non-required `ci-check-lifecycle-probe` checks through the production
immutable-verdict helper. It must supersede an authenticated terminal success
with a different queued ID, verify that new pending record is effective, reuse
and finalize it, and prove identical terminal publication adds no writes.
Every known created ID must be independently read back as neutral after cleanup.
Its version-2 `immutable-verdict` receipt is data, not a success flag: the operator
also verifies exact workflow/run/attempt/source, actual owned identities and
cleanup. Old reset receipts cannot qualify. Failure, lost identity or incomplete
cleanup leaves `QUEUE_HEAD_WORKERS_ENABLED` disabled. The staged publication
hold preserves mandatory queue protection; old unleased templates refuse before
publication and new production writers stay skipped while unset/false.

A complete fresh reconciliation repeats current policy, workflow authority,
fork vouch, DCO, source attempt, role/member and historical collision checks.
Only then may an identical terminal binding and outcome require no mutation.
Changed bindings or verdicts create one new queued generation; authenticated
nonterminal generations can be reused. A failed initial owned-history scan
remains an error, but a separate successful recovery scan can authenticate the
current owned record and authorize publication. The original error is retained;
recovery must verify the effective blocking state within the existing reserve.
If that recovery scan also fails, it authorizes no new queue mutation and cannot
establish an effective blocker. An uncertain creation is not retried. When safe creation
fails, a freshly authenticated effective owned record may receive a genuine
failure. This fallback is for an error, never routine pending.
No terminal check is reset, foreign checks are not patched, and complete
historical scans and their existing bounds remain. These final mutable reads
are not an atomic GitHub authority transaction.

Race weights are recalibrated from all twelve JSON timing artifacts of successful
full group 38279650054, attempt 1. The twelve-shard count, packing, complete target
discovery and database serialization are unchanged. The source snapshot contains
800 planner units, including whole-package units; this is not a test count.
The measured-cost model maximum improves from 985.78 to 753.51 seconds. This model
is not hosted latency proof. PR #893 bootstrap copies the planner from its
immutable trusted `BASE_SHA`, so it exercises the old placement. The new weights
are exercised after landing; a later legitimate full group must establish the
approximately 35-minute target.

Trusted-main probes demonstrated that omitting terminal reset fields retains
completed/success and that the explicit-null candidate is rejected. Those
canaries were cleaned up to neutral; neither protocol authorized activation.
The immutable-verdict protocol instead uses creation and nonterminal updates;
its actual workflow-token acceptance was independently verified before activation.
The separate legacy full-only proof policy is unchanged. Its observer can retire
unfinished records once the exact commit has no live PR or merge-group role.

## Reading historical check results

PR-head checks and the tested merge group are different identities; the landed
main commit can be the exact tested group SHA. Historical pending
`ci-required-next` and `ci-queue-required` records are synthetic verdicts, not
allocated runners or running tests. Closed and obsolete callbacks retire only
authenticated unfinished records after complete live-role checks. Successful
and failed terminal verdicts remain immutable. Retirement records `cancelled`,
which can leave a historical commit's aggregate badge red; it does not turn
unvalidated history green.
Compare the exact SHA, context, App and source binding with actual job steps and
runner assignment before diagnosing capacity or test failure.

Historical third-party review and optional performance-measurement results
remain separate evidence from the required all-16 suite. Required benchmark
smoke and floor enforcement still run; an optional paid measurement's skip or
failure does not remove them. Likewise, failed non-required lifecycle probes
record diagnostic failures, even after product validation passes. A green main
summary or a neutral canary does not erase an earlier failed result, and a stale
merged-head placeholder does not authorize a later merge. Inspect the current
required proof and resolve actual failures without deleting or bypassing checks.

Useful feedback and dependency checks supersede earlier revisions of the same
PR. Irrelevant metadata events have separate concurrency groups. Full legacy
candidates are still explicitly requested with `ci:full`; head/base changes and
closure invalidate the prior exact pair without starting another full suite.
An out-of-order callback cannot cancel a request for a different pair. If main
advanced independently before a head update, its webhook cannot identify an
older candidate's admission base; that candidate can finish, but exact-parent
proof rejects its stale result.

Run retirement is asynchronous and never supplies validation. It authenticates
the trusted merge-group workflow, repository, branch, attempt and complete live
roles, and requires the original group ref to be missing or moved. It rereads
that evidence before cancellation and verifies terminal state afterward. The
sweep has 120-second, 180-API-call and ten-cancellation bounds; incomplete
inventory or ambiguous state fails closed. Neither retirement workflow removes
required domains or adds a full suite after merge.

Check retirement has an hourly fallback and input-free manual backlog sweep.
Discovery reads the newest 1,000 main commits without holding a writer lease,
then routes effective pending contexts to the existing context-specific leases.
The logged `history_scope` distinguishes complete ancestry from a limited recent
window, including its immutable snapshot, scanned count and continuation cursor.
Older history uses exact-head backfill. API failures, incomplete pages, more
than 1,000 effective contexts per commit or more than 100 target heads refuse
all targets. Window completion is not evidence that all history was cleaned.
Legacy observer heads run in batches of at most ten unique heads concurrently
under their one global lease and compiled executable. Every child is awaited,
then failures are reported together. This bounds batch duration by the slowest
controller rather than ten sequential deadlines without adding runner jobs.
Targeted manual dispatch accepts one full `head` SHA.
See the [repair handoff](../handoff/ci-retirement-2026-10.md) for measurements,
historical limitations and verification commands.
