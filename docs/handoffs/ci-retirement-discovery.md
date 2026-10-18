# Retirement discovery reliability

## Problem and scope

Required-check retirement discovery sequentially inventories non-success commits
from an immutable snapshot of the newest 1,000 main commits. Unrelated native
failures still require inventory reads even when they produce no retirement
target. The 100-head routing bound counts owned unfinished contexts, so it does
not bound the number of reads made before reaching them.

[Hosted run 39121365975](https://github.com/Hikyo-Org/Hikyo/actions/runs/39121365975)
failed after a discovery step lasting about 185 seconds including compilation.
The CLI adapter discarded the underlying command or context failure, leaving
only a generic REST read error.

This change repairs read-only discovery and its diagnostics. It changes no
required-check producer, mutation lease, merge policy, test inventory, workflow
trigger, schedule or deadline. Backend impact selection remains advisory.

## Reproduction

The unchanged executable built from main
`748ecff709c07fc877a11968a218c7f749d6d3ea` reproduced the failure through a
transport that refuses mutations. The scan used local authenticated GitHub
access and a constructed schedule event. It exited after 180.237 seconds, after
163 completed API calls including 158 check-inventory requests. Its final error
was the same generic read failure seen in the hosted log. The original
three-minute context explains this local reproduction; the original hosted
error did not preserve enough information to prove its cause independently.

The candidate completed the same immutable main snapshot in 100.899 seconds.
It traversed all 942 reachable commits in ten history pages, inventoried all
278 candidate heads, and received 329 complete REST pages without an API error.
The diagnostic reported full completion before the unchanged three-minute
deadline. The baseline stopped after 158 inventory requests, so these timings
compare an incomplete timeout with a complete scan, not equal completed work.

Replaying both unchanged and candidate executables offline against the same
captured complete API responses produced identical target JSON: 53 queue heads,
77 legacy-observation heads and three reporter targets. Both declared complete
ancestry across the same 942 commits. This isolates selection equivalence from
live network timing and changes in GitHub state.

The [numeric evidence](ci-retirement-discovery-evidence.json) retains executable
digests and measurement boundaries. Live timing preceded the final GraphQL
error-chain correction; that diagnostic-only branch has separate subprocess
regression coverage. It does not establish hosted token acceptance.

## Resulting behavior

History and authority validation remain serial. Once a history page is
authenticated, at most four readers inventory its candidate heads concurrently.
Each reader retains complete per-head pagination and ownership validation.
Results aggregate in history order, then retain the existing deterministic
sorting and routing bounds. Any error cancels siblings, joins every reader and
discards all targets, including successful work from earlier pages.

The read-only API wrapper rejects mutation calls. Discovery emits one bounded
JSON diagnostic on stderr after its readers join, separately from stdout target
JSON. The diagnostic records the authenticated snapshot, commits examined,
history pages, candidate attempts and completions, REST requests and received
pages, elapsed milliseconds, status and reader limit. Failed or unrelated
inventory work therefore remains visible. Diagnostic-write failure also fails
closed.

GitHub read failures retain typed context cancellation, deadline and command
causes. Logs include validated HTTP status and command exit status when known;
raw response bodies and command stderr remain excluded from the error string.
Existing bounded GraphQL diagnostics remain in force.

GitHub recommends serial requests to minimize secondary rate-limit risk. The
four-reader limit is a deliberate latency tradeoff for an existing finite,
read-only scan. It does not add requests or retries for successful scans. A
failed request stops the scan and cancels siblings; no runner sleeps or retries
through a throttling refusal. Token permissions and hosted API behavior still
require exact-head acceptance.

## Evidence to retain

Baseline and candidate API snapshots, request timings, executable identities and
failed/passing regression output are local review artifacts, not repository
paths or production attestations. Hosted execution uses a different token and
runner environment. Any timing comparison is one instrumented local pair,
not a hosted performance distribution.

Regressions cover the reader cap and overlap, cancellation and draining,
deterministic output, failing second pages, rejection of partial sibling and
earlier-page targets, unrelated native-check request costs, diagnostic stream
separation and failure, typed API causes, and absence of raw body/stderr leaks.

## Validation commands

```sh
go test ./scripts/ci/... -count=1
go test -race ./scripts/ci/pr-gate -count=1
go vet ./scripts/ci/pr-gate
git diff --check
```

## Next performance experiment

Recent full suites finish on browser-flow shards after long runner waits.
Scheduling and race-weight changes require a separate complete-inventory
experiment with explicit assumptions and actual hosted validation before
claiming a speedup. Raising the merge-build limit or enabling backend impact
selection is outside this repair.

An offline replay of the three retained suite traces rejected reducing race
parallelism or postponing analysis fanout: both increased completion time under
the explicit modeled 20-slot assumption. A candidate block rebalance preserved
all 186 race invocations and shortened the modeled race tail, but also delayed
browser starts and increased modeled whole-suite completion. These negative
results do not justify a production scheduling or weight change. Observed
parallelism is not proof of account capacity, and replay results require paired
hosted validation before adoption.
