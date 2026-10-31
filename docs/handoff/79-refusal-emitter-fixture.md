# Issue 79: refusal emitter fixture settlement

## Failure and bounded diagnosis

Selected source `e9da355b5fd6a6e1c4cee4d6bbb84b26ef51304c` passed full-main
41182925041 attempt 1. Its separate full isolation-race run 41186257392
attempt 1 failed `TestAuditCore/postgres/every_registered_type_is_actually_emitted`
at `audit_e2e_test.go:436`, returning a context deadline after 0.10 seconds.
The original trace does not identify whether `Record` or `Seal` timed out.
It contains no detected-data-race warning. The failed profile remains failed.

An unmodified local race run of `TestAuditCore` passed once on both engines.
That observation does not establish the original timeout's causal condition.
A controlled regression holds the refusal writer beyond the approved runtime
accounting budget. The old emitter helper fails with a deadline on both engines.
This confirms its incorrect assumption that runtime accounting always provides
a successful fixture-construction receipt, despite the accepted explicit
unconfirmed-accounting outcome in the
[refusal signals amendment](../adr/auth-refusal-signals.md).

## Repair and evidence boundary

The registry-closure helper stages one fixture sample through the same
transactional `UpdateRequestRefusals` writer used by `Record`, using the existing
transaction owner. It then expires the window and starts and joins the real
`RefusalSignals.Run` worker through the existing three-second recovery observer.
The observer requires healthy, empty staging and the exact prior sealed count
plus one. The registered-emitter inventory assertion remains intact.

The new regression holds a real writer transaction during staging on SQLite and
PostgreSQL, then requires exactly one sealed sample. The existing blocked-sink
test independently requires runtime accounting failure to remain visible,
release ownership and avoid a later unobserved write. The production 100 ms
deadline, eight writers, 64 waiters, worker interval, transaction retry policy
and all runtime code are unchanged. Fixture construction is not proof that an
individual runtime accounting attempt will succeed within its budget.

The first repaired-path test run detected a duplicate rollback race in the new
PostgreSQL regression fixture. Its release worker now exclusively owns normal
rollback and is joined during cleanup. That local failure is retained separately
from the original hosted assertion failure.

## Focused verification

Use an isolated synthetic PostgreSQL database through
`HIKYO_TEST_POSTGRES_DSN`, with the pinned Go toolchain:

```sh
GOMAXPROCS=2 go test -p 2 -parallel 2 -race -count=1 ./internal/isolation \
  -run '^(TestAuditCore|TestAuditRefusalEmitterWaitsForStagingWriter|TestRequestRefusalBlockedDatastoreHonorsDeadline)$'
```

Complete-diff feedback and independent Standards and Spec reviews accompany the
source PR. Hosted protected checks and actual merge proof remain separate from
these local observations. This test-only repair changes qualification inputs;
it does not relabel any prior source, test, build or artifact receipt.

## Qualification continuation

Issue 79 remains open. Native acceptance and ops-floor stay on hold until the
failed race profile and the repaired source are dispositioned. After actual
protected integration, the coordinator must deliberately bind the resulting
source/tree and applicable fresh selector, test and artifact evidence. No
provider retry, credential, public resource, tag, freeze, Apple, registry,
publication or deployment authority follows from this repair.
