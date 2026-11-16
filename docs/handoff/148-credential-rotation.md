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
  (`create|list|show|pause|resume|run-now|delete`, `runs`) and its API paths.
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
- [x] S1 round 3 review fixes: halted state and `halt_reason`, `revoke_refused` phase and
  resume formula settled in migration 00091; cascade pause when an environment becomes
  protected or gains an approval policy; environment delete treats quarantine as live and
  names the blocking rotations; paged list with one batched expiry read; recorded-authority
  key resolution on every call; per-key denial attributed to the resume operation; tests
  for each; documentation made consistent on the gate (full before publish, fail-safe after)
- [ ] S2 state machine, worker, run-now, runs, metrics, doctor, audit run events (see the S2 checklist at the end)
- [ ] S2 dual-engine restart/failure/multi-node takeover e2e, Compose pickup e2e
- [ ] S3 SPA status surface, parity flip

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
   name (`ErrEnvironmentHasLiveRotation`, whose message carries the blocking rotation ids,
   at most ten of each kind) while any rotation is not `disabled` or any run may still
   hold a live role. `quarantine` is NOT terminal for this purpose: a quarantined run may
   hold a live role. Only `done` runs and `failed` runs that never published (the
   candidate was confirmed dropped) are purgeable; a `failed` run with a recorded publish
   and every other phase (including `revoke_refused`) blocks. When nothing blocks, the
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
   recorded under the resume operation, never `rotation.create`). The outer authorization
   has no key target, so it needs those grants environment-wide; a caller who holds the
   formula only through key-narrowed member rules gets the uniform not-found at that outer
   step and CANNOT resume (S2 checklist: make resume, pause and delete key-aware for
   narrowed creators). Inside, in order: a human caller, the environment still unattended,
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
   `409`. Every mutation increments `generation`.
6. **Audit.** Only `rotation.configured` (`action`: `created|paused|resumed|deleted`;
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
    batched read, not one query per rotation. Both parameters are additive.
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
needs no table rebuild). Non-terminal: everything except `done`, `quarantine`, `failed`.
For environment delete only `done` and a `failed` run without a `published_revision`
are purgeable.

Scheduling: a due rotation (`state=active`, `next_run_at <= now`) creates a run in
`minting` and advances `next_run_at = now + interval`, in one transaction. Missed
windows after downtime collapse to one run, not a backlog.

### Candidate-password durability (chosen order, "Option A": no sealed column)

The generated password exists only in worker memory and in the **one** publish
transaction. It is never stored, not even sealed. Order:

1. **`minting`.** One transaction: re-authorize (the Gate, below), record
   `candidate_handle` (deterministic `hikyo_rot_<run-id-prefix>`) and
   `superseded_handle` (the delivered handle of the latest run with
   `published_revision IS NOT NULL` for this `(environment_id, key_id)`, across ALL
   rotations that ever drove the key; `NULL` only when no run ever published for it),
   write the INTENT effect. The handle is durable **before** the provider is touched.
2. `CreateRole(candidate_handle, password, VALID UNTIL now + interval + delay + slack)`
   outside any transaction.
3. **`minted`.** OUTCOME(mint success) recorded. The password still lives only in
   memory. A run found in `minting` or `minted` after a crash has lost its password
   and **always rolls back**: `DropRole(candidate_handle)` (idempotent, absent is
   success), then `failed`. A fresh run mints a fresh candidate.
4. **`published`.** ONE transaction: re-authorize the full Gate, stage the new
   value(s) (`Values.stage`/`Declare`), `Revisions.PublishPlanned` with the staged
   version ids, write `published_revision`, set `phase=published` and
   `next_attempt_at = now + revocation_delay_seconds`, OUTCOME(publish success). A
   schema-validation failure is a definite failure: drop the candidate, `failed`.
   Because the phase write and the publish commit together, a crash can never leave a
   delivered new value with a run still saying `minted`, or the reverse. An ambiguous
   commit (connection lost at COMMIT) is resolved by reading the run row: `published`
   means published; anything earlier means not.
5. **`overlap`.** Durable wait until `next_attempt_at`. Nothing else happens. Skipping
   the overlap (`run-now --skip-overlap`) sets the run's `revocation_delay_seconds`
   to `0`, so this phase is immediate.
6. **`revoking`.** INTENT(revoke), `DropRole(superseded_handle)` idempotent, then
   OUTCOME and `done`. When `superseded_handle IS NULL` (no run ever published for the
   key, so the previous value was the operator's own) the phase is skipped and
   `published` goes straight to `done`.

### Crash-safety rules (binding on S2; S1's schema must not violate them)

1. An interrupted cycle must resume or roll back cleanly; no phase may require
   in-memory state that a restart loses, except the candidate password, whose loss
   always means roll back the candidate.
2. Revocation of the superseded credential **never** runs before the new credential is
   confirmed published. `published_revision` is recorded by the same transaction as
   the publish; `revoking` is unreachable without it, and the revoke step re-reads it.
3. Every step is audited with INTENT before the effect and OUTCOME after.
4. Failure **before** publish drops (or quarantines) the candidate and leaves the
   delivered value unchanged.
5. Failure **after** publish keeps both handles recorded, retries the drop with
   backoff, and surfaces "a possibly live credential exists" (status, metric
   `hikyo_rotation_runs_unknown_outcome`, `hikyo doctor`). It is never marked `done`
   without a successful drop OUTCOME.
6. An ambiguous provider outcome never destroys the last known-working credential:
   the superseded role is only dropped after a confirmed publish, and an unknown
   candidate drop leaves the run in `quarantine` (before publish) or `revoking`
   with an `unknown` effect (after publish), both visible.

| Crash point | State found on restart | Action |
| --- | --- | --- |
| After handle recorded, before `CreateRole` | `minting`, role absent | drop candidate (no-op), `failed` |
| During or after `CreateRole`, before OUTCOME | `minting` | drop candidate, `failed` |
| After OUTCOME(mint), before publish tx | `minted` | drop candidate, `failed` |
| Publish tx commit ambiguous | read the row | `published` continues, else roll back |
| After publish, before drop | `published` / `overlap` / `revoking` | resume, idempotent drop |
| During `DropRole` | `revoking`, effect INTENT only | re-probe `RoleStatus`, settle |
| Fence-losing worker after `CreateRole` (multi-node takeover) | candidate role exists, run `failed` or `quarantine` | reconciler re-probes `RoleStatus(candidate_handle)` and drops it (below) |

**`superseded_handle` is derived across rotations.** The role being superseded is the
delivered handle (the candidate) of the latest run with `published_revision IS NOT NULL`
for the same `(environment_id, key_id)`, across ALL rotations that ever drove the key,
whatever that run's phase. A deleted and recreated rotation therefore still revokes the
role that is live in the keys, and the first run of a recreated rotation does not skip the
revoke. It is `NULL` only when no run ever published for the key (the operator's own
initial value).

**Multi-node takeover orphan.** A worker whose row lease expired mid-run can still finish
`CreateRole` after another node took the run over and rolled it back to `failed`. The
fence-losing worker, on losing the fence, best-effort drops its candidate. The reconciler
also re-probes `RoleStatus(candidate_handle)`, but ONLY for runs with
`published_revision IS NULL` in `failed` or `quarantine`, once the lease has expired plus
the provider deadline, and drops any role still present. A run with a recorded publish is
never dropped by the reconciler: its candidate IS the credential delivered into the keys.
A post-publish run that was abandoned (`rotation delete --abandon`, closed as
`quarantine`) is told apart from a pre-publish quarantine solely by its non-NULL
`published_revision`; the reconciler's selection predicate is `published_revision IS NULL`
and nothing else, and its test must include an abandoned-past-publish run whose candidate
role must survive. The lease
TTL must exceed the provider deadline so a live worker is never taken over mid-call. Add
this row to the S2 crash tests and to the multi-node takeover e2e.

**`DROP ROLE` refused for a role that owns objects.** PostgreSQL refuses `DROP ROLE` with
SQLSTATE `2BP01` (dependent objects) when the role owns objects or holds privileges. Map it
to `ErrRefused` in `classifyExec` (`internal/dynamic/postgres/postgres.go`). The run then
enters a named state, `revoke_refused`, visible in `rotation show`, `rotation runs` and
`hikyo doctor`, retried with backoff and never marked `done`. Decision: the rotation does
NOT run `REASSIGN OWNED BY candidate TO grant_role` before the drop. Reasoning: a rotation
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
credential. A failed pre-publish gate moves the rotation to `halted` with a `halt_reason`.
**Divergence from the issue handoff**, which gates every transition.

### Failure, retry and credential expiry (S2)

A failed scheduled run does not wait a whole interval for its next try, because the
delivered credential's `VALID UNTIL` is fixed. A run that fails before publish is retried
with bounded exponential backoff (`attempt_count`, `next_attempt_at` on the rotation's next
run or a fresh run) for as long as the current credential still has at least one retry
window left; and when `credential_expires_at` falls within an extension margin the executor
runs the `extend` effect (the kind is already in the CHECK) to push `VALID UNTIL` out
instead of letting it lapse.
Concrete values: `slack = 24h`, so a role is minted `VALID UNTIL = publish + interval +
delay + 24h`; the retry window is `retry_window = 6h` and the extension margin is `2 *
retry_window = 12h` before expiry. Proof the schedule leaves at least one retry window: the
next run is scheduled at `publish + interval`, the old role is revoked at `publish +
interval + delay` at the earliest, and the delivered credential stays valid until
`publish + interval + delay + 24h`, so after the scheduled attempt there are at least 24h
(four retry windows) before expiry even when `delay` is the 5 minute minimum. The unit test
for the executor asserts `slack >= 2 * retry_window` for every configurable interval and
delay. When retries are exhausted the rotation moves to `halted` (`halt_reason`
`retries_exhausted`) and the failure is loud: `hikyo doctor`, the metric
`hikyo_rotation_halted`, and the credential-expiry warning, so a lapse is never silent.

### Pause, resume, delete (S2 semantics)

- Pause blocks scheduling and blocks starting new pre-publish phases (an unpublished
  candidate is rolled back, so a paused rotation never leaves a candidate live).
  Post-publish phases always continue to completion. This is the reading of the issue
  handoff's "never leaves a candidate unrevoked" that cannot strand a credential.
- Resume re-authorizes the recorded authority and recomputes `next_run_at`.
- Delete requires no non-terminal run, or `--abandon`. For a run before publish,
  `--abandon` quarantines the candidate, keeps the superseded role live, and is loud. For
  a run past publish the candidate IS the delivered credential: it stays live, `--abandon`
  only stops future cycles, the run is closed as `quarantine`, and the credential stays
  recorded (so `credential_expires_at` still names it and the superseded role is still
  dropped by the reconciler's revoke retry). Nothing about `--abandon` revokes a delivered
  credential.
- A paused or disabled rotation does not extend the delivered role's `VALID UNTIL`.
  See owner decision 2 and decision 10.

### Run-now (S2)

`OpRotationRunNow` requires `publish@env` plus a `PurposePublish` ceremony (unit = the
rotation's key ids) and `--confirm-destructive`. `--skip-overlap` snapshots
`revocation_delay_seconds = 0` for that run only; the response lists consumers that
may still hold the superseded credential (machine principals with `read@env` whose
last delivery `issued_at` precedes the publish, and adapter targets whose
`converged_revision < published_revision`). Event `rotation.run_now` carries
`skip_overlap`.

### Audit events specified for S2 (not registered in S1)

All tenant trail, `RetentionSecurity`, no credential material in any payload:

- `rotation.run_intent`: `{run_id, phase, handle}` (outcome `intent`).
- `rotation.run_outcome`: `{run_id, phase (enum), handle, error_code,
  not_converged_consumers (int, optional)}` (outcomes `success|failure|unknown`); the
  revoke OUTCOME sets `not_converged_consumers` to the number of consumers (machine
  principals with `read@env` whose last delivery precedes the publish, adapter targets
  whose `converged_revision < published_revision`) that may still hold the superseded
  credential.
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

Role `VALID UNTIL` (S2) = `now + interval + delay + slack` stays inside the PostgreSQL
provider's TTL range (`internal/dynamic/ttl.go`, whole seconds up to
`math.MaxInt64/1e9`), so the largest configurable cycle (90d + 7d + slack) never
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
refusal (naming the rotations, quarantine blocking) and purge on both the direct delete and
the definitions-apply path, cascade pauses on protect and on approval policy create and
update, a halted rotation's resume authorization, narrowed-authority resume and its
denial attribution under the resume operation, `RecordedPrincipalHolds` key resolution,
concurrent create of one key, local-principal refusals, paged listing with batched expiry,
credential expiry derivation, `include_disabled`, audit payload free of credentials,
contract admission and freeze tests. The S2 gate, not S1, owns key-deleted-underneath-a-rotation
handling during a run.

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

## S2 checklist (carried forward from review)

- Gate: `RecordedPrincipalHolds` only, with a key target per stored key (see The Gate);
  no rebuilt identity and no `az.Authorize`; the pre-publish list includes keys present and
  secret, environment unattended, provider active with credential.
- Provider delete and environment delete must refuse (or enqueue the revocation) while any
  run holds a possibly live role. Rotation roles are not `dynamic_leases` rows, so
  `ErrProviderHasActiveLeases` never sees them; S1's environment delete already blocks on
  them, the provider delete path must be extended.
- Failure retry with bounded backoff and the `extend` action inside the expiry window, with
  the concrete `slack` and retry window above; retries exhausted moves the rotation to
  `halted` with a loud doctor and metric signal.
- Resume, pause and delete key-aware for a creator who holds the formula only through
  key-narrowed rules: today the outer authorization has no key target, so such a creator
  gets the uniform not-found for all three.
- The reconciler selects only runs with `published_revision IS NULL` (see above).
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
