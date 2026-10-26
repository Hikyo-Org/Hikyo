# Serial isolation analyzer race checks

## Problem and change

Scheduled reliability run [40301445942](https://github.com/Hikyo-Org/Hikyo/actions/runs/40301445942), attempt 1 at main revision `4a7beff3272346fb46fec5da058506f1d558282e`, failed in `TestInvariant09bTransactionResultsAreDetached`. The report identifies concurrent `go/packages` workers reading and writing Go 1.27.3's `types.Named.fromRHS`. This matches [golang/go#81122](https://github.com/golang/go/issues/81122); the transaction-result analyzer had not run yet.

The reliability planner now extracts the six invariants that call `lint.LoadRepo` into a separate `race-analyzers` job. It starts Go with `GOMAXPROCS=1`, before `go/packages` initializes its worker pool, and runs all six in one process so they share one cached repository load. The eight runtime race shards keep their existing assignments and concurrency. Both job types retain `-race`, the shared module/compiler cache readers, failure propagation, and timing artifacts through `scripts/ci/test-isolation-race.sh`. The analyzer job does not provision PostgreSQL.

The planner compares the union of runtime and analyzer targets against the complete independent inventory, including fuzz seeds and runnable examples. Missing, stale, and duplicate targets fail before outputs are published. A source-based regression fixture ties the serial list to direct `LoadRepo` and `LoadRepoIn` callers; update that list if a new repository-loading invariant is added. If loading moves behind helpers, update discovery coverage as part of that change.

## Validation and operational boundary

Regression fixtures execute the checked-in planning shell and shared test runner. They cover exact-once grouping, missing and duplicate targets, planner failures, Go/compiler failures, target validation, pre-start analyzer serialization, unchanged runtime concurrency, output wiring, and shared cache-reader policy.

Before pushing, run the shared feedback selection and affected owner tests:

```sh
scripts/ci/fast-feedback.sh --working-tree --plan
scripts/ci/fast-feedback.sh --working-tree
go test -p 2 ./scripts/ci -count=1
```

Local checks establish source and fixture behavior. The PR feedback workflows do not execute this scheduled reliability graph. Hosted reliability qualification and performance measurement remain pending until a maintainer runs the `race` suite at the intended revision. Creating this PR does not request that costly run or authorize merge.

## Performance and removal

The failed run spread the six analyzer tests over five processes. Their measured test time totaled 20m47s; the longest runtime shard included about 4m45s of analyzer work. Consolidation removes four repeated repository loads, but the remaining serial load can take longer individually. No hosted speedup is claimed. Compare complete workflow duration, runner time, queue delay, and the `isolation-race-timings-analyzers` artifact against the baseline after qualification. The analyzer job needs an additional runner, so repository-wide capacity can affect scheduling.

Remove the temporary split only after the pinned Go toolchain contains the upstream fix and concurrent analyzer loading passes under the race detector. The proposed upstream repair is [golang/go#81871](https://github.com/golang/go/pull/81871); it was still open when this change was prepared.
