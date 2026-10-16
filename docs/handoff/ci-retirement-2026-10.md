# CI retirement and main-status repair

## Contract

Retire superseded unfinished checks and obsolete validation runs without
weakening merge proof or adding another full validation graph after merge.
The required `ci-queue-required` context, mandatory merge queue, full affected
domain coverage, signatures, DCO, reviews, benchmark thresholds and original
publisher deadlines remain in force.

## Reproduced failures

- Screenshot commits had ten unfinished synthetic records across seven SHAs,
  while all 214 sampled attached Actions runs were completed. A queued check
  record is not an allocated runner.
- Closed PR admission returned without marking a verified terminal or retired
  disposition. Generic blocking recovery then created another queued record.
  PRs #916 and #919 had successful full merge-group proof before merge; their
  PR-head placeholders were created afterward.
- On `6342ae361232fbb6d1b0dcef6f106a438a781718`, the group passed at 18:56:02 UTC
  on 16 October and merged at 18:56:52. A late callback failed with an empty
  DCO range after main moved to that exact group SHA. Another queue worker
  rejected its own running job because GitHub's Jobs API still said queued
  while retaining the correct job ID and start time.
- Published nightly `v0.0.1-nightly.20261016.79.gc20ab43e` failed binary scan
  receipt validation. Its authenticated stripped ELF reproduced seven
  `GO-2026-5932` wildcard findings. A minimal Argon2-only program reproduced
  them too. The pinned scanner's stripped-binary fallback reports vulnerable
  symbols across the module, including unused packages.
- Hosted Matrix performance run `38871999038` measured desktop save at
  212.6 ms against the existing 200 ms median limit. Controlled local profiles
  found repeated rebuilding of independent query-family indexes and transient
  objects for all 20,000 cells.

## Implementation owners

- `scripts/ci/pr-gate/check_retirement*.go`: exact-context ownership checks,
  complete live-role verification, immutable terminal verdicts, cancellation
  readback, and bounded discovery of effective pending checks in main history.
- `ci-check-retirement.yml`: trusted asynchronous housekeeping under the same
  head-specific queue lease or global legacy-observer lease. Queue writers
  honor the existing operator activation variable. Group-completion cleanup
  stays inside the existing callback rather than racing admission with another
  setup/compilation job under its lock.
- `scripts/ci/retire-runs` and `ci-run-retirement.yml`: authenticate nonterminal
  merge-group runs, preserve current main and complete live PR/group roles,
  require the old group ref to be absent or moved, reread the exact attempt,
  cancel, and verify terminal state. No deletion or reruns.
- Feedback/dependency concurrency supersedes useful earlier PR revisions.
  Candidate cancellation targets the predecessor head/base pair; metadata
  events never start another full suite. Reusable runs retain distinct run IDs.
- Queue bootstrap retries only the authenticated same-job queued-to-running
  transition, with at most five reads and the original 60-second deadline.
- `.goreleaser.yaml` retains symbols with `-w`, removing only `-s`. The actual
  configured linker flags are exercised by an ELF-symbol regression. Scanner
  and receipt policy are unchanged. Failure-only diagnostic receipts remain
  separate from PASS artifacts and exclude binaries, bundles and credentials.
- Matrix exposes existing stable query-family projections and computes
  problems from existing indexes. Denial sanitization, ordering, pending-value
  precedence, sensitive mutations and lifetime boundaries remain unchanged.
  The complete mutation-capable API source was independently reviewed before
  refreshing its sensitivity inventory pin.

## Evidence and performance limits

Lifecycle tests reproduce late closed/head/group callbacks, stale generations,
shared live roles, reopening, foreign records, incomplete pagination, uncertain
writes and readback disagreement. Run retirement tests cover attempt/ref/role
races, complete pagination, cancellation limits and API failures. Bootstrap
tests replay the actual queued Jobs API shape and prove that retries cannot
reset identity or extend the deadline.

Four recent successful hosted full groups took 28m14s, 30m51s, 28m22s and
28m27s. Their median is 28m24.5s; median recorded runner use is 416.15 minutes.
These are historical measurements, not a guarantee about later runner supply.
Housekeeping adds no full-suite dependency. Its asynchronous API and runner
cost must be distinguished from protected validation latency.

Live read-only discovery on main traversed all 893 reachable commits in nine
history pages, 30 context batches and one additional context page: 43 API calls
including bootstrap, 66.7 seconds excluding compilation. It identified 54
queue and 75 observer contexts across 93 distinct historical heads. This used
local authenticated API access with a constructed schedule event; hosted token
execution remains a separate acceptance step. The initial larger nested query
failed with HTTP 504 twice, motivating the measured smaller batches rather
than a timeout increase. Hourly fallback bounds idle housekeeping overhead;
normal lifecycle events still retire newly obsolete records directly.

A subsequent live dry-run checked all 129 selected contexts with the actual
retirement executable, fresh API reads, and a transport wrapper refusing
mutation commands. All 129 records were eligible; none had an active role or
an ownership/pagination error. The run took 126.4 seconds with four readers.
This is current read-only eligibility, not cleanup or a held workflow lease.

The separate empty `fuzz-report` run `34748394959`, queued since 13 September,
rejected normal and force cancellation. After explicit owner approval and fresh
zero-job/zero-artifact checks, its record was deleted: HTTP 204 followed by an
independent HTTP 404 lookup. Its pre-deletion API evidence was retained locally;
no other run was deleted.

The same locally built Linux application produced seven wildcard findings
with `-s -w` and none with `-w`; the module advisory remained visible in both.
The symbol-preserving compressed executable was 44,496,975 bytes versus
42,289,455 bytes, an increase of 2,207,520 bytes (5.22%). Local gzip times were
3.96 and 3.72 seconds respectively. This does not alter old released bytes or
establish a new hosted release-campaign PASS.

Alternating local baseline/revised Matrix runs with a fixed 4x CPU slowdown
measured save medians 262.9/223.4 ms and 262.9/196.6 ms, improvements of 15.0%
and 25.2%. Live-update results were mixed: 277.4/235.9 ms and 228.9/243.3 ms.
No live-update improvement or hosted acceptance is inferred. The original
sensor, sample count and limits remain unchanged; the slowdown is a local
stress experiment, not a hosted-runner model.

Final local validation passed all 1,776 web tests in 202 files, typecheck, lint,
and the unchanged four-case browser performance suite. Normal desktop
save/live medians were 69.1/65.0 ms. CI script, cancellation and release receipt
packages passed; workflow lint, Go vet, documentation verification and nightly
release contract fixtures also passed. These results do not replace the exact
PR-head feedback and full protected merge-group run.

## Recovery and verification

From an authenticated contributor checkout after the workflows reach main:

```sh
# Discover and retire the historical backlog through trusted main.
gh workflow run ci-check-retirement.yml --repo Hikyo-Org/Hikyo --ref main

# Reconcile a specific historical SHA using the workflow's own App token.
gh workflow run ci-check-retirement.yml --repo Hikyo-Org/Hikyo --ref main \
  -f head=FULL_COMMIT_SHA

# Run the bounded obsolete-group sweep.
gh workflow run ci-run-retirement.yml --repo Hikyo-Org/Hikyo --ref main
```

Replace `FULL_COMMIT_SHA` with the complete authenticated SHA. Retirement must
leave active/shared roles untouched and read back owned unfinished records as
completed/cancelled. Terminal success and failure remain unchanged; historical
cancelled results can render red rather than green. Discovery follows effective
pending rollup contexts, then each selected worker inventories all owned check
history for that SHA. Hidden old rows behind a terminal effective verdict are
not a pending badge or allocated job; exact-head backfill can inspect them.
The read-only discovery job authenticates its scheduled or input-free manual
default-branch event
and current main before scanning; its internal command is not a standalone
local CLI. Inspect its workflow output for discovered target SHAs.

A candidate whose admission base was superseded independently may finish if a
later webhook cannot identify that old base. Exact-parent proof still rejects
its completion. Cancellation is not a substitute for proof authentication.

Record the final PR head, complete group result, server merge SHA, current main
rollup and retirement readbacks separately. Never present local tests, queue
admission or an earlier full group as final merge proof.
