# Core database scheduling repair

Base: `407456a54dbb275cf322bbf274faaf78b8845260`. Branch: `fix/core-database-scheduling`. Worktree: `/tmp/hikyo-core-scheduling`. Parent owns review, signed commit, PR and merge on green.

The app expired-owner test completed its authority assertions, then its five-second PostgreSQL database-drop cleanup deadline expired during a 6.444-second shared checkpoint (6.180 seconds syncing files). Concurrent package migrations are the inferred source of checkpoint pressure; the scheduling change removes that overlap, with hosted confirmation still required. Exact failure: PR673 f31, run33969008887, job101314181251. See [decision and evidence report](../reports/1.0/core-database-scheduling.html).

The scheduler runs every non-isolation/non-app package with the existing `-count=1`, waits for completion, then runs app exactly once. Either group's failure fails the step; app still executes after an earlier group fails. Isolation keeps its existing dedicated shards. No timeout, fsync, datastore/authentication bound or Go source changes.

This must land separately before PR673: trusted `ci-control` invokes main's workflow, whose current inline command cannot call a scheduler added only on PR673. After this repair merges, parent rebases PR673 onto the merged base and reruns exact-head CI. The prior uncommitted scheduling patch/report section was removed exclusively from PR673, restoring f31 clean.

Validation retained: five isolated f31 runs, all four restore scenarios, 25 named passes and zero skips, 40.477 seconds. Scheduler regression checks exact coverage/order, failure in either group, and missing/duplicate/empty concurrent inventory refusal. The empty case requires its exact diagnostic. ShellCheck, Actionlint, cache/required-job policy and diff whitespace checks passed. Hosted exact-head validation remains pending.

Actual main-407 inventory replay: 96 packages listed, 95 dispatched once, app last and only isolation excluded. The earlier f31 proof listed 98 packages because it includes two added diagnostics packages. Dispatch evidence is not a full-suite execution. All author checks above passed again after the move and empty-inventory correction; no active runner remains.

## Trusted fixture follow-up

PR674 60e5 exposed a stale supply-chain assertion requiring the replaced inline `go list` command. The fixture now pins the two scheduler invocations and runs its coverage/order/failure regression directly, retaining all existing authority checks. Baseline refusal reproduced; focused fixture and ShellCheck passed; independent R2 CLEAN.

All 38 supply-chain workflow run blocks passed locally, including actual Cosign interoperability/readable restore signing. The local chart mutation check resumed with GNU sed after identifying the BSD sed prerequisite mismatch; earlier passing Go/Cosign checks were retained. Helm4.2.3 archive checksum verified; Python3.14/PyYAML used through local PATH only. External `core-scheduling-supply-chain/{result,proof}.json` records exact scope and all outcomes. Updated hosted exact-head CI remains pending. No scheduler/runtime changes; parent owns signing and delivery.
