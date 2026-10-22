# SQLite transaction settlement before shutdown

## Release blocker and evidence boundary

Nightly [39841612047, attempt 1](https://github.com/Hikyo-Org/Hikyo/actions/runs/39841612047)
tested merged commit `83d310cd20526bf8b2ada817077c54596fd96bc2`.
Its SQLite `TestNextRootLiveSelectionKeepsRotationSeparate` subtest failed
while removing its temporary directory in the race shard. The required
aggregate correctly refused validation. Build, signing, notarization,
publication and container jobs were skipped; no release was published.

Ten local race-enabled repetitions did not reproduce that exact filesystem
error. A controlled probe did reproduce a production lifetime gap: after
cancellation, the server returned while a real SQLite native rollback was
blocked and its connection remained checked out. This is compatible with late
cleanup activity, but does not prove the cause of the historical filesystem
error. The original nightly did not report a Go data race. A later controlled
concurrent-finalizer regression reproduced a race in the old admission-guard
closure, independently of that historical failure.

## Repair contract

`database/sql` can start rollback independently when a transaction context is
cancelled. Another `Commit` or `Rollback` call can return `sql.ErrTxDone` before
that native rollback has finished. The runtime SQLite wrapper must join actual
transaction settlement before returning to server teardown or releasing host
admission.

- Own the necessary SQL resource privately; do not expose a native connection
  or transaction handle to runtime callers.
- Retain host reader/writer exclusion until native SQL has settled, including
  when context cancellation wins finalization.
- Concurrent finalizers must join the same settlement. One caller must not
  release admission while another is still waiting for native completion.
- Preserve cancellation, deadlines, write-pool `BEGIN IMMEDIATE`, read-pool
  restrictions, original errors, prepared-statement ownership and audit actor
  behavior. Close resources in order on acquisition and admission failures.
- Keep the existing app worker/request joins and filesystem cleanup assertions.
  Filesystem retries, sleeps or longer shutdown deadlines do not repair this
  ownership gap.

PostgreSQL behavior, authorization policy, migrations and the merged Apple
signing/notarization workflow are outside this repair.

## Validation and delivery

Deterministic regressions must exercise cancellation winning native rollback,
retained host admission and server shutdown waiting for settlement. Passing
teardown must release its diagnostic gates and join all owned workers even
when an assertion fails. Cover normal settlement, both SQLite pools,
concurrent finalizers and relevant acquisition/error paths. Rerun the original
SQLite flow and affected sibling fixtures on the final implementation.

## Implementation and local evidence

`BeginSQLite` owns a private connection from the existing read or write pool.
It begins the transaction with the original context and options. Commit and
rollback preserve native errors and join one shared connection close before
releasing the host guard. `sync.Once` makes concurrent finalizers wait for the
same close. Acquisition, begin and admission-check failures close resources in
order. An already-discarded connection does not add a spurious
`sql.ErrConnDone` to the original transaction result.

The reviewed raw-SQL protocol inventory tracks the changed helper bodies and
their reachable settlement helpers. The repository's explicit reviewed-pin
updater refreshed this closure while preserving protocol owners, callers and
authority declarations. Ordinary lint checks must validate the new inventory.

The shutdown regression has a separate teardown abort channel. If a native
rollback hook never signals, cleanup aborts the worker's hook wait, releases the
native gate and joins the server. A missing-hook regression exercises that
same teardown without weakening the normal automatic-rollback assertion.

The real SQLite rollback and commit gates failed on the original implementation
and passed after the repair. The regressions also cover concurrent finalizers,
host lock retention, both pools, cancellation and errors, failure cleanup and
the actual server shutdown path. The original SQLite live-selection flow and
affected sibling fixtures passed under the race detector. Store transaction
consumer and SQLite upgrade admission checks passed. Final shared feedback and
boundary checks passed. An initial complete lint run identified stale reviewed
helper pins; the refreshed raw-SQL protocol and reviewed-pin owner checks
passed. Independent source review remains separate from execution evidence.

Exact-head protected CI, merge and a newly dispatched nightly remain pending.
The failed run is immutable and cannot incorporate a source repair. The
authorized endpoint remains a new nightly with independently verified
publication and artifacts. Passing local regressions do not establish exclusive
attribution of the historical filesystem error.
