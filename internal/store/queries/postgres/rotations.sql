-- name: RotationCreate :execrows
INSERT INTO rotations (id,org_id,project_id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at) VALUES (sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(provider_id),sqlc.arg(key_id),sqlc.arg(username_key_id),sqlc.arg(interval_seconds),sqlc.arg(revocation_delay_seconds),sqlc.arg(authority_principal_id),sqlc.arg(authority_principal_id),'active',sqlc.arg(next_run_at),1,sqlc.arg(at),sqlc.arg(at));

-- name: RotationGet :one
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at FROM rotations WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RotationList :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'disabled' AND id > sqlc.arg(after_id) ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: RotationListAll :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND id > sqlc.arg(after_id) ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: RotationSetState :execrows
UPDATE rotations SET state=sqlc.arg(state),halt_reason=NULL,next_run_at=sqlc.narg(next_run_at),generation=generation+1,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(expected_generation) AND state<>'disabled';

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
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND provider_id=sqlc.arg(provider_id) AND state='active' ORDER BY id;

-- name: RotationPauseActive :execrows
UPDATE rotations SET state='paused',next_run_at=NULL,generation=generation+1,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: RotationListActiveInEnvironment :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND state='active' ORDER BY id;

-- name: RotationListActiveInProject :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active' ORDER BY id;

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
-- writes match them one to one.
-- name: RotationAbandonOpenRuns :many
UPDATE rotation_runs SET phase=CASE WHEN published_revision IS NOT NULL AND superseded_handle IS NULL THEN 'done' ELSE 'quarantine' END,phase_at=sqlc.arg(at),last_error='abandoned',next_attempt_at=CASE WHEN published_revision IS NULL THEN sqlc.arg(at) WHEN phase='published' AND superseded_handle IS NOT NULL THEN sqlc.arg(published_due_at) ELSE next_attempt_at END,lease_owner=CASE WHEN published_revision IS NULL THEN lease_owner ELSE NULL END,lease_expires_at=CASE WHEN published_revision IS NULL AND lease_owner IS NOT NULL THEN sqlc.arg(at) ELSE NULL END,lease_claim_token=lease_claim_token+1 WHERE rotation_id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND phase NOT IN ('done','quarantine','failed') RETURNING id,phase,candidate_handle,superseded_handle;

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
