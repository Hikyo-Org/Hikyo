# CI guide

Every pushed PR commit receives automatic **ci-feedback**. When the change is
ready for merge, a maintainer requests **pr-candidate** with the `ci:full` label.
That candidate runs the complete suite against an immutable merge of the exact
PR head and current main. **ci-required-next** is the trusted completion-driven
merge gate after activation; fast feedback never grants merge authority.

The source/configuration in PR #886 prepares the final contract. A live ruleset
can still require legacy `ci-required` until the protected activation below is
completed. Local tests, a label, or a green feedback job do not establish that
activation or replace hosted full-suite proof.

## Choose a task

| What you need | Actions workflow | What it does |
| --- | --- | --- |
| Check each pushed commit | `ci-feedback` | Selected formatting, compilation, lint, package tests, workflow and docs checks on the exact PR head. Four-minute runner deadline includes setup; queue time is separate. |
| Refresh the bounded feedback compiler cache | `ci-feedback-cache` | Daily or manual, trusted main only. Compiles the CLI/CI dependency seed in a fresh dedicated directory, checks its size and file count, then publishes it for read-only feedback reuse. |
| Prove the final PR candidate | `pr-candidate` | A maintainer adds `ci:full`. The complete graph tests the immutable current-main merge, including benchmark smoke and every mandatory domain. |
| Validate all of main | `ci-main` | Manual full graph, including development snapshot artifacts. |
| Run maintenance checks | `reliability` | Choose `security`, `gitlab`, `reporting`, `race`, `channel`, or `all`. Original schedules run their matching suites. |
| Prove release hardware/restore acceptance | `release-acceptance` | Choose `backup-restore`, `operator`, or `all`. Existing custody secrets stay confined to the backup step. This does not publish a release. |
| Run operations-floor acceptance | `ops-floor` | Native ARM64/AMD64 operator diagnostics; also runs for relevant PR changes. |
| Measure performance | `codspeed` or the PR benchmark control | Measurements preserve the paid-run budget. Required benchmark smoke does not use paid runners. |
| Publish nightly builds | `nightly-release` | Scheduled publication or the repository-dispatch restart path, after full validation of the selected main commit. |
| Publish stable releases | `release-build` | Signed release-tag pipeline; follow the existing release ceremony. |
| Publish documentation | `docs` | Main-push Pages/Storybook build, deployment, and public-site verification. |

After either PR head or main advances, the previous full candidate is stale.
Remove and reapply `ci:full` to request a new one. Ordinary pushes do not restart
the expensive suite. Unrelated labels and metadata edits do not cancel useful
candidate or feedback work. A request label authorizes work only; the trusted
publisher independently checks source policy and exact run provenance.

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

The final source contains 20 YAML workflow files. GitHub's sidebar also contains
provider-generated and historical entries; deleting YAML does not erase history.

| Responsibility | Files | Why they remain separate |
| --- | --- | --- |
| Validation | `ci-feedback.yml`, `ci-candidate.yml`, `ci-main.yml`, `ci-merge-group.yml`, `ci.yml` | Fast per-commit feedback and explicitly admitted full candidates have different costs. Event entry points share a complete graph. PR code executes outside privileged controller contexts. |
| Trusted PR gate | `ci-pr-gate.yml` | Short trusted callbacks publish/reassess Checks API results; queued checks do not hold a runner. |
| Reporting | `dco-report.yml`, `fuzz-report.yml` | Write-capable reports execute trusted code separately from PR validation. |
| Performance | `benchmark-control.yml`, `pr-benchmark.yml`, `codspeed.yml`, `matrix-performance.yml`, `floor-bench.yml` | Budget control, PR measurement, main measurement, browser measurement, and native floor enforcement retain distinct trust/hardware contracts. |
| Maintenance | `reliability.yml` | Original daily security and weekly integration/race/channel checks, plus one manual menu. |
| Feedback compiler seed | `ci-feedback-cache.yml` | Trusted-main-only producer for bounded, dedicated Go build entries; PR feedback only reads them. |
| Release acceptance | `release-acceptance.yml`, `ops-floor.yml` | Manual secret-bearing acceptance is separate from PR-capable operations validation. |
| Publication | `nightly.yml`, `release.yml`, `docs.yml` | Nightly, stable tags, and Pages retain separate credentials and gates. |

Files live in [`.github/workflows/`](../../.github/workflows/). Display names may
differ from filenames. `ci.yml` and `matrix-performance.yml` are reusable graphs.

The old `go-security`, `gitlab-e2e`, `reporting-e2e`, `race-isolation`, and
`security-channel` workflows move to `reliability`. `floor-acceptance` and
`operator-floor` move to `release-acceptance`. Standalone `benchmark-smoke`
becomes a required core step. `ci-control`, `ci-fork`, and `ci-gate-reconcile`
retire only after the replacement gate is proven and the required context is
switched. Their security assertions transfer to the active producer workflows.

## Coverage and trust rules

Fast feedback selects inexpensive checks and reports a verdict for each distinct
pushed head. It does not run every integration, race, or fuzz test. Go feedback
compiles changed package owners, without a reverse-importer scan; a completely
deleted package can leave broken imports that only the full candidate exposes.
The runner timeout includes checkout, dependency setup, and execution. A timeout
is failed feedback, not proof of a completed check. Shared hosted runner queue
capacity prevents a universal five-minute promise without measured capacity.

Every admitted candidate selects all domains from the trusted
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
The request label is neither approval nor merge authority. Fork vouching and
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

## Protected gate activation

The final PR head already contains legacy workflow deletions. Their live
retirement occurs when that head merges; trusted default-branch legacy producers
remain available during the proof and required-context swap. No extra retirement
edit or second full run is needed solely for this ordering.

1. Preserve the currently required legacy context while proving a fresh full
   candidate through default-branch tooling. Inspect all mandatory jobs, exact
   source/workflow identity, and both merge parents against authenticated live
   main and head. Prove pending/success/failure transitions and invalidation after
   rerun or base/head movement. Old full #887 success proves its bootstrap,
   not the new hosted candidate contract.
2. Atomically replace the required context with `ci-required-next`, retaining
   strict current-main policy, signatures, resolved review threads, bypass
   restrictions, and every other original rule. Read the settings back and prove
   GitHub recognizes the new required check. Never leave an unprotected gap or
   create same-name success alongside a legacy producer.
3. Only then merge source retirement of `ci-control.yml`, `ci-fork.yml`, and
   `ci-gate-reconcile.yml`, after complete hosted validation of that exact final
   head on current main. If either revision changes, request a new candidate.
   Do not interpret a stale run, local tests, or optional provider skip as proof.

The publisher retains lifecycle and completion callbacks plus scheduled repair;
repair reads live state and does not poll a long-running validation inside a
runner. Hosted acceptance must measure commit-to-feedback time, including queue
and setup, during contention as well as quiet periods. Removing a polling runner
reduces load but does not reserve capacity in GitHub's shared hosted pool.
