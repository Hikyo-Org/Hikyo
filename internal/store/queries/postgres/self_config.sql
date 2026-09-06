-- Every query is singleton owner-local metadata; no secret material lives here.
-- hikyo:instance-scoped
-- name: GetSelfConfigBinding :one
SELECT * FROM self_config_binding WHERE id = 1;
-- hikyo:instance-scoped
-- name: LockSelfConfigBinding :one
SELECT * FROM self_config_binding WHERE id = 1 FOR UPDATE;
-- hikyo:instance-scoped
-- name: CreateSelfConfigBinding :execrows
INSERT INTO self_config_binding(id,owner_instance_id,adoption_key,adopted_by,org_id,project_id,environment_id,schema_version,generation,desired_revision,desired_snapshot_id,incarnation,suspended,created_at,updated_at)
SELECT 1,i.identity,sqlc.arg(adoption_key),sqlc.arg(adopted_by),s.org_id,s.project_id,s.environment_id,sqlc.arg(schema_version),1,s.revision,s.id,sqlc.arg(incarnation),FALSE,sqlc.arg(created_at),sqlc.arg(created_at)
FROM snapshots s JOIN instance_identity i ON i.id=1
WHERE s.id=sqlc.arg(snapshot_id) AND s.org_id=sqlc.arg(org_id) AND s.project_id=sqlc.arg(project_id) AND s.environment_id=sqlc.arg(environment_id) AND i.identity=sqlc.arg(owner_instance_id) AND s.payload_present;
-- hikyo:instance-scoped
-- name: GetSelfConfigJob :one
SELECT * FROM self_config_jobs WHERE id=sqlc.arg(id);
-- hikyo:instance-scoped
-- name: GetSelfConfigJobByKey :one
SELECT * FROM self_config_jobs WHERE idempotency_key=sqlc.arg(idempotency_key);
-- hikyo:instance-scoped
-- name: ListSelfConfigJobs :many
SELECT * FROM self_config_jobs ORDER BY created_at DESC,id DESC LIMIT 100;
-- hikyo:instance-scoped
-- name: CountSelfConfigOpenJobs :one
SELECT COUNT(*) FROM self_config_jobs WHERE status IN ('preparing','applying','partial');
-- hikyo:instance-scoped
-- name: InsertSelfConfigJob :execrows
INSERT INTO self_config_jobs(id,idempotency_key,confirm_restored_credentials,principal_id,snapshot_id,revision,schema_version,expected_generation,generation,status,created_at,updated_at)
SELECT sqlc.arg(id),sqlc.arg(idempotency_key),sqlc.arg(confirm_restored_credentials),sqlc.arg(principal_id),s.id,s.revision,sqlc.arg(schema_version),b.generation,b.generation+1,'preparing',sqlc.arg(created_at),sqlc.arg(created_at)
FROM self_config_binding b JOIN snapshots s ON s.org_id=b.org_id AND s.project_id=b.project_id AND s.environment_id=b.environment_id
WHERE b.id=1 AND s.id=sqlc.arg(snapshot_id) AND s.revision=sqlc.arg(revision) AND s.payload_present AND b.generation=sqlc.arg(expected_generation) AND b.schema_version=sqlc.arg(schema_version);
-- hikyo:instance-scoped
-- name: UpdateSelfConfigJob :execrows
UPDATE self_config_jobs SET status=sqlc.arg(status),error_code=sqlc.arg(error_code),updated_at=sqlc.arg(updated_at) WHERE id=sqlc.arg(id) AND status=sqlc.arg(previous_status);
-- hikyo:instance-scoped
-- name: CommitSelfConfigTarget :execrows
UPDATE self_config_binding SET desired_snapshot_id=sqlc.arg(snapshot_id),desired_revision=sqlc.arg(revision),generation=generation+1,suspended=FALSE,updated_at=sqlc.arg(updated_at)
WHERE id=1 AND generation=sqlc.arg(expected_generation);
-- hikyo:instance-scoped
-- name: SetSelfConfigPrevious :exec
UPDATE self_config_binding SET previous_snapshot_id=sqlc.arg(snapshot_id) WHERE id=1;
-- hikyo:instance-scoped
-- name: FenceSelfConfigRestored :execrows
UPDATE self_config_binding SET incarnation=sqlc.arg(incarnation),suspended=TRUE,updated_at=sqlc.arg(updated_at) WHERE id=1 AND incarnation<>sqlc.arg(incarnation);
-- hikyo:instance-scoped
-- name: ListSelfConfigNodes :many
SELECT * FROM self_config_nodes ORDER BY node_id;
-- hikyo:instance-scoped
-- name: PutSelfConfigNode :exec
INSERT INTO self_config_nodes(node_id,job_id,schema_version,prepared,active_generation,active_revision,incarnation,error_code,updated_at)
VALUES(sqlc.arg(node_id),sqlc.arg(job_id),sqlc.arg(schema_version),sqlc.arg(prepared),sqlc.arg(active_generation),sqlc.arg(active_revision),sqlc.arg(incarnation),sqlc.arg(error_code),sqlc.arg(updated_at))
ON CONFLICT(node_id) DO UPDATE SET job_id=excluded.job_id,schema_version=excluded.schema_version,prepared=excluded.prepared,active_generation=excluded.active_generation,active_revision=excluded.active_revision,incarnation=excluded.incarnation,error_code=excluded.error_code,updated_at=excluded.updated_at;
-- hikyo:instance-scoped
-- name: DeleteSelfConfigNodes :exec
DELETE FROM self_config_nodes;
-- hikyo:instance-scoped
-- name: ListSelfConfigRetained :many
SELECT snapshot_id FROM self_config_retention ORDER BY slot;
-- hikyo:instance-scoped
-- name: SetSelfConfigRetention :exec
INSERT INTO self_config_retention(slot,snapshot_id) VALUES(sqlc.arg(slot),sqlc.arg(snapshot_id)) ON CONFLICT(slot) DO UPDATE SET snapshot_id=excluded.snapshot_id;
-- hikyo:instance-scoped
-- name: DeleteSelfConfigRetention :exec
DELETE FROM self_config_retention WHERE slot=sqlc.arg(slot);
-- hikyo:instance-scoped
-- name: GetSelfConfigRetentionSlot :one
SELECT snapshot_id FROM self_config_retention WHERE slot=sqlc.arg(slot);

-- hikyo:instance-scoped
-- name: ListSelfConfigParticipants :many
SELECT node_id FROM ha_nodes WHERE heartbeat_at >= sqlc.arg(since_at) ORDER BY node_id;
-- hikyo:instance-scoped
-- name: CountRecentSelfConfigJobs :one
SELECT COUNT(*) FROM self_config_jobs WHERE created_at >= sqlc.arg(since_at);

-- hikyo:instance-scoped
-- name: LockSelfConfigMembership :exec
SELECT pg_advisory_xact_lock(1464159830,87);
-- hikyo:instance-scoped
-- name: CountSelfConfigSeedDisagreement :one
SELECT COUNT(*) FROM ha_nodes n LEFT JOIN self_config_seed_attestations s ON s.node_id=n.node_id
WHERE n.heartbeat_at >= sqlc.arg(since_at) AND (s.node_id IS NULL OR s.heartbeat_at < sqlc.arg(since_at) OR s.schema_version <> sqlc.arg(schema_version) OR s.fingerprint <> sqlc.arg(fingerprint));

-- hikyo:instance-scoped
-- name: RecoverSelfConfigTarget :execrows
UPDATE self_config_binding SET desired_snapshot_id=sqlc.arg(snapshot_id),desired_revision=sqlc.arg(revision),generation=generation+1,updated_at=sqlc.arg(updated_at)
WHERE id=1 AND generation=sqlc.arg(expected_generation)
AND EXISTS(SELECT 1 FROM snapshots s WHERE s.id=sqlc.arg(snapshot_id) AND s.org_id=self_config_binding.org_id AND s.project_id=self_config_binding.project_id AND s.environment_id=self_config_binding.environment_id AND s.revision=sqlc.arg(revision) AND s.payload_present);

-- hikyo:instance-scoped
-- name: ListSelfConfigSeedInputs :many
SELECT i.*, s.schema_version, s.fingerprint AS owner_fingerprint, s.heartbeat_at
FROM self_config_seed_inputs i JOIN self_config_seed_attestations s ON s.node_id=i.node_id
ORDER BY i.node_id;

-- hikyo:instance-scoped
-- name: PutSelfConfigSeedAttestation :exec
INSERT INTO self_config_seed_attestations(node_id,schema_version,fingerprint,heartbeat_at)
VALUES(sqlc.arg(node_id),sqlc.arg(schema_version),sqlc.arg(fingerprint),sqlc.arg(heartbeat_at))
ON CONFLICT(node_id) DO UPDATE SET schema_version=excluded.schema_version,fingerprint=excluded.fingerprint,heartbeat_at=excluded.heartbeat_at;

-- hikyo:instance-scoped
-- name: PutSelfConfigSeedInput :exec
INSERT INTO self_config_seed_inputs(node_id,owner_instance_id,incarnation,fingerprint,ciphertext,dek_version)
VALUES(sqlc.arg(node_id),sqlc.arg(owner_instance_id),sqlc.arg(incarnation),sqlc.arg(fingerprint),sqlc.arg(ciphertext),sqlc.arg(dek_version))
ON CONFLICT(node_id) DO UPDATE SET owner_instance_id=excluded.owner_instance_id,incarnation=excluded.incarnation,fingerprint=excluded.fingerprint,ciphertext=excluded.ciphertext,dek_version=excluded.dek_version,row_version=self_config_seed_inputs.row_version+1;

-- hikyo:instance-scoped
-- name: ClearSelfConfigSeedInputs :exec
DELETE FROM self_config_seed_inputs;


-- hikyo:instance-scoped
-- name: GetSelfConfigRollout :one
SELECT * FROM self_config_rollouts WHERE job_id = sqlc.arg(job_id);

-- hikyo:instance-scoped
-- name: NextSelfConfigRolloutSequence :one
INSERT INTO self_config_rollout_sequences (enrollment_id, sequence) VALUES (sqlc.arg(enrollment_id), 1)
ON CONFLICT (enrollment_id) DO UPDATE SET sequence = self_config_rollout_sequences.sequence + 1
RETURNING sequence;

-- hikyo:instance-scoped
-- name: InsertSelfConfigRollout :execrows
INSERT INTO self_config_rollouts (job_id,enrollment_id,incarnation,plan_digest,command_json,response_json,external_phase,sequence,row_version)
VALUES (sqlc.arg(job_id),sqlc.arg(enrollment_id),sqlc.arg(incarnation),sqlc.arg(plan_digest),sqlc.arg(command_json),sqlc.arg(response_json),sqlc.arg(external_phase),sqlc.arg(sequence),1);

-- hikyo:instance-scoped
-- name: UpdateSelfConfigRollout :execrows
UPDATE self_config_rollouts SET plan_digest=sqlc.arg(plan_digest),command_json=sqlc.arg(command_json),response_json=sqlc.arg(response_json),external_phase=sqlc.arg(external_phase),sequence=sqlc.arg(sequence),row_version=row_version+1
WHERE job_id=sqlc.arg(job_id) AND enrollment_id=sqlc.arg(enrollment_id) AND incarnation=sqlc.arg(incarnation) AND row_version=sqlc.arg(expected_version);

-- hikyo:instance-scoped
-- name: CountSelfConfigCompletedGeneration :one
SELECT COUNT(*) FROM self_config_jobs WHERE generation=sqlc.arg(generation) AND status='applied';

-- hikyo:instance-scoped
-- name: GetSelfConfigPreviousRevision :one
SELECT s.revision FROM self_config_binding b JOIN snapshots s ON s.id=b.previous_snapshot_id AND s.org_id=b.org_id AND s.project_id=b.project_id AND s.environment_id=b.environment_id WHERE b.id=1;

-- hikyo:instance-scoped
-- name: GetSelfConfigClock :one
SELECT to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')::text AS observed_at;

-- Preserve both roots until the committed deployment can no longer restore.
-- The caller holds LockSelfConfigBinding before consulting this count.
-- hikyo:instance-scoped
-- name: CountSelfConfigRootFinalizationBlockers :one
SELECT COUNT(*) FROM self_config_binding b
JOIN self_config_jobs j ON j.generation=b.generation
JOIN self_config_rollouts r ON r.job_id=j.id AND r.incarnation=b.incarnation
WHERE b.id=1 AND j.status IN ('applying','partial','applied','superseded')
AND r.external_phase<>'applied';
