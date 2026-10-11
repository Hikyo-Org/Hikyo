# CI cleanup, 11 October 2026

The local cleanup reduces 26 workflow files to 20, removes the standalone
benchmark-smoke runner, and repairs the observation gate's long-branch identifier
failure. The existing required gate and repository protections remain active.
This is a locally validated implementation prepared for PR delivery, not a merged
change or a measured hosted speedup. The [CI guide](../development/ci.md) is the
contributor entry point.

## What to use now

| Task | Workflow |
| --- | --- |
| Normal contribution | Automatic `pr-validation`; follow the required `ci-required` check. |
| Full validation of main | Manual `ci-main`. Nightly also runs the full graph before publishing. |
| Security, GitLab, reporting, race, channel maintenance | `reliability`, with a suite selector. Original daily/weekly schedules remain. |
| Manual backup/restore or operator acceptance | `release-acceptance`, with a suite selector. |
| Operations acceptance | `ops-floor`, retained separately because it accepts PR execution. |
| Paid performance measurement | Existing `codspeed` and PR benchmark controls, with existing admission budgets. |

## Changes and reasons

1. Five scheduled/manual entry points become `reliability.yml`: `go-security`,
   `gitlab-e2e`, `reporting-e2e`, `race-isolation`, and `security-channel`.
   Original suite steps, cron cadence, native runners, matrices, timeouts, pinned
   tools, caches, and artifacts remain. Only the fallback-channel job can write
   issues. Explicit routing rejects unknown events, cron values, and menu choices.
2. `floor-acceptance` and `operator-floor` become `release-acceptance.yml`.
   Backup custody secrets remain confined to the existing backup step. The
   PR-capable `ops-floor` workflow remains separate from this secret-bearing flow.
3. Benchmark smoke runs as a step in `test_core`, immediately after the same
   packages have been compiled and tested. It still exercises every benchmark in
   crypto, dotenv, scanning, server, and service once. Its failure now fails a
   required job. The separate three-iteration scanning guard remains. Relevant
   changes retain coverage; docs-only changes lose the unrelated cold Go job.
4. The observed PR gate writes bounded versioned check identifiers. Its old
   base64-encoded JSON exceeded GitHub's persisted length on long base refs.
   The replacement retains full commit identities and hashes the complete base
   ref. Legacy-state recovery only permits invalidation and fresh reassessment;
   an incomplete old identifier is never success evidence.
5. Metadata-only validation callbacks skip the observer runner, matching the
   controller's existing no-op. New-run, in-progress, completed, lifecycle, and
   scheduled repair events remain. In-progress callbacks matter because
   [GitHub does not send requested events for reruns](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_run).
6. A single [CI guide](../development/ci.md), linked from CONTRIBUTING, explains
   all 20 remaining files, manual commands, trust boundaries, and gate migration.
   Active policy/docs references follow the consolidated workflows; historical
   reports retain their original evidence.

Consolidation improves navigation but adds a small routing job to each maintenance
or acceptance run. It does not remove those suites' test execution. Benchmark
reuse removes one separately allocated job on relevant PRs and the entire smoke
job on irrelevant PRs. These are structural reductions, not measured elapsed-time
savings. Smoke adds a short serial step to core tests; the independent local run
completed its service benchmarks in 23.1 seconds, excluding other packages and
compilation. No tests, security scans, signatures, or paid-run admission gates were
removed to make CI green.

## What the measurements show

Read-only GitHub evidence was collected on 11 October. Queue is job start minus
job creation; execution is job completion minus job start. Runner-minute sums
across parallel jobs are neither wall-clock duration nor invoice amounts.

| Run | Context | Elapsed minutes | Execution job-minutes | Median job queue minutes |
| --- | --- | ---: | ---: | ---: |
| [38092256249](https://github.com/Hikyo-Org/Hikyo/actions/runs/38092256249) | PR | 148.58 | 373.60 | 42.73 |
| [38092248570](https://github.com/Hikyo-Org/Hikyo/actions/runs/38092248570) | PR | 148.00 | 402.57 | 39.67 |
| [38092248227](https://github.com/Hikyo-Org/Hikyo/actions/runs/38092248227) | PR | 143.23 | 398.28 | 36.99 |
| [38074450244](https://github.com/Hikyo-Org/Hikyo/actions/runs/38074450244) | Main | 32.13 | 356.13 | 0.37 |
| [38063236970](https://github.com/Hikyo-Org/Hikyo/actions/runs/38063236970) | Main | 150.83 | 417.52 | 56.60 |
| [38058367653](https://github.com/Hikyo-Org/Hikyo/actions/runs/38058367653) | Main | 36.72 | 374.25 | 2.29 |

Each sampled run has 46 successful jobs. Similar full graphs finish in roughly
32 minutes with little queueing and take 151 minutes under contention. Some PR
jobs waited 61-92 minutes for a runner. This establishes queue contention as the
dominant source of the extreme durations; it does not identify the effective
organization quota or other repositories' load.

The GitHub inventory has 32 registered workflows versus 26 original source files:
four provider-generated entries and two historical deleted paths account for the
difference. Deleting YAML does not erase Actions history or guarantee that its old
sidebar entry disappears. A separately approved archive-and-delete operation
retires obsolete history. The eight workflows replaced here must remain active
until these replacements reach main. GitHub-managed security settings stay intact.

## Why the polling gate stays for now

The earlier replacement is still called `ci-required-next`. Hosted observation
accepted queued checks and successes, and REST `filter=latest` selected the newest
check. But long stacked Dependabot refs produced exactly 255-byte identifiers
containing incomplete JSON. [Scheduled reconciliation 38101138824](https://github.com/Hikyo-Org/Hikyo/actions/runs/38101138824)
failed with unexpected EOF for four PRs; 17 of the latest 60 sampled observer runs
were failed callbacks. The local identifier repair addresses that proven defect.

Same-head rerun invalidation and recognition as the required repository check
remain unproven on GitHub. Ruleset 20539346 still requires the old `ci-required`,
strict current-base status, signatures, and resolved review threads, with no bypass
actors. Removing the old polling job and its repair workflow before this proof
would be premature. No ruleset changes are part of this patch.

The largest remaining speed opportunity is replacing the runner-held wait with
the repaired completion-driven gate. The rollout must demonstrate long-ref
publication/readback, stale-success invalidation after rerun/base changes, and
required-check recognition before removing the legacy pair. Increasing timeouts,
dropping race coverage, or blindly reducing shard count would not fix the
measured source of delay. The sampled longest race shard already takes 22.5
minutes against a 30-minute limit.

## Validation and review

Passed validation:

- `go test ./scripts/ci/... -count=1`, `go vet ./scripts/ci/...`, and gate race tests.
- Repository-pinned workflow lint via `scripts/ci/run-go-tool.sh actionlint`,
  ShellCheck on changed shell files, import formatting for 1,884 handwritten Go
  files, and `git diff --check`.
- Required-job, trusted-script, cache-policy, artifact-reuse, changed-path, and
  OSS-policy shell fixtures. No required job or classifier registry was weakened.
- The unchanged real five-package benchmark command, run by the implementer and
  independently by the reviewer. Tests execute revision guards, command failure
  propagation, and rejection of a failed core job by the required aggregate.
- Every manual menu choice and preserved schedule, unknown-event/input refusals,
  original-job structural comparison, and cross-job permission/secret checks.
- Docs typecheck: 43 files with zero errors/warnings/hints. Full docs build:
  76 pages. The new contributor link is rewritten by the canonical site mapper.

GPT-6.1 Sol at high effort performed research and implementation, followed by
two fresh independent reviews. The workflow review caught an unquoted YAML `#`
in the metadata filter; the folded expression fixed it. Security review caught
legacy identifier recovery failing after base movement/retargeting and at some
truncation boundaries. The final recovery handles those without trusting old
outcomes. Independent adversarial tests cover malformed numbers/digests,
canonicality, stale bases, retargets, old PRs sharing a head, Unicode and escape
boundaries, and the two actual malformed checks saved from GitHub. Both reviews
report no remaining actionable findings. Hosted publication remains untested.

After PR #885 reached main, the overlapping controller changes were combined
with its deployed 156-character binary v2 identifier format. This preserves
compatibility with checks already published by main while retaining this PR's
stricter partial-JSON validation and publication checks. Regression cases from
both branches remain covered. The prior head's `ci-required` failure explicitly
reported a merge conflict, rather than a failed application test.

The merged controller source SHA-256 is
`13ed08b103d8b4e70aa192312afee22b5f8bc0ef502b73726f8e8ed59810f6d2`.
Raw timing JSON, test logs, and paired research/security-review artifacts are
retained locally under `/tmp/hikyo-ci-audit`. The linked GitHub runs are the
durable sources for the measurements. Delivery proceeds through a signed PR with
hosted checks; merge, release, deployment, and gate activation are not established
by the local checks above. The separately approved sidebar cleanup archives
available logs/artifacts before deleting runs belonging only to retired workflows.

The graph was rebuilt for navigation using AST extraction without an LLM/API
charge; its 17 parser warnings concern partially understood existing files, so
workflow/runtime conclusions use source and executable checks. Graph extraction
used zero semantic-model tokens; agent/review token usage was not measured.
