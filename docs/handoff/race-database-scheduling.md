# Race database scheduling repair

Base: `ee8924e6d1a1d2361343e14744f88f71ecbe58a1`. This is the CI prerequisite for GitHub adapter PR #677.

## Failure and cause

PR #677 head `b1759366c953fd488ceecb80961e6ce44da86501`, run `33992983950`, job `101378606464`, failed `TestUpgradeDrillActualBothEngineRecoveryAndConfigOnlyEscrow/postgres/secret=true`. Line 58 of `backup_upgrade_drill_test.go` reports fixture cleanup, after the recovery assertions: its five-second `DROP DATABASE ... WITH (FORCE)` context expired.

The PostgreSQL log records a forced checkpoint starting at 21:32:18.801 UTC. The database drop was canceled at 21:32:23.803, and the checkpoint completed at 21:32:23.806: 5.005 seconds total, 4.962 seconds syncing files. The race shard dispatched the application package alongside peer packages against one PostgreSQL service. This repeats the cleanup/checkpoint contention previously addressed for ordinary core tests by PR #674.

## Change and trust boundary

The new race scheduler preserves each shard's assigned package set and the existing `-race -p 2 -timeout=20m -vet=off -count=1` flags. It runs peer packages first, then application tests alone. It runs application tests even if peers fail and propagates either failure. Empty, duplicate, unknown, blank, option-like and isolation-package entries are refused before test execution. Shards without the app and app-only shards remain valid.

Race dispatch remains base-controlled. Pull-request jobs retrieve the scheduler from `BASE_SHA`, like the existing trusted package planner; trusted main pushes copy the committed scheduler. The race checkout fetches history for that exact-base lookup. PR-head code cannot replace the scheduler to omit assigned tests. Existing workflow credentials and write permissions are unchanged.

Recommended choice: remove cross-package checkpoint pressure through scheduling. Alternatives were raising the cleanup deadline, weakening PostgreSQL durability, or adding fixture coordination locks. No production code, test assertion, cleanup deadline, package timeout, durability setting or required coverage changes.

This CI repair must merge on green before PR #677 is rebased and retested. The trusted controller uses main's workflow, so an invocation changed only inside #677 would not fix that PR's current execution. Local workflow and scheduler validation cover the proposed implementation; hosted execution of the new scheduling begins after this prerequisite lands.

## Validation

The exact old inline invocation failed the new scheduling regression because app and peers shared a test batch. The new helper passes nine execution/failure combinations and 13 invalid-input cases, pinning exact coverage, app ordering, unchanged flags and failure propagation. ShellCheck, Bash syntax, Actionlint, trusted-CI behavior checks and diff whitespace checks pass.

Five isolated repetitions of the actual dual-engine recovery test under the race detector passed: 25 named PASS events, zero failures or skips, 152.645 seconds including compilation. The unchanged app source is bound to base `ee8924e6`; PostgreSQL ran in a dedicated disposable container. This confirms the fixture in isolation, while the hosted logs establish the original checkpoint wait. It does not claim a locally reproduced shared-runner I/O load.

The [HTML report](../reports/1.0/race-database-scheduling.html) records the decision. Red/green scheduler logs and source hashes remain in the local `race-cleanup-repair` artifact directory; the original hosted log and isolated real-test JSON remain under `github-acceptance-prerequisites`.
