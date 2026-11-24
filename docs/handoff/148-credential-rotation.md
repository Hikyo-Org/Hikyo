# Handoff: #148 Scheduled credential rotation (PostgreSQL)

Issue: https://github.com/Hikyo-Org/Hikyo/issues/148 (blocked by #147, #63, #64, all merged).
Depends on `docs/handoff/147-dynamic-secrets.md` (provider, effects, worker pattern).

ADR amendments (operative when the PR implementing slice S1 merges). Cross-model
review waiver, dated 2026-11-16: the owner's 2026-11-02 instruction (the Anthropic
subscription is active, the Codex subscription has ended, so OpenAI reviewers and
`codex exec` routes are unavailable until explicitly restored; WORKSTYLE review
policy owner update) makes cross-provider review unavailable, so it was not run and
is not reported CLEAN. Independent adversarial Standards, Spec and Security reviews
by Claude Opus 5.5 ran in three rounds; the review records are posted on the PR by
the session that ran the reviews, not kept in this file.

- `mvp-boundary.md` §4.1 "Arbitrary third-party rotation": flipped **In for exactly
  one provider, PostgreSQL**; every other provider keeps the per-provider trigger.
- `api-cli-surface.md`: declared amendment adding the closed `rotation` verb family
  (`create|list|show|pause|resume|release-hold|run-now|delete`, `runs`) and its API paths.
- `audit-events` (`rotation.configured`): the `action` enum gains `hold_released` (S2b-2);
  no new event type.
- `system-architecture.md` § Jobs: a third domain-specific outbox in the dynamic
  outbox pattern; no generic queue, no `SiteScheduler` growth.
- `docs/spec/ops-catalogue.md` § Scheduled credential rotation (#148): the four bounds.

## What this is

An operator configures a rotation for one PostgreSQL provider (#147), one secret key
(the password) and one more key (the role name, required), an interval and a
revocation delay. On schedule Hikyo mints a replacement role, publishes the new
value(s) through the ordinary validated revision path, waits a bounded overlap, then
drops the superseded role. Delivery cursors move for free because the publish is an
ordinary publish. The rotation acts as its **recorded authority principal** (the
human who configured it): the full authority gate runs before every transition that
can still be rolled back, and after the publish only fail-safe checks apply (see
decision 13). Never a `SystemProof`, never a scheduler site.

## Slice plan (stacked PRs, each its own PR)

- **S1 (this PR): configuration only.** ADR amendments, this doc, migration 00091
  (`rotations`, `rotation_runs`, `rotation_effects`), store, authz operations, audit
  event, service CRUD, API, CLI, docs. No run execution, no scheduler.
- **S2: execution.** The durable state machine below, the worker/scheduler claim
  loop, rotate-now (`run-now`), runs inspection (`rotation runs`), metrics, `hikyo
  doctor`, the run audit events, restart and failure e2e on both engines.
- **S3: browser.** SPA rotation list and run timeline (status only) on the machine
  access / Values surface, parity rows flipped from `{issue: 148}` to webui.

### Progress

- [x] S1 ADR banners + this doc
- [x] S1 migration 00091 (both engines) + sqlc + store + compat pin + upgrade drill
- [x] S1 authz op + store ops + audit event + budget classification + bound registry
- [x] S1 service (create/list/show/pause/resume/delete); key-delete refusal dropped, see decision 2
- [x] S1 OpenAPI + apigen + TS client + server handlers + parity
- [x] S1 CLI `rotation create|list|show|pause|resume|delete` + help golden
- [x] S1 docs page (`rotation.mdx`) + cli-reference
- [x] S1 round 2 review fixes: owner decisions Q1 to Q5 below, key claims table, environment
  delete purge or refusal, provider delete and credential revoke pause rotations,
  ceremony at create and resume, protected and approval-covered environments refused,
  credential expiry in status, bounded list, shared `d` duration parsing in the CLI
- [x] S1 round 3 review fixes: halted state and `halt_reason` and the `revoke_refused` phase
  settled in migration 00091 (the resume formula `edit ∧ publish ∧ reveal @environment`
  lives in `internal/authz/registry.go`, `OpRotationResume`, not in the migration); cascade pause when an environment becomes
  protected or gains an approval policy; environment delete treats quarantine as live and
  names the blocking rotations; paged list with one batched expiry read; recorded-authority
  key resolution on every call; per-key denial attributed to the resume operation; tests
  for each; documentation made consistent on the gate (full before publish, fail-safe after)
- [x] S1 round 4 review fixes: `revoke_refused` also requires a recorded publish; the
  credential-expiry read is ranked in SQL (one row per requested key, never the run
  history); environment-delete refusals name distinct rotations and the truncation marker
  is exact; `rotationDuration` no longer overflows; S2 design closes the delayed-commit
  hazard, the restore hazard, the post-publish revoke obligation and the slack proof
- [x] S2a executor: durable state machine, worker wiring, fenced claims, schedule, retries,
  extend, reconcilers, quarantine exits, `rotation.run_intent|run_outcome|quarantined`
  audit events, provider-delete and environment-delete guards (see "S2a as built")
- [x] S2a dual-engine crash, takeover and scenario e2e, delivery-path e2e, real PostgreSQL
  provider e2e (skips without `HIKYO_TEST_DYNAMIC_PG_*`)
- [x] S2b-1 run-now (`rotation.run_now`, `OpRotationRunNow`, manual runs, `--skip-overlap`),
  runs inspection (API and CLI), `--abandon` on the wire, user docs (see "S2b-1 as built")
- [x] S2b-1 consumer-convergence report: adapter (sync) targets only
  (`not_converged_adapter_targets`); the machine-principal part is option A, accepted as
  recommended (see "Owner decisions on S2b-2")
- [x] S2b-2 metrics, `hikyo doctor`, the `restore_hold` column and release, key-aware
  pause, resume, delete and run-now (see "S2b-2 as built")
- [ ] S2b-2 incident ledger enumeration of rotations: owner chose B (account scope only),
  built in a follow-up branch from dev after this PR merges, not in this PR; containment itself
  already halts the rotation
- [x] S2b-2 run-level `published_versions_changed` hold release: option A (status only, as
  built), accepted as recommended
- [x] S3 SPA status surface (merged); parity flip of the mutating rows to #1440 and the
  halt reason and owed-drop summary in S4 (below)

## Decisions taken in S1 (divergences from the issue handoff flagged)

1. **Tables.** `rotations` and `rotation_runs` as in the issue handoff, plus columns
   the handoff omitted and S2 needs so S2 needs no table rebuild (SQLite CHECK enums
   cannot be altered in place):
   `rotation_runs` gains the tenant chain (`org_id`, `project_id`, `environment_id`
   with a composite FK to `rotations`, the `dynamic_effects` shape), the row-lease
   fence (`lease_owner`, `lease_expires_at`, `lease_claim_token`, as
   `dynamic_leases`), `run_trigger` (`schedule|manual`), `revocation_delay_seconds` (the
   per-run snapshot, `0` for a skip-overlap run), and `candidate_valid_until`.
   **Divergence: a third table `rotation_effects`** (INTENT/OUTCOME ledger mirroring
   `dynamic_effects`): the issue handoff mandates INTENT/OUTCOME per transition and an
   `unknown` metric, which needs the ledger. A third separate outbox, not a reuse.
2. **Integrity constraints.** One non-terminal run per rotation (partial unique
   index). One live rotation per key in the environment, whichever role the key plays:
   table `rotation_key_claims` has primary key `(environment_id, key_id)` and a rotation
   inserts one claim per key (password and username) when it is created and releases
   both when it is disabled. This replaces two per-role partial unique indexes, which
   could not stop rotation B using rotation A's password key as its username key. A
   unique violation on the claim insert alone maps to the named in-use refusal; foreign
   key and CHECK violations keep their own errors. `username_key_id` is `NOT NULL` and
   `<> key_id`; a CHECK ties `next_run_at` to the `active` state; a CHECK ties a
   `rotation_effects` outcome, its audit id and its finish time together (all set or all
   unset). **Divergence: no foreign key to `keys`.** A disabled rotation is history and
   must outlive a deleted key, and the soft delete frees the slot, so a key FK would
   either block key deletion forever or erase history. As a result `Keys.Delete` does
   **not** refuse while a live rotation references the key. Resume re-checks that both
   keys exist and the password key is still `secret`, and refuses by name
   (`ErrRotationKeyMissing`); the S2 gate does the same before every PRE-publish
   transition and halts with a named refusal. After the publish the key check does not
   apply (decision 13): a key deleted underneath a published run must not stop the
   revoke of the superseded role. **Correction to the round 1 text:** provider deletion is a
   soft tombstone, not a foreign key failure. Deleting a provider, or revoking its admin
   credential, now pauses every active rotation that mints from it in the same
   transaction and audits each pause with a `reason` (`provider_deleted` or
   `provider_credential_revoked`); resume refuses with `ErrRotationProviderInactive`
   until the provider is active and has a credential. Deleting an environment refuses by
   name (`ErrEnvironmentHasLiveRotation`, whose message carries the blocking rotation ids:
   up to ten distinct rotations of each kind in id order, so one rotation with many runs
   cannot hide another, and `(first 10 shown)` is appended only when more block than are
   named) while any rotation is not `disabled` or any run may still
   hold a live role. `quarantine` is NOT terminal for this purpose: a quarantined run may
   hold a live role. Only `done` runs and `failed` runs that never published (the
   candidate was confirmed dropped) are purgeable; a `failed` run with a recorded publish
   (a combination the S2 executor never writes, kept as a defensive net) and every other
   phase (including `revoke_refused`) blocks. When nothing blocks, the
   environment's rotation effects, runs, claims and rotations are purged in the same
   transaction (the SSH `PurgeEnvironment` pattern), on both the direct delete and the
   definitions-apply path, which share `purgeEnvironmentRotations`.
   **A rotation never stays active in an environment that stops being unattended.**
   Protecting an environment (`SetEnvironment`, false to true) and creating or enabling an
   approval policy (`CreatePolicy`, `UpdatePolicy` with `Enabled`; a project-wide policy
   reaches every environment of the project) pause that environment's active rotations in
   the SAME transaction and audit each pause with `reason` `environment_protected` or
   `approval_policy_enabled`, the caller as actor. Those are the only writers of
   `environments.protected` and enabled policies (found by auditing every `Approvals()`
   and `Environments()` write in `internal/service`); definitions apply does not touch
   either. Resume refuses until the environment is unattended again. (Dynamic leases have a similar
   unpurged-history gap; that is issue #1420 and is not changed here.)
3. **Delete is soft.** `rotation delete` transitions to `disabled` and keeps the row,
   so run history and effect FKs survive and the key slot is released. A rotation
   with a non-terminal run refuses delete (S1 can have none; the check and its
   error exist so S2 slots in; `--abandon` is S2).
4. **Authz (all S1 operations human-session only).** Configure (create):
   `edit ∧ publish ∧ reveal @environment`, authorized on BOTH stored keys as
   individual keys (`AuthorizeKey`), so a key-narrowed member access rule applies and an
   invisible key is the uniform not-found. The creator is recorded as
   `authority_principal_id`. Create and resume are human acts: the caller must be a
   signed-in person (`Class` human with a session), because a machine or session-less
   caller has no ceremony to consume and would silently skip the step that authorizes the
   delegation. They consume a `PurposePublish` reauthentication ceremony over the
   environment and both keys (owner decision Q1), through the same `requireCeremony` a
   values publish uses; the CLI reuses the existing ceremony handoff. Pause and delete are
   `publish@environment` with no key target: they only reduce what the rotation does. Their
   human-only property rests on the contract (`x-hikyo-artifacts: [human-session]`, tested
   by `TestRotationOperationsAdmitHumanSessionOnly`), not on a service check. Inspect (list,
   show): `read@environment`. Reads are human-session only for now; widening to machine
   credentials is additive later, narrowing is not. `manage-identities` is deliberately
   **not** required: `lease.mint` already admits any `read` plus `reveal` holder to mint
   from any provider in the project, so a rotation does not widen who can obtain a provider
   credential.
   Create additionally validates: the provider exists in this project, is active and has
   its credential set; `key_id` exists and is `secret`-classified (a `config` key would
   hand the password to plain readers); `username_key_id` exists; the two keys differ;
   neither key is already claimed by a live rotation; interval and delay are inside the
   bounds; the environment is not protected and no approval policy covers it. A scheduled
   rotation publishes unattended, so an environment whose publishes need a per-publish
   ceremony or an approval request cannot be its target; both refusals are named
   (`ErrRotationProtectedEnvironment`, `ErrRotationApprovalPolicy`) and apply again at
   resume. Lifting either refusal later is additive (a looser rule never breaks a stored
   rotation).
   Resume (owner decision Q5) is the `rotation.resume` operation, whose formula is
   `edit ∧ publish ∧ reveal @environment` (the same atoms as create, so a resume denial is
   recorded under the resume operation, never `rotation.create`). S1 had no key target in the
   outer authorization; S2b-2 authorizes the operation on both stored keys, so a caller who
   holds the formula through key-narrowed member rules resumes like an environment-wide one
   (see "S2b-2 as built"). Inside, in order: a human caller, the environment still unattended,
   the provider active with a credential, both keys present and secret, the CALLER holding
   the formula on both stored keys (`AuthorizeKey`), the recorded authority principal still
   holding it on both stored keys (`RecordedPrincipalHolds` with a key target, resolved from
   the database on every call so a deleted key is never held through environment-wide
   grants; the caller stays the actor of any denial and `AuthorityID` names the authority),
   and the ceremony. A recorded authority who holds the formula only through key-narrowed
   rules does hold it, so an environment-wide second person can resume that rotation.
   A publish-only caller cannot re-arm another person's delegation. Resume applies to a
   `paused` or a `halted` rotation and runs the same full authorization for both. This is
   the S2 `Gate` shape in miniature.
5. **CAS.** `pause`, `resume`, `delete` carry `expected_generation`; a stale value is
   `409`. Every mutation increments `generation`, **except `release-hold` and the restore
   hold itself** (S2b-2: an in-flight run asserts the generation it started under; see
   "S2b-2 as built"). The pre-00094 fallback pause of a restore does bump it.
6. **Audit.** Only `rotation.configured` (`action`: `created|paused|resumed|deleted`, plus `hold_released` from S2b-2;
   plus `environment_id` on every action, the optional `reason` of a cascade pause
   (`provider_deleted`, `provider_credential_revoked`, `environment_protected`,
   `approval_policy_enabled`) and the `credential_expires_at` in force) is registered in S1. **Divergence from the brief:** the run events are *specified*
   below but not registered, because the registry-emitter closure test fails any
   registered type that nothing emits. S2 registers and emits them. Likewise
   `run-now` and `runs-read` operations are not declared in the authz registry (no
   wire route yet), and the contract omits their paths until S2.
7. **Bounds.** `MinRotationInterval = 1h`, `MaxRotationInterval = 90d`,
   `MinRevocationDelay = 5m`, `MaxRevocationDelay = 7d`. Constants live in
   `internal/domain`; pinned in the conformance bound registry
   (`internal/conformance/boundregistry_test.go`, the repo's bounds-invariant
   mechanism) and in `docs/spec/ops-catalogue.md`. Rows additionally carry
   `> 0` CHECKs; the bound values themselves are code constants so they can move
   without a migration. A skip-overlap run's `0` delay is a run snapshot, not a
   configuration value.
8. **Parity.** Browser parity for the new operations is tracked by `{issue: 148}`
   (S3 of this issue) in `api/parity.yaml`.
9. **Restore.** No sealed column exists, so there is nothing to invalidate. S2 should
   decide whether a restored rotation starts `paused` (a restored instance must not
   mint against a provider before its operator has reconciled grants); S1 leaves the
   rows as restored, and no scheduler exists to act on them yet. The S2 checklist records
   a `restore_hold` column for this.
10. **Credential expiry is visible (owner decision Q2).** A paused, deleted or failed
    rotation lets PostgreSQL expire the delivered role at `VALID UNTIL`, an outage the
    operator can cause. `show`, `list`, and the pause and delete responses carry
    `credential_expires_at`, derived (no new column) from the latest `rotation_runs` row
    with `published_revision IS NOT NULL` for the key across all rotations that ever drove
    it, so a delete and recreate still reports the live role. The CLI names the expiry on
    pause and delete. The credential already in the keys before the first run (the
    operator's own value) is never revoked by a rotation and has no expiry here. The
    `extend` effect kind is admitted by the `rotation_effects` CHECK now so S2 can add an
    extend action without a table rebuild.
11. **List is bounded and paged.** `list` returns live rotations by default;
    `include_disabled` (`--include-disabled`) adds the deleted history. A page holds at
    most `limit` rotations (default 100, ceiling 500, an out-of-range value is refused as
    invalid) in id order; `next_after` is the last id and `after` (`--after`) continues
    from it, so a response is always bounded. Credential expiries for a page come from one
    batched read ranked in SQL (the newest published run per requested key, one row per
    key whatever the run history), not one query per rotation. Both parameters are additive.
    **States and slots settled now.** `rotations.state` admits `active`, `paused`,
    `halted` and `disabled`; `halted` is the executor's named stop after a failed gate and
    carries a non-secret `halt_reason` (at most 200 characters, only legal on a halted
    rotation, cleared by any transition). S1 never writes `halted`. A pause reason needs no
    column: it is on the `rotation.configured` audit event. `rotation_runs.phase` admits
    `revoke_refused` (non-terminal, keeps the open-run slot, blocks environment delete).
    A nullable `pause_reason` column, if S2 wants one, is an additive `ADD COLUMN` on both
    engines and needs no rebuild.
12. **Pause semantics.** A pause before publish rolls the unpublished candidate back; a
    pause after publish lets the run finish, so a delivered credential never strands its
    predecessor. S1 has no runs, so a pause only stops scheduling today.
13. **Full gate before publish, fail-safe after.** The full authority gate (edit, publish
    and reveal per key, provider active, keys present and secret) runs before the publish.
    After the publish only fail-safe checks apply: the row fence, the recorded
    `published_revision` and the provider's existence. See `system-architecture.md` and
    `mvp-boundary.md`.

## S2 state machine (design; nothing here executes in S1)

Every transition is an INTENT/OUTCOME effect (`rotation_effects`) under the recorded
authority. A run row is the job, claimed under its own row lease
(`SELECT ... FOR UPDATE SKIP LOCKED`, per-org cap, exact-row re-assertion on every
settling write), exactly as `dynamic_leases`; no singleton scheduler lease.

Phases (`rotation_runs.phase`): `minting`, `minted`, `staged`, `published`,
`overlap`, `revoking`, `revoke_refused`, `done`, `quarantine`, `failed`. `staged` is
reserved and not entered by this design (kept in the CHECK so a sealed-candidate design
needs no table rebuild). Non-terminal (they hold the one-open-run slot): everything except
`done`, `quarantine`, `failed`. `done` and `failed` are the only phases that hold no
obligation; `quarantine` is terminal for scheduling but still owes work (see "Revoke
obligation and exits from quarantine"). For environment delete only `done` and a
`failed` run without a `published_revision` are purgeable, and a run that holds an
unsettled revoke obligation is never purgeable whatever its phase. The executor never
writes `failed` for a run that has a `published_revision` (crash-safety rule 5 keeps
such a run retrying); the delete predicate still blocks on that combination as a
defensive net. `done` requires a `published_revision` (the CHECK), so a run that never
published ends as `failed`, never `done`.

Scheduling: a due rotation (`state=active`, `next_run_at <= now`) creates a run in
`minting` in one transaction and parks `next_run_at` at the run's pre-publish deadline
(`now + publish_budget`, `publish_budget = 1h`; the column must stay non-NULL while the
rotation is active). **One anchor: the publish time `P`.** The publish transaction sets
`next_run_at = P + interval`; nothing else derives the next run from the creation time.
A failed run sets `next_run_at` to its next backoff attempt (see the retry section), not
to `now + interval`. Missed windows after downtime collapse to one run, not a backlog.

### Candidate-password durability (chosen order, "Option A": no sealed column)

The generated password exists only in worker memory and in the **one** publish
transaction. It is never stored, not even sealed. Order:

1. **`minting`.** One transaction: re-authorize (the Gate, below), record
   `candidate_handle` (deterministic `hikyo_rot_<run-id-prefix>`) and
   `superseded_handle` (the delivered handle of the latest run with
   `published_revision IS NOT NULL` for this `(environment_id, key_id)`, across ALL
   rotations that ever drove the key; `NULL` only when no run ever published for it),
   write the INTENT effect. The handle is durable **before** the provider is touched.
2. `CreateRole(candidate_handle, password, VALID UNTIL mint + publish_budget + interval +
   delay + slack)` outside any transaction. A run that has not published by `mint +
   publish_budget` is rolled back, so for any actual publish time `P <= mint +
   publish_budget` the role is valid until at least `P + interval + delay + slack`; the
   proof below is stated from `P` only.
3. **`minted`.** OUTCOME(mint success) recorded. The password still lives only in
   memory. A run found in `minting` or `minted` after a crash has lost its password
   and **always rolls back**, in this order and never otherwise: (a) a **fenced claim
   write** under the worker's current claim token, `UPDATE rotation_runs SET
   phase='failed', ... WHERE id=? AND phase IN ('minting','minted') AND
   lease_claim_token=?`, which waits on the row lock and is committed BEFORE any provider
   call; (b) only when it matched one row, `DropRole(candidate_handle)` (idempotent,
   absent is success); (c) the OUTCOME (an unknown drop outcome moves the run on to
   `quarantine`, rule 6). A fresh run mints a fresh candidate. Zero rows
   matched means the run is no longer `minting`/`minted` (it published, or another
   worker owns it): re-read and follow that state, never drop. See crash-safety
   rule 8.
4. **`published`.** ONE transaction: re-authorize the full Gate, stage the new
   value(s) (`Values.stage`/`Declare`), `Revisions.PublishPlanned` with the staged
   version ids, write `published_revision`, set `phase=published` and
   `next_attempt_at = now + revocation_delay_seconds` (the claim time of `published`
   only: recovery re-establishes the overlap, see note 38), set the rotation's
   `next_run_at = P + interval` (`P` = the commit time), OUTCOME(publish success). A
   schema-validation failure is a definite failure: the fenced claim write to `failed`
   first, then drop the candidate.
   Because the phase write and the publish commit together, a crash can never leave a
   delivered new value with a run still saying `minted`, or the reverse. The publish
   transaction takes the run row first (the exact-row fenced `UPDATE ... SET
   phase='published'` is its first write), so it holds the row lock for the whole
   transaction. An ambiguous commit (connection lost at COMMIT) is resolved by the same
   fenced claim write as step 3, never by a plain read: the write waits for the in-flight
   transaction to finish, then matches zero rows if it published and one row if it did
   not (the run is then rolled back as in step 3).
5. **`overlap`.** Durable wait until `revoke_not_before_at` (the wake, `next_attempt_at`,
   is that time or the delivered credential's extension margin when earlier, and a visit
   at the margin extends and parks again, see note 38). Skipping
   the overlap (`run-now --skip-overlap`) sets the run's `revocation_delay_seconds`
   to `0`, so this phase is immediate.
6. **`revoking`.** First the **restore assertion**: for both keys, the current published
   value version must still be a version published by this run, or by a later run of the
   same key (compared by version id from `published_revision` and the later runs' recorded
   publishes; no secret is read). Only then INTENT(revoke), `DropRole(superseded_handle)`
   idempotent, then OUTCOME and `done`. A failed assertion moves the run to the named hold
   `revoke_refused` with `last_error = 'published_versions_changed'` and does NOT revoke
   (crash-safety rule 7). When `superseded_handle IS NULL` (no run ever published for the
   key, so the previous value was the operator's own) the phase is skipped and
   `published` goes straight to `done`.

### Crash-safety rules (binding on S2; S1's schema must not violate them)

1. An interrupted cycle must resume or roll back cleanly; no phase may require
   in-memory state that a restart loses, except the candidate password, whose loss
   always means roll back the candidate.
2. Revocation of the superseded credential **never** runs before the new credential is
   confirmed published, and never while a key may be serving the superseded value again.
   `published_revision` is recorded by the same transaction as the publish; `revoking` is
   unreachable without it, and the revoke step re-reads it and runs the restore
   assertion (rule 7).
3. Every step is audited with INTENT before the effect and OUTCOME after.
4. Failure **before** publish drops (or quarantines) the candidate and leaves the
   delivered value unchanged.
5. Failure **after** publish keeps both handles recorded, retries the drop with
   backoff, and surfaces "a possibly live credential exists" (status, metric
   `hikyo_rotation_runs_unknown_outcome`, `hikyo doctor`). It is never marked `done`
   without a successful drop OUTCOME, and the executor never writes `failed` for a run
   with a `published_revision`: such a run stays in `revoking` or `revoke_refused` (or is
   closed as `quarantine` by `--abandon`) until its obligation settles.
6. An ambiguous provider outcome never destroys the last known-working credential:
   the superseded role is only dropped after a confirmed publish, and an unknown
   candidate drop leaves the run in `quarantine` (before publish) or `revoking`
   with an `unknown` effect (after publish), both visible.
7. **Restore hazard.** Before `DropRole(superseded_handle)` the revoke step asserts that
   each of the two keys' current published value version is still one this run
   published (or a later run of the key published), by version id, with no secret read.
   `revision.restore`, or a manual publish, can put the superseded value back; dropping
   its role then is an outage. On mismatch the run moves to `revoke_refused` with
   `last_error = 'published_versions_changed'`, the revoke is NOT performed, and the hold
   is visible in `rotation show`, `rotation runs` and `hikyo doctor` (the last two are S2b).
   The hold is re-evaluated on each backoff retry and ends in exactly two ways: the
   assertion passes again (each key has no served entry, or its served entry was published
   by a run of that key at or after this run's revision), or the superseded role is dead
   (absent, or its `VALID UNTIL` past the skew margin). A restore or manual republish
   stages a new version id that no run published, so it never passes the assertion, and no
   later run of this rotation can start while this one is held (owner decision A).
8. **Delayed-commit hazard.** A publish transaction can commit after another worker has
   already looked at the run (a stalled COMMIT, a partition healing). Every place that
   resolves an ambiguous run state (`minting` or `minted` after a crash or takeover, an
   ambiguous publish commit, a fence-losing worker's cleanup) must FIRST do the fenced
   claim write of step 3, which waits on the row lock the publish transaction holds, and
   commit it BEFORE `DropRole`. A plain read is never enough: it can see `minted`
   while the publish is about to commit. Zero rows matched means follow the state the run
   is now in; never drop a candidate whose run might have published.

| Crash point | State found on restart | Action |
| --- | --- | --- |
| After handle recorded, before `CreateRole` | `minting`, role absent | fenced claim write to `failed` (committed), then drop candidate (no-op) |
| During or after `CreateRole`, before OUTCOME | `minting` | fenced claim write to `failed` (committed), then drop candidate |
| After OUTCOME(mint), before publish tx | `minted` | fenced claim write to `failed` (committed), then drop candidate |
| Publish tx commit ambiguous | row lock may still be held | fenced claim write waits: zero rows means `published`, continue; one row means roll back |
| Publish tx still in flight when a takeover worker looks (delayed commit) | plain read says `minted`, publish holds the row lock | takeover's claim write blocks on the lock; after the commit it matches zero rows and the run continues as `published`, no `DropRole`; after a rollback it matches one row and rolls back |
| Death or lease lapse between the publish commit and the overlap settle | `published`, deadline stored before the commit | claimed once that deadline is due; extend check, then `overlap` with now(recovery) + delay (never revoked on the stored value); one extra delay at worst |
| After publish, before drop | `published` / `overlap` / `revoking` | resume, restore assertion, idempotent drop |
| Keys serve the superseded value again before the drop (restore or manual publish) | `revoking`, assertion fails | `revoke_refused` (`published_versions_changed`), no drop, loud |
| During `DropRole` | `revoking`, effect INTENT only | re-probe `RoleStatus`, settle |
| Fence-losing worker after `CreateRole` (multi-node takeover) | candidate role exists, run `failed` or `quarantine` | candidate reconciler re-probes `RoleStatus(candidate_handle)` and drops it (below) |
| Key restored after the restore assertion but before the drop (TOCTOU) | `revoking`, assertion passed | the drop proceeds; the window is the span between the assertion read and the provider call, bounded by one provider deadline, and is accepted: a restore stages a new revision, and the role was already past its overlap. Since S4 round 1 the assertion is read before `LoadSupersededRole` and the provider open, so the gap between that read and the drop is slightly wider (local steps only: a store read and a provider open). A remote keyring would need a re-read after the open |
| Run abandoned past publish (`--abandon`) | `quarantine`, `published_revision` set, superseded role may be live | revoke-obligation loop drops `superseded_handle` after the overlap deadline (below); a run abandoned while still `published` gets abandon time + delay (note 38) |

**`superseded_handle` is derived across rotations.** The role being superseded is the
delivered handle (the candidate) of the latest run with `published_revision IS NOT NULL`
for the same `(environment_id, key_id)`, across ALL rotations that ever drove the key,
whatever that run's phase. A deleted and recreated rotation therefore still revokes the
role that is live in the keys, and the first run of a recreated rotation does not skip the
revoke. It is `NULL` only when no run ever published for the key (the operator's own
initial value).

**Multi-node takeover orphan.** A worker whose row lease expired mid-run can still finish
`CreateRole` after another node took the run over and rolled it back to `failed`. The
fence-losing worker drops its candidate best-effort ONLY after a definite fence loss (one of
its own fenced writes matched zero rows); after an ambiguous commit it does nothing and
leaves the run to the takeover path (rule 8). The **candidate reconciler** re-probes
`RoleStatus(candidate_handle)` for runs with `published_revision IS NULL` in `failed` or
`quarantine`, once the lease has expired plus the provider deadline, and drops any role
still present. It owns `candidate_handle` only and its selection predicate is
`published_revision IS NULL` and nothing else: a run with a recorded publish is never
touched by it, because that candidate IS the credential delivered into the keys. Its test
must include an abandoned-past-publish run whose candidate role must survive. The lease
TTL must exceed the provider deadline so a live worker is never taken over mid-call. Add
the takeover row to the S2 crash tests and to the multi-node takeover e2e.

### Revoke obligation and exits from quarantine

A run owes the revoke of `superseded_handle` from its publish until a revoke OUTCOME
`success` is recorded for it (a `rotation_effects` row of kind `revoke`) or the superseded
role is observed gone. The obligation predicate is `published_revision IS NOT NULL AND
superseded_handle IS NOT NULL AND` no successful revoke effect; `done` is by construction
settled. It is owned by a **second loop, the revoke-obligation loop**, with its own
selection, separate from the candidate reconciler, and it runs on the same row-lease
machinery. Which component owes what, per post-publish case:

| Case | Run ends in | Candidate (delivered credential) | Superseded role |
| --- | --- | --- | --- |
| Normal | `done` | stays live, it is the delivered value | the executor's `revoking` step drops it |
| Provider refuses the drop (`2BP01`) or the restore assertion fails | `revoke_refused`, non-terminal | stays live | executor retries with backoff, never `done` |
| `rotation delete --abandon` past publish | `quarantine` | stays live until its `VALID UNTIL`; nothing revokes it | revoke-obligation loop drops it once the overlap deadline (`published + revocation_delay_seconds`; abandon time + delay for a run still in `published`, note 38) has passed |
| Provider unreachable for a long time | stays `revoking` (never `failed`) | stays live | executor retries; the failure is loud |
| Rotation deleted (soft) with the run already `done` | `done` | stays live until `VALID UNTIL` | nothing owed |
| Environment or provider delete | refused while the obligation is unsettled | n/a | the loop settles it first, then the delete proceeds |

The revoke-obligation loop selects runs in `quarantine` (and, defensively, `failed`) with
the predicate above, runs the same restore assertion and fail-safe checks as the
executor's `revoking` step (row fence, recorded `published_revision`, provider existence;
never the authority gate), then INTENT(revoke), `DropRole(superseded_handle)`, OUTCOME.
It never drops `candidate_handle`.

**Exits from `quarantine`.** A quarantined run is not terminal for environment delete or
provider delete while it owes anything. It leaves `quarantine` only by one of:

- Pre-publish quarantine (`published_revision IS NULL`, an unknown candidate drop): the
  candidate reconciler confirms the role absent via `RoleStatus(candidate_handle)`, or
  the candidate's `candidate_valid_until` has passed (an expired role cannot
  authenticate); the run then becomes `failed`, which is purgeable. It cannot become
  `done`, because `done` requires a `published_revision`.
- Post-publish quarantine (abandoned): the revoke-obligation loop confirms
  `superseded_handle` absent via `RoleStatus`, or the superseded role's `VALID UNTIL`
  (the earlier run's `candidate_valid_until`) has passed; the run then becomes `done`.

Each exit writes its own effect and OUTCOME. The environment-delete refusal advice
("delete each rotation and let its runs finish first") is therefore followable: every
blocking run reaches `done` or an unpublished `failed` on its own, and a stuck one is
named by `hikyo doctor` with its `last_error`.

**`DROP ROLE` refused for a role that owns objects.** PostgreSQL refuses `DROP ROLE` with
SQLSTATE `2BP01` (dependent objects) when the role owns objects or holds privileges. Map it
to `ErrRefused` in `classifyExec` (`internal/dynamic/postgres/postgres.go`). The run then
enters a named state, `revoke_refused` (`last_error = 'role_owns_objects'`), visible in
`rotation show`, `rotation runs` and `hikyo doctor`, retried with backoff and never
marked `done`. `revoke_refused` has two causes, told apart by `last_error`:
`role_owns_objects` and `published_versions_changed` (the restore assertion). A quarantined
(abandoned) run cannot enter `revoke_refused`, because it no longer holds the open-run slot
a non-terminal phase needs; its refusal is recorded in `last_error` with the phase staying
`quarantine`, and `hikyo doctor` reports it from there. Decision: the rotation does NOT run `REASSIGN OWNED BY candidate TO grant_role` before the drop. Reasoning: a rotation
mints roles that are only ever members of the provider's `grant_role`, and silently
reassigning ownership would hide a consumer that created objects as the rotated role (a
real misconfiguration that would also break after the next rotation). The documented rule
is that rotated roles must not own objects; objects belong to the grant role. The named
refusal makes a violation loud rather than a silent outage.

### The Gate (full before publish, fail-safe after)

The gate uses `RecordedPrincipalHolds` only: no identity is rebuilt from the recorded
`authority_principal_id` and `az.Authorize` is never called with it. The worker is the
caller of record for any denial and `AuthorityID` names the recorded principal. The
per-key check is `RecordedPrincipalHolds(ctx, caller, authority, OpRotationResume, scope,
&key)` for each of the two stored keys, exactly as S1 resume does (the key is resolved from
the database on every call, and a key-narrowed member rule on the authority applies).
Pre-publish transitions (`minting`, publish) require, in a fresh read transaction:
the authority still holds `edit ∧ publish ∧ reveal` on both keys; both keys present and the
password key still `secret`-classified; the environment still unattended (not protected, no
approval policy covering it); the provider active with its credential set; the rotation
`active`; `generation` unchanged. **Post-publish transitions (`overlap`, `revoking`)
re-assert only the row fence, the recorded `published_revision` and the provider's
existence**, the fail-safe direction `dynamic_leases` already takes for revoke: losing
authority, a deleted key or a protected environment must never strand a live superseded
credential. The one extra post-publish check is the restore assertion before the drop
(the keys' current published value versions are still those this run, or a later run,
published; by version id, no secret read). It is not an authority check: it can only
REFUSE a destructive step, holding the run in `revoke_refused` with `last_error =
'published_versions_changed'`, so it cannot strand anything it did not already leave
live. A failed pre-publish gate moves the rotation to `halted` with a `halt_reason`.
**Divergence from the issue handoff**, which gates every transition.

### Failure, retry and credential expiry (S2)

A failed scheduled run does not wait a whole interval for its next try, because the
delivered credential's `VALID UNTIL` is fixed. **One anchor throughout: the publish time
`P` of the delivered credential.** With `slack = 24h`, `publish_budget = 1h` and
`retry_window = 6h`:

- Delivered role: `VALID UNTIL >= P + interval + delay + slack` (step 2 mints it with
  `publish_budget` of headroom so this holds for any publish inside the budget).
- Next scheduled attempt: `S = P + interval` (the publish transaction writes
  `next_run_at = S`).
- A run that starts at `t` publishes by `t + publish_budget` at the latest, and a publish
  at `T` keeps the superseded role (the delivered credential until then) valid until
  `T + delay`, because the revoke is at `T + delay` at the earliest. The overlap is intact
  only if `T + delay <= VALID UNTIL(superseded)`.
- Therefore a run may START only while `now + publish_budget + delay <= VALID UNTIL
  (superseded)`, i.e. `now <= D` with `D = S + slack - publish_budget` (the delay terms
  cancel against the `delay` already inside `VALID UNTIL`). Retries are attempted with
  bounded exponential backoff inside `[S, D]` only; the rule is "stop when the remaining
  validity of the superseded role is less than `delay + publish_budget`". When `D` passes,
  or the attempt cap is reached, the rotation moves to `halted` (`halt_reason`
  `retries_exhausted`) instead of publishing with a silently truncated overlap. The
  failure is loud: `hikyo doctor`, the metric `hikyo_rotation_halted`, and the
  credential-expiry warning.
- `extend`: when `credential_expires_at` falls within the extension margin
  (`2 * retry_window = 12h`) the executor runs the `extend` effect (the kind is already in
  the CHECK) to push the delivered role's `VALID UNTIL` out instead of letting it lapse.
  It runs only while the rotation is active and `now <= D`; a paused, disabled or `halted`
  rotation does not extend, so the loud signals are its only protection.
  A successful extend moves `VALID UNTIL` only. It does NOT move `P`, `S` or `D`, does not
  reset `attempt_count` and does not reopen the retry window: the budget is the single
  fixed `[S, D]` window of the cycle, so repeated successful extends cannot keep a failing
  rotation retrying forever. A resumed `halted` rotation starts a new cycle whose `D` is
  recomputed from the then-current `VALID UNTIL`.

Proof the schedule leaves room: `D - S = slack - publish_budget = 23h`, which is at least
`2 * retry_window` (12h) whatever `interval` and `delay` are, because both cancel. The unit
test for the executor builds the schedule for the corner cases of the bound grid
(`MinRotationInterval`/`MinRevocationDelay`, `MaxRotationInterval`/`MaxRevocationDelay`,
and the delay one second below the interval) and asserts, with the delay term included,
that `VALID UNTIL(superseded) - (D + publish_budget + delay) >= 0` (no overlap is ever
truncated by a permitted retry) and `D - S >= 2 * retry_window`; it also asserts that a
start at `D + 1s` is refused as `retries_exhausted`.

### Pause, resume, delete (S2 semantics)

- Pause blocks scheduling and blocks starting new pre-publish phases (an unpublished
  candidate is rolled back, so a paused rotation never leaves a candidate live).
  Post-publish phases always continue to completion. This is the reading of the issue
  handoff's "never leaves a candidate unrevoked" that cannot strand a credential.
- Resume re-authorizes the recorded authority and schedules the next run with
  `rotationResumeNext`: `min(now + interval, last permitted start, extendDue)`, clamped
  to at least now, so a delivered credential cannot lapse before the first run.
- Delete requires no non-terminal run, or `--abandon`. For a run before publish,
  `--abandon` quarantines the candidate, keeps the superseded role live, and is loud; the
  candidate reconciler later confirms the candidate gone and the run becomes `failed`. For
  a run past publish the candidate IS the delivered credential: it stays live, `--abandon`
  only stops future cycles, the run is closed as `quarantine`, and the credential stays
  recorded (so `credential_expires_at` still names it). The superseded role is still owed:
  the revoke-obligation loop (not the candidate reconciler) drops it after the overlap
  deadline and moves the run to `done` (see "Revoke obligation and exits from
  quarantine"). Nothing about `--abandon` revokes a delivered credential.
- A paused or disabled rotation does not extend the delivered role's `VALID UNTIL`.
  See owner decision 2 and decision 10.

### Run-now (S2)

Superseded by "S2b-1 as built" (the Run-now subsection) and, for who may call it, by
"S2b-2 as built" (run-now is key-aware: the formula is authorized on both keys, not only
environment-wide): `OpRotationRunNow` takes resume's
formula (`edit`, `publish` and `reveal` at the environment, environment-wide), a
`PurposePublish` ceremony over both keys and a required `confirm_destructive` body field
(CLI `--confirm-destructive`) on every request. `--skip-overlap` snapshots
`revocation_delay_seconds = 0` for that run only. The response lists no consumers; the revoke
outcome carries `not_converged_adapter_targets` (adapter targets only; the machine-principal
part is an open owner decision, options A to D). Event `rotation.run_now` carries `run_id`
and `skip_overlap`.

### Audit events specified for S2 (not registered in S1)

All tenant trail, `RetentionSecurity`, no credential material in any payload:

- `rotation.run_intent`: `{run_id, phase, handle}` (outcome `intent`).
- `rotation.run_outcome`: `{run_id, phase (enum), handle, error_code,
  not_converged_adapter_targets (int, optional)}` (outcomes `success|failure|unknown`); the
  revoke OUTCOME sets `not_converged_adapter_targets` to the number of consumers (machine
  principals with `read@env` whose last delivery precedes the publish, adapter targets
  whose `converged_revision < published_revision`) that may still hold the superseded
  credential. (S2b-1 counts the adapter targets only; see "S2b-1 as built".)
- `rotation.quarantined`: `{run_id, candidate_handle, superseded_handle}`.
- `rotation.run_now`: `{run_id, skip_overlap (bool)}`.

`rotation.configured` (registered in S1) carries `action`, `rotation_id`,
`environment_id`, `provider_id`, `key_id`, `username_key_id`, `interval_seconds`,
`revocation_delay_seconds`, `authority_principal_id`, `generation`, and optionally
`reason` and `credential_expires_at`.

## Bounds

| Bound | Value | Where |
| --- | --- | --- |
| `MinRotationInterval` | 1 hour | `domain.MinRotationInterval` |
| `MaxRotationInterval` | 90 days | `domain.MaxRotationInterval` |
| `MinRevocationDelay` | 5 minutes | `domain.MinRevocationDelay` |
| `MaxRevocationDelay` | 7 days | `domain.MaxRevocationDelay` |

Role `VALID UNTIL` (S2) = `mint + publish_budget + interval + delay + slack` stays inside
the PostgreSQL provider's TTL range (`internal/dynamic/ttl.go`, whole seconds up to
`math.MaxInt64/1e9`), so the largest configurable cycle (90d + 7d + 24h + 1h) never
overflows it.

## Test plan

S1 (this PR): both engines for Hikyo's own state (SQLite default; PostgreSQL when
`HIKYO_TEST_POSTGRES_DSN` is set), every actor a real signed-in session with a ceremony
window: store round trips, constraints (one live rotation per key across roles, one
non-terminal run, run and effect CHECKs, delete refused with a run in flight), tenant
isolation fixtures, bounds, create refusals (ceremony missing, protected and
approval-covered environments, machine caller, key-narrowed creator, publish-only caller,
provider missing, cross-project, tombstoned or without a credential, key missing or not
secret, out-of-bounds, duplicate), CAS conflict, resume refusals (authority lost, attributed
to the caller with the authority named; keys missing or reclassified; provider inactive),
provider delete and credential revoke pausing rotations with a reason, environment delete
refusal (naming every blocking rotation, one rotation with many blocking runs not hiding
another, an exact truncation marker, quarantine blocking) and purge on both the direct delete and
the definitions-apply path, cascade pauses on protect and on approval policy create and
update, a halted rotation's resume authorization, narrowed-authority resume and its
denial attribution under the resume operation, `RecordedPrincipalHolds` key resolution,
concurrent create of one key, local-principal refusals, paged listing with batched expiry,
credential expiry derivation (newest published run per key over a long run history,
bounded to one row per key), the run CHECK refusing every published-onward phase without a
recorded publish, `rotationDuration` above the `time.Duration` range, `include_disabled`, audit payload free of credentials,
contract admission and freeze tests. The S2 gate, not S1, owns key-deleted-underneath-a-rotation
handling during a run.

S2 additions for the hazards and obligations above:

- Delayed commit: hold the publish transaction open (its fenced `UPDATE` executed, not
  committed) in a test, run the takeover worker's recovery in another goroutine, and
  assert it BLOCKS and the fake provider has recorded no `DropRole`; commit the held
  transaction and assert the recovery sees `published`, never drops the candidate, and the
  run proceeds to overlap and revoke. The rollback variant: roll the held transaction back
  and assert the run is `failed` (committed) BEFORE the single `DropRole(candidate)`.
  Assert the ordering from the fake provider's call log and the row's commit order, and
  repeat for the ambiguous-COMMIT resolution.
- Restore hazard: after publish, `revision.restore` (and separately a manual publish) put
  the superseded value back; assert the revoke step does NOT call `DropRole(superseded)`,
  the run is `revoke_refused` with `last_error = 'published_versions_changed'`, it is
  named in status and `hikyo doctor`, only the superseded role's expiry or absence ends the hold (owner decision A);
  a restore or republish does not.
- Revoke obligation: an abandoned-past-publish run (`quarantine`, `published_revision`
  set) has its `superseded_handle` dropped by the revoke-obligation loop after the overlap
  deadline and ends `done`, its candidate role survives both loops, and the candidate
  reconciler's selection excludes it; a pre-publish quarantine becomes `failed` once
  `RoleStatus` reports the candidate absent or `candidate_valid_until` has passed;
  environment delete is refused while a run holds an unsettled obligation and succeeds
  once the loops settle it; an executor never writes `failed` for a published run.
- Slack: the bounds test of "Failure, retry and credential expiry" with the delay term;
  an extend leaves `attempt_count`, `S` and `D` unchanged; a start after `D` is
  `retries_exhausted`, never a truncated overlap.

S2: full happy path with a fake clock (mint, publish, overlap, revoke, role absent;
cursor changes exactly once per publish; Compose render picks up the value), failure
before publish (quarantine, delivered value unchanged), failure after publish (both
handles recorded, retry succeeds), kill-9 between INTENT and OUTCOME (`unknown`,
reconcile), pause mid-overlap, run-now without confirmation refused, authority loses
`publish` (pre-publish run halts with a named refusal; post-publish run still
revokes), multi-node takeover, audit payloads free of credentials, Kubernetes
operator e2e (`k8se2e`) asserting a rollout after rotation.

## Owner decisions (resolved)

1. **Ceremony at creation (Q1): required.** Create and resume consume a `PurposePublish`
   ceremony whose unit is the environment plus both stored key ids, because the rotation
   delegates publish authority to a standing worker that no ceremony will ever gate again.
2. **Pause and `VALID UNTIL` (Q2): widened.** S1 exposes `credential_expires_at` in status
   and in the pause and delete responses and CLI output, and admits the `extend` effect
   kind. S2 adds the doctor warning, the metric and the extend action itself.
3. **Username key (Q3): required.** `rotations.username_key_id` is `NOT NULL`; the dead
   nullable branches are removed.
4. **Incident containment (Q4): S2.** The incident ledger enumerates rotations by
   authority principal, never auto-resumes on release, and a failed gate moves a rotation
   to a named halted state. Nothing in S1 touches the incident ledger.
5. **Key-narrowed authority (Q5): resume adds a key-aware check on top of an
   environment-level one; pause and delete stay `publish@environment`.** Resume requires
   `edit ∧ publish ∧ reveal` at the environment with no key target (the outer
   authorization), then the caller on both stored keys (`AuthorizeKey` per key), then the
   recorded-authority re-check. A creator authorized only by a rule narrowed to the two
   keys can CREATE (create authorizes both keys as keys) but can NOT resume, pause or
   delete their own rotation: the outer environment-level step has no key and answers the
   uniform not-found. An environment-wide second person can resume such a creator's
   rotation as long as the creator still holds the formula on both keys through the rules.
   Making resume, pause and delete key-aware for narrowed creators is on the S2 checklist.
   **Superseded by "S2b-2 as built" (Key-aware pause, resume, delete, run-now):** all four
   verbs, and release-hold, now authorize the operation on both stored keys; pause and
   delete keep their `publish` formula but a one-key holder is refused. The outer
   environment-level step described here no longer decides.

## S2a as built (executor)

Code: `internal/service/rotation_executor.go` (state machine), `rotation_schedule.go`
(anchors and bounds), `internal/store/rotation_runtime*.go` (the proofless closed
`RotationRuntime`), `internal/app/rotation.go` (worker wiring beside the dynamic
worker). No migration: 00091 already carries every column the executor needs.

### Phases and effects

`minting` -> `minted` -> (stage and publish in one transaction) -> `published` ->
`overlap` -> `revoking` -> `done`, plus `failed`, `quarantine` and `revoke_refused`.
Every provider call is bracketed by an INTENT audit event plus a `rotation_effects`
row opened in the same transaction as the fenced claim write, and an OUTCOME written
when the call returns (`success`, `failure` or `unknown`). A worker that crashed with an
open effect has it closed as `unknown` (`interrupted`) by the next claimant before it
re-probes the provider; no effect is ever repeated blindly.

- `SettleRun` is the single fence: worker, lease, claim token and `phase = ANY(from)`.
  It is both the exact-row re-assertion and the fenced claim write before any
  ambiguous-state `DropRole` (rule 8). A stale worker gets `ErrRotationFenceLost`.
- The publish runs through `PublishPlanned` with the executor's hooks (begin, stage,
  commit) in one transaction; `published_revision` is stamped in that transaction.
- The overlap phase is entered by a follow-up settle. A lease that lapses between the
  publish commit and that settle leaves the run in `published`. The deadline stored with
  it was read before the commit, so recovery does not revoke on it: a claimed
  `published` run first gets its extend check, then settles into `overlap` with
  now(recovery) + `revocation_delay_seconds` (an observation after the commit) and is
  revoked only after that (note 38).
- Claim: `FOR UPDATE SKIP LOCKED` on PostgreSQL (a takeover skips a row held by an
  open publish transaction), the writer lock on SQLite. A per-organisation cap bounds
  concurrent runs. The lease term is 2 minutes.
- The executor publishes as `LocalPrincipal(authority)` after
  `Authorize(OpRotationRun)` and `RecordedPrincipalHolds` (the Gate: full before
  publish, fail-safe after).

### Schedule

`P` is the publish commit time, `S = P + interval`, `D = S + 24h - publish_budget`
(`publish_budget` 1h, slack 24h, retry window 6h, extend margin 12h). Backoff is 5m,
doubling, capped at 6h, with an attempt cap of 12. A start is allowed up to and including
`D`. A cycle with no superseded credential has only the attempt cap. Candidate
`VALID UNTIL = mint + budget + interval + delay + slack`. `extend` adds 24h at
`VALID UNTIL - 12h` while `now <= D` and no later publish exists. Retries exhausted
moves the rotation to `halted` (`halt_reason`). `TestRotationScheduleGrid` proves
`D - S >= 2 * retry_window` and `superseded VALID UNTIL >= D + budget + delay` over the
configurable grid.

### Loops

- The candidate reconciler owns `candidate_handle` of runs with `published_revision IS
  NULL`; the revoke-obligation loop owns `superseded_handle` of published runs. A
  takeover of an unconfirmed pre-publish run drops its candidate under a fenced claim.
- `superseded_handle` is derived across rotations: it is the candidate of the newest run
  that published into the key, across every rotation that ever drove the key (as its
  password key or its username key), so a recreated rotation still revokes the live role.
- Restore hazard (rule 7): before the superseded drop the executor asserts that the
  current entry ids of the delivered keys are still the ones it published; a mismatch
  holds the run in `revoke_refused` / `published_versions_changed` instead of dropping.
- Quarantine exits: probe says the role is absent or expired -> `done`; reachable and
  still present -> the drop is retried; `ErrRoleOwnsObjects` (SQLSTATE `2BP01`) ->
  `revoke_refused` / `role_owns_objects`, retried on the backoff and never lost.
- Service-level delete with `--abandon` (pause does not abandon: it leaves runs alone and
  the next pre-publish gate rolls an unpublished run back): pre-publish runs go to `quarantine` due now
  (the reconciler confirms the candidate gone); published runs keep their overlap
  deadline and the revoke loop drops the superseded role after it.

### Guards that changed

- Provider delete is refused while any run may hold a live role
  (`rotations.ProviderLiveRuns`, registered under `OpDynamicProviderDelete`).
- Environment delete treats a failed run that never published as live until a
  `drop_candidate` effect with outcome `success` is recorded.

### Audit

`rotation.run_intent`, `rotation.run_outcome`, `rotation.quarantined` are registered
(plus `rotation.configured` from S1). Abandon writes one row per run it closed.
Payloads carry ids, phase, effect kind, outcome and error codes; no credential material.
`rotation.run_now` and the `not_converged_adapter_targets` count on the revoke outcome arrived
in S2b-1 (see "S2b-1 as built").

### Test map

`internal/isolation/rotation_executor_round2_test.go` (round 2: create and resume draft
refusal, provider delete guard over a superseded role, abandon outcome row, infra-fault
counter) and `internal/isolation/rotation_executor_round1_test.go` (round 1: grouped keys, drafts,
restore assertion, resume scheduling, held-run extend, expiry exits, superseded-provider
binding, audit attribution, abandon, races, per-org cap) and
`internal/app/rotation_wiring_test.go` (worker wiring and shutdown) were added in round 1.
`internal/isolation/rotation_executor_e2e_test.go` (fake provider, fake clock, both
engines): happy path, crash table (three pre-publish points, revoke before and after
the drop), delayed commit (commit and rollback, stale-claim fence), restore hazard,
abandon past publish, quarantine exits, orphaned candidate (takeover and reconciler),
pause, authority loss, retries exhausted, slack window and extend, superseded across
rotations, provider delete guard, environment delete guard, delivery path (cursor moves
once per publish, delivered username is the candidate, revealed password matches the
provider, no password in audit or run rows).
`rotation_executor_real_pg_test.go`: a real TLS PostgreSQL target (mint, login with the
delivered credential, overlap, drop of the superseded role, a role that owns an object
refused as `role_owns_objects` and dropped after the object moves).
`internal/service/rotation_schedule_test.go`: the schedule grid, backoff and extend.

### Deviations and judgment calls

1. The executor publishes as the authority's local principal and gates with
   `Authorize(OpRotationRun)`; the S1 note said "`RecordedPrincipalHolds` only".
2. The candidate handle is `dynamic.RoleName(runID)`.
3. Password is a Go string end to end, so `crypto.Zero` is not applied to it; Option A
   (memory only, never stored) holds.
4. `extend` adds 24h to `VALID UNTIL` and now runs the pre-publish Gate minus the
   generation check (owner decision B). A failed gate halts the rotation with a named
   reason instead of extending. A held run's delivered credential is extended the same
   way (`extendDelivered`) until `D`, then the run halts `retries_exhausted` and the
   revoke proceeds with next attempt = now.
5. A new generation's cycle `D` is anchored at its first run.
6. The publish rate budget applies to the authority principal.
7. The restore hold (`published_versions_changed`) releases on either exit: the restore
   assertion passes (each key has no served entry, or its served entry was published by a
   run of that key at or after this run's revision), or the superseded role is dead. A
   superseded role that the provider reports absent or expired no longer needs the
   restore assertion: `supersededDead` self-heals the hold, and an
   expired role (`VALID UNTIL` passed by more than the 5-minute skew margin, no uncertain
   extend) skips the assertion; the drop is still attempted when the provider opens, and
   the run settles by expiry when it cannot. A restore stages new versions, so it
   can never clear the assertion (owner decision A).
8. A crashed worker's open effect is closed `unknown` by the next claimant.
9. New provider error `ErrRoleOwnsObjects`.
10. Not covered by a test: blocking claim write on PostgreSQL (SKIP LOCKED never blocks,
    so the stale-fence assertion covers it); a `startRun` id, intent or `StartRun` error
    deferring the rotation (code-reviewed only, the harness cannot induce it); an
    end-to-end infrastructure fault (the executor's keyring is concrete, so only the
    classification, the backoff curve and the store counters are tested). The
    head-of-line deferral is tested at the store level (`DeferRotation` then
    `DueRotation`), because a non-definite gate error cannot be induced in the harness.

Round 1 review additions:

11. Gate placement: the pre-start gate (`startRun`) and the extend gate (`extendDelivered`)
    each run `gateWithin` in their own write transaction as the recorded authority
    (`Authorize(OpRotationRun)` first, then `RecordedPrincipalHolds` per key). The publish
    runs `gateWithin` again inside the publish transaction's `begin` hook, with the
    run's generation. The draft check runs at the pre-start gate and at stage; the extend gate does not
    repeat it.
    Grouped keys are refused at create and resume (`ErrRotationKeyGrouped`); an authority
    draft on either key is refused at create and resume (`ErrRotationDraftPending`). A
    draft appearing on a rotated key halts the rotation `gate_failed` at the pre-start
    gate; at stage time the run rolls back. The gate's draft check runs in the same read
    transaction as the rest of the gate.
12. Gate and key-role matching: the gate accepts a key in either role of a rotation (a
    key claim binds the key, not its role).
13. Audit attribution: the actor stays the authority principal. A ctx marker
    (`audit.WithContext`: origin `system`, authority id, correlation id = run id) is used
    because `ResolveActorClass` refuses an asserted system actor under a principal-backed
    proof. Runtime-written rows use `RotationRtInsertAudit` with actor class `system`.
14. The superseded role is dropped on the provider that minted it
    (`RotationRtLoadSupersededRole`), not the rotation's current provider.
15. Infrastructure faults (datastore, keyring) record `last_error`
    `infrastructure_unavailable`, are excluded from `RotationRtFailedAttempts` and never
    spend the attempt cap. They get the same capped exponential backoff as other
    failures, driven by `RotationRtInfraFaults` (5m doubling to the 6h cap, clamped to
    `D`). A decryption failure is a definite fault (the sealed material is unreadable),
    not infrastructure, and spends an attempt. A cycle with a `D` still halts at the
    window: a start past `D` halts `retries_exhausted` even when every earlier failure
    was infrastructure. Without a `D` an outage only backs off.
16. The per-organisation cap (fewer than four live leases) gates `ClaimDueRun` and
    `DueRotation`, but it is soft: the count and the claim are separate statements, so a
    burst can exceed it by the number of concurrent claimants.
17. Refusals from the shared publish budget count as ordinary retries, so a busy
    authority can exhaust a rotation's window.
18. A failed orphan-candidate drop (`drop_orphan`) is logged and left: the role is inert
    and expires on its own `VALID UNTIL`.
19. `AbandonOpenRuns` audits only the rows it changed (RETURNING): a run left owing a
    role gets `rotation.quarantined`, and an abandoned-past-publish run with no
    superseded handle settles straight to `done` and gets a `rotation.run_outcome` row
    with phase `abandon` and error code `abandoned`. These rows are authored by the
    deleting operator (the caller), not by the rotation authority or the `system` origin
    the executor's own rows use.
20. No migration was needed: `rotation_runs` joins `rotations` for `provider_id`.
21. No Compose render test was added; `TestRotationExecutorDeliveryPath` covers delivery.
22. The "key deleted after publish" case is covered by unset-after-publish.

Round 2 review additions:

23. The authority-draft refusal now also runs at create and at resume (before the
    ceremony), as `ErrRotationDraftPending`; the executor's gate and stage checks stay.
    `OpRotationCreate` and `OpRotationResume` declare the pending-list store op.
24. Expiry exits need a 5-minute skew margin: the role's expiry is judged on the database
    server's clock, so Hikyo requires `now > VALID UNTIL + 5m` before it settles a run by
    expiry (`rotationExpirySkew`). The boundary is a unit test.
25. The provider delete guard (`RotationProviderLiveRuns`) also counts a run of any
    rotation whose superseded handle was minted on that provider and is still owed.
26. `RotationAbandonOpenRuns` lost its `hikyo:instance-scoped` exemption: the SQL
    predicate analyzer now cuts a statement's WHERE at `RETURNING` (`predicateTails`), so
    the chain columns in the WHERE satisfy it.
27. `RotationCredentialExpiries` matches a key in either role, per role as two queries
    (`RotationCredentialExpiries` and `RotationCredentialExpiriesByUsernameKey`, sqlc-sqlite
    cannot take a single `UNION` or `sqlc.slice` twice) merged in Go by `published_revision`.
28. `startRun` defers the rotation on any error (id, intent, `StartRun`), as its other
    errors do, so one failing rotation cannot sit at the head of the due queue.
29. The worker logs a fixed `stage` word for every failed iteration (`self_config`,
    `misconfigured`, `claim`, `select`, `run`, from `service.SweepError`). Self-config and
    misconfiguration log their error as is (no tenant or provider data); every other stage
    logs only `service.RotationErrorClass(err)` (an unclassified claim or select error is
    `datastore`), never the raw error.

Round 3 review additions:

30. The superseded-role branch of the provider delete guard counts only runs that
    published or may still publish (`published_revision IS NOT NULL` or phase `minting`,
    `minted`, `staged`, `published`): a failed or quarantined run that never published never revokes
    the role, so it does not hold the minting provider's delete.
31. A provider can be deleted before a later rotation supersedes its role (the guard only
    counts runs that already owe it). The revoke still works because a provider delete is a
    tombstone that keeps the sealed credential and `RotationRtLoadSupersededRole` has no
    state filter; `TestRotationSupersededRoleRevokedThroughTombstonedProvider` pins this.
32. `RotationRtInfraFaults` counts all infrastructure faults of the cycle since its
    start, not a consecutive streak: a definite failure in between does not reset it.

Round 4 review additions (CodeRabbit threads on PR #1426):

33. The superseded-role restore hold lets go when either the restore assertion passes or
    the provider reports the role absent or its `VALID UNTIL` more than the 5-minute skew
    margin past (`supersededDead` applies `rotationExpirySkew` like `roleExpired`). The
    assertion passes when each key has no served entry, or when its served entry was
    published by a run of that key at or after this run's revision. `revoke` checks the
    assertion first and consults `supersededDead` only when it fails.
34. `extendDelivered` records its extend INTENT before the `LaterPublished` check, and
    every exit that makes no provider call (superseded, window spent, inactive, gate or
    provider-open failure, a failed read) closes that intent as a `failure` with code
    `extend_skipped`. Race against a later publish: a publish that commits after the intent
    is durable runs its revoke read afterwards, so the read counts this extend as
    uncertain (`extend_uncertain=1`) and the role is judged by the provider, not the
    recorded `VALID UNTIL`; a publish that committed before the check is seen by the check
    and ends the extend. The window between the intent commit and the provider call is
    therefore covered either way. A closed-without-call intent has outcome `failure`, which
    `RotationRtLoadSupersededRole` does not count, so the superseded role is not left
    uncertain. Crash table: a worker that dies between the intent and its close leaves an
    open intent that the next claimant closes `unknown` (`staleEffect`); that role stays
    uncertain, which is the safe side (the revoke asks the provider). The close-write
    failing after a read error has the same outcome. The race window itself is not
    forced in the harness (no hook between the intent commit and the supersession read),
    but the ordering is pinned: the provider is opened after the supersession check and
    the gate, and `TestRotationExtendIntentIsDurableBeforeTheProviderOpens` observes an
    open extend effect at that moment (it fails with the intent moved back after them).
    The superseded exit is covered by
    `TestRotationExtendSkippedBySupersessionClosesItsIntent`. A held run revisited past
    extend-due with a provider-open or non-definite gate failure now writes an intent
    and an `extend_skipped` outcome on each revisit (bounded by the backoff and by D);
    it wrote nothing before.

35. `RotationAbandonOpenRuns` keeps a pre-publish run's lease owner and ends its lease at
    the abandon time (a run nobody held keeps no lease). The token bump stops the worker's
    settles but not a provider call it already started, so the claim's lease cut (one
    provider deadline past the lease end) holds the candidate reconciler back until the
    client has stopped waiting on that call. The cut bounds the client's wait, not the
    server: the PostgreSQL provider therefore sends its `CREATE ROLE` behind a server-side
    `statement_timeout` of three quarters of the time left, so the server aborts a blocked
    mint first and answers SQLSTATE 57014, a definite refusal
    (`TestProviderMintIsBoundedOnTheServer`, real target). What remains: a server before
    PostgreSQL 13 starts that timer once per message, and a partition during COMMIT leaves
    the outcome ambiguous. In those cases a mint the client abandoned can still commit
    after the reconciler probed the role absent and settled the run, leaving a role no
    run tracks. It cannot be used: its password was generated in the worker's memory and
    is published only after a successful mint, and `VALID UNTIL` expires it. No sweep drops
    it before then; `dropOrphan` and the reconciler's drop cover only a role that exists
    when they run. A published run keeps the null lease: its revoke and the done settle
    make no provider call a late candidate could orphan.
    `TestRotationAbandonDelaysTheCandidateReconcileByAProviderDeadline` pins it.

36. `StartRun` re-reads the org's live-lease count (`RotationRtOrgLeaseCount`, cap
    `RotationOrgLeaseCap` = 4) inside the creation transaction and starts nothing at the
    cap; the rotation stays due and is selected again. The selection read stays as the
    cheap pre-check. Race safety: SQLite runs one writer at a time; the PostgreSQL
    transaction is SERIALIZABLE (`dbTransaction`), so two starts that both counted below
    the cap form a read-write cycle and the second to commit fails with a serialization
    error, which `startRun` already treats as a start failure and defers
    (`deferDue`). `TestRotationStartRunRechecksTheOrgLeaseCap` fails before the fix;
    `TestRotationConcurrentStartsRespectTheOrgLeaseCap` pins the invariant (at most 4
    live leases, at least one start wins the free slot, and a losing start fails only
    with a transaction conflict: PostgreSQL 40001 or 40P01, SQLite BUSY or LOCKED) but
    cannot force the interleaving, so it also passes when the starts happen to serialize.

37. The user docs now state three limits: rollback and pause ATTEMPT to delete an
    unpublished role and a failed or unknown drop stays tracked for retry
    (`dropCandidate`), so a pause is not proof of cleanup; the intent and outcome events
    cover tracked effects only; `dropOrphan` (a worker that lost its fence while the mint
    was in flight) may call `DropRole` with no intent or outcome, so a missing event does
    not prove that no provider call was made. The orphan call is best effort by design:
    the worker that took the run owns the drop under its own events.

38. Publish clocks. The publish transaction reads the clock for a rotation at its start
    (authorization, and the `begin` budget check, which runs before the project lock),
    right after the project lock (the publish stamps, and the budget check `stage` and
    `commit` apply), and again at the end of the commit hook, on a fresh executor clock
    read. That last read is checked against the budget too, so a publish whose staging and materialization outlast the budget rolls
    back instead of committing, and it anchors the overlap deadline stored with the
    `published` phase. The settle into `overlap` after the commit reads the clock again
    and anchors the deadline there, never earlier than the hook's read, so consumers keep
    the whole revocation delay after the credential is durable. If that settle does not
    happen (a crash, or a lease that lapsed during a slow commit), the run stays
    `published` with the hook's deadline, which was read BEFORE the commit and so can be
    earlier than commit + delay by the commit latency. Recovery therefore never revokes
    on it: a claimed run in `published` is settled into `overlap` with
    now(recovery) + `RevocationDelaySeconds`, an observation after the commit, and only
    that overlap deadline gates the revoke. `followState` (an ambiguous commit that did
    commit) does the same. The cost is one extra delay on that rare path.
    `rotation delete --abandon` of a run still in `published` gets the same treatment in SQL: the abandon statement sets
    its `next_attempt_at` to the abandon time plus the rotation's revocation delay
    (`published_due_at`, computed in Go and passed to both engines), because the quarantined
    run's revoke-obligation loop would otherwise drop the superseded role at the
    pre-commit value (`TestRotationAbandonOfAPublishedRunWaitsAFullDelayAfterTheCommit`).
    The rotation's delay is fixed at creation and copied to each run, so it is the run's
    delay; a run abandoned from `overlap` or later keeps its (post-commit) deadline. No
    other path reads a published run's stored deadline except the claim: extend and the
    restore hold use the current clock. The recovery branch follows the extend step, so a credential due for extension is extended (or the
    rotation halts by name) before the wait starts. The wake is clamped, the deadline
    is not: a settle into `overlap` (publish, recovery, `followState`) stores the revoke
    deadline in `rotation_runs.revoke_not_before_at` (migration 00092) and parks the run
    at `rotationOverlapWake`: the deadline, or the credential's extension margin when
    that comes first, never before now. `revoke` has no deadline check of its own (the
    wake and the deadline were the same column), so a clamp alone would extend on the
    early visit and then fall through to the drop; instead `revoke` compares the stored
    deadline after the extend step and re-parks (to the next margin, or the deadline)
    without revoking. A quarantined run (abandon) has no extend step, so it waits for
    the stored deadline itself; the abandon of a `published` run needs no clamp because
    that run is never extended. A NULL column (a run from before the migration, or one
    that never settled into overlap) means the wake is the deadline. Tests:
    `TestRotationRecoveredOverlapIsVisitedAtTheExtendMargin` (visited at the margin,
    extended once, parked at the deadline, dropped only after it) and
    `TestRotationOverlapClampDoesNotShortenTheDelay` (an abandon during the wait keeps
    the stored deadline; no drop at any visit before it). Both fail, dropping early, if
    the comparison is removed and only the clamp is kept. The delay is below the
    interval, so the clamp only bites a recovery that is late by more than the interval
    plus 13 hours. Other tests:
    `TestRotationOverlapDeadlineFollowsPublicationTime`,
    `TestRotationOverlapDeadlineCountsWorkAfterTheLockRefresh`,
    `TestRotationPublishBudgetRechecksAtCommit` (the clock jumps past the budget after
    the stage check) `TestRotationOverlapDeadlineIsCommitTimePlusDelayAfterLateWork` and
    `TestRotationPublishedRecoveryWaitsAFullDelayAfterTheCommit` (the clock moves 20
    minutes between the hook's read and the commit, the settle is lost, and no revoke may
    happen before commit + delay). The harness has no hook between the commit and the
    settle, so the post-commit read itself is not separately exercised.

39. Extend intent ordering. `extendDelivered` records its INTENT before the
    `LaterPublished` check, so a replacement run that publishes after that check (and so
    is invisible to it) still has its revoke read count the extend as uncertain
    (`extend_uncertain` = 1) and never settle the superseded role as expired.
    `RotationExecutor.AfterExtendSupersessionCheck` is the conformance seam (nil in
    production) between the check and the rest of the extend;
    `TestRotationExtendIntentCoversAPublishAfterTheSupersessionCheck` publishes a
    replacement there and fails if the intent moves after the check.

### Owner decisions on the S2a review (resolved)

- A. A restore never clears the restore-hazard hold by itself: the hold ends only when the
  assertion passes again (see note 33) or the superseded role is absent or expired
  (fail-safe). S2b adds a loud status showing the deadline plus an
  operator action to release it.
- B. `extend` gets the pre-publish Gate minus the generation check. A failed gate halts
  with a named reason instead of extending.
- C. Incident containment by authority principal and key-aware resume, pause and delete
  stay in S2b. **S2b must ship in the same release as S2a**: until it lands, an
  operator has no run-aware pause or delete and no `restore_hold` release.

### What S2b must wire

Done in S2b-1: run-now, runs inspection, `--abandon` on the wire and the user docs (see "S2b-1 as built"). Built in S2b-2 (see "S2b-2 as built"): metrics, doctor, the `restore_hold` column and
release, and key-aware pause, resume, delete and run-now. Still open: ledger enumeration of
rotations, owner decision B, built in a follow-up branch after this PR merges (see "Owner
decisions on S2b-2").

- Parity rows (S3 flips them).
- The machine-principal part of the consumer report, if the owner picks option B or C
  below.

## S2b-1 as built (run-now, runs, abandon wire)

Migration 00093 (both engines): `rotations.manual_run_id`, `rotations.manual_skip_overlap`
and `rotation_runs.not_converged_adapter_targets`. API revision 18 (additive).

### Run-now

- `OpRotationRunNow` (resume's formula: `edit`, `publish` and `reveal` at the environment,
  `postGrantForbidden`, wire route `POST .../rotations/{rotation}/run-now`, 202 with the
  pre-generated `run_id`). The service authorizes, requires a human session and the body's
  `confirm_destructive: true` (a body without it, or with false, gets the named refusal, enforced server-side on every request; no body at all is a plain 400),
  checks generation and active state (a paused, halted or deleted rotation is refused), checks
  there is no open run, consumes the publish ceremony over both keys, then
  writes the request (`manual_run_id`, `manual_skip_overlap`, `next_run_at = now`) and the
  `rotation.run_now` audit row (`run_id`, `skip_overlap`) in one transaction. A request that
  is already pending is refused by the store write, after the ceremony; the refusal rolls the
  whole transaction back, the ceremony consumption included, so it costs no reauth. It starts
  nothing and holds no provider handle. There is no per-key `AuthorizeKey` after the
  environment check: a key-narrowed rule never satisfies an operation that passes no key and an
  environment-wide holder passes for every key, so it could never deny. Like resume, run-now
  therefore needed the formula environment-wide in S2b-1; S2b-2 made run-now, pause, resume and
  delete key-aware (see "S2b-2 as built"). The request resets `next_run_at` to now, so it also
  overrides a retry backoff; the attempt cap and the retry deadline still apply.
- The executor's due read exposes the request; `startRun` uses the pre-generated id with
  trigger `manual` through the same Gate, window and attempts checks as a scheduled run.
  `StartRun` re-asserts the locked manual fields and clears them in the run-insert
  transaction. Pause (user and system), resume, delete and halt clear a request that has not
  started. A manual run whose Gate fails halts the rotation like a scheduled one and clears
  the request.
- `--skip-overlap` snapshots `revocation_delay_seconds = 0` on that run only. The candidate
  `VALID UNTIL` still uses the rotation's delay, because that role becomes the next run's
  superseded role and must outlast that overlap. `RotationRun.RotationDelaySeconds` (the
  rotation's own delay) feeds the halt audit and the windows so a skip run cannot shorten them.
- The open-run check is the service's: it counts before the ceremony and again after the
  request write, and rolls the request back (`ErrRotationRunInFlight`) if a run opened in
  between. The write transaction is serializable (retried on a serialization failure on
  PostgreSQL, `BEGIN IMMEDIATE` on SQLite) and the request UPDATE holds the row the executor
  locks to start a run, so a racing run is caught either by the recount or by the
  serialization retry. No interleaving test: the window needs a concurrent transaction.

### `not_converged_adapter_targets` (adapter targets only, option A accepted)

The field was named `not_converged_consumers` while it was designed; it is renamed because it
counts adapter (sync) targets only, never "delivery targets" (that term is the Kubernetes
operator's). A zero does not prove that machine consumers are unaffected. The run-now
response carries no consumer list (nothing has been revoked when it answers).

Smallest truthful definition from what the code can know: the count of non-tombstoned
`adapter_targets` that mirror either rotation key (`adapter_target_keys`) with
`converged_revision IS NULL OR < run.published_revision`, computed just before the provider
drop in `revoke()`. It is emitted on the revoke `rotation.run_outcome` payload and stored in
`rotation_runs.not_converged_adapter_targets`, for every revoking run (a run with no superseded
role has none). Machine principals reading keys directly are not counted: no per-principal
delivery record exists, so the spec text "machine principals with `read@env` whose last
delivery precedes the publish" cannot be computed. Options for that part:

- A: keep adapter targets only and document it (as built).
- B: add per-principal last-delivery tracking (a migration plus a write on every value read).
- C: proxy through `delivery_target_reports` and revision pins.
- D: drop the machine-consumer wording from the spec and the field name's promise.

### Runs inspection

`OpRotationRuns` (`read@environment`, audited-none bare read), `GET .../rotations/{rotation}/runs`
with `limit` and `after`, newest first by run id (UUIDv7 ids are time-ordered; a manual run's id
is minted at request time and it is the first run to start after the request). Items carry id, trigger, phase, published revision, revocation delay,
timestamps, `not_converged_adapter_targets` and the fixed-word `error_code`; never a handle,
password or provider detail (the run id is the lowercased role name, public metadata). No run-detail route (the ADR family does not declare one).

### Abandon on the wire

`DELETE .../rotations/{rotation}?abandon=true` and `hikyo rotation delete --abandon` pass the
service's abandon. Abandon is human-session only (`requireRotationHuman`, like create, resume
and run-now); it keeps its per-run audit rows (`rotation.quarantined` or a `rotation.run_outcome`
of phase `abandon`). A run that has not published is quarantined, and the candidate reconciler
drops its candidate later; nothing is revoked at abandon time. Plain delete without a run in
flight is unchanged.

### Judgment calls and deviations

1. `--confirm-destructive` is required on every run-now, as the issue and test plan say. The
   server enforces it through the required `confirm_destructive` body field (the
   `confirm_protected` pattern) with the named refusal `ErrRotationRunNowUnconfirmed`; the CLI
   always requires the flag and sends the field.
2. The run-now response cannot list consumers (no run exists yet, and nothing was revoked). The
   report lives on the revoke outcome row and in `runs`.
3. A pending request is not shown by `rotation show`.
4. `rotation.runs` and `rotation.run_now` are human-session only on the wire like the rest of the family.

### Still S2b-2

Done; see "S2b-2 as built". S2b-2 must ship in the same release as S2a and S2b-1.

## S2b-2 as built (operability)

### Metrics and doctor

`hikyo_rotation_runs_quarantined`, `_runs_unknown_outcome`, `_runs_revoke_refused`,
`_halted`, `_restore_held`, `_overdue` and `_credentials_expiring`, with
`hikyo_rotation_gauges_known`. All label-free, from one `RotationRtGauges` read (instance
scope, one series each; the conformance bound registry pins `MaxSeries: 1`). Overdue means an
active, unheld rotation more than `RotationOverdueGrace` (15 minutes) past `next_run_at` with
no run open, measured from the first due time (`rotations.deferred_from`, set only when a
datastore fault defers the start, cleared by every real schedule write) so a deferral cannot
hide it; expiring means a delivered credential whose `credential_expires_at` is in the next
`RotationExpiringWithin` (6 hours) and not yet past, for any state but deleted. A lapsed
credential is not counted (it halts or overdues its rotation instead); a separate "expired"
gauge was left out as unneeded. The counts are read once per scrape, by the metrics
collector; the audited retention health read (doctor) reads them with a 2 second bound.
A partial index (`rotation_runs_env_published`, migration 00094) backs the expiring
subquery. Doctor reports counts only and names no tenant or rotation id, a deliberate
deviation from listing the affected ids: it tells the operator to run `hikyo rotation list`
(halted, held, overdue) and `hikyo rotation runs <rotation>` (quarantined, refused, unknown
outcome). **Accepted as recommended (owner, round 5).** `hikyo doctor` has one `rotation`
finding fed by `RetentionHealth` (new optional fields, omitted when unmeasured, so doctor
says `unknown`, never ok).

### Key-aware pause, resume, delete, run-now

`authorizeRotationKeys` authorizes the operation on both stored keys. An environment-wide
holder passes both keys, so nothing narrows for them; a key-narrowed holder passes exactly
when they pass for each key, like the create that made the rotation. The rotation is looked
up under a proof chosen by the same evaluation `Authorize` makes, member rules included
(round 2: the round-1 choice used `CallerHolds`, which reads grants only, so a person whose
read and operation come only from member rules got not-found with no audit row). The probe is
`TxAuthorizer.AuthorizeFirst` (round 3 folded the round-2 `AuthorizeUnrecorded` primitive into it and
unexported the primitive, so a caller cannot refuse quietly; shared `CallerHolds` is unchanged, it
has other callers): it authorizes the first of the given operations the caller holds, without
recording a refusal of the earlier ones, and when all refuse it records one denial under the first. The order is the operation's own environment-wide proof when held (so a publisher
without read can pause), else the inspect proof (how a key-narrowed holder finds the keys),
else the operation itself is authorized so the refusal is recorded once under it. Every
refusal, an absent rotation included, answers the same not-found and is recorded the same
way (one denial under the operation, none under `rotation.inspect`): a caller who reached the
rotation through inspect only is recorded under the operation whether the rotation is absent
or one of its keys refuses them, so neither the answer nor the audit trail is an existence
oracle. A caller who holds the operation environment-wide passes both keys, so a key refusal
cannot occur for them and there is nothing to tell apart.
**Remaining deviation:** a person who holds the operation only through key-narrowed rules and
lacks read cannot look the rotation up, so they are refused (denied under the operation).
Options: (A) accept, as built; (B) an authz primitive that resolves a rotation's keys inside
authz (a new registry concept, not a wider `SiteScheduler`); (C) require read for key-narrowed
rules. Recommendation: A. Also: a key-narrowed holder cannot pause or delete a rotation whose
key was deleted (the key lookup answers not-found); an environment-wide holder can.
`TestRotationTransitionsAreKeyAware`, `TestRotationTransitionLookupNeedsNoRead`,
`TestRotationRuleOnlyMemberTransitions` and `TestRotationReaderRefusalIsIndistinguishable` pin it.

### Restore hold

- Migration 00094 adds `rotations.restore_hold` (both engines). `CompleteRestore` calls
  `HoldRestoredRotations`: every non-deleted rotation, paused and halted included, is held.
  The closure runs against the archive's schema before migrations, so it probes the table
  and the column; an archive from migrations 00091 to 00093 (nightly tags such as
  `v1.1.0-nightly.20261111` already contain 00091; no stable release does) has no column,
  so only its **active** rotations are paused, and that pause bumps the generation. Its
  paused and halted rotations restore **unheld**, exactly as they were. The hold also clears
  a queued run-now (`manual_run_id`, `manual_skip_overlap`), so releasing never fires a
  skip-overlap revoke of a role still serving; the rotation stays due, so the first run
  after release is an ordinary scheduled one. Delete clears the hold in the same write
  (`RotationSetState`), so no reader sees a deleted rotation held.
- Audit: a restore writes one instance event, `restore.completed`, for the whole act. The
  hold is not a per-rotation decision (it applies to every non-deleted rotation, by the local
  host authority), so it adds no per-rotation row; each release is audited
  (`rotation.configured`, `hold_released`) and the held state is visible in `show`, `list`
  and the gauge. If an owner wants a per-rotation restore row, that is a follow-up.
- A held rotation is a non-active one to the executor (projected into the run's
  `RotationState`): no start, no pre-publish step, no extend, no candidate drop; a run past
  its publish continues to its revoke. The stored `state` is unchanged,
  so resume cannot bypass the hold: resume and run-now refuse with `ErrRotationRestoreHeld`;
  pause and delete are allowed. The Gate refuses a held rotation.
- `rotation release-hold` (`rotation.release_hold`, `POST .../release-hold`): human only,
  resume's formula (edit, publish and reveal) on both keys through `authorizeRotationKeys` (so key-narrowed operators can run
  it), the publish ceremony, an optional generation compare-and-set, audited as
  `rotation.configured` with `action: hold_released` (no new event type). It lifts the hold
  only. It does not resume, does not drop a superseded role, does not touch a run and does
  not bump the generation (an in-flight run asserts the generation it started under). For an
  **active** held rotation, before the ceremony is consumed it runs the Gate's whole check
  set (`requireRotationStartable`: environment unattended, provider active with a credential,
  keys present and fitting, the recorded authority holding the formula on both keys, no
  draft staged by the authority), refusing with the Gate's named errors, so a release never
  hands the executor a rotation whose first Gate halts it. A **paused or halted** held
  rotation is released without that preflight: release only lifts the hold, and resume
  re-runs every check, so a provider that was revoked cannot trap the hold. Denials of the
  authority check are recorded under `rotation.release_hold`, not `rotation.resume`.
  A run found in flight while the rotation is held is not recovered at all (round 5, owner
  decision B); the recovery code still records `rotation_paused` for a paused rotation.
- `rotation show`, `list` and `runs` print a `RESTORE HOLD` line on stderr with the
  `credential_expires_at` deadline; the JSON carries `restore_hold`.
- Observed: a hold that outlasts the delivered credential's window makes the first run after
  release halt (`retries_exhausted`); the operator resumes after checking the credential.
- API: `restore_hold` is a **required** field of the `CredentialRotation` response, so a
  revision 19 SDK talking to a revision 18 server fails to parse the rotation. Accepted:
  rotations have not shipped in a stable release and the nightlies that carry them move with
  the server; making it optional would hide a held rotation from an old client.
- Run records gain `credential_valid_until` (additive, optional): the expiry of the role the
  run still owes a drop, and `credential_valid_until_lower_bound` (true only when set). For
  `revoke_refused` and for a quarantine run that published, that is the superseded role's
  expiry, found by chain and handle (so it survives a recreated rotation); the candidate is
  then the serving credential and is never named. For a quarantine run that never published
  it is the candidate's expiry. The flag is true when an `extend` of that role has an open
  or unknown outcome: the recorded time is then a lower bound, the role may outlive it, and
  the CLI says "at least ... and possibly longer". The expiry is resolved per owed run
  (extra reads, only for those phases), which keeps `RotationListRuns` a single-table query
  the predicate analyzer proves, so its `instance-scoped` annotation is gone. `rotation runs`
  and `rotation show` print an `ATTENTION` line per such run; `show` pages through every
  run and warns on stderr, without failing, when the runs cannot be read or the history
  exceeds 20 pages of 500. The run-level release action stays unbuilt.
  A run refused as `published_versions_changed` (a restore put an earlier value back, so the
  keys may serve the superseded role again; the executor deliberately keeps the role) gets a
  different `ATTENTION` line: do **not** drop it at the provider until the keys have moved off it.
  A quarantine run that published can reach the same hold. `role_owns_objects` and
  `provider_unavailable` keep the drop advice, now with the same caution (review round 4):
  check that neither key serves the role first, because a restore after the refusal can
  put it back and no flag records that.
- Tests: `TestRotationRestoreHold`, `TestRotationRestoreHoldFallbackPausesWithoutTheColumn`,
  `TestRotationReleaseLeavesARefusedRunAlone` (the run-level hold stays untouched).

### Review round 2 (as built)

- Publish rollback records `rotation_held` (a distinct sentinel, `errRotationHeld`) when the
  Gate refused because a restore holds the rotation, and `rotation_paused` for a pause.
- The doctor's rotation gauges are read through the `RotationGauger` interface, so a test
  pins that `OperationalHealth` reads them once and within the 2 second bound.
- **Rebuild note (migration 00094).** Rounds 1 and 3 edited migration 00094 in place (it adds
  `rotations.deferred_from`, and in round 3 the `rotation_runs_minter` index). A development or
  preview database that applied an earlier version of 00094 lacks that column or index and
  **must be rebuilt**; no stable release carries the
  migration, so nothing shipped is affected.
- `release_hold` storeOps gained the approval-policy and pending-list reads the Gate's checks
  use for an active rotation.
- Tests: `internal/isolation/rotation_round3_test.go`, the CLI `credential_rotation_owed_test.go`
  and the server `TestRotationRunBodyMapsTheExpiry`.
- Not covered over HTTP: wire ids must match `^[a-z]{2,8}_[0-9a-fA-F-]{36}$`, which the
  isolation fixtures' short ids do not, so the transport leg of the human-only test uses
  absent well-formed ids (status 404 or 403) and the service leg proves the refusal.

### Review round 3 (as built)

- The `ATTENTION` line branches on `published_versions_changed` (above); openapi `error_code`
  and `credential_valid_until`, `rotation.mdx` and `credential_rotation_owed_test.go` say the same.
- `TxAuthorizer.AuthorizeFirst` replaces the exported `AuthorizeUnrecorded`; the unrecorded probe
  is private to it, so the "must then record the refusal" rule is enforced by the method, not by
  a comment. `tx_authorizer_surface.json` follows.
- `TestRotationReleaseHoldMatchesResume` (authz registry test) pins release-hold's formula,
  post-grant reauthentication and self-config membership to resume's: the Gate checks the recorded
  authority under resume's formula, so the two rows change together.
- `TestRotationReleaseHoldUsesTheGate` now also asserts the ceremony window is not spent by a
  refused release (the sliding window's expiry does not move; the successful release moves it) and
  that exactly one `rotation.release_hold` denial is recorded.
- **Expiry lookup index.** Migration 00094 gains `rotation_runs_minter (org_id, project_id,
  environment_id, candidate_handle)` on both engines (edited in place, unreleased; the
  rebuild note of round 2 covers it, and `development.json` is regenerated). The superseded
  role is found by handle, and the revoke step's own role lookup (`RotationRtLoadSupersededRole`)
  uses the same join, so neither scans a chain's runs any more. A run leaves `quarantine` after a
  successful drop: a pre-publish one settles to `failed` once its candidate drop is confirmed and a
  published one to `done` once its revoke succeeds, so the per-run lookups of `rotation runs` and
  `show` are bounded by the runs whose drop keeps failing.
- **Extend racing a restore hold (confirmed, fixed).** The claim skips held rotations, but a hold
  landing between the claim and the Gate made `extendDelivered` treat `errRotationHeld` as
  inactive and park the settled run at "never", so nothing extended the delivered credential after
  the release. A held refusal now leaves the run due (`NextAttempt` now); the claim still skips it
  while held. `TestRotationExtendRacingARestoreHoldIsRetriedAfterRelease` failed before the fix (the
  run parked at year 9000) and passes after.
- **Published `failed` runs.** Not reachable by any writer: the only writers of `failed` are the
  pre-publish rollback and the candidate reconciler (both `published_revision IS NULL`), a published
  run that cannot drop its superseded role stays in its phase (`revoke_refused`, `quarantine`, or
  the executor phases) and a successful revoke settles `done`, and abandon sets `done` or
  `quarantine`. The SQL still treats a published `failed` run as possibly owing a role (environment
  delete, the claim), so the CLI is left flagging `revoke_refused` and `quarantine` only: flagging a
  published `failed` run would also flag one whose revoke already succeeded, which the run record
  cannot tell apart.

### Review round 4 (as built)

- **Runs read survives a failed hold read.** `rotation runs` reads the rotation after the runs;
  when only that second read fails it renders the runs and warns on stderr that the hold status
  is unknown (`TestRotationRunsRenderWhenTheHoldStatusCannotBeRead`).
- **Every manual drop carries the key check.** The `ATTENTION` line of a `role_owns_objects`,
  `provider_unavailable` or unflagged quarantine run now says to check that neither key serves
  the role before dropping it, because a revision restore after the refusal can put it back and
  nothing flags that. `published_versions_changed` keeps its stronger "do NOT drop". openapi,
  `rotation.mdx` and the owed-run test follow; the reassign-owned-objects instruction stays.
- **Release-hold transport test is exact.** `TestRotationReleaseHoldRefusesNonSessions` pins a
  404 for the person, the workload machine and the automation credential on an absent rotation
  and fails if the answers differ; none of them gets a 403 there (observed on sqlite and
  PostgreSQL).
- **Key-narrowed authority (confirmed, fixed).** The Gate and the three publish callbacks
  authorized `OpRotationRun` with no key, which a key-narrowed rule never satisfies, while
  create, resume and release-hold accept such an authority through `RecordedPrincipalHolds` on both
  keys. A rotation created by a narrowed authority therefore halted `authority_lost` at its first
  due time. `authorizeRotationRun` now authorizes `OpRotationRun` on both of the rotation's keys
  (`AuthorizeKey`), as the recorded authority itself; an environment-wide holder passes for every
  key as before, and no identity is synthesized. `TestRotationExecutorRunsAKeyNarrowedAuthority`
  failed before the fix (`authority_lost`) and publishes and settles after it.
- **Hold landing between claim and publish (confirmed, fixed).** The run is loaded before the
  hold, so `failureSettle` still saw an active rotation: its retry plan could return a halt, and
  the halt, schedule and defer statements matched `state='active'` only, so a held row was
  halted (a release does not undo a halt) or rescheduled. `RotationRtHaltRotation`,
  `RotationRtSetNextRun` and `RotationRtDeferNextRun` now also require the hold to be absent, on
  both engines; a halt that matches no row writes no halt audit. The same predicate makes the
  start transaction's schedule write refuse (conflict, rolled back) when a hold lands after the
  Gate, so no run starts under a hold. Left alone on purpose: the human verbs `pause`,
  `set state` and `run-now` read the hold in their own transaction. Consequences: a publish that
  commits after a hold does not re-arm the schedule, so a release finds the rotation due; and the
  run the hold interrupts is a failed attempt like any other (`rotation_held` counts toward the cap).
  `TestRotationHoldDuringAClaimedRunLeavesTheRotationAlone` (retry and would-halt cases) failed
  before the fix.

### Review round 5 (as built)

- **Hold landing between stage and commit of a publish (not reproducible; guard documented).**
  The begin callback checks the hold, and the commit callback checks only authorization, but the
  commit's last write is the schedule write on the rotation row, which targets an active, unheld
  row. Under PG serializable a hold that lands after the begin check conflicts with that write, the
  publish transaction aborts, and the run settles as held (`rotation_held`), not halted; sqlite's
  single write connection cannot interleave a hold. A negative control (removing that write) made
  the new test publish a held rotation, so the guard is real but was incidental: it is now named in
  `RecordRunPublish`, and `TestRotationHoldDuringThePublishTransactionRollsItBack` (PG, hold
  landing at the baseline read) pins it. No separate hold read was added: the existing write
  already is the serialization guard, and the pause and delete publish behavior is untouched.
- **Extend gate: hold classified before the halt reason (confirmed, fixed).** The gate authorizes
  before it reads the rotation, so a held rotation whose recorded authority was also refused came
  back as a definite halt reason. The hold blocks the halt UPDATE (round 4), but the done run was
  still settled at year 9000 and a release never re-arms it, so the delivered credential was never
  extended. `extendDelivered` now reads the hold (`RotationRuntime.RestoreHeld`, new query
  `RotationRtRestoreHeld` on both engines) before choosing `next` for a definite refusal and keeps
  the run due like the `errRotationHeld` branch. `TestRotationExtendHeldAndAuthorityRefusedStaysDueAfterRelease`
  failed before the fix on both engines (`next_attempt_at` 9000-01-01, 0 extends after release).
  Siblings audited: the other `rotationNever` writers are hold independent (a superseded or
  window-spent credential, a failed run's finished drop) or unreachable for a held rotation (the
  claim skips held rotations, so a claimed run's state is never the held state); the pre-start gate
  in `startRun` halts a held rotation's due row, which the hold also blocks, leaving the row due
  for the release.
- **Hold decided in the settle transaction (confirmed residual, fixed).** The `RestoreHeld` read
  and the settle that follows were separate transactions, so a hold landing between them still
  parked the done run at "never" while the halt refused the held rotation. `RotationSettle` now
  carries `DueIfHeld`, which `settleDrop` sets for every done-phase settle that parks at
  `rotationNever` (extend parks, skips and halts alike). `RotationRuntime.Settle` then reads the
  hold last, in the transaction that wrote the settle and tried the halt, and leaves the run due
  now (`RotationRtDueRun`, both engines) when the rotation is held. Under PG serializable a hold
  committed after the transaction began conflicts with the halt write and fails the transaction
  (`dbTransaction` has no retry, so the executor does not retry it: the error goes to the sweep, the
  run is reclaimed after the release plus the lease term, and `staleEffect` then closes the open
  extend INTENT as unknown, the safe direction) and one committed before it is seen by the read, so
  the held decision and the halt agree; sqlite has one writer. The earlier read stays as the fast path. The run stays due, the claim
  skips the held rotation, and a release re-evaluates the gate. A nested SELECT inside the settle
  UPDATE was tried first and refused by the SQL predicate analyzer, hence the in-transaction read.
  `TestRotationExtendHoldLandingAfterTheHoldReadStaysDueAfterRelease` (seam `AfterExtendHoldRead`)
  failed before the fix on both engines (next attempt 9000-01-01, 0 extends after release).

- **Candidate drops while held (owner decision B, built).** Every path that issues `DROP ROLE`
  for a candidate was checked: `dropOrphan`, `dropCandidate` (the rollback of `recoverInFlight`
  and of a refused publish), the reconciler's drop in `reconcileCandidate`, and the superseded
  revoke. The revoke is unchanged. The other three now read the hold first and issue no drop for
  a held rotation. The claim query no longer selects `minting`, `minted` and unpublished
  `failed`/`quarantine` runs of a held rotation (both engines), so the run and its role are left
  untouched until `release-hold`; `recoverInFlight` and `reconcileCandidate` also read the hold
  themselves for a hold landing after the claim, and the drops re-read it just before the
  provider call (`leaveCandidateHeld` closes an opened INTENT as a failure with `rotation_held`
  and leaves the run due). The `rotation_held` recovery label is gone with the recovery: a
  held rotation recovers nothing. After the release the run is due and the normal rollback and
  drop happen. Tests: `TestRotationHeldRotationDoesNotRecoverInFlightRuns` (minting and minted),
  `TestRotationHeldRotationDoesNotReconcileCandidates` and
  `TestRotationHoldDuringARunSkipsTheRollbackDrop`, each failing before the change on both
  engines. The residual window is a hold landing between the final read and the provider call.

### Review round 6 (as built)

- **Delete and abandon refused while held (owner decision to confirm, default A).** `RotationSetState`
  clears `restore_hold` when the state becomes `disabled`, and `delete --abandon` turns minting and
  minted runs into quarantine in the same transaction, so the reconciler (its claim has no state
  filter) found the rotation unheld and dropped a candidate the live instance may have published,
  which undid decision B. A plain delete also succeeded when only unpublished failed or quarantine
  runs existed (`CountOpenRuns` ignores them). The service now refuses both with
  `ErrRotationRestoreHeld` ("release it with release-hold") before it counts or abandons anything,
  so nothing changes: no state, no run, no audit row. Pause stays allowed while held; delete and
  abandon are refused until release. The clear on disable stays and is now unreachable for a held
  row. Tests: `TestRotationDeleteRefusedWhileRestoreHeld` (delete and abandon, run left minting),
  `TestRotationDeleteSucceedsAfterRelease`, `TestRotationPauseAllowedWhileRestoreHeld` and
  `TestRotationDisabledRowNeverKeepsAHold`, the first two failing before the change on both engines.
  **Owner decision, flagged:** A (built): refuse until `release-hold`. B: keep the hold through
  the delete and let `release-hold` accept a deleted rotation (needs the claim to filter on state or
  the release to settle the runs, and a hold on a disabled row, so a larger change). Reversal of A
  is the one `cur.RestoreHold` check in the delete branch.
- **Release triggers the candidate drops (docs, CLI).** After `release-hold` the held runs are due
  at once and the executor rolls back or reconciles them, dropping an unpublished candidate the live
  instance may have published. The earlier wording that the unused role "lingers until release and
  expires at its VALID UNTIL" (option B above) is superseded by: the role is left untouched until
  release, and the release lets the executor drop it. `rotation.mdx`, the release-hold CLI help
  and its golden, the post-release stderr line, the OpenAPI descriptions of delete and release-hold
  and the ADR banner say so; `rotation runs` is the check before releasing.
- **Hold landing after the claim, each in-executor read pinned.** A seam
  `RotationExecutor.BeforeHeldRead(site)` runs before every executor hold read (`recover`,
  `reconcile:entry`, `reconcile:predrop`, `drop_orphan`, `drop_candidate`). Tests land the hold at
  each site and assert no `DROP ROLE`; removing any one read turns its test red (checked by
  mutation on both engines). `TestRotationHoldDuringARunSkipsTheRollbackDrop` also asserts exactly
  one drop after the release.
- **Lease on the early returns.** `recoverInFlight` and `reconcileCandidate` returned on a hold
  that landed after the claim and kept the lease until its term. They now call `yieldHeldRun`: the
  fenced settle keeps the phase, releases the lease and leaves the run due, so the claim, which
  skips the held rotation, takes it again right after the release. The claim's `attempt_count`
  bump for that visit stays (it only sizes the backoff); it is one per hold landing in the claim
  window, so it is bounded and left as is. Removing the yield turns the release-immediately
  assertion red.

### Owner decisions on S2b-2 (resolved)

Owner answered "continue as recommended" on the open S2b-2 decisions:

- Decision 1, provider-role drops while held: **B**, built above.
- Decision 2, containment ledger: **B** (account scope only: object kind `rotation`, owner
  function `rotation.halt`, enumerated by authority principal in `collectAccount`, one new store
  read and one authz read operation), conditional on org and project scopes inheriting through
  the accounts they select. Not in PR #1430: built in a follow-up branch from dev after it merges.
- Decision 3, run-level `published_versions_changed` hold release: **A**, status only, as built
  (accepted as recommended).
- Decision 4, key-narrowed holders without read: **A**, accepted (accepted as recommended).
- Doctor count-only and `not_converged_adapter_targets` adapter-only: **A**, accepted as
  recommended; no code change.

### Incident containment

A contained principal loads no grants, so a Gate that runs while the principal is contained
(`RecordedPrincipalHolds`) refuses at the rotation's next start and halts it. Containment
does not stop a run already past its Gate or a revoke in flight; a release of the principal never resumes it
(`TestRotationHaltsWhenItsAuthorityIsContained`). That is containment through the existing
seam. **Stopped, owner decision needed:** listing a principal's rotations as ledger items and
pausing them through an owner function. What is settled: the ledger vocabulary is code-level
(no CHECK on `owner_function` or `object_kind`), and `Incidents.Owners` has no registered
owners yet (#1039 wires them). What is not settled: the ADR lists scheduled rotation under
"Not decided here", and it gives no rotation effect for the org and project scopes.

- A. Leave it. Containment halts by effect; the ledger does not list rotations. Cheapest;
  the operator sees halted rotations in doctor and metrics but not on the incident.
- B. Account scope only: add object kind `rotation` and owner function `rotation.halt`,
  enumerate by authority principal in `collectAccount` (new store read and authz op), the
  owner function halts with a named reason; org and project scopes ride on the accounts they
  select. Registration of the owner stays with #1039.
- C. B plus a project-scope item for every rotation in the project, whatever its authority.
  Needs an ADR amendment for project scope.
- Recommendation: B, after the owner confirms that org and project scopes inherit through
  the accounts they select. It needs one new store read, one authz read operation and about
  one slice of tests; none of it is in this PR.

### Run-level hold (`revoke_refused` with `published_versions_changed`)

Not released by `release-hold`. Decisions 7 and 33 say it exits only through the restore
assertion passing or the superseded role being dead, and release must not drop a role early.
Options for the operator's view of that hold after a restore:

- A. Status only (as built): `rotation runs` shows the phase and error; `release-hold` leaves it.
- B. A separate human action moves the run to `quarantine` so the next cycle supersedes it.
- C. A separate human acknowledgement that extends the superseded role.
- Recommendation: A for this release, B as a follow-up if operators ask. C hands the
  operator a way to keep a role the instance can no longer account for.

### Restored in-flight runs do not drop a role consumers already fetched (resolved, option B)

(Security review round 1, M2; resolved by the owner in round 5.) Scenario: the archive was
taken while a run was `minting` or `minted`; the live instance then published that run's
candidate and consumers fetched it. After the restore the archive says the run never
published, so recovery used to roll it back and `DROP ROLE IF EXISTS` killed a credential
consumers already hold, with no restore assertion guarding the drop. Built as option B: while a
rotation is restore-held, no `DROP ROLE` is issued for a candidate that never published.
See "Review round 5" for the mechanism. An unpublished candidate role is left untouched
until `release-hold`, and the release is what lets the executor drop it (round 6; this replaces
the earlier "expires at its `VALID UNTIL`" wording). Delete and abandon are refused while held. Revokes of the superseded role are
unchanged (the restore assertion guards them) and runs past their publish still finish.

### Review round 7 (as built)

- **Hold committed during a provider call (CodeRabbit thread 4607573921, no code change).**
  `reconcileCandidate`, `dropCandidate` and `dropOrphan` read the hold once and then call the
  provider. A hold landing between that read and the call is unobserved, but no path was found where
  a role consumers already fetched is dropped. The hold is set only by `HoldRestoredRotations`
  inside `CompleteRestore`'s transaction, and a restore targets an empty SQLite path or an empty
  PostgreSQL database with the server stopped (`store.ErrTargetNotEmpty` refuses anything else;
  `RestoreDestination` serves no repositories), so the hold commits with the rows it covers, before
  any worker can claim a restored run. A worker of the pre-restore instance reads and fences the old
  database and its settles never reach the restored one. After the commit the hold clears only by a
  human `release-hold` or by a disable, which round 6 refuses while held. The post-claim reads
  (`BeforeHeldRead` sites) are defence in depth for the synthetic case that the tests build by
  landing a hold in a live database. No cheap exclusion that spans the provider call is needed;
  a lease-fence advance in the restore would protect a database no worker reads.
- **Held published run waited for its revoke deadline (CodeRabbit outside-diff, real, narrow).**
  While held, `overlapSettle` dropped the candidate validity from the wake, so a published run
  slept until `RevokeNotBefore`; after `release-hold` (which changes no schedule) it had no
  window if the candidate lapsed before that deadline, and keeping the expiry in the wake would
  re-claim at once, because `rotationOverlapWake` clamps an extension time that has passed to
  now. `overlapWakeFor` now keeps the candidate validity in the wake while held, and a wake that
  has already passed is pushed five minutes ahead (never past the deadline), so the run is polled
  and not spun. `ReleaseHold` is unchanged: it still flips only the flag. The documented rule
  stays: when the cycle window is spent (deadline later than `VALID UNTIL` minus the window), the
  first run after release halts the rotation with `retries_exhausted` before the credential
  lapses; the docs now say so for the post-publish case. Test
  `TestRotationHeldPublishedRunIsWokenBeforeItsCredentialExpires` failed before the change
  (run not woken before the credential expiry) and passes on both engines.
- **Key-narrowed transitions need environment read (CodeRabbit outside-diff, docs).** True for
  holders whose publish/edit/reveal comes only from rules narrowed to the keys: the rotation is
  looked up under `rotation.inspect` (environment `read`), so without `read` the answer is the
  uniform not-found with one denial under the operation. A `read` rule narrowed to the keys is
  enough, because read applies to the whole environment, so the earlier claim that a key-narrowed
  `read` rule never reaches the environment was wrong in the other direction: read ignores key
  limits. `TestRotationKeyNarrowedTransitionsNeedEnvironmentRead` pins both. The rotation page and
  the OpenAPI text for pause, resume, run-now and release-hold now say it (the stale "Pause needs
  publish on the environment, not per-key access" and the run-now "access held only through
  key-narrowed rules does not reach it" sentences are gone).

### Judgment calls

- Generation is not bumped on release.
- The audit enum gains `hold_released` on `rotation.configured`; no new event type.
- `RestoreHold` is a boolean on rotations and an integer on `pki_issuers.restore_hold`; the
  query contract lint accepts int32 for that one name instead of renaming either column.
- The audit coverage test lacked a `rotation.run_now` emitter (a gap from S2b-1); the
  executor audit lifecycle now issues one run-now.

## S3 as built (browser status surface): owner decisions (resolved in S4)

- **`halt_reason` was not on the wire.** Resolved, option A: S4 adds it as fixed words to
  `CredentialRotation`, `hikyo rotation list` and `show`, and the browser.
- **No last result or owed-drop summary on the list.** Resolved, option A: S4 adds
  `last_run` and the owed-drop counts to `CredentialRotation`.
- The six mutating rotation rows in `api/parity.yaml` now point at #1440, "Management
  WebUI for scheduled credential rotation", which carries the management surface.

## S4 as built (halt reason, last run and owed-drop summary)

API revision stays 20: it is the in-flight revision, shared with the OIDC lifecycle
settings, so `api/spec.go` appends a sentence to the revision 20 comment and no revision
is cut. Additive only: every new `CredentialRotation` property is a response
property, and `halt_reason` is an open enum (`x-extensible-enum`), so the freeze check
needs no allowance. The `halt_reason` column (migration 00091) existed; review round 1
added one index, migration 00096 (below).

- **`halt_reason`.** A fixed word, `retries_exhausted`, `authority_lost`, `gate_failed`
  (the words the executor writes) or `unknown`. The store keeps the raw column text;
  `service.rotationHaltWord` maps anything else (NULL, a legacy value, free text) to
  `unknown`, and a rotation that is not halted carries none, so no provider text or
  secret can reach the wire. The CLI prints a `HALT REASON` column, and a `HALTED` line
  on stderr with a sentence per word. The browser puts the same sentence in the halted
  notice and on the row. A word a client does not know is printed quoted, not
  interpreted. The sentences live in three places; a web test keeps the browser equal to the CLI source:
  `haltReasonSentence` (`internal/cli/credential_rotation.go`), `HALT_SENTENCES`
  (`web/src/api/rotations.ts`) and `rotation.mdx`.
- **`last_run`.** The newest run of the rotation (id, phase, `error_code`, `phase_at`),
  absent when it never ran. `RotationLastRuns` reads it for a page as one correlated probe
  per requested rotation, newest by `(started_at, id)` through `rotation_runs_rotation`
  (checked on 100,000 runs of one rotation: SQLite answers it in microseconds, the sort
  reads only the first group of equal start times). It equals the first row of the runs
  list (newest by id) because a run id is a time-ordered UUIDv7 made at the instant the
  same clock stamps `started_at`, and a rotation has at most one open run; a test on runs
  the executor started asserts the two agree. Reviewed pin in
  `internal/lint/testdata/reviewed_pins.json`.
- **Owed-drop definition (single source).** A run owes a drop iff its `phase` is
  `revoke_refused` or `quarantine`, whatever its error code. It lives in
  `RotationOwedDropCounts` (both engine query files, one grouped aggregate per page),
  and agrees with the CLI `noteOwedRuns` (`internal/cli/credential_rotation.go`), the
  browser `owesDrop` (`web/src/api/rotations.ts`) and the
  `hikyo_rotation_runs_{quarantined,revoke_refused}` gauges. An isolation test checks that
  the summary equals the count of the runs list on both engines, including a deleted rotation
  and a restore hold.
- **`owed_drop_runs` and `owed_drop_runs_versions_changed`.** Two counts, never a boolean.
  The second is the subset refused as `published_versions_changed`: the keys may serve
  that role again, so no surface says to drop it. The CLI rotation-level `ATTENTION` line,
  `owedSummaryText` and the row badge read "do NOT drop" and "Do not drop: restore"
  whenever the second count is above zero, and otherwise point at
  `hikyo rotation runs <rotation>` without repeating the per-run text. The S3 strings
  (`owedRunText`, `restoreHoldText`, `noteOwedRuns`, `noteRestoreHold`) are unchanged.
  The browser stays status only: no mutating button.
- **No N+1, and not on the executor's path.** The summary is a response concern.
  `RotationReader.Get`, `List` and every mutation return a record without it, so the
  executor's gates and existence checks pay nothing. `RotationReader.Summarize` fills a
  slice with two reads whatever its length; the service calls it for List after trimming
  the page to its limit, for Get, and for the SetState and ReleaseHold responses.
  `internal/store/rotation_summary_cost_test.go` counts the reads.
- **Parity.** `createRotation`, `deleteRotation`, `pauseRotation`, `resumeRotation`,
  `releaseRotationHold` and `runRotationNow` are `{issue: 1440}` in `api/parity.yaml`;
  `check-parity-issues.sh` accepts it (#1440 open). `listRotations`, `showRotation` and
  `listRotationRuns` stay `webui: machine-access`.

### Review rounds (S4)

Round 1 (two independent reviews, Security/data and Standards/Spec). Every item was fixed at
its root; nothing was deferred.

- **H1 (safety).** In `revoke()` the restore assertion (a store read) now runs before the
  provider is opened. A transient provider failure, or a missing superseded role, on a
  run already held as `published_versions_changed` keeps that code, and a restore that
  follows a `role_owns_objects` refusal is detected on the next visit. Tests in
  `internal/isolation/rotation_executor_restore_retry_test.go`. The summary never says
  "Owes a drop" plainly when the run code is `published_versions_changed` or a restore
  hold is on; the rotation-level CLI line and browser notice carry the per-run key
  check ("check that neither key serves that role, because a revision restore can put it
  back").
- **M1 (cost).** See "No N+1" above. Migration 00096 (`rotation_runs_owed`, a partial
  index on `(org_id, project_id, environment_id, rotation_id)` for phases
  `revoke_refused` and `quarantine`) makes the owed counts and the deleted-rotation
  probe read only those runs. Without it SQLite chose `rotation_runs_minter` and read
  every run of the environment. Both engines, additive;
  `internal/buildcompat/development.json` regenerated against PostgreSQL 18. **Re-check
  the migration number before merge**: 00096 was the next free number on `origin/dev`
  when it was added.
- **M2 (deleted rotations).** `RotationList` keeps a disabled rotation when it owns a
  `revoke_refused` or `quarantine` run (`EXISTS`, same chain), still in id order, so
  the default browser list shows it with `deleted` in its state badge, and since round 2
  the default CLI table prints `deleted` in its STATE cell too (round 1 said "marked
  deleted", which was true only of the CLI `ATTENTION` line). The owed-drop notice and
  count stay on it.
- **M3.** The OpenAPI, `rotation.mdx` and this handoff say `owed_drop_runs` counts the runs
  that need an operator (`revoke_refused`, `quarantine`); a run still in flight owes a
  drop the executor carries out. The definition did not change.
- **M4.** `unknown` has its own sentence in Go, TypeScript and `rotation.mdx`; "does not
  know" is only for a word no client recognises. A web test keeps all three equal.
- **L1.** A run's `error_code` reaches the summary and the runs list only as a fixed word
  (the executor's codes and `abandoned`), else `unknown`.
- **L2.** The browser looks halt words up with `Object.hasOwn`, so `toString` is unrecognised.
- **L3.** The CLI table and the `HALTED` line print the halt reason through one helper.
- **L4.** `rotation.mdx` scopes "never a yes or no" to the CLI; the browser badge is a yes/no
  and its notice carries the count.
- **L5.** Tests for cross-org, project, environment and rotation reads of the owed counts,
  and for the counts in SetState and ReleaseHold responses.

Round 2 (two independent reviews of 9b8a104dc, cf58e58a7 and d12fc352c). Final fix round.

- **H-1 and M-1 (one root cause: the do-not-drop signal lived in a column other writers
  replace).** A run's `last_error='published_versions_changed'` is what tells every
  reader "a restore refused this drop, the keys may serve the role again". Two writers
  replaced it: `extendDelivered` (an `ExtendRole` failure wrote `extend_failed` on the
  refused run, so the next revoke never re-read the assertion), `AbandonOpenRuns`
  (wrote `abandoned`, turning a restore refusal into a plain quarantine); deleting a
  rotation clears its `restore_hold`, so the run's code is the only record left. Design: **the refusal is sticky in the store, no new column and
  no migration** (00096 is unchanged). `RotationRtSettleRun` keeps the stored
  `last_error` when the stored value is `published_versions_changed`, the stored phase
  and the new phase are both `revoke_refused` or `quarantine`, and the settle did not
  carry `RestoreChecked`. Only `revoke()` sets `RestoreChecked`, after a fresh
  `RestoreAssertion` read or for a role that has expired (there the revoke settle and
  `finish()` also set it after a failed read, which is safe: `finish` always ends in
  `done` on that path), so only a re-read or an expired role can clear or replace the code.
  `RotationAbandonOpenRuns` keeps it in a `CASE`. A restore refusal now survives an
  extend outage, an abandon and a deleted rotation, and clears itself once the
  assertion holds again (a drop then follows). A dedicated column was rejected: it
  would need a migration, a backfill and a second source of truth for the same fact.
  Tests (both engines): `TestRotationRestoreRefusalSurvivesExtendOutage`,
  `TestRotationAbandonKeepsTheRestoreRefusal`,
  `TestRotationRestoreRefusalClearsOnceTheAssertionHoldsAgain` and `...OnQuarantine...`.
  The extend-outage and abandon tests guard stickiness: they fail when the CASE is
  reverted (checked on both engines). The two `...ClearsOnce...` tests guard liveness
  only: they fail if `RestoreChecked` is never set, so a refusal could never clear.
- **Writer audit** (every writer of `rotation_runs.phase` and `last_error` that can
  touch a `revoke_refused` or `quarantine` run):

  | Writer | Verdict |
  | --- | --- |
  | `RotationRtInsertRun` | new run, phase `minting`: not applicable |
  | `RotationRtClaimRun` | writes attempt and lease only, not `last_error`: safe |
  | `RotationRtSettleRun` | the one place the rule lives (see above) |
  | `revoke()` `hold(...)` (restore refusal, provider unavailable) | `RestoreChecked: true` after a fresh read: may set or clear, correct |
  | `revoke()` revoking settle and `finish()` | `RestoreChecked: true`: clear the code on the way to `done`, correct |
  | `extendDelivered` (success, failure, skip, held skip) | no `RestoreChecked`: refused code kept (was H-1) |
  | `settleDrop` callers, `confirmAbsent`, failed rollbacks, `finishFailed` | move runs that are not refused or quarantined, or carry no `pvc`: unaffected |
  | `haltClosing`, `haltDue`, wake and the reconciler's drop-candidate settle | keep the run's own `LastError`, or act on `minting`/`minted` runs: unaffected |
  | `RotationAbandonOpenRuns` | keeps `pvc`, else `abandoned` (was M-1) |
  | `RotationSetState(disabled)` | writes the rotation row only (it clears `restore_hold`); the run's code stays, so the deleted rotation keeps its warning |
  | release-hold | writes `restore_hold` only: unaffected |
  | restore paths | write the hold and revisions, never a run: unaffected |
- **L-1.** The assertion read now runs before `LoadSupersededRole` and the provider open
  (note on the TOCTOU row above).
- **L-2.** A transient assertion read error no longer blocks settling an expired role;
  the error is handled when the role has expired (test with `RestoreAssertionFault`).
- **L-3.** The `RotationList` query comment says it is the environment delete's read of
  live rotations; the API's default list uses the owed-aware query.
- **L-4.** The CLI list prints `deleted` in the STATE cell for a deleted rotation.
- **L-5.** A table test ties every `rotCode*` constant to the fixed error words (service)
  and the CLI's word list to the executor's constants.
- **L-6 and Low 3.** Key names and error codes break only after an underscore (`<wbr>`),
  `published_versions_changed` never splits mid-word; stories and e2e check a 45 character
  key name at 1280, 860 and 390 (name inside its cell, no sideways scroll).
- **M-A.** `rotation.mdx` now says a deleted rotation that owes a drop is listed by default,
  the state word is `deleted` in the CLI and browser, the checkbox is "Include all
  deleted rotations", and "Do not drop: restore" also covers a restore hold.
- **Low 5 to 8.** The CLI HALT REASON cell follows `State==halted` through one helper; run
  phases and codes print bare when known and quoted otherwise; the halt reason field reads
  `retries_exhausted: the retry window ran out`.
- **00096.** Still the next free migration after the rebase onto origin/dev (6d8db0f4d),
  `development.json` regenerated. Check again before merge.

### Left after S4

- The management WebUI for the mutating verbs is #1440, built: see `1440-rotation-webui.md`.
- Containment ledger B is still not built (see "Incident containment").
- Remaining owner items from earlier slices are unchanged; S4 opened none.

## S2 checklist (carried forward from review)

Status after S2a and its round 1 review: closed in S2a are the Gate, the provider-delete and environment-delete
guards, retry with backoff, `extend` and `halted`, the two loops, quarantine exits, the
rule 7 and rule 8 tests, the schedule bounds test, `superseded_handle` across rotations,
the service side of `--abandon`, multi-node takeover, the `2BP01` refusal, the grouped-key
and draft refusals, resume scheduling, the held-run extend and expiry exits. Gate placement is described in
judgment call 11. Carried to
S2b: the doctor warning and metric, incident containment, the `restore_hold` column,
key-aware resume, pause and delete (all built in S2b-2 except ledger enumeration), the consumer-convergence report and run-now/runs.
The list below is the original text and stays as the acceptance record.

- Gate: `RecordedPrincipalHolds` only, with a key target per stored key (see The Gate);
  no rebuilt identity and no `az.Authorize`; the pre-publish list includes keys present and
  secret, environment unattended, provider active with credential.
- Provider delete and environment delete must refuse (or enqueue the revocation) while any
  run holds a possibly live role or an unsettled revoke obligation. Rotation roles are not `dynamic_leases` rows, so
  `ErrProviderHasActiveLeases` never sees them; S1's environment delete already blocks on
  them, the provider delete path must be extended.
- Failure retry with bounded backoff and the `extend` action inside the expiry window, with
  the concrete `slack` and retry window above; retries exhausted moves the rotation to
  `halted` with a loud doctor and metric signal.
- Resume, pause and delete key-aware for a creator who holds the formula only through
  key-narrowed rules: today the outer authorization has no key target, so such a creator
  gets the uniform not-found for all three.
- Two loops with disjoint selections: the candidate reconciler (`published_revision IS
  NULL`, owns `candidate_handle`) and the revoke-obligation loop (`published_revision IS
  NOT NULL` with an unsettled revoke, owns `superseded_handle`); exits from `quarantine`
  as defined in "Revoke obligation and exits from quarantine".
- Fenced claim write before any `DropRole` that resolves an ambiguous state (rule 8), and
  the restore assertion before the superseded drop (rule 7), each with its test.
- The failure schedule with the single anchor `P`, `publish_budget`, the `[S, D]` retry
  window and the delay-inclusive bounds test.
- Doctor warning, metric and `extend` action for an approaching `credential_expires_at`
  (Q2).
- Incident containment by authority principal; never auto-resume on release; a named
  halted state for a failed gate (Q4).
- `restore_hold` column: a restored rotation starts held until an operator releases it
  (decision 9), so a restored instance never mints before grants are reconciled.
- `superseded_handle` across rotations, `--abandon` past publish, multi-node takeover
  orphan reconciliation, and the `2BP01` named refusal (see the state machine section).
- Scheduled revocation reports consumers that may still hold the superseded credential
  (machine principals with `read@env` whose last delivery precedes the publish, adapter
  targets whose `converged_revision < published_revision`) in the revoke OUTCOME, not only
  for `run-now --skip-overlap`.
- Run, quarantine and run-now events and operations, metrics, `hikyo doctor`.

## S1 divergences from the issue handoff (summary)

- A third table, `rotation_effects`, is created now (decision 1).
- No foreign key to `keys`, so no `Keys.Delete` refusal (decision 2).
- Only `rotation.configured` is registered; run events, `run-now` and `runs` ops and
  paths wait for S2 (decision 6).
- `username_key_id` is required by the service and the column; the revocation delay must
  be shorter than the interval; only a signed-in person with a ceremony may create or
  resume.
- A key claims table replaces the per-role unique indexes; environment delete refuses or
  purges; provider delete and credential revoke pause rotations (decision 2).
- Reads (`rotation.inspect`) are human-session only.
- API revision 17 (`x-hikyo-min-revision: 17`); parity rows use `{issue: 148}`, so
  #148 must stay open until S3 flips them.
- Review: cross-provider review was not run (owner waiver dated 2026-11-16 citing the
  2026-11-02 instruction; see the top of this file); independent adversarial Claude review
  is recorded on the PR, not in this file.
