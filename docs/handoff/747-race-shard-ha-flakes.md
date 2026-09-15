# #747 — Flaky race-shard HA-coordination failures

## Status

The two **named** failures were already fixed on `main` before #747 was filed
(2026-09-15). The issue cites CI runs that predate their fixes — see below.
This change adds the one remaining sibling of the anti-pattern (a genuine
sleep-then-assert in the HA scheduler integration test) and recommends closing
#747.

## The two named failures were already fixed

#747 cites two runs. Both predate the commit that fixed them, so the issue is
quoting stale red:

| Cited run | When | Failure | Fixed by | When |
|-----------|------|---------|----------|------|
| 34328354454 | 2026-09-09 08:18 UTC | `self_config_topology_repair_test.go` — `repaired owner did not retain shared admission` | #718 `7bc43e6f` | 2026-09-09 09:26 UTC (~1 h later) |
| 33938375023 | 2026-09-05 02:11 UTC | `approval_acceptance_test.go` — `scheduler did not acquire leadership` (5 s failsafe) | #708 `32794d60` | 2026-09-07 (2 days later) |

What each fix did (verified against the diffs):

- **Repair** (#718): the old assertion built a second `peerLimiter` and checked
  a single `AllowDiscovery` boolean, which straddled a minute-window boundary —
  a real minute-boundary flake. It now consumes `MetaPerIPPerMinute` requests
  and asserts `SUM(hits)` across **all** minute windows for the subject, so the
  boundary can't split the count. Current L214–L229.
- **Approval** (#708): the old scheduler gave lease calls a `Heartbeat: 20ms`
  budget — below one round trip on a loaded CI Postgres, so contention turned
  into repeated claim timeouts that blew the 5 s ceiling. Raised to `100ms`
  (with every node's log retained for diagnosis). Current L286–L323. #708 also
  **spread `internal/app` race tests across every race shard**, and #733
  (2026-09-13) balanced race coverage across six shards — the shard rebalance
  is done.

Line numbers in #747 (`:227`, `:277`) point at the **pre-fix** files; current
code at those lines is different, which is how the staleness first surfaced.

No recurrence on `main` since the fixes: `--log-failed` of the post-fix main
failures (34747598944 #733, 34459740003, 34125515793) contains neither test
name nor either failure string; the rest are `nightly-release` and dependabot.

## Fixed here

`internal/app/scheduler_ha_integration_test.go` —
`TestSchedulerHAThreeNodesOnePostgres` had two genuine positive
sleep-then-assert sites (`time.Sleep(300ms)` then `jobRuns == 1` / `== 2`).
Under `-race` the startup catch-up need not have run within 300 ms → false
`FAIL`. Replaced with the existing `waitFor` helper (`scheduler_test.go:66`):
wait for `jobRuns >= N`, then assert `== N`.

This removes the false-fail on the positive half (the run is guaranteed to have
landed before the assert). The *duplicate* guard — that no node ran the job a
second time — does not rest on the assert (which now fires the instant the
count reaches N); it rests on `waitForSingleLeader` proving exactly one leader,
plus the hour `Interval` that stops anything but the startup catch-up from
firing. `go vet ./internal/app/` clean; `go test -run TestSchedulerHA` green
(the Postgres leg skips locally — `HIKYO_TEST_POSTGRES_DSN` unset).

Audited `internal/app` + `internal/isolation` HA/scheduler tests for the same
anti-pattern. The only other fixed sleep is `scheduler_test.go:95`
(`TestSchedulerHAOnlyLeaderRunsJobs`): a **negative** assertion (`runs == 0`
after 60 ms). A fixed sleep is correct there — more time can only expose a real
bug, never false-fail — so it is left alone.

## Recommendation

Close #747: both named failures are fixed on `main` (#718, #708) with no
recurrence, and the shard rebalance the issue's cause would motivate is already
done (#708, #733). This change lands the last sleep-then-assert sibling.

## Reproduce (needs Postgres)

```
HIKYO_TEST_POSTGRES_DSN=... go test -race -count=10 \
  -run 'TestSchedulerHAThreeNodesOnePostgres' ./internal/app/
```
