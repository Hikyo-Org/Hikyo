# Issue 981: catalogue presence indexing

## Scope and behavior

Baseline: `cb78b6be111029335e744aa3e4dcc956d5194ba1`.
The live #981 and #227 issue bodies and comments were checked before editing.
Issue #981 was open with no comments and no matching open PR.

`Keys.List` and `buildCurrentState` now share `indexPresenceByKey`, owned by
`internal/service/keys.go`. Each read buckets the already-authorized project
presence rows once, then decodes each catalogue key from its own bucket.
The total presence-row traversal is O(P), alongside O(K) key projection,
instead of inspecting all P presence rows for each of K keys. Declaration
parsing and output construction still contribute their existing costs.

The helper preserves row order within each key. Catalogue order still comes
from the original key slice, never map iteration. `presenceOf` remains the
owner of explicit/all/none decoding, including empty explicit sets and symbolic
all for future environments. Schema revision reads, authorization, database
queries, parse errors and refusal ordering are unchanged. Buckets belong to
one immutable read and are rebuilt on each invocation. There is no process
cache, new per-key query, migration, API change or generated-code change.
The group validation index and mutation paths retain their existing lifetime.

## Regression coverage

- Sparse and dense equivalence against the original whole-project scan at the
  supported 1,000-key/50-environment maximum, including interleaved input order.
- Every required/forbidden mode pair, unknown rows, empty inputs, missing keys
  and rebuilding after input mutation.
- Definitions state construction preserves key/environment/group ordering and
  revision, reads catalogue inputs once and still refuses unreadable declarations.
- Both-engine authorized catalogue list/get and definitions export equivalence,
  symbolic all/none and explicit presence, name ordering and unchanged revision.

## Measurement method

`BenchmarkCataloguePresence` compares the retained pre-change scan with the
indexed path in the same binary, on synthetic 100-key/10-environment and
1,000-key/50-environment catalogues. Sparse inputs populate two environments
for one key in ten; dense inputs populate every key/environment pair with
alternating required/forbidden rows. Both cases parse declarations and project
service keys. The indexed case rebuilds its buckets inside every timed read.
Fixture generation is excluded, and the loop does not retain the whole output
list. All declarations are string rules. Real callers also pay database and
output-collection costs. These are local CPU-path/allocation benchmarks,
not database, end-to-end service, production latency or hardware-floor evidence.
The 100-key case is illustrative, not a measured production distribution.

## Matched local measurements

Go 1.27.2, macOS arm64, Apple M2 Pro. The completed batch ran through the shared
one-job queue with `GOMAXPROCS=2`, `GOMEMLIMIT=512MiB`, `GOGC=50` and Go package
parallelism two. The heap setting is a soft per-process target. Other desktop
and persistent workloads were not controlled. Values below are medians of three
300 ms benchmark samples; the retained scan and indexed cases share one binary.

| Keys/envs and density | Scan ns/op | Indexed ns/op | Scan B/op | Indexed B/op | Scan allocs/op | Indexed allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 100/10 sparse | 85,293 | 86,949 | 79,578 | 81,761 | 920 | 943 |
| 100/10 dense | 358,426 | 238,604 | 127,281 | 299,357 | 1,700 | 2,209 |
| 1000/50 sparse | 1,446,874 | 926,100 | 795,699 | 820,523 | 9,201 | 9,410 |
| 1000/50 dense | 114,716,361 | 8,890,271 | 2,810,018 | 10,508,266 | 21,004 | 28,029 |

At maximum dense size the indexed projection was about 12.9 times faster in
this local comparison. It allocated an additional 7,698,248 bytes per projection
(2.81 MB to 10.51 MB). The additional buckets copy presence rows and allocate
backing slices. This transient memory cost is a tradeoff of the implementation,
not a process cache. The small sparse case showed no consistent timing gain
and incurred additional allocations. No across-the-board improvement is claimed.

Separate two-second maximum-dense CPU profiles recorded 2.22 sampled CPU seconds
for 20 scan projections and 3.79 seconds for 364 indexed projections. Amortized
sampled CPU was approximately 111.0 ms versus 10.41 ms per projection. These
whole-process profiles include fixture/harness and parallel runtime/GC work;
they are coarse CPU evidence, not isolated request CPU budgets. In the scan
profile, `presenceOf` accounted for 77.03% cumulative sampled CPU.

Portable benchmark reproduction from the repository root:

```sh
GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 go test -p 2 ./internal/service \
  -run '^$' -bench '^BenchmarkCataloguePresence$' -benchmem \
  -benchtime=300ms -count=3
```

## Completed validation

The queue's finite sequential batch completed successfully. Test executables,
JSON event logs, benchmark output and profiles are retained outside Git.

- Full `internal/service` test binary: PASS, 118.723 s, no skipped tests.
  Built with `go test -p 2 -c`, then run from the package directory with
  `-test.count=1 -test.parallel=2` through the matching Go `test2json` tool.
- `go test -p 2 -count=1 ./internal/definitions ./internal/schema`: PASS.
- Targeted `internal/isolation` binary: PASS, 79.712 s, no skipped tests.
  This includes every `TestInvariant*`, `TestIsolation`, `TestDefinitions`,
  the new `TestCataloguePresenceListAndDefinitions`, MCP definitions/list
  equivalence, per-key definition mutations, group publish closure and the
  selected-environment group presence refusal tests. All both-engine cases ran
  against SQLite and a disposable native PostgreSQL 18.6 server.
- The new authorized catalogue/list/export regression passed on SQLite
  (0.62 s) and PostgreSQL (2.31 s).
- `go vet -p 2` for service, isolation, definitions and schema: PASS.
- Changed-file `gofmt`, `git diff --check` and the repository import formatter
  check over 2,035 handwritten Go files: PASS.
- Canonical sqlc, OpenAPI, scanning and authz generation, plus operator
  deepcopy/CRD generation: PASS. Tracked and untracked generated-output checks
  were clean; no generated outputs changed.
- All eight benchmark variants and both CPU profile runs: PASS.

The full repository/race suite, hardware-floor acceptance, production latency
and hosted full-candidate CI were not run locally. No browser flow changed.
Graphify remains paused under the parent's resource-coordination instruction;
its interrupted update is not completed evidence. Independent parent review
and exact-head hosted CI remain separate delivery gates.

## Preserved execution history

- The initial focused build caught test-only `authz.Proof{}` literals; Proof is
  an interface. The stubs now pass nil. The corrected focused tests and final
  full service suite passed.
- An early focused compile was interrupted after severe shared-host paging.
  Its lower-memory rerun passed with one Go worker, a 512 MiB soft heap limit
  and GOGC=25. The final batch used the parent-prescribed settings above.
- The first full-package run was gracefully interrupted on the parent's
  resource-coordination instruction. It is incomplete, not PASS. The subsequent
  serialized full package run passed without weakening tests or deadlines.
- The initial graph refresh finished AST extraction, then was interrupted to
  reduce shared-host memory pressure. No sibling or PostgreSQL process was
  stopped. The old lightweight mutex waiter was stopped when the canonical
  policy migrated to the shared one-job queue; it never owned a validation slot.

## Delivery boundary

This work is authorized through signed+DCO commit, push and PR only. Merge,
auto-merge, queue admission, release and deployment remain held for the parent
and maintainer. Exact-head hosted CI and review are distinct from local checks.
