# #934: Monotonic dynamic lease renewal ceilings

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/934>

A renewal could previously replace a 3,600-second maximum with 86,400 seconds.
The worker then extended the PostgreSQL credential for a full day. The ceiling
is a maximum for each renewal period, not an absolute lifetime since issuance.

## Behavior and ownership

Both engines now compare a positive requested ceiling with the stored ceiling
inside `DynamicEnqueueRenewingWithTTL`. A larger value affects no row and returns
the existing conflict response (HTTP 409), without a queued transition or audit
intent. Rejection preserves the API's existing state-conflict contract instead
of silently accepting a different value. Equal and smaller ceilings work.
Omitted renewal values retain the stored ceiling through the service's existing
zero sentinel. The public request schema continues to require a positive value
when a non-null ceiling is supplied.

The existing active-state predicate still admits only one competing renewal.
Worker claims retain the tightened ceiling through retries, ambiguous outcomes,
and reclaims. Current read/reveal authority and machine-reveal opt-in are still
checked before any provider extension. Stale claim tokens cannot settle or retry.

`internal/dynamic/ttl.go` owns `TTLDuration` and `MaxTTLSeconds`, the checked
positive seconds-to-duration conversion. Renewal input and persisted renewal
execution use it. Unrepresentable persisted values settle as failed renewals
without an external extension or a changed expiry. Issue #935 owns applying this
shared conversion to mint and other overflow ingress; this change does not
claim that broader overflow repair is complete.

SQL source remains in `internal/store/queries/{sqlite,postgres}`. Generated Go
files come from `go tool sqlc generate`. No schema migration or API shape change
is needed.

## Acceptance evidence

The original authorized workload/service/worker flow was reproduced on the
starting main commit: 3,600 became 86,400, followed by an active expiry more than
23 hours ahead. The new regression exercises the same real bearer, grants,
sealed provider custody, service, runtime and store, on both engines.

- `TestDynamicRenewalCeiling`: widening refusal with unchanged row/audit count,
  equal values, zero retention, repeated tightening and provider expiry.
- `TestDynamicRenewalConcurrentCeilings`: simultaneous competing updates,
  exactly one admitted transition, no subsequent restoration of the old ceiling.
- `TestDynamicRenewalRetryAndAuthorization`: unreachable-provider retry retains
  the ceiling; withdrawn reveal authority and sibling-environment access refuse.
- `TestDynamicRenewalCheckedTTL`: negative/overflow input refusal and safe worker
  handling of a previously persisted overflowing ceiling.
- `TestDynamicRenewalClaimFence`: a stale claim from the same worker identity
  cannot settle a larger expiry or release the replacement claim.
- `TestDynamicRenewalAmbiguityAndOptIn`: ambiguous retry retains the ceiling;
  withdrawing machine-reveal opt-in prevents provider extension.
- `TestDynamicRenewalRealPostgresValidity`: actual TLS PostgreSQL role creation,
  zero renewal and tightening through both Hikyo engines. `RoleStatus` reads the
  actual `VALID UNTIL`; widening leaves it unchanged. The provider formats expiry
  to seconds, while Hikyo retains its existing microsecond timestamp precision.
- `TestTTLDuration`: smallest/largest representable whole-second durations and
  zero, negative and overflow refusal boundaries.

## Hosted coverage

The isolation shard containing `TestDynamicRenewalRealPostgresValidity` starts
the existing TLS PostgreSQL fixture before executing its assigned tests. Its
required flag makes missing configuration fail loudly. An `always()` cleanup
removes only that fixture container. Other shards do not provision this target.
Executable selection/startup-refusal tests and exact additive workflow checks
preserve the frozen full-suite coverage inventory without changing its hashes.

## Repeating the checks

Provide an isolated `HIKYO_TEST_POSTGRES_DSN` with database-creation rights.
The existing `scripts/ci/start-dynamic-pg.sh` provisions the separate TLS provider
and writes its connection settings to `GITHUB_ENV`; local runs must export those
settings into the test process. Keep the fixture's required flag enabled so a
missing provider cannot silently skip. Clean up only the owned fixture container
and its generated public CA after the run.

```sh
go tool sqlc generate
go test ./internal/isolation -run 'TestDynamicRenewal|TestDynamicRuntimeGeneratedFenceAndSubsecondDeadline|TestAuditCore' -count=1
go test ./internal/service/... ./internal/store/... ./internal/dynamic/...
go vet ./internal/service/... ./internal/store/... ./internal/dynamic/... ./internal/isolation/...
```

The final serial renewal suite passed on both storage engines with the real
provider in 25.394 seconds. The complete audit lifecycle also passed on both
engines. Scoped vet, SQL guardrails, stable sqlc regeneration, workflow lint and
additive CI fixture/conservation checks passed. An initial broad parallel
package run hit PostgreSQL fixture startup timeouts and the package timeout;
its failure remains recorded and is not a local full-suite pass.

Full hosted validation and verified merge remain separate evidence from these
local checks. No release publication or production deployment belongs to #934.
