# Hikyo CI: concurrent PR timeouts and completion-driven recovery

Prepared 10 October 2026. All times are UTC. This report covers the linked PR #877 failure, the concurrent PR workload, changes actually merged during the preceding 72 hours, and the local implementation. Markdown and its HTML sibling contain the complete report.

## Finding

The reported failure was a trusted gate timing out while healthy validation waited for runners. It was not a failing goose dependency test or a rejection because the PR was a fork. The gate held a hosted runner while polling for up to 90 minutes, competing with the validation jobs whose completion it needed. Several concurrent PRs amplified that contention. The observed queue delays establish a scheduling problem; they do not identify GitHub's effective organization quota or exclude platform-wide scheduling pressure.

PR #877's [original required job](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060919131/job/114238831062) failed in `Require PR-scoped validation` at 16:30:40. Its [validation run](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060921960) eventually passed **all 47 jobs** at 17:41:36, **175 minutes 32 seconds** after creation. The newly merged callback [requested recovery](https://github.com/Hikyo-Org/Hikyo/actions/runs/38072775225) at 17:44:22. The same run's [required job on attempt 2](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060919131/job/114273990994) passed from 17:46:37 to **17:46:49**, taking **12 seconds**. Passing after upstream completion confirms the original red gate was orchestration failure.

The workflow called `fork-ci` validates every PR, including branches in this repository. Only author vouching is specific to forks. Its name obscures the flow, but the name is not a classification bug and did not cause this timeout. Source: [landed PR validation workflow](https://github.com/Hikyo-Org/Hikyo/blob/c0f820712e16726fc67b53f86022256368ffe303/.github/workflows/ci-fork.yml), [trusted gate and 5,400-second default](https://github.com/Hikyo-Org/Hikyo/blob/c0f820712e16726fc67b53f86022256368ffe303/scripts/ci/check-fork-validation.sh).

## Measured contention

Six original gate attempts, on PRs #873, #874, #875, #876, #877 and #881, consumed **542.72 runner-minutes**, mostly waiting. Their active intervals overlap from 15:39:07 to 16:23:31. In the measured concurrency window, six of the observed 20 occupied job slots were polling gates, or **30%**. Removing that waiting work can free capacity without reducing coverage or increasing timeouts.

| PR | Trusted run | Original gate runner minutes |
| --- | --- | ---: |
| #873 | [38060889865](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060889865) | 90.63 |
| #874 | [38060895787](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060895787) | 90.58 |
| #875 | [38060904893](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060904893) | 90.20 |
| #876 | [38060912911](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060912911) | 90.70 |
| #877 | [38060919131](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060919131) | 90.25 |
| #881 | [38063555046](https://github.com/Hikyo-Org/Hikyo/actions/runs/38063555046) | 90.35 |

At PR #877's timeout, 22 validation jobs had completed, 20 were queued, three were running and two had not yet been created. That snapshot explains why a 90-minute wall-clock gate can fail even when the remaining tests are healthy.

| Validation PR | Jobs | Median created-to-start delay | Longest delay |
| --- | ---: | ---: | ---: |
| [#873](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060894920) | 47 | 63.07 min | 101.78 min |
| [#874](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060898138) | 47 | 33.13 min | 95.10 min |
| [#875](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060908091) | 47 | 32.10 min | 110.07 min |
| [#876](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060912875) | 47 | 49.25 min | 108.95 min |
| [#877](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060921960) | 47 | 59.75 min | 101.90 min |

For a concrete example, PR #877's [race shard 9](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060921960/job/114249522476) was created at 15:40:47, started at 17:22:41 and completed at 17:40:55: approximately 102 minutes waiting and 18 minutes assigned execution. PR #873's [desktop browser shard 2](https://github.com/Hikyo-Org/Hikyo/actions/runs/38060894920/job/114249075740) waited approximately 102 minutes and ran for seven minutes.

These calculations use the linked GitHub runs and job timestamps; raw API responses and derived metrics were retained as local investigation evidence. Runner time is `completed_at - started_at`; queue delay is `started_at - created_at`. Queue sums across parallel jobs are not run elapsed time. Dependency-ready timestamps, other repositories' workload and the effective hosted scheduling quota are not fully exposed by these inputs. Runner-minutes here describe occupied capacity, not an invoice calculation.

[#878](https://github.com/Hikyo-Org/Hikyo/pull/878) is closed. Its [validation run](https://github.com/Hikyo-Org/Hikyo/actions/runs/38039612569) was cancelled; cancellation is reported separately rather than counted as a test failure or a successful validation.

## What actually landed in the last 72 hours

Live main was `c0f820712e16726fc67b53f86022256368ffe303` when this history was checked. Dates below are actual GitHub merge timestamps; a squash commit's date can precede its merge. The window is 7-10 October 2026.

| Merged UTC | PR and resulting behavior | Merge commit |
| --- | --- | --- |
| Oct 8 16:50 | [#860](https://github.com/Hikyo-Org/Hikyo/pull/860): race shards 6 to 8; prepare cold cross-platform lint compiler exports before package-test timeout begins | `b186e280f` |
| Oct 8 18:47 | [#863](https://github.com/Hikyo-Org/Hikyo/pull/863): daily/requested benchmark budgets, cancellable PR smoke, lint execution separated from concurrent runtime suites | `e9be2c42e` |
| Oct 9 11:41 | [#865](https://github.com/Hikyo-Org/Hikyo/pull/865): compiler/networking security updates and compatible analysis-tool pins | `d9f799964` |
| Oct 9 17:16 | [#871](https://github.com/Hikyo-Org/Hikyo/pull/871): replace required merge queue with strict up-to-date `ci-required`; preserve signatures, PR protection, thread resolution and no bypass actors | `8d65494ff` |
| Oct 10 08:52 | [#869](https://github.com/Hikyo-Org/Hikyo/pull/869): race shards 8 to 12, refreshed weights and target timing artifacts, eight isolation race shards, categorized gate failures, bounded API reads, trusted recovery, Docker daemon mirror with fallback and PostgreSQL startup after checkout | `2a50a75dd` |
| Oct 10 15:20 | [#879](https://github.com/Hikyo-Org/Hikyo/pull/879): automatic eligible PR benchmark admission within actual/projected shared spending allowance | `c0f820712` |
| Oct 10 18:06 | [#881](https://github.com/Hikyo-Org/Hikyo/pull/881): Storybook coverage and theme validation within existing CI; merged during this investigation | `d3bf670c4` |

[#869's post-merge main run](https://github.com/Hikyo-Org/Hikyo/actions/runs/38039422634) passed. Its recovery callback now has direct runtime proof in the PR #877 sequence above. Recovery is useful but still reruns a polling gate. Increasing race fan-out helps fit individual job limits; it does not remove aggregate scheduling pressure. #879 limits benchmark spending, not ordinary CI admission across the organization.

The clarity patch from the earlier audit **never merged**. It was an uncommitted local proposal on `dunky13/ci-clarity-and-queue-audit`, with Markdown and HTML audit notes dated 9 October. Those historical checks do not validate today's combined changes. The audited main revision still used `fork-ci` and allocated a same-repository vouch runner.

[#881](https://github.com/Hikyo-Org/Hikyo/pull/881), head `9448331e1c4818b55b6f57dfe8e3a35d158a1044`, adds Storybook docs/theme checks within existing CI. It was open during the initial investigation and merged at 18:06:31 while this local repair was being reviewed. Its validation completed successfully at 18:04:26. That delivery was separate from this gate repair.

## Hosted isolation failure on the CI test declarations

On head `4bc6078fd`, [isolation shard 0](https://github.com/Hikyo-Org/Hikyo/actions/runs/38080722780/job/114306891321) failed `TestInvariant12CacheDiscipline`. Its repository-wide declaration sweep found ten new CI test/helper names containing `Cache`; these describe workflow YAML assertions, not runtime stores. The focused invariant reproduced all ten failures locally. Renaming those declarations to describe reuse, including their workflow test selector and the browser matrix fixture's shared type, makes the unchanged invariant pass. No cache keys, writers, test assertions, runtime code or scanner exemptions changed. The complete `scripts/ci` Go suite, Go vet and workflow lint also passed. Hosted validation must run again for the repaired commit.

## Local repair

The replacement uses a short trusted publisher. While validation is unfinished, its check remains queued in GitHub and releases its runner. A completion event reassesses the current PR and publishes a terminal result only after validation and trusted policy pass. Scheduled repair covers missing callbacks, reruns, retargets and later policy changes. The local workflow starts under the separate observation name `ci-required-next`, preserving the existing required gate until hosted behavior has been verified. The final switch removes waiting runner occupancy; no hosted latency benefit has yet been measured.

The intended ownership is explicit:

1. [ci-pr-gate.yml](../../.github/workflows/ci-pr-gate.yml) and [scripts/ci/pr-gate/main.go](../../scripts/ci/pr-gate/main.go) execute default-branch tooling. Only this isolated controller receives `checks: write`. PR artifacts and PR executable tooling never enter its write-capable process.
2. [ci-fork.yml](../../.github/workflows/ci-fork.yml) displays `pr-validation` while preserving the legacy `fork-ci #N` title and workflow path for older trusted consumers. Same-repository vouch allocation is skipped; fork validation still depends on successful admission.
3. [check-fork-validation.sh](../../scripts/ci/check-fork-validation.sh) supports `PR_GATE_ONCE=1`: one assessment returns exit 75 when validation or mergeability is pending. Legacy polling remains the default for older callers during migration.
4. The new publisher binds repository, PR, head, base ref/revision, validation workflow, run and attempt. A retarget or changed integration candidate requires fresh validation. It rechecks mutable state and current fork vouch policy immediately before publication, verifies the completed aggregate, and refuses any second open PR sharing the head commit because a commit-level check cannot express independent PR authority. It invalidates its prior success before reassessment, repairs absent initial events, and handles the Checks API's allowed null fields separately from non-nullable schema fields.
5. [ci-control.yml](../../.github/workflows/ci-control.yml) retains the existing PR required gate throughout observation. [ci-merge-group.yml](../../.github/workflows/ci-merge-group.yml) separately retains merge-group validation and its required gate, so the privileged PR entrypoint has no call path to untrusted validation. [ci-gate-reconcile.yml](../../.github/workflows/ci-gate-reconcile.yml) retains old/new display-name compatibility for existing run recovery and ignores metadata-only runs.

No test coverage or trusted policy is intentionally removed. Existing maintainer authority, signatures, fork vouching, read-only PR execution, cache boundaries and strict repository checks remain requirements. A queued result must never become success merely because a callback ran, a test job was skipped or a previous base passed.

## Observation and migration requirements

**The local workflow is configured for observation, not replacement of the required gate.** It explicitly publishes `ci-required-next` while preserving the existing `ci-required` job and repository policy. After this observation version reaches the default branch, verify both pending and terminal transitions on GitHub, including accepted Checks API payloads, latest-check precedence, rerun invalidation, and the repository recognizing a controller-created required check. Local and read-only tests cannot establish these provider behaviors.

Only after that proof, activate the replacement in one change: set the publisher name to `ci-required`, retire the `pull_request_target` polling/vouch workflow in `ci-control.yml`, and retain the separate merge-group validation and required job in `ci-merge-group.yml`. A skipped legacy PR job with the same name must not coexist with the new publisher. Update the workflow trust tests to enforce that final ownership. No ruleset weakening or bypass is part of this repair.

The publisher deliberately refuses a legacy job-created `ci-required` check on the same commit instead of adopting or overwriting a result it does not own. Once activation reaches the default branch, update each open PR against its current base to create a fresh head before expecting the replacement to satisfy its required check. For a stack, propagate the updated base through each layer. A base branch moving by itself does not create that new PR head.

## Validation and delivery status

Local checks passed: `go test ./scripts/ci ./scripts/ci/pr-gate -count=1`, controller race tests and Go vet, shell fixtures, ShellCheck, Actionlint and trusted-script policy checks. Executable regression coverage includes unfinished validation, failed or missing aggregate, fork admission, GitHub-proved reuse of a successful vouch on partial reruns, and later vouch revocation, stale head, newer attempt, moved/retargeted base, same-head collisions, prior-success invalidation, absent callbacks, malformed API metadata, Checks API field nullability and a lost write response.

A controlled before/after fixture demonstrates the behavior change: the landed polling verifier reports timeout and exits 1 for queued validation when its timeout is set to zero; the new one-shot verifier reports pending and exits 75 without polling sleep. This is executable evidence of control flow, not a hosted duration benchmark. The fixture output was retained as local investigation evidence.

Read-only live controller replays also completed. With `--dry-run` and the observation name `ci-required-next`, [PR #877](https://github.com/Hikyo-Org/Hikyo/pull/877) produced a proposed success bound to head `6c837a419ad122505cdfe3c6a68071d8662e7e56`, base branch `dependabot/go_modules/github.com/sigstore/sigstore-1.11.0` at `6e3defa90599594da01dc8ce457d65658a8e6d94`, and run `38060921960` attempt 1. This passed again with the final controller source. [PR #881](https://github.com/Hikyo-Org/Hikyo/pull/881) initially produced a proposed queued check while [run 38063556913](https://github.com/Hikyo-Org/Hikyo/actions/runs/38063556913) was in progress; the final replay correctly took no action because the PR had since merged. The replay logs were retained as local investigation evidence. No Checks API POST or PATCH was performed, so these replays establish live metadata and policy evaluation rather than check publication, native required-check acceptance or hosted speed improvement.

Three GPT-6.1 Sol subagents aggregated T3 history, Git history and live queue evidence, implemented the controller, and performed independent adversarial review rounds. All identified safety findings were fixed and their regressions pass. The final controller and observation-workflow reviews found no remaining actionable findings. The observation delta also passed the workflow Go tests, full trusted-script fixtures, ShellCheck and Actionlint; the legacy PR verifier and its policy remain in force; merge-group validation now has a separate entrypoint. Native check publication, latest-check precedence and required-check acceptance remain runtime unverified until hosted observation. The Jev PR-readiness probe will run against the committed candidate and live PR state before merge.

The implementation is being delivered through [PR #882](https://github.com/Hikyo-Org/Hikyo/pull/882). Hosted observation and activation remain pending. The automatic recovery described earlier was produced by the already-landed #869 workflow. Delivery status must be assessed from the current PR and workflow evidence.

The HTML sibling embeds all report content and styles. It requires no server, scripts, images, fonts or network access to read; source links are optional navigation. Browser previews at 390px and 1100px confirmed no page overflow, three contained tables, and no external assets. Both report files are local handoff deliverables. The local Graphify structure was refreshed; its existing partial Astro-parser warnings are unrelated to this Go/shell/workflow change.
