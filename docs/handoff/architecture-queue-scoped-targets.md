# Queue reconciliation and scoped CLI preparation

## Task and accepted scope

The owner requested implementation of both architecture candidates after
PR #1092. This change starts from its merged commit,
`34de836434071d6904842e82987fa8c65b6ba76f`, and preserves observable behavior.

- Deepen queue reconciliation so one owner carries observation, publication,
  blocking recovery, settlement and authenticated retirement for one held-head
  pass. Admission and full merge-group proof remain distinct.
- Deepen scoped CLI preparation for parameter, retention, SSH and adapter-target
  commands. Preparation owns the actual required dimensions, canonical
  authentication and scoped path; command owners retain parsing, requests,
  confirmation and rendering.

Both implementations use separate fresh GPT-6.1 Sol xhigh contexts. Independent
GPT-6 Astra xhigh Standards and Spec reviews are required for the candidate;
their exact-revision results belong in the pull request. These are automated
reviews, not independent human security approval.

## Preserved contracts

Queue reconciliation must retain fresh policy, source, role, member authority
and final identity verification; exact-head writer leases; complete owned-check
history authentication; immutable terminal records and new queued generations;
lost-write recovery; existing retry windows, hard stops and publication reserves;
and an effective blocker on uncertain proof. No authority is cached across
invocations. Healthy reconciliation must not grow check history. Neither a
successful PR admission nor a historical group success authorizes a new group.
The operative lifecycle contract remains in
[merge queue policy](merge-queue-policy.md) and
[transient proof reads](queue-proof-transient-reads.md).

CLI preparation must preserve current error precedence, scope and selector
semantics, authentication artifact selection and ambiguity refusal, origin
binding, trust-store fallback, target echo and path escaping. These callers
authenticate before requiring their scope dimensions. Existing DEK preparation,
machine-only delivery and developer-credential contracts stay with their current
owners. The coherent scope contract remains in
[the CLI ADR](../adr/api-cli-surface.md) and the hierarchy in
[the domain model](../spec/domain-model.md).

## Implementation

The queue reconciliation owner carries per-pass observations and settlement.
Proof callers supply freshly verified results without manipulating lifecycle
flags. Publication authenticates the final history and selects the effective
generation within this owner; uncertain receipts and authenticated retirement
use the same pass state. The non-required lifecycle diagnostic continues to
exercise the same immutable publication protocol.

Scoped CLI preparation uses the existing operation identity to select the
required depth. It delegates authentication to the existing implementation,
requires dimensions in their existing order, and returns the prepared request
parent. Adding an adapter target retains its Project request path while requiring
an Environment in its request body. No new authentication implementation or
generic delivery mode is introduced.

## Validation and review

The parent owns Go execution so the delegates do not run competing builds.
Validation over the implementation bytes produced these local results:

| Check | Result |
| --- | --- |
| Full queue tests, two workers | PASS, 40.654 seconds |
| Queue race tests, two workers | PASS, 67.576 seconds |
| Queue vet | PASS |
| Full CI fixtures, two workers | PASS, 402.798 seconds |
| Architecture boundaries, two workers | PASS, 7.704 seconds |
| Full CLI package, two workers | PASS, 12.633 seconds |
| Complete-change shared feedback plan and driver | PASS, 16 seconds |
| Lint compiler-cache preparation, all four contexts | PASS |
| Full local lint | TIMEDOUT in two ten-minute attempts |

Commands use `GOMAXPROCS=2`, `-p 2` and `-parallel 2` for Go tests. The CLI
package passed within a combined CLI/lint invocation whose aggregate exit was
nonzero because lint timed out. The first timeout occurred during
`TestArtifactDisposableDatabaseProtocolsRejectDrift`. A second lint-only run,
after the prescribed `scripts/ci/prepare-lint-cache` preparation, timed out during
`TestGrantLockRepo/ui`. Neither attempt reported an assertion failure before
the timeout. Its cause remains unconfirmed; this is not a local lint pass.
No timeout, worker limit, assertion or production policy was relaxed. Complete
hosted invariant and protected merge-group validation remain necessary.

Caller-level tests cover failure settlement, moved or pinned group identity,
uncertain writes, retirement role renewal, scope/authentication error order and
prepared requests. Shared feedback checked whitespace, Go formatting and
compilation of both changed owners against the complete change set.

Local fixtures establish their exercised contracts only. Hosted protected queue
proof and the actual merge are separate evidence. The motivating PR #1092
publisher failure followed three failed GitHub metadata reads after all 48
validation jobs passed; this ownership refactor does not claim to eliminate
provider read failures or relax refusal of uncertain authority.

## Rollback

Revert the refactor with its direct consumers and tests. No data migration,
persisted format, workflow policy or deployment step is introduced.
