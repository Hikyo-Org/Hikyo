# CI cache, invariants and queue report handoff

[PR #1005](https://github.com/Hikyo-Org/Hikyo/pull/1005) adds a shared race
compiler cache, advisory PR invariant checks and readable queue reports. The
[CI operator guide](../development/ci.md) records the workflows and trust model.

## Review repair

The [queue title finding](https://github.com/Hikyo-Org/Hikyo/pull/1005#discussion_r4313132831)
was reproduced against head `c8835db0c7b310949961435b11943603b3a78b0c`.
The run and jobs are read in separate API calls. A successful completed run
paired with unfinished jobs could claim that every job passed. The renderer
now uses a progress title when jobs remain running or queued.

Four added renderer cases failed before the repair and pass afterward: a
single running job, a single queued job, mixed finished/unfinished jobs, and
skipped jobs mixed with unfinished jobs. Existing terminal-success, failure
and cancellation expectations remain covered.

Local checks passed: `go test -p 2 ./scripts/ci/queue-report -count=1`,
`go test -p 2 -race ./scripts/ci/queue-report -count=1`,
`go vet -p 2 ./scripts/ci/queue-report`, and `git diff --check`.
These checks do not establish hosted CI, fresh review or merge-group success.
Read the PR's current head, reviews, threads and protected queue evidence before
merging; queue admission alone is not merge proof.

## Queue invariant repair

[Merge-group run 39250118338](https://github.com/Hikyo-Org/Hikyo/actions/runs/39250118338)
passed 45 jobs but failed isolation shard 0 on `TestInvariant12CacheDiscipline`.
The other two failures were aggregate jobs blocked by that invariant failure.
The focused invariant reproduced the same nine unregistered declaration names
locally. They model workflow YAML rather than runtime stores.

The declarations and their references now use reuse terminology, matching
existing CI fixtures. Reversing the nine identifier renames reproduces the
original formatted code exactly, apart from the explanatory comment. Workflow
keys, writers, assertions, coverage normalization and the invariant itself are
unchanged.

The focused invariant, complete CI tooling suite and vet passed after the rename:

```sh
go test -p 2 ./internal/isolation -run '^TestInvariant12CacheDiscipline$' -count=1
go test -p 2 ./scripts/ci/... -count=1
go vet -p 2 ./scripts/ci/...
```

## After merge

Seed the trusted-main shared race cache once after the new workflow lands:

```sh
gh workflow run ci-race-cache.yml --repo Hikyo-Org/Hikyo --ref main
```

Verify that run succeeds and publishes its cache before claiming hosted cache
reuse. The scheduled workflow can start hours late. Existing shard cache
entries age out through ordinary eviction; cache deletion is unnecessary.
