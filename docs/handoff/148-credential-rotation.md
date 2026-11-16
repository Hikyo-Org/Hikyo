# Handoff: #148 Scheduled credential rotation (PostgreSQL)

Issue: https://github.com/Hikyo-Org/Hikyo/issues/148 (blocked by #147, #63, #64, all merged).
Depends on `docs/handoff/147-dynamic-secrets.md` (provider, effects, worker pattern).

ADR amendments (operative when the PR implementing slice S1 merges; cross-provider
review SKIPPED because OpenAI review is unavailable by owner instruction, so it is
not CLEAN; independent adversarial Claude review is recorded on the PR):

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
human who configured it), re-authorized before every transition. Never a
`SystemProof`, never a scheduler site.

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
- [ ] S2 state machine, worker, run-now, runs, metrics, doctor, audit run events
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
2. **Integrity constraints.** One non-terminal run per rotation (partial unique index);
   one non-`disabled` rotation per `(environment_id, key_id)` and per
   `(environment_id, username_key_id)`; `username_key_id <> key_id`; a CHECK ties
   `next_run_at` to the `active` state. **Divergence: no foreign key to `keys`.** A
   disabled rotation is history and must outlive a deleted key, and the soft delete
   frees the slot, so a key FK would either block key deletion forever or erase
   history. As a result `Keys.Delete` does **not** refuse while a live rotation
   references the key. The S2 gate must re-check, before every transition, that both
   keys still exist and are secret-classified, and halt with a named refusal
   otherwise. A rotation whose provider is deleted is held by the FK to
   `dynamic_providers`; provider deletion with a live rotation fails on that FK and
   has no named refusal or test in S1 (follow-up).
3. **Delete is soft.** `rotation delete` transitions to `disabled` and keeps the row,
   so run history and effect FKs survive and the key slot is released. A rotation
   with a non-terminal run refuses delete (S1 can have none; the check and its
   error exist so S2 slots in; `--abandon` is S2).
4. **Authz (all S1 operations human-session only).** Configure (create):
   `edit ∧ publish ∧ reveal @environment`, so the authorizing chokepoint itself proves
   the creator currently holds all three, and the creator is recorded as
   `authority_principal_id`. Pause, resume, delete: `publish@environment`. Inspect
   (list, show): `read@environment`. Reads are human-session only for now; widening to
   machine credentials is additive later, narrowing is not. `manage-identities` is
   deliberately **not** required: `lease.mint` already admits any `read` plus
   `reveal` holder to mint from any provider in the project, so a rotation does not
   widen who can obtain a provider credential. Create additionally validates: the
   provider exists, is active and has its credential set; `key_id` exists and is
   `secret`-classified (a `config` key would hand the password to plain readers);
   `username_key_id` exists; the two keys differ; neither key is already driven by a
   live rotation; interval and delay are inside the bounds.
   Resume re-authorizes the **recorded authority principal** (not the caller) for the
   configure formula and refuses by name if it no longer holds it. This is the
   S2 `Gate` shape in miniature.
5. **CAS.** `pause`, `resume`, `delete` carry `expected_generation`; a stale value is
   `409`. Every mutation increments `generation`.
6. **Audit.** Only `rotation.configured` (`change`: `created|paused|resumed|deleted`)
   is registered in S1. **Divergence from the brief:** the run events are *specified*
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
   rows as restored, and no scheduler exists to act on them yet.

## S2 state machine (design; nothing here executes in S1)

Every transition is an INTENT/OUTCOME effect (`rotation_effects`) under the recorded
authority. A run row is the job, claimed under its own row lease
(`SELECT ... FOR UPDATE SKIP LOCKED`, per-org cap, exact-row re-assertion on every
settling write), exactly as `dynamic_leases`; no singleton scheduler lease.

Phases (`rotation_runs.phase`): `minting`, `minted`, `staged`, `published`,
`overlap`, `revoking`, `done`, `quarantine`, `failed`. `staged` is reserved and not
entered by this design (kept in the CHECK so a sealed-candidate design needs no
table rebuild). Non-terminal: everything except `done`, `quarantine`, `failed`.

Scheduling: a due rotation (`state=active`, `next_run_at <= now`) creates a run in
`minting` and advances `next_run_at = now + interval`, in one transaction. Missed
windows after downtime collapse to one run, not a backlog.

### Candidate-password durability (chosen order, "Option A": no sealed column)

The generated password exists only in worker memory and in the **one** publish
transaction. It is never stored, not even sealed. Order:

1. **`minting`.** One transaction: re-authorize (the Gate, below), record
   `candidate_handle` (deterministic `hikyo_rot_<run-id-prefix>`) and
   `superseded_handle` (the previous `done` run's candidate, `NULL` on the first run),
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
   OUTCOME and `done`. On the first run (`superseded_handle IS NULL`) the phase is
   skipped and `published` goes straight to `done`.

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

### The Gate (re-authorization per transition)

Identity is rebuilt from the recorded `authority_principal_id` (class human) and
`az.Authorize` runs in a fresh read transaction, as the adapter and dynamic workers
do. Pre-publish transitions (`minting`, publish) require: authority still holds
`edit ∧ publish @env` and `reveal @env`; provider active with credential set;
rotation `active`; `generation` unchanged. **Post-publish transitions (`overlap`,
`revoking`) re-assert only the row fence, the recorded `published_revision` and the
provider's existence**, the fail-safe direction `dynamic_leases` already takes for
revoke: losing authority must never strand a live superseded credential.
**Divergence from the issue handoff**, which gates every transition.

### Pause, resume, delete (S2 semantics)

- Pause blocks scheduling and blocks starting new pre-publish phases (an unpublished
  candidate is rolled back, so a paused rotation never leaves a candidate live).
  Post-publish phases always continue to completion. This is the reading of the issue
  handoff's "never leaves a candidate unrevoked" that cannot strand a credential.
- Resume re-authorizes the recorded authority and recomputes `next_run_at`.
- Delete requires no non-terminal run, or `--abandon`, which quarantines the
  candidate, keeps the superseded role live, and is loud.
- A paused or disabled rotation does not extend the delivered role's `VALID UNTIL`.
  See open question 2.

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
- `rotation.run_outcome`: `{run_id, phase (enum), handle, error_code}` (outcomes
  `success|failure|unknown`).
- `rotation.quarantined`: `{run_id, candidate_handle, superseded_handle}`.
- `rotation.run_now`: `{run_id, skip_overlap (bool)}`.

`rotation.configured` (registered in S1) carries `change`, `provider_id`, `key_id`,
`username_key_id`, `interval_seconds`, `revocation_delay_seconds`, `generation`,
`authority`.

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
`HIKYO_TEST_POSTGRES_DSN` is set): store round trips, constraints (one live rotation
per key, one non-terminal run), tenant isolation fixtures, bounds, create refusals
(authority lacks publish, provider missing or credential unset, key missing or not
secret, out-of-bounds, duplicate), CAS conflict, resume re-authorization refusal,
audit payload free of credentials, contract and freeze tests. The S2 gate, not S1,
owns key-deleted-underneath-a-rotation handling.

S2: full happy path with a fake clock (mint, publish, overlap, revoke, role absent;
cursor changes exactly once per publish; Compose render picks up the value), failure
before publish (quarantine, delivered value unchanged), failure after publish (both
handles recorded, retry succeeds), kill-9 between INTENT and OUTCOME (`unknown`,
reconcile), pause mid-overlap, run-now without confirmation refused, authority loses
`publish` (pre-publish run halts with a named refusal; post-publish run still
revokes), multi-node takeover, audit payloads free of credentials, Kubernetes
operator e2e (`k8se2e`) asserting a rollout after rotation.

## Open questions for the owner

1. **Ceremony at creation.** Should `rotation create` consume a `PurposePublish`
   ceremony, given it delegates publish authority to a standing worker? S1 requires
   only the three-atom formula and a human session.
2. **Pause and `VALID UNTIL`.** A paused rotation lets PostgreSQL expire the delivered
   role at `VALID UNTIL`, an outage the operator caused by pausing. Options: (a) S2
   `ExtendRole`s the delivered role while paused, (b) document the expiry and surface
   `credential expires at` in status. Recommendation: (b), with the field.
3. **Username key.** Roles are minted per cycle with a new name, so an overlap only
   works if consumers also receive the new role name, which means `username_key_id`
   is effectively required. **S1 requires it in the service and API (the column stays
   nullable)**, diverging from the issue handoff. Options: (a) keep it required, (b)
   make it optional and let S2 define a password-only mode (`ALTER ROLE ...
   PASSWORD`, no overlap). Recommendation: (a), the current behaviour.
4. **Incident containment.** Should the incident ledger enumerate rotations by
   `authority_principal_id` so a contained principal's rotations are paused? Not done
   in S1; there is no executor yet.

5. **Key-narrowed authority.** Pause, resume and delete are `publish@environment`
   with no key target, so a key-narrowed member access rule cannot satisfy them
   (fail closed). A creator authorized only by a rule narrowed to the two keys can
   therefore create a rotation but not pause, resume or delete it. Options: (a) keep
   publish across the whole environment for these three; (b) make them key-aware by
   reading the row under the inspect proof and then authorizing the operation on both
   stored keys. Recommendation: (a) for S1; revisit with the S2 gate, which already
   re-authorizes per key.


## S1 divergences from the issue handoff (summary)

- A third table, `rotation_effects`, is created now (decision 1).
- No foreign key to `keys`, so no `Keys.Delete` refusal (decision 2).
- Only `rotation.configured` is registered; run events, `run-now` and `runs` ops and
  paths wait for S2 (decision 6).
- `username_key_id` is required by the service; the revocation delay must be shorter
  than the interval; only a human session (or local host authority) may create.
- Reads (`rotation.inspect`) are human-session only.
- API revision 17 (`x-hikyo-min-revision: 17`); parity rows use `{issue: 148}`, so
  #148 must stay open until S3 flips them.
- Review: cross-provider review was SKIPPED (OpenAI unavailable by owner instruction);
  independent adversarial Claude review is recorded on the PR, not in this file.
