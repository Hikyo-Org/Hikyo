# CI guide

For a normal contribution, open a PR and follow **pr-validation**. Its jobs run
the checks selected by changed paths. **ci-required** is the trusted merge gate;
**ci-required-next** is an observation check for its replacement, not a second
test suite. A pending gate can mean validation is waiting for a runner. Open the
validation run before interpreting a gate timeout as a test failure.

## Choose a task

| What you need | Actions workflow | What it does |
| --- | --- | --- |
| Validate a PR | `pr-validation` | Automatic read-only checks on the exact PR head; includes benchmark smoke when core tests run. |
| Validate all of main | `ci-main` | Manual full graph, including development snapshot artifacts. |
| Run maintenance checks | `reliability` | Choose `security`, `gitlab`, `reporting`, `race`, `channel`, or `all`. Existing schedules run only their matching suites. |
| Prove release hardware/restore acceptance | `release-acceptance` | Choose `backup-restore`, `operator`, or `all`. Only the backup step receives its existing custody secrets. This does not publish a release. |
| Run operations-floor acceptance | `ops-floor` | Native ARM64/AMD64 operator diagnostics; also runs for relevant PR changes. |
| Measure performance | `codspeed` or the PR benchmark control | Main measurements and admitted PR measurements preserve the paid-run budget. Smoke checks do not use paid runners. |
| Publish nightly builds | `nightly-release` | Scheduled publication or the existing repository-dispatch restart path, after full validation of the selected main commit. |
| Publish stable releases | `release-build` | Signed release-tag pipeline; follow the existing release ceremony. |
| Publish documentation | `docs` | Main-push Pages/Storybook build, deployment, and public-site verification. |

From the repository root with authenticated GitHub CLI access, after the
consolidated workflows are merged into main:

```sh
gh workflow run reliability.yml --repo Hikyo-Org/Hikyo --ref main -f suite=security
gh workflow run release-acceptance.yml --repo Hikyo-Org/Hikyo --ref main -f suite=operator
gh workflow run ci-main.yml --repo Hikyo-Org/Hikyo --ref main
```

Each command requests one run and normally prints its URL. Dispatch acceptance
does not establish a passing result. Inspect that run's jobs and artifacts.
Use the Actions menu's suite selector for the other choices. The backup-restore
suite requires the pre-existing custody secret configuration.

## The complete workflow map

There are 20 YAML files after consolidation. These are grouped by responsibility
here because GitHub's sidebar also includes generated and historical entries.

| Responsibility | Files | Why they remain separate |
| --- | --- | --- |
| Validation | `ci-fork.yml`, `ci-main.yml`, `ci-merge-group.yml`, `ci.yml` | Three event-specific entry points share one test graph. PR execution must stay outside `pull_request_target`'s cache and privilege context. |
| Trusted PR gate | `ci-control.yml`, `ci-pr-gate.yml`, `ci-gate-reconcile.yml` | Existing required gate, observed replacement, and timeout repair. The legacy pair stays until hosted replacement acceptance is proven. |
| Feedback | `dco-report.yml`, `fuzz-report.yml` | Write-capable reporting runs trusted code, separate from PR execution. |
| Performance | `benchmark-control.yml`, `pr-benchmark.yml`, `codspeed.yml`, `matrix-performance.yml`, `floor-bench.yml` | Spending control, unprivileged PR measurement, main measurement, reusable browser measurement, and native floor enforcement have distinct trust/hardware contracts. |
| Maintenance | `reliability.yml` | Daily security and weekly integration/race/channel checks, plus one manual menu. |
| Release acceptance | `release-acceptance.yml`, `ops-floor.yml` | Manual secret-bearing backup acceptance is separate from the PR-capable operations workflow. |
| Publication | `nightly.yml`, `release.yml`, `docs.yml` | Nightly, stable tags, and Pages have different publication credentials and gates. |

Files live in [`.github/workflows/`](../../.github/workflows/). Display names can
differ from filenames. `ci.yml` and `matrix-performance.yml` are reusable
implementation graphs rather than workflows you need to dispatch directly.

The old `go-security`, `gitlab-e2e`, `reporting-e2e`, `race-isolation`, and
`security-channel` entries move to `reliability`. `floor-acceptance` and
`operator-floor` move to `release-acceptance`. `benchmark-smoke` becomes a step
inside required core validation, reusing the packages it just compiled and tested.

GitHub-managed CodeQL, dependency graph, Dependabot, and advanced-security entries
are not additional YAML pipelines in this checkout. Deleted workflow entries can
remain visible because GitHub retains run history. This source change does not
delete history or change those security settings.

## Coverage and trust rules

The base-controlled [job registry](../../scripts/ci/ci-job-registry.json) and
[classifier](../../scripts/ci/classify-changed-paths.sh) determine PR coverage.
Unknown paths select the full graph. Core Go changes keep blocking test, race,
and fuzz coverage; API and operator plans preserve their integration checks.
Benchmark smoke exercises every benchmark in the same five packages as before,
once each, inside `test_core`. A smoke failure now fails that required job.
Docs-only changes do not start an unrelated cold Go benchmark job.

Merging does not repeat the full graph on every main push. Full main validation
is available manually and is a dependency of nightly publication. Stable-release
preparation requires exact-commit full validation through the existing ceremony.
Main-push Pages and GitHub-managed security checks retain their own triggers.

Keep action pins, cryptographic/DCO signatures, exact revision guards, fork
admission, trusted classifiers, and cache ownership intact. Reusable jobs inherit
their caller's event context; merging privileged controllers and PR executors
into one workflow can cross security boundaries even when jobs have conditions.
See [GitHub's reusable-workflow context rules](https://docs.github.com/en/actions/reference/workflows-and-actions/reusing-workflow-configurations).

## Why the two PR gates still exist

The observation publisher replaces runner-held polling with queued Checks API
results and short callbacks. Hosted observation exposed truncated identifiers
on long base branch names; bounded identifiers and conservative legacy repair
fix that defect. Its repaired behavior still needs hosted verification before
removing the required polling gate and its reconciliation workflow.

Activation must prove pending and terminal transitions, superseding prior
success after reruns/retargets, and recognition as the repository's required
check. Do not simply rename `ci-required-next`, remove the old gate, or weaken
the ruleset. Removing the polling runner is the largest remaining measured
opportunity; consolidation alone does not establish a faster hosted PR run.
