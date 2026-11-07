# Handoff: dev merge refused with "ci-dev-required is expected"

## Result

GitHub sometimes refused to merge a dev PR with `Required status check
"ci-dev-required" is expected`, although a successful `ci-dev-required` from
`github-actions` (app 15368) existed on the exact head. `gate-dev` no longer runs on
`pull_request_target` `edited` events, which removes the observed trigger.
`scripts/ci/dev_gate_workflow_test.go` pins the trigger set.

## Root cause

This model is inferred from suite and run data; GitHub documents neither rule.
It explains both blocked heads and every merge in the sample below.

1. A check run that a workflow creates through the Checks API with `GITHUB_TOKEN` is
   filed in the oldest `github-actions` check suite on the head, whichever workflow
   owns that suite. The run that wrote it does not matter.
2. GitHub judges a required check only by the newest check suite of each workflow
   on the head. A rerun reuses its suite and does not count as newer.
3. When the host suite belongs to `gate-dev` and a later `gate-dev` run starts on
   the same head, the record disappears from the required check. Republishing
   cannot help, because each new record lands in the same hidden host suite.

Evidence, from `actions/runs?head_sha=`, `commits/<sha>/check-runs` and the
timelines:

| PR | Head | Host suite (workflow) | Superseding run before merge |
| --- | --- | --- | --- |
| #1173 | `39efcbc53` | 113114482715 (`gate-dev`) | 41741906040, an `edited` run at 23:47:32, one second after the push |
| #1167 | `63deb741c` | 114557434322 (`gate-dev`) | 42347746828 (renamed and ready at 06:36) and 42347884389 (06:38), both `edited` |

- On #1167 the record written at 06:36:41 by run 42347746828, and the three records
  written by the 06:47 rerun of 42279197760, all landed in 114557434322. None of
  them cleared the block.
- The fresh heads `1bf298a01` and `f848ce635` hosted the record in
  `release-ops-floor` and `perf-control` suites, which never reran, so both PRs merged.
- In the 39 merged dev PRs sampled (#1158 to #1249), the record was always in the
  oldest Actions suite, and no host workflow reran on the head before the merge.
  Hosts: `perf-control` 16, `report-dco` 8, `tidy-checks` 6, `gate-dev` 3,
  `ci-advisory-invariants` 3, and `release-ops-floor`, `trusted-merge-ci` and CodeQL
  one each. #1234 and #1211 had a later `gate-dev` suite, but only from the `closed`
  event at the merge itself.
- The delegated idea of republishing from the newest `gate-dev` run would not work:
  the new record still goes to the oldest suite.

## Why removing `edited` keeps admission complete

- The publisher never reads the title, body or draft state. Its binding covers
  repository, PR, head, base SHA, base ref and feedback attempt.
- A base retarget reruns `ci-feedback`, which is not a metadata-only edit, and
  `gate-dev`'s `workflow_run` callback then publishes for the PR. Discovery maps the
  callback's head to every open dev PR on it. This is shown from the code: on #1158,
  retargeted to dev at 2026-11-04T16:46:23Z, the callback run 41870829461 completed
  `ci-dev-required` at 16:52:30, but a push and a `gate-dev` `edited` run arrived at
  the same time. `TestDevGateDoesNotRerunOnPullRequestEdits` pins both the callback
  and `ci-feedback`'s `edited` trigger.
- `ci-feedback` does not start when the PR conflicts with its new base, or while a
  fork run awaits approval. The hourly schedule and manual dispatch then reassess.
- Removing a trigger only removes publications. It cannot admit a head that the
  publisher would refuse.

## Rollout

- This PR targets `main`. CI and publisher definitions land through main first
  ([branching guide](../development/branching.md#feature-work)).
- `pull_request_target` reads dev's copy of the workflow, so dev PRs keep the old
  triggers until a reviewed main-to-dev backmerge.
- The publisher contract refuses open dev PRs between the main merge and the
  backmerge ([CI guide](../development/ci.md)). Backmerge promptly, then merge dev
  into each open dev PR.

## Remaining risks (not fixed)

- Closing and reopening a PR reruns every workflow that hosted the record in the
  sample: `tidy-checks` and `gate-dev` on `closed`, and `gate-dev`, `perf-control`,
  `report-dco` and the `pull_request` workflows on `reopened`. A reopened dev PR
  still needs a new signed head.
- Other workflows can still rerun on an unchanged head: `ci-feedback` and
  `ci-advisory-dependencies` on metadata-only `edited` events (as skipped runs),
  `pr-benchmark` on `ready_for_review`, and the non-skipped `ci-feedback` and
  `ci-advisory-dependencies` runs of a real base retarget. None of them hosted the record in the
  sample, but the `pull_request` workflow `ci-advisory-invariants` hosted it three
  times, so they can. Workflows defined in the PR branch itself can also add suites.
- `ci-queue-gate.yml` has the same trigger types on main PRs. Whether the queue
  check has the same exposure is unverified.

## Next steps

1. Merge this PR to main once it is reviewed and its queue proof is green.
2. Open the main-to-dev backmerge, then merge dev into each open dev PR.
3. If a dev PR is still blocked after the backmerge, list its suites with
   `gh api 'repos/Hikyo-Org/Hikyo/actions/runs?head_sha=<sha>'` and the record's
   `check_suite.id` from `commits/<sha>/check-runs?check_name=ci-dev-required`.
   Then check which workflow reran on the head. The workaround is still an empty
   signed commit.
