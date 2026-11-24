# Handoff: a PR edit hid `ci-dev-required` through `ci-feedback`

## Result

On 2026-11-14, merging #1374 (base `dev`) failed with `Required status check
"ci-dev-required" is expected`. A successful record existed on the head, and
GraphQL `statusCheckRollup` reported it as required and SUCCESS. `gh pr merge`
and `gh stack merge` were refused the same way.

Fix:

- `ci-feedback.yml` no longer runs on `edited`. It gains `workflow_call`.
- The new `ci-feedback-retarget.yml` runs only on `pull_request` `edited`. For a
  genuine base retarget, it calls `ci-feedback.yml`.
- The new workflow is also named `ci-feedback`. The `gate-dev` and `gate-queue`
  `workflow_run` callbacks select workflows by name, so they reassess the PR without
  changing either gate workflow.
- The publisher (`scripts/ci/pr-gate/queue.go`, `feedback`) reads the runs of both
  workflows at once and uses the newest useful run.
- `scripts/release/configure-dev.sh` lists `ci-feedback-retarget.yml` among the
  activation inputs that must match trusted main.
- The advisory `ci-advisory-dependencies` workflow had the same `edited` trigger.
  It dropped `edited` and its metadata-skip plumbing.

## Root cause

This is the same model as [dev-gate-stale-required-check.md](./dev-gate-stale-required-check.md).
It is inferred from suite data; GitHub documents neither rule.

1. GitHub files a `ci-dev-required` record created with `GITHUB_TOKEN` in the
   lowest-id `github-actions` suite on the head, whichever workflow owns it.
2. A newer suite of that same workflow on the head hides the record from ruleset
   evaluation. Suites of other workflows do not hide it.

Evidence (`commits/<sha>/check-suites`, `check-runs`, `actions/runs?check_suite_id=`):

| PR | Host suite (workflow) | Newer suite of the host workflow | Outcome |
| --- | --- | --- | --- |
| #1374 `bc62d112f` | 117920054950 (`ci-feedback`, opened, 02:57:09) | 117921004839 (`ci-feedback` `edited`, skipped, 03:01:16) | blocked |
| #1288 `55ab7397b` | 116029609471 (CodeQL dynamic) | none. A skipped `ci-feedback` `edited` suite at 15:26:56 belonged to another workflow | merged 15:27:36 |

- 96 heads among the last 100 dev PRs carry a record, and every record was in the
  head's lowest-id Actions suite. Hosts with a known workflow: `perf-control` 26,
  `gate-dev` 17, `report-dco` 16, `tidy-checks` 15, `ci-advisory-invariants` 6,
  CodeQL 2, `release-ops-floor` 2, `ci-feedback` 2, `ci-advisory-golangci` 2 and
  `perf-pr` 1. The remaining 7 host suites no longer listed a workflow run.
- On #1201 a record POSTed at 12:23:18 still landed in the lowest-id suite. A newer
  `ci-feedback` suite already existed by then. This shows that republishing cannot
  move a record into a visible suite. Whether #1201 was closed because of this is
  not established.
- The same mechanism hid records after base retargets. A real retarget ran feedback
  in a newer `ci-feedback` suite. If `ci-feedback`'s opened suite hosted the record,
  every later record also landed there, hidden. This is derived from the two rules
  above, not observed directly. `ci-feedback-retarget` never runs when a head first
  gets suites, so its suites are never the lowest id and never host a record.

### Unverified: supersession by file or by name

The new workflow shares the name `ci-feedback`. The fix for records that
`ci-feedback` hosts assumes GitHub supersedes suites per workflow file. Nothing
observed distinguishes the two: every sampled workflow that did not hide a record
had both another file and another name, and no two workflows in this repository have
ever shared a name (`actions/workflows`, 2026-11-14). If GitHub keys by name, an
edit or retarget still hides a record hosted in `ci-feedback`'s opened suite, as
before this change, and fails closed until a new head. Records hosted by any other
workflow, 94 of 96 in the sample, are unaffected either way. Next step 4 settles
it. The fallback is a distinct name added to the `gate-dev` and `gate-queue`
callback filters and the publisher's source list; the publisher contract allows
the `gate-dev` edit only after the backmerge.

## #1375: duplicate suites from a stack force-push

#1375 (head `704cbbe2a`) was refused the same way after a retarget to dev at
14:32:47. The retarget did not hide the record:

- The record (14:43:06) sits in suite 118062886878, the first `tidy-checks`
  suite (14:09:52).
- A second `tidy-checks` suite, 118062891659, already existed at 14:09:53. The
  base and head refs were both force-pushed at 14:09:50 and 14:09:51, and most
  workflows on the head got two suites one second apart.
- So the host suite was superseded 20 minutes before the retarget, and the record
  was hidden when it was written. The retarget's newer suite is `ci-feedback`'s,
  another workflow, and on #1288 and #1186 a newer suite of another workflow did
  not hide the record.
- #1376 and #1377 show the same duplicates. #1384 had the same two force-pushes in
  one second but single suites, so the duplication is a race, not a rule.

No trigger change prevents this: GitHub starts every opened or synchronize
workflow twice. The remedy is a new signed head pushed on its own. Before
expecting a merge, check that no workflow has two suites on the head (query below).

## `gh stack merge` and stacked layers

- An atomic stack merge judges every layer by the stack base's rules. Each layer
  therefore needs a visible `ci-dev-required` record.
- Discovery already publishes for layers whose `stack.base.ref` is `dev`
  (`docs/development/ci.md`).
- The #1374 refusal was the hidden record. With this fix, neither a body edit nor a
  retarget of a layer starts a newer suite of a workflow that can host its record,
  subject to the file-or-name question above. Nothing further is needed for stack
  merges.
- Not changed, noted only: stacked-layer admission fails closed when the cumulative
  compare of the `dev` tip against the head lists 300 files or more
  (`scripts/ci/check-fork-validation.sh`, `PR_GATE_FILES_BASE`). A large stack can
  hit this limit.

## Rollout and version skew

- `pull_request` workflows run from the PR's merge commit. Dev PRs keep dev's
  `ci-feedback.yml`, with `edited`, until main is backmerged into dev. Until then,
  editing a dev PR after admission can still hide its record.
- The publisher runs from main. It still accepts the old-shape skipped `edited` runs
  of `ci-feedback`. Old heads keep those runs.
- This PR does not touch `ci-dev-gate.yml`. Open dev PRs are therefore not refused by
  the publisher contract after the main merge.
- Main PRs pick up the split when this PR merges.

## Remaining risks (not fixed)

- `perf-pr` (`pr-benchmark.yml`) runs on `ready_for_review`. It hosted one record
  in the sample, so marking that kind of draft dev PR ready can hide the record.
  Automatic benchmarks skip drafts, so the trigger is functional and stays.
- `gate-queue` (`ci-queue-gate.yml`) still runs on `edited` for main PRs. A body edit
  can hide a `ci-queue-required` record that its own suite hosts. This is unverified.
- `TestPullRequestWorkflowsDoNotRerunOnAnUnchangedHead` lists both as explicit
  exceptions. Any new head-preserving trigger fails it.
- Closing and reopening a PR reruns every workflow, so it still needs a new head.
- CodeQL dynamic runs are provider-owned. A CodeQL rerun on the same head would hide
  a record it hosts.
- Two head-creating events on one head, as in the #1375 stack force-push, start
  every workflow twice. The publisher cannot detect or repair it. A follow-up could
  make the publisher fail visibly when its host suite has a newer suite of the same
  workflow.
- Workaround for any blocked head: push a new signed commit on its own, for example
  an empty `git commit -S -s --allow-empty`.
- To diagnose a head, list `gh api 'repos/Hikyo-Org/Hikyo/actions/runs?head_sha=<sha>'`
  by `path` and `check_suite_id`, and find the record's `check_suite.id` in
  `commits/<sha>/check-runs?check_name=ci-dev-required`. The record is hidden when
  its host workflow has a newer suite.

## Next steps

1. Merge this PR to main after review and queue proof.
2. Backmerge main into dev, so dev PRs stop running `ci-feedback` on `edited`.
3. Until then, do not edit the title or body of an admitted dev PR. For #1374 to
   #1384, a new signed head clears a hidden record.
4. After the backmerge, find a dev PR whose record sits in `ci-feedback`'s opened
   suite (diagnostic query above), edit its body, then confirm it still merges. Do
   the same for a retargeted stacked layer. In the jobs API, confirm the edit's run
   has one skipped job named `retarget` and the retarget's lanes are named
   `retarget / feedback (go)`; any other shape fails admission closed. If either is refused, rename
   `ci-feedback-retarget.yml`'s workflow and add that name to the `gate-dev` and
   `gate-queue` callback filters and the publisher's source list.
