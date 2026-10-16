# CI cleanup, 11 October 2026

> Historical local artifact names below identify the original run only and are not included in this checkout. Recorded results were not rerun for this path cleanup.

PR #886 integrates the completed #887 fast-feedback/full-candidate split with
workflow consolidation and benchmark compilation reuse. The prepared final
source contains 19 workflow files: the original 26, eight retired standalone
entries, two consolidation menus, two feedback/candidate entry points, and
three retired legacy validation/polling workflows.

This is the source handoff, not a claim that gate activation, #886 merge, or a
universal five-minute hosted deadline is complete. Follow the
[CI guide](../development/ci.md) for contributor choices and protected activation.

## Final behavior

| Task | Workflow and merge authority |
| --- | --- |
| Each pushed PR head | Automatic `ci-feedback`; selected checks only, never merge authority. Four-minute runner limit includes setup, with queue time measured separately. |
| Final merge candidate | Maintainer requests `pr-candidate` with `ci:full`; every full-suite domain tests the exact head merged with current main. |
| Required merge check | `ci-required-next`, published by trusted `ci-pr-gate` after exact repository/workflow/run/attempt/source/parent proof. Live requirement changes only after protected hosted activation. |
| Complete main validation | Manual `ci-main`; nightly publication also depends on full validation. |
| Security/GitLab/reporting/race/channel maintenance | `reliability`; original schedules and isolated job contracts remain. |
| Native backup/restore/operator acceptance | `release-acceptance`; existing custody secrets remain confined to the backup step. |
| Operations acceptance and paid measurements | Separate `ops-floor` and existing performance workflows, preserving hardware and budget boundaries. |
| Nightly/stable/docs publication | Existing separate pipelines with their independent credentials and gates. |

The request label authorizes work only. After head or main changes, remove and
reapply it. Ordinary pushes receive feedback without restarting full validation.
Metadata edits cannot cancel or evict useful feedback. The trusted gate rejects
selective/head-only old runs and independently rechecks live source freshness.
Existing fork vouching and explicit exact-head workflow-change approval policies
remain; a label does not expand their authority.

## Conservation and load reduction

Five maintenance entry points become `reliability`, retaining daily security,
Monday 04:00 race, Monday 05:00 GitLab/reporting, and Monday 06:17 channel checks.
Two native manual acceptance entries become `release-acceptance`; PR-capable
operations remain separate. Routing rejects unknown events and suite selections.

The required core job exercises every benchmark once in crypto, dotenv,
scanning, server, and service after compiling/testing those packages. This removes
its separate smoke runner without deleting benchmark execution. The separate
three-iteration scanning guard remains. Smoke adds serial work to the full core
job; compilation reuse is not a measured hosted wall-clock improvement.

The frozen full-domain/execution baseline remains unchanged. Exact additive
#884 demo and #886 benchmark steps require independent assertions and refusal
mutations. Tests for removed legacy producers transfer their applicable source,
permissions, cache, fork, trigger, and failure guarantees to active workflows.
No test, race, fuzz, security, signature, native hardware, release, or paid-budget
gate may be dropped to obtain green.

The completion-driven gate uses short callbacks and queued Checks API results.
`ci-control`, `ci-fork`, and `ci-gate-reconcile` retire after hosted proof and an
atomic required-context swap. Same-name legacy/new writers must not coexist as
alternative success authority. Branch rules match check names and optional app
identity, not producer workflows; the generic GitHub Actions app cannot establish
stronger workflow-origin enforcement. Manual inspection of the exact trusted
publisher/full-candidate provenance remains mandatory. This platform limit
predates the split and is not claimed solved here. The bounded binary v2
identifiers and conservative truncated-v1 invalidation repair remain compatible
with deployed checks.

## Evidence and its limits

Historical full-suite samples collected on 11 October:

| Run | Context | Elapsed minutes | Execution job-minutes | Median job queue minutes |
| --- | --- | ---: | ---: | ---: |
| [38092256249](https://github.com/Hikyo-Org/Hikyo/actions/runs/38092256249) | PR | 148.58 | 373.60 | 42.73 |
| [38092248570](https://github.com/Hikyo-Org/Hikyo/actions/runs/38092248570) | PR | 148.00 | 402.57 | 39.67 |
| [38092248227](https://github.com/Hikyo-Org/Hikyo/actions/runs/38092248227) | PR | 143.23 | 398.28 | 36.99 |
| [38074450244](https://github.com/Hikyo-Org/Hikyo/actions/runs/38074450244) | Main | 32.13 | 356.13 | 0.37 |
| [38063236970](https://github.com/Hikyo-Org/Hikyo/actions/runs/38063236970) | Main | 150.83 | 417.52 | 56.60 |
| [38058367653](https://github.com/Hikyo-Org/Hikyo/actions/runs/38058367653) | Main | 36.72 | 374.25 | 2.29 |

The sampled full graphs have 46 successful jobs; quiet runs take about 32 minutes,
while contention stretches them toward 151 minutes. Execution runner-minutes are
parallel work, not elapsed duration or invoice amounts. These samples identify
queue contention; they do not prove a reserved feedback capacity.

PR #887's final bootstrap head `d48a18a2a4ce1571eb4b01638548d5758fd8e880`
completed [full run 38133568624](https://github.com/Hikyo-Org/Hikyo/actions/runs/38133568624)
and [required controller 38133566382](https://github.com/Hikyo-Org/Hikyo/actions/runs/38133566382).
All 16 mandatory domains passed on an integration tree matching current main `48087bde`
plus that head. It merged as `a5059c93f13d475b30c6eae6629c67f7a4b7aba3`.
This legacy bootstrap result does not prove the new hosted candidate publisher.

The final #887 feedback completed in 95 seconds: 19 seconds to runner start and
75 seconds execution, with API timestamps accounting for the remaining second.
The first observed feedback took 5 minutes 15 seconds, dominated by 4 minutes 02 seconds
of queueing. A four-minute execution deadline does not guarantee five-minute
commit-to-feedback under a saturated shared runner pool. Measure new accepted
heads after activation, distinguishing queue, setup, execution, and provider waits.

PR #886's prior `ceef331b8fe008565e7c6af7675faac9a349bc14` full run
[38128732411](https://github.com/Hikyo-Org/Hikyo/actions/runs/38128732411)
was cancelled during authorized resource cleanup. It is missing proof, not a
passing suite or a new application failure. Qodo resolved its contributor-doc
finding; CodeRabbit's then-current coverage was older `2491dee` with rate-limited
ceef review. Optional paid benchmarks skipped budget admission. Provider skips
are unavailable evidence, never clean reviews or performance success.

Original #886 local validation included CI/gate tests, vet, workflow lint,
shell policy/cache/source fixtures, two real five-package benchmark runs, and
routing/security comparison. After integration, rerun the relevant complete CI
script tests, workflow lint, shell fixtures, docs checks and independent review.
The migrated OSS-policy fixtures passed against the existing built site, including
seven canonical gate/protection refusal mutations and a complete positive
synthetic baseline. ShellCheck on both OSS scripts, read-only docs content
preparation, and scoped whitespace checks also passed. These are local checks;
they do not prove live protection activation.

Record final exact-head hosted evidence separately; do not carry old CLEAN or
local validation claims forward to the changed patch.

## Operator completion checklist

1. Validate the integrated signed/DCO #886 range and complete fresh full-candidate
   CI on the exact latest-main merge. Independently inspect all mandatory jobs,
   trusted-source identity, run attempt, completion marker and both parents.
2. Prove the default-branch shadow producer publishes correct pending/terminal
   states and invalidates stale successes. Inspect policy refusal and shared-head
   check ownership; native legacy checks cannot become success evidence.
3. Atomically require `ci-required-next` while preserving every other original
   protection. Verify semantic readback and required-check recognition. The live
   ruleset may still require legacy `ci-required` until this step; do not silently
   treat local configuration as live activation.
4. Merge the prepared #886 source retirement only after that swap, with the
   complete final exact-head/latest-main suite and addressed review findings.
   The workflow deletions are already in the tested final head; live retirement
   occurs at merge, without a separate retirement edit that would change the head.
   Revalidate after any head/base change. No tests are waived.
5. Measure per-commit feedback including queue under representative load. If the
   shared hosted pool cannot meet the target, report capacity limits honestly;
   removing polling does not reserve runner slots. Nightly, main/manual,
   release and publication acceptance remain independent.

Deleting workflow YAML does not erase historical Actions entries or provider-
generated security workflows. This patch does not delete run history, publish
releases, deploy applications, or change paid benchmark admission. Historical
research raw data remains under `hikyo-ci-audit`; newer scoped audit and
readiness evidence is under `hikyo-ci-five-minute`. GitHub links above are
the durable sources. Graph output is navigational and runtime claims require
source and executable evidence.
