# Issue #330 — analysis shard matrix ownership

Issue: https://github.com/Hikyo-Org/Hikyo/issues/330 (parent #326; audit
finding `F-S35-1`).

**State: implemented.** The trusted analysis planner now owns the shard count
used by both its plans and the race/fuzz job matrices.

## Contract

- `analysis_shards` keeps the sole workflow shard-count literal and passes it
  to both planner modes.
- The plan's JSON array keys become the `shards` job output. Both downstream
  matrices consume that output through
  `fromJSON(needs.analysis_shards.outputs.shards)`.
- `ci_job_registry_test.go` parses matrix nodes and refuses either shard job
  unless it uses the shared output expression.
- Aggregate job IDs, cache keys, package assignment, and fuzz-target assignment
  are unchanged.
- Contract or migration decisions: none. Generated outputs: none.

## Validation

- `go test -count=1 ./scripts/ci`: 7 passed.
- `./scripts/ci/analysis-shards_test.sh`: complete/disjoint plans passed.
- `./scripts/ci/check-required-jobs_test.sh`: success/skip and refusal fixtures
  passed.
- `go test -count=1 ./...`: 3,454 passed in 61 packages.
- Standards review: CLEAN. Issue #330 spec review: CLEAN.
