-- name: TransitListKeys :many
SELECT id,environment_id,name,algorithm,custody,allowed_operations,state,latest_version,min_encrypt_version,min_decrypt_version,min_available_version,compromised_through_version,rotation_period_seconds,deletion_after,created_by,created_at,updated_at FROM transit_keys WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'destroyed' ORDER BY name;

-- name: TransitGetKey :one
SELECT id,environment_id,name,algorithm,custody,allowed_operations,state,latest_version,min_encrypt_version,min_decrypt_version,min_available_version,compromised_through_version,rotation_period_seconds,deletion_after,created_by,created_at,updated_at FROM transit_keys WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND name=sqlc.arg(name) AND state<>'destroyed';

-- name: TransitGetKeyForUse :one
SELECT id,environment_id,name,algorithm,custody,allowed_operations,state,latest_version,min_encrypt_version,min_decrypt_version,min_available_version,compromised_through_version,rotation_period_seconds,deletion_after,created_by,created_at,updated_at FROM transit_keys WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND name=sqlc.arg(name) AND state<>'destroyed' FOR SHARE;

-- name: TransitAdmissionLock :one
SELECT id FROM environments WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND id=sqlc.arg(chain_env) FOR UPDATE;

-- name: TransitCountKeys :one
SELECT COUNT(*) FROM transit_keys WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'destroyed';

-- name: TransitListVersions :many
SELECT id,version,public_key,CAST(CASE WHEN material_ciphertext IS NULL THEN 0 ELSE 1 END AS INTEGER) AS has_material,CAST(CASE WHEN external_ref IS NULL THEN 0 ELSE 1 END AS INTEGER) AS external_held,created_at FROM transit_key_versions WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND key_id=sqlc.arg(key_id) ORDER BY version;

-- name: TransitListCallers :many
SELECT principal_id,operations FROM transit_key_callers WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND key_id=sqlc.arg(key_id) ORDER BY principal_id;

-- name: TransitVersionMaterial :one
SELECT id,version,material_ciphertext,external_ref,public_key FROM transit_key_versions WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND key_id=sqlc.arg(key_id) AND version=sqlc.arg(version);

-- name: TransitInsertVersion :exec
INSERT INTO transit_key_versions (id,org_id,project_id,environment_id,key_id,version,material_ciphertext,external_ref,public_key,created_at) VALUES (sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(key_id),sqlc.arg(version),sqlc.arg(material_ciphertext),sqlc.arg(external_ref),sqlc.arg(public_key),sqlc.arg(at));

-- name: TransitDeleteCallers :exec
DELETE FROM transit_key_callers WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND key_id=sqlc.arg(key_id);

-- name: TransitInsertCaller :exec
INSERT INTO transit_key_callers (org_id,project_id,environment_id,key_id,principal_id,operations) VALUES (sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(key_id),sqlc.arg(principal_id),sqlc.arg(operations));

-- name: TransitInsertKey :exec
INSERT INTO transit_keys (id,org_id,project_id,environment_id,name,algorithm,custody,allowed_operations,exportable,state,latest_version,min_encrypt_version,min_decrypt_version,compromised_through_version,rotation_period_seconds,deletion_after,created_by,created_at,updated_at) VALUES (sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(name),sqlc.arg(algorithm),sqlc.arg(custody),sqlc.arg(allowed_operations),0,'active',1,1,1,0,sqlc.arg(rotation_period_seconds),NULL,sqlc.arg(created_by),sqlc.arg(at),sqlc.arg(at));

-- name: TransitKeyByID :one
SELECT id,environment_id,name,algorithm,custody,allowed_operations,state,latest_version,min_encrypt_version,min_decrypt_version,min_available_version,compromised_through_version,rotation_period_seconds,deletion_after,created_by,created_at,updated_at FROM transit_keys WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: TransitCountExistingKey :one
SELECT COUNT(*) FROM transit_keys WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'destroyed';

-- name: TransitAppendVersion :execrows
UPDATE transit_keys SET latest_version=sqlc.arg(version),updated_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND latest_version=sqlc.arg(expect_latest) AND state IN ('active','retired','disabled');

-- name: TransitCountVersions :one
SELECT COUNT(*) FROM transit_key_versions WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND key_id=sqlc.arg(key_id);

-- name: TransitConfigure :execrows
UPDATE transit_keys SET min_encrypt_version=sqlc.arg(min_encrypt),min_decrypt_version=sqlc.arg(min_decrypt),rotation_period_seconds=sqlc.arg(rotation_period),updated_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state NOT IN ('destroyed','pending-deletion') AND sqlc.arg(min_encrypt)<=latest_version AND sqlc.arg(min_decrypt)>=min_available_version;

-- name: TransitChangeState :execrows
UPDATE transit_keys SET state=sqlc.arg(next_state),deletion_after=sqlc.arg(deletion_after),updated_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND (state<>'pending-deletion' OR (deletion_after>sqlc.arg(at) AND purge_started=0)) AND state=ANY(sqlc.arg(from_states)::text[]);

-- name: TransitCompromise :execrows
UPDATE transit_keys SET compromised_through_version=latest_version,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'destroyed';

-- name: TransitCompromisedThrough :one
SELECT compromised_through_version FROM transit_keys WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: TransitFenceTrim :execrows
UPDATE transit_keys SET min_available_version=min_decrypt_version,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state NOT IN ('destroyed','pending-deletion');

-- name: TransitTrimFloor :one
SELECT min_available_version FROM transit_keys WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: TransitAvailableTrimFloor :one
SELECT min_available_version FROM transit_keys WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state NOT IN ('destroyed','pending-deletion');

-- name: TransitTrimVersions :execrows
DELETE FROM transit_key_versions WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND key_id=sqlc.arg(key_id) AND version<sqlc.arg(through);

-- name: TransitListReencrypt :many
SELECT id,environment_id,key_id,material_ciphertext FROM transit_key_versions WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND id>sqlc.arg(cursor) AND material_ciphertext IS NOT NULL ORDER BY id LIMIT CAST(sqlc.arg(page_limit) AS BIGINT);

-- name: TransitReencrypt :execrows
UPDATE transit_key_versions SET material_ciphertext=sqlc.arg(new_ct) WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND id=sqlc.arg(id) AND material_ciphertext=sqlc.arg(old_ct);

-- hikyo:reason StoreTransitSelectDeletionDue verifies the existing closed system scheduler authority; globally lists due pending-deletion keys so each subsequent purge resolves its own tenant chain in a transaction.
-- hikyo:instance-scoped
-- name: TransitSelectDeletionDue :many
SELECT k.org_id,k.project_id,k.id,k.environment_id,k.name,k.algorithm,k.custody,k.allowed_operations,k.state,k.latest_version,k.min_encrypt_version,k.min_decrypt_version,k.min_available_version,k.compromised_through_version,k.rotation_period_seconds,k.deletion_after,k.created_by,k.created_at,k.updated_at FROM transit_keys k WHERE k.state='pending-deletion' AND k.deletion_after<=sqlc.arg(now) AND k.id>sqlc.arg(after_id) ORDER BY k.id LIMIT CAST(sqlc.arg(page_limit) AS BIGINT);

-- hikyo:reason StoreTransitSelectRotationDue verifies the existing closed system scheduler authority; globally lists active rotation candidates with their latest-version timestamp before scoped worker authorization.
-- hikyo:instance-scoped
-- name: TransitSelectRotationDue :many
SELECT k.org_id,k.project_id,k.id,k.environment_id,k.name,k.algorithm,k.custody,k.allowed_operations,k.state,k.latest_version,k.min_encrypt_version,k.min_decrypt_version,k.min_available_version,k.compromised_through_version,k.rotation_period_seconds,k.deletion_after,k.created_by,k.created_at,k.updated_at,v.created_at AS latest_created_at FROM transit_keys k JOIN transit_key_versions v ON v.key_id=k.id AND v.org_id=k.org_id AND v.version=k.latest_version WHERE k.state='active' AND k.rotation_period_seconds>0 AND k.id>sqlc.arg(after_id) ORDER BY k.id LIMIT CAST(sqlc.arg(page_limit) AS BIGINT);

-- name: TransitFencePurge :execrows
UPDATE transit_keys SET purge_started=1 WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='pending-deletion' AND deletion_after<=sqlc.arg(now);

-- name: TransitExternalVersions :many
SELECT id,version,external_ref FROM transit_key_versions WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND key_id=sqlc.arg(key_id) AND external_ref IS NOT NULL ORDER BY version;

-- name: TransitDestroy :execrows
UPDATE transit_keys SET state='destroyed',deletion_after=NULL,updated_at=sqlc.arg(now) WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='pending-deletion' AND deletion_after<=sqlc.arg(now);

-- name: TransitEraseMaterial :execrows
UPDATE transit_key_versions SET material_ciphertext=NULL,external_ref=NULL WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND key_id=sqlc.arg(key_id);
