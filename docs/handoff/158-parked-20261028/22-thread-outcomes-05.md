# Historical T3 thread outcomes, part 5/16

These are per-thread scope and reported outcomes, not current operational approvals. Parent and current core status control. Source-only/component/static/no-findings verdicts do not establish real AWS, full lifecycle, adoption or final merge. Local references and typographic dashes are normalized, so the quoted presentation bytes differ from the original message-text hash. Every known task/controller thread has a stable reference and independently read API identity.

## T074: [Diagnose current publisher budget failures](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-f3-publisher-budget-diagnosis-20261022-r1)

Created 2026-10-22T20:24:52.286Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the research sub-agent for this task. Read-only/source-only evidence diagnosis of repeated f3 publisher budget failures for PR969. Do NOT query GitHub/gh/network or watch/poll; Root has logs. Do not edit/report files/Git/browser/private/nativeDB/delegate. Repo <local-artifact>/t3-a78c6309 HEAD f3f964d096ff6934134c7a8b151feb4892a9f4e9 currentmain83d310cd20526bf8b2ada817077c54596fd96bc2. Read AGENTS/shared/WORKSTYLE. Exact logs <local-artifact>/hikyo-158-f3f-publisher-120058830669.raw.sanitized.log and <local-artifact>/hikyo-158-f3f-publisher-120058841535.raw.sanitized.log. Scoped searches before large context/Jev ifneeded. First run39845862780 job120058830669: setupinclusive60s budget; cold Go1.27.3 download ~19s; several policy/DCO r [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `d8959ded91356098b55825082612a0c1bae9d282f85b0e328b150d16917bef90`):

> **The evidence supports cold startup plus a long serial API path exhausting the work budget. It does not establish an accidental reassessment loop, duplicate DCO fetch, or compiler defect.**
>
> 1. **Both builds succeeded; admission had little time left.**
>
>    - Run `39845862780`: authenticated start `20:10:49`, build finished `20:11:17.402`, work cutoff `20:11:34`. Admission had **16.6 seconds** remaining.
>    - Run `39845866657`: start `20:12:58`, build finished `20:13:30.889`, cutoff `20:13:43`. Admission had **12.1 seconds** remaining.
>    - Build steps consumed **18.48 / 19.48 seconds**, including the Go 1.27.3 download. The logs cannot separate download, extraction, compilation and linking.
>    - Both checked out trusted main `83d310cd20526bf8b2ada817077c54596fd96bc2`.
>
>    Evidence: first log (local reference: `hikyo-158-f3f-publisher-120058830669.raw.sanitized.log:182`), second log (local reference: `hikyo-158-f3f-publisher-120058841535.raw.sanitized.log:182`).
>
> 2. **The apparent repeated policy/DCO work mostly has an explicit purpose.**
>
>    Normal successful admission reads policy twice. After timeout, `refuseAdmission` reads it twice again. The third and fourth policy messages in each log occur **after the work cutoff**, consistent with refusal publication. They do not demonstrate four admission passes. Targeted admission explicitly disables the group reread loop.
>
>    The first run fetched DCO metadata once, then logged `reused_metadata=true`; its final canonical DCO/conflict checks took about **49 ms combined**. Removing that recheck would save little and weaken the final proof.
>
>    The first failure identifies the feedback Jobs endpoint, but **does not prove that endpoint stalled**: the request may simply have encountered the shared deadline near the end of the serial path. The second error lacks endpoint detail.
>
>    Sources: admission flow (local reference: `queue.go:306`), timeout refusal (local reference: `queue_deadline.go:92`), reread eligibility (local reference: `queue.go:217`).
>
> 3. **There is concrete API cost, but no proven redundant-read defect.**
>
>    The composed head-worker fixture requires **22 controller reads plus 10/12 canonical shell-verifier reads** for BDFL/reviewer workflow authority. Additional pagination and fork proof can add more. The shell verifier independently reads current PR metadata, changed files and applicable authority during both verification phases.
>
>    Current code already overlaps the two policy endpoints and the two role-inventory endpoints. Much of the surrounding proof remains serial. The build helper downloads/selects the exact toolchain once and builds once; source inspection found no duplicate SDK-install/build loop.
>
>    Sources: composed read counts (local reference: `queue_policy_composition_test.go:119`), canonical verifier (local reference: `check-fork-validation.sh:185`), build helper (local reference: `build-queue-publisher.sh:25`).
>
> 4. **Existing coverage misses the combined failure scenario.**
>
>    - Toolchain coverage executes the workflow/helper against a **Go shim**, including download hangs and cutoff enforcement. It does not measure real cold compilation.
>    - The injected-latency test adds **30 ms per controller read**, mocks policy/DCO verification, and logs elapsed time without asserting a useful timing budget.
>    - Canonical shell composition tests verify read counts and authority revocation, using immediate transport fixtures.
>    - Deadline tests verify terminal refusal and moved-tuple safety, but do not combine cold startup, realistic API latency and canonical verifier cost.
>
>    Sources: toolchain test (local reference: `queue_native_toolchain_test.go:133`), latency test (local reference: `queue_parallel_reads_test.go:89`), deadline test (local reference: `queue_deadline_test.go:39`).
>
> 5. **Recommended narrow corrective direction and verification.**
>
>    Add phase timings for toolchain selection/download, compilation, canonical authority verification and API reads. Then overlap independent reads **within existing initial/final proof boundaries**, using cancellation and join semantics. Preserve fresh final head/base, policy, authority, feedback attempt/jobs, role/ownership scans, canonical DCO and signature protections. The evidence does not yet establish how much this would save.
>
>    Required verification is a combined direct-callback and worker replay with approximately **28/33 seconds already consumed by setup**, delayed API and real canonical verifier composition. Assert eligible admission completes before the original **45-second cutoff**, or safely refuses by the original **60-second deadline**, retaining the **15-second reserve**. Preserve all movement, revocation, collision and cancellation regressions. Real cold hosted execution remains necessary to establish timing.
>
>    Both workflows use trusted default-branch tooling, so PR-only changes cannot repair these publishers until that tooling lands on main.
>
> No tests, edits, Git operations, network calls, delegation or monitoring were performed. The existing worker and independent full candidate were left untouched. Memory supplied triage conventions only; findings above were checked against the supplied logs and source.

## T075: [Review preparation fixes and source composition](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-AH-composition-review-20261022-r3)

Created 2026-10-22T20:54:29.589Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. Independent adversarial SOURCE-ONLY combined review, no edits/report writes/browser/CLI/client/core execution/provider/AWS/GH/auth/cache/profile/nativeDB/Git/delegation. Allow only disposable synthetic test outputs cleaned afterward; never recurse into __pycache__. Root B=<approval-bank>. Read applicable shared/WORKSTYLE/Python/JS rules. Three narrow packages, review separately in one result: A chrome-first-prepare-custody-integration-source: original real configured-model Chrome preparation/runtimebinding and complete command custody. Prior review fixed realmanifest null fields, JSUnicode receipt canonicalization separateG, callback URL beforeDOM. Latest P2 shipped source-binding.unconfigured.json [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `3e3109a51996372363a98774d76fb8bd6598bf4b51c67ac2c59ad0eac5aae860`):

> Two findings: C has a compiler source-custody gap; H has a malformed diff artifact. A has qualified no findings.
>
> 1. **A: no findings within the R4 correction.** Reviewed manifest: `0de074b0eed95d64d35ea51031339ec19c29e086b9e0e853df06f12f5fbd28e7`.
>
>    Independently compared all 48 prior files: only the template changed. All 53 current pins match. The new regression passed using the actual unchanged verifier, including rejection of the old owner-pin mutant. Registry/script consistency and unconfigured null/false fields remain checked. Prior tests are unchanged; I did not rerun the full 40-Python/22-VM/68-VM suites.
>
> 2. **H: [P3] Repair the complete diff’s missing file boundary.** At line 171 (local reference: `<approval-bank>/chrome-persistent-create-lock-runner-H-R4.complete.diff:171`), the HTML’s final line and the next `--- reviewed-H455/SOURCE-PINS.json` header are concatenated. The following `+++` header is consequently orphaned. This is malformed unified-diff output and cannot reliably reconstruct the exact files.
>
>    Regenerate with correct handling of files lacking a trailing newline, including the conventional no-newline annotation.
>
>    Reviewed H manifest: `edae7c4a1c8921933832fca09cd561c9ecce3ec37e6df932675e5f77afbb1647`. Supplied diff SHA: `ee3725528606d50c9f3438e359d885e291a383ea95d7cf146cb95a44af516b28`.
>
>    No H implementation finding: all 39 previous files are byte-identical, all 43 current pins match, and exactly one existing ancestor row changed. Permanent burns, whole-command custody, false-authority flags, and the 55-second window including SQL validation remain intact. The 29-test suite was not rerun.
>
> 3. **C: [P2] Execute the verified compiler snapshot rather than reopening mutable source paths.** compose.py:136 (local reference: `<approval-bank>/root-owner-composite-staging-source/compose.py:136`) passes the live base directory to the bridge; validate_contracts.cjs:7 (local reference: `<approval-bank>/root-owner-composite-staging-source/validate_contracts.cjs:7`) then requires its modules again. The earlier verified bytes are not the bytes necessarily executed.
>
>    Reproduced using the existing disposable `Fixture`: immediately before invoking the real validator, append a harmless marker write to synthetic `protocol.cjs`, then restore it in `finally`. Result: `inspectOK=true`, the unpinned module’s marker was written, and final baseline verification passed. No actual core, loader, browser, or candidate was used.
>
>    Execute only verified module snapshots with controlled dependency resolution. Add a regression asserting that transient replacement cannot execute the marker; rejection after execution is insufficient.
>
>    Reviewed helper manifest: `f39c259d12772a2ebbccfdf635375c676f53993f4c48e9dd5dc0c08cd01d8b73`. All 27 focused tests passed unchanged. The 64-script, 65,536-byte script, and 262,144-byte manifest caps remain enforced.
>
> The additional UI subset has qualified no findings at registry SHA `acb646c40a2db3e072ba354537282f38295774d1a3a59cbba50b45880734445f`, 91,979 bytes. All nine complete entries, script bytes, and source-pin rows match U; selected/omitted provenance is exact. No omitted-ID dependency appeared in the inspected phase callers or retained scripts. In-memory serialization confirmed 247,877 bytes for base+A+full U and 172,323 for base+A+subset.
>
> G63, R2’s 146 enumerated files, helper/artifact pins, and full Markdown/offline-HTML parity verified. Synthetic outputs were cleaned; source readback remained unchanged. Billing/form behavior, pending Create operators, final composition fit, runtime evidence, and adoption readiness are not certified. Fresh review is required after corrections.

## T076: [Review billing and KMS form readouts](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-billing-form-readonly-review-20261022-r1)

Created 2026-10-22T20:56:41.873Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. Independent adversarial SOURCE-ONLY first review of B/chrome-billing-form-readonly-source, B=<approval-bank>. No edits/report/log writes/browser/CLI/client/core/provider/GH/AWS/auth/cache/profile/nativeDB/Git/delegation. Disposable synthetic test outputs only, clean afterwards; no recursive __pycache__ inventory. Readshared/WORKSTYLE/matchingJSrules. Originalbrief: NEWsibling2strictread-only executable registry scripts to obtain actual nativeBilling period/refresh/displayedtotal/tax/completechargetables (NO interpretation/fabricatedzero/gross/billable inference/creditoffset/spendingauthority) on ONLY two historicallyverified URLs with/without region query, and narrow bounded KMScreate publicformstr [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `a549845153b37de79138a8a72ee5099e9bb8e2218e9276500723155053fb4a88`):

> Five actionable source findings remain:
>
> 1. **P1: KMS can export editor contents as public names.** public-dom.cjs:54 (local reference: `<approval-bank>/chrome-billing-form-readonly-source/public-dom.cjs:54`) excludes only literal `contenteditable="true"`. Append a synthetic editable div with `contenteditable=""` or `"plaintext-only"` beneath the fixture label: both scripts’ actual compiler checks accept KMS output containing `DescriptionSYNTHETIC_EDITOR_TEXT`, while `valuesRead` remains false. Recognize all editable states, including inheritance, before walking public text; add getter-trap regressions.
>
> 2. **P1: Billing accepts incomplete hidden or collapsed charge data.** billing-readout.cjs:39 (local reference: `<approval-bank>/chrome-billing-form-readonly-source/billing-readout.cjs:39`) silently skips hidden tables, and collapse checks cover only table descendants. Appending a hidden charge table containing synthetic `USD 99.00` still succeeds with the original two tables. A visible collapsed section outside those tables also succeeds. Hold on bounded evidence of omitted charge sections instead of treating the remaining tables as complete.
>
> 3. **P2: Account matching accepts ambiguous identity.** public-dom.cjs:23 (local reference: `<approval-bank>/chrome-billing-form-readonly-source/public-dom.cjs:23`) accepts the expected digits anywhere in banner text. Setting the fixture banner to `Alias 099256073061, account 000000000000` succeeds. Require an unambiguous account identity; at minimum, reject conflicting account-number candidates.
>
> 4. **P2: Visibility checks miss transparent ancestors.** public-dom.cjs:32 (local reference: `<approval-bank>/chrome-billing-form-readonly-source/public-dom.cjs:32`) checks only the element’s own computed opacity. A label beneath an `opacity:0` ancestor is still returned with `visible:true`. Apply bounded effective-visibility checks to public names, fields, and the account banner.
>
> 5. **P2: Header spanning cells bypass the declared hold.** billing-readout.cjs:41 (local reference: `<approval-bank>/chrome-billing-form-readonly-source/billing-readout.cjs:41`) validates spans only on data cells. Setting the first header’s `colspan="2"` succeeds with three projected columns. Validate header visibility and spans too.
>
> Verification completed:
>
> - **51/51 supplied tests and 10 syntax checks passed.**
> - **192 additional account-access redirect/expiry probes held before subsequent DOM access**, across initial, intermediate, post-read, and terminal callbacks.
> - Both generated scripts exactly match the declared scoped derivation. Existing mutation negative controls pass.
> - Markdown/offline-HTML parity, 16 local pinned files, U39, G63, and five native copies verified.
> - Node `26.7.0` binary SHA256: `a9bd0630891c2dcdee70de88270fee2cc0c4a9e76495039dd3b4f91c5e6b71df`.
> - SOURCE-PINS SHA256: `fb6f5f0b36106dc2aec914a18777e7d7f92d8cc1c7ddeebdacc0f5e41989feec`.
> - Fragment SHA256: `044c178b9d3c7bd628b871a9d7ea9ea0aa9fd03dc2da4d4c2af21836a9e650e4`.
>
> No edits, report/log writes, browser/provider operations, Git, or delegation were performed. Synthetic probes stayed in memory. This review grants no runtime, readiness, acceptance, or privacy/completeness approval.

## T077: [Verify compiler snapshot custody fix](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-composition-snapshot-review-20261022-r2)

Created 2026-10-22T21:12:36.570Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. NEW independent adversarial SOURCE-ONLY R2 review of C=B/root-owner-composite-staging-source, B=<approval-bank>. No edits/report/log writes/browser/provider/GH/AWS/auth/profile/cache/nativeDB/Git/delegation or actual candidate staging/copyback/adoption. Disposable syntheticoutputs only cleanup. Read shared/WORKSTYLE/Python/JS rules; no recursive __pycache__ inventory. Originalbrief: inert compose.py inspect/stage REQUIRED explicitpinnedfragmentlist, verified R2base146enumeratedpublicsource/vendorfiles, actualownerclosedZodschema/compiler+syntax WITHOUTloader/core/script evaluation; newexclusiveB siblingcandidate, exactnonregistrycode/contract/vendor preservation; ONLY addedreviewed-scripts and chan [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `2f866ced993631c597a88abcb71ee96deb49d7b35a5c92f40ae228bbfe78e275`):

> C: **R1’s P2 is corrected for all three owner-module inputs.** Qualified no additional findings within the report’s trusted-helper/Node/OS model. A broader guarantee that no substituted code executes does **not** hold.
>
> - Independently repeated transient replacement for `contracts.cjs`, `protocol.cjs`, and `utilsBundle.js`: the checkpoint executed each marker; the correction executed none. Every inspect succeeded, final baseline checks passed, and no candidate was staged.
> - **Remaining custody limitation:** compose.py:144 (local reference: `<approval-bank>/root-owner-composite-staging-source/compose.py:144`) launches Node and the bridge by pathname. In disposable copies, replacing either immediately before subprocess execution, then restoring it, produced `inspect.ok=true` **with the marker executed**. The report explicitly trusts these components; the correction supplies no protection against their substitution.
> - Direct focused suite: **33/33 passed**. Verified all 27 prior test bodies/statements and unchanged fixture helpers, dependency rejection, stripped Node preload variables, three Python AST checks, Node syntax/version, unchanged caps, and exact correction-patch reconstruction.
> - Verified all **146 R2 pins**, checkpoint pins, correction tables, delivery/artifact entries, and complete offline Markdown/HTML parity. Final readback found no source changes.
>
> Exact C pins verified:
>
> - Helper: `6ff4c9c58f31d4b34d98fd746e09aa7a12e390239560d636a544370c9c25bab9`
> - Delivery: `b74c1ab6f3cc5b18d4a167b7c8e4bf6865c25bd6c7ac025800676c035fd0b1ba`
> - Artifact: `b6cad38cc40217c6a69c13b737b94b0cb74bdee442ed7a9aef0ef2172f1ac8d4`
> - Node 26.7.0: `a9bd0630891c2dcdee70de88270fee2cc0c4a9e76495039dd3b4f91c5e6b71df`
>
> H artifact correction: **qualified no findings**. Independently applied the corrected diff with `/usr/bin/patch -p1 -t`; all **44 leaves** reconstructed byte-for-byte. All **39 previous file entries** remain unchanged; all **43 current pins** verify.
>
> - Corrected diff: `ed5867cda0a531a79ee5a879fffd4b3b7d936afc2ff879291048e2cef5f26948`
> - Preserved malformed diff: `ee3725528606d50c9f3438e359d885e291a383ea95d7cf146cb95a44af516b28`
> - Unchanged H manifest: `edae7c4a1c8921933832fca09cd561c9ecce3ec37e6df932675e5f77afbb1647`
>
> G63 was not identifiable in the allowed C/checkpoint/owned R2 inputs, so that named check remains unverified.
>
> No actual loader/core or reviewed-script bodies executed. H behavior tests were not rerun. Disposable outputs were removed; no source/report/log edits, actual staging, copyback, adoption, browser/provider/Git/delegation occurred. These results convey no completion or readiness approval.

## T078: [Review guarded native CreateKey stage](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-native-create-stage-review-20261022-r1)

Created 2026-10-22T21:14:34.932Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. Independent adversarial SOURCE-ONLY first review of N=B/chrome-native-create-stage-source, B=<approval-bank>. No edits/report/logwrites/browser/CLI/core/client/provider/GH/AWS/auth/profile/cache/nativeDB/Git/delegation/actualproductionloader. Disposable synthetictests onlycleaned. Readshared/WORKSTYLE/Python/JS. ExactSOURCE-PINS966b708bf5c842346fa967917be2d3c95138bc4ea73951f96147e9afb6bb948f; fragment40d1bb5d706e8372646e1d971b08f756fc2a183621da2a507cabe63aad23ce96, compact42508bytes.14Python/48VMclaimed pinnedNode26.7.0 SHAa9bd0630891c2dcdee70de88270fee2cc0c4a9e76495039dd3b4f91c5e6b71df. Readfullhandoff/source/tests/provenance andcompleteMDofflineHTMLparity. Originalbrief concrete5strictclosedregis [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `cf3fd8ee657ae5811fc27d0a7348e0d6c541066e7c85f4f423405739c198bbce`):

> Two actionable findings. This source review is **not clean**.
>
> 1. **P1: Arm and Finish can execute after descriptor expiry.** stage-kernel.cjs:55 (local reference: `<approval-bank>/chrome-native-create-stage-source/stage-kernel.cjs:55`)
>
>    `descriptor()` checks time before its Review/status/DOM work. Subsequent `gate()` calls enforce the shared window, but omit the descriptor’s 2,000 ms and physical deadlines. Arm and click therefore remain reachable after those deadlines.
>
>    Reproduced using the existing real Python descriptor and frozen-function VM: advance `NOW` to `effectBeforeMS` during the second Review heading read. Arm leaves the guard **armed**; Finish produces **one click**. Both subsequently return `native-create-stage-held`. Advancing to H’s physical expiry also permits one click.
>
>    Recheck descriptor and physical time after all DOM/status/API resolution, immediately before each effect. Retain completion checks. Add regressions asserting zero arm transitions/clicks when expiry occurs during final validation.
>
> 2. **P2: Finish eligibility omits CSS visibility and effective disabled state.** stage-kernel.cjs:85 (local reference: `<approval-bank>/chrome-native-create-stage-source/stage-kernel.cjs:85`)
>
>    `getClientRects().length` does not establish CSS visibility, and `button.disabled` does not cover an ancestor disabled fieldset. Programmatic `.click()` supplies no visibility check.
>
>    Additional DOM doubles exposed both omissions: a CSS-hidden button was clicked; a fieldset-disabled button’s modeled native click did nothing, while the operator returned `clickDispatched:true`. These are synthetic reproductions, not browser evidence.
>
>    Add bounded checks for CSS visibility and native effective disabled state within the same callback, with corresponding refusal cases.
>
> Verification completed:
>
> - Exact source pin: `966b708bf5c842346fa967917be2d3c95138bc4ea73951f96147e9afb6bb948f`.
> - Exact fragment pin: `40d1bb5d706e8372646e1d971b08f756fc2a183621da2a507cabe63aad23ce96`.
> - All 51 local and 218 dependency pin/size checks matched; complete derivation and Markdown/offline HTML text parity matched.
> - Supplied **14 Python tests and 48 VM scenarios passed**. Existing scenarios miss the findings above.
> - Final source bytes remained unchanged; disposable directories were cleaned.
>
> Real native probe/wizard compatibility, composite adoption, owner/control authentication, and a concrete preflight-plus-action driver remain separate unresolved Root gates. No runtime, provider, acceptance, or readiness grant is made.

## T079: [Verify billing and form readout corrections](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-billing-form-readonly-review-20261022-r2)

Created 2026-10-22T21:16:29.381Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. NEW independent adversarial SOURCE-ONLY BF R2 review B/chrome-billing-form-readonly-source whereB=<approval-bank>. No edits/report/logwrites/browser/CLI/client/core/provider/GH/AWS/auth/profile/cache/nativeDB/Git/delegation. Disposable synthetictestoutputs onlycleanup; no recursive__pycache__. Readshared/WORKSTYLE/matchingJSrules. Originalbrief2strictclosedread-onlyregistry scripts billing-native-projection-v1 andkms-create-form-structure-v1; exacttwoBillingURLs with/withoutregion andoneKMScreate; actualCDPtarget/context/mainframe/loader+correctfragmentrelationbefore/after; original<=5min actualclockwindow andexactroute/unambiguousaccount099256073061 beforeANYDOM andcompletion; no arbitraryselector [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `659c3a17ac5c172a1c039dd6593f334fb8a42878e5e7ad0a00746bcfe1cbb822`):

> Three actionable findings remain.
>
> 1. **P1: KMS exports text inside a textbox ancestor.** public-dom.cjs:77 (local reference: `<approval-bank>/chrome-billing-form-readonly-source/public-dom.cjs:77`) checks `role="textbox"` only on the current node. Reproduction: wrap a label containing `SYNTHETIC_EDITOR_TEXT` in `<div role="textbox" aria-label="Key policy">`, including that wrapper in the field inventory. The exact compiled script succeeds and exports the marker as label and associated-field names with `valuesRead:false`. Heading and button variants also succeed. Check protected editor ancestors before every text walk.
>
> 2. **P1: Billing misses an enabled ARIA Load more button.** billing-readout.cjs:42 (local reference: `<approval-bank>/chrome-billing-form-readonly-source/billing-readout.cjs:42`) includes `[role="button"][aria-label="Next page"]` but omits its `Load more` counterpart. Adding a visible enabled `<div role="button" aria-label="Load more">` still returns both existing tables successfully. Native-button variants and ARIA Next page correctly hold. Cover the omitted variant within the existing bounded pagination guard.
>
> 3. **P2: Account validation aggregates hidden/editor text.** public-dom.cjs:34 (local reference: `<approval-bank>/chrome-billing-form-readonly-source/public-dom.cjs:34`) reads whole-banner `textContent` after checking only banner/ancestor visibility. A visible banner whose approved number exists solely in an `opacity:0` descendant passes. A plaintext-only editable banner or editable descendant also succeeds; the synthetic KMS execution records 185 aggregate plaintext reads while returning `valuesRead:false`. Use a bounded, route/clock-guarded public-text extraction that protects hidden/editor subtrees before reading text.
>
> Verification completed:
>
> - **71/71 tests and 10 syntax checks passed** directly under Node **26.7.0**; binary SHA256 matched `a9bd0630891c2dcdee70de88270fee2cc0c4a9e76495039dd3b4f91c5e6b71df`.
> - All five exact R1 reproductions are corrected. All 51 original test bodies remain byte-identical. Each of six mutations preserves the same healthy positive and revives its corresponding defect.
> - Generated scripts match their declared composition byte-for-byte. Provenance, contracts, HTML/Markdown equivalence, U39/G63/native5, local pins, and the read-only checkpoint verified stable.
> - SOURCE-PINS: `aa75981fec755f1b5582f13776c5154691bc5d28bba25bbccbcdf5a8a021e7e9`
> - Fragment: `7680093d266ee4ccf252e79c235d16f0a08afda892a398e13961e48277fa8c22`
>
> No files were written. These are synthetic source findings; no runtime, readiness, privacy-completeness, real Billing layout/Expand All, C-composition, or NCreateStage approval is given.

## T080: [Recover the proven native KMS read probe](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-historical-native-probe-route-audit-20261022-r1)

Created 2026-10-22T21:24:09.743Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the research sub-agent for this task. Narrow READ-ONLY historical public evidence/source research to resolve actual native KMS successfulReadProbes prerequisite for c05. No edits/report writes, browser/provider/CLI/GH/network/auth/config/cache/profile/nativeDB/Git/delegation. E=<local-artifact>/aws-recovery-20261020, B=E/banked-series-approval-20261022,G=E/next-run-source-preparation/banked-series-first-attempt-source. ReadAGENTS/shared/WORKSTYLE. Source/evidenceonly, doNOTrecursive inventory E (huge PGstores), no __pycache__. Start explicitpublicG native/native_owner_kms_xhr_guard.source-only.js and future_key_guard_deadline: actualrecheck demands completedallowednativeKMSreadprobe>=1,0activeReads,CreateKey0sends. Guardallowedreads  [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `8caadb189434fa318e1f0ca794ea2654ff2c1d9e95fb400a8c8923cbe0fb42eb`):

> Fixed the pre-final-sample RETURN gap and sealed the successor for NEW independent review.
>
> - **167/167 tests passed**, plus 12 syntax checks.
> - All 130 prior cases/assertions retained; only the documented mutant recipe extension changed.
> - Original 87-test file unchanged.
> - Both outside checkpoints verified read-only; all 35 dependencies unchanged.
>
> Exact SHA256 pins:
>
> ```text
> SOURCE-PINS (13535 bytes)
> 17f6dee86f3d589259c9814cbe84892fcfe7d6a37fa28a011c0b34a67d53e4ef
>
> Registry (10063 bytes)
> b92188fff4322ffc9502c58a31233e462114afcc1cd21bc0bf533831a2ef3515
>
> Script (28829 bytes)
> 70d12d30797e85d0022f078077937062c3d345972b0df0447189edcedd753b03
> ```
>
> Full Markdown report (local reference: `<approval-bank>/chrome-setup-policy-fixed-fragment-navigation-source/REPORT.md`) · Complete offline HTML (local reference: `<approval-bank>/chrome-setup-policy-fixed-fragment-navigation-source/REPORT.html`)
>
> Post-final-sample reply loss remains an external Root custody boundary: Root must stop/hold on a held or unauthenticated result. Runtime, native compatibility, composite, adoption, provider and resource gates remain held.

## T081: [Verify native CreateKey expiry and visibility fixes](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-native-create-stage-review-20261022-r2)

Created 2026-10-22T21:32:32.173Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. Independent adversarial source-only R2 review. Task endpoint is ultimately issue158/PR969 real acceptance+cleanup+merge, but YOUR scope only B/chrome-native-create-stage-source (N). B=<approval-bank>. Current N SOURCE-PINS.json SHA256 2ba7838bacc288d6e0d9a4ebd8cafef2a6fb0ef47822947907fc14c209acfc9a; fragment950bf5fc6320577205d4fab96ca50a401a5a26158f34bb3ed78fb3ca85a05e8f. Read HANDOFF.md,R1-FIX-REPORT.md and all local runtime/tests/provenance. Original reviewed checkpoint B/chrome-native-create-stage-review-966b708b-readonly-checkpoint (source966b708bf5c842346fa967917be2d3c95138bc4ea73951f96147e9afb6bb948f,fragment40d1bb5d706e8372646e1d971b08f756fc2a183621da2a507cabe63aad23ce96). Prior R1 finding1  [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `60a2da219ec3a7fbcd20c32c9d99ff0b3ace0fa5f4a5dff5d1745c7d65224f34`):

> No confirmed findings in this source-only R2 review of N. Both prior R1 fixes withstand the tested regressions.
>
> - **Validation:** 16 Python tests passed, including 48 preserved VM scenarios and 96 R1 regression/mutant scenarios. Another 22 independent synthetic boundary, redirect and completion cases passed.
> - **P1:** Expiry cases refuse before guard transition or native click. Removing the timing fix in memory breaks the zero-effect assertions.
> - **P2:** Hidden and effectively disabled controls refuse before click invocation. Independent visibility/disabled mutants fail; healthy button, submit and first-legend cases still work.
> - **Integrity:** All 56 local entries and 218 unchanged dependencies verify. Original tests remain byte-identical, with no detected weakening or policy relaxation. All eight generated outputs reproduce exactly in memory. Both provenance diffs and HTML full-text companions verify.
> - **Final readback:** Source and checkpoint remain unchanged; synthetic temporary directories were cleaned up.
>
> Verified immutable SHA256 pins:
>
> ```text
> SOURCE-PINS.json
> 2ba7838bacc288d6e0d9a4ebd8cafef2a6fb0ef47822947907fc14c209acfc9a
>
> review-manifest.fragment.json
> 950bf5fc6320577205d4fab96ca50a401a5a26158f34bb3ed78fb3ca85a05e8f
> ```
>
> This establishes source and synthetic-test evidence only. Actual native probe/wizard compatibility, owner/exclusive control, composite freeze/adoption and the paired preflight/action driver remain Root gates. No runtime readiness, adoption or provider acceptance is granted.

## T082: [Verify billing privacy corrections and composite public harness](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-billing-form-and-public-harness-review-20261022-r3)

Created 2026-10-22T21:36:57.128Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. Fresh independent adversarial source-only R3 review, primarily BF, plus a one-line Root public-harness derivation. No edits, actual provider/browser/nativeDB/privateauth/Git/network/delegation. B=<approval-bank>. BF=B/chrome-billing-form-readonly-source; current SOURCE-PINS58b10d6c38a827c971169da52c205719c4d84315b3e5d9dbfa4aab5768bc6a6b; fragmentcd31677350845bf428e67c2a134e3817a9f9ad126d2228a33de4d3bc3ebc8219. Read fullREPORT/provenance+runtime/tests. Bothscripts use closedpublicDOM extraction; billingholds ifincompleteunknown, KMSform returnsstructuralpublicnamesonly noinputvalues/editorpolicies. R1 fivebugs fixed: contenteditable empty/plaintext/inheritance; hidden/collapsedchargecoverage; confli [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `1f212f447d269eadd063a7e44d94388f4beddd2d1a9a96474f553c94f42180d2`):

> **BF: changes required, two P1 findings.**
>
> 1. public-dom.cjs:78 (local reference: `<approval-bank>/chrome-billing-form-readonly-source/public-dom.cjs:78`): Billing’s descendant filter misses protected roles. A nested `role="searchbox"` exports synthetic editor text in both the charge description and table text. Getter traps confirm plaintext acquisition before refusal; a corrective mutant prevents access.
>
> 2. public-dom.cjs:100 (local reference: `<approval-bank>/chrome-billing-form-readonly-source/public-dom.cjs:100`): selecting the innermost main region can omit charges. A synthetic outer main’s visible USD 99.00 table was omitted while Billing returned success. Rejecting ambiguous main regions holds correctly.
>
> Exact R1/R2 reproductions remain fixed. All **83 BF tests and 11 syntax checks passed**, alongside the independent boundary probes.
>
> **Root P: no findings in the narrow derivation.** Verified exactly one changed assertion, unchanged test bytes, six passing utility tests and two passing syntax checks; harness main was never run.
>
> Final SOURCE-PINS matched:
>
> - BF: `58b10d6c38a827c971169da52c205719c4d84315b3e5d9dbfa4aab5768bc6a6b`
> - P: `13d663bb349ad390d8776926ccd8f922ca7bf634a74b1e4b2eec31db312f5559`
>
> Full fragment/executable pins, line references, reproductions, provenance and limits: Markdown report (local reference: `REVIEW.md`) · matching offline HTML (local reference: `REVIEW.offline.html`).
>
> Sources remained unchanged; moving Nav was untouched. These source-only verdicts grant no runtime adoption, readiness or provider acceptance.

## T083: [Review paired native arm and Finish driver](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-native-paired-driver-review-20261022-r1)

Created 2026-10-22T21:58:45.167Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. Independent adversarial SOURCE-ONLY first review of D=B/chrome-native-paired-driver-source. B=<approval-bank>. Exact D SOURCE-PINSfb60129f49e8b2dd95359a0b3dcafa911f79915a5f35f0bdcfb581169e78603f. Purpose missingconcrete2secdeliveryseam forrealissue158ownerCreateKey; NOTruntimeauthority. ReadfullHANDOFF+allruntime/tests/provenance. Thisnewdriverexplicitonephasearm/finishusesactualunchangedNpreflight sameconfiguredmodel/claim/state/intent/burnedticket/Hready/physicaljournal/boundedreadonlySQL, derivesexactNstageNodechild requestthenfilteredenvone Popen adjacentclockgate. Inputstandalonestatusoriginalunchanged/authenticatedthroughcurrentstreamSQL. ActualparentnativecarrierisPythonDRIVER; internalNode  [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `c9d9a63b5dfeda8403c3796d6afbf5680b54b282b2825f805b0ff2ba88215421`):

> **Qualified no findings.** I found no confirmed source defect in the reviewed D checkpoint.
>
> Verified pins:
>
> - D: `fb60129f49e8b2dd95359a0b3dcafa911f79915a5f35f0bdcfb581169e78603f`
> - N: `2ba7838bacc288d6e0d9a4ebd8cafef2a6fb0ef47822947907fc14c209acfc9a`
> - N registry: `950bf5fc6320577205d4fab96ca50a401a5a26158f34bb3ed78fb3ca85a05e8f`
>
> All 24 D deliverables and 275 dependency entries matched before and after testing. DERIVATION, fixture copies, provenance and complete Markdown/offline-HTML parity checked successfully.
>
> Validation passed:
>
> - All 13 author tests, including both mutants.
> - 19 independent semantic, framing, timing, UUID and sequence refusals, including responses with recomputed valid digests.
> - 14 independent arm/Finish parent-custody checks.
> - Synthetic `n_runtime.prepare()` integration using real N/G checks, disposable H records and in-memory SQL.
> - Exact 1 MB child output, EOF/nonzero exit, DEVNULL stdin, frozen launch environment, real SIGTERM/SIGINT cleanup and reaping, and installed macOS pipe flags.
> - Python AST and pinned Node 26.7.0 public compiler/digest/syntax checks.
>
> Production-only boundaries in the preparation test were explicitly doubled. No production loader, actual native DB, actual Node client, browser, provider, network or Git operation ran. No source/report/log changes or temporary residue remain.
>
> This establishes source-review evidence only. Root adoption, actual probe/wizard compatibility, owner/control authentication, composite freeze and remaining cleanup connections remain open.

## T084: [Review native setup-policy version reader](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-setup-version-preview-review-20261023-r1)

Created 2026-10-22T22:10:15.208Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. Independent adversarial SOURCE-ONLY first review of V=B/chrome-setup-version-preview-source. B=<approval-bank>. V SOURCE-PINS2b63ee700b51155f700a9f22ff54d1c7eb4162e0646ae08391bf65112f0987c1, fragmentda684de9f40806a21120afaec104303a3e6dedbf0a78242bf240db8b5ddcb9a5. Goalonlyprebindnativeownedsetup GetPolicyVersion v1-v5 UIpreview; notpolicywrites/cleanup orreadiness. ReadfullREPORT/runtime/tests/derivation. ActualcapturedpublicIAMsourcesprove Version N cells/native row-scoped[data-testid=expand] BUTTON andariaExpanded/publicpreview-row/readhandlers. Operatoroneexplicitversion, nativeclickonlynoSDK/fetch/customprobe; Gwholepolicyobserver alreadyinstalled separatefreshdoc, expectedmanifestbeforeDelete  [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `25e225461246f004101a16d7ed1065a19e7b5ce152afed23edbdea96f721ff19`):

> Changes requested. Three confirmed source findings:
>
> 1. **[P2] Click guards allow editor/form ancestors and conflicting roles.** public-dom.cjs:51 (local reference: `<approval-bank>/chrome-setup-version-preview-source/public-dom.cjs:51`) checks visibility and disabled state but omits the text walker’s protected-ancestor checks. Making only the button’s wrapper contenteditable, a FORM, or `role="textbox"`, or giving the button `role="checkbox"`, still produced one click, one synthetic native request, and success through unchanged G. Reject these controls before dispatch.
>
> 2. **[P2] The two-second deadline starts after the effect.** page-operation.cjs:50 (local reference: `<approval-bank>/chrome-setup-version-preview-source/page-operation.cjs:50`) clicks and runs another guard before calculating `Date.now()+2000`. Advancing the synthetic clock 2,500 ms inside the click returned success at 2,700 ms. Capture the deadline immediately before dispatch and enforce it after the click, throughout polling, and at completion.
>
> 3. **[P2] Final completion is not bound to the checked document.** operator.js.txt:35 (local reference: `<approval-bank>/chrome-setup-version-preview-source/operator.js.txt:35`) checks CDP identity, detaches, then evaluates completion on the current document. Switching that final evaluation to another VM document with its own actual G observer, identical manifest/window, and a different loader returned success with the original loader in `after`. Bind final validation to the original document/execution context and add this regression.
>
> Verification completed:
>
> - All **38 author tests passed** in disposable storage; 17 additional synthetic probes exercised controls, timing, document switching, and v1-v5 no-ops.
> - Regeneration matched the pinned script and fragment exactly.
> - All 21 V files and 11 named dependencies matched before and after; dependency copies and full Markdown/offline-HTML parity matched.
> - Tested SOURCE-PINS: `2b63ee700b51155f700a9f22ff54d1c7eb4162e0646ae08391bf65112f0987c1`
> - Tested fragment: `da684de9f40806a21120afaec104303a3e6dedbf0a78242bf240db8b5ddcb9a5`
>
> The 16,072-byte value is hard-coded as `navigationRegistryBytes`, associated with navigation provenance. Native-create N’s size, final composite fit, and actual closed-owner compiler compatibility were not established by the permitted dependency set.
>
> The U-to-A boundary return path remains source-compatible. Native compatibility, owner control, composition/adoption, readiness, and cleanup remain unverified Root gates. V and its parents were unchanged; disposable outputs were removed.
