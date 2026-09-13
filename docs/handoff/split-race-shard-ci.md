# Six race shards

Branch: `t3code/split-race-shard-ci`.

## Problem and change

Main CI run [34725771747](https://github.com/Hikyo-Org/Hikyo/actions/runs/34725771747)
finished its three race shards in 17m25s, 22m50s, and 20m15s. The other validation
jobs finished about 12 minutes before the final race shard. Service alone took
965.968s; shard 0's app subset took 900.020s.

Race now has its own six-entry matrix. Fuzz and isolation retain three entries.
App and service targets hash uniformly across shards 0 through 3. Store and lint
use shard 4; upgradegate and store/upgrade use shard 5. Other packages retain
deterministic package hashing. This balances work approximately; test counts
are not duration measurements.

Each filtered suite runs sequentially after the whole-package batch, retaining
the existing PostgreSQL cleanup isolation. All race flags and failure
propagation remain intact. Target discovery includes tests, fuzz seeds, and
runnable examples, including external test packages. The base-controlled
planner and scheduler trust boundary remains intact.

## Validation

- Planner fixture verifies exact-once coverage for one, three, and six shards.
- Scheduler fixture verifies filters, ordering, failures, and malformed inventory refusals.
- Actual workflow planning produces six race shards and three fuzz/isolation shards;
  all repository packages except isolation are represented, with 153 app targets
  and 218 service targets split across four runners.
- Actionlint, ShellCheck, trusted CI scripts, cache policy, and required-job fixtures
  validate the surrounding workflow and gates.
- Cross-provider review skipped at the user's request.

## Hosted benchmark still required

PR validation executes the base branch's workflow, planner, and scheduler. Its
race timings therefore cannot demonstrate this change before merge. Do not
weaken that trust boundary to benchmark a PR.

After an authorized merge, inspect the first completed main CI run for six race
jobs. Compare each job's test-step duration, total duration, queue delay, and the
final required-gate completion against run 34725771747. Check a subsequent run
after shard caches have warmed. The provisional target is 10 to 12 minutes per
race shard; no speedup is claimed until those hosted measurements exist.
