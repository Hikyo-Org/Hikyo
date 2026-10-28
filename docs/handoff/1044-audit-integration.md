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
