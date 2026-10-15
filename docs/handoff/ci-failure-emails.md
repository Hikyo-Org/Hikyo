# CI failure email investigation

Investigation on 15 October 2026 against main e01cd1afd20c530b1f9c733208217018d7e70007. Implementation and independent review were delegated to GPT-6.1 Sol at high reasoning. The table maps the eight screenshots in attachment order. Findings use GitHub run/job/check APIs, preserved logs and source-bound reproductions. Local validation does not establish deployed workflow behavior.

## Screenshot evidence

| Screenshot | Run | Evidence | Disposition |
| --- | --- | --- | --- |
| Queue gate, main ef3d900 | [38722637213](https://github.com/Hikyo-Org/Hikyo/actions/runs/38722637213) | Invalid ordered merge queue entry. The rejected snapshot was not retained, so the exact field is unknown. | Strict rejection retained; field-level diagnostics added. Underlying historical cause unresolved. |
| Trusted merge CI, PR #920 | [38714270787](https://github.com/Hikyo-Org/Hikyo/actions/runs/38714270787) | Supply-chain floor fixture rejected a dirty checkout; test_core also rejected stale reviewed SQL caller pins. | Already corrected by commit 219f7c9c7 on current main. Later full queue runs passed. |
| Queue report, PR #922 | [38733609829](https://github.com/Hikyo-Org/Hikyo/actions/runs/38733609829) | Reporter rejected its own check 116406012493 because GitHub returned /runs/116406012493 instead of the submitted workflow-attempt URL. | Fixed exact canonical-URL recognition; App, source binding, head, name and check ID remain authenticated. |
| Queue gate, main 568e9a1 / group e01cd1a | [38733605860](https://github.com/Hikyo-Org/Hikyo/actions/runs/38733605860) | Temporary PR #922 queue ref vanished around merge completion; a live follow-up GET returned HTTP 404. | Only this ephemeral-ref 404 becomes stale pending. An authenticated blocking check remains; no merge success is granted. |
| Queue gate, main 48e2fd9 / group 568e9a1 | [38728258199](https://github.com/Hikyo-Org/Hikyo/actions/runs/38728258199) | Check-runs API read failed. Original log omitted HTTP status, so transport versus HTTP cause is unknown. | Errors now retain validated HTTP status when available. Read failures still fail closed; no root cause claimed. |
| Queue report, PR #920 | [38725112094](https://github.com/Hikyo-Org/Hikyo/actions/runs/38725112094) | Same canonical-URL mismatch; own check 116389095971 remained in_progress. Screenshot report 38728258133 belongs to the same source run 38725101239. | Fixed by the reporter change. Both source runs 38725101239 and 38730188084 succeeded; reporting failed independently. |
| Go feedback, f2bb05f | [38723647008](https://github.com/Hikyo-Org/Hikyo/actions/runs/38723647008) | Attempt 1 exhausted the cumulative deadline compiling 16 packages after module downloads. Attempt 2 on the same SHA passed. | Recovered, not claimed fixed. Existing time, selection and cache-size limits retained. |
| Queue gate, publish f2bb05f | [38723644041](https://github.com/Hikyo-Org/Hikyo/actions/runs/38723644041) | Metadata run 38723673990 completed 10:55:22Z; job 116384219098 was created 10:55:32Z. Gate failed 10:55:31.399Z, between those events. | Completed/skipped run with a valid empty job list stays pending. It cannot reuse older success until the canonical metadata job exists. |

## Additional worker failures

Runs 38728437258 and 38718421247 failed bootstrap with “invalid publisher job identity”. Their logs omit the rejected response; later job metadata matches the immutable fields, which cannot reconstruct the earlier response. Both direct and worker bootstraps now report only the six allowlisted actual/expected identity fields. Predicates, source authentication and start+60-second deadline are unchanged. No retry or accepted-status widening was introduced.

## Why the Go cache was not broadened

The hosted trusted seed reported 1,166,292,541 expanded bytes and 8,594 files; its 243,101,097-byte archive restored in about 5 seconds during the failed run. A local Darwin export-archive comparison measured 1,312,356,770 bytes for the existing seed versus 2,185,476,756 bytes for the failed 16-package frontier. That exceeds the existing 1.5 GiB bound before other cache entries. These local size measurements are not hosted latency proof. The same-head successful retry does not erase the original timeout or prove every cold dependency update meets the budget.

## Validation

Before-fix regressions reproduced the reporter ownership rejection, deleted-ref operational failure, missing metadata job failure and absent bootstrap diagnostic. After-fix tests preserve wrong host/repository/check-ID rejection, 403/429/500/network/malformed failures, pending-to-ready recovery and blocking state. The 20 bootstrap cases exercise both real workflow snippets, including valid deadline export and invalid identity refusal. Full scripts/ci passed in 82.943 seconds; final bootstrap cases passed separately after the positive cases were added. Final gate/report race tests passed in 36.850 seconds and 1.338 seconds; vet passed. An earlier concurrent tooling run failed three bootstrap hash contracts while edits were in progress; the synchronized tooling run passed. No test was deleted. Actionlint 1.7.12 reports the same pre-existing unsupported concurrency.queue errors on base and changed workflows, with no additional diagnostics. Graphify was refreshed with existing parser warnings.

## Review and acceptance boundary

The independent Sol review reported no actionable verified findings in the code diff, including the metadata materialization fix. Parent review also checked the live canonical check and deleted-ref response against the changes. This is local review and test evidence. Hosted CI, merge and runtime acceptance must be recorded separately. The invalid ordered-entry field, worker bootstrap field and historical check-read status remain unknown; added diagnostics are not presented as fixes for those unidentified causes.

## After merge

Observe a normal merge-group lifecycle: the informational report should reach the source run’s terminal state on the same owned check, deleted queue refs should leave blocking pending outcomes without workflow failure, and early metadata completion should remain pending until its job exists. Inspect any new worker or ordered-entry failure using the added field diagnostics. Retain the original failed runs and exact run attempts as evidence.

## Follow-up: repair PR feedback failure

Run 38737427016 on initial repair commit 8bdab60e9 failed both feedback lanes at git diff --check: each new handoff file had an extra blank line at EOF. The local unstaged diff check had omitted those then-untracked files. Commit c666a38cf removes exactly those two blank lines; the full base-to-head whitespace check passes. This was an introduced report-formatting error, separate from the earlier CI defects. Future verification for this PR checks the complete base-to-head diff, including added files.

The external pending-state review identified a diagnostic gap: the generic pending message omitted the blocking skipped run. Reconciliation already retries through callbacks and scheduled discovery, with bounded individual invocations. The follow-up logs the exact PR, head, run and attempt on each empty-job observation and retains the blocking result until the canonical job materializes. No source-age cutoff or timeout increase is added.

## Follow-up: targeted publisher work-budget exhaustion

Run 38738461558, job 116433687176, matches the 14:58 queue-gate failure email for publish-755a522. It executed trusted main e01cd1a. The job started at 12:57:06Z and launched the publisher at 12:57:27.180Z, leaving 23.820 seconds before the original 45-second work cutoff. Setup included an exact Go 1.27.2 toolchain download/build. The final policy verifier was interrupted; the log reports both “trusted one-shot policy verifier could not execute” and “targeted admission work budget exhausted: context deadline exceeded”.

The four policy diagnostics are the initial/final proof and the failure-publication reserve, not an unbounded retry loop. The original log has no per-request timings, so it does not identify a particular slow API request. Same-head run 38738673919 subsequently succeeded in 36 seconds; run 38738681047 succeeded in about 33 seconds. Recovery does not erase the timeout. These runs all used main before this PR was merged.

The follow-up overlaps two independent reads within each protection snapshot and each PR/queue role snapshot. Initial and final snapshots remain separate and fresh; every existing predicate, error refusal and the setup-inclusive 60-second deadline is retained. Both readers are cancelled and joined on failure. A deterministic response barrier fails on the serial implementation and passes with overlap. Full-admission fixture replay, with the shell authority verifier stubbed and 30 ms injected per controller API read used the same 23 reads: 728.0 ms serial versus 606.8 ms concurrent (16.6% less elapsed time), with at most two reads in flight. Local read-only API medians over three paired samples were 1.349 s versus 0.520 s for protection and 1.286 s versus 0.764 s for roles; a 7.44 s serial protection outlier was retained. These measurements establish less serial waiting, not a guarantee against arbitrary hosted API delays or a hosted acceptance result.

Validation for the concurrency follow-up: the serial implementation failed the deterministic overlap test; the concurrent implementation passed. An existing test encoded inventory-before-queue ordering, so it was updated to require both final reads after final feedback, retaining duplicate, missing and malformed inventory rejection. Ten additional cases force either response order and verify healthy admission plus collision, shared-head, actor-change and missing-member refusal. Final gate/report race tests passed in 40.736 s and 1.357 s; full scripts/ci passed in 88.725 s; vet and full-range whitespace checks passed. Independent Sol review found no actionable issue, with focused race tests repeated 20 times. Graphify refreshed with its existing 17 Astro parser warnings. No hosted timing improvement is claimed before merge and a trusted-main run.

## Follow-up: PR 923 merge-group source-security cache failure

The 16:22 report email maps to run 38750549402, job 116475501331 (22 seconds). It checked out main e01cd1a and rejected owned check 116466620492 because GitHub returned its canonical /runs/116466620492 URL. The existing reporter fix in this PR already covers that exact case; focused URL and ownership regressions passed again. The report failure is separate from the source validation failure.

The source merge-group run 38747909139 tested commit 3cfb15299aecf640b0191d917d0f986e487d8c5e. Its source-artifact-security job 116469496314 restored go-test-v2 fallback cache 8979722714, a 4,024,407,236-byte compressed archive. Download consumed about 44 seconds; extraction started at 14:13:33.850Z and was still running when the original two-minute cache-restore limit expired at 14:14:50.252Z. The fixture builds and adversarial campaign never ran. The shared module restore had succeeded.

The user queued PR 923 at 14:02:26Z; GitHub removed it at 14:31:02Z after validation failed. Live referenced-workflow metadata confirms ci.yml came from the merge-group commit, so a fresh candidate/group can exercise this workflow correction before merge. The reporter separately reads trusted default-branch tooling. No cache deletion, security-test bypass, or timeout increase is needed.

The source-security job now restores only the existing go-feedback-v1 compiler-cache family into .ci-cache/go-feedback, with matching GOCACHE, and no full-test fallback or new writer. The trusted seeder retains its 1.5 GiB expanded-size and 12,000-file publication limits. At investigation time no seed was retained, so the exact three source build commands were run with an empty compiler cache and the real group SPA artifact: all passed locally on macOS/arm64 in 73.56 seconds under the existing 300-second timeout. The resulting local cache grew to 2.34 GB during compilation but was not published. This is cold-build evidence, not hosted Linux campaign proof.

Full scripts/ci passed in 106.470 seconds. Cache policy, source-security coverage contracts, trusted-base migration, actionlint for ci.yml, ShellCheck and whitespace checks passed; Graphify refreshed. Independent review found no actionable issue and confirmed premerge workflow authority accepts the corrected candidate while still requiring successful security-job results. Trusted-main bounded-seed refresh run 38753300351 was dispatched separately; its result and the fresh merge-group result remain hosted acceptance evidence to collect.
