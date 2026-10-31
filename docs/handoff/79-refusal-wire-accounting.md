# Issue 79: refusal wire accounting observations

## Actual failure and reproduction

Selected source `595b158d18cea1724b3e7094dd5f181c82ec6bb9` passed full-main
41196325279 attempt 1 and its exact green-main selector. Separate full
isolation-race 41198930283 attempt 1 failed
`TestAuthenticationRefusalWireCompatibility/postgres` at the assertion
`serial flood unexpectedly failed accounting`. The trace observes a changed
failure counter, without the underlying `Record` error or scheduling cause.
No detected-data-race warning was observed. The campaign remains failed.

An unmodified focused race run passed once on both engines. A controlled run
held the real refusal writer during one wire request, then released it. The
old assertion failed on both engines with visible accounting failure and no
retained in-flight writer or waiter. This reproduces the incorrect fixture
assumption, not the original hosted scheduling condition.

The [accepted refusal contract](../adr/auth-refusal-signals.md) permits visible,
explicitly unconfirmed accounting when the sink, queue or 100 ms deadline
fails. Serial callers cannot turn that bounded accounting outcome into a
guaranteed successful receipt.

## Repair and preserved assertions

The test observes the real recorder's returned results without changing its
context, writer, deadline or errors. Every flood response must preserve the
same unauthorized body and produce exactly one accounting attempt. Each
acknowledged result must report healthy accounting without a failure increment;
each error must report unhealthy accounting with exactly one failure increment.
Every completed request must release its in-flight and waiting ownership. The
fixed failure metric must expose the actual cumulative counter.

The test requires acknowledged progress and fences PostgreSQL settlement using
the existing singleton writer before comparing staging plus sealed counts.
Aggregate committed counts must cover acknowledgements without exceeding the
attempt count; those bounds do not independently identify each request's durable
result or establish per-request uniqueness. A failed commit acknowledgement
remains unconfirmed rather than being asserted rolled back. Existing sample
limits, dedicated-event ownership, stage-summary positive controls, secret
redaction and reader/export assertions remain. Quiet recovery uses the existing
real sealer and three-second observer,
requiring the exact durable total and healthy accounting. After that worker
joins, an independent persisted snapshot must have zero window start, zero
counts, no samples and the unchanged exact sealed total. Storage and wire
callers share engine dispatch, fenced reads, decoding and validation while
retaining their own bounds.

A second root runs the same complete wire lifecycle with the first flood
writer held. It must observe an actual accounting deadline and still satisfy
all wire, health, durable-count, recovery, metric and redaction assertions. This
is a deliberate negative control, not a retry or a synthetic accounting result.
Production code, 100 ms budget, queue/writer capacities and retry policy do not
change.

The initial Standards review found that the recovery observer's health and
sealed-count checks did not independently inspect persisted staging. A local
mutation restored a nonzero window and count after the real worker joined:
the old checks passed on both engines, while the added persisted-reset check
rejected that same mutation on both engines with the sealed total unchanged.
The mutation is retained as evidence and removed from committed source. The
review also prompted one shared accounting snapshot owner, preserving each
caller's aggregate or stage-specific bounds and the PostgreSQL settlement fence.
The wire negative control and blocked-datastore deadline fixture also share one
held-writer owner, with exactly-once explicit release and test cleanup.

## Focused verification

With the pinned Go toolchain and an isolated synthetic PostgreSQL fixture in
`HIKYO_TEST_POSTGRES_DSN`:

```sh
GOMAXPROCS=2 go test -p 2 -parallel 2 -race -count=1 ./internal/isolation \
  -run '^(TestAuthenticationRefusalWireCompatibility|TestAuthenticationRefusalWireUnconfirmedAccounting|TestRequestRefusalSharedNodesCountEveryCommittedUpdate|TestRequestRefusalSnapshotWaitsForPostgresSettlement|TestRequestRefusalLostPostgresCommitReceiptRetainsEvidence)$'
```

The shared complete-diff feedback driver, pinned import checks and independent
Standards and Spec reviews accompany the source PR. Local results, current-head
CI, mandatory protected-group proof and actual integration remain distinct.
No rendered UI changes or screenshot claim is involved.

## Qualification disposition

Issue 79 stays open. Full-main on source595 remains passed, and its separate
race campaign remains failed. Prior e9da and source595 failures retain their
original evidence and unobserved causal limits. Native-all and ops-floor stay
on hold until the repair is actually integrated and a resulting source/test
candidate is deliberately mapped and selected. No prior receipt is relabeled,
and no intermediate broad campaign or provider, credential, public-resource,
tag, freeze, registry, Apple, notification, publication or deployment action is
authorized by this test repair.
