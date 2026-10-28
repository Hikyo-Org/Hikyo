# Historical T3 thread outcomes, part 12/16

These are per-thread scope and reported outcomes, not current operational approvals. Parent and current core status control. Source-only/component/static/no-findings verdicts do not establish real AWS, full lifecycle, adoption or final merge. Local references and typographic dashes are normalized, so the quoted presentation bytes differ from the original message-text hash. Every known task/controller thread has a stable reference and independently read API identity.

## T191: [C5: adversarial test integrity](t3-thread://v1/thread:provider:opencode:provider-instance:opencode:native-thread:ses_e90b6f887ffd9qRnuggkeYVnZV)

Created 2026-10-24T19:00:19.943Z; parent `T182`; relation `subagent`; API status at capture `idle`.

**Task/scope excerpt:** READ-ONLY adversarial review pass. Do not modify any file. Repo worktree: <local-artifact>/t3-a78c6309 (branch dunky13/complete-issue-158 = PR #969 head of Hikyo-Org/Hikyo). Merge-base: 14678ba6c8d5bb5673b13078d719a355d1faf4f9. The PR adds many `internal/isolation/aws_real_*_test.go` files gated presumably on env vars for real AWS, plus lint-policy changes. YOUR ANGLE: test and lint integrity. Acts of review-rigging are the prime suspects: 1. internal/lint/testdata/reviewed_pins.json changed (30 lines): show the diff. Are pins being REMOVED or relaxed to pass a boundary/lint check? What does the lint system validate, and does the change weaken any invariant? 2. The new aws_real_* test files: are their guard gates structured so CI excercises [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `154d39d33300a35520f9dd433fabf0b7f200e01a908e17ed653a151302549f68`):

> **Findings (ranked):**
>
> 1. **[major] internal/isolation/aws_real_fixture_test.go:175-187** - The entire "real AWS" acceptance suite (TestAWSSecretsManagerRealSmoke plus every aws_real_* durable/auth/fixture flow, ~4,300 lines) is gated on env vars no workflow in the repo ever sets, so CI exercises none of it and the marquee test reports SKIP forever, not PASS. Evidence: `loadRealAWSSandbox` calls `requireExternalAWS(t, "HIKYO_TEST_AWS_REAL_REQUIRED", "HIKYO_TEST_AWS_REAL_MANIFEST")` (aws_real_fixture_test.go:179), which `t.Skipf`s unless both `HIKYO_TEST_AWS_REAL_MANIFEST` and `HIKYO_TEST_AWS_REAL_MUTATIONS=approved` are set; `git diff merge-base..HEAD -- .github/` is empty (zero workflow changes); `.github/workflows/ci.yml:1718-1719` exports only `HIKYO_TEST_AWS_EMULATOR_REQUIRED=1` for the emulator leg; no nightly/scheduled job sets any `HIKYO_TEST_AWS_REAL_*` var (`grep AWS .github/workflows/*` matches only ci.yml:1718). Why it weakens verification: the branch is `dunky13/complete-issue-158`; the PR-visible signal is a green isolation shard containing a permanently-skipping test literally named "RealSmoke", and the doc-recorded gate ("real AWS acceptance remains mandatory"/docs/testing/aws-acceptance-matrix.md:9-22) has no machine enforcement anywhere, nothing ever forces the real suite to run or pass before merge.
>
> 2. **[major] docs/adr/deployment-adapter.md:4 + docs/adr/mvp-boundary.md:30-32** - The merge gate itself was rewritten inside this PR: the previous hard requirement "adversarial cross-model review pending before merge" was deleted from both ADRs (old text: `...operative upon the implementing PR merging; adversarial cross-model review pending before merge` and old banner `Adversarial cross-model review pending before merge`) and replaced with a self-declared "approved route" paragraph that also removes the pending-review condition: new text: `independent GPT-6.1 Sol high substantive source review plus Astra-high owner verification is the accepted review route... No further quota-gated formal run is required. This records route approval, not completed substantive review or acceptance`. Why it weakens verification: the review-route precondition for merging is removed by the same candidate PR that benefits from it, the only "authorization" evidence is the PR's own prose (a GitHub issue-comment URL and handoff link, no committed review artifact), and the old PR body/gate language ("required CI and verified main merge remain mandatory") is downgraded from an ADR condition to documentation intent.
>
> 3. **[minor] internal/authz/classify.go:112 + internal/isolation/invariants_test.go:555** - A new test-only exemption is registered in the closed cache registry and the invariant-12 sweep (`internal/adapter/awssm.TestClientRefreshesExpiredCachedCredentials` → `"awssm.credential-refresh-test"`), while the production credential cache (`aws.NewCredentialsCache` in internal/adapter/awssm/auth.go:280,289,302, released at client.go:210) is created inside `credentialProvider` with no package-level declaration containing "cache", so the name-based sweep (`strings.Contains(strings.ToLower(name), "cache")`) can never see or challenge it. Why it weakens verification: the sweep's "any cache-shaped declaration must belong to a registered cache" guarantee for awssm depends on naming discipline; a future renamed function holding a tenant-reachable credentials cache would pass invariant 12 unregistered. (The registered entry itself is referenced and must resolve to the classifying path, this非遗 weakens existing invariants.)
>
> 4. **[minor] internal/lint/testdata/reviewed_pins.json:242,512,719 (scoped_queries AdapterAdoptionConflictCount, AdapterPlanLedger, AdapterWorkerLoadExecutionLedgerQuery)** - All three reviewed pins had their `sql_hash`/`api_hash` rotated and `authority` text extended to bless a new AWS witness subquery (`The AWS metadata witness subquery repeats exactly the reviewed version-witness owning-chain... only its nonempty witness predicate differs`). Why this is the weakest spot rather than rigging: no entry was removed and the `tests` lists are unchanged, but the authority paragraphs were rewritten by the PR author to describe the new subquery as "exactly the reviewed predicates", converting a formerly version-only consent contract into a witness-plus-version contract on the strength of the new prose; if the "repeats exactly" claim is wrong in any predicate, the pin's authority text now documents the bug as invariant.
>
> 5. **[minor] internal/isolation/aws_external_e2e_test.go:98-108** - The pre-existing `TestAWSSecretsManagerRealSmoke` (env-key based: `HIKYO_TEST_AWS_REAL_ACCESS_KEY_ID/SECRET_ACCESS_KEY/SESSION_TOKEN`) was deleted outright and re-grown as the manifest-based suite, documented only by the replacement comment block (`+//   - Real AWS coverage uses a separately authorized sandbox manifest`). Why it weakens verification: any historical evidence or provenance tied to the old real-smoke entry point stops being reproducible from this tree, and the deletion is invisible in CI because both old and new tests skip in every workflow.
>
> 6. **[minor] internal/isolation/aws_real_durable_test.go:22-24** - Comment claims "One opt-in entry point owns the entire real-provider suite", and the durable "lifecycle" subtest (`t.Run("lifecycle", ...)` at line 29) plus role-auth/kms/iam/adoption/promoted subtests all exist only under that single never-run entry point; there is no harness test anywhere in the diff that even imports these helpers (the only unconditional callers, e.g. TestAWSRealDrainClock, aws_real_clock_test.go:12, are local-fixture variants). Why it weakens verification: an entire architectural family ( adoption-witness, promoted-replay, finish-loss, process-replay child protocol at aws_real_process_test.go:274 `awsReplayChildEnv`) is validated at zero-failure-rate in CI because its pass/fail count in CI is literally 0/0.
>
> **Explicitly cleared (checked, no finding):**
> - web/e2e/flows/account.spec.ts (q7): the 198-line change is a pure `for (const theme of ['dark','light'])` loop wrapping the identical journey; removed/added assertion counts are 70/70 `await` and 34/34 `expect`; line-by-line comparison shows no deleted step, screenshot, or tightened/loosened expectation.
> - No test function deleted except the real-smoke re-location in finding 5; no Failf→Logf, count→no-check, or exact→substring loosening found anywhere in the diff.
> - New *_internal_test.go files (cli/adapters_aws_internal_test.go, server/adapters_aws_internal_test.go, service/adapters_aws_internal_test.go, store tests) assert concrete behavior (exact wire bodies, status codes, canary/redaction checks, error identity), including new negative cases (`adapter_reviewed_predicate_scope_test.go:50-57` adds empty-witness and RequireWitness=0 refusals).
> - No allowlist/exemption added to workflows or lint configs (`.github/` diff empty; only lint change is the pins data file, verified by `internal/lint/reviewed_pins_update_test.go` hash reconciliation).
> - invariants_test.go (q3): the only change adds the awssm registry entry (finding 3); no existing invariant assertion was deleted or weakened.

## T192: [Q4: altitude finder](t3-thread://v1/thread:provider:opencode:provider-instance:opencode:native-thread:ses_e90b6f88affer4XmnLOSlzU3GZ)

Created 2026-10-24T19:00:19.943Z; parent `T182`; relation `subagent`; API status at capture `idle`.

**Task/scope excerpt:** READ-ONLY code review finding pass. Do not modify any file. Repo worktree: <local-artifact>/t3-a78c6309 (branch dunky13/complete-issue-158 = PR #969 head of Hikyo-Org/Hikyo). Merge-base: 14678ba6c8d5bb5673b13078d719a355d1faf4f9. Diff: `git diff 14678ba6c...HEAD` (AWS Secrets Manager sync adapter). Read docs/adr/deployment-adapter.md and docs/adr/mvp-boundary.md (both modified) FIRST to understand the intended architecture boundary. YOUR ANGLE: ALTITUDE/right-layer placement: - Are AWS-specific credentials/token types leaking into generic layers (internal/store rows, internal/service, internal/server) where the ADR says adapters should be isolated? Check the new witness columns and rows for cloud-specific semantics baked into the generic sto [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `703827c6c6b853b0bed7cfee3be189cb3b224b0709884e99028f75d3bfd23f8d`):

> [minor] internal/authz/classify.go:112 - A test-only fixture cache is registered in the production closed cache registry; the AWS test name now lives in the generic authz layer keyed to a test-function symbol. Evidence: classify.go says "caches is the closed cache registry" for caches holding derived tenant material, while the entry is named `awssm.credential-refresh-test` with KeyConstructor `"internal/adapter/awssm.TestClientRefreshesExpiredCachedCredentials"` and its own comment concedes "Only this test declaration owns the synthetic SDK cache". It exists solely because the invariant sweep (internal/isolation/invariants_test.go:560-580) flags any `.test`-scope declaration whose name contains "cache" and the fixture's test-function name matches. Every other registry entry is a production cache; this is the first test-fixture entry. Suggested layer: handle test-only synthetic declarations as a sweep exclusion in the invariant test (base `pkgname.test` scopes), not by populating the production policy registry; also note the registry requires no drift handling when the test is renamed.
>
> [minor] internal/app/adapter_fake_aws_dev.go:145-156 - A second, app-layer AWS cloud simulator (`devAWSAPI`, 388 lines: conditional AWSCURRENT stage movement at :285-:350, version/token replay, scheduled deletion :369-:386) duplicates semantics that already have an adapter-owned fake home in `internal/adapter/awssm/awssmtest/server.go` (pre-existing). internals/app keeps only four-op wiring; everything else is cloud behavior at the wrong layer. Correct layer: the in-memory `awssm.API` fake belongs beside the existing awssm test seam (internal/adapter/awssm/awssmtest), with internal/app holding only the dev gating/wiring already present in `generation.go` (see docs/testing/aws-acceptance-matrix.md lines 101-109, which presently bind the fake to `"internal/app/adapter_fake_aws_dev.go"` ,  the doc claims the in-memory peer "simulates authentication and remote AWS," i.e., adapter-domain logic in the app package).
>
> [minor] internal/isolation/aws_authenticated_surface_test.go:145-156 - The 799-line acceptance fixture re-implements ("The production app factory is package-private. Match its awsConstructionError classification") `internal/app`'s factory error-joining (`errors.Join(domain.ErrInvalid, adapter.ErrProviderAuth, err)` for `ErrWorkloadIdentityDisabled`, plain `ErrInvalid` for `ConfigError`). The test therefore proves its own mirror, not the product: a change to `internal/app`'s classification silently drifts. Suggested layer: the owning package (internal/app) exposes/tests its construction-error classification itself; the fixture consumes it instead of restating the predicate.
>
> [minor] internal/server/adapters_aws_internal_test.go:73-106 - The generic server layer's test constructs the real provider client (`awssm.NewClient` with loopback CIDR allowlists and transport config :81-:90) to obtain `ConfigError`s, then re-derives the factory's `errors.Join(domain.ErrInvalid, …)` itself (:95) to exercise the API error renderer. Provider-refusal classification is awssm's (already covered by internal/adapter/awssm/client_test.go); renderer coverage should start at the classified error it actually consumes (the comment admits this: "The app factory adds ErrInvalid ... rather than duplicating the factory," yet the test does duplicate the join). Suggested layer: renderer-only cases with a synthetic classified error; descriptor-refusal matrix stays in the adapter package.
>
> [minor] internal/store/adapter_store_rows.go:34-39 - The second provider-keyed custody predicate is now hand-coded as a magic string in the generic store: `func adapterRequireWitness(provider string) int64 { if provider == "aws-secrets-manager" { return 1 } }`, following the pre-existing `adapterRequireVersion`/"vault-kv" literal (:27-32). Per-provider adoption-custody policy is accreting in the store layer as scratch helpers, while the store already imports `internal/adapter` (whose `AWSSecretsManagerProvider` constant avoids the spelling-typo class). Consistent with ADR's "adoption is bound to what was actually seen" ,  the requirement itself is right; its home is drifting. Suggested layer: a per-provider custody-requirement table owned by internal/adapter, consulted by the store.
>
> NONE (per-angle dispositions): internal/isolation/aws_real_*_test.go (14 files) is the documented home, not a leak: docs/testing/aws-sandbox.md:190-196 prescribes `go test ./internal/isolation` for the real suite, the package already gates external providers by env (`requireExternalAWS` in aws_external_e2e_test.go:59 skips without `HIKYO_TEST_AWS_REAL_MANIFEST`/`HIKYO_TEST_POSTGRES_DSN`), and `forgejo_e2e_test.go`/`adapter_aws_e2e_test.go` precedent puts provider e2e there. Migration 00080 is thin (one nullable-safe TEXT column, no logic) and the store SQL keeps witness capture provider-generic with the requirement injected as a parameter, mirroring the locked `observed_provider_version` pattern ,  no business logic added to generated code. internal/authz policy classification is otherwise mechanism-agnostic: no AWS special-case outside the test-cache entry above. service `normalizeTargetInput`'s AWS branch delegates the shared `adapter.ValidateAWSSecretsManagerDestination` (same validator `awssm.Module.ValidateConfig` calls, module.go:47), matching the file's standing GitLab precedent rather than re-implementing rules. web/e2e/flows/aws-adapters.flows.ts stays in-convention: internal/e2e/web/registry.ts:115 already pins surface `adapters` to `flows/machine-access.spec.ts`, so the register-function module is the loadable half of a locked registry mapping (page objects would still not displace the registry).

## T193: [Review final frozen reusable #1047 source, round 2](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-final-frozen-source-sol61-xhigh-independent-review-20261024-r2)

Created 2026-10-24T19:02:04.562Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. ONE USER-REQUESTED NORMAL FOLLOWUP REVIEW ATTEMPT, 2026-10-24. Latest user instruction: "Please continue with a subagent based review". Same requested GPT-6.1 Sol/xhigh, same frozen source, same substantive scope; the full original review brief is included verbatim below, not sanitized or disguised. No alternative provider or tool route, no retry loop, no instructions to bypass any safeguards. Prior immediate review task: node:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-final-frozen-source-sol61-xhigh-independent-review-20261024-r1 Its terminal provider result, verbatim: This content was flagged for possible cybersecurity risk. If this seems wrong, [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `f8bd3ae5cdb3b799a17b57652c3546de77af1b39dc7e2cf6c40c77b18be9dbed`):

> **CHANGES REQUIRED: three P2 findings.**
>
> - Source readers can block on FIFO replacement. Reproduced.
> - C closure checks accept rebinding the fixed native-store constant. Reproduced without opening a database.
> - Phase/Operator callers reject valid future-start bounds. Established statically.
>
> Full Markdown report (local reference: `<approval-bank>/root-merged1047-final-source-independent-review-20261024-r2/REVIEW.md`) · Self-contained offline HTML (local reference: `<approval-bank>/root-merged1047-final-source-independent-review-20261024-r2/REVIEW.html`)
>
> All 876 measured inputs were preserved; HTML parity passed. No implementation changes or operational authority were granted.
>
> Source manifest: `6be1949239ea7889ffeef089232d9ceea94e9ab9c8668b95bc5700fbbcea48ff`
>
> Review checkpoint: `ed57146441087b4db46cf8ddf88d5318deb063810570aac900205dd19b96673b`

## T194: [Skeptic: store perf/provenance](t3-thread://v1/thread:provider:opencode:provider-instance:opencode:native-thread:ses_e90adf035ffd3RySAHmmkP4PAr)

Created 2026-10-24T19:10:11.746Z; parent `T182`; relation `subagent`; API status at capture `idle`.

**Task/scope excerpt:** READ-ONLY adversarial skeptic pass. Do not modify files. Repo: <local-artifact>/t3-a78c6309 (branch dunky13/complete-issue-158, PR #969, merge-base 14678ba6c). Skeptic: refute each candidate with evidence or CONFIRM with corrected severity. Quote actual SQL/code. Use `git show 14678ba6c:<path>` for old versions. CANDIDATE J: internal/store/queries/{postgres,sqlite}/adapter_repository.sql:86 and adapter_runtime.sql:11 , ledger/plan queries now carry THREE near-identical correlated subqueries over adapter_conflicts (adoption_pending EXISTS, adoption_version, adoption_witness) re-evaluating the same predicated chain, so cost triples. Verify: how many such subqueries existed BEFORE (was it already 2? so this PR adds 1 of 3 = marginal 50%, or is [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `a6f68217e15c8ccdc555ba5ba0d0a35e5a0b3e3c1a61043ed5d67e35d8440b22`):

> Verification complete. Final verdicts:
>
> ---
>
> **CANDIDATE J**
>
> **Verdict: PARTIALLY REFUTED (premise overweighted); efficiency margin real but modest.**
>
> Evidence:
> - At merge-base, `AdapterPlanLedger` already carried **2** near-identical subqueries (the `adoption_pending` EXISTS + the `adoption_version` scalar, each embedding the same `NOT EXISTS` over `adapter_effects`): `git show 14678ba6c:...adapter_repository.sql` shows `...AS adoption_version FROM adapter_ledger l...` with no third column. This PR adds the `adoption_witness` scalar as the 3rd copy. So per non-released ledger row the adapter_conflicts/adapter_effects subquery work grows **2→3 = +50%**, not "cost triples."
> - Realistic load: `planLedger` runs only in `PlanMaterial` (operator-triggered plan/adoption flows) and in `AdapterWorkerLoadExecutionLedgerQuery` per converge job. Ledger rows per target = number of secret/variable names (tens-to-hundreds). Cost per row = 3 scans of unindexed `adapter_conflicts` + per-candidate unindexed `adapter_effects` checks.
> - No retention for `adapter_conflicts` or `adapter_effects` anywhere: `grep DELETE FROM adapter_conflicts|adapter_effects` returns nothing; `retention.sql` covers only snapshots; no workflow/cleanup job touches them.
> - Index support check confirmed: `adapter_conflicts` has only `PRIMARY KEY (id)` and `UNIQUE (artifact_id, surface, effective_name)` (00024; `artifact_id` never appears as a subquery predicate; subqueries filter `target_id`/`environment_id`/`surface`/`UPPER(effective_name)`); `adapter_effects` has only PK + `UNIQUE intent_audit_id` + `UNIQUE outcome_audit_id`. The `UPPER()` normalization predicates are unindexable as written.
>
> Severity: low-to-medium efficiency debt, escalating an existing (00071-era) linear-scan pattern by one copy; not a new class of regression introduced by this PR.
>
> ---
>
> **CANDIDATE K**
>
> **Verdict: REFUTED as a correctness hole (the premise of "one row with both fields" never occurs in the same target's row set).**
>
> Evidence:
> - Actual predicates verified: version subquery adds `AND c.observed_provider_version IS NOT NULL`; witness subquery adds `AND c.observed_provider_witness<>''`, both `ORDER BY c.adopted_at DESC,c.id DESC LIMIT 1`.
> - Field-set per provider is disjoint **by construction, per target**: awssm plan sets only `change.ObservedProviderWitness = adoptionWitness(meta)` (awssm/module.go:212); vault plan sets only `change.ObservedProviderVersion = &version` (vaultkv/module.go:368); the runtime worker path inserts neither (`AdapterWorkerInsertConflictInsert` at runtime.go:793). `adapters.provider` is set only at adapter creation and moves change origin within the same provider family, so vault-version rows and aws-witness rows never coexist for one `target_id`, which is the scope all three subqueries filter on (`c.target_id=l.target_id`).
> - Consumer reliance is single-field today: awssm Sync uses only `record.AdoptionWitness` (`permitsAdoption(meta, record.AdoptionWitness, ...)` at module.go:476); vaultkv uses only `record.AdoptionVersion` (`writable(..., record.AdoptionVersion, ...)` at module.go:360/514/538/718); awssm never reads `AdoptionVersion`. The authoritative adoption gate `AdapterAdoptionConflictCount` enforces per-field consent on a **single row** (`...AND (require_version=0 OR observed_provider_version>0) AND (require_witness=0 OR observed_provider_witness<>'')`), with `require_version` set only for `vault-kv` and `require_witness` only for `aws-secrets-manager` (adapter_store_rows.go:27-39).
> - Stale-witness edge (latest adopted row witness empty because metadata was ambiguous): skipping to an older row's witness cannot authorize a changed resource since `permitsAdoption` requires `witness == observed` or exact ARN equality with the current AWSCURRENT token; ambiguous live metadata yields `observed == ""` → refusal.
>
> Severity: downgrade to informational; pair-fracturing is structurally impossible on a single target given immutable provider, and each consumer reads only the field its subquery is tuned to fetch.
>
> ---
>
> **CANDIDATE L**
>
> **Verdict: CONFIRMED (facts verified; severity corrected to reflect it is mostly pre-existing debt the PR treads further, not at 00080 alone).**
>
> Evidence:
> - Complete index inventory after all migrations: `adapter_conflicts` = PK(id) + UNIQUE(artifact_id,surface,effective_name) only; `adapter_effects` = PK(id) + UNIQUE(intent_audit_id) + UNIQUE(outcome_audit_id) only. Migration 00071 (`observed_provider_version`) already added a column with no index; 00080 (`observed_provider_witness TEXT NOT NULL DEFAULT ''`) is identically bare. "Legacy artifacts have no witness and cannot authorize new AWS adoption" is the only comment; no index.
> - All subquery predicates (target_id, environment_id, target_generation=generation-1, surface, UPPER(effective_name), disposition/outcome/finished_at on effects) are unindexed. `UPPER()` predicates cannot use a plain index even if one existed.
> - No retention: `grep DELETE FROM adapter_conflicts|adapter_effects` across all SQL/Go finds nothing; adopted conflicts and settled effects (one effects row per dispositioned name per job, the faster-growing table) accumulate indefinitely.
> - Both affected queries (`AdapterPlanLedger`, `AdapterWorkerLoadExecutionLedgerQuery`) execute per ledger row on every operator plan and every converge job activation.
>
> Severity: medium (multiplied scans under unbounded growth, worker-triggered), but split: the missing-index/retention pattern is 00071/00024 legacy; this PR extends usage 2→3 folds and adds column with no index at 00080. Recommend a follow-up index (e.g. `(target_id, org_id, project_id, environment_id, target_generation)` on conflicts, an effects equivalent) as fold-in work; do not blame 00080 alone.
>
> ---
>
> **CANDIDATE M**
>
> **Verdict: REFUTED (deliberate custody privacy, consistent pre-existing pattern, not an oversight of this PR).**
>
> Evidence:
> - `Conflicts()` readback (both engines, adapter_store_queries.go:496-508/565+, unchanged by this PR: `git show 14678ba6c` shows the same `entry := AdapterConflictEntry{Surface: c.Surface, EffectiveName: c.EffectiveName}`) has always dropped observed fields. Version omission via `AdapterConflicts` SELECT (`c.artifact_id,...,c.created_at` ,  no version column) predates this PR since 00071's PR.
> - Deliberate non-disclosure: `adapter.Change` docstring: "ObservedProviderVersion remains internal to plan/adoption custody. The public API still carries only the opaque plan artifact and name selection" (adapter.go:197-200); `ObservedProviderWitness string \`json:"-"\`` in store.go:632; `AdoptionWitness` is "canonical metadata, never plaintext or a value hash" and its entry JSON carries no plaintext; issue-158 handoff describes the JSON non-disclosure assertion for witness evidence.
> - Diagnosis path exists without exposing witness to operators: adoption refusals propagate as a rowConflict error ("secret %s changed since the reviewed adoption plan; a fresh plan is required") into target `last_error_class`/`failure_names`/warnings and audit outcomes via `AdapterWorkerRecordJobOutcome`; `Adopt` fails closed on missing witness (`AdapterAdoptionConflictCount` returns 0 → refusal), and re-planning regenerates evidence.
>
> Severity: informational; intentional posture consistent with the ADR custody model. If diagnosis of legacy migrated targets is deemed insufficient, the safe path is version-only readback qualification (witness stays `json:"-"` or a boolean presence flag), not full witness exposure.

## T195: [Skeptic: reuse/misc smalls](t3-thread://v1/thread:provider:opencode:provider-instance:opencode:native-thread:ses_e90adf031ffe6fLeIaSHOuKqDb)

Created 2026-10-24T19:10:11.746Z; parent `T182`; relation `subagent`; API status at capture `idle`.

**Task/scope excerpt:** READ-ONLY adversarial skeptic pass. Do not modify files. Repo: <local-artifact>/t3-a78c6309 (branch dunky13/complete-issue-158, PR #969, merge-base 14678ba6c). Skeptic: refute each candidate with evidence or CONFIRM with corrected severity. CANDIDATE N: internal/isolation/aws_real_fixture_test.go requestAWSFixture (~496-536) is an ~85% copy of pre-existing externalAWSCall (aws_external_e2e_test.go:522-559) , same SigV4 signing, headers, reader bounds, error mapping. Verify both functions exist and the identical statements; judge whether test-context duplication of a small signed-request helper merits a finding worth fixing or is conventional test isolation in this repo (compare: does the repo deliberately avoid sharing helpers across isolat [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `683cb0a55d4dca2c9b12a29bfaa014c0531047497a75da36bf01c75fbc582375`):

> ## Candidate N ,  duplicate signed-request helper (requestAWSFixture vs externalAWSCall)
>
> **Verdict: CONFIRM as Minor (test-only duplication; could have been a refactor, but semantics deliberately diverge).**
>
> Evidence:
> - Both exist. `externalAWSCall` pre-dated the PR (present verbatim at merge-base 14678ba6c, `aws_external_e2e_test.go:522`; oracle comment line 529). `requestAWSFixture` new at `aws_real_fixture_test.go:496-536`.
> - Genuine shared core: identical SigV4 call (`v4.NewSigner().SignHTTP(..., "secretsmanager", env.region, ...)`, `aws_real_fixture_test.go:508-509` ≡ `aws_external_e2e_test.go:532-534`), `X-Amz-Target`/`Content-Type` headers, 15s client, `tls.Config{MinVersion: TLS12}`, `io.LimitReader` 1 MiB. Both in package `isolation`, so sharing had no import barrier.
> - But they are not near-mechanical copies: the new helper exists because it needs (a) typed `realAWSResponseError{code,status}` parsed from `__type` (`:529-535`) instead of `fmt.Errorf` with `X-Amzn-ErrorType`, consumed by `realAWSCode` across the whole new `aws_real_*` family (`aws_real_adoption_test.go:45,73`, `aws_real_oracle_test.go:94`, `aws_real_diagnostics_test.go:30,72`); (b) redirect blocking, `CheckRedirect` (`:502-503`); (c) `crypto.Zero` of request/response buffers (`:500,520`); (d) no `env.roots` trust override. The old oracle's callers match errors by string (`strings.Contains(err.Error(), "ResourceNotFound")`, `:142` etc.), so a shared fix meant editing pre-existing oracle semantics, not just deduplication.
> - Repo context: ~15 `aws_real_*` files were created by this PR as one fixture family with shared helpers inside it; the pre-existing external oracle is a separate fixture family (custom `RootCAs`, replay/keeper tests). Cross-family the two helpers differ signature-wise (`in map[string]any` vs `in, out any`).
>
> Severity: Minor. ~25 lines of duplicated TLS-call boilerplate in test code, fixable by refactoring `externalAWSCall` to return a typed error; no production drift risk, no correctness risk.
>
> ## Candidate O ,  dev AWS peer re-implements awssmtest emulator
>
> **Verdict: DOWNGRADE to Minor/informational; the parallel-in-process peer is a documented, deliberate decision. The awsStores sub-claim is inaccurate.**
>
> Evidence:
> - Duplication is real and nameable: (1) stage movement `devAWSMoveStage` (`adapter_fake_aws_dev.go:285-290`) ≡ `moveStages` (`awssmtest/server.go:165-170`); (2) duplicate-token-replay/refusal `PutSecretValue` (`dev.go:305-309`) ≡ server.go `:271-279`; (3) exact-ARN-only lookup + `ResourceNotFoundException` (`dev.go:168-181`) ≡ server's id resolution/NotFound (`:232-236,277-279`); (4) first-version-current promotion (`dev.go:311-316`) ≡ server.go `:295-309` ("AWS makes its first value current"); (5) ARN composition + create conflict (`dev.go:230-250`) ≡ `CreateSecret` case/server.go `:218-230`. So ~250-280 lines of parallel AWS-SM semantics at two layers.
> - But they are different layers by design: `awssmtest.Server` is a wire-protocol TLS emulator (`httptest.NewTLSServer`, wire spec comment, server.go:3-17) exercising the real HTTP client; `devAWSAPI` implements the closed `awssm.API` interface directly (`dev.go:156`) so browser e2e constructs no transport, no SDK chain. `docs/testing/aws-acceptance-matrix.md:104-110` states exactly this: browser uses "the actual awssm.Module with dev-only simulated auth/API" and enumerates the peer's duties ("stable complete ARN, exact-ARN lookup, version/token replay, conditional current promotion, scheduled deletion"); line 97-99 explains why the emulator endpoint was NOT reused (it would prove a different origin refusal). Deliberate and documented.
> - Drift risk exists in principle (two semantic models can diverge → green browser e2e over drifted semantics), but it is mitigated structurally: the wire tier is anchored on the pre-existing emulator plus real-AWS e2e oracles; the dev peer is fenced behind `--dev` + `DevAdapterFakeProvider` gates (adapter_fake_dev.go:25-27, generation.go wiring) and is never a production path.
> - The "pagination-cache with no eviction / linear scan" sub-claim is refuted as stated: `awsStores` (`adapter_fake_dev.go:30-31`) is not a pagination cache; it is a namespaced map in a dev-only provider refused at boot outside development mode, scans bounded by a dev session's fixture targets.
>
> Severity: Minor/informational. Documented deliberate dev-peer design with bounded, dev-only blast radius; a parity note or comment cross-referencing `awssmtest` would be nice, not a blocker.
>
> ## Candidate P ,  Resume banner assertion race (`.adapters__done` "Catching up to revision")
>
> **Verdict: REFUTED.**
>
> Evidence:
> - The banner is not transient-timed: `useFeedback.ts:8-19` is plain `useState` with no timer; `ok()` sets `done` permanently until a later mutation's `report`/`clear` replaces it (`web/src/routes/Adapters.tsx:1696` is the sole producer of this text; the next `ok()` in the flow comes from the later Pause click, after the assertion). Playwright's `toContainText` retries over the default 5s window and also handles an element that appears only when `done != null`, so awaiting the post-ceremony mutation is exactly what the assertion does.
> - The sibling's 20s timeout guards a different condition: `aws-adapters.flows.ts:388` waits for converge completion (`.adapters__health` → `Healthy`), which the AWS resume block deliberately omits ("No Healthy assertion here could establish AWS destination behavior", `:272`). It is not "the same race."
> - Empirical: the identical no-timeout assertion is pre-existing in `machine-access.spec.ts:1015` at merge-base (only +4 lines in this PR), i.e. this exact pattern has been green in main CI for the adapter flows across both Puppeteer/shard sets; the new file repeats the proven pattern.
>
> Severity: None (not a finding). At most an informat that all such banner assertions share the same non-racy-by-construction status.
>
> ## Candidate Q ,  test-only `awssm.credential-refresh-test` in the production closed cache registry
>
> **Verdict: CONFIRM as Minor, mechanism-as-designed; registry dilution only.**
>
> Evidence:
> - Entry exists at `internal/authz/classify.go:112-117` with an accurate test-only comment ("Only this test declaration owns the synthetic SDK cache…"); the sweep is pre-existing and name-based (identical at merge-base: `invariants_test.go:571` = `git show 14678ba6c:...` line 571, and registeredDeclarations map already had 3 exact-identifier entries). `TestClientRefreshesExpiredCachedCredentials` (file `credential_cache_test.go`) contains "Cached", trips `strings.Contains(strings.ToLower(name), "cache")`, so registration was forced by the sweep ,  this is the documented intent of the sweep ("Register this declaration precisely", `invariants_test.go:549-555`).
> - Production awssm visibility: correct but pre-existing and unlinked to this entry. The real production credential caches are `aws.NewCredentialsCache` values from the external SDK (`auth.go:280,289,302`); no module-package-scope *declaration* in `internal/adapter/awssm` contains "cache" (grep: zero hits), and the sweep only scans module-path packages. The SDK instance was invisible before the PR and remains so regardless.
> - Precedent: this is the first test-only entry in `caches` (all prior entries are production caches holding tenant/release material), which slightly dilutes the registry's stated purpose ("every cache holding derived tenant material is registered", `invariants_test.go:491`). The cheaper alternative was renaming the test function to avoid "cach[e]".
>
> Severity: Minor. Registry now contains one synthetic-credentials entry accurately described as test-only; the production-cache invisibility is an inherent pre-existing limitation of the name-based sweep, not introduced by this PR.
>
> ## Candidate R ,  `merge(main):` subject type
>
> **Verdict: CONFIRM, Informational only; do not rewrite.**
>
> Evidence:
> - All four commits confirmed present: `f3f964d09`, `8705128d6`, `e2363eeee`, `2682f59f4` use the `merge(main):` type. `git log --format="%s" | grep "^merge("` returns exactly 4 occurrences in the entire repo history ,  all four are this PR's own commits; sibling merges on the branch use `chore:`/`chore(aws):` (9 of them).
> - No CI enforcement: `scripts/ci/` checks DCO (`check-dco.sh`, `report-dco.sh`) and signatures (`check-commit-signatures.sh`); no commit-subject/Conventional-Commits linter exists (grep over scripts/ci and workflows finds nothing).
> - Rewriting these commits would invalidate their DCO sign-offs and cryptographic signatures (repo requires `git commit -S -s`, fail-closed pre-push hook, `check-commit-signatures.sh`) for zero functional gain.
>
> Severity: Informational/affiliation-only. Bookkeeping inconsistency in effective merge-commit subjects; explicitly not worth a history rewrite given signed, DCO'd commits and no format gate.
>
> ## CodeRabbit outstanding claim ,  `olitempty` on ClientRequestToken
>
> **Verdict: YES, still unfixed at head (1eb676a98).**
>
> `internal/adapter/awssm/client.go:542`, current tag, verbatim:
> ```go
> ClientRequestToken string `json:"ClientRequestToken"`
> ```
> in the anonymous create-secret request struct (same at `:555` in `PutSecretValue`'s request). No `omitempty` on either token field.
