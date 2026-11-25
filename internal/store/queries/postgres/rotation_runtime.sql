-- Rotation executor (#148, slice S2a). Two boundaries share these statements. The
-- proof-carrying RotationRepo methods (create a run, take the run's row lock for
-- the publish, record the publish) run under the recorded authority's
-- rotation.run proof and bind the proof's tenant chain. The closed RotationRuntime
-- (claim, fenced settle, effects) is the dynamic-lease worker's shape: it claims
-- one due run across tenants under the per-org cap and then binds the claimed
-- run's immutable chain and its lease fence (owner, expiry, claim token) on every
-- write, so a worker that lost its lease affects zero rows. Every time the
-- statements compare is passed in from the service clock, never read from the
-- database.

-- hikyo:reason RotationRuntime closed scheduler picks one due rotation across tenants under the per-org concurrency cap; the run is then created under the recorded authority's proof and binds the selected immutable chain.
-- hikyo:instance-scoped
-- name: RotationRtDueRotation :one
SELECT ro.id,ro.org_id,ro.project_id,ro.environment_id,ro.provider_id,ro.key_id,ro.username_key_id,ro.interval_seconds,ro.revocation_delay_seconds,ro.authority_principal_id,ro.generation,ro.manual_run_id,ro.manual_skip_overlap
 FROM rotations ro
 WHERE ro.state='active' AND NOT ro.restore_hold AND ro.next_run_at<=sqlc.arg(now)
 AND NOT EXISTS (SELECT 1 FROM rotation_runs op WHERE op.rotation_id=ro.id AND op.phase NOT IN ('done','quarantine','failed'))
 AND (SELECT COUNT(*) FROM rotation_runs x WHERE x.org_id=ro.org_id AND x.lease_owner IS NOT NULL AND x.lease_expires_at>sqlc.arg(now)) < 4
 ORDER BY ro.next_run_at,ro.id LIMIT 1;

-- The live leases of an org, the number the per-org cap bounds. StartRun re-reads it
-- inside the creation transaction (the due-rotation selection read it earlier, in
-- another transaction).
-- hikyo:reason RotationRuntime closed scheduler bounds concurrent runs per org: the count spans every project and environment of the one org the due rotation belongs to and carries no tenant data out.
-- hikyo:instance-scoped
-- name: RotationRtOrgLeaseCount :one
SELECT COUNT(*) FROM rotation_runs WHERE org_id=sqlc.arg(chain_org) AND lease_owner IS NOT NULL AND lease_expires_at>sqlc.arg(now);

-- name: RotationRtLockDueRotation :one
SELECT id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,generation,manual_run_id,manual_skip_overlap FROM rotations WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='active' AND restore_hold=FALSE AND generation=sqlc.arg(expected_generation) AND next_run_at<=sqlc.arg(now) FOR UPDATE SKIP LOCKED;

-- The role currently delivered into a key: the candidate of the run that published
-- the newest revision, across every rotation that ever drove the key (as its
-- password key or its username key), so a recreated rotation still revokes the
-- live role. Ordered by the published revision, which is the order the snapshots
-- were served in, not by when the run started. valid_until_at is that role's
-- recorded VALID UNTIL.
-- name: RotationRtLatestPublished :one
SELECT ru.candidate_handle,ru.candidate_valid_until AS valid_until_at FROM rotation_runs ru JOIN rotations ro ON ro.org_id=ru.org_id AND ro.project_id=ru.project_id AND ro.environment_id=ru.environment_id AND ro.id=ru.rotation_id WHERE ru.org_id=sqlc.arg(chain_org) AND ru.project_id=sqlc.arg(chain_project) AND ru.environment_id=sqlc.arg(chain_env) AND (ro.key_id=sqlc.arg(key_id) OR ro.username_key_id=sqlc.arg(key_id)) AND ru.published_revision IS NOT NULL ORDER BY ru.published_revision DESC,ru.id DESC LIMIT 1;

-- name: RotationRtInsertRun :exec
INSERT INTO rotation_runs (id,org_id,project_id,environment_id,rotation_id,generation,run_trigger,phase,candidate_handle,superseded_handle,candidate_valid_until,revocation_delay_seconds,started_at,phase_at,lease_owner,lease_expires_at,lease_claim_token,attempt_count,next_attempt_at) VALUES (sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(rotation_id),sqlc.arg(generation),sqlc.arg(run_trigger),'minting',sqlc.arg(candidate_handle),sqlc.narg(superseded_handle),sqlc.arg(candidate_valid_until_at),sqlc.arg(revocation_delay_seconds),sqlc.arg(at),sqlc.arg(at),sqlc.arg(worker),sqlc.arg(lease_until_at),1,1,sqlc.arg(at));

-- The schedule moves, never the generation: the run that is in flight asserts the
-- generation it started under, so a schedule write must not invalidate it. A rotation
-- a restore holds is not moved at all (here, by a deferral or by a halt): the hold can
-- land after the run was claimed, and releasing it must find the rotation as it was.
-- name: RotationRtSetNextRun :execrows
UPDATE rotations SET deferred_from=NULL,next_run_at=sqlc.arg(next_run_at),updated_at=sqlc.arg(now) WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='active' AND restore_hold=FALSE;

-- A datastore fault that keeps failing a rotation's start pushes next_run_at out so it
-- yields the head of the queue. The first due time of that wait is kept in deferred_from,
-- which only a real schedule write clears, so the overdue gauge measures from the time the
-- rotation first came due and a stuck rotation cannot defer its way out of it.
-- name: RotationRtDeferNextRun :execrows
UPDATE rotations SET next_run_at=sqlc.arg(next_run_at),deferred_from=COALESCE(deferred_from,next_run_at),updated_at=sqlc.arg(now) WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='active' AND restore_hold=FALSE;

-- The request a started run consumed: cleared in the start transaction.
-- name: RotationRtClearManual :exec
UPDATE rotations SET manual_run_id=NULL,manual_skip_overlap=FALSE WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- The adapter targets that mirror either of the rotation's keys and have not
-- converged to the run's published revision: the adapter targets that may still
-- hold the superseded credential. Tombstoned targets receive nothing.
-- name: RotationRtNotConverged :one
SELECT COUNT(DISTINCT t.id) FROM adapter_targets t JOIN adapter_target_keys k ON k.target_id=t.id AND k.org_id=t.org_id AND k.project_id=t.project_id AND k.environment_id=t.environment_id
 WHERE t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.environment_id=sqlc.arg(chain_env) AND t.state<>'tombstoned' AND k.key_id IN (sqlc.arg(key_id),sqlc.arg(username_key_id)) AND (t.converged_revision IS NULL OR t.converged_revision<sqlc.arg(revision));

-- name: RotationRtHaltRotation :execrows
UPDATE rotations SET deferred_from=NULL,state='halted',halt_reason=sqlc.arg(halt_reason),next_run_at=NULL,manual_run_id=NULL,manual_skip_overlap=FALSE,generation=generation+1,updated_at=sqlc.arg(now) WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='active' AND restore_hold=FALSE;

-- name: RotationRtInsertEffect :exec
INSERT INTO rotation_effects (id,org_id,project_id,environment_id,run_id,kind,intent_audit_id,created_at) VALUES (sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(run_id),sqlc.arg(kind),sqlc.arg(intent_audit_id),sqlc.arg(at));

-- name: RotationRtInsertClosedEffect :exec
INSERT INTO rotation_effects (id,org_id,project_id,environment_id,run_id,kind,intent_audit_id,outcome_audit_id,outcome,created_at,finished_at) VALUES (sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(run_id),sqlc.arg(kind),sqlc.arg(intent_audit_id),sqlc.arg(outcome_audit_id),sqlc.arg(outcome),sqlc.arg(at),sqlc.arg(at));

-- name: RotationRtCloseEffect :exec
UPDATE rotation_effects SET outcome=sqlc.arg(outcome),outcome_audit_id=sqlc.arg(outcome_audit_id),finished_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND run_id=sqlc.arg(run_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND outcome IS NULL;

-- name: RotationRtInsertAudit :exec
INSERT INTO audit_tenant_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,scope_class,org_id,project_id,env_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES (sqlc.arg(id),sqlc.arg(type),1,sqlc.arg(at),false,sqlc.arg(at),NULL,'system',sqlc.arg(authority_id),'env',sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),'rotation',sqlc.arg(rotation_id),sqlc.arg(outcome),sqlc.arg(run_id),'system',sqlc.arg(payload));

-- The lease cut is now minus one provider deadline: a run is taken over only that
-- long after its lease expired, so a provider call the old worker started inside
-- its lease has finished or timed out before the takeover's rollback drops.
-- hikyo:reason RotationRuntime closed scheduler claims one due run across tenants under the per-org concurrency cap; writes bind the selected immutable chain and the claim token. Selections: an in-flight run, a pre-publish run whose candidate is not confirmed dropped (the candidate reconciler, published_revision IS NULL only; the in-flight and reconciler selections skip a rotation a restore holds, because the archive may not know a publish the live instance made and a drop would kill a credential consumers fetched), a post-publish run that still owes the revoke of its superseded role (the revoke-obligation loop), and a settled run whose delivered credential is due an extension.
-- hikyo:instance-scoped
-- name: RotationRtClaimDueRun :one
SELECT ru.id,ru.org_id,ru.project_id,ru.environment_id,ru.rotation_id,ru.generation AS run_generation,ru.run_trigger,ru.phase,ru.candidate_handle,ru.superseded_handle,ru.candidate_valid_until AS candidate_valid_until_at,ru.revoke_not_before_at,ru.published_revision,ru.revocation_delay_seconds,ru.started_at,ru.phase_at,ru.next_attempt_at,ru.attempt_count,ru.last_error,ru.lease_claim_token,ro.provider_id,ro.key_id,ro.username_key_id,ro.interval_seconds,ro.revocation_delay_seconds AS rotation_delay_seconds,ro.authority_principal_id,ro.state AS rotation_state,ro.restore_hold AS rotation_restore_hold,ro.generation AS rotation_generation
 FROM rotation_runs ru JOIN rotations ro ON ro.org_id=ru.org_id AND ro.project_id=ru.project_id AND ro.environment_id=ru.environment_id AND ro.id=ru.rotation_id
 WHERE ru.next_attempt_at<=sqlc.arg(now)
 AND (ru.lease_owner IS NULL OR ru.lease_expires_at IS NULL OR ru.lease_expires_at<=sqlc.arg(lease_cut))
 AND (ru.phase IN ('published','overlap','revoking','revoke_refused')
  OR (ru.phase IN ('minting','minted') AND NOT ro.restore_hold)
  OR (ru.phase IN ('failed','quarantine') AND ru.published_revision IS NULL AND NOT ro.restore_hold AND NOT EXISTS (SELECT 1 FROM rotation_effects ef WHERE ef.run_id=ru.id AND ef.kind='drop_candidate' AND ef.outcome='success'))
  OR (ru.phase IN ('failed','quarantine') AND ru.published_revision IS NOT NULL AND ru.superseded_handle IS NOT NULL AND NOT EXISTS (SELECT 1 FROM rotation_effects ef WHERE ef.run_id=ru.id AND ef.kind='revoke' AND ef.outcome='success'))
  OR (ru.phase='done' AND ru.candidate_valid_until IS NOT NULL AND ro.state='active' AND NOT ro.restore_hold))
 AND (SELECT COUNT(*) FROM rotation_runs x WHERE x.org_id=ru.org_id AND x.lease_owner IS NOT NULL AND x.lease_expires_at>sqlc.arg(now)) < 4
 ORDER BY ru.next_attempt_at,ru.id FOR UPDATE OF ru SKIP LOCKED LIMIT 1;

-- name: RotationRtClaimRun :exec
UPDATE rotation_runs SET attempt_count=sqlc.arg(attempt),lease_owner=sqlc.arg(worker),lease_expires_at=sqlc.arg(until_at),lease_claim_token=sqlc.arg(claim_token) WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RotationRtAssertRun :one
SELECT COUNT(*) FROM rotation_runs WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND lease_owner=sqlc.arg(worker) AND lease_expires_at>sqlc.arg(now) AND lease_claim_token=sqlc.arg(claim_token);

-- The one settling write. It names the phases it may leave, so it is both the
-- exact-row fence and the "fenced claim write" of crash-safety rule 8: it waits on
-- a row lock a publish transaction holds, and matches zero rows once the run has
-- left those phases. A settle that keeps the lease passes keep_lease.
-- A run held in revoke_refused or quarantine with published_versions_changed (a
-- restore may have put the superseded role back in service) keeps that code through
-- any settle that leaves it in those phases, so an extend failure or a wake never
-- replaces the do-not-drop signal; only a settle that passes restore_checked (the
-- revoke step, after it re-read the restore assertion) may change it.
-- name: RotationRtSettleRun :execrows
UPDATE rotation_runs SET phase=sqlc.arg(phase),phase_at=sqlc.arg(now),last_error=CASE WHEN last_error='published_versions_changed' AND phase IN ('revoke_refused','quarantine') AND (sqlc.arg(phase)='revoke_refused' OR sqlc.arg(phase)='quarantine') AND sqlc.arg(restore_checked)::bigint=0 THEN last_error ELSE sqlc.narg(last_error) END,next_attempt_at=sqlc.arg(next_attempt_at),candidate_valid_until=COALESCE(sqlc.narg(valid_until_at),candidate_valid_until),revoke_not_before_at=COALESCE(sqlc.narg(revoke_not_before_at),revoke_not_before_at),published_revision=COALESCE(sqlc.narg(published_revision),published_revision),not_converged_adapter_targets=COALESCE(sqlc.narg(not_converged_adapter_targets),not_converged_adapter_targets),lease_owner=CASE WHEN sqlc.arg(keep_lease)::bigint=1 THEN lease_owner ELSE NULL END,lease_expires_at=CASE WHEN sqlc.arg(keep_lease)::bigint=1 THEN lease_expires_at ELSE NULL END WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND lease_owner=sqlc.arg(worker) AND lease_expires_at>sqlc.arg(now) AND lease_claim_token=sqlc.arg(claim_token) AND phase = ANY(sqlc.arg(from_phases)::text[]);

-- Leaves a settled run due now. Run inside the settle's own transaction, after a
-- settle that would park the run at "never" met a rotation a restore holds: the
-- claim skips a held rotation and a release never re-arms a parked run.
-- name: RotationRtDueRun :execrows
UPDATE rotation_runs SET next_attempt_at=sqlc.arg(now) WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RotationRtGetRun :one
SELECT ru.id,ru.org_id,ru.project_id,ru.environment_id,ru.rotation_id,ru.generation AS run_generation,ru.run_trigger,ru.phase,ru.candidate_handle,ru.superseded_handle,ru.candidate_valid_until AS candidate_valid_until_at,ru.revoke_not_before_at,ru.published_revision,ru.revocation_delay_seconds,ru.started_at,ru.phase_at,ru.next_attempt_at,ru.attempt_count,ru.last_error,ru.lease_claim_token,ro.provider_id,ro.key_id,ro.username_key_id,ro.interval_seconds,ro.revocation_delay_seconds AS rotation_delay_seconds,ro.authority_principal_id,ro.state AS rotation_state,ro.restore_hold AS rotation_restore_hold,ro.generation AS rotation_generation
 FROM rotation_runs ru JOIN rotations ro ON ro.org_id=ru.org_id AND ro.project_id=ru.project_id AND ro.environment_id=ru.environment_id AND ro.id=ru.rotation_id
 WHERE ru.id=sqlc.arg(run_id) AND ru.org_id=sqlc.arg(chain_org) AND ru.project_id=sqlc.arg(chain_project) AND ru.environment_id=sqlc.arg(chain_env);

-- Failed runs of one rotation that never published, started after a cut: the
-- attempts a cycle has spent. A run that failed only because the executor's own
-- datastore or keyring faulted (infrastructure_unavailable) is not an
-- attempt: the provider was never tried, so it must not exhaust the retries. A zero cut counts the rotation's whole history.
-- name: RotationRtFailedAttempts :one
SELECT COUNT(*) FROM rotation_runs WHERE rotation_id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation) AND phase IN ('failed','quarantine') AND published_revision IS NULL AND (last_error IS NULL OR last_error<>'infrastructure_unavailable') AND started_at>sqlc.arg(since_at);

-- Failed runs of one rotation that never published and failed only because the
-- executor's own datastore or keyring faulted, started after a cut: the
-- infrastructure faults this cycle, which size the infrastructure backoff. They are not attempts, so they never
-- exhaust the retries, but they must still back off instead of retrying at the floor.
-- name: RotationRtInfraFaults :one
SELECT COUNT(*) FROM rotation_runs WHERE rotation_id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation) AND phase IN ('failed','quarantine') AND published_revision IS NULL AND last_error='infrastructure_unavailable' AND started_at>sqlc.arg(since_at);

-- The commit time of the snapshot a run published: the single anchor P of the
-- failure schedule. It is the publish transaction's own timestamp.
-- name: RotationRtSnapshotPublishedAt :one
SELECT published_at FROM snapshots WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND revision=sqlc.arg(revision);

-- name: RotationRtCurrentEntry :one
SELECT id FROM value_entries WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND key_id=sqlc.arg(key_id);

-- The value versions a key legitimately serves after a run: the entries pinned by
-- the snapshot of every published revision of any run (this one or a later one)
-- that drove the key. Ids only; no secret is read.
-- name: RotationRtPublishedEntries :many
SELECT DISTINCT se.value_entry_id FROM rotation_runs ru JOIN rotations ro ON ro.org_id=ru.org_id AND ro.project_id=ru.project_id AND ro.environment_id=ru.environment_id AND ro.id=ru.rotation_id JOIN snapshots sn ON sn.org_id=ru.org_id AND sn.project_id=ru.project_id AND sn.environment_id=ru.environment_id AND sn.revision=ru.published_revision JOIN snapshot_entries se ON se.snapshot_id=sn.id AND se.org_id=sn.org_id AND se.project_id=sn.project_id AND se.environment_id=sn.environment_id WHERE ru.org_id=sqlc.arg(chain_org) AND ru.project_id=sqlc.arg(chain_project) AND ru.environment_id=sqlc.arg(chain_env) AND se.key_id=sqlc.arg(key_id) AND (ro.key_id=sqlc.arg(key_id) OR ro.username_key_id=sqlc.arg(key_id)) AND ru.published_revision>=sqlc.arg(from_revision);

-- Whether a restore holds the rotation. A held rotation's claim, schedule and halt
-- writes are all refused, so the executor reads this to keep a run due instead of
-- parking it when a refusal it cannot act on (a halt) would otherwise end it.
-- name: RotationRtRestoreHeld :one
SELECT COUNT(*) FROM rotations WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND restore_hold=TRUE;

-- Whether a later run published for the key: the delivered credential of this run
-- is superseded and must not be extended.
-- name: RotationRtLaterPublished :one
SELECT COUNT(*) FROM rotation_runs ru JOIN rotations ro ON ro.org_id=ru.org_id AND ro.project_id=ru.project_id AND ro.environment_id=ru.environment_id AND ro.id=ru.rotation_id WHERE ru.org_id=sqlc.arg(chain_org) AND ru.project_id=sqlc.arg(chain_project) AND ru.environment_id=sqlc.arg(chain_env) AND (ro.key_id=sqlc.arg(key_id) OR ro.username_key_id=sqlc.arg(key_id)) AND ru.published_revision IS NOT NULL AND ru.published_revision>sqlc.arg(revision);

-- name: RotationRtLoadProviderMaterial :one
SELECT p.kind,p.origin,p.tls_mode,p.grant_role,p.id,p.admin_credential_ciphertext FROM dynamic_providers p JOIN rotation_runs ru ON ru.org_id=p.org_id AND ru.project_id=p.project_id JOIN rotations ro ON ro.org_id=ru.org_id AND ro.project_id=ru.project_id AND ro.environment_id=ru.environment_id AND ro.id=ru.rotation_id AND ro.provider_id=p.id WHERE ru.id=sqlc.arg(run_id) AND ru.org_id=sqlc.arg(chain_org) AND ru.project_id=sqlc.arg(chain_project) AND ru.environment_id=sqlc.arg(chain_env) AND ru.lease_owner=sqlc.arg(worker) AND ru.lease_expires_at>sqlc.arg(now) AND ru.lease_claim_token=sqlc.arg(claim_token);

-- The role a published run replaces, bound to the provider that MINTED it: the run
-- whose candidate_handle is this run's superseded_handle, and that run's rotation's
-- provider. It is not this run's own provider, which a recreated rotation may have
-- moved to another server; dropping or probing a handle on the wrong server would
-- read "absent" as "dead". valid_until_at is the minting run's recorded VALID UNTIL
-- and extend_uncertain counts its extends whose outcome is open or unknown (the
-- recorded VALID UNTIL is then a lower bound, not a fact).
-- hikyo:reason The superseded role is minted by another run of the same org, project and environment chain (joined on every chain column), under a rotation of possibly another provider; the read is fenced on the claimed run's lease and chain, and the extend-uncertainty count is a chain-bound subquery.
-- hikyo:instance-scoped
-- name: RotationRtLoadSupersededRole :one
SELECT p.kind,p.origin,p.tls_mode,p.grant_role,p.id,p.admin_credential_ciphertext,sup.candidate_valid_until AS valid_until_at,
 (SELECT COUNT(*) FROM rotation_effects ef WHERE ef.run_id=sup.id AND ef.org_id=sup.org_id AND ef.project_id=sup.project_id AND ef.environment_id=sup.environment_id AND ef.kind='extend' AND (ef.outcome IS NULL OR ef.outcome='unknown')) AS extend_uncertain
 FROM rotation_runs ru
 JOIN rotation_runs sup ON sup.org_id=ru.org_id AND sup.project_id=ru.project_id AND sup.environment_id=ru.environment_id AND sup.candidate_handle=ru.superseded_handle
 JOIN rotations ro ON ro.org_id=sup.org_id AND ro.project_id=sup.project_id AND ro.environment_id=sup.environment_id AND ro.id=sup.rotation_id
 JOIN dynamic_providers p ON p.org_id=ro.org_id AND p.project_id=ro.project_id AND p.id=ro.provider_id
 WHERE ru.id=sqlc.arg(run_id) AND ru.org_id=sqlc.arg(chain_org) AND ru.project_id=sqlc.arg(chain_project) AND ru.environment_id=sqlc.arg(chain_env) AND ru.lease_owner=sqlc.arg(worker) AND ru.lease_expires_at>sqlc.arg(now) AND ru.lease_claim_token=sqlc.arg(claim_token);

-- name: RotationRtOpenRunCount :one
SELECT COUNT(*) FROM rotation_runs WHERE rotation_id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND phase NOT IN ('done','quarantine','failed');

-- The effect a crashed worker left open: its INTENT is durable and its OUTCOME is
-- not, so the resumed run re-probes the provider instead of repeating the effect.
-- name: RotationRtOpenEffect :one
SELECT id,kind FROM rotation_effects WHERE run_id=sqlc.arg(run_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND outcome IS NULL ORDER BY created_at DESC,id DESC LIMIT 1;

-- The schedule's anchors for one rotation cycle: the newest revision this
-- rotation published at its current generation (P is that snapshot's commit
-- time) and the first run it started at this generation (a cycle with no publish
-- of its own is anchored at its first attempt).
-- name: RotationRtOwnPublished :one
SELECT published_revision FROM rotation_runs WHERE rotation_id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation) AND published_revision IS NOT NULL ORDER BY published_revision DESC LIMIT 1;

-- name: RotationRtCycleStart :one
SELECT started_at FROM rotation_runs WHERE rotation_id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation) ORDER BY started_at,id LIMIT 1;

-- The instance-wide counts /metrics and doctor read, so an operator hears about a
-- rotation that needs them without opening each one. quarantined and revoke_refused
-- are runs that still owe a revoke or hold a possibly live role; unknown_outcome is
-- the runs that are not settled and recorded an effect as unknown; halted is the
-- rotations a failed gate or an exhausted retry window stopped; overdue is the
-- active rotations nothing has started although they came due before the grace;
-- expiring is the rotations (not deleted) whose delivered credential expires after now and
-- before the horizon (a lapsed credential is not counted), derived the way credential_expires_at is for the rotation's password key.
-- hikyo:reason RotationRuntime metrics scrape and doctor read only instance-wide counts of rotations and runs that need an operator through the closed runtime boundary; no tenant identity leaves it.
-- hikyo:instance-scoped
-- name: RotationRtGauges :one
SELECT
 (SELECT COUNT(*) FROM rotation_runs WHERE phase='quarantine') AS quarantined,
 (SELECT COUNT(*) FROM rotation_runs WHERE phase='revoke_refused') AS revoke_refused,
 (SELECT COUNT(*) FROM rotation_runs ru WHERE ru.phase NOT IN ('done','failed') AND EXISTS (SELECT 1 FROM rotation_effects ef WHERE ef.run_id=ru.id AND ef.outcome='unknown')) AS unknown_outcome,
 (SELECT COUNT(*) FROM rotations WHERE state='halted') AS halted,
 (SELECT COUNT(*) FROM rotations WHERE state<>'disabled' AND restore_hold) AS restore_held,
 (SELECT COUNT(*) FROM rotations ro WHERE ro.state='active' AND NOT ro.restore_hold AND COALESCE(ro.deferred_from,ro.next_run_at)<sqlc.arg(overdue_at) AND NOT EXISTS (SELECT 1 FROM rotation_runs op WHERE op.rotation_id=ro.id AND op.phase NOT IN ('done','quarantine','failed'))) AS overdue,
 (SELECT COUNT(*) FROM rotations ro WHERE ro.state<>'disabled' AND (SELECT ru.candidate_valid_until FROM rotation_runs ru JOIN rotations r2 ON r2.org_id=ru.org_id AND r2.project_id=ru.project_id AND r2.environment_id=ru.environment_id AND r2.id=ru.rotation_id WHERE ru.org_id=ro.org_id AND ru.project_id=ro.project_id AND ru.environment_id=ro.environment_id AND (r2.key_id=ro.key_id OR r2.username_key_id=ro.key_id) AND ru.published_revision IS NOT NULL ORDER BY ru.published_revision DESC,ru.id DESC LIMIT 1)>sqlc.arg(now_at) AND (SELECT ru.candidate_valid_until FROM rotation_runs ru JOIN rotations r2 ON r2.org_id=ru.org_id AND r2.project_id=ru.project_id AND r2.environment_id=ru.environment_id AND r2.id=ru.rotation_id WHERE ru.org_id=ro.org_id AND ru.project_id=ro.project_id AND ru.environment_id=ro.environment_id AND (r2.key_id=ro.key_id OR r2.username_key_id=ro.key_id) AND ru.published_revision IS NOT NULL ORDER BY ru.published_revision DESC,ru.id DESC LIMIT 1)<sqlc.arg(expiring_at)) AS expiring;
