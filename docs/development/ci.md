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
| Refresh the bounded feedback compiler caches | `cache-seed-feedback-backend` | Compiles the feedback lanes' Go exports without DWARF into fresh dedicated directories, checks each entry's size and file count against the existing bound, and publishes version-2 entries: three per family, each under the existing 1,610,612,736 byte and 12,000 file bound: `application` (`internal/app`) and `cli` (`internal/cli`), both restored by the go lane, and `tooling` (`scripts/ci`, `internal/boundary`, `internal/isolation`, `internal/lint` and the pinned `actionlint`), restored by the tooling lane. The whole graph is about 1.6 GB, so no single entry can hold it. Two trust families, named in every key: `main` (daily or manual, from main) is read only by feedback runs on PRs that do not target dev; `dev` (a push to dev, at most one seed per UTC day, or a manual run on dev) is read only by runs on PRs that target dev. The reader picks its family from the PR's base branch and its entry from the lane's Go scope, and neither restore prefix matches the other family. Seeds publish only from those two refs, from the immutable pushed commit with a read-only token; pull requests, including their edited workflows' own runs, never publish. What separates the families is GitHub's ref scoping: a PR reads entries saved on its base branch and on the default branch, nothing else, and only trusted refs can write. The family string in the key and prefix keeps the prefixes honest and covers prefix fallbacks, but it is not the boundary: GitHub's `==` on strings ignores case, so a base branch named `DEV` takes the `dev` family string, finds no dev-saved entries in its own ref scope, and builds cold. Go does not re-hash a restored object: it looks up an action ID and checks that the output exists with the right size (only `GODEBUG=gocacheverify=1` re-hashes), so the guarantee is that only trusted refs can write, not content verification. After a successful seed a separate `prune` job (`actions: write` only, no checkout, no repository code, same trusted guard) deletes the superseded entries of the same ref, family and entry by exact key prefix and cache id, keeping the entry just saved and the newest one, and treating a 404 as done; the compiling job keeps a read-only token. Steady-state footprint (compressed): the 2026-11-25 measurement is two main `go-feedback-v2` entries of 306 MB each (a 0.20 ratio on the roughly 1.54 GB they hold); applied to the measured raw sizes this is an estimated 0.69 GB for the main family and 0.76 GB for the dev family, about 1.45 GB for both, replacing the old 0.3 to 0.6 GB single entry. Each family holds one set once its prune has run; a set exists twice only between a save and its prune. Repository usage was measured at 10.59 GB over 37 entries, already above the 10 GB budget before this change; the repository's limit setting is not readable with the workflow token (HTTP 402), so 10 GB is the documented default, not a measurement. The two old unfamilied `go-feedback-v2-<os>-<arch>-<abi>-<hash>-<sha>` entries (612 MB) match neither family's prune prefix and expire or are evicted on their own; deleting them after the first main seed frees that space. Until the backmerge, dev's old reader prefix also matches the new main-family entries (it names no family), so dev PRs restore a main entry; after it, dev PRs build cold until the first dev push seeds. The go lane restores two entries (`application` and `cli`, about 2.4 GB and 18,500 files expanded): read their hosted restore times from the first warm run (`gh run view <run> --json jobs`, the two restore steps) and the tooling lane's from its own, instead of the 4 to 5 s of the old single 321 MB entry. The seed workflow runs from the pushed ref, so a change to it reaches dev through a backmerge before dev pushes seed. Dispatch it manually (`--ref main`, `--ref dev`) after the workflow changes or a `go.mod`/`go.sum` change lands. |
| Refresh the production-fixture compiler cache | `cache-seed-feedback` | Daily or manual, trusted main only. Publishes the version-1 CLI/CI seed with default compiler flags. Only `source-artifact-security` in the full suite reads it; feedback no longer does. |
| Refresh the shared race compiler cache | `cache-seed-race` | Daily or manual, trusted main only. Compiles every race-instrumented package and test dependency without running tests, then publishes the single cache that all merge-group race shards and the reliability race suite restore. It is also the daily second writer of `go-mod-v2` (restore, `scripts/ci/populate-go-module-cache.sh` on a miss, save from trusted main only; `ci.yml` runs the same script, so the two writers save the same contents), so an evicted module cache refills within a day instead of at the next Sunday `ci-main` run. Run it manually after a `go.mod`/`go.sum` change lands. |
| Refresh the lint driver compiler cache | `cache-seed-lint` | Daily or manual, trusted main only. Prepares the lint matrix compiler cache and runs the `internal/lint` tests in a fresh cache, then publishes one entry per ISO week that `test_core_serial` and `ci-advisory-invariants` restore by prefix. Its key follows `go.mod`, `go.sum`, the tool modules, the runner toolchain and the week, so a seed is at most a week stale. Expect up to two seeds during a weekly rotation, plus one per module change; superseded seeds go idle and age out after seven days. Run it manually after a dependency or toolchain change on main. |
| Refresh the pinned tool downloads | `cache-seed-tools` | Daily or manual, trusted main only. Downloads each checksum-pinned release file (ShellCheck, kind, Cosign, Node, Helm, the Calico manifest) with bounded backoff and publishes one entry per `scripts/ci/pinned-tools/<name>.pin`. `lint`, `supply-chain-checks`, `k8s-e2e`, reliability and `ci-feedback` restore it so a GitHub release outage does not fail them. Dispatch it manually after a pin file changes. |
| Prune the repository cache | `cache-prune` | Hourly, manual, or minutes after a cache writer finished on main (CodeQL, `ci-main`, the lint, race, tool and feedback seeds), trusted main only, `actions: write` and nothing else, no checkout. Deletes the superseded entries of the regenerable cache families and idle pull-request entries so the 10 GB repository limit does not evict live entries; see Repository cache budget below. |
| Refresh the dev feedback seed | `cache-refresh-dev-seed` | Daily (cron 03:44 UTC, which in practice runs hours later, see the budget section) or manual, trusted main only, `actions: write` and nothing else, no checkout: runs `gh workflow run ci-feedback-backend-cache.yml --ref dev`. Schedules run on the default branch only, so dev has no cron; without this the dev seed is last used on the Friday before a quiet weekend and is the first entry the Monday lint driver rollover evicts. The dispatched run is a dev run in every respect (it checks out dev, the key carries the dev family and the UTC day, only the dev ref can save it), so the trust families stay apart. A run takes about 3.6 minutes when it compiles (measured on the main seed) and 0.2 when the day already has a seed. |
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
| Trusted gates | `ci-dev-gate.yml`, `ci-queue-gate.yml`, `ci-queue-worker.yml`, `ci-queue-retry.yml` | Trusted callbacks publish dev admission and queue admission/group proof; the retry workflow reruns a failed head publisher (see [Queue publisher head leases](#queue-publisher-head-leases)). Queued checks do not hold a polling runner; only a completed merge group whose GitHub job metadata lags, or whose reads fail transiently, is re-read for a bounded window. |
| Retired checks | `ci-check-retirement.yml` | Trusted asynchronous cleanup of authenticated unfinished synthetic records, with the existing writer leases and live-role checks. Never grants merge proof. |
| Superseded runs | `ci-run-retirement.yml` | Bounded asynchronous cancellation of authenticated obsolete merge-group runs. Preserves active PR/group/default-branch roles and verifies terminal readback. |
| Security feed freshness | `security-feed-freshness.yml` | Reads what installations read, on its own schedule and credentials, because the publisher cannot be trusted to report its own staleness. Inert until `HIKYO_SECURITY_FEED_ENABLED` is `true`. |
| Reporting | `dco-report.yml`, `fuzz-report.yml` | Write-capable reports execute trusted code separately from PR validation. `fuzz-report.yml` also keeps the one issue open while dev is red. |
| Queue diagnostics | `ci-queue-report.yml` | Mirror merge-group progress onto the PR head (title shows failures and progress; the summary links the run). Neither grants merge authority. |
| Performance | `benchmark-control.yml`, `pr-benchmark.yml`, `codspeed.yml`, `matrix-performance.yml`, `floor-bench.yml` | Budget control, PR measurement, main measurement, browser measurement, and native floor enforcement retain distinct trust/hardware contracts. |
| Maintenance | `reliability.yml` | Original daily security and weekly integration/race/channel checks, plus one manual menu. |
| Compiler cache seeds | `ci-feedback-cache.yml`, `ci-race-cache.yml`, `ci-lint-cache.yml` | Trusted-main-only producers of Go build entries. (`ci-feedback-backend-cache.yml` is the one trusted-branch producer that also publishes from dev; see the first table.) PR feedback, source-artifact validation, race shards, the reliability race suite, `test_core_serial` and the advisory invariants job only read them. One shared race entry replaces twelve per-shard entries that could not fit the 10 GB repository cache budget together. |
| Pinned tool seed | `ci-tool-cache.yml` | Trusted-main-only producer of the pinned release-file entries. Every other workflow only restores them, and the helper verifies each file's SHA-256 before use, so a poisoned or stale entry cannot run. |
| Cache prune | `ci-cache-prune.yml` | Scheduled, manual, and after a named writer workflow completed on main (`workflow_run` of `CodeQL`, `ci-main`, `cache-seed-lint`, `cache-seed-race`, `cache-seed-tools`, `cache-seed-feedback`, filtered to `branches: [main]`; the job guard also requires that run's head branch to be main in this repository, and nothing else is read from it). Its concurrency group sits on the job, after the guard, so a skipped run cannot replace a pending real prune. Holds `actions: write`, checks nothing out and runs no repository code. A `workflow_run` job is the only way to give those writers a post-save prune without `actions: write` in a job that runs PR code (`ci-main` reuses `ci.yml`) or in a workflow that is not ours (CodeQL default setup). Superseded: of the listed regenerable families (`go-mod-v2`, `go-test-v2`, `go-race-v2`, `go-lint-v2`, `go-lintdriver-v2`, `go-generated-v2`, `go-k8s-e2e-v2`, `go-fuzz-v2`, `go-feedback-v1`, `pinned-tool-v1`, `pnpm-store-v2`, the CodeQL `overlay-base-database`, `dependencies` and `trap` entries), every entry on `refs/heads/main` or `refs/heads/dev` except the newest of its ref and lane (the key without its trailing hash, commit, attempt or week, the runner image, the tool version and the config hash, so an image or CodeQL bump retires the old lane at once). Idle: entries on `refs/pull/*` not accessed for 24 hours. The `go-feedback-v2` seeds are not listed: the backend seed's own prune keeps the newest set per family and entry. Other refs and unlisted families are never touched. One to two listing calls and at most 100 deletes per run: 24 scheduled runs a day plus a few per writer, at most 102 calls a run against the shared 1000 an hour. |
| Release acceptance | `release-acceptance.yml`, `ops-floor.yml` | Manual secret-bearing acceptance is separate from PR-capable operations validation. |
| Publication | `nightly-promote.yml`, `nightly.yml`, `nightly-manual.yml`, `release.yml`, `apple-release-recovery.yml`, `security-release.yml`, `security-feed.yml`, `docs.yml` | Promotion, nightly, stable tags, the security feed, and Pages retain separate credentials and gates. Promotion opens its PR as the nightly App, because a PR opened with the workflow token starts no CI. |

### Repository cache budget

GitHub documents a default limit of "10 GB" per repository and does not say whether that is 10^9 or 2^30
bytes, so everything below is given for both: 10.00 GB and 10 GiB (10.74 GB). The limit can be raised
in the repository's Actions settings, with usage beyond 10 GB billed; the storage-limit endpoint
answers 402 to the workflow token, and the limit is not raised, by owner decision, and no setting is changed here. Past the limit GitHub saves the new
entry and then evicts by least recent access, whatever the entry is. Enforcement is lazy in practice:
the repository sat at 10.646 GB, over the limit, in two listings 12 hours apart with entries from 11-10
still present. `ci-cache-prune.yml` removes the dead entries first, so the model below, which evicts at
the instant of the save, is the worst case.

**Before this round the budget did not fit.** Everything is measured unless marked estimated.

| Demand once the prune has run (the low point) | GB | Source |
| --- | --- | --- |
| 26 entries from the 2026-11-25 listing after the rules and the by-hand deletion | 8.18 | measured |
| `pnpm-store-v2-host` and `-playwright`, 0.205 each | 0.41 | host measured (zstd, macOS arm64), playwright estimated equal |
| Main and dev feedback seeds | 1.45 | estimated: 0.69 and 0.76 from 306 MB per entry measured |
| Total | 10.04 | margin -0.04 GB at 10.00 GB, +0.70 GB at 10 GiB |

**Adopted:** DWARF off in the k8s-e2e and lint driver compiles, `go-mod-v2` archiving `cache/download`
only, the prune also right after each writer, and a daily dev seed refresh. Measured on macOS arm64
with a fresh build cache and `tar | zstd -3`; the live figures apply the measured ratio to the 11-25
entry, so they are estimated.

| Measure | Measured | Live entry (estimated) | Cost |
| --- | --- | --- | --- |
| `GOFLAGS=-gcflags=all=-dwarf=false` in the k8s-e2e job; key suffix `-nodwarf` | 898 to 755 MB (x0.84) | 1.62 to 1.36 GB (-0.26) | none for a run; the binaries lose DWARF |
| The same flag in `internal/lint` `BuildContext.Env` (`loaderGOFLAGS`, replacing the cleared `GOFLAGS`); weekly key suffix `-nodwarf` | 1135 to 905 MB (x0.80) | 1.71 to 1.37 GB (-0.35); also the weekly overshoot | the seed re-saves at its next run; readers fall back to the old entry until then |
| `go-mod-v2` archives `~/go/pkg/mod/cache/download` in the two writer steps (`ci.yml` and `ci-race-cache.yml`, both filled by `scripts/ci/populate-go-module-cache.sh`) and the 25 restores | 663 to 372 MB (x0.56; 791 to 437 with a toolchain in the tree, x0.55) | 1.03 to 0.58 GB (-0.45) | see below |
| Prune after each writer (`workflow_run`) | n/a | window in which two generations coexist drops from up to 60 to about 2 minutes | none; the replay shows no change in peak |
| Daily dev seed refresh (`cache-refresh-dev-seed`) | main seed run 3.6 min (measured) | keeps the dev seed the newest-accessed entry before the Monday rollover | one 0.76 GB save and one prune a day |

Cost of the module cache change, measured on the same macOS arm64 host at load average 10 to 15 (so
the absolute times are pessimistic and the Linux runner is not measured): restoring the download
archive takes 3 s and the first `go list -deps -test ./...` then extracts what the module needs in 16 s
wall (26 s CPU); a full `go mod download` extracts everything in 23 s wall (49 s CPU). Restoring
today's whole-tree entry takes 185 s wall (134 s CPU) on this host (many small files). Extraction cost
replaces untar cost, so the adoption is judged small: at most the 16 to 23 s above per job, against
a restore that is not smaller on this host. The new entry (a different path, so a different cache
version) is saved by the existing writer on its next miss, and the old whole-tree entry of the same key
is the older one of its lane, so the prune removes it.

Panics and stack traces keep file:line: DWARF and the pc-line table are separate, so
`-gcflags=all=-dwarf=false` and `-ldflags=-w` leave `main.boom ... main.go:5` and `main.go:9 +0x5c`
unchanged in a test program's runtime panic (checked with go1.27.4 by building with each flag and
reading the trace; the binary shrinks from 2.13 MB to 1.72 MB with the compile flag and to 1.47 MB with
`-w`, and only `-w` removes the debug sections).

Eviction model (`scripts/ci/cache_eviction_model_test.go`, `go test -run TestCacheBudgetModel -v`):
the 2026-11-25 listing (`testdata/cache-listing-2026-11-25.json`: real keys, sizes, creation and
access times), the prune workflow's own jq at :47 hourly and two minutes after each writer, the measured
CodeQL overlay creation times, least recently accessed first, a tie against the seeds. Not in the listing
and assumed: entries the listing shows touched within 90 minutes are touched hourly on a weekday and
the others (overlays, kind, buildx) only when created; on a quiet weekend nothing is touched after
Friday 20:30 except what a schedule saves; the old lint driver entry was looked up when the lint run
started, two minutes before it saves.

**A cron time is not a run time.** Scheduled runs on 2026-11-22 to 2026-11-25 (one `gh api` call on
the schedule-event runs; `run_started_at` to `updated_at`) started 8 to 10.5 hours after their cron minute:

| Workflow | Cron (UTC) | Started (UTC), 11-22 to 11-25 | Took |
| --- | --- | --- | --- |
| `cache-seed-feedback` (`go-feedback-v1`) | 03:17 | 11:25, 12:40, 13:42, 12:55 | about 2 min |
| `cache-seed-feedback-backend` (main seed) | 03:32 | 11:46, 13:26, 14:04, 13:18 | 0 to 4 min |
| `cache-seed-race` (race and `go-mod-v2`) | 03:41 | 11:58, 13:39, 14:15, 13:29 | 1 to 7 min |
| `cache-seed-lint` (lint driver) | 04:11 | 14:46 (ended 14:57), 14:12 | 2 to 11 min |
| `ci-main` (Sunday) | 13:23 | 20:24 on 11-22, cancelled at 21:07 | n/a |

The order (feedback, main seed, race, lint) held on every observed day; the gaps did not. The model
replays the saves at the run ends of 2026-11-25 (feedback 12:58, main seed 13:22, race 13:36, lint
14:14, Sunday `ci-main` 20:31); the dev refresh is not deployed, so its time is assumed to follow the main
seed (13:26). Earlier revisions of this section used the cron minutes (03:41, 04:11), which were never
observed. The `ci-main` run of 11-22 was cancelled, so whatever only it writes was not refreshed that week.

| Scenario | Plan | Demand | Peak at 10.00 GB / 10 GiB | Evicted live entries at 10.00 GB / 10 GiB | Newest seeds |
| --- | --- | --- | --- | --- | --- |
| Weekday | before | 10.04 | 10.51 / 11.23 | 16 / 6 | a main seed entry evicted at 10.00 GB (09:10), none at 10 GiB |
| Weekday | adopted | 8.98 | 9.74 / 9.74 | 0 / 0 | both survive |
| Monday lint driver rollover (14:14) after a quiet weekend | before | 10.04 | 11.46 / 11.75 | 4 / 4 (shellcheck, helm, cosign, `go-mod-v2`) | **dev seed evicted** at both limits |
| Monday lint driver rollover | adopted | 8.98 | 10.34 / 10.34 | 4 / 0 | both survive |
| Sunday 20:31 fuzz rollover | before | 10.04 | 10.02 / 10.48 | 0 / 0 | main seed entry evicted on the tie at 10.00 GB |
| Sunday 20:31 | adopted | 8.98 | 9.42 / 9.42 | 0 / 0 | both survive |
| go.mod/go.sum change day, observed times | adopted | 8.98 | 10.26 / 10.34 | 7 / 0 | both survive |
| same, race and lint saves in the same minute (14:14) | adopted | 8.98 | 11.32 / 11.63 | 14 / 12 | both survive |
| same, both at 11:59 (before the day's seeds are re-saved) | adopted | 8.98 | 11.20 / 11.63 | 12 / 9 | **main and dev seed entries evicted** at both limits |
| Sunday 20:31 `ci-main` after a go.sum change (every save at once) | adopted | 8.98 | 12.27 / 12.27 | 8 / 7 | both survive |

New low point 8.98 GB (before 10.04): margin +1.02 GB at 10.00 GB and +1.76 GB at 10 GiB. Peaks 9.74
(weekday), 9.42 (Sunday fuzz) and 10.34 (Monday), against 10.51, 10.02 and 11.46 before. Verdict for
the three ordinary scenarios: the newest main and dev seeds survive a weekday, the Sunday fuzz rollover
and the Monday rollover at both limits. The one residual of those three is at exactly 10.00 GB on
Monday: for the instant in which the old and new lint driver entries coexist (until the prune two
minutes later) the model evicts the four least recently used idle entries (three pinned tools and
`go-mod-v2`); at 10 GiB nothing live is evicted. Without the dev refresh the dev seed is lost again on
Monday at 10.00 GB; without the shrinks the Monday peak is back at 11.46 GB and seven entries go; without
the prune after a writer an ordinary weekday peaks at 10.19 GB (10.26 at 10 GiB) and evicts three entries
at 10.00 GB (the test pins all of these). On Monday and Sunday the prune after a writer changes nothing,
because the model evicts at the moment of the save; it shortens the window in which two generations
coexist, which matters if GitHub's eviction is lazy, as the 12-hour observation suggests.

**Days that rotate keys are not covered by those three.** `go.mod` or `go.sum` changed on main on 7
of the last 30 days (committer date, `origin/main` at 2026-11-25). Then the daily race seed (with
`go-mod-v2`), the daily `go-feedback-v1` save and the lint driver seed each save next to the old
generation until the prune. With the observed run ends the day peaks at 10.26 GB and takes seven idle
entries at 10.00 GB, none at 10 GiB, and both seeds survive. The result depends on timing, and the
timing is not under our control: when the race and lint saves land in the same minute there is no prune
between them, and the peak reaches 11.32 GB (14 entries evicted at 10.00 GB, 12 at 10 GiB) with the
seeds surviving. If that happens at 11:59, before the day's seed run has re-saved the seeds, the model
loses the main seed and the dev seed entries at both limits. That is the worst case this model can show
for an ordinary day, not a prediction: it needs a rotation day and two writers finishing in one minute
(race and lint finished 38 and 41 minutes apart on 11-25 and 11-24; the lint seed has only those two observed runs).

How long a lost seed stays lost: the main and dev seeds are re-saved daily, and the observed run ends
spread over 2.7 hours (11:25 to 14:07). A lost seed is therefore back after at most about 27 hours, and in
the early case above within about 2 hours. The cost is cold feedback builds in that window (the
main seed compile took 3.6 minutes in the measured run), so slower feedback jobs and no loss of
coverage: the same checks run, uncached.

Sunday after a go.sum week. Only the weekly `ci-main` writes the test, lint, generated, k8s-e2e and
fuzz entries, so on every Sunday after a week with a `go.mod` or `go.sum` change it saves about 3.3 GB at
once: module, test, lint, generated, k8s-e2e and fuzz entries, with the old ones restored and so freshly
accessed. That is the usual Sunday after such a week, not a rare one: 3 of the last 4 Sundays followed a
week with a change (the weeks ending 11-01, 11-08 and 11-15; not 11-22). The model evicts 8 live entries
at 10.00 GB and 7 at 10 GiB (peak 12.27 GB), both seeds survive, and the evicted entries are idle ones:
`buildx-dl-bin` (next Docker build), three CodeQL overlays, `codeql-dependencies` and `codeql-trap`
(next CodeQL run on main), `go-feedback-v1` (the daily save), and `go-lintdriver-v2` (the weekly seed
saves it again on Monday, so lint runs without the driver cache until then). `ci-main` saves over its
run, not at one instant, and it was cancelled on 11-22, so the figure is a worst case.

What an eviction costs. An evicted entry is a cache miss, never lost coverage: the job that would
have read it rebuilds from source and the checks run unchanged. Only the weekly `ci-main` writes
`go-test-v2`, `go-lint-v2`, `go-generated-v2`, `go-k8s-e2e-v2` and `go-fuzz-v2`, so an evicted one of
those is missing until the next Sunday `ci-main`, up to six days, and the jobs that read it run cold in
the meantime. The others return sooner: `go-mod-v2` at the next race seed run, `go-race-v2` and
`go-lintdriver-v2` at their seed runs, the pinned tools at the next job that pins them, CodeQL's
entries at its next run on main.

Module cache. `go-mod-v2` has two writers, the daily `cache-seed-race` and the weekly `ci-main` run;
25 restores read it. Before the daily writer, a Monday eviction (the residual above) meant every restore
missed for about six days. Now the entry is lost until the next race seed run, under 27 hours: every job
downloads its modules from the proxy in that time (`go mod download` of the repository and the pinned
tools took 23 s wall on the loaded macOS host above, so a few seconds to a minute a job is the
estimated cost, not measured on a runner).

Removing the Monday instant for real would need the lint driver seed to delete the old week's entry
before it saves, with a cold lint cache until the new one lands; that is the owner's call and is not
done here. `go-feedback-v1` stays: `ci.yml` reads it. CodeQL's overlay and dependency entries are
written by default setup, so their prune is the `workflow_run` trigger on `CodeQL`; GitHub fires
`workflow_run` for that workflow by name, which is not checked here until the first run on main.

Delivery order. The dev refresh does nothing until the backmerge to dev: dev still has the old
main-only guard on the feedback seed, so a dispatched run on dev would not save. Every `go-mod-v2`
restore misses until a writer saves the new `cache/download` entry (the key is unchanged but the path
is part of the cache version); `ci-main` is dispatched by hand after the merge, and the daily race seed
saves it at its next run at the latest. The old whole-tree entries of the same key are the older ones
of their lane, so the prune removes them when the new entry saves.

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
`ci-queue-retry.yml` then reruns only the failed jobs of that run (a new job,
so a new sixty-second budget) when a publisher job's reconcile step failed,
including a job its one-minute timeout then cancelled. Worker group jobs share
the publisher's name and get the same reruns, and so does a failed setup, because
reconcile always runs. The run attempt is the only loop bound: attempt 3 publishes
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
file list at GitHub's 300-file limit all reassess every PR. A push to `main`, the schedule, feedback callbacks, manual
dispatch and PR events are unchanged. An empty selection leaves the run green and
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
suite out of the listing. Retirement discovery does the same after its commit
read and then reads by suite instead of refusing.
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
