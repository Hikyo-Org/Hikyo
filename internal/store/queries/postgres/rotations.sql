-- name: RotationCreate :execrows
INSERT INTO rotations (id,org_id,project_id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at) VALUES (sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(provider_id),sqlc.arg(key_id),sqlc.arg(username_key_id),sqlc.arg(interval_seconds),sqlc.arg(revocation_delay_seconds),sqlc.arg(authority_principal_id),sqlc.arg(authority_principal_id),'active',sqlc.arg(next_run_at),1,sqlc.arg(at),sqlc.arg(at));

-- name: RotationGet :one
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at,restore_hold,halt_reason FROM rotations WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- Live rotations only: the environment delete's read of what blocks it. The API's
-- default list uses RotationListOrOwed, which also keeps a deleted rotation that owes a drop.
-- name: RotationList :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at,restore_hold,halt_reason FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'disabled' AND id > sqlc.arg(after_id) ORDER BY id LIMIT sqlc.arg(page_limit);

-- Live rotations, plus a deleted one that owns a run still owing a provider role a
-- drop (phase revoke_refused or quarantine): the default list of the API, so the
-- warning that run owes is not lost with the delete.
-- name: RotationListOrOwed :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at,restore_hold,halt_reason FROM rotations WHERE rotations.org_id=sqlc.arg(chain_org) AND rotations.project_id=sqlc.arg(chain_project) AND rotations.environment_id=sqlc.arg(chain_env) AND (rotations.state<>'disabled' OR EXISTS (SELECT 1 FROM rotation_runs owed WHERE owed.rotation_id=rotations.id AND owed.org_id=rotations.org_id AND owed.project_id=rotations.project_id AND owed.environment_id=rotations.environment_id AND owed.phase IN ('revoke_refused','quarantine'))) AND rotations.id > sqlc.arg(after_id) ORDER BY rotations.id LIMIT sqlc.arg(page_limit);

-- name: RotationListAll :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at,restore_hold,halt_reason FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND id > sqlc.arg(after_id) ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: RotationSetState :execrows
UPDATE rotations SET deferred_from=NULL,state=sqlc.arg(state),halt_reason=NULL,manual_run_id=NULL,manual_skip_overlap=FALSE,restore_hold=CASE WHEN sqlc.arg(state)='disabled' THEN FALSE ELSE restore_hold END,next_run_at=sqlc.narg(next_run_at),generation=generation+1,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(expected_generation) AND state<>'disabled';

-- name: RotationCountOpenRuns :one
SELECT COUNT(*) FROM rotation_runs WHERE rotation_id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND phase NOT IN ('done','quarantine','failed');

-- name: RotationClaimInsert :exec
INSERT INTO rotation_key_claims (org_id,project_id,environment_id,key_id,rotation_id) VALUES (sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(key_id),sqlc.arg(rotation_id));

-- name: RotationClaimsRelease :exec
DELETE FROM rotation_key_claims WHERE rotation_id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- The delivered credential's expiry: the VALID UNTIL of the latest published run
-- for this key across rotations, so a delete and recreate on the same keys still
-- reports the role that is live in them.
-- name: RotationCredentialExpiry :one
SELECT ru.id AS run_id, ru.candidate_valid_until AS expires_at FROM rotation_runs ru JOIN rotations ro ON ro.org_id=ru.org_id AND ro.project_id=ru.project_id AND ro.environment_id=ru.environment_id AND ro.id=ru.rotation_id WHERE ru.org_id=sqlc.arg(chain_org) AND ru.project_id=sqlc.arg(chain_project) AND ru.environment_id=sqlc.arg(chain_env) AND (ro.key_id=sqlc.arg(key_id) OR ro.username_key_id=sqlc.arg(key_id)) AND ru.published_revision IS NOT NULL ORDER BY ru.published_revision DESC, ru.id DESC LIMIT 1;

-- The delivered-credential expiry for a page of keys, one query per key role
-- (the password key and the username key of a rotation): the newest published run per key, ranked in SQL so the result holds
-- one row per requested key whatever the run history. The store merges the two
-- roles by published_revision, exactly as the single-key variant orders them, so a
-- key that was one rotation's username key and is now another's password key
-- reads the same in a list and in a get. A page never queries per row.
-- name: RotationCredentialExpiries :many
SELECT key_id AS key_id,run_id AS run_id,expires_at AS expires_at,published_revision AS published_revision FROM (
 SELECT ro.key_id AS key_id,ru.id AS run_id,ru.candidate_valid_until AS expires_at,ru.published_revision AS published_revision,ROW_NUMBER() OVER (PARTITION BY ro.key_id ORDER BY ru.published_revision DESC,ru.id DESC) AS ordinal
 FROM rotation_runs ru JOIN rotations ro ON ro.org_id=ru.org_id AND ro.project_id=ru.project_id AND ro.environment_id=ru.environment_id AND ro.id=ru.rotation_id
 WHERE ru.org_id=sqlc.arg(chain_org) AND ru.project_id=sqlc.arg(chain_project) AND ru.environment_id=sqlc.arg(chain_env) AND ro.key_id = ANY(sqlc.arg(key_ids)::text[]) AND ru.published_revision IS NOT NULL
 ) ranked WHERE ordinal=1 ORDER BY key_id;

-- name: RotationCredentialExpiriesByUsernameKey :many
SELECT key_id AS key_id,run_id AS run_id,expires_at AS expires_at,published_revision AS published_revision FROM (
 SELECT ro.username_key_id AS key_id,ru.id AS run_id,ru.candidate_valid_until AS expires_at,ru.published_revision AS published_revision,ROW_NUMBER() OVER (PARTITION BY ro.username_key_id ORDER BY ru.published_revision DESC,ru.id DESC) AS ordinal
 FROM rotation_runs ru JOIN rotations ro ON ro.org_id=ru.org_id AND ro.project_id=ru.project_id AND ro.environment_id=ru.environment_id AND ro.id=ru.rotation_id
 WHERE ru.org_id=sqlc.arg(chain_org) AND ru.project_id=sqlc.arg(chain_project) AND ru.environment_id=sqlc.arg(chain_env) AND ro.username_key_id = ANY(sqlc.arg(key_ids)::text[]) AND ru.published_revision IS NOT NULL
 ) ranked WHERE ordinal=1 ORDER BY key_id;

-- The newest run of each rotation of a page (the top row of its runs list), and the count of
-- runs that still owe a provider role a drop. One query each for the whole page, never one per
-- rotation. The newest run is the one with the latest started_at, id breaking a tie: run ids are
-- UUIDv7 stamped from the same clock as started_at and a rotation has at most one open run, so it is
-- the runs list's first row (id DESC), and the read is one probe of rotation_runs_rotation
-- (rotation_id, started_at) per rotation, never a scan of its history.
-- A run owes a drop in exactly the phases the CLI ATTENTION lines, the web notice and the
-- hikyo_rotation_runs_{quarantined,revoke_refused} gauges count: revoke_refused and quarantine,
-- whatever their error code. versions_changed is the part of that count a restore refused
-- (published_versions_changed): its role may be served again.
-- name: RotationLastRuns :many
SELECT ro.id AS rotation_id,ru.id AS id,ru.phase AS phase,ru.last_error AS last_error,ru.phase_at AS phase_at
FROM rotations ro JOIN rotation_runs ru ON ru.rotation_id=ro.id AND ru.org_id=ro.org_id AND ru.project_id=ro.project_id AND ru.environment_id=ro.environment_id
WHERE ro.org_id=sqlc.arg(chain_org) AND ro.project_id=sqlc.arg(chain_project) AND ro.environment_id=sqlc.arg(chain_env) AND ro.id = ANY(sqlc.arg(rotation_ids)::text[])
AND ru.id=(SELECT latest.id FROM rotation_runs latest WHERE latest.rotation_id=ro.id AND latest.org_id=ro.org_id AND latest.project_id=ro.project_id AND latest.environment_id=ro.environment_id ORDER BY latest.started_at DESC,latest.id DESC LIMIT 1)
ORDER BY ro.id;

-- name: RotationOwedDropCounts :many
SELECT rotation_id AS rotation_id,COUNT(*) AS owed,COUNT(CASE WHEN last_error='published_versions_changed' THEN 1 END) AS versions_changed FROM rotation_runs
WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND rotation_id = ANY(sqlc.arg(rotation_ids)::text[]) AND phase IN ('revoke_refused','quarantine')
GROUP BY rotation_id ORDER BY rotation_id;

-- A run blocks an environment delete while it may hold a live role: every phase
-- except done and failed (quarantine and revoke_refused included), plus a failed
-- run that had already published. A failed run that never published had its
-- candidate confirmed dropped, so it and done are the only purgeable runs. The
-- distinct rotation ids are returned so the refusal can name them and one rotation
-- with many runs cannot hide the others under the page limit.
-- name: RotationBlockingRunRotations :many
SELECT DISTINCT rotation_id FROM rotation_runs WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND phase NOT IN ('done','failed') ORDER BY rotation_id LIMIT sqlc.arg(page_limit);

-- name: RotationPublishedFailedRunRotations :many
SELECT DISTINCT rotation_id FROM rotation_runs WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND phase='failed' AND published_revision IS NOT NULL ORDER BY rotation_id LIMIT sqlc.arg(page_limit);

-- name: RotationPurgeEffects :exec
DELETE FROM rotation_effects WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RotationPurgeRuns :exec
DELETE FROM rotation_runs WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RotationPurgeClaims :exec
DELETE FROM rotation_key_claims WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RotationPurge :exec
DELETE FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RotationListActiveByProvider :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at,restore_hold,halt_reason FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND provider_id=sqlc.arg(provider_id) AND state='active' ORDER BY id;

-- name: RotationPauseActive :execrows
UPDATE rotations SET deferred_from=NULL,state='paused',next_run_at=NULL,manual_run_id=NULL,manual_skip_overlap=FALSE,generation=generation+1,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: RotationListActiveInEnvironment :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at,restore_hold,halt_reason FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND state='active' ORDER BY id;

-- name: RotationListActiveInProject :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at,restore_hold,halt_reason FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active' ORDER BY id;

-- Abandon closes every unsettled run and bumps the claim token, so a worker still
-- holding the run loses its fence. A pre-publish run becomes quarantine and is due
-- now, but it keeps its lease owner and its lease ends at the abandon time: a
-- worker that was mid-provider call is not stopped by the token, and the claim's
-- lease cut (one provider deadline past the lease end) makes the candidate
-- reconciler wait for that call to finish before it probes and drops the candidate.
-- A pre-publish run nobody held has no lease to keep and is claimable at once. A
-- published run that owes a revoke becomes quarantine and keeps its overlap deadline (the
-- revoke-obligation loop drops the superseded role after it), except a run still in
-- phase published: its stored deadline was read before the publish committed, so
-- it takes published_due_at, the abandon time plus the revocation delay, an observation
-- after the commit. A published run with
-- no superseded role owes nothing, so it settles straight to done; left in
-- quarantine it would hold its provider and environment forever. RETURNING is the
-- list of the rows this statement actually changed, so the audit rows the caller
-- writes match them one to one. A run holding published_versions_changed keeps that
-- code: an abandoning delete must not turn the do-not-drop signal into a plain abandon.
-- name: RotationAbandonOpenRuns :many
UPDATE rotation_runs SET phase=CASE WHEN published_revision IS NOT NULL AND superseded_handle IS NULL THEN 'done' ELSE 'quarantine' END,phase_at=sqlc.arg(at),last_error=CASE WHEN last_error='published_versions_changed' THEN last_error ELSE 'abandoned' END,next_attempt_at=CASE WHEN published_revision IS NULL THEN sqlc.arg(at) WHEN phase='published' AND superseded_handle IS NOT NULL THEN sqlc.arg(published_due_at) ELSE next_attempt_at END,lease_owner=CASE WHEN published_revision IS NULL THEN lease_owner ELSE NULL END,lease_expires_at=CASE WHEN published_revision IS NULL AND lease_owner IS NOT NULL THEN sqlc.arg(at) ELSE NULL END,lease_claim_token=lease_claim_token+1 WHERE rotation_id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND phase NOT IN ('done','quarantine','failed') RETURNING id,phase,candidate_handle,superseded_handle;

-- A failed run that never published only stops holding a live role once its
-- candidate drop is recorded: the fenced claim write fails the run before the drop.
-- hikyo:reason A failed run that never published may still hold a live candidate role until a drop is confirmed; the chain is bound on the run row and the effect rows join under the same chain.
-- hikyo:instance-scoped
-- name: RotationUnconfirmedFailedRunRotations :many
SELECT DISTINCT ru.rotation_id FROM rotation_runs ru LEFT JOIN rotation_effects ef ON ef.run_id=ru.id AND ef.org_id=ru.org_id AND ef.project_id=ru.project_id AND ef.environment_id=ru.environment_id AND ef.kind='drop_candidate' AND ef.outcome='success' WHERE ru.org_id=sqlc.arg(chain_org) AND ru.project_id=sqlc.arg(chain_project) AND ru.environment_id=sqlc.arg(chain_env) AND ru.phase='failed' AND ru.published_revision IS NULL AND ef.id IS NULL ORDER BY ru.rotation_id LIMIT sqlc.arg(page_limit);

-- Runs that need a provider to settle: a run of one of its rotations that may
-- hold a live role (every phase but done, and a failed run that never published
-- without a confirmed drop), and a run of ANY rotation that still owes the revoke
-- of a superseded role this provider minted (a recreated rotation may have moved
-- to another provider, but the old role is only droppable where it was minted). A
-- run that never published and has settled (failed, quarantine) never revokes that
-- role, so it does not count.
-- hikyo:reason The provider delete guard counts runs across the provider's rotations, and runs of other rotations that owe a role this provider minted, under the org and project chain; the effect join and the minting-run subquery are pinned to the run's own chain.
-- hikyo:instance-scoped
-- name: RotationProviderLiveRuns :one
SELECT COUNT(*) FROM rotation_runs ru JOIN rotations ro ON ro.org_id=ru.org_id AND ro.project_id=ru.project_id AND ro.environment_id=ru.environment_id AND ro.id=ru.rotation_id LEFT JOIN rotation_effects ef ON ef.run_id=ru.id AND ef.org_id=ru.org_id AND ef.project_id=ru.project_id AND ef.environment_id=ru.environment_id AND ef.kind='drop_candidate' AND ef.outcome='success' WHERE ru.org_id=sqlc.arg(chain_org) AND ru.project_id=sqlc.arg(chain_project) AND ru.phase<>'done' AND NOT (ru.phase='failed' AND ru.published_revision IS NULL AND ef.id IS NOT NULL) AND (ro.provider_id=sqlc.arg(provider_id) OR ((ru.published_revision IS NOT NULL OR ru.phase IN ('minting','minted','staged','published')) AND EXISTS (SELECT 1 FROM rotation_runs sup JOIN rotations so ON so.org_id=sup.org_id AND so.project_id=sup.project_id AND so.environment_id=sup.environment_id AND so.id=sup.rotation_id WHERE sup.org_id=ru.org_id AND sup.project_id=ru.project_id AND sup.environment_id=ru.environment_id AND sup.candidate_handle=ru.superseded_handle AND so.provider_id=sqlc.arg(provider_id))));

-- A run-now request: records the run it asks for and makes the rotation due now.
-- It matches only an active rotation at the expected generation with no pending
-- request; the service re-counts open runs after this write, in the same
-- transaction, and the caller reads the row to say which condition refused.
-- name: RotationRequestRun :execrows
UPDATE rotations SET deferred_from=NULL,manual_run_id=sqlc.arg(run_id),manual_skip_overlap=sqlc.arg(skip_overlap),next_run_at=sqlc.arg(now),updated_at=sqlc.arg(now) WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='active' AND generation=sqlc.arg(expected_generation) AND manual_run_id IS NULL;

-- Whether a run-now request is pending for the rotation.
-- name: RotationManualPending :one
SELECT COUNT(*) FROM rotations WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND manual_run_id IS NOT NULL;

-- A page of a rotation's runs, newest first. Run ids are time-ordered (UUIDv7); a
-- manual run's id is minted when it is requested and it is the first run to start
-- after the request, so id order is start order. The cursor is the last run id of the
-- previous page. Only operator-visible metadata: no handle, no candidate, no
-- provider detail beyond the fixed-word last_error code.
-- candidate_valid_until and superseded_handle let a run that still owes a role drop say when that role
-- lapses on its own; the superseded role's expiry is read per owed run (RotationRunMinterByHandle).
-- name: RotationListRuns :many
SELECT id,run_trigger,phase,published_revision,revocation_delay_seconds,started_at,phase_at,last_error,not_converged_adapter_targets,candidate_valid_until,superseded_handle FROM rotation_runs WHERE rotation_id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) ORDER BY id DESC LIMIT sqlc.arg(page_limit);

-- name: RotationListRunsAfter :many
SELECT id,run_trigger,phase,published_revision,revocation_delay_seconds,started_at,phase_at,last_error,not_converged_adapter_targets,candidate_valid_until,superseded_handle FROM rotation_runs WHERE rotation_id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND id < sqlc.arg(after_id) ORDER BY id DESC LIMIT sqlc.arg(page_limit);

-- The run that minted a role, by its handle, under the chain: the same lookup the executor makes for a
-- superseded role (chain and handle only, never the rotation, so a recreated rotation finds it too).
-- Its candidate_valid_until is that role's recorded VALID UNTIL.
-- name: RotationRunMinterByHandle :one
SELECT id,candidate_valid_until FROM rotation_runs WHERE candidate_handle=sqlc.arg(handle) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) ORDER BY id DESC LIMIT 1;

-- Extends of a run whose outcome is open or unknown: its recorded VALID UNTIL is then only a lower bound.
-- name: RotationRunExtendUncertain :one
SELECT COUNT(*) FROM rotation_effects WHERE run_id=sqlc.arg(run_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND kind='extend' AND (outcome IS NULL OR outcome='unknown');

-- Clears the restore hold of one rotation. The generation is not bumped: a run in
-- flight asserts the generation it started under, and a release must not
-- invalidate it. Zero rows means the rotation is gone or was not held.
-- name: RotationReleaseHold :execrows
UPDATE rotations SET restore_hold=FALSE,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND restore_hold=TRUE;

-- Incident enumeration (#1471): one org's live rotations whose recorded authority
-- principal is the contained account, in id order, ids and the state only.
-- hikyo:reason Org-bounded incident enumeration lists the live rotation identifiers of one authority principal across the projects of one org behind the org credential-reset operation; the org_id predicate is the tenant bound and no project chain exists because the page spans the org's projects.
-- hikyo:instance-scoped
-- name: RotationListByAuthorityInOrg :many
SELECT id,project_id,environment_id,state FROM rotations WHERE org_id=sqlc.arg(chain_org) AND authority_principal_id=sqlc.arg(authority_principal_id) AND state<>'disabled' AND id > sqlc.arg(after_id) ORDER BY id LIMIT sqlc.arg(row_limit);

-- The same page across every org, for an account whose footprint spans several.
-- hikyo:reason Instance-level incident enumeration lists the live rotation identifiers of one authority principal across orgs only behind the instance credential-reset operation, whose proof carries an empty chain; the page is bounded and no rotation secret, key or provider detail is selected.
-- hikyo:instance-scoped
-- name: RotationListByAuthorityInInstance :many
SELECT id,org_id,project_id,environment_id,state FROM rotations WHERE authority_principal_id=sqlc.arg(authority_principal_id) AND state<>'disabled' AND id > sqlc.arg(after_id) ORDER BY id LIMIT sqlc.arg(row_limit);
