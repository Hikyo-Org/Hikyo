-- +goose Up
-- Blank GitLab adoption claims did not name the real environment scope.
-- Do not guess ownership in the configured scope or modify provider data.
CREATE TEMP TABLE hikyo_gitlab_scope_repair_targets (
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    PRIMARY KEY (org_id, project_id, environment_id, target_id)
);
INSERT INTO hikyo_gitlab_scope_repair_targets
SELECT DISTINCT t.org_id,t.project_id,t.environment_id,t.id
FROM adapter_targets t
JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id
JOIN adapter_ledger l ON l.target_id=t.id AND l.org_id=t.org_id AND l.project_id=t.project_id AND l.environment_id=t.environment_id
WHERE a.provider='gitlab' AND t.destination_scope<>'' AND l.destination_scope='' AND l.state<>'released';

-- A repaired moving member stops its entire unfinished route transition.
-- Siblings retain provider custody; only this move's pending jobs and exact
-- active pointers are retired before operator CancelMove -> ResumeTarget.
CREATE TEMP TABLE hikyo_gitlab_scope_repair_moves (
    org_id TEXT NOT NULL, project_id TEXT NOT NULL,
    adapter_id TEXT NOT NULL, move_id TEXT NOT NULL,
    PRIMARY KEY (org_id, project_id, move_id)
);
INSERT INTO hikyo_gitlab_scope_repair_moves
SELECT DISTINCT m.org_id,m.project_id,m.adapter_id,m.id
FROM adapter_route_moves m
JOIN adapter_route_move_targets mt ON mt.move_id=m.id AND mt.org_id=m.org_id AND mt.project_id=m.project_id
JOIN adapter_targets t ON t.id=mt.target_id AND t.org_id=mt.org_id AND t.project_id=mt.project_id AND t.environment_id=mt.environment_id AND t.adapter_id=m.adapter_id
JOIN hikyo_gitlab_scope_repair_targets r ON r.target_id=t.id AND r.org_id=t.org_id AND r.project_id=t.project_id AND r.environment_id=t.environment_id
WHERE t.state='moving' AND m.state IN ('scrubbing','activating','attention_required');

CREATE TEMP TABLE hikyo_gitlab_scope_repair_jobs (
    org_id TEXT NOT NULL, project_id TEXT NOT NULL, environment_id TEXT NOT NULL,
    target_id TEXT NOT NULL, job_id TEXT NOT NULL,
    PRIMARY KEY (org_id, project_id, environment_id, target_id, job_id)
);
INSERT INTO hikyo_gitlab_scope_repair_jobs
SELECT DISTINCT j.org_id,j.project_id,j.environment_id,j.target_id,j.id
FROM hikyo_gitlab_scope_repair_moves r
JOIN adapter_route_move_targets mt ON mt.move_id=r.move_id AND mt.org_id=r.org_id AND mt.project_id=r.project_id
JOIN adapter_targets t ON t.id=mt.target_id AND t.org_id=mt.org_id AND t.project_id=mt.project_id AND t.environment_id=mt.environment_id AND t.adapter_id=r.adapter_id
JOIN adapter_outbox j ON j.route_move_id=r.move_id AND j.target_id=t.id AND j.org_id=t.org_id AND j.project_id=t.project_id AND j.environment_id=t.environment_id
WHERE j.state IN ('queued','running');

UPDATE adapter_outbox AS j
SET state='superseded',finished_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),lease_owner=NULL,lease_expires_at=NULL
WHERE j.state IN ('queued','running') AND EXISTS (
    SELECT 1 FROM hikyo_gitlab_scope_repair_jobs r
    WHERE r.job_id=j.id AND r.target_id=j.target_id AND r.org_id=j.org_id AND r.project_id=j.project_id AND r.environment_id=j.environment_id
);
UPDATE adapter_targets AS t SET active_job_id=NULL
WHERE EXISTS (
    SELECT 1 FROM hikyo_gitlab_scope_repair_jobs r
    WHERE r.job_id=t.active_job_id AND r.target_id=t.id AND r.org_id=t.org_id AND r.project_id=t.project_id AND r.environment_id=t.environment_id
);
UPDATE adapter_route_moves AS m SET state='attention_required'
WHERE EXISTS (
    SELECT 1 FROM hikyo_gitlab_scope_repair_moves r
    WHERE r.move_id=m.id AND r.org_id=m.org_id AND r.project_id=m.project_id AND r.adapter_id=m.adapter_id
);
DROP TABLE hikyo_gitlab_scope_repair_jobs;
DROP TABLE hikyo_gitlab_scope_repair_moves;


UPDATE adapter_outbox AS j
SET state='superseded',finished_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),lease_owner=NULL,lease_expires_at=NULL
WHERE j.state IN ('queued','running') AND EXISTS (
    SELECT 1 FROM hikyo_gitlab_scope_repair_targets r
    WHERE r.target_id=j.target_id AND r.org_id=j.org_id AND r.project_id=j.project_id AND r.environment_id=j.environment_id
);

UPDATE adapter_targets AS t
SET generation=CASE WHEN generation<9223372036854775807 THEN generation+1 ELSE -1 END,
    paused_at=COALESCE(paused_at,strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    sync_status='failed',drift_attention=1,last_error_class='refused',
    warnings=json_insert(warnings,'$[#]','gitlab_scope_claims_released_operator_review'),
    active_job_id=NULL,provider_lease_job_id=NULL,provider_lease_effect_id=NULL,provider_lease_expires_at=NULL
WHERE EXISTS (
    SELECT 1 FROM hikyo_gitlab_scope_repair_targets r
    WHERE r.target_id=t.id AND r.org_id=t.org_id AND r.project_id=t.project_id AND r.environment_id=t.environment_id
);

UPDATE adapter_ledger AS l
SET state='released',updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE l.destination_scope='' AND l.state<>'released' AND EXISTS (
    SELECT 1 FROM hikyo_gitlab_scope_repair_targets r
    WHERE r.target_id=l.target_id AND r.org_id=l.org_id AND r.project_id=l.project_id AND r.environment_id=l.environment_id
);
DROP TABLE hikyo_gitlab_scope_repair_targets;

-- A historical terminal job from an older credential generation cannot
-- remain the active pointer. Preserve its outcome and every provider fence.
UPDATE adapter_targets AS t SET active_job_id=NULL
WHERE EXISTS (
    SELECT 1 FROM adapter_outbox j
    WHERE j.id=t.active_job_id AND j.target_id=t.id AND j.org_id=t.org_id
      AND j.project_id=t.project_id AND j.environment_id=t.environment_id
      AND j.generation<>t.generation AND j.state IN ('succeeded','failed','superseded')
);
