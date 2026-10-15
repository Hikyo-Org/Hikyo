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
