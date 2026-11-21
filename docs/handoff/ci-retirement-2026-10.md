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
- The legacy observer also created a new check when its bound run or attempt
  changed, leaving the previous bound generation pending behind a later
  terminal rollup. A source-bound regression reproduced this sequence.
- Three informational queue reports remained in progress after their source
  runs completed. Their completion callbacks failed with `ambiguous or
  unauthenticated existing queue report` when GitHub substituted its canonical
  details URL. The authenticated source outcomes were two successes and one
  failure; recovery must preserve those different outcomes.
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
  readback, and discovery of effective pending checks within the newest 1,000
  commits of an immutable main snapshot. Output explicitly distinguishes full
  ancestry from a limited recent window; older heads use exact-head backfill.
  A candidate over 1,000 latest records or 1,000 suites is inventoried through
  a GraphQL scan of its `github-actions` suites, then REST reads of only the
  suites with unfinished latest runs.
- `ci-check-retirement.yml`: trusted asynchronous housekeeping under the same
  head-specific queue lease or global legacy-observer lease. Queue writers
  honor the existing operator activation variable. Group-completion cleanup
  stays inside the existing callback rather than racing admission with another
  setup/compilation job under its lock. Up to ten unique observation heads run
  concurrently inside their shared writer lease after one compilation. Every
  child is awaited and failures are aggregated, so one failure cannot starve
  sibling heads or multiply the controller's three-minute deadline.
- Legacy observation publication closes a superseded unfinished bound
  generation before creating its successor. Original bindings and terminal
  verdicts remain immutable; a known newer attempt cannot be replaced by an
  older snapshot for the same target base ref.
- `scripts/ci/queue-report`: the existing discovery scan also routes unfinished
  informational reports to their original run/attempt lease. Recovery reads
  the exact completed source and merge parents, updates only an existing owned
  report, and verifies the original source outcome by readback. It adds no
  separate history scan or schedule.
- `scripts/ci/retire-runs` and `ci-run-retirement.yml`: authenticate nonterminal
  merge-group runs, preserve current main and complete live PR/group roles,
  require the old group ref to be absent or moved, reread the exact attempt,
  cancel, and verify terminal state. Authenticated same-attempt transitions
  between status queries are deduplicated; same-page duplicates or changed
  identities still fail closed. No deletion or reruns.
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
The revised reporter-aware scan repeated the same immutable main snapshot in
76.5 seconds with exactly the same 43 API calls. Its three reporter targets
matched the exhaustive audit exactly. Network timings vary; the extension
added no API requests or separate discovery job.

A subsequent live dry-run checked all 129 selected contexts with the actual
retirement executable, fresh API reads, and a transport wrapper refusing
mutation commands. All 129 records were eligible; none had an active role or
an ownership/pagination error. The run took 126.4 seconds with four readers.
This is current read-only eligibility, not cleanup or a held workflow lease.

A later exhaustive REST audit used `filter=all` across all 893 commits reachable
from immutable main `05b41d5e25467ada1af070cda96b9ddad7ddc387`. All 1,026 pages
were complete and retained 45,810 check records. Of these, 45,650 were terminal;
156 unfinished records belonged to the gate protocols, three to the queue
reporter, and one was inconsistent native Actions metadata. The audit found
27 gate records outside the original 129-record set; 25 were on ten additional
heads. Effective rollups alone do not establish that every older record is
terminal. Historical exact-head backfill must include those additional heads.

The separately inspected successful group `15c25690903993476b81093ec483f0c789393ae9`
is not in that main snapshot's ancestry. GitHub compare and local Git both
established divergence. Its unfinished owned check `117084330679` passed the
same retirement dry-run and is tracked as a separate obsolete candidate.

Native check/job `97185789943` on main ancestor
`c90ecf7092a31d6420dd2ee791a037b27808e3f3` reports `in_progress` while already
carrying conclusion `success` and completion time `2026-08-23T11:15:21Z`.
Its owning run `32636018661` is completed/successful. This is inconsistent
status metadata, not evidence of an active runner. It must retain its recorded
successful verdict and must not be processed as an owned gate check.
Two live read-only check/job/run/ancestry snapshots confirmed the inconsistency,
but did not establish write support. GitHub explicitly prohibits changing
native Actions check status/conclusions with a workflow token; the proposed
mutation helper and workflow were therefore removed before merge. The original
23 August run is also outside the documented 30-day rerun window, and a
check-run rerequest explicitly leaves the check itself unchanged. No supported
metadata-preserving repair for this native record has been demonstrated.
This remains a GitHub metadata issue, separate from owned synthetic retirement.
See the [native-check mutation restriction](https://github.blog/changelog/2025-02-12-notice-of-upcoming-deprecations-and-breaking-changes-for-github-actions/),
[rerun limits](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/re-run-workflows-and-jobs),
and [rerequest semantics](https://docs.github.com/en/rest/checks/runs#rerequest-a-check-run).

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

The exhaustive-audit follow-up passed all CI Go packages, including the
113.3-second CI contract suite, gate/reporter race tests, Go vet,
pinned workflow lint and documentation verification. Independent Sol 6.1 high
review covered the legacy generation guard, discovery routing, reporter replay,
workflow leases and the native metadata limitation. Historical write acceptance
and final protected merge proof are still separate requirements.

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
pending rollup contexts within its declared recent window, then each selected
worker inventories all owned check
history for that SHA. Hidden old rows behind a terminal effective verdict are
not a pending badge or allocated job; exact-head backfill can inspect them.
The read-only discovery job authenticates its scheduled or input-free manual
default-branch event and current main before scanning. Its `history_scope`
output reports the immutable snapshot, scanned count and whether older ancestry
remains outside the 1,000-commit window. It does not claim a limited window is a
complete historical audit. Its internal command is not a standalone
local CLI. Inspect its workflow output for discovered target SHAs.
Informational reporter recovery preserves each source attempt's actual outcome,
including failure. The native inconsistency above must remain separately
reported until GitHub supplies a supported repair; do not delete its original
evidence, publish replacement success, or claim the read-only diagnosis fixed it.

A candidate whose admission base was superseded independently may finish if a
later webhook cannot identify that old base. Exact-parent proof still rejects
its completion. Cancellation is not a substitute for proof authentication.

Record the final PR head, complete group result, server merge SHA, current main
rollup and retirement readbacks separately. Never present local tests, queue
admission or an earlier full group as final merge proof.

## Oversized commits (2026-11-15)

Hourly discovery failed on every scheduled run from 2026-11-05 to 2026-11-15:
nightly merge `f9b558f3d10fcd04c6ac8d95c166bc373b689d5a` has 1,132 latest
records, all completed, over the old 1,000-record refusal. No exact-head
backfill can lower a record count, so the commit would have kept main red until
it left the window. `807d433ea037d0aa8a8c861c88b4a995919a8de8` has 2,005 suites
(1,996 `github-actions`): REST showed 1,931 records against 3,912 summed over
its suites, and a queued native `report` run sat in suite 1,856 from newest,
invisible to REST. Suites holding queued synthetic runs reported
`completed/success`, so suite status cannot replace run reads.

Measured live from a workstation: the GraphQL suite scan costs one point and
about 2 s per 100 suites (12 s for `f9b558f`, 44 s for `807d433`). Full-window
runs took 133 to 164 s before the regression, 143 s with the first version
of this fix and 213 s (36 suite pages, exit 0) with the final one; one run of
the suite scan hit the old 3-minute deadline, so the discovery deadline is now
5 minutes inside an 8-minute job. The `retirement_discovery_scan` record reports
successful `suite_pages`.

Resolved by #1397: `listOwnedChecks`, the queue's `groupChecks` and the queue
reporter's `existing()` read per suite above 900 suites through the shared
`scripts/ci/checksuites` reader (a GraphQL suite scan selected by check name,
then one REST listing per holding suite), so discovery no longer refuses a
synthetic target on an oversized commit and targeted dispatch no longer misses
one. Every listing now costs one extra GraphQL count read, which the pinned
admission read budgets include.

Limits kept open:

- The deadline, not the 5,000-suite cap, bounds oversized commits: suite pages
  cost about 2 s each and history pages run sequentially, leaving room for
  about 40 more suite pages per window. A deadline spent on suite pages now says
  so. On 2026-11-15 the window held four commits over 1,000 suites (1,039,
  1,147, 1,715 and 2,005), and `807d433` gained 308 suites in one hour of queue
  retries. Reading candidates across history pages concurrently is not done.
- A commit can still cross 1,000 suites between discovery's REST inventory and
  the history count that chose it; the next hourly run then takes the suite
  path. The owners count suites immediately before each listing, so the only
  residual race is a commit counted at 900 or fewer suites that passes 1,000
  before the REST listing finishes.
- Queue admission has a 60-second budget with a 15-second reserve, so 45 seconds
  of reads, and it lists the owned checks at least three times on one head, each
  past 900 suites repeating the whole scan. The owners' scan reads one named
  count per suite and cost about 1.4 s per 100 suites live (27.6 s for 20 pages;
  discovery's scan reads two counts and costs about 2 s). Three scans of a head
  with 1,000 suites take about 42 s of the 45, leaving about 3 s for every other
  read, so admission may time out from 900 suites on. Past that the work clock
  ends the read, admission fails
  closed and the run is retried up to `admissionMaxAttempts` against the same
  scan time; the failure path lists twice more inside the 15-second reserve and
  can fail to publish its own verdict, leaving the non-final record for
  retirement to clean. A PR whose head stays that large remains stuck until a new
  head is pushed. All four oversized commits seen on 2026-11-15 (1,039 to 2,005
  suites) are past that point. Reusing one scan across the listings of an
  admission would cut the cost to one scan, but a suite created or joined by the
  controller's own write between listings would then be missed, so it is not done
  here. Admission before this change did not time out on these heads; it listed
  only the newest 1,000 suites and could report success while missing a check.
- Each scan page spends one GraphQL point (counts only), so a listing on a
  2,000-suite head spends about 20 points against the hourly limit shared with
  every other CI client.
- On a head past 900 suites the owners open only `github-actions` suites, so each
  also lists the context's name by commit (one more REST read per listing) to see
  other apps' records in REST's newest 1,000 suites. They refuse what a listing
  by commit refused: the queue verdict refuses any same-name record of another
  app, the retirement owner one that carries a controller external ID, and the
  reporter one that carries its report's external ID. A forged record in an older
  suite than REST's newest 1,000 is not seen on an oversized head. The main
  ruleset's required `ci-queue-required` context is not bound to an
  integration, unlike the dev ruleset's `ci-dev-required` (bound to 15368); the
  owner may want to bind it, which this change does not touch.
- An owner scan refuses when the head's `github-actions` suite total changes between pages, which
  fails closed. `807d433` gained about one suite every 12 s while its queue
  retried, and one scan took 47 s, so owner listings on a head that is still
  gaining suites can refuse and succeed on a later try; discovery has the same
  rule.
- `scripts/release/nightly-promote.sh` lists `ci-queue-required` by commit for
  its stuck check, which has the same cap. A miss there reads as "no check yet"
  and never blocks a promotion, so it is tracked separately.
- The release-admission policy (`scripts/ci/release-admission/spec/POLICY.md`)
  pins the `pr-gate/main.go` and `queue.go` blobs but not the new
  `scripts/ci/checksuites` reader they now call; the policy already states it is
  not a complete transitive attestation.

Not verified: the suite query from hosted runner IPs against any organisation IP
allow list; it filters by app ID and reads only counts, so it traverses no
foreign app object. The suite scan selected by check name was run read-only from a
workstation against `807d433` (2,005 suites, 1,996 `github-actions`). It found
`ci-queue-required` in 1 suite and `ci-required-next` in none, far under the
100-suite selection cap, which counts every suite holding any record of a name.
The GraphQL named count agreed with REST there: suite 113231756024 held run
126431783532 of `ci-queue-required` (GraphQL count 1), `check-suites/113231756024/check-runs?check_name=ci-queue-required&filter=all&per_page=100&page=1`
returned exactly that run, and the commit-wide REST listing returned none. The
per-suite REST call returned 404 once without `per_page` and `page`; the owners
always send both. Residual risk: GraphQL's named run count could lag the REST
listing for a run created moments earlier. The listing by commit that owners also
read shows such a run while it sits in REST's newest 1,000 suites, and a
controller record it shows that the suite inventory lacks is refused rather than
taken for absent, so a lagging count cannot make a publisher create a second
record there. A lag on a run in an older suite is not caught.
