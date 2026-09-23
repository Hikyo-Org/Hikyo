# PR CI: slot pressure, web setup waits, weighted race shards

Branch: `ci/speed-up-pr-ci`.

## Problem

PR CI took 17 to 20 minutes alone and 25 to 43 minutes when two PRs overlapped
(run [35871072451](https://github.com/Hikyo-Org/Hikyo/actions/runs/35871072451),
PR #793, against `feat/605-social-signin-migration`). Three independent causes:

1. **Runner slots.** Hikyo-Org is on the GitHub Free plan: 20 concurrent
   GitHub-hosted jobs org-wide, CodeQL default setup included. A full-plan PR
   run expanded to 44 jobs. Web legs queued 7 to 12 minutes, `no-egress` (25s
   of work) 11.5 minutes, the `test` gate 6 minutes after `test_core` ended.
   More shards make this worse, not better. Upgrading the plan was rejected.
2. **Web globalSetup regression.** Since #786 (`76f77920`) every web leg spent
   ~4m55s in `startInstance` before its first test, up from 2m15s: about eight
   TOTP ceremonies across two admins ran serially, each waiting up to 30s for
   an unspent step. Test-body time did not change.
3. **Unbalanced race shards.** Packages were placed by FNV hash and a hand-kept
   pin map; app and service were split by test-name hash. Shards ran 9.9 to
   17.4 minutes (run 35847641622).

## Changes

- **Fewer jobs** (`ci: fold gate and tiny jobs to cut runner slots`).
  `no-egress` runs as the last steps of `web-go`. The `test`, `race` and `fuzz`
  gate jobs are gone; `ci-required` needs `test_core`, `isolation_shard`,
  `race_shard` and `fuzz_shard` directly and merges fuzz reproducers only when
  a fuzz shard failed (artifact name unchanged for `fuzz-report.yml`). `dco`,
  `freeze-guard` and `generated` share one `preflight` job with step-level plan
  gates; `docs` stays separate (Playwright container, no `gh`). Plan keys are
  unchanged, so `check-required-jobs.sh` is untouched. 44 to 38 jobs.
- **Web setup** (`ci(web): overlap e2e TOTP setup ceremonies`). The viewing and
  serving admin chains run concurrently; redundant step-ups after a login
  challenge are dropped (the challenge already mints `[password, totp]`, which
  `authz.AdequateAssurance` accepts with no age check); the serving admin gets
  the same spent-step ledger as the viewing admin; `nextTotpCode` uses the
  current step when it is unspent. Local desktop `login.spec.ts`: globalSetup
  306.5s to 129.8s. Full desktop and mobile suites green. Harness only; no
  server behaviour changed.
- **Race balance** (`ci(race): weight-balance race shards by measured duration`).
  LPT packing over weights embedded in the planner source (PRs run the base
  planner, so weights cannot come from the PR checkout). Store, store/upgrade
  and upgradegate are now split by test name like app and service, and run in
  the two-wide pool; app and service stay sequential after it. Predicted test
  time is ~648s on every shard (was 473 to 988s). Regeneration commands sit
  above `racePackageSeconds` and `raceTargetSeconds`; `main_test.go` fails when
  a shard predicts over 110% of the mean. The race shard cache key includes
  the planner's hash: saves run only on an exact-key miss, so without it the
  reshuffled shards would recompile their new suites until go.sum changed.

## Not done, on purpose

- Dropping the non-race `test_core` run: rejected, both runs stay.
- Server-side step-up grace after login: rejected, product auth change.
- Passkey for the serving admin: moot once its chain overlaps the viewing one.
- Enrolling the viewing admin's passkey earlier would save ~30s per leg more.

## Verification still required after merge

PR validation runs the base branch's `ci.yml`, planner, race script, registry
and checker, so only the web change is visible on this PR's own run. On the
first main push after merge, and one later PR run, check: 38 jobs; race shard
test steps within about a minute of each other (expect ~13.5 to 14.5 minutes a
job including setup, down from a 17.4 maximum); web leg globalSetup near 2
minutes; `ci-required` green; a deliberately failing fuzz shard still uploads
`fuzz-reproducers-<run_id>-<attempt>`.
