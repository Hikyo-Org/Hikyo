# CI guide

Feature PRs target **dev** and receive automatic **ci-feedback**. Required
**ci-dev-required** admission verifies the exact head's three feedback lanes,
signatures/DCO, fork vouch and trusted workflow authority. Dev has no full merge
queue. Each merged dev SHA requests full **ci-dev** integration validation:
one active run finishes, with only the newest pending run retained.

Frozen release candidates target **main** and enter its mandatory single-entry
queue. **ci-queue-required** admits the exact PR head and then requires complete
current-base merge-group proof. Branches under `release/` always select all 17
domains. Dev movement never cancels a fixed candidate's validation.

Stable consumers and signed nightlies can reuse authenticated complete proof for
the identical landed commit. Nightlies are built and signed from main as before.
**nightly-promote** lands the last fully green dev commit on main through the
queue first, so every main commit has queue proof. See the [branching guide](branching.md)
for activation, promotion and operator commands.

## Choose a task

| What you need | Actions workflow | What it does |
| --- | --- | --- |
| Check each pushed commit | `ci-feedback` | Selected formatting, compilation, lint, package tests, workflow and docs checks on the exact PR head. Four-minute runner deadline includes setup; queue time is separate. |
| Refresh the bounded feedback compiler cache | `cache-seed-feedback-backend` | Daily or manual, trusted main only. Compiles application, CLI and CI tooling exports without DWARF in a fresh dedicated directory, checks its size and file count, then publishes the version-2 seed that the application and tooling feedback lanes read. Dispatch it manually after this workflow changes or a `go.mod`/`go.sum` change lands. |
| Refresh the production-fixture compiler cache | `cache-seed-feedback` | Daily or manual, trusted main only. Publishes the version-1 CLI/CI seed with default compiler flags. Only `source-artifact-security` in the full suite reads it; feedback no longer does. |
| Refresh the shared race compiler cache | `cache-seed-race` | Daily or manual, trusted main only. Compiles every race-instrumented package and test dependency without running tests, then publishes the single cache that all merge-group race shards and the reliability race suite restore. Run it manually after a `go.mod`/`go.sum` change lands. |
| Refresh the pinned tool downloads | `cache-seed-tools` | Daily or manual, trusted main only. Downloads each checksum-pinned release file (ShellCheck, kind, Cosign, Node, Helm, the Calico manifest) with bounded backoff and publishes one entry per `scripts/ci/pinned-tools/<name>.pin`. `lint`, `supply-chain-checks`, `k8s-e2e`, reliability and `ci-feedback` restore it so a GitHub release outage does not fail them. Dispatch it manually after a pin file changes. |
| Run repository invariants before queueing | `ci-advisory-invariants` | Advisory, per PR push touching Go code, the golangci-lint config, workflows, Git hooks, scripts, release policy, or the store, lint fixture and web e2e inputs those suites read. Runs the `internal/boundary`, `internal/lint` and `scripts/ci` suites on the exact PR head. Not a required context; the queue still runs everything. |
| Run the Storybook suite before merge | `ci-advisory-storybook` | Advisory, per PR push touching `web/`, `clients/ts/`, `.nvmrc`, the root `package.json`, `docs/site/scripts/test-csp.mjs`, `scripts/ci/storybook-suite.sh` or the workflow's setup helpers (`install-corepack.sh`, `export-pnpm-cache.sh`). Runs the same commands as the `storybook` job in `ci.yml` (story tests in both palettes, production build, theme and docs checks, static demo) on the exact PR head in the Playwright container. The pre-push hook runs the same script locally. Not a required context; `ci-dev` and the queue still run the job. |
| Lint new Go code | `ci-advisory-golangci` | Advisory, per PR push touching Go files, `go.mod`, `go.sum`, `.golangci.yml`, `scripts/ci/golangci-lint-new.sh`, its version pin or `scripts/ci/run-go-tool.sh`. Runs golangci-lint (version in `scripts/ci/golangci-lint-version`, rules in `.golangci.yml`: errcheck, errorlint, errchkjson, noctx, wastedassign, unused, staticcheck `SA*`/`QF1001`/`QF1003`, gosec G115/G204/G306 outside tests) and reports only issues on lines changed since the merge base, so the existing baseline is not enforced. Run it locally with `scripts/ci/golangci-lint-new.sh <base>`; the pre-push hook runs it too (`HIKYO_PREPUSH_GOLANGCI=skip` opts out). Lints only the default build (not other-OS files or `ui`, `k8se2e`, `floor*` tags); type errors are always reported in full. Restores caches only, saves none. Not a required context and in no required job or fast-feedback lane. |
| Lint the desktop module | `ci-advisory-golangci-desktop` | Advisory, per PR push touching `desktop/`, `.golangci.yml`, `.golangci-strict.yml`, `scripts/ci/golangci-lint-version`, `scripts/ci/run-go-tool.sh` or the workflow itself. Pinned `golangci-lint` over the whole nested module on macOS; a Linux pre-job skips the macOS job where `desktop/go.mod` does not exist. The strict pass runs only where `.golangci-strict.yml` exists. |
| Validate integrated development | `ci-dev` | Complete read-only graph for the merged dev SHA; finishes one active run and coalesces pending work. |
| Prove queued release candidates | `ci-merge-group` | Queue `checks_requested` runs the complete graph, including benchmark smoke and all 17 domains. Trusted `ci-queue-required` publication authorizes only the exact current group. |
| Validate all of main | `ci-main` | Weekly or manual full graph, including development snapshot artifacts. The schedule is the automatic writer of main-scoped compile caches. |
| Run maintenance checks | `reliability` | Choose `security`, `gitlab`, `reporting`, `race`, `channel`, or `all`. Original schedules run their matching suites. |
| Prove release hardware/restore acceptance | `release-acceptance` | Choose `backup-restore`, `operator`, or `all`. Existing custody secrets stay confined to the backup step. This does not publish a release. |
| Run operations-floor acceptance | `release-ops-floor` | Native ARM64/AMD64 operator diagnostics; also runs for relevant PR changes. |
| Measure performance | `perf-codspeed` or `perf-control` | Measurements preserve the paid-run budget. Required benchmark smoke does not use paid runners. |
| Promote dev to main for the nightly | `nightly-promote` | Daily at 22:17 UTC or manual on main. Opens a `release/nightly-*` PR for the last green dev commit when main lacks it and queues it. A merged promotion starts the nightly. |
| Publish nightly builds | `nightly-release` | Scheduled publication or the repository-dispatch path, built and signed from main after complete verified source proof of that exact commit. A commit that is already published is skipped. |
| Publish stable releases | `release-build` | Signed tag pipeline consuming exact-source full proof; artifact acceptance remains fresh. |
| Publish documentation | `docs` | Main-push Pages/Storybook build, deployment, and public-site verification. |

New PR heads need fresh admission evidence. Main movement, membership changes,
or queue reordering invalidate old group proof; GitHub may rebuild the group.
The queue removes failed or conflicting entries. Resolve the cause and request
queue admission again; never promote stale green proof.

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

The source contains 40 YAML workflow files. GitHub's sidebar also contains
provider-generated and historical entries; deleting YAML does not erase history.

| Responsibility | Files | Why they remain separate |
| --- | --- | --- |
| Validation | `ci-feedback.yml`, `ci-dev.yml`, `ci-main.yml`, `ci-merge-group.yml`, `ci.yml` | Fast per-commit feedback and full validation have different costs. Event entry points share a complete graph. PR code executes outside privileged controller contexts. |
| Advisory PR checks | `ci-dependency-compatibility.yml`, `ci-golangci.yml`, `ci-invariants.yml`, `ci-storybook.yml` | Read-only early signals for dependency bumps, new-code static analysis, repository-invariant suites that otherwise fail only in the serial queue, and the Storybook suite that otherwise runs only after merge to dev. None is a required context. |
| Trusted gates | `ci-dev-gate.yml`, `ci-queue-gate.yml`, `ci-queue-worker.yml`, `ci-queue-retry.yml` | Trusted callbacks publish dev admission and queue admission/group proof; the retry workflow reruns a failed head publisher or group job (see [Queue publisher head leases](#queue-publisher-head-leases)). Queued checks do not hold a polling runner; only a completed merge group whose GitHub job metadata lags, or whose reads fail transiently, is re-read for a bounded window. |
| Retired checks | `ci-check-retirement.yml` | Trusted asynchronous cleanup of authenticated unfinished synthetic records, with the existing writer leases and live-role checks. Never grants merge proof. |
| Superseded runs | `ci-run-retirement.yml` | Bounded asynchronous cancellation of authenticated obsolete merge-group runs. Preserves active PR/group/default-branch roles and verifies terminal readback. |
| Security feed freshness | `security-feed-freshness.yml` | Reads what installations read, on its own schedule and credentials, because the publisher cannot be trusted to report its own staleness. Inert until `HIKYO_SECURITY_FEED_ENABLED` is `true`. |
| Reporting | `dco-report.yml`, `fuzz-report.yml` | Write-capable reports execute trusted code separately from PR validation. `fuzz-report.yml` also keeps the one issue open while dev is red. |
| Queue diagnostics | `ci-queue-report.yml` | Mirror merge-group progress onto the PR head (title shows failures and progress; the summary links the run). Neither grants merge authority. |
| Performance | `benchmark-control.yml`, `pr-benchmark.yml`, `codspeed.yml`, `matrix-performance.yml`, `floor-bench.yml` | Budget control, PR measurement, main measurement, browser measurement, and native floor enforcement retain distinct trust/hardware contracts. |
| Maintenance | `reliability.yml` | Original daily security and weekly integration/race/channel checks, plus one manual menu. |
| Compiler cache seeds | `ci-feedback-backend-cache.yml`, `ci-feedback-cache.yml`, `ci-race-cache.yml` | Trusted-main-only producers of Go build entries. PR feedback, source-artifact validation, race shards and the reliability race suite only read them. One shared race entry replaces twelve per-shard entries that could not fit the 10 GB repository cache budget together. |
| Pinned tool seed | `ci-tool-cache.yml` | Trusted-main-only producer of the pinned release-file entries. Every other workflow only restores them, and the helper verifies each file's SHA-256 before use, so a poisoned or stale entry cannot run. |
| Release acceptance | `release-acceptance.yml`, `ops-floor.yml` | Manual secret-bearing acceptance is separate from PR-capable operations validation. |
| Publication | `nightly-promote.yml`, `nightly.yml`, `nightly-manual.yml`, `release.yml`, `apple-release-recovery.yml`, `security-release.yml`, `security-feed.yml`, `docs.yml` | Promotion, nightly, stable tags, the security feed, and Pages retain separate credentials and gates. Promotion opens its PR as the nightly App, because a PR opened with the workflow token starts no CI. |

Files live in [`.github/workflows/`](../../.github/workflows/). Display names may
differ from filenames. `ci.yml` and `matrix-performance.yml` are reusable graphs.

GitHub sorts its sidebar by display name, so a prefix groups each responsibility:

| Prefix | Meaning |
| --- | --- |
| `ci-`, `trusted-merge-ci` | Runs validation. `ci-advisory-` entries never block a merge. |
| `gate-` | Publishes merge admission. Runs no PR code. |
| `release-`, `nightly-`, `docs` | Publishes or accepts a release. |
| `security-` | Publishes the security feed (`security-feed`), or checks that installations still see it fresh (`security-feed-freshness`). |
| `perf-` | Measures performance. |
| `report-` | Writes comments and issues about other runs. |
| `tidy-`, `cache-seed-` | Housekeeping and compile cache writers. |

Do not rename `ci`, `ci-main`, `ci-dev`, `ci-feedback`, `trusted-merge-ci`,
`nightly-release` for tidiness. Source proof and release consumers match those
exact names, including on runs that already happened. CodeQL, Dependabot Updates and
Dependency Graph are provider entries that the repository cannot rename or remove.

Historical workflow entries can remain in GitHub even after source consolidation.

Scheduled triggers are best effort. Between 12 and 19 October 2026 GitHub
delivered each 15-minute control-plane schedule (`pr-gate`, `queue-gate`,
`check-retirement`) about seven times per 24 hours, and the 02:00 UTC nightly
started between 06:21 UTC
([run 39009622432](https://github.com/Hikyo-Org/Hikyo/actions/runs/39009622432))
and 08:39 UTC
([run 38370804480](https://github.com/Hikyo-Org/Hikyo/actions/runs/38370804480)).
Treat scheduled repair and cache seeding as hours-scale, not minutes-scale;
dispatch the workflow manually when a fresher result matters.
Between 16 October and 3 November 2026 the 15-minute `queue-gate` schedule ran
100 times in 17.7 days: gaps of 109 minutes at the shortest, 253 at the median
and 623 at the longest. A repair that waits for it is not a repair.

## Coverage and trust rules

Fast feedback selects inexpensive checks and reports a verdict for each distinct
pushed head. It does not run every integration, race, or fuzz test. Go feedback
compiles changed package owners, without a reverse-importer scan; a completely
deleted package can leave broken imports that only full validation exposes.
The runner timeout includes checkout, dependency setup, and execution. A timeout
is failed feedback, not proof of a completed check. Shared hosted runner queue
capacity prevents a universal five-minute promise without measured capacity.

### Desktop shell (macOS)

`desktop/` is a nested Go module (the Wails shell, #1215). Wails, cgo and WebKit
must never reach the root build, so the root lanes and the root module never load it; they cannot
(`go list ./desktop/...` from the root is an error) and `golangci-lint-new.sh`
lints it from inside `desktop/` instead. The registry domain `desktop` and the
`desktop` job in `ci.yml` own it:

1. The runner follows the tree. Where `desktop/go.mod` exists the job runs on a
   pinned `macos-15` hosted runner (paid minutes), with no cache restored or
   written and `CGO_CFLAGS` and `CGO_LDFLAGS` set to `-mmacosx-version-min=13.0`.
   Where it does not (main and dev before the shell lands) a full plan still needs
   the job to report success, so it runs on `ubuntu-latest`, executes only step 2
   and costs seconds. The `changes` job publishes `desktop_module` for this.
2. `scripts/ci/check-desktop-deps.sh` keeps Wails and its platform bindings out of
   the root `go.mod` and `go.sum` and refuses a `go.work` or a root requirement on
   `desktop/`. Where `desktop/` exists it also requires `replace
   github.com/Hikyo-Org/hikyo => ../` in `desktop/go.mod`, and lists every root
   package `desktop/` compiles (`go list -deps`, darwin, probe tag) and fails when
   the classifier would not select the desktop job for it.
3. On macOS: `go test -race ./...`, a `-tags probe` build, a release build, and the
   driven WKWebView lane, `go run ./probes/driven`, which opens the probe shell
   against a fake instance (Playwright cannot drive WKWebView).
4. Lint is advisory, like the root ratchet, and the root `golangci` names must stay
   out of `ci.yml`: `ci-advisory-golangci-desktop` runs the pinned `golangci-lint`
   over the whole module on macOS, and `golangci-lint-new.sh` lints `desktop/` from
   inside it for the pre-push hook and `ci-advisory-golangci`.

The classifier selects the `desktop` domain for `desktop/*`, `api/openapi.yaml`
(the refusal list derives from it) and the root packages `desktop/` imports, and
nothing else for `desktop/` changes: those paths do not select `test`, `race`,
`fuzz` or the other Go domains. A root `go.mod` or `go.sum` change, a workflow
change and every unknown path still select the full plan, including `desktop`.
Fast feedback keeps `desktop/` out of the root compile (`go-compile`) and compiles
it with its own `desktop-compile` check, `go -C desktop test -c`, only where
`desktop/go.mod` exists. The frozen conservation baseline stays exact: `desktop` is
the only domain beyond its list, and the baseline jobs are unchanged.

Every merge group selects all domains from the trusted
[job registry](../../scripts/ci/ci-job-registry.json), using the immutable current
base's [classifier](../../scripts/ci/classify-changed-paths.sh). All previous test,
race, fuzz, web, integration, docs, native-floor, supply-chain, and release
snapshot checks remain mandatory. The frozen conservation baseline rejects
removed or weakened execution; exact additive demo and benchmark steps are
checked separately. Benchmark smoke exercises every benchmark in the same five
packages once, inside `test_core`; a failure fails that required job.
`internal/app` and `internal/lint` (database drops and cross-platform compiler
loads that must not contend with the concurrent package pool) run alone, app
then lint, in the parallel required job `test_core_serial` via
`scripts/ci/test-core-packages.sh serial`; `test_core` runs every other package.

The trusted queue publisher binds repository, merge-group workflow, run, attempt,
exact group head, and live main. PR-controlled artifacts are not executable policy. Workflow byte identity normally matches trusted
main; the existing explicit exact-head workflow-change approval policy remains
the sole exception. Revoked or stale approval does not authorize a later head.
An admission result is neither source review nor full merge proof. Fork vouching and
GitHub's fork-run approval remain separate admission boundaries.

GitHub branch rules match a required check name and, when configured, its app;
they do not identify the producer workflow. The generic GitHub Actions app is
shared by workflows. The controller's source/provenance checks do not establish
stronger platform enforcement against another workflow publishing a same-name
check. Maintainers must inspect the exact trusted publisher and merge-group
provenance before merging; this existing platform limit is not solved by the
split. The controller refuses a conflicting native same-name check, leaves it
untouched, and creates its own queued barrier. That barrier is not a guarantee
of GitHub's behavior for ambiguous same-name checks.

Main-push publication and security checks retain their triggers. Full main
validation is manual recovery plus a weekly cache-writing run. Stable releases
and nightlies reuse exact-commit full source proof; a nightly is built from main,
never from dev. Preserve pins, signatures, cache
ownership, native runners, budget admission, and protected publication.

## Pinned tool downloads

GitHub release downloads return transient 504s that outlast a short retry, so
tools that CI installs from a release are pinned in one place and fetched by one
helper. Each `scripts/ci/pinned-tools/<name>.pin` holds exactly two lines,
`url=<https url>` and `sha256=<64 lowercase hex>`; change a tool by editing its pin.
The ShellCheck, `kind-linux-amd64`, Cosign and `helm-linux-amd64` pins replaced
downloads that the frozen full-suite inventory fingerprints, so
`frozenToolPins` in `scripts/ci/pinned_tool_reuse_workflow_test.go` locks their
exact content. Bumping one of them is a reviewed coverage change: update its
`frozenToolPins` entry in the same PR, keep the restored frozen steps and the
baseline hashes unchanged, and confirm `go test ./scripts/ci` still passes.

`scripts/ci/fetch-pinned-tool.sh <name> <output>` reads the pin beside itself,
uses `${PINNED_TOOL_CACHE:-$HOME/.cache/hikyo-pinned-tools}/<name>/<sha256>` when
that file's checksum matches, and otherwise downloads with curl's exponential
backoff (no `--retry-delay`, so it waits 1, 2, 4 seconds and on, bounded by
`--retry-max-time 300`). A download that does not match its pin fails closed and
is never cached or written. `--prefetch <name>` only fills the cache. It also
works outside CI, with the default cache directory.

Readers that may use caches (`ci.yml` `lint`, `supply-chain-checks` and
`k8s-e2e`, `reliability.yml`, `ci-feedback.yml`) add a restore-only
`actions/cache/restore` step before the helper. Its path
`~/.cache/hikyo-pinned-tools/<name>` and key
`pinned-tool-v1-${{ runner.os }}-<name>-${{ hashFiles('.../pinned-tools/<name>.pin') }}`
must be byte-identical to the seed's, because the entry version derives from the
path string; `TestPinnedToolReuseDrift` enforces this, including that each restore
runs before the step that first fetches its tool. A missed or failed restore
falls through to the download, with one exception: the `ci-feedback.yml` restore
keeps the one-minute step limit of its other restores, so a hung cache service
fails that lane (as the existing frontend restore does) instead of falling through.
Lanes that forbid caches (`floor-bench.yml`, `release-acceptance.yml`) and the
privileged release, nightly and security jobs call the helper with backoff only.
`kind-linux-arm64` and `helm-linux-arm64` are therefore not seeded. Helm is also
installed from its pinned archive instead of `azure/setup-helm`, which checks
nothing it downloads.

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
limits one, zero minimum wait, `MERGE`, and a 120-minute check-response timeout (raised from 60 by #1371).
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
actual group, all 17 domains, trusted publication and landed tree are verified.

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

If capability, membership or full proof fails, leave merges blocked and repair
the queue. Never remove the required context, fabricate success or relax
protection to recover.

The strict legacy rollback (`ci:full`, `ci-required-next`, its ruleset and
publisher) was retired. It lives in Git history: find the removal with
`git log --all -- .github/workflows/ci-candidate.yml` and restore from its parent
if that policy is ever needed again. `plan-merge-group.sh` still treats a
`ci:full` label on a queued docs-only PR as a request for the complete plan.

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

The gate workflows follow the `ci-feedback` (and, for the queue, `trusted-merge-ci`)
callbacks on `requested`, `in_progress` and `completed`. GitHub documents that the
`requested` activity type does not occur when a workflow is re-run
([events that trigger workflows](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows),
`workflow_run`), so a rerun of an exact-head feedback run sends no callback at its
start, and attempt 1's success would keep admitting a merge until the rerun completes.
Each attempt is therefore reconciled once at its start: a first attempt on
`requested`, a rerun on `in_progress`. Every gate job that takes these callbacks has
`github.event.action != 'in_progress' || github.event.workflow_run.run_attempt > 1`,
so a first attempt's `in_progress` is skipped, which costs no API calls. The
reconcile reads the live, unfinished run and publishes pending
(`TestFeedbackRerunNeverLeavesAStaleSuccess`); `completed` settles the verdict. The
event sequence per attempt is pinned by `TestGateCallbacksFollowTheRealEventSequencePerAttempt`.

The per-head publisher groups (`queue-check-...`, `dev-check-...`) keep one running and
the newest pending reconcile (no `queue: max`), because every reconcile rereads live
state. That is lossless only while one open PR has the head: a replaced
`pull_request_target` publish reassesses only its own PR, so another open PR on the same
head waits for a later feedback callback or worker, which can be replaced too. That is
an accepted residual. `ci-check-retirement.yml` shares the head lease and keeps
`queue: max`, which does not protect it: GitHub does not document mixed settings in one
group, and a publisher in default mode may replace a pending retirement job. The
publishers reassess the head live and retire stale records themselves.

Targeted publisher execution is limited to one minute from the authenticated
current job start, including checkout, setup and compilation. Controller work
uses 45 seconds and reserves the remaining absolute budget for a fresh blocking
publication. A verdict whose checks finished before the 45 seconds may spend three
more seconds on its write, because a write cut off after GitHub applied it would make
the run replace its own pass with a barrier. Feedback retains its four-minute job limit; maintenance
and full-group jobs retain ten minutes. These active execution limits do not
guarantee push-to-eligibility wall time when GitHub queues or APIs are delayed.
Startup or terminal transport failure remains a failure, never a green fallback.
The initial job lookup tolerates GitHub briefly reporting that same executing
job as pending, queued or in progress. One complete bounded Jobs API read must
match its run, attempt, source head, unique job ID and original start timestamp,
and that read gets one retry. The absolute deadline is retained without polling or
another execution budget.

The budget pays for setup first, so setup stays small. The publishers check
out only `scripts/ci` and root files: a full tree costs 8 to 30 seconds or more. `pr-gate`
must not import `net/http`, which adds 86 packages to its cold compile on Linux. A test
pins that, the sparse checkout, its packages and the scripts it runs by name; it cannot
see a direct file read outside the tree.

A changed `ci-dev-gate.yml` lands on `main` first, and the publisher contract then refuses
every open dev PR until `main` is merged back into `dev` and `dev` into each PR. That is a
verdict, not a failed run: the record says what to merge, and another attempt would find
the same bytes.

`ci-feedback.yml` needs history for its merge base, so it keeps a full-depth fetch but
leaves old blobs behind, under the same one-minute step limit. `fast-feedback.sh` fetches
the blobs its whitespace check needs in one batch, anonymously. That relies on this
repository being public, and a lane fails if GitHub refuses anonymous fetches at that moment.

Setup, checkout and compilation can still take most of the 45 seconds on a slow
runner, so a pass can run out of them on a healthy head. That
is a flake, not a verdict. While the run attempt (`GITHUB_RUN_ATTEMPT`) is below
three, the reserve publishes a blocking, non-terminal "PR validation pending"
record in place of the terminal refusal, and the run still fails.
`ci-queue-retry.yml` then reruns the jobs of that run (each a new job,
so a new sixty-second budget) whose reconcile step failed, including a job its
one-minute timeout then cancelled. GitHub refuses a rerun of another job while the
run is active again (403, "the workflow run containing this job is already running"),
so at most one rerun is requested per run per pass. Several failed jobs of one run go
together through the run-level endpoint (`rerun-failed-jobs`, which reruns every failed
and cancelled job) only when the run's failed and cancelled set is exactly the wanted
set and all of one kind; otherwise the highest-priority job (a group job first) is
rerun by job id and the rest are left for that rerun's own completion, which fires the
retry again within the attempt bound. A pending reconcile that a newer one replaced
ends cancelled without running a step and is never wanted, so it keeps the run-level
endpoint off. Worker group jobs share the publisher's name and get the same reruns, so
do the queue gate's own `group-<sha>` jobs, and so does a failed setup, because
reconcile always runs. Before any rerun it reads `rate_limit` (which is not
counted; the core and graphql buckets are separate, and the one with the fewest
requests left decides, the absolute count the Go gate checks per bucket) and, while less than a tenth of the budget is left, reruns
nothing and hands the run to the `rerun-after-reset` job. Group jobs are rerun first,
and any other job is rerun only while its expected cost (42 reads) still leaves the
reserve (see "API reserve for merge-queue groups" below), which is also the threshold
below which the job defers itself (it defers when `remaining - 42 < 250`, in either
bucket); the others are held and handed straight to the wait, so a deferral costs
no attempt it cannot recover. That job is a designed wait: it sleeps
until the window's reset plus 5 seconds (refusing a reset more than 61 minutes away),
re-reads `rate_limit`, then reruns, group jobs first, every qualifying failed
job of the last three hours from the three publisher workflows, under the same trust
rule and attempt bound. It pages through every failed and cancelled run of that period
(status and `created` filters, `--paginate`), with no fixed run count, and requests one
rerun per run as above. A window that has already recovered when the job starts (a reset
passed in between, or the sleeper queued behind another) skips only the sleep; the sweep
still runs. If a non-group job then does not fit above the reserve (remaining between 250
and 291, say), the job waits for the reset once and releases the held set once; a hold
that only group jobs suffer does not raise the sleep threshold, and those jobs are named. The reruns are spaced 15 seconds apart. A released rerun spends only after
it starts, so the live `rate_limit` does not show it yet: each is counted at its expected
cost (41 reads for a group job, 42 otherwise) against the lower of the live remaining and
the budget when releasing began, and the job stops starting reruns when that projection
would cross a fifth of the window (a non-group job: the reserve), or 66 minutes in, and
names what it held, so a burst cannot re-exhaust the window it waited for. A held job keeps its pending check. Nothing
completes it automatically unless another failed gate run starts a new wait, so the
operator command `gh workflow run ci-queue-gate.yml -f pr_number=<n>` is the completer
for a merge-group head. A jobs or runs listing that fails is retried with the same
backoff; one that still fails is named and skipped, and the job fails visibly at the
end instead of abandoning every other rerun. A refused rerun request is an
idempotent re-request, not a gate write, so it is retried with backoff (the `rerun` job
twice more within its two minutes, the wait job three more times, 65 seconds at most)
before the job is reported not rerun. Its 70-minute `timeout-minutes` is one primary window plus the sweep and is the
only deliberately long timeout in these workflows; the merge-queue timeout is
unchanged. One repo-wide concurrency group (`queue-retry-after-reset`, no cancel) lets
one sleeper run with one behind it, so a burst of failures costs at most one waiting
runner. A merge-group head gets no later event, so this wait, not the cron, completes
its `ci-queue-required` check after a rate-limited group or publish job; a window that
is still spent after the wait fails the job visibly. A refused rerun of one job does not stop the others; the
step names each job or run not rerun and fails. A secondary limit is not detected and
is rerun like any other failure, bounded by the attempt count. The run attempt is the only loop bound: attempt 3 publishes
the terminal refusal, unless the PR, its base, the rules or the feedback attempt
moved during it, and is never rerun, the retry workflow does not follow
itself, and it holds only `actions: write`, checks nothing out and runs no
repository code. It follows only the three publisher workflows by path, and only
their real triggers on `main` or `dev`, or `pull_request_target`, which runs the
base branch's file. A pass that already published its verdict keeps it when the
work clock ends a moment later. A pending record blocks like any other; the rerun,
not the cron, settles it. The record stays pending (blocking) until the next event
or discovery run if the retry workflow is disabled, if a runner is lost so reconcile
never fails, if the PR, its base, the rules or the feedback attempt moves during
attempt 3, or if setup fails on attempt 3, which publishes nothing.

#### API reserve for merge-queue groups

Merge CI takes 36 to 73 minutes (measured) and the `group-<sha>` job starts only after
it, so a merge-queue head cannot afford to lose the shared window to everything else:
a wait of up to 60 minutes plus the rerun would exceed the 120-minute queue timeout.
Within option A (the repository's `GITHUB_TOKEN`, no App token) the root fix is a
reserve of 250 requests of the 1,000 per hour. Every job that does not gate a
merge-queue head (dev publishers, queue PR-admission publishers and head workers, the
retirement jobs, the retry reruns of such jobs, and the scheduled dev sweep) first
reads `rate_limit`, which costs nothing, and defers while either the core or the
graphql bucket would be left with fewer than 250 after the job's own expected cost
(42 reads), that is, below 292: it spends and writes nothing, its Reconcile
step fails, and the retry and wait workflows rerun it after the reset. A retirement
job (`tidy-checks`) is not one the retry follows: its deferral is completed by the next
hourly discovery, a push or a manual dispatch, which is acceptable because it only
retires records of heads that are no longer active and so gates no merge. To a
contributor a deferral looks like a red gate job whose log says "deferred: ... too few to
leave the 250 kept for merge-group jobs after this job's own 42; this job spent nothing and
is rerun after the reset"; the
retry hands it to the wait job, which reruns it after the reset. The last run attempt
spends instead of deferring, since nothing reruns after it; a spent window there ends as
the rate-limited final attempt, which stays pending. For a non-group job that is a
residual too: nothing completes it automatically, and the operator command
`gh workflow run ci-queue-gate.yml -f pr_number=<n>` (or, for a dev PR, a new push or the
next sweep) does. A group job
(plain `--queue`, `--head-worker group`, the group repair and the worker dispatch) may
spend into the reserve and is rerun first. The number is the measured group cost (about 41
calls) times two overlapping merge groups (`max_entries_to_build` is 1, and merge CI of
36 to 73 minutes lets at most two finish in one window) times three attempts: 246,
rounded to 250. It lives in `groupReserve` (`scripts/ci/pr-gate/reserve.go`),
`reserve=250` in `ci-queue-retry.yml` and `scripts/ci/discover-dev-prs.sh`. A deferred
job writes nothing, so a record already on the head stays as it is until the rerun; a
feedback rerun's pending barrier can therefore be delayed by up to one window. A group
job is rate-limited much less often, not never. The reserve covers the completed
passes of two overlapping groups (2 x 3 x 41 = 246). On top of that come the pass on a
group's `requested` callback (measured on the fixtures: 17 reads and 1 write, about 18, so
about 36 more for two groups), the worker dispatch (a group job, which never defers),
the other workflows (under about 100 an hour, which do not check the reserve) and
concurrent non-group jobs that all pass the check together and then spend 42 each. Group
demand alone can exceed 250 within an hour, and so can the overshoot of those others.
`R` was not enlarged for them (that would shift every threshold); the residual is
completed by
`gh workflow run ci-queue-gate.yml -f pr_number=<n>`, which also completes a group
job that reached attempt 3 rate-limited (nothing reruns it automatically). The dispatch
job is deliberately not made to defer: a failed dispatch is not a job the retry follows,
so deferring it would leave group work undispatched until the next cron.

The pending and refusal records say only that the publisher did not finish (it
failed or ran out of its budget) and that merging stays blocked. They name no
cause, because every error reaches them, and the refusal says to fix an error that
persists. A rerun is promised only while attempts remain; the records name what
reassesses the head: a new push to this PR, a
feedback result, a push to `main`, the schedule, or `gh workflow run gate-dev -f
pr_number=N` (`gate-queue` for queue admission). A push to `dev` is not named: it
reassesses nothing unless it changed the dev publisher. A trace whose feedback or
protection read never returned has nothing to compare there, so that is not movement
and attempt 3 publishes the refusal. A protection read that returned disabled, or a
queue that is off in the reserve, is a different case and is judged as before. A dev
PR that reaches the queue gate gets no budget refusal from here: its pending record is retired by the
next pass, and retirement skips completed records. The last attempt can still leave a
pending record when the budget ends before a trace exists, when the reserve can
neither read nor write, or when the tuple truly moved. That record keeps the text of
the last write that reached it, so it can be the plain fail-safe text, which names
nothing that reassesses the head. #1194 narrows the case of a verdict whose
write lands after the work clock.

`gate-dev` does not run on PR `edited` events. On #1167 and #1173, GitHub filed
every `ci-dev-required` record in the oldest Actions check suite on the head,
whichever workflow owned it, and judged the required check only by the newest
suite of each workflow. A `gate-dev` run for a title or body edit on the same
head therefore hid the record and left the merge waiting for an "expected" check
until a new head arrived. A base retarget reruns `ci-feedback`, and its callback
reassesses the PR. When that run does not start, because the PR conflicts with
its new base or a fork run awaits approval, the hourly schedule reassesses it.
Closing and reopening a PR still reruns the workflow that hosts the record, so a
reopened PR needs a new head. See the [handoff](../handoff/dev-gate-stale-required-check.md).

GitHub judges every PR of a stack by the rules of the stack base, so each layer of
a stack based on `dev` waits for `ci-dev-required` (#1245 to #1247 and #1137 on
2026-11-07), although its own base is a lower layer's branch. The dev publisher
reads the `stack` field GitHub adds to such a PR. A layer whose `stack.base.ref` is
`dev` is discovered, assessed and published like a dev PR, against the live `dev`
tip. The PR keeps its real base for everything GitHub itself identifies: the
`ci-feedback` run association, role matching and movement checks. The result binds
to the real base as well as the `dev` tip, so retargeting the layer to `dev` is a
new tuple. A stack on `main` or any other branch gets nothing from dev admission.
A malformed `stack` field makes dev admission refuse that PR, and discovery does not
select a PR through its stack, so one bad stack affects neither other PRs nor the
main queue.

A layer's own file list starts at the layer below, so it would hide a lower layer's
CI definition edits that the layer carries into `dev`. For a stacked layer, both the
dev CI-definition check and the `.github/` maintainer-review gate read the compare of
the resolved `dev` tip against the head instead. The publisher passes that tip to
`scripts/ci/check-fork-validation.sh` as `PR_GATE_FILES_BASE`, so both judge one
tuple. A compare that omits its file list, or lists 300 files or more, fails closed,
and the BDFL exemption applies unchanged. Direct dev PRs keep the pull request file list. The DCO and
publisher-contract checks cover the same `dev...head` range. Signature
verification belongs to the GitHub ruleset, not to this gate.

Discovery lists every open PR and selects those whose base or stack base is `dev`.
The `gate-dev` `discover` job still skips `pull_request_target` events for these
PRs, because the publisher contract requires that workflow file to be identical on
main, the base and the head, so it cannot change in a PR. The `ci-feedback`
callback, the hourly schedule and manual dispatch run main's tooling and assess the
stacked heads. A stacked PR that shares its head with another PR is ambiguous,
exactly as two dev PRs would be.

`gate-queue` treats a stacked PR like a dev PR. Only main has a queue, so the
`ci-feedback` callback publishes no `ci-queue-required` record on a head whose PR
targets any other branch, and it retires a pending record left by an earlier pass.
Such a PR holds no queue role: it neither protects a queue record from retirement
nor makes a main PR on the same commit ambiguous. A refusal that an older publisher
left on a stacked head stays until the next push, because retirement skips
completed records. A stack's later retarget to main reruns `ci-feedback`, whose callback
admits the PR afresh.

`gate-queue` treats a stacked PR like a dev PR. Only main has a queue, so the
`ci-feedback` callback publishes no `ci-queue-required` record on a head whose PR
targets any other branch, and it retires a pending record left by an earlier pass.
Such a PR holds no queue role: it neither protects a queue record from retirement
nor makes a main PR on the same commit ambiguous. A refusal that an older publisher
left on a stacked head stays until the next push, because retirement skips
completed records. A stack's later retarget to main reruns `ci-feedback`, whose callback
admits the PR afresh.

The `gate-queue` publisher answers only callbacks whose run path is
`.github/workflows/ci-feedback.yml`. `ci-feedback-retarget.yml` shares the name
`ci-feedback` so that `gate-dev` selects it, but a run of it is not feedback: the
publisher's job condition skips it, where its controller would refuse it as a foreign
callback and leave a red run on the default branch's tip. A test keeps that list
equal to the paths the Go check accepts, so a backmerge that teaches the check a new
path fails until the condition follows. A retarget to main still
reaches `gate-queue` through its `edited` pull request event. `gate-dev` has no
`edited` trigger, so its discovery and publisher accept a callback from either
`ci-feedback.yml` or `ci-feedback-retarget.yml` (event `pull_request`, an exact open
dev PR head); any other path is refused. Such a callback run belongs to the default
branch commit, not the PR head, so it cannot supersede the suite hosting
`ci-dev-required`.

Discovery on a push to `dev` selects no PR. A green `ci-dev-required` stays valid
when `dev` moves: the check is not strict about the base, and GitHub itself refuses
a merge that now conflicts. It reassesses every open PR only when the push changed
`.github/workflows/ci-dev-gate.yml`, since the publisher contract compares the
base's copy against main's and a backmerge that repairs it must release PRs
refused earlier. The push's `before` and `after` are compared through the compare
API. A zero or missing `before`, a force push, an invalid SHA, a failed compare, a
range that is neither ahead nor identical, an ahead range with no listed files or a
file list at GitHub's 300-file limit all reassess every PR. A push to `main`, feedback callbacks, manual
dispatch and PR events are unchanged. The schedule re-proves at most five
open dev PRs per run (`DEV_SWEEP_CAP`). The window rotates on a cursor that advances
once per executed run: the number of `gate-dev` schedule runs (one REST read of the
workflow's run list, `event=schedule`, `total_count`), because the real schedule runs
6 to 12 hours apart and a clock cursor repeats windows. PRs are sorted by number and
the start is count x 5 mod open. It reads no check record and writes no state, because
records are rewritten by events and by re-proofs that change nothing, and an order
based on them can starve a PR. Every open PR is re-proved within ceil(open/5)
consecutive runs; a PR opened or closed in between shifts the windows by at most one
cycle. A run that fails or is deferred still advances the count, so the window it skipped
is reached ceil(open/5) runs later: the bound is in proving runs, not in all runs. GitHub
returns at most 1,000 results for a filtered list, so the count can stop moving, and
expired run history can lower it; from 1,000 on the run number of the current run
(monotonic, no read) is the cursor, and a count that plateaus below that by retention is
the one case this does not cover (not observed, and nothing correct depends on the
sweep). If the count cannot be read the clock hour stands in. Unless a bucket can afford
one publisher above the reserve (292) the sweep selects nothing. The cap delays drift detection and recovery of a
missed event by up to that many hours; it removes neither, and no correctness depends
on it, because events and the retry path settle checks first. An empty selection leaves the run green and
skips the publisher job.

A dev publisher whose final checks see `dev`, the rules, the feedback attempt, the
fork vouch or the verifier result change publishes its pending barrier and fails, so
the retry reassesses that head within minutes. A moved head instead leaves the job
green, since the new head's own event reassesses it. When only the verifier or vouch
changed, the reserve replaces the barrier with its pending record, and on attempt 3
with the terminal refusal. Any other pending record, or a refusal that a later dev
commit would clear, including one from a transient verifier failure on the first pass,
waits for the next PR event, feedback callback, `main` push, scheduled run or dispatch. GitHub throttles
the schedule to hours, so dispatch when it matters:
`gh workflow run ci-dev-gate.yml --ref main -f pr_number=N`.

The final authority reads of an admission (policy verifier, fork vouching, DCO,
feedback) run at once and are judged in their former order; the final role
inventory is still read last. None of them is skipped or reused.

The first pass overlaps its independent reads the same way. Once the policy is
live, feedback, the role inventory, the main route check and the dev publisher
contract run together and are judged in their former order; the policy verifier
still starts only after the feedback and role inventory are judged, so a failed
read, a stale head or a group collision spends no verifier run. A read the serial
order would have skipped can still hang until the work deadline and replace a
refusal with the budget message, which blocks just the same. Dev reads main's tip
and publisher bytes beside the ruleset discovery, reads the three publisher copies
together and runs its CI-definition check beside DCO (DCO is judged first). Both
gates read the final PR beside the final policy (a failed or closed PR is judged
first). Dev admission starts from the PR
its routing already read; the final phase re-reads the PR and compares it with
that observation, so the proof is unchanged. Measured locally in dry-run, a dev
admission took about 25 s before and 17 s after on an 83-file PR with workflow
edits, and about 22 s before and 13 s after on small PRs. Runners read faster, so
the hosted saving is smaller, and a slow setup can still overrun and rely on the
rerun. The policy verifier (2.0 s, or 3.6 s when the PR edits workflows) runs twice
and is now the largest remaining cost. Its repeated PR reads are deliberate re-proofs and stay.

Queue publishers retry GitHub reads that fail with HTTP 429, 500, 502, 503 or
504, or with a `gh` failure that reports no status (except authentication and
cancellation): three attempts with 1 and 3 second backoff, never past the
invocation deadline. Writes are never retried. Discovery dispatch, retirement
discovery keep single-attempt reads. After its own check-run write, any publisher
invocation may re-list or re-read while GitHub still trails that write (0.5, 1
and 1.5 s). The barrier and failure confirmations of one pass share one 3 s
budget, and each retirement readback has its own. A different record or a spent
budget refuses as before.

The full-group callback and the group worker are the only invocations that
wait.
- When a successful merge-group run's proof jobs are still listed as
  unfinished or missing, they poll that run's jobs every 20 seconds, then
  repeat the complete reassessment once.
- When other reads keep failing transiently, the full reassessment repeats
  after 20, 40, 80 and then 120 seconds.
- A primary rate limit (HTTP 403 or 429 with the core, or for GraphQL the graphql,
  budget spent) is waited out once when its reset plus 5 seconds fits the job's
  budget (`PR_GATE_DEADLINE`, five minutes from the group step's start); otherwise
  the read fails naming the reset time. A secondary limit ends waiting at once.
  GitHub's GraphQL docs say a primary limit answers HTTP 200 and a secondary one
  200 or 403
  ([GraphQL rate limits](https://docs.github.com/en/graphql/overview/rate-limits-and-query-limits-for-the-graphql-api));
  both shapes are handled: an `errors` entry of type `RATE_LIMITED` (the docs name no
  type; GitHub sends this one) or whose message names the rate limit, and a 403 or 429
  whose gh stderr names it.
- A pass that ends on a rate limit never publishes a terminal refusal, even on the
  last run attempt: it leaves the check blocking and pending for a later window.
  Nothing reruns attempt 3 automatically, so a merge-group head in that state is
  completed by the operator command `gh workflow run ci-queue-gate.yml -f pr_number=<n>`;
  the reserve is what makes a group job reaching it rare. A rate limit counts when any
branch of a joined error is one: the work error is joined before the block error, and
the first match alone would miss it.

If GitHub stays incomplete or unreadable for 6 minutes, the publisher writes
the distinct terminal refusal "Merge group proof metadata unavailable", so the
PR can be re-queued instead of waiting for the queue's 120-minute timeout. The
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

The lifecycle probe that gated this activation has been removed now it has served
its purpose. Before re-enabling `QUEUE_HEAD_WORKERS_ENABLED` after any rollback,
restore `ci-check-lifecycle-probe.yml`, `scripts/ci/pr-gate/check_lifecycle.go`, its
`lifecycleName` allowance in `queue_publication.go` and the `--check-lifecycle-probe`
flag together from Git history, and require a passing probe first. The staged publication
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

Race weights are recalibrated from all twelve `race-timings-*` JSON artifacts of
ci-dev run 44901528773, attempt 1, on dev 4c7d11f6c; all twelve race shards
passed. Complete target discovery and database serialization are unchanged.
The planner prefers shards where each package's modelled `go test` invocation
stays within a 600-second budget, half of the 20-minute `-timeout`, before it
balances shard totals. The budget is a preference, not a guarantee: when no
shard fits, `bestRaceShard` places the unit on the cheapest shard. The planner
test fails if the repository's plan has any invocation over the budget, and it
schedules every shard the way `test-race-packages.sh` runs it (two `xargs`
lanes in shard order, then the filtered app and service suites one at a time),
requiring 1.5 times that wall plus 120 seconds of job overhead to fit the
30-minute job limit. Twelve shards schedule up to 1196 seconds on the Linux
inventory, 1914 seconds with that margin. The race shard count is 18 because a
merge group plans with the base branch's planner but takes the shard count from
the merge ref. Main's previous planner put race shard 6 at the job limit in
merge groups 45168962693 and 45175593611, at least about 1.2 times its
scheduled wall (the app suite was cancelled 287 seconds in). At 18 shards that
planner schedules at most 1101 seconds, 1772 seconds with margin, only about 28
seconds under the limit. Its greedy placement is sensitive to the inventory:
adding one 1-second package to main's tree moved its worst 18-shard plan to
1189 seconds, 1903 seconds with margin. That residual risk lasts until this
planner lands on main, because the ceiling test checks only the current
planner.
The current planner schedules at most 801 seconds at 18 shards, 1321 seconds
with margin.
This model is not hosted latency proof. Merge groups plan with the base
branch's planner, so the new weights take effect after they land on main and
are backmerged to dev.

Trusted-main probes demonstrated that omitting terminal reset fields retains
completed/success and that the explicit-null candidate is rejected. Those
canaries were cleaned up to neutral; neither protocol authorized activation.
The immutable-verdict protocol instead uses creation and nonterminal updates;
its actual workflow-token acceptance was independently verified before activation.
The legacy `ci-required-next` publisher is retired. The hourly sweep cancels
unfinished records on recent main commits. Those on closed or superseded PR heads
stay queued until someone dispatches that exact head. None are required checks.

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
failure does not remove them. Likewise, failed non-required diagnostics
record diagnostic failures, even after product validation passes. A green main
summary or a neutral canary does not erase an earlier failed result, and a stale
merged-head placeholder does not authorize a later merge. Inspect the current
required proof and resolve actual failures without deleting or bypassing checks.

Useful feedback and dependency checks supersede earlier revisions of the same
PR. Irrelevant metadata events have separate concurrency groups.

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
Older history uses exact-head backfill. REST lists a commit's runs from only
its newest 1,000 check suites, so a candidate with more than 1,000 latest check
records or more than 900 suites (counted in the history query, with a margin for
suites added before REST reads it) is read differently: a GraphQL scan of its
`github-actions` suites compares each suite's latest and finished run counts,
and REST reads only the suites where they differ. Suite status is not used;
GitHub reports a suite completed while a synthetic run in it is still queued.
The retire and reporter owners read the same way: past 900 suites (a count read
before every listing) they list a check's runs only in the `github-actions`
suites that hold runs of its name, one REST listing per suite, so a context on an
oversized commit is routed, retired and found by the reporter like any other.
Queue admission lists a head's checks at least three times inside its 45-second
read budget and each listing repeats the scan (about 1.4 s per 100 suites for the
owners' scan). Past 900 suites admission may time out; at 1,000 the scans alone
take about 42 s. It then fails closed and the PR needs a new head.
Because those reads open only `github-actions` suites, the owners also list the
name by commit and refuse another app's same-name record there, as a listing by
commit alone did, each by its own rule. The queue verdict refuses any other
app's same-name record. Retirement refuses one only when its external ID carries
the controller prefix. The reporter refuses one only when its external ID matches
the report's. They refuse when the `github-actions` suite total changes during a
scan, so a head still gaining suites can refuse until a later attempt. The
controller and the queue verdict refuse a `github-actions` record the listing by
commit shows but the suite inventory lacks, instead of treating it as absent,
whether or not its external ID carries the controller prefix; the reporter
compares only records with the report's external ID. At 900 suites or fewer each
owner reads the commit listing, then counts every app's suites again and
refuses if the count has passed 900, since REST could then have left an older
suite out of the listing.
Both use the shared `scripts/ci/checksuites` reader. A suite inventory that
cannot be completed, a run reported by two suites, or a check in more than 100
suites refuses instead of listing partially. Discovery refuses all targets on
API failures, incomplete pages, more than 5,000 `github-actions` suites or 100
unfinished suites on a candidate, more than 1,000 records in one suite's listing
or more than 100 target heads. A commit still gaining checks, such as a busy main
head, can change its totals mid-scan and refuse until the next run. A full window
takes about three to four minutes, so discovery has a five-minute deadline. Each
100 suites scanned adds about 2 s and history pages run one after another, so the
deadline, not the suite cap, bounds oversized commits: about 40 more suite pages
per window than today's 36. Running out of time after suite pages reports how many
pages and history pages were read. Window completion is not
evidence that all history was cleaned.
Legacy observer heads run in batches of at most ten unique heads concurrently
under their one global lease and compiled executable. Every child is awaited,
then failures are reported together. This bounds batch duration by the slowest
controller rather than ten sequential deadlines without adding runner jobs.
Targeted manual dispatch accepts one full `head` SHA.
See the [repair handoff](../handoff/ci-retirement-2026-10.md) for measurements,
historical limitations and verification commands.
