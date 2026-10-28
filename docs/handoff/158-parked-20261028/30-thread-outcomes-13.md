# Historical T3 thread outcomes, part 13/16

These are per-thread scope and reported outcomes, not current operational approvals. Parent and current core status control. Source-only/component/static/no-findings verdicts do not establish real AWS, full lifecycle, adoption or final merge. Local references and typographic dashes are normalized, so the quoted presentation bytes differ from the original message-text hash. Every known task/controller thread has a stable reference and independently read API identity.

## T196: [Skeptic: awssm client/module](t3-thread://v1/thread:provider:opencode:provider-instance:opencode:native-thread:ses_e90adf035ffeSYozNc39V923dv)

Created 2026-10-24T19:10:11.746Z; parent `T182`; relation `subagent`; API status at capture `idle`.

**Task/scope excerpt:** READ-ONLY adversarial skeptic pass. Do not modify files. Repo: <local-artifact>/t3-a78c6309 (branch dunky13/complete-issue-158, PR #969, merge-base 14678ba6c). You are a skeptic: try to REFUTE each candidate with concrete evidence from the actual code. REFUTED = evidence shows the claim is wrong or impact is nil. Otherwise CONFIRMED + corrected severity. Use `git show 14678ba6c:<path>` for old versions. CANDIDATE E: internal/adapter/awssm/module.go , Plan now DESCRIBES every conflict-name row; any single Describe error returns the whole plan with error (`return adapter.Plan{}, err`), where pre-change code never inspected conflict rows so a transient describe error could never fail planning; also scales API traffic with conflict count. Verif [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `e64035d8739616ebf5c76caf62cb8eaddac63e0b3a3427c974ee8bb8d182b278`):

> Evidence gathered. Final report:
>
> ---
>
> ## CANDIDATE E: Plan describes conflict rows; describe error aborts whole plan
>
> **Verdict: PARTIALLY CONFIRMED (delta only in unowned-conflict rows), corrected severity LOW/MINOR**
>
> Key framing claim refuted: pre-change Plan already aborted on describe errors. Old code (14678ba6c, module.go lines ~188-205):
> ```go
> if change.Disposition == adapter.Delete || !held || (record.State != adapter.Owned && record.State != adapter.Dispatched) {
>     continue
> }
> ...
> meta, found, err = m.describe(ctx, destination, change.EffectiveName)
> if err != nil {
>     return adapter.Plan{}, err
> }
> ```
> A transient describe failure for any held Owned/Dispatched row already failed planning. The only delta is `change.Disposition != adapter.Conflict && (...)` (new module.go:192), which additionally describes rows PlanChanges marks Conflict because `providerSecrets[row.EffectiveName]` is true while the ledger does not hold them Owned/Dispatched (internal/adapter/ledger.go:105-115), plus sets `ObservedProviderWitness` (module.go:211-213).
>
> - Outbox coupling: none. `adapters.go:1837` calls `lease.Module.Plan` only from interactive `Adapters.Plan`; the outbox worker only invokes `Module.Sync` (internal/adapter/outbox.go:267). A throttled describe (client.go:337-339 maps throttle codes to `adapter.ErrRateLimited`) surfaces to the operator as a failed plan run; re-running the plan is the natural recovery. No job permanently fails.
> - API traffic: one extra DescribeSecret per conflict name per plan invocation, gated per-row by `req.Gate` (module.go:198-200) under `enterProviderBudget` (adapters.go:1799). Bounded and interactive, not in a write loop.
> - Test coverage of describe-error-during-plan: **none**. Only `TestPlanIsValueBlindAndMarksConflicts` (module_test.go:779, happy path witnesses) and `TestHeldOwnershipPlanRechecksAuthorityBeforeMetadata` (adoption_replay_test.go:51, gate error). No test injects a describe error into Plan.
>
> **Severity: minor.** Widened error-abort surface in an interactive, operator-retryable operation; the "never could fail planning before" premise is false, and no outbox job is affected.
>
> ---
>
> ## CANDIDATE F: Promote path 3-attempt retry with fixed sleeps, then ErrConflict
>
> **Verdict: CONFIRMED as a deliberate, tested behavior change (claims partly inaccurate: ~600ms is wrong; it is 300ms sleeps + 3 describes; ErrConflict retries, it does not permanently fail); corrected severity MINOR**
>
> Diff verified (module.go:591-634 new vs old 535-560): old did a single `describeResource`, skipped promotion only when proven-current (`!fresh.put && !fresh.promote`, semantically equiv to token==AWSCURRENT), otherwise fell through to a blind `UpdateSecretVersionStage(awsCurrent, token, "")` with an empty predecessor. New code retries up to 3 attempts with sleeps of `attempt * 100ms` (100ms + 200ms = **300ms total, not ~600ms**), guarded by ctx cancellation, then `ErrConflict: current version is not visible after staging`.
>
> - (a) The trigger is persistent metadata invisibility across three sequential describes spanning >300ms of wall time, not a describe-latency spike; describe latency alone only delays the attempt.
> - (b) Confirmed: `time` is a new import to awssm/module.go; these are the first wall-clock sleeps in the package, fixed (no jitter; the outbox's `w.Jitter`/`retryDue` helpers live in internal/adapter/outbox.go:158-160 and are not used here).
> - (c) ErrConflict does NOT permanently fail the sync job. The sync-path failure list omits ErrConflict (`outbox.go:315`: `ErrProviderAuth || ErrAckForged || ErrOperatorReview` go to Fail; ErrConflict only fails Activate jobs, outbox.go:236). Sync conflicts reach `Store.Retry` (outbox.go:317-319) and self-heal: on retry, the same token's Pending version is by then AWSCURRENT, so `decide` sets `put/promote = false` and the replay adds no second version (that convergence is exactly what `TestSignedWireFirstCurrentMetadataLag` asserts: puts stay at 1).
> - (d) Mitigations exist: the loop is covered by `TestSignedWireFirstCurrentMetadataLag` scenarios (client_test.go:228-237: 1 stale read and 2 stale reads converge, persistent lag → ErrConflict, foreign concurrent version → ErrConflict with foreign value preserved, gate revoked, cancelled), and the old behavior it replaced was the unsafe empty-predecessor blind move the code explicitly refuses now.
>
> **Severity: minor.** Intentional safety change with direct test coverage and outbox retries that converge without re-writing; worst case is a delayed job rather than a lost one.
>
> ---
>
> ## CANDIDATE G: Conflict message embeds provider-controlled `current` unbounded
>
> **Verdict: REFUTED as a PR finding (observation factually true but the code is byte-identical to the merge-base; not introduced or touched by this PR)**
>
> Current module.go:435-436:
> ```go
> if current != "" && !ours && (...) {
>     return writePlan{conflict: "AWSCURRENT version " + current + " was written outside Hikyo; ..."}
> }
> ```
> Old code at 14678ba6c (module.go:383-384) contains the identical string. The full diff of `decide` between merge-base and HEAD touches nothing in this function (diff hunks only add adoptionWitness/permitsAdoption and modify converge/apply/Plan). The package's defensive bounding elsewhere is real: `errorCode` caps at 64 alphanumerics (client.go:343-356) and `ResponseError.Error()` never formats provider messages (client.go:218-229). `current` is indeed an unvalidated DescribeSecret `VersionIdsToStages` map key, and no test pins its length; it ends up in operator-facing conflict text persisted via `Store.Retry` cause records.
>
> **Severity: none for this PR.** Pre-existing latent hardening opportunity, not a regression; recommend at most a follow-up bound, not a PR blocker.
>
> ---
>
> ## CANDIDATE H: Map-iteration nondeterminism when Stages share labels
>
> **Verdict: REFUTED as a PR finding (code loop is byte-identical to merge-base; only reachable under non-conformant metadata; the new adoptionWitness path already defends ambiguity)**
>
> Current module.go:416-428 loop assigning `current` and `plan.previousHikyo` from `meta.Stages` map iteration is identical to merge-base module.go:364-376 (side-by-side verified; diff shows decide's loop untouched). Notably the PR's new `adoptionWitness` explicitly guards this class of data: `if current != "" || version == "" { return "" // Ambiguous metadata must never authorize adoption }` (module.go:223-229), so the adoption path is deterministic-safe; the write path (`previousHikyo` feeding the conditional `UpdateSecretVersionStage(..., CurrentStage, token, plan.previousHikyo)` at module.go:655) is not, but is unchanged.
>
> Reachability: conformant AWS data cannot produce it (staging-label moves remove the label from all other versions); the dev fake enforces the same invariant, `devAWSMoveStage` strips the stage from every other version before adding it (internal/app/adapter_fake_aws_dev.go:285-290), including for AWSCURRENT. So only directly seeded fixture or corrupted external data triggers it. No test pins determinism of the decide loop under duplicate labels (tested only indirectly via adoptionWitness ambiguity).
>
> **Severity: none for this PR.** Pre-existing latent nondeterminism under non-conformant data only; safe to note for follow-up, not a regression.
>
> ---
>
> ## CANDIDATE I: CreateSecretIdentity duplicates ARN/account validation inline
>
> **Verdict: REFUTED (sites exist as described, but semantics are equivalent; no concrete divergent rule causing an invalid identity accept or a panic was demonstrated)**
>
> All cited sites verified:
> 1. `adapter.awsAccountID` regex `^[0-9]{12}$` (internal/adapter/aws_names.go:28, enforced at :36).
> 2. `verifyARN` (internal/adapter/awssm/module.go:128-140): checks parts[0]=="arn", parts[2]==serviceName, parts[5]=="secret", account, region; does **not** check parts[1].
> 3. `CreateSecretIdentity` (internal/adapter/awssm/client.go:516-531): account check `len(account) != 12 || strings.Trim(account, "0123456789") != ""` is character-for-character equivalent to `^[0-9]{12}$` (remaining non-empty iff any non-digit; middle non-digits survive Trim). It then **reuses** `verifyARN` for ARN structure/account/region, and adds `parts[1] != "aws"`, name equality, and suffix checks `match && len(suffix) == 6 && strings.Trim(suffix, alnum) == ""`, which is set-equivalent to `awsARNSuffix`'s `-[A-Za-z0-9]{6}$` charset. The only rule divergence found is that CreateSecretIdentity is **stricter** (parts[1]=="aws" plus exact Name equality), never looser.
> 4. Dev fake ParseInt (internal/app/adapter_fake_aws_dev.go:55): immediately preceded by `adapter.ValidateAWSSecretsManagerDestination(destination)` (line 53), which applies the same 12-digit regex, so the fake is not laxer.
> 5. Fixture regex `realAWSAccount = ^[0-9]{12}$` (internal/isolation/aws_real_fixture_test.go:61): identical semantics.
>
> Panic concern disbanded: `parts[6]` indexing (client.go:526-527) is safe because `verifyARN` is called first and rejects `len(parts) != 7` (module.go:130), and the ambiguous-suffix name guard (`validateAWSSecretName` refusing names ending `-XXXXXX`, aws_names.go:73-75) is enforced at config/manifest validation, which the fake and all production paths run before module use; even with an ambiguous name, `CutPrefix(parts[6], input.Name+"-")` resolves the exact suffix deterministically. CreateSecretIdentity's only callers are the test fixture (aws_real_fixture_test.go:679) and its own tests; no production caller exists.
>
> **Severity: minor cleanup at most (dedupe into a shared helper per the project's dead-code/duplication taste); speculative drift risk, no live defect.**
>
> ---
>
> ### Summary table
>
> | ID | Verdict | Corrected severity |
> |----|---------|--------------------|
> | E | Partially confirmed (delta confined to unowned conflict rows; outbox premise wrong) | Minor |
> | F | Confirmed as intentional, tested change (figures corrected: 300ms sleeps not 600; retry not permanent fail) | Minor |
> | G | Refuted (byte-identical to merge-base) | None (follow-up hardening) |
> | H | Refuted (byte-identical to merge-base; dev fake enforces label uniqueness) | None (latent) |
> | I | Refuted (duplication real, semantics equivalent, no panic or invalid-accept path) | Minor cleanup |

## T197: [Final sweep: what's missing](t3-thread://v1/thread:provider:opencode:provider-instance:opencode:native-thread:ses_e90adf030ffeIq2qsZiAT6dq1w)

Created 2026-10-24T19:10:11.746Z; parent `T182`; relation `subagent`; API status at capture `idle`.

**Task/scope excerpt:** READ-ONLY final sweep for a max-preset code review. Do not modify files. Repo: <local-artifact>/t3-a78c6309 (branch dunky13/complete-issue-158 = PR #969, merge-base 14678ba6c, diff via `git diff 14678ba6c...HEAD`). The PR implements a production AWS Secrets Manager synchronization adapter (issue #158). Prior finders already covered: store-layer SQL/memory correctness, behavior diffs in awssm client/module, e2e flows, test gates (real-AWS skip), ADR gate rewrite, lint pin rotations, reuse duplication, perf of ledger subqueries, authz registry pollution, commit formats, migration parity, fake-AWS dev adapter, retry/backoff paths, JSON tags, error bounding. YOUR JOB: find what they all MISSED. 8 candidate slots. Systematically probe gaps the a [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `703073c87296be2731da3bfb380e421961e1be0be96bd47de05ca47a55091160`):

> Final sweep complete. Everything from the resume plan is done: the tail of `aws_authenticated_surface_test.go` (560-799) is clean, the "stale 78 doc" candidate collapsed on re-verification (it is preserved chronological history; lines 868-871 and 895-900 of the same handoff doc explicitly record the moves to 79 and 80 with corrected drill order, and the acceptance matrix/top of file already says 80), and the migration-wedge candidate survived full source re-verification down to every hop.
>
> ## Final candidate
>
> **[low→medium] `internal/adapter/awssm/module.go:476` + `internal/store/migrations/{sqlite,postgres}/00080_aws_adoption_witness.sql:3` - Legacy (pre-witness) adopted AWS rows whose interrupted job already landed the `MANAGED_BY_HIKYO` tag are trapped in a permanent convergence refusal, and the prescribed recovery ("a fresh plan") silently cannot engage in exactly that state**
>
> Evidence (full causal chain, each verified in source on this branch):
>
> 1. Trap condition: a legacy `adapter_conflicts` row keeps `AdoptionPending=true` from the store predicate (adopted, no later success write, `target_generation = target.generation-1`) with empty `observed_provider_witness` (`''`).
> 2. `module.go:476-479`: `converge` returns `rowConflict` (`adapter.ErrConflict: ... a fresh plan is required`) whenever `record.AdoptionPending && !permitsAdoption(...)`. `permitsAdoption` (`module.go:246-248`) returns `false` unconditionally when `witness == ""`, so metadata state is irrelevant: the refusal fires every attempt, forever, before `decide` can tag or write - and because no success write ever happens, the store predicate that clears `AdoptionPending` never clears. Permanent loop.
> 3. Recovery is the dead end: the fresh-plan route needs a disposition-`Conflict` change so that `service/adapters.go:1855-1861` records a witness row via `RecordPlan`. But:
>    - `adapter/ledger.go:110-111`: with the ledger record in `Owned`/`Dispatched` (and not `Missing`), `PlanChanges` yields `Update`, not `Conflict`.
>    - The forced-conflict path in `module.go:208-210` only fires when `meta.Tags[SentinelName] != req.Target.ID`. An interrupted `plan.tag` write (`module.go:523`: `tags = {SentinelName: req.Target.ID}`) stores the same target ID, so a landed tag **matches** and the forced conflict does not fire.
>    - Net: fresh `Plan()` returns only Update changes → never records a conflict entry → no witness row ever lands → step 2 repeats. The migration comment ("Legacy artifacts have no witness and cannot authorize new AWS adoption") is honored without exit, and no test covers this combination.
> 4. Recovery that actually works requires provider-side surgery: manually removing/altering the `MANAGED_BY_HIKYO` tag on the secret makes `module.go:208` force a real `Conflict`, a fresh reviewed plan then records a witness, and adoption review recovers. That path (tag removal as the documented escape) is not stated in the migration comment, the handoff doc, or the upgrade drill.
>
> Impact: only hits AWS adapters adopted before the 00080 migration whose adoption job crashed after the tag API call landed but before the success effect, then upgrades. Narrow class, but the failure mode is a confusing self-referential error: the message names the one action ("fresh plan") that provably does not resolve the state in every variant of its own trap. Suggested fix direction (for the author, not applied): either have `Plan()` also force `Conflict` when `AdoptionPending` ledger evidence lacks a witness (even when the tag matches), or document tag removal as the recovery path in the migration comment.
>
> ## Examined and clean (beyond prior coverage)
>
> - `internal/isolation/aws_authenticated_surface_test.go` (both halves now read through line 799): replacement custody verified against sealed ciphertext + `ParseDescriptor`, zero-operation shutdown assertions for all three workload modes, revoke fencing/supersession/generation-fence, pause/resume consent, both retain/prune directions, teardown ledger checks, and secret-holder scanning of audit payloads and remote operations (`GetSecretValue`/`BatchGetSecretValue` refusal).
> - `docs/handoff/158-aws-secrets-manager.md` migration-slot narrative: false positive on first read; chronological history later in the same file self-corrects to slot 80.
> - Fake-AWS dev adapter, real-fixture manifest/budget validator (including NaN/Inf rejection and 0 < budget ≤ 10), docs env-name/name cross-references, SDK citation at v1.47.3 `credential_cache.go:95`, service-layer `Plan`/`RecordPlan` conflict wiring, `insertConflict` dedup path (`adapter_runtime.go:740-819`: dedup guard returns correctly under retries).
>
> No files were modified.

## T198: [Skeptic: docs/gates/integrity](t3-thread://v1/thread:provider:opencode:provider-instance:opencode:native-thread:ses_e90adf036ffeXz0dduLnqIrrq3)

Created 2026-10-24T19:10:11.746Z; parent `T182`; relation `subagent`; API status at capture `idle`.

**Task/scope excerpt:** READ-ONLY adversarial skeptic pass. Do not modify files. Repo: <local-artifact>/t3-a78c6309 (branch dunky13/complete-issue-158, PR #969 of Hikyo-Org/Hikyo, merge-base 14678ba6c). You are a skeptic: for each candidate below, try to REFUTE it with concrete evidence. If the refutation succeeds, say REFUTED with evidence. If it survives, say CONFIRMED with corrected severity and exact citations. Quote the actual current file text and the merge-base version (`git show 14678ba6c:<path>`) where relevant. CANDIDATE A: The docs/adr/deployment-adapter.md and docs/adr/mvp-boundary.md diffs DELETED the "adversarial cross-model review pending before merge" requirement and replaced it with a self-declared approved review route written by the same PR that [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `2f72d4b52cdbd4592d9fdc30413f7d8d58de65a5be5c9723c522748ac71f1c0b`):

> ## CANDIDATE A ,  REFUTED (characterization as self-granted rigging fails; narrow residual note)
>
> **Evidence:**
> - Old text deleted: merge-base `docs/adr/mvp-boundary.md` amendment 8 said "...operative upon the implementing PR merging; adversarial cross-model review pending before merge" and merge-base `docs/adr/deployment-adapter.md` banner: "adversarial cross-model review pending before merge" ,  both removed. That part is true.
> - Authorship: the ADR commits are authored by the owner, not an agent self-granting: `git log` shows `c11a2402f 2026-10-23 Marc Went docs(aws): distinguish controlled revocation coverage`, `c64a4f977 2026-10-22 Marc Went docs(aws): reconcile issue 158 completion evidence`, `0ff27e3d6 2026-10-18 Marc Went fix(aws): persist adoption witness for safe replay`.
> - "No committed review artifact" is false in spirit: `docs/handoff/158-aws-secrets-manager.md` (new text, lines 685-697) records three named review rounds with concrete findings and repairs: "Substantive R1 found an initial-consent race and a fixture deadline bug; R2 found a replay regression in the consent repair. Source fixes and regression drafts are present; R3 independently verified all 38 pinned files and returned SOUND for the scoped source review. Astra independently rechecked the hashes and repair paths. No runtime acceptance is claimed."
> - The new ADR text explicitly does NOT claim completion, exactly as the candidate hypothesizes: "This records route approval, not completed substantive review or acceptance. Real AWS acceptance, required CI and verified main merge remain mandatory." and "This disposition is #158-only". It also preserves the adverse facts in the handoff: "All 15 GitHub review threads are resolved, but the aggregate review still says changes requested; this is not final review" (handoff line 79-80).
> - Repo precedent for owner-granted route changes exists in the same diff context: amendment 9 states "cross-provider review explicitly waived by the owner for this amendment only" (#164).
>
> **Severity:** minor (residual: owner route approval is attested only in-repo prose and cannot be externally verified from repo evidence; but the PR removed no acceptance/CI/merge gate and recorded review activity honestly, so "improper self-granted gate change for a merge-blocking precondition" is not supported).
>
> ## CANDIDATE B ,  REFUTED as stated (deletion true, "no workflow sets the gates" and CI-regression framing false)
>
> **Evidence:**
> - Deletion is real: merge-base smoke was `func TestAWSSecretsManagerRealSmoke(t *testing.T)` gated only on `HIKYO_TEST_AWS_REAL_ACCOUNT/REGION/ACCESS_KEY_ID/SECRET_ACCESS_KEY` (quoted from `git show 14678ba6c:internal/isolation/aws_external_e2e_test.go`), removed in HEAD; replacement gates verified in `internal/isolation/aws_real_fixture_test.go:177-186`: `path := os.Getenv("HIKYO_TEST_AWS_REAL_MANIFEST")` ... `if os.Getenv("HIKYO_TEST_AWS_REAL_MUTATIONS") != "approved"` ... `if os.Getenv("HIKYO_TEST_POSTGRES_DSN") == ""`.
> - "No repo workflow sets any of those" is false: `scripts/ci/aws-acceptance/runner.py:510-513` sets `"HIKYO_TEST_AWS_REAL_REQUIRED": "1", "HIKYO_TEST_AWS_REAL_MUTATIONS": "approved", "HIKYO_TEST_AWS_REAL_MANIFEST": str(projection.custody / "manifest.json"), "HIKYO_TEST_POSTGRES_DSN": "postgres://hikyo:hikyo@127.0.0.1:5432/hikyo_test"`, driven by the pre-existing (present at merge-base: `14678ba6c:.github/workflows/aws-acceptance.yml`) protected-environment, human-reviewed dispatch workflow.
> - The zero-execution fact pre-exists: `git grep HIKYO_TEST_AWS_REAL 14678ba6c -- scripts/ci/` shows no always-on CI wiring for the old smoke either; the smoke skipped (`requireExternalAWS` → `t.Skipf`) whenever env was absent, and nothing always-on ever supplied it. 0 real executions before and after; no CI-coverage regression.
> - Replacement is an explicitly documented deliberate decision: `docs/testing/aws-sandbox.md` head: "The real path never borrows the emulator's combined writer/oracle/cleanup identity" (separation of writer/oracle/cleanup identities is a security upgrade over the old single-credential smoke), with full manifest/scope/budget governance ("Real AWS is opt-in twice: a bounded operator manifest and an explicit mutation ... approval").
> - Old smoke's lifecycle coverage is still runnable always-on: `TestAWSSecretsManagerEmulatorLifecycle` is unchanged and runs lifecycle + concurrent writes + replacement race + adoption replay via the moto emulator (`scripts/ci/start-aws-emulator.sh` sets `HIKYO_TEST_AWS_EMULATOR_REQUIRED=1`); the old smoke called only `runExternalAWSLifecycle`, a subset. Oracle coverage moved to the gated `aws_real_oracle_test.go` per the documented identity-separation rationale.
>
> **Severity:** minor (documented governance tightening of dangerous real-AWS mutations; the always-on emulator legacy lifecycle coverage is retained; residual fact: real-AWS coverage executes only via the manually dispatched protected workflow, not regular CI).
>
> ## CANDIDATE C ,  REFUTED (legitimate rotation; prose factually accurate; nothing removed; tampering mechanically caught)
>
> **Evidence:**
> - Nothing removed: full diff of `internal/lint/testdata/reviewed_pins.json` is 15 insertions/15 deletions = exactly `authority` + 2 `sql_hash` + 2 `api_hash` lines for the three named entries; all `tests` lists are untouched context lines. No entries, tests, or files removed.
> - Mechanical reconciliation exists and would fail on any pin/SQL mismatch: `internal/lint/sqlcontract.go:115-120`: `actual.SQLiteSQLHash != approved.SQLiteSQLHash || ...` → finding "sqlpredicate: query %q differs from its approved exact cross-engine pin". Rotation cannot bless unchanged SQL and unfaked SQL cannot pass with stale pins. The updater is skip-by-default and stamps itself non-authoritative: `internal/lint/reviewed_pins_update_test.go`: "Updating is not proof or approval: normal CI still checks the reviewed diff" (`if !*updateReviewedPins { t.Skip("explicit reviewed-pin update only") }`).
> - Prose claim "repeats exactly the reviewed ... predicates; only its nonempty witness predicate differs" is factually true: in `internal/store/queries/postgres/adapter_repository.sql` (AdapterPlanLedger), the new witness subquery repeats every predicate of the pre-existing `adoption_version` subquery verbatim and swaps only `AND c.observed_provider_version IS NOT NULL` → `AND c.observed_provider_witness<>''`; identical in the sqlite variant (with julianday forms) and in `internal/store/queries/postgres/adapter_runtime.sql` (AdapterWorkerLoadExecutionLedgerQuery). `AdapterAdoptionConflictCount` adds only the symmetric opt-in arm `(CAST(sqlc.arg(require_witness) AS INTEGER)=0 OR observed_provider_witness<>'')` beside the pre-existing `require_version` arm.
> - The rotation accompanies a reviewed, visible SQL change (witness column added to `adapter_conflicts`, migration `00080_aws_adoption_witness.sql`), not a covert bless.
>
> **Severity:** drop (legitimate pin rotation mechanically reconciled against reviewed SQL; no review-rigging evidence).
>
> ## CANDIDATE D ,  REFUTED as an independent finding (merge into B)
>
> **Evidence:**
> - Mechanism verified: `internal/isolation/aws_real_durable_test.go:25` `s := loadRealAWSSandbox(t)` → same three gates as B (`HIKYO_TEST_AWS_REAL_MANIFEST` + `HIKYO_TEST_AWS_REAL_MUTATIONS=approved` + `HIKYO_TEST_POSTGRES_DSN`, fixture lines 177-186, else `t.Skip`; setting `HIKYO_TEST_AWS_REAL_REQUIRED` without the manifest fails closed, line 179-180).
> - Same single root cause as B (gate wiring), and B's branch of the analysis already resolves it: `scripts/ci/aws-acceptance/runner.py:512-513` sets the manifest and DSN for the committed `aws-acceptance.yml` workflow; always-on CI never ran the pre-existing real-AWS smoke either, so "0/0" is not a new regression fact.
> - Line-count nit: `wc -l` of the `aws_real_*` family is **3,810** lines total (`... 473 aws_real_process_test.go, 337 aws_real_scope_test.go, 3810 total`), not ~4,300.
>
> **Severity:** drop (count as part of candidate B's minor residual CI-coverage note, not a separate finding).

## T199: [Repair collector source reads and closure correspondence](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-review-F1-F2-collector-custody-repair-20261024-r1)

Created 2026-10-24T19:49:26.646Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the implementation sub-agent for this task. User now says "Fix accordingly" after two independent reviews. This authorizes implementing repairs, replacing the previous read-only monitoring scope. AWS, adoption, actual authentication, native database access and provider/resource operations remain held. Read AGENTS.md, <local-artifact>/shared.md, matching language rules, and WORKSTYLE. Use diagnosis: verify source, reproduce narrowly, preserve before-fix result, repair canonical owner, run appropriate regressions without relaxing original bounds/assertions. Preserve all immutable source/test/report ancestors. Other authors work on independent owned paths. Do not overwrite TASKS.json or another author's folder. Heavy checks through one  [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `18a80579256681f02d0b2fa2fca1645c410729b754ffcab089bb337865ffe67f`):

> Sealed the delivery-only addendum. All 11 corrected rows verify; nine negative controls refuse as expected. The original manifest and strict exit1 remain preserved. T is unchanged; zero target runs occurred.
>
> Markdown report (local reference: `<approval-bank>/root-merged1047-dependency-pin-delivery-correction-20261025-r1/ADDENDUM.md`) · Complete offline HTML (local reference: `<approval-bank>/root-merged1047-dependency-pin-delivery-correction-20261025-r1/ADDENDUM.html`)
>
> - Checkpoint: `ae92011d957280ea77b31bfcb23191ac7fb6826d1af8501f492739ffd88f42a3`
> - Corrected declaration: `0b72671aae028cc510e53dd7cebcec76db7ffe09bc1246860bf6f786c16db8fc`
> - Source-set: `971840c8cb0e760e6ad3d93ea2e2a30f309f7f0a61fc267291f5704cb111e7fc`
> - Artifact index: `b4039cde4e697af5df99b8b0b88be08bb4b315cd9217c52ca96a57c43f3d400f`
>
> Final readback passed. Root’s delivery review remains pending; no qualification or adoption is claimed.

## T200: [Repair future-start phase and Operator compatibility](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-review-F3-future-start-canonical-owner-repair-20261024-r1)

Created 2026-10-24T19:49:26.964Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the implementation sub-agent for this task. User now says "Fix accordingly" after two independent reviews. This authorizes implementing repairs, replacing the previous read-only monitoring scope. AWS, adoption, actual authentication, native database access and provider/resource operations remain held. Read AGENTS.md, <local-artifact>/shared.md, matching language rules, and WORKSTYLE. Use diagnosis: verify source, reproduce narrowly, preserve before-fix result, repair canonical owner, run appropriate regressions without relaxing original bounds/assertions. Preserve all immutable source/test/report ancestors. Other authors work on independent owned paths. Do not overwrite TASKS.json or another author's folder. Heavy checks through one  [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `ed55dafaed30261d83bee7126d7c3e6485979432e38071c87bd6e3aa7b590cb3`):

> Frozen R4 delivered. Job1773 passed all 13 subjects, including real Root adapter/public arm and terminal-HOLD controls. All children were awaited. Import-free verification passed all 919 artifacts.
>
> - Final Markdown report (local reference: `<approval-bank>/root-merged1047-owner-ledger-proof-lock-repair-20261025-r4/REPORT-r2.md`)
> - Complete offline HTML (local reference: `<approval-bank>/root-merged1047-owner-ledger-proof-lock-repair-20261025-r4/REPORT-r2.html`)
> - Exact source/API/runtime pins (local reference: `<approval-bank>/root-merged1047-owner-ledger-proof-lock-repair-20261025-r4/FROZEN-COMPONENT.json`)
> - Final checkpoint (local reference: `<approval-bank>/root-merged1047-owner-ledger-proof-lock-repair-20261025-r4/CHECKPOINT.json`): `ecdb28f40ad874e3da7096ba9b7ccfe07644851c185c354d9c73e69a70f49707`
>
> Private Ledger interfaces remain unchanged; scope identity stays opaque. Old failures and post-CAS refusal remain preserved. Independent review, final composition and operational acceptance remain held. No AWS actions occurred.

## T201: [Repair PR 969 adoption recovery and review findings](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-PR969-review-code-and-comment-repairs-20261024-r1)

Created 2026-10-24T19:49:27.436Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the implementation sub-agent for this task. User now says "Fix accordingly" after two independent reviews. This authorizes implementing repairs, replacing the previous read-only monitoring scope. AWS, adoption, actual authentication, native database access and provider/resource operations remain held. Read AGENTS.md, <local-artifact>/shared.md, matching language rules, and WORKSTYLE. Use diagnosis: verify source, reproduce narrowly, preserve before-fix result, repair canonical owner, run appropriate regressions without relaxing original bounds/assertions. Preserve all immutable source/test/report ancestors. Other authors work on independent owned paths. Do not overwrite TASKS.json or another author's folder. Heavy checks through one  [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `bf1349c38bab934adfb10d3508a037ff980d4d6baebcec3bb4c8c3aa8dbb71a8`):

> Scanner closures match, and controlled native binaries are identical. The hosted slowdown remains unexplained; CodSpeed remains failed. No source/Git changes or paid runs occurred.
>
> Markdown diagnosis (local reference: `<approval-bank>/root-PR969-CodSpeed-scan-comparison-diagnosis-20261025-r1/DIAGNOSIS.md`) · Complete offline HTML (local reference: `<approval-bank>/root-PR969-CodSpeed-scan-comparison-diagnosis-20261025-r1/DIAGNOSIS.html`)
>
> Next bounded step: inspect existing CodSpeed samples and instrumentation metadata without rerunning benchmarks.

## T202: [Review collector custody and namespace repairs](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-collector-F1-F2-repair-independent-review-20261024-r1)

Created 2026-10-24T20:09:26.329Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. Perform a new independent technical source review of the repaired collector-custody component for Hikyo #158. User explicitly authorized "Fix accordingly" after the final review found threeP2 issues, and requested Sol6.1 extra-high reviewers/parallel work. You are not its author. One normal review attempt only; no retries, rephrasing or provider rerouting if blocked. Do not implement fixes. No further subagents. Read applicable AGENTS/shared/Python/WORKSTYLE and review workflow. Fixedpoint is the exact immutable component manifests and baseline source, not current Git HEAD. Separately assess Standards and accepted Spec; concrete P0-P3 findings only, or a qualified component-only no-findings verdict [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `89184450825b91763300d3e1bcd31e9ac97ef388822706f2a4cdf19d16f10c6f`):

> CHANGES REQUIRED: one P2 Spec finding. A registered module’s `__class__` can redirect callable dispatch while closure checks pass. F1 is addressed; broader F2 remains incomplete. Standards: no findings. Final migration HELD.
>
> [Review](<<approval-bank>/root-merged1047-collector-custody-repair-independent-review-20261024-r1/REVIEW.md>) · [Offline HTML](<<approval-bank>/root-merged1047-collector-custody-repair-independent-review-20261024-r1/REVIEW.html>) · [Checkpoint](<<approval-bank>/root-merged1047-collector-custody-repair-independent-review-20261024-r1/CHECKPOINT.json>)
>
> Checkpoint SHA256: `a2c0624ef431114af0f4b60c287ea9f1d9c1681e7202297de60c81207b2bb2c9`
>
> All six probe children awaited; 234 named inputs preserved. No source edits. Cross-provider/Jev skipped.

## T203: [Review PR 969 repair increment](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-PR969-repair-increment-independent-review-20261024-r1)

Created 2026-10-24T20:33:20.809Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. Independently review the PR #969 repair increment, new review round on changed Go/SQL/test/docs sources. User asked "Fix accordingly" after a full independent GLM review of PR969 and a separate frozen-source review. You are NOT the implementation author. One GPT-6.1 Sol xhigh review only. No repairs, commits, push, feedback comments/resolutions, AWS or provider actions. No additional review agents. Read canonical AGENTS/shared/WORKSTYLE and review/code-review workflow; apply Standards and accepted Spec separately, fixedpoint as below. Source-only technical verdict, no human approval or operational grant. Repo <local-artifact>/t3-a78c6309, local branch dunky13/complete-issue-158. HEAD1eb676a988a2976 [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `423676399f2381e50004d23279557b545ecb7a736556d7dd9f900be357e270fc`):

> **CHANGES REQUIRED: one P1 Spec finding; no Standards findings.**
>
> At module.go:222 (local reference: `module.go:222`), a nonempty stale adoption witness with a matching ownership tag still yields Update. Fresh consent remains impossible. Reproduced through the SQLite service/store/worker and both destination-mode unit probes.
>
> Reports: Markdown (local reference: `<approval-bank>/root-PR969-review-repairs-independent-review-20261024-r1/REVIEW.md`) · Complete offline HTML (local reference: `<approval-bank>/root-PR969-review-repairs-independent-review-20261024-r1/REVIEW.html`)
>
> Exact pins:
>
> - HEAD: `1eb676a988a2976e7bb20e588df49ffc00ce21b3`
> - Manifest: `96fd6419caf0d4a4afcfce6cf5905e20db9b974a78e11f1a88e6171b3b14dc50`
> - Patch: `d87a468b8cd9dc9092d9884edc5599d6bf1a6f0c370f8a978a551aca0ee4b6e7`
>
> All 20 files preserved. Reports distinguish passing checks, failed probes and skipped coverage. No repairs or operational approval.

## T204: [Review collector module-type repair](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-collector-module-type-repair-independent-review-20261024-r2)

Created 2026-10-24T20:35:57.209Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. NEW INDEPENDENT REVIEW ROUND R2, on substantively repaired source. User's "Fix accordingly" authorizes fixing/reviewing the findings; requested reviewer GPT-6.1 Sol extra-high. Review only, no implementation, no further agents. One ordinary attempt; if provider-blocked report exact error/stop, no retry/rephrase/reroute. CURRENT TARGET (supersedes R1 target/output paths in appended original brief): <approval-bank>/root-merged1047-collector-source-custody-repair-20261024-r2 HANDOFF.md18784/raw26afd9f52f07706b3d6c342d1134f2ccdb60b85dbe65cfbd146a7f5c21c503fd HANDOFF.html387703/raw88fe332853123d1847ece5e2a87403c73fd01d5e7b88060955e63b04d317ce8e CHECKPOINT.json2024/raw5c2c4f9dbc6afd1398040c5e1911ee69b93c [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `1c69c17ea19c1330b4b1e4fa3e9019b001209c6eede507aae351bdb445932deb`):

> Qualified component-only no findings: Standards 0, Spec 0. R1’s module-type finding is addressed. Final migration, combined review and adoption remain HELD.
>
> Markdown report (local reference: `<approval-bank>/root-merged1047-collector-custody-repair-independent-review-20261024-r2/REVIEW.md`) · Offline HTML (local reference: `<approval-bank>/root-merged1047-collector-custody-repair-independent-review-20261024-r2/REVIEW.html`) · Checkpoint (local reference: `<approval-bank>/root-merged1047-collector-custody-repair-independent-review-20261024-r2/CHECKPOINT.json`)
>
> Checkpoint SHA256: `ec2db32eea03ceab859f48e89786cd370b5eb5e26d44d7141cb2c4d5653d69cf`
>
> All three probe children were awaited. All 5,027 measured input paths remained unchanged.

## T205: [Review future-start canonical owner repair](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-future-start-repair-independent-review-20261024-r1)

Created 2026-10-24T20:47:01.035Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. Independent technical review of NEW frozen Hikyo #158 F3 repair, Sol6.1 xhigh, one ordinary round. Root is implementing the user's "Fix accordingly" and already authorized subagent reviews. Perform Standards and Spec axes separately as ONE reviewer; no further agents/provider calls, no cross-provider substitution, no filtered-review retry/rephrase/reroute. If provider blocks, retain exact failure and stop. This source changed substantively since prior review; not a request to repeat the unchanged blocked packet. Read AGENTS/shared/Python/WORKSTYLE and applicable review/code-review workflow. Explicit one reviewer/no child agents/local fixed manifests override skill parallel delegation and Git ref di [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `cb0d34243f2ba1d3c64f18fef759acc0db4365a415212f8f0c0ceb7b18b49517`):

> Bounded qualified-no-findings: **Standards 0, Spec 0** for the named F3 repair.
>
> Full future-start execution and the combined K aggregate remain **UNQUALIFIED**. No target tests were rerun. All 827 measured inputs were preserved.
>
> Full Markdown report (local reference: `<approval-bank>/root-merged1047-future-start-repair-independent-review-20261024-r1/REVIEW.md`) · Complete offline HTML (local reference: `<approval-bank>/root-merged1047-future-start-repair-independent-review-20261024-r1/REVIEW.html`) · Sealed checkpoint (local reference: `<approval-bank>/root-merged1047-future-start-repair-independent-review-20261024-r1/CHECKPOINT.json`)

## T206: [Review PR 969 stale-consent repair](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-PR969-stale-witness-independent-review-20261024-r2)

Created 2026-10-24T21:21:02.238Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. Independent review round2 for the NEW exact PR969 stale-witness repair. User requested Sol6.1 extra-high subagent reviews and then "Fix accordingly"; ordinary fresh review of substantive changed source, no retry/rephrase/reroute to evade provider blocks. If blocked, retain exact failure and stop. ONE reviewer, Standards and Spec separately, no further agents/provider requests. Read AGENTS/shared/WORKSTYLE and review/code-review workflows; explicit fixed manifest+single reviewer/no extra providers overrides Git merge-base discovery and parallel child steps. No implementation edits or Git operations beyond read-only status/diff; no commits/stage/push/merge/GitHub messages, actual AWS/auth/native/oper [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `b978041892fa818305310cfcb38b1069c406e1ac2764495dc019c492d50d9b3a`):

> Qualified no findings: **Standards 0, Spec 0**. Prior P1 R1 is resolved. Original probes fail before and pass after repair; focused SQLite recovery/replay checks pass. All frozen inputs remain unchanged.
>
> Markdown report (local reference: `<approval-bank>/root-PR969-stale-witness-independent-review-20261024-r2/REVIEW.md`) · Complete offline HTML (local reference: `<approval-bank>/root-PR969-stale-witness-independent-review-20261024-r2/REVIEW.html`) · Checkpoint and exact pins (local reference: `<approval-bank>/root-PR969-stale-witness-independent-review-20261024-r2/CHECKPOINT.json`)
>
> HEAD: `1eb676a988a2976e7bb20e588df49ffc00ce21b3`
> Manifest: `267ee5738c8a6457cba855a2b551076849c386ad0ff29f0b2fc6a084c84f13fe`
>
> Cross-provider/Jev skipped. This bounded 20-file verdict grants no merge, AWS, authentication or adoption approval.

## T207: [Review downstream future-start repair: Standards](t3-thread://v1/thread:delegated-task:command%3Amcp%3A8c69c7ef-afec-4515-8712-b399a52d864d%3Adelegate-task%3Aissue158-downstream-f3r2-standards-independent-review-20261024-r2)

Created 2026-10-24T21:51:12.503Z; parent `T031`; relation `subagent`; API status at capture `completed`.

**Task/scope excerpt:** Act as the review sub-agent for this task. Review the NEW frozen Hikyo #158 downstream source successor, not unchanged blocked review inputs. This is an authorized source-only independent review on OpenAI gpt-6.1-sol/xhigh. Do not edit source, Git, ledger, modes or old reports; do not send messages, launch children/providers, retry a blocked call, or access credentials/native production DB/network/CLI/resources/AWS. No adoption/merge/runtime authority. Root coordinates independent Standards and Spec siblings; you run only the named axis, no nested agents. Separate current findings from inherited limitations, report exact file:line, severity, evidence and bounded verdict. Read full attached prior brief/reviews and author response, never recl [task excerpt ends]

**Reported final outcome and argumentation** (historical, presentation-normalized; original captured message-text SHA256 `a401bb398aba33506b616169e97454aeeccfdb762ae18e2b4952602f42c911e7`):

> **Standards: 0 hard breaches, 0 actionable smells.** Bounded qualified-no-findings for R2 only. Full hosted entry remains **UNQUALIFIED**.
>
> Markdown report (local reference: `<approval-bank>/root-merged1047-downstream-source-standards-review-20261024-r2/REVIEW.md`) · Offline HTML (local reference: `<approval-bank>/root-merged1047-downstream-source-standards-review-20261024-r2/REVIEW.html`) · Checkpoint (local reference: `<approval-bank>/root-merged1047-downstream-source-standards-review-20261024-r2/CHECKPOINT.json`)
>
> Text parity, output pins and all 10,118 preservation rows verified. No target tests rerun.
