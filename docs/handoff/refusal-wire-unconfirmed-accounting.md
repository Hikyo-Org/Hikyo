# Refusal wire tests and unconfirmed accounting

## Failure

The post-merge `ci-dev` run 42376115723 on dev `36a1c989e` failed isolation
shard (1). `TestRequestRefusalConnectionAndWorkspaceWire/postgres` reported
`auth_refusal_siblings_test.go:204: request missing from supported instance
reader` after 1.36 seconds. The package ended at 859 seconds, inside its budget,
so this was not a timeout.

## Diagnosis

- **History.** The test was added in `42a3dea49` on 2026-10-28. No isolation
  shard failed on any dev `ci-dev` run before `36a1c989e`. That covers about 60
  runs from 2026-11-03. Shard (1) passed on the #1167 merge `4c1e13e53` in 824
  seconds.
- **#1167.** It changes none of the code on this path. That includes the
  refusal middleware (`internal/server/refusal.go`), the signals owner
  (`internal/service/refusal.go`), the window writer
  (`internal/store/authn/refusal.go`), the window and sample bounds
  (`internal/audit/refusal.go`), the reader and the test helpers. Its audit
  registry change adds personal value events only. No open PR touches these
  files.
- **Local runs on PostgreSQL 18.** Alone, 30 of 30 runs passed. Under `-race`
  with `GOMAXPROCS=1`, 40 of 40 passed, and every attempt reported zero
  accounting failures. The full package was also run; see the PR for its result.
- **Mechanism.** The refusal middleware discards `Refusals.Record` errors by
  design. Under the
  [refusal signals ADR](../adr/auth-refusal-signals.md), `Record` has a 100 ms
  total deadline. A missed deadline is a visible, non-durable accounting failure
  in health, not a refused response. The test served each request once. It then
  asserted that the paired request's sample was retained, without checking that
  its accounting had confirmed. If one write misses the budget on a loaded
  runner, the sample is never staged and the assertion fails with exactly this
  message. This is the same assumption issue 79 corrected for the emitter
  fixture (`b52f5d564`). The CI trace does not record health, so it cannot prove
  that this attempt was the one that missed its budget.

## Repair

`confirmedRefusal` in `auth_refusal_storage_test.go` serves a refused request,
compares the health failure counter before and after, and returns the first
attempt whose accounting confirmed. Each unconfirmed attempt is logged. Three
unconfirmed attempts fail the test with the health snapshot. A confirmed attempt
whose sample is missing still fails the retention assertion.

It is applied to every test that asserts a retained sample after a runtime
`Record`:

- connection and workspace wire;
- SCIM revoked;
- revoked replay;
- failed dedicated commit;
- supported instance consumers;
- MCP revocation.

`TestAuthenticationRefusalWireUnconfirmedAccounting` and the blocked datastore
test still own the unconfirmed outcome. Production code is unchanged: the
deadline, writers, waiters, retry policy and worker interval are all untouched.

`TestConfirmedRefusalServesAgainAfterUnconfirmedAccounting` holds the real
writer on both engines for the first attempt. It requires exactly two attempts,
one recorded failure, and a retained sample from the second attempt. With the
helper bypassed, it fails on both engines with one attempt and one recorded
failure. If its attempt check is also removed, it fails at `no pending window`,
because its only sample was never staged. In the CI test, the other two requests
staged their samples, so the window was pending and only the paired sample was
missing. That gives exactly the CI message. This shows the mechanism can
produce the failure. It does not show that it did on CI.

## Falsifier

If this test fails again in CI and no `refusal accounting attempt ... unconfirmed`
line is logged, the mechanism above was wrong. Reopen the diagnosis from that
trace.
