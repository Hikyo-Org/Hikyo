# CodSpeed measurement repair and PR workload selection

Implemented on 2026-10-26 against main `ae2f1dda8bac294fa5bd3a20927115f5f3f00844`, following the accepted recommendation in the [benchmark relevance review](codspeed-benchmark-review.md). Delivery is authorized through a signed commit, pull request, protected merge and the repository's normal signed nightly release. The evidence below distinguishes completed local verification from hosted qualification and publication.

## Result

Publish, mixed import and bulk audit now upload the operation's actual duration. PR measurements select affected workloads, while daily/manual main runs retain the complete diagnostic matrix. Browser-only changes allocate no Macro job. A new validation command rejects large disagreements between printed Go timings and processed CodSpeed results before upload.

Required correctness checks, the complete one-iteration benchmark smoke, budget amounts, reservation margin, Macro hardware, timeouts and CodSpeed performance thresholds remain intact. The work-count invariants already in the test suite remain primary for acknowledgement decoding, presence equivalence, delivery work and audit lookups. Narrow helpers and historical comparison implementations remain available in scheduled/main diagnostics.

## Timer repair and clean measurement identities

The affected benchmarks previously resumed timing after their untimed readback. CodSpeed Go 1.3.0 records time since the last timer start for a Loop sample, so that final restart produced a microsecond tail instead of the real operation.

Each repaired benchmark now verifies the preceding write at the start of the next iteration, with the timer stopped, before restoring the fixture. It verifies the final write after Loop exits. Every operation still receives its result/error checks and exactly one content/durability readback. The operation is the final timed segment before the Loop boundary.

Each top-level benchmark now owns an `operation` child, creating fresh CodSpeed identities:

- `BenchmarkPublishSingleDraftLargeProject/operation`
- `BenchmarkImportMixedExisting/operation`
- `BenchmarkAuditBulkDisclosure300/operation`

This separates corrected millisecond measurements from invalid historical microsecond samples. Old identities can appear as skipped until an administrator archives them. No backend history was edited. The first corrected main measurement establishes the new comparable baseline; PRs before that baseline cannot establish improvement or regression for these new identities.

Owners: [publish/import](../../internal/service/ux_benchmark_test.go), [bulk audit](../../internal/service/audit_benchmark_test.go).

## PR selection

The trusted controller reads the complete changed-file list and both sides of renames. Truncation, a missing name or a file count beyond GitHub's supported limit fails closed. It seals a closed workload bitset into the existing head/run/attempt authorization marker. The unprivileged PR workflow uses only fixed commands emitted by the trusted verifier, and rechecks authorization before running Go code on Macro.

| Changed owner | Selected measurement |
| --- | --- |
| Web, generated TypeScript client, Node pin or browser workflow | Browser Matrix only |
| Service | Service read/write workflows, representative delivery polling and router admission |
| Scanner | Mixed-import/read/write workflows and the 64 KiB scanner |
| Server | Router admission |
| Dotenv | Service read/write and delivery workflows |
| Shared Go runtime, crypto, store, schema, dependencies or unknown internal owner | All representative Go groups |
| API contracts or measurement tooling/workflows | All representative Go groups and browser Matrix |
| Documentation, web unit tests and stories | No automatic measurement |

Service changes include router coverage because public middleware invokes service-owned caller-activity tracking. Unknown shared runtime owners select conservatively across Go consumers.

The representative Go groups contain 14 service read/write cases, four delivery cases, one scanner case and two router cases. Delivery uses SQLite, full projection, typical/render-limit datasets, and changed/unchanged cursors. A service PR selects 20 cases; the maximum representative Go selection is 21. Daily/manual main still discovers all 66 cases. Envelope, dotenv helpers, acknowledgement timing, historical presence comparisons and the advisory overload matrix stay outside routine PR measurement while remaining in main diagnostics.

A manually requested documentation-only head selects representative Go and browser work. Existing authorization markers without a workload field retain their original complete Go/browser scope. Browser-only requests bypass the Macro usage forecast because no Macro job is allocated. Their skipped, unassigned walltime job counts as zero usage; missing runner evidence for an assigned walltime job remains an error. Go requests retain the same allowance and projected-usage checks. Reporting requires all selected jobs to complete successfully on assigned runners.

Owners: [selection](../../scripts/ci/benchmark/selection.go), [controller](../../scripts/ci/benchmark/control.go), [PR workflow](../../.github/workflows/pr-benchmark.yml), [main workflow](../../.github/workflows/codspeed.yml).

## Measurement integrity guard

[codspeed-verify](../../scripts/ci/codspeed-verify/main.go) consumes stdout and the actual processed results from the same instrumented run. Both measurement workflows invoke it after successful benchmarks and before upload. Shell failure propagation preserves benchmark errors through `tee`, and verifier failure stops the upload.

The guard requires qualified Go integration version 1.3.0, walltime results, finite positive means and nonzero retained rounds. It matches package and nested benchmark identities, rejects missing or duplicate cases and malformed evidence, and refuses a ratio outside 0.5 to 2 between the two measurement paths. The generous ratio checks measurement boundaries; it is not a performance regression threshold. A 10% performance regression still belongs to CodSpeed's existing comparison policy.

The explicit integration pin and result-version check also catch reuse of a different preinstalled Go runner. Qualify a new integration version and update both declarations together. Native allocation reporting and instrumentation retention semantics have other limitations described in the [research note](codspeed-benchmark-guidance-research.md); this guard does not claim to fix them.

## Executed evidence

The pinned CodSpeed Go 1.3.0 macOS ARM64 release was downloaded, checked against its release checksum and executed locally without the outer CodSpeed profiler, authentication, upload or Macro hardware. Go was 1.27.3. These runs validate integration timing and selection, not deployment latency or hosted runner stability.

A known-work negative control performed a 5 ms operation with an untimed 20 ms readback. The old timer pattern printed 5.619 ms while processed samples averaged 8.012 microseconds. The corrected pattern printed 5.645 ms and processed samples averaged 5.641 ms. Reset and readback stayed excluded.

The actual final service sources, with fresh child identities, produced:

| Workload | Go stdout ms/op | CodSpeed mean ms/op | Retained rounds |
| --- | ---: | ---: | ---: |
| Publish | 27.772349 | 27.771911 | 8 |
| Mixed import | 54.693468 | 54.693094 | 4 |
| Bulk audit, 300 disclosures | 18.610774 | 18.610431 | 12 |

The timing validator passed all three. A separate real pinned-integration execution used the exact nested delivery selector, executed exactly the four intended cases and passed timing validation. This checks the consumer rather than relying only on the selector string. Local receipts bind the source file hashes and integration archive checksum. Those receipts are local evidence, not committed portable artifacts.

The independent local review found one missing service-to-router consumer, which was corrected and covered by selector tests; it reported no remaining actionable findings.

Completed local validation:

- Race-enabled controller, timing-validator and command-owner tests passed.
- The complete `scripts/ci` owner suite passed, including workflow failure propagation, CI coverage conservation, runner policy and required benchmark smoke checks.
- Focused acknowledgement, presence and delivery-work invariants passed on the local SQLite scope.
- All 66 cases in the five-package native one-iteration smoke passed.
- Scoped Go vet, the shared fast-feedback driver, ShellCheck, pinned Actionlint, docs content checks and whitespace checks passed.
- After the final service-to-router selection correction, the complete controller suite was rerun successfully on the final source. Final timing-validator tests and docs checks also passed.

During implementation, an additional queued repeat failed before execution because the queue launcher's Python 3.12 interpreter was missing. The earlier complete queued batch succeeded. This queue setup failure is preserved separately and is not recorded as a passed repeat; no heavy check was run by bypassing the queue. Queue availability was restored before delivery checks.

## Local verification

Run normal owner checks from the repository root:

```sh
go test -p 2 -parallel 2 -race ./scripts/ci/benchmark ./scripts/ci/codspeed-verify
go test -p 2 -parallel 2 ./scripts/ci
go test -p 2 -parallel 2 ./internal/service \
  -run '^(TestAckSet|TestPresence|TestBuildCurrentStatePresenceEquivalence|TestDeliveryPollWork)'
go test -p 2 -parallel 2 -run '^$' -bench . -benchtime=1x \
  ./internal/crypto/ ./internal/dotenv/ ./internal/scanning/ ./internal/server/ ./internal/service/
scripts/ci/fast-feedback.sh --working-tree
```

The benchmark command above is native one-iteration smoke. It does not reproduce instrumentation. PostgreSQL delivery tests remain explicitly skipped locally without the required disposable database DSN; this task did not provision one. No TypeScript application source changed.

For an explicit measurement-boundary diagnostic, install the checksum-verified `codspeed-go-runner` 1.3.0 executable on PATH and use Bash. Its pinned parser supports the `-benchtime` diagnostic argument used here; CI continues to use the documented `-bench` command surface only.

```bash
set -euo pipefail
profile_dir=$(mktemp -d)
export CODSPEED_PROFILE_FOLDER="$profile_dir"
GOFLAGS=-p=2 codspeed-go-runner test \
  -bench='^Benchmark(PublishSingleDraftLargeProject|ImportMixedExisting|AuditBulkDisclosure300)$' \
  -benchtime=200ms ./internal/service/ | tee "$profile_dir/benchmark.log"
go run ./scripts/ci/codspeed-verify \
  --log "$profile_dir/benchmark.log" --results "$profile_dir/results"
```

Expected result: three `/operation` cases at millisecond scale and `CodSpeed timing validation passed for 3 benchmark cases.` On a shared development host, run heavy commands through its installed queue. These local diagnostics do not upload or spend Macro minutes.

## Remaining hosted qualification

### Delivery review and queue-publisher follow-up

CodeRabbit identified that unauthorized-request tests accepted extra output such as `go=true` after `allowed=false`. The assertion now requires exact unauthorized output while allowing the additional authorized selection fields.

Trusted-main queue publishers for PR #1062 failed in [run 40347935477](https://github.com/Hikyo-Org/Hikyo/actions/runs/40347935477) and [run 40347937101](https://github.com/Hikyo-Org/Hikyo/actions/runs/40347937101). Setup used approximately 24 to 27 seconds, leaving about 18 to 20 seconds before the original 45-second work cutoff. Subsequent serial GitHub reconciliation exhausted that cutoff; the 15-second publication reserve produced the terminal failure. These were publisher failures, not failed benchmark or merge-group tests.

The follow-up overlaps the independent feedback-workflow definition and first exact-head run-list reads within each feedback phase, then joins and validates both before reading jobs. All request counts, job checks, authority/DCO checks, final source/role/owned-check barriers and the original 60-second deadline remain. Deterministic tests failed before the overlap repair and passed afterward, including cancellation of either failed read and complete proof using a budget with 26 seconds already spent on setup.

The trusted publisher build also omits DWARF debugger data while retaining the exact toolchain and isolated environment. A frozen-source local cold-cache comparison measured 10.642 versus 10.518 seconds and reduced binary size from 7.75 MB to 5.62 MB. This does not establish a meaningful startup improvement or hosted deadline recovery. The original probe's help comparison included temporary executable names; that probe failure is retained, and the corrected comparison normalizes only the executable path.

The full publisher/controller race suites, complete CI-owner suite, scoped vet, shared feedback, ShellCheck and pinned Actionlint passed for the follow-up. Independent local review found no actionable correctness or security issues. Publishers still executing the previous trusted main cannot use PR-side changes before merge; fresh hosted admission and full merge-group proof remain required.

Workflow byte-identity admission intentionally prevents this changed PR workflow from receiving paid PR admission before its workflow is on main. Local tests do not bypass that boundary. After authorized delivery, the first corrected main run and a newly admitted PR run must establish Linux/Macro assignment, result upload, selected-job reporting and the fresh baseline. Browser-only hosted selection also needs a real workflow run.

Live per-benchmark thresholds and old-identity archival were not changed. Calibrate tolerances from corrected same-hardware samples and workload relevance after hosted qualification. The independent local review does not establish cross-provider review, hosted CI or release acceptance.
