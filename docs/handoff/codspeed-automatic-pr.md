# CodSpeed automatic PR admission

CodSpeed walltime runs automatically for performance-relevant PR changes,
daily on `main`, and when a maintainer requests it. No CodSpeed job or full CI
rerun is added to the merge queue by this change.

## Selection and execution

- Automatic selection covers the `internal/` runtime tree including embedded
  assets and benchmark fixtures, Go/SQL files under `cmd/`, Go dependency
  manifests, API definitions, web
  runtime/build inputs, the TypeScript client consumed by the web app, `.nvmrc`,
  and the benchmark tooling and workflows, including Corepack setup.
- Markdown, docs-site changes, unrelated CI workflows, web stories and web
  unit-test-only changes do not request a paid measurement. Maintainers can
  request any current PR head using the existing **Run benchmark** checkbox.
- Draft PRs are not admitted automatically. Marking a draft ready creates a
  discovery run, as do opening, reopening and updating a PR.
- Automatic admission is limited to branches in this repository. Fork PRs
  remain eligible for explicit maintainer requests under the same budget.
- The trusted controller reacts only to successful first-attempt discovery
  completion. It validates the live run, current PR head, paginated changed
  files, and the benchmark workflows against `main` before requesting a rerun.
  Renames consider both filenames; incomplete file lists fail closed.
- Stale heads and already-claimed requests do not start another automatic
  measurement. Admission rechecks state after reading usage.
- A checked manual request owns the control comment and is preserved by
  automatic admission. Superseded discovery runs exit without changing status.
- Control-comment creation uses the same shared lease as automatic admission,
  preventing duplicate comments from concurrent preparation.
- Automatic, requested and daily measurements share `codspeed-walltime`
  serialization. The controller holds that lease until the paid rerun finishes.
- PR code still executes in its original read-only `pull_request` context with
  no secrets, OIDC or shared cache writes. Trusted controllers execute only
  `main` tooling. The reusable browser benchmark remains part of admitted runs.

## Budget and projection

Normal runs share **600 minutes over a rolling 32-day window**. This replaces
the former 528-minute daily and 72-minute manual sub-allowances. Each admission
reserves 16 minutes against the existing 15-minute Macro job timeout. Assigned
failed and cancelled jobs, retries, rounding and the existing extra minute of
accounting margin all count. Unassigned/skipped jobs do not count as spending.

Automatic PR admission additionally projects 32 days of daily coverage plus
the last seven days of PR/manual spending, then adds the next full reservation.
Daily cost uses the mean of charged daily jobs in the 32-day ledger. Without
daily calibration it uses the full 16-minute reservation. The observed period
is shortened for new history, with a one-day minimum. The projection is an
admission estimate, not a guarantee of future demand; every run also checks the
actual allowance. Manual requests can bypass the pacing estimate, not normal
budget admission.

The existing separately confirmed, explicit manual credit option remains:
up to 156 additional minutes. Automatic PR and scheduled runs never opt into
credit, and stop if actual spending plus the reservation exceeds 600 minutes.

The ledger covers Hikyo's two benchmark workflows, not other repositories or
provider invoices. Reset-date research and account-side budget changes are
deferred. No calendar-month reset is assumed.

## Validation and activation

Controller regression tests exercise discovery, admission, paid rerun and
completion reporting through a fake GitHub CLI, plus documentation-only,
draft, stale, duplicate and projected-budget rejection cases. Budget tests
cover cold starts, short observation periods, actual exhaustion, complete-job
reservation boundaries, failed attempts and old usage. Relevant race tests, Go vet, workflow
lint, cache-policy and trusted-CI checks passed locally.

Delivery is tracked in [PR #879](https://github.com/Hikyo-Org/Hikyo/pull/879).
It must land on `main` before the trusted controller can use it. Benchmark
workflow changes must match `main` before their own PR can use the paid lane.
Remote CI and provider review are in progress. Live automatic benchmark,
provider upload, merge and deployment have not yet been verified.
