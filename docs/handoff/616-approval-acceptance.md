# Handoff: #616 approval acceptance and scheduler write fencing

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/616>.
Base: `e114b56e2423a0996fa266fafbb59b561712d35d`.
Implementation: Codex. Parent 1.0 work owns combined review, signed commit, PR,
exact-head CI, and merge. No deployment claim is made by this handoff.

## Result

The approval expiry scheduler previously fenced claim and renewal, but only
cancelled jobs after a heartbeat noticed leadership loss. Expiry itself did not
check a fence. A paused worker could resume after another node acquired the
lease and mutate requests before cancellation reached it.

The scheduler now carries its lease name, owner, and fence in its term context.
Every attempt at the common write boundary validates and locks that live lease
inside the same database transaction as its work. The lock lasts through commit;
takeover cannot slip between a lease check and tenant mutation. A stale snapshot
retries against the current fence. PostgreSQL checks `clock_timestamp()` so a
transaction's old start time cannot revive an expired lease. SQLite uses its
process clock and existing `BEGIN IMMEDIATE` admission.

`store.ErrSingletonLeaseLost` refuses expired and superseded terms, including a
restart that reuses the node owner name. Ordinary user requests and single-node
schedulers have no lease context and retain their existing transaction contract.
No migration, tenant permission, or public API changes were needed.

This guard applies to scheduler work using `tx.Write`, `tx.WriteResult`, or
`tx.WriteSerialized`; it does not claim to fence arbitrary external effects or
proof-free coordination methods. Approval expiry and its audit event already
share this write boundary and now share its lease guard.

## Acceptance evidence

`internal/isolation/approval_acceptance_test.go` opens independent connection
pools and independent keyrings on the same datastore. Both SQLite and PostgreSQL
run every test. SQLite parity is writer-concurrency evidence, not support for
multi-node SQLite deployments.

- `TestApprovalVoteRetryAcrossNodes`: concurrent duplicate vote delivery remains
  one vote, one audit, and below a two-person quorum. Two distinct voters race a
  fresh request to quorum; response-loss retry on another replica stays
  idempotent. Racing merges produce one commit, one conflict, and one merge audit.
- `TestApprovalTargetMovementInvalidation`: a different approved request moves
  the target environment. Two attempts to merge the old approved request fail
  closed, persist `env_advanced`, emit one `approval.invalidated`, and leave
  value/draft/revision row counts unchanged.
- `TestApprovalExpirySchedulerTakeover`: real scheduler terms on separate nodes
  advance the stored fence. The old context deliberately retains its lease while
  suppressing cancellation, proving stale work is refused independently of
  heartbeat timing. The successor expires the first node's request exactly once,
  including concurrent stale delivery and a repeated sweep.
- `TestApprovalFenceSerializesTakeover`: a takeover is blocked until an admitted
  transaction completes; the previous fence then fails closed.
- `TestApprovalExpiryRejectsExpiredAndReusedOwner`: an expired lease fails even
  before a successor exists, and a new generation under the same node name does
  not revive the old term.

## Owner dispositions under delegated judgment

The user explicitly authorized recommended choices without questions in the
1.0 release task. The complete question/options/choice record is in
[`docs/reports/1.0/approval-acceptance.html`](../reports/1.0/approval-acceptance.html).

1. **Environment policy scope accepted.** Project defaults cover all project
   environments. Folders remain organizational and have no authorization scope,
   as already declared in the MVP boundary amendment.
2. **Requester-only merge and bypass accepted.** Ordinary publish materializes
   only the caller's drafts. Being a named bypasser does not confer ownership
   of another user's staged change. Cross-owner publish needs its own ownership
   contract before expanding this surface.
3. **Lazy invalidation accepted.** Policy drift commits invalidation and audit
   before refusing the next vote or merge. A queue entry may display its prior
   status until an action checks it, but cannot authorize a stale publish.
4. **`read@env` request inspection accepted.** The response exposes request and
   vote metadata, not secret value fields. Voting, ceremony binding, merge, and
   bypass retain their independent publish, eligibility, and ceremony checks.

The immutable #151 handoff is not edited. Its migration number is corrected
here: approvals shipped in `00041_change_approvals.sql`, not 00040. Release-wide
ledger disposition belongs to the parent 1.0 inventory.

## Local validation

All commands used `GOMAXPROCS=2` and `-p 1`. The PostgreSQL base was a newly
created scratch database `hikyo_616`; the harness used its own
`hikyo_616_isolation` database. Existing application databases were not reset.

```sh
go test -p 1 ./internal/isolation \
  -run '^Test(Approval|Approvals|ConcurrentApproval|ProtectedApproval)' -count=1
go test -p 1 ./internal/store/tx ./internal/app \
  -run 'Test(Retry|Scheduler)' -count=1
go test -p 1 ./internal/isolation -run '^TestInvariant(07|08|09)' -count=1
go test -race -p 1 ./internal/isolation \
  -run '^TestApproval(VoteRetryAcrossNodes|TargetMovementInvalidation|ExpirySchedulerTakeover|FenceSerializesTakeover|ExpiryRejectsExpiredAndReusedOwner)$' -count=3
go vet ./internal/store/tx ./internal/app ./internal/isolation
```

Results: approval suite passed (30 top-level and engine subtests), scheduler and
retry tests passed (15 tests), proof/predicate/driver/result confinement passed
(5 tests). The five new acceptance tests passed three repetitions under the race
detector on both engines (45 top-level and engine subtests). Scoped vet and
`git diff --check` passed. Final release verification belongs to the parent PR.
