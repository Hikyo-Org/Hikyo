# CI guide

Feature PRs target **dev** and receive automatic **ci-feedback**. Required
**ci-dev-required** admission verifies the exact head's three feedback lanes,
signatures/DCO, fork vouch and trusted workflow authority. Dev has no full merge
queue. Each merged dev SHA requests full **ci-dev** integration validation:
one active run finishes, with only the newest pending run retained.

Frozen release candidates target **main** and enter its mandatory single-entry
queue. **ci-queue-required** admits the exact PR head and then requires complete
current-base merge-group proof. Branches under `release/` always select all 16
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
| Refresh the bounded feedback compiler caches | `cache-seed-feedback-backend` | Compiles the feedback lanes' Go exports without DWARF into fresh dedicated directories, checks each entry's size and file count against the existing bound, and publishes version-2 entries: three per family, each under the existing 1,610,612,736 byte and 12,000 file bound: `application` (`internal/app`) and `cli` (`internal/cli`), both restored by the go lane, and `tooling` (`scripts/ci`, `internal/boundary`, `internal/isolation`, `internal/lint` and the pinned `actionlint`), restored by the tooling lane. The whole graph is about 1.6 GB, so no single entry can hold it. Two trust families, named in every key: `main` (daily or manual, from main; the key ends in `<commit>-<UTC day>` so a day on which main did not move still saves, which keeps the seed recently accessed) is read only by feedback runs on PRs that do not target dev; `dev` (a push to dev, at most one seed per UTC day, or a manual run on dev) is read only by runs on PRs that target dev. The reader picks its family from the PR's base branch and its entry from the lane's Go scope, and neither restore prefix matches the other family. Seeds publish only from those two refs, from the immutable pushed commit with a read-only token; pull requests, including their edited workflows' own runs, never publish. What separates the families is GitHub's ref scoping: a PR reads entries saved on its base branch and on the default branch, nothing else, and only trusted refs can write. The family string in the key and prefix keeps the prefixes honest and covers prefix fallbacks, but it is not the boundary: GitHub's `==` on strings ignores case, so a base branch named `DEV` takes the `dev` family string, finds no dev-saved entries in its own ref scope, and builds cold. Go does not re-hash a restored object: it looks up an action ID and checks that the output exists with the right size (only `GODEBUG=gocacheverify=1` re-hashes), so the guarantee is that only trusted refs can write, not content verification. After a successful seed a separate `prune` job (`actions: write` only, no checkout, no repository code, same trusted guard) deletes the superseded entries of the same ref, family and entry by exact key prefix and cache id, keeping the entry just saved and the newest one, and treating a 404 as done; the compiling job keeps a read-only token. Steady-state footprint (compressed): the 2026-11-25 measurement is two main `go-feedback-v2` entries of 306 MB each (a 0.20 ratio on the roughly 1.54 GB they hold); applied to the measured raw sizes this is an estimated 0.69 GB for the main family and 0.76 GB for the dev family, about 1.45 GB for both, replacing the old 0.3 to 0.6 GB single entry. Each family holds one set once its prune has run; a set exists twice only between a save and its prune. Repository usage was measured at 10.59 GB over 37 entries, already above the 10 GB budget before this change; the repository's limit setting is not readable with the workflow token (HTTP 402), so 10 GB is the documented default, not a measurement. The two old unfamilied `go-feedback-v2-<os>-<arch>-<abi>-<hash>-<sha>` entries (612 MB) match neither family's prune prefix and expire or are evicted on their own; deleting them after the first main seed frees that space. Until the backmerge, dev's old reader prefix also matches the new main-family entries (it names no family), so dev PRs restore a main entry; after it, dev PRs build cold until the first dev push seeds. The go lane restores two entries (`application` and `cli`, about 2.4 GB and 18,500 files expanded): read their hosted restore times from the first warm run (`gh run view <run> --json jobs`, the two restore steps) and the tooling lane's from its own, instead of the 4 to 5 s of the old single 321 MB entry. The seed workflow runs from the pushed ref, so a change to it reaches dev through a backmerge before dev pushes seed. Dispatch it manually (`--ref main`, `--ref dev`) after the workflow changes or a `go.mod`/`go.sum` change lands. |
| Refresh the production-fixture compiler cache | `cache-seed-feedback` | Daily or manual, trusted main only. Publishes the version-1 CLI/CI seed with default compiler flags. Only `source-artifact-security` in the full suite reads it; feedback no longer does. |
| Refresh the shared race compiler cache | `cache-seed-race` | Daily or manual, trusted main only. Compiles every race-instrumented package and test dependency without running tests, then publishes the single cache that all merge-group race shards and the reliability race suite restore. It is also the daily second writer of `go-mod-v2` (restore, `scripts/ci/populate-go-module-cache.sh` on a miss, save from trusted main only; `ci.yml` runs the same script, so the two writers save the same contents), so an evicted module cache refills within a day instead of at the next Sunday `ci-main` run. Run it manually after a `go.mod`/`go.sum` change lands. |
| Refresh the lint driver compiler cache | `cache-seed-lint` | Daily or manual, trusted main only. Prepares the lint matrix compiler cache and runs the `internal/lint` tests in a fresh cache, then publishes one entry per ISO week that `test_core_serial` and `ci-advisory-invariants` restore by prefix. Its key follows `go.mod`, `go.sum`, the tool modules, the runner toolchain and the week, so a seed is at most a week stale. Expect up to two seeds during a weekly rotation, plus one per module change; superseded seeds go idle and age out after seven days. Run it manually after a dependency or toolchain change on main. |
| Refresh the pinned tool downloads | `cache-seed-tools` | Daily or manual, trusted main only. Downloads each checksum-pinned release file (ShellCheck, kind, Cosign, Node, Helm, the Calico manifest) with bounded backoff and publishes one entry per `scripts/ci/pinned-tools/<name>.pin`. `lint`, `supply-chain-checks`, `k8s-e2e`, reliability and `ci-feedback` restore it so a GitHub release outage does not fail them. Dispatch it manually after a pin file changes. |
| Prune the repository cache | `cache-prune` | Hourly, manual, or minutes after a cache writer finished on main (CodeQL, `ci-main`, the tool and feedback seeds; the race and lint seeds call it themselves right after their save), trusted main only, `actions: write` and nothing else, no checkout. Deletes the superseded entries of the regenerable cache families and idle pull-request entries so the 10 GB repository limit does not evict live entries; see Repository cache budget below. |
| Refresh the dev feedback seed | `cache-refresh-dev-seed` | Daily (cron 03:44 UTC, which in practice runs hours later, see the budget section) or manual, trusted main only, `actions: write` and nothing else, no checkout: runs `gh workflow run ci-feedback-backend-cache.yml --ref dev`. Schedules run on the default branch only, so dev has no cron; without this the dev seed is last used on the Friday before a quiet weekend and is the first entry the Monday lint driver rollover evicts. The dispatched run is a dev run in every respect (it checks out dev, the key carries the dev family and the UTC day, only the dev ref can save it), so the trust families stay apart. A run takes about 3.6 minutes when it compiles (measured on the main seed) and 0.2 when the day already has a seed. |
| Run repository invariants before queueing | `ci-advisory-invariants` | Advisory, per PR push touching Go code, the golangci-lint config, workflows, Git hooks, scripts, release policy, or the store, lint fixture and web e2e inputs those suites read. Runs the `internal/boundary`, `internal/lint` and `scripts/ci` suites on the exact PR head. Not a required context; the queue still runs everything. |
| Run the Storybook suite before merge | `ci-advisory-storybook` | Advisory, per PR push touching `web/`, `clients/ts/`, `.nvmrc`, the root `package.json`, `docs/site/scripts/test-csp.mjs`, `scripts/ci/storybook-suite.sh` or the workflow's setup helpers (`install-corepack.sh`, `export-pnpm-cache.sh`). Runs the same commands as the `storybook` job in `ci.yml` (story tests in both palettes, production build, theme and docs checks, static demo) on the exact PR head in the Playwright container. The pre-push hook runs the same script locally. Not a required context; `ci-dev` and the queue still run the job. |
| Lint new Go code | `ci-advisory-golangci` | Advisory, per PR push touching Go files, `go.mod`, `go.sum`, `.golangci.yml`, `scripts/ci/golangci-lint-new.sh`, its version pin or `scripts/ci/run-go-tool.sh`. Runs golangci-lint (version in `scripts/ci/golangci-lint-version`, rules in `.golangci.yml`: errcheck, errorlint, errchkjson, noctx, wastedassign, unused, staticcheck `SA*`/`QF1001`/`QF1003`, gosec G115/G204/G306 outside tests) and reports only issues on lines changed since the merge base, so the existing baseline is not enforced. Run it locally with `scripts/ci/golangci-lint-new.sh <base>`; the pre-push hook runs it too (`HIKYO_PREPUSH_GOLANGCI=skip` opts out). Lints only the default build (not other-OS files or `ui`, `k8se2e`, `floor*` tags); type errors are always reported in full. Restores caches only, saves none. Not a required context and in no required job or fast-feedback lane. |
| Lint the desktop module | `ci-advisory-golangci-desktop` | Advisory, per PR push touching `desktop/`, `.golangci.yml`, `.golangci-strict.yml`, `scripts/ci/golangci-lint-version`, `scripts/ci/run-go-tool.sh` or the workflow itself. Pinned `golangci-lint` over the whole nested module on macOS; a Linux pre-job skips the macOS job where `desktop/go.mod` does not exist. The strict pass runs only where `.golangci-strict.yml` exists. |
| Validate integrated development | `ci-dev` | Complete read-only graph for the merged dev SHA; finishes one active run and coalesces pending work. |
| Prove queued release candidates | `ci-merge-group` | Queue `checks_requested` runs the complete graph, including benchmark smoke and all 16 domains. Trusted `ci-queue-required` publication authorizes only the exact current group. |
| Validate all of main | `ci-main` | Weekly or manual full graph, including development snapshot artifacts. The schedule is the automatic writer of main-scoped compile caches. |
| Run maintenance checks | `reliability` | Choose `security`, `gitlab`, `reporting`, `race`, `channel`, or `all`. Original schedules run their matching suites. |
| Prove release hardware/restore acceptance | `release-acceptance` | Choose `backup-restore`, `operator`, or `all`. Existing custody secrets stay confined to the backup step. This does not publish a release. |
| Run operations-floor acceptance | `release-ops-floor` | Native ARM64/AMD64 operator diagnostics; also runs for relevant PR changes. |
| Measure performance | `perf-codspeed` or `perf-control` | Measurements preserve the paid-run budget. Required benchmark smoke does not use paid runners. |
| Validate desktop nightly | `nightly-desktop` | Daily at 03:41 UTC or manual on main. Runs the macOS desktop validation against the dev and main heads and keeps one `ci: nightly desktop validation failing` issue open until a green night. Advisory: not required by dev or the queue. |
| Promote dev to main for the nightly | `nightly-promote` | Daily at 22:17 UTC or manual on main. First back-merges main into dev through a `backmerge/auto-*` PR with auto-merge when dev lacks real main commits (also right after each merged same-repository PR into main whose head is not `release/nightly-*`, so fork PRs wait for the next run; stuck after 6 hours or a conflict fails the run; a hand-made `backmerge/*` PR takes precedence). Then opens a `release/nightly-*` PR for the last green dev commit when main lacks it and queues it. A merged promotion starts the nightly. |
| Publish nightly builds | `nightly-release` | Scheduled publication or the repository-dispatch path, built and signed from main after complete verified source proof of that exact commit. A commit that is already published is skipped. The independent `promotion-health` job fails the run when the latest scheduled or manual `nightly-promote` run did not succeed or none completed in 36 hours, without blocking publication. |
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

The source contains 42 YAML workflow files. GitHub's sidebar also contains
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
| Nightly desktop | `nightly-desktop.yml` | Advisory macOS validation of the dev and main heads (owner decision 2026-11-25), kept off the dev and queue critical paths. Holds the only `issues: write` permission in its own report job. |
| Maintenance | `reliability.yml` | Original daily security and weekly integration/race/channel checks, plus one manual menu. |
| Compiler cache seeds | `ci-feedback-cache.yml`, `ci-race-cache.yml`, `ci-lint-cache.yml`, `ci-main-headroom.yml` | Trusted-main-only producers of Go build entries (`ci-main-headroom.yml` writes none: it only frees room for ci-main's weekly saves, see "Headroom before the save"). (`ci-feedback-backend-cache.yml` is the one trusted-branch producer that also publishes from dev; see the first table.) PR feedback, source-artifact validation, race shards, the reliability race suite, `test_core_serial` and the advisory invariants job only read them. One shared race entry replaces twelve per-shard entries that could not fit the 10 GB repository cache budget together. |
| Pinned tool seed | `ci-tool-cache.yml` | Trusted-main-only producer of the pinned release-file entries. Every other workflow only restores them, and the helper verifies each file's SHA-256 before use, so a poisoned or stale entry cannot run. |
| Cache prune | `ci-cache-prune.yml` | Scheduled, manual, and after a named writer workflow completed on main (`workflow_run` of `CodeQL`, `ci-main`, `cache-seed-tools`, `cache-seed-feedback`, filtered to `branches: [main]`; the job guard also requires that run's head branch to be main in this repository, and nothing else is read from it). It is also a reusable workflow (`workflow_call`): `ci-race-cache.yml` and `ci-lint-cache.yml` call it in a separate `prune` job (`needs: seed`, `actions: write` only, no checkout) right after their save, so the superseded generation of those seeds lives for seconds. Its concurrency group sits on the job, after the guard, and carries the calling workflow's name, so a skipped run cannot replace a pending real prune. Holds `actions: write`, checks nothing out and runs no repository code. A `workflow_run` job is the only way to give those writers a post-save prune without `actions: write` in a job that runs PR code (`ci-main` reuses `ci.yml`) or in a workflow that is not ours (CodeQL default setup). Superseded: of the listed regenerable families (`go-mod-v2`, `go-test-v2`, `go-race-v2`, `go-lint-v2`, `go-lintdriver-v2`, `go-generated-v2`, `go-k8s-e2e-v2`, `go-fuzz-v2`, `go-feedback-v1`, `pinned-tool-v1`, `pnpm-store-v2`, the CodeQL `overlay-base-database`, `dependencies` and `trap` entries), every entry on `refs/heads/main` or `refs/heads/dev` except the newest of its ref and lane (the key without its trailing hash, commit, attempt or week, the runner image, the tool version and the config hash, so an image or CodeQL bump retires the old lane at once). Idle: entries on `refs/pull/*` not accessed for 24 hours. The `go-feedback-v2` seeds are not listed: the backend seed's own prune keeps the newest set per family and entry. Other refs and unlisted families are never touched. One to two listing calls and at most 100 deletes per run: 24 scheduled runs a day plus a few per writer, at most 102 calls a run against the shared 1000 an hour. |
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
| Prune after each writer (`workflow_run`, and a direct call in the race and lint seeds) | n/a | window in which two generations coexist drops from up to 60 minutes to about 2 (seconds for the race and lint seeds) | none; on a weekday the replay peaks at 9.74 GB with it and 10.19 GB without (10.26 at 10 GiB), on Monday and Sunday it changes nothing because the model evicts at the instant of the save |
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
| Weekday | before | 10.04 | 10.51 / 11.23 | 8 / 6 | both survive |
| Weekday | adopted | 8.98 | 9.74 / 9.74 | 0 / 0 | both survive |
| Monday lint driver rollover (14:14) after a quiet weekend | before | 10.04 | 11.46 / 11.75 | 4 / 4 (shellcheck, helm, cosign, `go-mod-v2`) | **dev seed evicted** at both limits |
| Monday lint driver rollover | adopted | 8.98 | 10.34 / 10.34 | 4 / 0 | both survive |
| Sunday 20:31 fuzz rollover | before | 10.04 | 10.02 / 10.48 | 0 / 0 | main seed entry evicted on the tie at 10.00 GB |
| Sunday 20:31 | adopted | 8.98 | 9.42 / 9.42 | 0 / 0 | both survive |
| go.mod/go.sum change day, observed times | adopted | 8.98 | 10.26 / 10.34 | 8 / 0 | both survive |
| same, race at 14:14 and lint three minutes later (the shared concurrency group) | adopted | 8.98 | 10.26 / 10.34 | 5 / 0 | both survive |
| same, race at 11:59 and lint three minutes later | adopted | 8.98 | 10.26 / 10.34 | 7 / 0 | both survive |
| same, without the shared group, both saves at 14:14 | adopted | 8.98 | 11.35 / 11.63 | 12 / 9 | both survive |
| same, without the shared group, both at 11:59 | adopted | 8.98 | 10.95 / 11.63 | 11 / 9 | both survive |
| Sunday 20:31 `ci-main` after a go.sum change (every save at once) | adopted | 8.98 | 12.27 / 12.27 | 8 / 7 | both survive |
| Monday after a quiet weekend, main key by commit alone (before the daily key component) | adopted | 8.98 | 10.34 / 10.34 | 0 / 0 | **main seed evicted** at 10.00 GB, none at 10 GiB |
| Sunday `ci-main` after a go.sum change, main key by commit alone | adopted | 8.98 | 12.27 / 12.27 | 8 / 7 | **main seed evicted** at every minute of the sweep, both limits |

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

**The main seed is saved every day (review finding M1).** The main feedback key used to end in the
commit alone and the save runs only on a miss, so on a day on which main did not move (5 of the last 37
days) nothing was saved: over a quiet weekend the main seed kept its Friday access time and, when a
Sunday `ci-main` run saved 3.3 GB after a go.sum week, it was the least recently accessed live entry. The
key now ends in `<commit>-<UTC day>` (the dev key in `dev-<UTC day>`, as before one per day), so the daily
seed run misses and saves every day, and the seed workflow's own prune keeps the entry just saved and the
newest one (`suffix=<commit>-<day>`; a same-commit entry of an earlier day is superseded). This does not
rely on whether a `lookup-only` hit updates `last_accessed_at`, which GitHub does not document. The cost
is one more save of the main family (an estimated 0.69 GB, three entries) on a day main did not move,
about 5 days in 37; it is deleted by the next day's prune. The steady-state demand does not change: the
model already assumed one main save a day, and two sets coexist only between a save and the seed's own
prune job (seconds). With the old key the model loses the main seed on every sampled Sunday go.sum minute
at both limits and on the Monday rollover at 10.00 GB (rows above, pinned by the `main key without the
day` subtests, which fail if the daily component stops being load-bearing).

**Days that rotate keys are not covered by the first three scenarios.** `go.mod` or `go.sum` changed on
main on 7 of the last 30 days (committer date, `origin/main` at 2026-11-25). Then the daily race seed
(with `go-mod-v2`), the daily `go-feedback-v1` save and the lint driver seed each save next to the old
generation. With the observed run ends the day peaks at 10.26 GB and takes eight idle entries at
10.00 GB, none at 10 GiB, and both seeds survive.

Four things keep the newest seeds alive on such a day. The first three are the situation without the
fourth (the headroom delete, below), which is what makes survival independent of traffic.

1. The race and lint seed workflows prune their own superseded entries in a separate `prune` job
   straight after the save (`ci-cache-prune.yml` called as a reusable workflow, `actions: write` only,
   no checkout), and they are not in that workflow's `workflow_run` list, so the direct call is the only
   prune path for them. Its concurrency group carries the calling workflow's name, so the hourly prune
   can never replace a pending prune of a seed run (which would show that seed run as cancelled). In
   reality the old generation lives for seconds. The model does not take that credit: it still evicts at
   the instant of the save, which is the worst case.
2. The race and lint seeds share one concurrency group (`ci-cache-seed-writers`, `cancel-in-progress:
   false`), so a running run is never cancelled and the second run starts only when the first, its prune
   included, has finished: two rotated generations never coexist. GitHub keeps one pending run per
   group: a third run queued meanwhile (a manual dispatch, say) replaces the pending one, which is then
   reported as cancelled. The replaced run's save is not lost for good: the next scheduled run (the
   cron fires daily) saves what it would have saved. The model replays that as the lint save three minutes after the race save. Saves in
   one minute (the case without the shared group, kept as a contrast) peak 0.6 to 1.0 GB higher.
3. **Without the headroom delete (item 4) the survival rests on an assumption: the newest main and dev
   seeds are restored by every feedback run on a weekday**, so
   their last access is always recent. The listing shows the newest main seed accessed 5 minutes before it
   was taken. The rates differ by base, and they are the weak point for main: the review counted 24 pull
   requests to main against 80 to dev in 8 days (not re-measured here), and the tooling and cli entries
   are restored only by specific lanes. The survival below rests on this assumption; the sensitivity
   shows what is left without it. The model's busy entries are all touched at one fixed offset (25
   minutes before the hour), the same for every entry and not a function of the key; ties go against
   the seeds.

4. **Headroom before the save** (review finding F1). The newest seed is an LRU entry like any other, so
   under the assumption above it is safe only while it is touched more recently than the entries the
   save pushes out. The writers therefore make room first. `scripts/ci/cache-prune.sh` is the one
   selector (the hourly prune workflow carries its text verbatim, a test pins them byte-equal, because
   that workflow has no checkout). Run with `HEADROOM_PREFIXES` naming the lanes about to be saved, it
   deletes, after the usual superseded and idle entries, whatever is needed until
   `total + incoming <= 9.75 GB`, in this order: older feedback seeds (not the newest of their ref),
   entries of pull-request refs, entries idle for more than three days, then the least recently used
   entries that are not seeds. It never deletes the newest main or dev entry of any seed family
   (`go-race-v2`, `go-lint(driver)-v2`, `pinned-tool-v1`, the familied `go-feedback-v2`), never touches
   the two unfamilied `go-feedback-v2` entries, and deletes at most 100 entries a run.
   * **Where it runs.** `actions: write` never sits in a job that runs repository code (a security review
     finding on the first version, which put the step in the seed jobs next to `go run` and `go test`).
     The race and lint seed workflows each get a `headroom` job before `seed` (`needs: headroom`):
     `contents: read` plus `actions: write`, a checkout of the immutable main commit without
     credentials, the runner ABI step and the script, nothing else; `seed` keeps the read-only token and
     is pinned so by the workflow tests. The job is best effort (`continue-on-error`), so the seed never
     waits on its success. It runs once for the race lane and once for the module lane (race), or for
     the lint driver lane (lint), each with `HEADROOM_SKIP_KEY` set to the exact key the seed is about to
     look up: on an exact main hit no save follows and the script deletes nothing. The weekly `ci-main`
     writers are jobs of `ci.yml`, which runs pull-request code, so none of them can hold the token;
     and `ci-main.yml` is a promotion-proof producer whose job inventory `scripts/ci/source-proof`
     checks, so a job added there would fail the next proof. Option (b) was taken: a separate workflow,
     `ci-main-headroom.yml` (one job, same permissions and checkout, all six Go lanes, skip key the exact
     `go-test-v2` key of the current `go.mod`/`go.sum`, then a second step for the two pnpm stores, below),
     scheduled 45 minutes before ci-main's cron (12:38 on Sunday). `source-proof` is untouched, and
     `ci-main.yml` is byte-identical to before, which a test pins (one job, read-only permissions).
     **`ci.yml` is a policy file of the source proof** (`policyPaths` in
     `scripts/ci/source-proof/resolve.go`), and this change edits it (the module-cache paths, the
     k8s-e2e `GOFLAGS` and its `-nodwarf` key). It is main-first, so main itself is fine, but the dev
     source proof and the nightly promotion stay blocked until a main-to-dev backmerge carries the change;
     that backmerge is a landing step, not a side effect. A manual ci-main run is covered by a `workflow_run`
     trigger of the same workflow (`workflows: [ci-main]`, `types: [requested]`, `branches: [main]`):
     `requested` is the earliest event GitHub offers, before any job of the run. The trigger runs the file
     as it is on main, at main's latest commit, with a write-capable token, so the job carries the guard of
     `ci-cache-prune.yml` (event branch and repository, `github.ref`) and takes its concurrency group at
     job level, after that guard, so a run the guard skips cannot replace a pending trusted one. What the
     documentation says: `workflow_run` supports `requested`, `in_progress` and `completed`, and
     `requested` does not occur on a re-run; it does not say whether `requested` fires for a run started
     by `workflow_dispatch`. Observed for `completed`: `fuzz-report` ran two seconds after each of five
     dispatched ci-main runs on 2026-10-31. Whether `requested` fires for a dispatched ci-main run is
     therefore **unverified until the first manual run after the merge**; the landing steps check it, and
     until then dispatching `ci-main-headroom` first is the fallback. The race between the headroom job
     and the writers' first saves is modelled below.
   * Weekly pnpm stores (`pnpm-store-v2-host-` and `-playwright-`, 0.41 GB together): the validation job
     saves them when a lockfile or the Node pin changed, which `go.mod` and `go.sum` do not show, so the
     go-lanes skip key never sizes them on its own. The headroom must count them together with the go
     lanes: the first design gave them a step of their own that sized only the stores, so on a day with
     both changes each step left room for its own save and not for the other's, short by the smaller one
     (0.41 GB). The model lost the newest seed at 10.00 GB with it (peak 10.31 GB; kept as a mutation, the
     test asserts the loss). The go step now names `pnpm-store-v2-` as well, so the room covers 3.7 GB at
     once; the second step stays for a day on which only a lockfile changed (the go skip key hits and the
     first step does nothing). Cost: on a go.sum-only day the go step counts 0.41 GB that is not saved, a
     conservative extra deletion (the model includes it). The stores' keys carry the Node ABI, which needs
     Node and so repository tooling the job must not run, so the second step's skip keys end in the
     lockfile hash and leave the ABI as a star (the script matches one `*` as a start and an end, and skips
     only when every key hits a main entry). Residual: a Node ABI bump alone, with no lockfile change, is
     not seen by that skip and sizes the stores (conservative, 0.41 GB). The model asserts that a lockfile
     change on the same day as go.sum loses no newest seed at either limit (sampled by default, every minute in the full sweep).
   * Margin 0.25 GB (budget 9.75 GB): it equals the largest single CodeQL overlay (0.268 GB), so one
     concurrent CodeQL save between the delete and the save still fits.
   * Incoming size: the sum, over the lanes named, of the size of the newest main entry of that lane
     (compressed archive bytes, which is what the new generation will be about the size of). `du` is not
     used: the headroom job has no build cache, and `du` of the uncompressed tree overestimates the
     archive by about 2 to 3 times, which would delete live entries every day. A lane with no entry
     contributes nothing, because a lane with no main entry has already been evicted and the room is
     there. A lane prefix names the lane without the runner image or the ABI (`go-race-v2-<os>-<arch>-`):
     a prefix that named the image (`...-ubuntu26-`) would match no entry on the first run after an image
     bump, whose entries carry the old image, so the lane would look empty and the save would be sized as
     nothing; the exact-key skip, which must match, keeps the image. A test runs the script on entries of
     an older image.
   * Module cache (`go-mod-v2`, race seed): the module headroom step has no skip key. Its save is a hit
     only when key and cache version match, and the version includes the cached path, which this change
     alters (`cache/download` only), so an exact-key listing hit says nothing about the save and the
     version of the new save cannot be computed in the headroom job (the listing exposes `version`, but
     only for entries that exist). The step therefore sizes the module save every day. The cost is
     conservative: the newest `go-mod-v2` entry is 0.44 GB (estimated for the new path, measured 0.79 GB
     for the old), so on a day with no save the step may delete up to that much idle material that
     GitHub would not have evicted yet. The first save under the new path is also the one that rolls the
     old entry (0.79 GB) out, which the model plays as part of the rotation-day sweeps.
   * Window: the headroom job runs before the seed's compile, so the room is made minutes before the
     save, not seconds: the race seed ran 1 to 7 minutes on the observed days (its job limit is 30; the
     model takes 20), the lint seed 2 to 11 (the model takes 11, and less behind the race run in the
     shared group, which it cannot precede), and `ci-main` 45 minutes on the Sunday cron. The model plays
     these leads, and a test ties the lanes it names to the workflows' own. The
     per-minute sweep of the first version of this move showed what the window costs: the daily
     `go-feedback-v1` save (observed 12:58, between the race and lint saves) landed unsized and the seed
     was lost at 30 of 361 minutes at 10.00 GB. Both seed headroom jobs therefore also reserve the
     newest `go-feedback-v1` entry's size (a prefix of the same script call); the sweep then loses none.
     A short peak that evicts one idle entry remains possible and is accepted.
   * Cost: the headroom deletes live entries the cache would not yet have evicted. The model shows 3 to 6
     live least-recently-used entries freed per rotation day and 3 on Monday; they are rebuilt by the
     next run that needs them.
   * Residual: the CodeQL saves (`codeql-trap`, `codeql-dependencies` and the overlays, about 12 saves
     a day) run in `github/codeql-action` and cannot carry the step. A CodeQL save larger than the
     0.25 GB margin that lands in the window between the delete and the seed save can push the total over
     the limit. Only `codeql-dependencies` (0.755 GB) exceeds the margin, so the overshoot is at most about
     0.5 GB, and an eviction needs the newest seed to be the least recently used entry, which the order
     and the daily restore make unlikely. Estimated, not measured: on the order of 1 to 2 percent of
     days, a little more than before the move out of the seed job because the window is now minutes.
Sweep, every minute (`seed survival at every minute`): the race seed saves at every minute from 10:59
to 16:59 on a rotation day with the lint seed three minutes later (361 minutes; the observed seed runs
ended 11:25 to 14:57), and the Sunday `ci-main` after a go.sum change saves at every minute from 10:59 to
22:59 (721 minutes; cron 13:23, observed 20:31). By default the test samples every third hour (rotation, from 10:59) and every sixth hour
(Sunday), because each replay runs the prune's jq about thirty times and the package has the hosted 600 s
budget. The headroom sweeps (rotation day with headroom at 10.00 GB) also replay every minute at which an
event the sweep moves (the seed saves, the headroom before them) passes an event it does not (the day's
other saves and deletes), derived from the model's own event list (`sweepMinutes`). The outcome of a
replay is constant between two such crossings, so a seed lost over ten minutes is found at the crossing
that opens them and the minute after it (events that tie at the crossing sort either way), wherever the
coarse samples fall; the hourly prune and touch recur every hour and are
left to the coarse samples and the full sweep. This is what makes the default run see the failure that
the first version of this move had: with `go-feedback-v1-` removed from the model's race and lint
headroom lanes (the mutation of the review) the default run loses the seed at 20 of 65 sampled minutes
and fails, where the three-minute sample passed. Default-mode `TestCacheBudgetModel`: about 13 s (measured,
quiet host, 4 cores); `HIKYO_CACHE_SWEEP_FULL=1 go test -run TestCacheBudgetModel ./scripts/ci/` runs
every minute (several minutes) and is how the tables below were measured.

| Sweep (adopted plan, live seeds) | Limit | Minutes with a newest seed lost | Worst peak | Most live entries evicted |
| --- | --- | --- | --- | --- |
| Rotation day, race then lint (shared group), **no headroom** | 10.00 GB | 0 of 361 | 10.77 GB | 18 |
| Rotation day, race then lint (shared group), **no headroom** | 10 GiB | 0 of 361 | 11.10 GB | 11 |
| Rotation day, both saves in one minute (no shared group) | 10.00 GB | 0 of 361 | 11.35 GB | 18 |
| Rotation day, both saves in one minute (no shared group) | 10 GiB | 0 of 361 | 12.09 GB | 12 |
| Sunday after a go.sum week | 10.00 GB | 0 of 721 | 12.27 GB | 9 |
| Sunday after a go.sum week | 10 GiB | 0 of 721 | 12.27 GB | 8 |
| Sunday after a go.sum week, main key by commit alone | 10.00 GB | 721 of 721 | 12.27 GB | 8 |
| Sunday after a go.sum week, main key by commit alone | 10 GiB | 721 of 721 | 12.27 GB | 7 |

With the headroom jobs (measured per minute, `HIKYO_CACHE_SWEEP_FULL=1`, merged tree 79e28858c, agent-heavy job 8123; the seed
jobs' headroom leads are 20 minutes (race) and 11 minutes or less (lint), ci-main's 45 minutes unless a
row says otherwise):

| Case (with headroom) | Limit | Minutes with a newest seed lost | Worst peak | Most live entries evicted |
| --- | --- | --- | --- | --- |
| Rotation day, observed traffic (main 4, dev 10 restores a day) | 10.00 GB | 0 of 361 | 10.41 GB | 1 |
| Rotation day, observed traffic | 10 GiB | 0 of 361 | 10.41 GB | 0 |
| Rotation day, sparse traffic: no restore between the daily saves | 10.00 GB | 0 of 361 | 10.41 GB | 1 |
| Rotation day, sparse traffic | 10 GiB | 0 of 361 | 10.41 GB | 0 |
| Monday rollover, no restore between saves | 10.00 GB | 0 | 9.74 GB | 0 (headroom freed 3) |
| Sunday ci-main after a go.sum week | 10.00 GB | 0 of 721 | 9.74 GB | 0 |
| Sunday ci-main after a go.sum week | 10 GiB | 0 of 721 | 9.74 GB | 0 |
| Manual ci-main on a weekday after a go.sum change (headroom workflow dispatched 45 minutes ahead) | 10.00 GB | 0 of 721 | 10.66 GB | 1 |
| Manual ci-main on a weekday after a go.sum change (same) | 10 GiB | 0 of 721 | 10.66 GB | 0 |
| The same, headroom started by the run's request, listing taken 1 / 3 / 5 / 10 minutes after it | 10.00 GB | 0 of 721 each | 10.59 / 10.37 / 9.96 / 10.40 GB | 1 / 1 / 0 / 2 |
| The same, 1 / 3 / 5 / 10 minutes | 10 GiB | 0 of 721 each | 10.59 / 10.37 / 9.96 / 10.40 GB | 0 |
| The same, 20 minutes after the request | 10.00 GB | 669 of 721 | 10.64 GB | 9 |
| The same, 20 minutes after the request | 10 GiB | 502 of 721 | 11.40 GB | 8 |

The same cases without the headroom, which is what the headroom is for: sparse traffic on a rotation day
loses a newest seed at 311 of 361 minutes at 10.00 GB (peak 10.77 GB, 10 evicted); a manual ci-main run
on a weekday after a go.sum change at 687 of 721 minutes at both limits (peak 12.54 GB, 11 and 9
evicted, because every entry but the saved ones is touched hourly). Two mutations of the script the model
must see: a selection that may delete a newest seed loses it at 361 of 361 minutes, and one that frees
nothing at 311. The cost: at 10 GiB the manual weekday case shows the headroom freeing entries GitHub
would not have evicted; the Monday rollover frees 3 live entries.

The on-request rows are the race between the headroom job and the writers' first saves (finding F-B): the
saves land at 2 minutes (lint, 40 MB), 2.5 (generated, 182 MB), about 7 (fuzz), 14 (go-test, 707 MB), 16
(module) and 24 (k8s-e2e, 1.6 GB) after the request (offsets measured from the 2026-11-22 run; the
request-to-listing delay is not measured, 1 to 3 minutes is expected on a hosted runner). A listing up to
10 minutes after the request loses nothing; at 20 minutes the go-test save has landed unsized and a seed
is lost.

Sensitivity (review finding L1): if a weekday run touched a seed no more often than any other busy entry
(the hour mark, a tie going against the seeds), the rotation day with the serialised saves loses a seed
at these minutes of 361:

| Seed treated as an ordinary entry | At 10.00 GB | At 10 GiB |
| --- | --- | --- |
| main only | 162 | 0 |
| dev only | 166 | 4 |
| both | 166 | 0 (earlier revision; the sampled sweep of this case was dropped for test time) |

Before the daily key and the shared group the same case lost a seed at 327 of 361 minutes at 10.00 GB and
326 at 10 GiB. What remains is at 10.00 GB only, when the day's peak (up to 10.77 GB) passes the limit
and the hourly-touched entries tie with a seed; short peaks that evict other entries are ordinary LRU
behaviour and accepted, the acceptance test being that the newest main and dev seeds survive under the
stated assumption. If the assumption is wrong for main, a rotation day at exactly 10.00 GB can lose it, when no headroom
job runs; the with-headroom rows above show the sparse-traffic case, which is what the headroom is for, losing none.
The cost is below. An earlier revision of this section said the seeds are lost only when the saves
coincide at 11:59; that was an artefact of per-key offsets in the model and a labelling slip (the case
named 14:14 had actually moved the lint save to 13:36), both removed.

**Restated steady state after M1.** Demand once the prune has run (the low point) is 8.98 GB: margin
+1.02 GB at 10.00 GB and +1.76 GB at 10 GiB. Peaks at 10.00 GB / 10 GiB: weekday 9.74 / 9.74, Monday
10.34 / 10.34 (4 / 0 live entries evicted), Sunday fuzz 9.42 / 9.42 (the weekly fuzz rollover, 0.44 GB, is not sized by any headroom job on a Sunday
without a go.sum change, where the exact `go-test-v2` key exists and the step skips; its peak stays under
the 9.75 GB budget in the model, with no eviction at either limit, which `adopted/sunday fuzz rollover`
pins), rotation day 10.26 / 10.34 at the
observed times and at most 10.77 / 11.10 with the saves serialised, Sunday after a go.sum week 12.27 /
12.27. The daily main save of M1 is already inside the 8.98 GB demand.

How long a lost seed stays lost: the main and dev seeds are re-saved daily (main by its day key, dev by
its refresh); the main-seed saves ended 11:46 to 14:07 (2.35 hours apart) on the observed days, and the
worst gap between two saves is about 26.4 hours, so a lost seed is back after at most about 27 hours.
The cost is cold feedback builds in that window (the main seed compile took 3.6 minutes in the measured
run), so slower feedback jobs and no loss of coverage: the same checks run, uncached. The daily
`cache-seed-tools` run (14:16 to 14:49) is not modelled.

Sunday after a go.sum week. Only the weekly `ci-main` writes the test, lint, generated, k8s-e2e and
fuzz entries, so on every Sunday after a week with a `go.mod` or `go.sum` change it saves about 3.3 GB at
once: module, test, lint, generated, k8s-e2e and fuzz entries, with the old ones restored and so freshly
accessed. That is the usual Sunday after such a week, not a rare one: 3 of the last 4 Sundays followed a
week with a change (the weeks ending 11-01, 11-08 and 11-15; not 11-22). The model evicts 8 live entries
at 10.00 GB and 7 at 10 GiB (peak 12.27 GB), both seeds survive, and the evicted entries are idle ones:
`buildx-dl-bin` (next Docker build), three CodeQL overlays, `codeql-dependencies` and `codeql-trap`
(next CodeQL run on main), `go-feedback-v1` (the daily save), and `go-lintdriver-v2` (the weekly seed
saves it again on Monday, so lint runs without the driver cache until then). `ci-main` saves over its
run, not at one instant, and it was cancelled on 11-22, so the figure is a worst case. Its superseded
entries are removed by the existing prune on `workflow_run` of `ci-main`, so the two generations coexist
for the rest of the `ci-main` run after its last save and the time the prune takes to start (not
measured here).

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
lints it from inside `desktop/` instead.

**Owner decision 2026-11-25** ([ci-coverage-cuts ADR](../adr/ci-coverage-cuts.md)):
desktop validation is not part of `ci.yml`, so it gates neither `ci-dev` nor the
merge queue. The `macos-15` job was not acquired in 5 of 21 `ci-dev` attempts and kept
dev red. `nightly-desktop.yml` owns it instead, daily at 03:41 UTC or by manual
dispatch on main:

1. A `heads` job pins the dev and main commits and picks each leg's runner: `macos-15`
   (paid minutes) where `desktop/go.mod` exists at that commit, `ubuntu-latest` where
   it does not (main before promotion), so a boundary-only leg cannot fail on macOS
   capacity. `desktop-dev` and `desktop-main` each check out their exact commit, with
   no cache restored or written and `CGO_CFLAGS` and `CGO_LDFLAGS` set to
   `-mmacosx-version-min=13.0`. A leg step fails when the module exists on a
   non-macOS runner.
2. Each runs `scripts/ci/check-desktop-deps.sh`. It keeps Wails and its platform
   bindings out of the root `go.mod` and `go.sum` and refuses a `go.work` or a root
   requirement on `desktop/`. Where `desktop/` exists it also requires `replace
   github.com/Hikyo-Org/hikyo => ../` in `desktop/go.mod`, and its third check keeps
   `feedback-plan.sh` honest: every root package that `desktop/` compiles must select
   the `desktop-compile` fast-feedback check.
3. Where `desktop/go.mod` exists: `go test -race ./...`, a `-tags probe` build, a
   release build, and the driven WKWebView lane, `go run ./probes/driven`, which
   opens the probe shell against a fake instance (Playwright cannot drive WKWebView).
   Main before the shell lands runs only step 2, on Linux, so until promotion brings
   the module to main, the main leg covers only the boundary check.
4. A `report` job runs with `issues: write` only, no checkout and no repository code.
   When either leg is not `success` (a runner that is never acquired is reported as
   `cancelled`), it looks the issue up by the `ci-nightly-desktop` label plus an exact title, marker and
   bot-author match, then opens or comments on the one issue titled `ci: nightly desktop
   validation failing` with the run link and each branch's head SHA and result, then
   fails, so the run stays red. It runs only on main; a manual run from another ref skips
   it. A later green night closes the issue with a comment.
   A `shim` job (no checkout, API reads only) checks whether the desktop-removal shim
   in `ci.yml` is dead: once neither head's job registry lists `desktop`, the shim and
   its migration tests are dead code (tracked in #1595). The report keeps that in its
   own issue, `ci: desktop-removal shim check (#1595)` (label
   `ci-desktop-removal-shim`), opened, with the actual state in its body, when the shim is dead or its check cannot
   read a file, and closed once it is healthy. The desktop issue follows only the two legs, so
   a dead shim never hides a desktop failure; either failing keeps the run red.
   Each night costs two issue-list calls, plus one write per issue when its failure
   repeats (a comment) or it closes, and two when it is first opened (the label, then
   the issue).
5. Lint is advisory, like the root ratchet, and the root `golangci` names must stay
   out of `ci.yml`: `ci-advisory-golangci-desktop` runs the pinned `golangci-lint`
   over the whole module on macOS, and `golangci-lint-new.sh` lints `desktop/` from
   inside it for the pre-push hook and `ci-advisory-golangci`.

Nothing requires `nightly-desktop`, and `nightly-promote` and `nightly-release` read
none of its results. A red night therefore does not stop promotion or publication; it
only keeps the issue open. Desktop edits select no `ci.yml` job: `desktop/*` is a
known path with an empty plan, so a desktop-only PR gets an empty plan, and the
root packages it imports select only their own domains. A root `go.mod` or `go.sum` change, a workflow change and every unknown path
still select the full plan. Fast feedback keeps `desktop/` out of the root compile
(`go-compile`) and compiles it with its own `desktop-compile` check, `go -C desktop
test -c`, only where `desktop/go.mod` exists; that Ubuntu check stays on the PR
admission path. The frozen conservation baseline is exact again: `desktop` is not a
domain.

Until the main-to-dev backmerge lands this change, `ci.yml`'s `ci-required` step adapts
a protected base that still registers `desktop` (see the comment there).

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
limits one, zero minimum wait, `MERGE`, and a 120-minute check-response timeout (raised from 60 by #1371; the live ruleset 20539346 is already at 120, and the policy files in this repository only state what is live: nothing is raised here).
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
below which the job defers itself (it defers when `remaining - 42 < 480`, in either
bucket); the others are held and handed straight to the wait, so a deferral costs
no attempt it cannot recover. That job is a designed wait: it sleeps
until the window's reset plus 5 seconds (refusing a reset more than 61 minutes away),
re-reads `rate_limit`, then reruns, group jobs first, every qualifying failed
job of the last three hours from the three publisher workflows, under the same trust
rule and attempt bound. It pages through every failed and cancelled run of that period
(status and `created` filters, `--paginate`), with no fixed run count, and requests one
rerun per run as above. A window that has already recovered when the job starts (a reset
passed in between, or the sleeper queued behind another) skips only the sleep; the sweep
still runs. If a non-group job then does not fit above the reserve (remaining between 480
and 521, say), the job waits for the reset once and releases the held set once; a hold
that only group jobs suffer also waits for the reset once and releases the held group jobs, newest run per job name first (job names carry the kind and head, `group-<head>` for a group worker job and `publish-<head>` otherwise, so a group job is classified as a group job). The wait never sleeps past 66 minutes from its start (the second sleep is capped at the time left minus the 65 seconds the budget read after it can back off) and names the held jobs when the reset is later. The reruns are spaced 15 seconds apart. A released rerun spends only after
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
reserve of 480 requests of the 1,000 per hour. Every job that does not gate a
merge-queue head (dev publishers, queue PR-admission publishers and head workers, the
retirement jobs, the retry reruns of such jobs, and the scheduled dev sweep) first
reads `rate_limit`, which costs nothing, and defers while either the core or the
graphql bucket would be left with fewer than 480 after the job's own expected cost
(42 reads), that is, below 522: it spends and writes nothing, its Reconcile
step fails, and the retry and wait workflows rerun it after the reset. A retirement
job (`tidy-checks`) is not one the retry follows: its deferral is completed by the next
hourly discovery, a push or a manual dispatch, which is acceptable because it only
retires records of heads that are no longer active and so gates no merge. To a
contributor a deferral looks like a red gate job whose log says "deferred: ... too few to
leave the 480 kept for merge-group jobs after this job's own 42; this job spent nothing and
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
36 to 73 minutes lets at most two finish in one window) times three attempts: 246, plus the 232 requests an hour that nothing can defer
(event discovery 102, retry listings about 30, other workflows about 100): 478, rounded to 480. This is the
original derivation, which assumed nothing else spends; the honest bound, which exceeds 480, is the sizing
table of the interim exception below. It lives in `groupReserve` (`scripts/ci/pr-gate/reserve.go`),
`reserve=480` in `ci-queue-retry.yml` and `scripts/ci/discover-dev-prs.sh`. A deferred
job writes nothing, so a record already on the head stays as it is until the rerun; a
feedback rerun's pending barrier can therefore be delayed by up to one window. A group
job is rate-limited much less often, not never. The reserve covers the completed
passes of two overlapping groups (2 x 3 x 41 = 246). On top of that come the pass on a
group's `requested` callback (measured on the fixtures: 17 reads and 1 write, about 18, so
about 36 more for two groups), the worker dispatch (a group job, which never defers),
the other workflows (under about 100 an hour, which do not check the reserve) and
concurrent non-group jobs that all pass the check together and then spend 42 each. Group
demand alone can exceed 480 within an hour, and so can the overshoot of those others.
The sizing is in the interim exception below. The residual is
completed by
`gh workflow run ci-queue-gate.yml -f pr_number=<n>`, which also completes a group
job that reached attempt 3 rate-limited (nothing reruns it automatically). The dispatch
job is deliberately not made to defer: a failed dispatch is not a job the retry follows,
so deferring it would leave group work undispatched until the next cron.

#### Interim exception to the delay-only rule (2026-11-26, ends when #1598 lands)

The accepted rule is that an overage delays a merge group and never drops it. The interim
exception, decided by the owner on 2026-11-26, is that a merge group can occasionally be
dequeued and re-queued when ungated spend exceeds the reserve: the group's check then
waits for the reset (up to about 61 minutes after a 36 to 73 minute merge CI) and can pass
the 120-minute response timeout, which the live ruleset 20539346 already has. No App token
exists, so the reserve stays at R = 480 (defer below 522) and the exception ends when the
separate token of #1598 (https://github.com/Hikyo-Org/Hikyo/issues/1598) lands.

Sizing, per hour (M measured, E estimated; the incident hour is 2026-11-23 21:00-22:00 UTC):

| Spender | Requests | Basis |
| --- | --- | --- |
| Event-driven dev discovery | 102 | M: 34 discovers x 3 reads |
| Retry first pass | 75 | M: 75 executed retry runs x at least one jobs listing |
| Two groups, three attempts | 246 | M: 2 x 3 x 41 |
| `requested` passes of two groups | 36 | M: about 18 each (fixtures) |
| Gated concurrency margin | 180 | E: 4 lanes x 45 (a dev pass is the gate's 42 plus the 3 reads of its post-write confirmation listing); `max-parallel: 2` on the dev publisher and head-worker matrices now bounds it per run, runs can overlap |
| **Measured total** | **639** | |
| Retry wait sweeps | at least 62 | E: 2 sweeps x (6 run listings + jobs listing per failed run in 3 h) |
| Attempt-3 bypass of a pass with an unconfirmable success | 87 per job | M: counted on the dev fixture, 82 reads and 5 writes: the pass 38 + 2, the confirmation listing 12, the fail-closed POST with its fail-safe re-list and re-POST (`queue_refresh.go`) 6 + 2, and the admission-reserve refusal phase (`withAdmissionBudget`, `refuseAdmission`) 26 + 1. The last attempt spends instead of deferring, which is why it is ungated there; the 87 is what such a pass costs on every attempt, not only attempt 3 |
| Ungated in-repo workflows (`perf-control`, `ci-feedback`, `report-dco`, `tidy-checks`, `pr-benchmark`, advisory jobs, queue report) | 60 to 550 | E: about 55 runs an hour at 1 to 10 calls; per-run calls are not logged |
| First revocation pass (`dev-revoke`) | 70 | E: 7 events x (3 + 7 heads); 7 push runs in the busiest hour of the last 100, 7 open dev PRs; never deferred |
| **Honest lower bound** | **918** | measured plus the lower estimates |

R = 480 is below 918. With 658 allowed to gated work (1000 - 522 + 180), the pinned
worst case at the lower bound is 658 + 738 = 1,396 requests, 39.6% over the window. That is
above the 25% over-window the owner accepted for the exception (Q2), by 14.6 points. The
owner accepted this figure on 2026-11-26 as part of the dated interim exception, which ends when #1598 lands. The post-write
confirmation listing is gated spend, inside the dev pass after the reserve check, so it is in
the margin (45 per pass) and not an ungated row: a created-record pass that confirms costs 41 + 2
requests and fits. The bound counts one attempt-3 bypass per hour. A pass whose confirmation
never catches up costs 87 on every attempt, ungated at attempt 3 (counted on the fixture), and a lane that lags costs
that, not 45, in the margin as well. Attempts 1 and 2 of such a pass are gated like any pass;
under sustained listing lag on a fan-out, each of N heads can spend 87 ungated at attempt 3
(N x 87), and N lagging lanes cost 87 each in the margin. That is disclosed here, not counted:
it needs a sustained GitHub listing outage at the same time. A listing is only needed when the
pass created its record with a POST (a PATCH in place keeps a lower ID than any later
revocation), so 45 is a ceiling for the normal case, not the usual cost. The
retirement fan-outs of `ci-check-retirement.yml` are not capped and not counted (hourly
discovery, small). Expected drop frequency (an estimate): ungated spend near the incident
level while a merge group is in flight. Over the last seven days 25 of 168 hours (15%) had
both a merge-queue run in flight (43 hours, from 31 merge-group runs) and at least 195
runs created (half the incident hour's 390; 45 hours); that is an upper bound on the share
of hours at risk, from run counts as a proxy for spend, and only a rate-limited group whose
wait then passes the timeout is dropped. `TestReserveSizingIsPinnedAndDocumented` pins the
numbers above and fails if R ever covers the bound, so the exception cannot outlive it.

##### Revocation is not queued behind the throttle

A push to `main` or `dev`, or a dispatch for every PR, can make every earlier
`ci-dev-required` success stale. The `publish` matrix reconciles heads at most
`max-parallel: 2` at a time, so on its own a head at the end of a large matrix would keep
its stale success for many waves. The `dev-revoke` job of `ci-dev-gate.yml` therefore runs
first, outside the matrix: one job runs `pr-gate --dev --dev-revoke` on the discovered
work and writes a blocking pending record on every routed head that is still open on `dev`
at that head. It never reads the rate limit and is never deferred. `publish` needs it but
runs even when it fails or is skipped (`!cancelled()`). A head the first pass cannot
confirm (moved, closed, retargeted) is left to its own publisher run or retirement.

Cost of the first pass: the repository, the open PR list and the dev tip are read once, and
each head costs one record create, so 3 + N requests for N routed heads (N up to 100 per
page); the sizing table counts 7 events an hour at N = 7. The alternative, exempting push
and dispatch from `max-parallel`, costs nothing extra but removes the throttle on exactly
the events that fan out, which is the large-fan-out margin the throttle exists for, so it
was not chosen.

The first pass is never rerun. GitHub reruns a job together with every job that needs it,
and every dev publisher needs `dev-revoke`, so a rerun would cost one reconcile per head
(about N x 42, and at attempt 3 the reserve bypass applies to all of them). The structure
prevents it: the job has `continue-on-error: true`, so a failed first pass does not fail the
job. The publisher itself is cut at two minutes, inside a four-minute job timeout; the checkout
and the build of the publisher sit outside that cut, so a slow or failed checkout or build, or the
job timeout, can still end the job as failed or cancelled: in that case the job reports
`failure` or `cancelled`, which is why the retry must handle it (below). `ci-queue-retry.yml` counts `dev-revoke` in the failed total of both
filters but never qualifies it, so a cancelled one is never rerun and forces job-level
reruns of any failed publishers beside it (the run-level rerun skips the per-job reserve
gating); a failed first pass announces itself with an `::error::` annotation and a step
summary (the last step of the job, `if: failure() || cancelled()`, because a job timeout is a cancellation), since `continue-on-error` hides it from
the run's conclusion; and the jobs the retry does reorder or rerun by id
(`publish`, `group`) have no dependents. `TestDevRevokeRerunCannotCascadeIntoThePublishers`
models GitHub's dependent rerun on the three workflows, and
`TestQueueRetryWaitNeverRerunsAFailedDevRevokeOrTakesTheRunLevelPath` runs the wait job against a
run whose revoke failed beside failed publishers. Assumption, unverified against GitHub's
documentation: a `continue-on-error` job that fails reports `conclusion == "failure"` in the
jobs API, so the retry's filters see it and count it in the run total. If it reported
`success` instead, it would not be in the total at all and a run-level rerun of failed publishers
would be the normal path, which is the case without a revoke and is not harmful: the revoke
would not be among the failed jobs that rerun selects.

The revocation sticks. The record carries the title `PR validation revoked`.
- A publisher that began before it (its authenticated job start is more than 3 seconds
  before the record's `started_at`) never rewrites it in place: not with a success, and not
  with a pending or refusal result, which would lose the title. It ends as stale work. The
  3 seconds absorb the difference between a job start and an API write, both GitHub's
  clocks; the reconcile the same event queued starts after the revoke job ended, tens of
  seconds later, and a stale pass began before the event that preceded the revocation by a
  discovery job, a checkout and a build, so neither lands in that margin. A record whose
  start cannot be read fails the pass (it is retried) rather than ending it quietly.
- A pass that created its own record with a POST (it listed the checks while the newest
  record was completed or absent, or created its pending barrier) can end with a record whose ID
  is above a revocation written between its listing and the creation. After a success write the
  pass therefore lists the head's checks once more (the suite-aware listing and bounded waits every
  other write confirmation uses, 3 reads, counted in the 45 per pass of the sizing table) and acts
  only on a listing that shows the exact success it wrote as completed; a listing with the right
  ID in another status is not a confirmation. If the listing is absent, older, in another status or
  an error, the pass fails closed: a blocking record is written over the success it cannot
  confirm and the pass fails (it is retried). That failure is an unconfirmed success, and like a
  rate-limited one it never becomes a terminal refusal on the last attempt: the record stays
  pending. The proof is finished at that point, so the listing and the fail-closed write run
  under the write context the work clock cannot cancel, not under the work context that may
  already have expired; the listing and its backoff stop 1.5 s before that context ends, so the
  fail-closed write always has time. If a revocation newer than the pass sits below its record, a fresh
  blocking record that keeps the revocation's start replaces it. The stale success is on top
  for the time between its write and that listing, a second or two; the queued reconcile then
  proves the head again. A record found and patched in place keeps its ID, so a revocation
  written after it stays above it and no listing is needed; and a revocation already in the
  listing a pass reads before writing a success, below another pass's barrier, ends the pass as
  stale work instead of turning that barrier green.
- The final phase of every pass already rereads policy, dev tip, feedback, DCO and roles
  before a success is written.

`TestDevRevocationSticksAgainstAnOlderPassSuccessWrite` pins the boundary (equal to the
margin is older, one second more is newer), `TestDevRevocationSurvivesAnOlderPassThatListedACompletedRecord`
the completed-newest interleaving, and `TestDevRevocationIsNeverRewrittenByAnOlderPass` the
pending and refusal rewrites, and `TestDevPostWriteListingFailsClosedWhenItCannotConfirmTheSuccess` the
lagging, absent and failing listings, `TestDevPostWriteListingWithOurIDAtAStaleStatusIsNotAConfirmation` the
listing that holds the written ID at a stale status, `TestDevPostWriteConfirmationSurvivesAnExpiredWorkContext`
the work clock expiring right after the success write, `TestDevUnconfirmedSuccessStaysPendingOnEveryAttemptIncludingTheLast`
the final attempt, `TestDevRevocationWrittenAfterAnInPlaceSuccessStaysOnTopWithoutAListing` the in-place
write that needs no listing, and `TestDevRevocationBelowAnotherPassBarrierBlocksAnOlderPassSuccess`
the barrier above a revocation.

Residual. The first pass can fail or be cut for reasons other than an exhausted window: a
transient API error, a bad dev ref, a failed build of the publisher, a failed checkout, the
two-minute cut of the publisher or the four-minute job timeout. It cannot write then, and when
the window is exhausted the publishers defer for the same reason. A head the first pass did not
block keeps its stale green until its own publisher runs: about N/2 minutes for N routed heads
at `max-parallel: 2`, or, if that publisher defers, until `rerun-after-reset` reruns it, up to
about 61 minutes after the reset plus the sweep. A head whose first pass succeeded stays blocked
while its reconcile defers; a head whose first pass failed does not. No token other than
`GITHUB_TOKEN` exists until #1598, and the first pass is the cheapest write that can revoke.

Sibling matrices. `ci-queue-worker.yml` has the same `max-parallel: 2`. Its `publish`
workers write PR-head queue admissions, and a stale one only lets a head enter the queue:
the merge group is judged by its own full proof, which never reuses an earlier success, so
a delayed revocation there cannot merge a stale head. `ci-queue-retry.yml` only reruns
failed jobs and publishes nothing, and `ci-check-retirement.yml` only retires unfinished
records. `TestDevGateRevokesEveryRoutedHeadBeforeThrottledWork` pins the dev ordering and
`TestDevRevokeBlocksEveryRoutedHeadWithoutThrottledWork` pins that every routed head gets a
blocking record without any verifier work.

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
returns at most 1,000 results for a filtered list, and retention can delete old runs, so
the count is taken within the current 30-day epoch only (`created` at or after the epoch
start, at most 720 runs) and the epoch number is added as a stable offset: windows are
consecutive within an epoch and the sequence restarts at an arbitrary offset once per
epoch, a shift of at most one cycle (a `shortcut:` in the script; a persisted cursor would
remove it and needs a write the job does not have). If the count cannot be read the clock hour stands in. Unless a bucket can afford
one publisher above the reserve (522) the sweep selects nothing. The cap delays drift detection and recovery of a
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
app's same-name record. The controller and retirement refuse one only when its
external ID carries the controller prefix or it has the `github-actions` app ID
or slug. The reporter refuses one only when its external ID matches the
report's. They refuse when the `github-actions` suite total changes during a
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
