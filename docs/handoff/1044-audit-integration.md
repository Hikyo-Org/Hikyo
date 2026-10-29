# Audit export and instance trail integration (#1044, #1045)

## Carrier and scope

#1045 merged into #1044's original feature branch at
`e387e527f67dbcfb813e55f69e65ea232e314bb7`; that merge did not target main.
#1044 is now the main-targeted carrier of both implementation histories.
The signed integration commit `a0d0e73d686a6d9d8f0061173b3bb4e00b5b9ea3`
merges main `3ca472243967fb92843c9653f15fdbe42a1ef897` without manual conflicts.
Both source histories remain intact. #969 is excluded.

The combined change adds negotiated terminal export receipts, bounded shared
consumers, real CLI dispatch and human-authorized instance audit routes/UI.
The original [completion handoff](1036-audit-export-completion.md) and
[instance handoff](1035-instance-audit.md) retain the detailed protocol and
acceptance contracts. Their earlier stack-retarget instructions describe the
original delivery order; the combined carrier supersedes that order.

## Review repairs

Separate automated Standards and Spec reviews of the integrated source found:

- CLI export wrapped a redirect/trust refusal as availability exit 6. It now
  preserves the existing typed policy-refusal exit 4. An actual dispatcher
  regression first reproduced exit 6, then passed with no stdout or redirect
  credential forwarding. Ordinary incomplete exports still fail as unavailable.
- Browser export did not fence its asynchronous download to the initiating
  session. It now uses the existing session epoch, combines caller/owner
  cancellation and reconciles session ownership before consuming a response
  and before publishing the verified Blob. Deferred response and stream tests
  first returned a Blob after session replacement, then correctly rejected.

The completion verifier and all identity, snapshot, count, clean-EOF, staging,
size, per-page authorization and tenant-confinement rules remain in force.
These repairs change no rendered markup; #1045's existing attached desktop/mobile
UI evidence remains applicable to the unchanged rendered components.

## Validation and remaining gates

At the initial integrated revision, complete-diff feedback passed all 218 web
unit files / 2,064 tests and 67 client tests, typechecks, lint, Go compilation,
docs checks and import formatting. CLI/server/service/authz/API/consumer/boundary
owner suites and focused vet passed. Real HTTP and actual CLI audit scenarios,
revocation, retention, commit-order and completion cases passed on both SQLite
and disposable PostgreSQL in 74.877 seconds. The disposable database was removed.
The two review regressions were reproduced before their repairs and pass after.

Final exact-source feedback, browser integration results and hosted status are
recorded on #1044. Initial results do not stand in for final-source proof.
Reproduce with pinned contributor dependencies through the shared heavy-job queue:

```sh
set -eu
export VITEST_MAX_WORKERS=2
scripts/ci/fast-feedback.sh --plan
scripts/ci/fast-feedback.sh
go test -p 2 -parallel 2 ./internal/cli ./internal/server ./internal/service ./internal/authz ./api/... ./internal/auditexport ./internal/boundary
```

The original handoffs provide the disposable PostgreSQL and filtered native
browser commands. Never point those database tests at durable storage. Whole
merge-candidate CI remains mandatory and distinct from local validation.

Maintainer security review and the applicable adversarial security review are
still required by CONTRIBUTING.md. Automated reviews are compensating evidence,
not independent human approval. Cross-provider review is SKIPPED because the
approved subscription is unavailable; no paid fallback or new waiver is granted.
No release, deployment, live-provider or external collector acceptance is claimed.

## Section-tab story consumer repair

Full current-base merge validation found one additional stale consumer:
`InstanceAdmin.stories.tsx` still expected the pre-Audit section inventory.
The real Chromium story reproduced the exact extra `Audit` tab, with the other
nine stories passing. The strict ordered inventory now includes the intended
Audit section; the existing selected-Certificates and single-selection checks
remain intact. No product navigation, assertion strength or timeout was changed.
Final dark/light story results and refreshed hosted proof are recorded on #1044.

## Canonical automation grantability repair

Inspection of failed steps inside the cancelled queue run found a second real
failure: the machine-artifact contract could not satisfy the four project/env
audit operations. The audit-model ADR's declared amendment part 3 already admits
`audit-read` for project automation, but the domain allowlist omitted it.
The prior wire fixture inserted its machine grant directly and hid the public
grant writer's refusal. Replacing that seed with an authenticated grant POST
reproduced HTTP 409 before repair.

The canonical allowlist now admits only automation's declared audit capability.
The independent owning-project, scope-depth and human-assurance gates are
unchanged. The real grant writer and router tests cover project/environment
success, workload refusal, and org/instance/foreign-project refusal. The strict
machine-artifact contract is preserved, with no test exemption or gate removal.
Final both-engine and owner results are recorded on the PR; this restores an
existing accepted contract and introduces no new artifact class or broad
collector authority.

## CLI classification closure

The next full merge-group run passed 45 jobs but failed classification totality:
the implemented `audit` CLI verb had no wire-registry entry. The unchanged
invariant reproduced that failure locally. The canonical generator input now
classifies the mixed tenant/instance verb as instance, matching the existing
`access` convention. Regenerated output adds only that entry. Individual tenant
routes keep their stronger confinement classification, and no authorization
formula, grant, audit disposition or invariant is changed. Generator checks,
registry invariants and both-engine audit/authority results are recorded on the
PR; fresh current-base full merge-group proof remains required.

The following full run exposed a missing second half of that entry: its audit
operation linkage. The unchanged audit-completeness invariant reproduced the
failure. The generator input now names all eight query/export operations across
org, project, environment and instance scopes, inheriting their existing audit
dispositions. No exemption is added, and no event or authorization owner changes.
All isolation `TestInvariant*` checks run together for this repair, alongside
generator and complete-diff feedback checks, before refreshed hosted validation.

## Main refresh after DEK integration

After #1042 merged, main advanced to
`2b98bcf3565864d0858f2d40cc9ba4488a895790`. The sole textual conflict was
the sensitivity inventory's review note and InstanceAdmin hash. The combined
component was read in full: it retains Audit navigation and the corrected DEK
target/scope examples, with no change to mutation handlers, confirmations or
custody boundaries. Both review notes are retained and the combined source pin
is refreshed. Shared CLI dispatch, audit/DEK authority and both-engine acceptance,
the related stories and complete-diff feedback validate the integration before
fresh current-base queue proof. No source history or security gate is removed.

## SQLite caller-cancellation settlement repair

Full queue run `40786078752` passed 45 jobs but its store race shard observed
an expired SQLite commit return success. Caller cancellation closes its Done
channel before visiting child contexts, and database/sql commits against a
derived child. A controlled callback gate reproduced that propagation gap with
real SQLite: commit returned success and published a write after caller
cancellation. This is a production refusal gap, not a reason to loosen the test.

The transaction owner now retains the original context and checks its error
before deciding to commit. A cancelled caller takes the existing rollback path,
which preserves native errors and joins connection settlement before releasing
host exclusion. Cancellation after the commit decision retains the native
outcome. The original deadline, pool, native-settlement and host-lock assertions
are unchanged. The new controlled regression joins every held callback and
asserts refusal, absent writes, released connections and host exclusion. Both
focused Standards and Spec source reviews reported zero actionable findings;
they are automated evidence, not independent human approval. Local race and
consumer results and fresh full current-base validation are recorded on the PR.
