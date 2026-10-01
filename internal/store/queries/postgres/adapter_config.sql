-- Scoped adapter route collisions and current-generation findings.

-- name: AdapterConfigConfiguredNames :many
SELECT (t.id)::text AS target_id,(t.name_prefix)::text AS prefix,(COALESCE(k.name,''))::text AS canonical_name FROM adapter_targets t
			JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id
			JOIN adapters candidate ON candidate.id=sqlc.arg(adapter_id) AND candidate.org_id=t.org_id AND candidate.project_id=t.project_id
			LEFT JOIN adapter_target_keys tk ON tk.target_id=t.id AND tk.org_id=t.org_id AND tk.project_id=t.project_id AND tk.environment_id=t.environment_id
			LEFT JOIN keys k ON k.id=tk.key_id AND k.org_id=tk.org_id AND k.project_id=tk.project_id
			WHERE t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.state='active' AND a.state='active'
			AND a.origin=candidate.origin AND t.destination_kind=sqlc.arg(destination_kind) AND t.destination_id=sqlc.arg(destination_id) AND t.destination_scope=sqlc.arg(destination_scope) AND t.id<>sqlc.arg(exclude_target_id)
			ORDER BY t.id,k.name;

-- name: AdapterConfigPendingNames :many
SELECT (c.target_id)::text AS target_id,(c.effective_name)::text AS effective_name FROM adapter_route_move_claims c JOIN adapters candidate ON candidate.id=sqlc.arg(adapter_id) AND candidate.org_id=c.org_id AND candidate.project_id=c.project_id WHERE c.org_id=sqlc.arg(chain_org) AND c.project_id=sqlc.arg(chain_project) AND c.provider_origin=candidate.origin AND c.destination_kind=sqlc.arg(destination_kind) AND c.destination_owner=sqlc.arg(destination_owner) AND c.destination_name=sqlc.arg(destination_name) AND c.destination_environment=sqlc.arg(destination_environment) AND c.destination_scope=sqlc.arg(destination_scope) AND c.target_id<>sqlc.arg(exclude_target_id) ORDER BY c.target_id,c.effective_name;

-- name: AdapterConfigAWSConfiguredNames :many
SELECT (t.id)::text AS target_id,(t.destination_kind)::text AS kind,(t.destination_name)::text AS name,(t.name_prefix)::text AS prefix,(COALESCE(k.name,''))::text AS key_name FROM adapter_targets t
		JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id
		JOIN adapters candidate ON candidate.id=sqlc.arg(adapter_id) AND candidate.org_id=t.org_id AND candidate.project_id=t.project_id
		LEFT JOIN adapter_target_keys tk ON tk.target_id=t.id AND tk.org_id=t.org_id AND tk.project_id=t.project_id AND tk.environment_id=t.environment_id
		LEFT JOIN keys k ON k.id=tk.key_id AND k.org_id=tk.org_id AND k.project_id=tk.project_id
		WHERE t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.state='active' AND a.state='active'
		AND a.origin=candidate.origin AND t.destination_kind IN ('json-object','per-key') AND t.destination_owner=sqlc.arg(destination_owner) AND t.id<>sqlc.arg(exclude_target_id)
		ORDER BY t.id,k.name;

-- name: AdapterConfigAWSPendingNames :many
SELECT (c.target_id)::text AS target_id,(c.effective_name)::text AS effective_name FROM adapter_route_move_claims c JOIN adapters candidate ON candidate.id=sqlc.arg(adapter_id) AND candidate.org_id=c.org_id AND candidate.project_id=c.project_id WHERE c.org_id=sqlc.arg(chain_org) AND c.project_id=sqlc.arg(chain_project) AND c.provider_origin=candidate.origin AND c.destination_kind IN ('json-object','per-key') AND c.destination_owner=sqlc.arg(destination_owner) AND c.target_id<>sqlc.arg(exclude_target_id) ORDER BY c.target_id,c.effective_name;

-- name: AdapterConfigFindings :many
SELECT (surface)::text AS surface,(effective_name)::text AS effective_name,(finding)::text AS finding FROM (
 SELECT e.surface,e.effective_name,e.finding,ROW_NUMBER() OVER (PARTITION BY e.surface,UPPER(e.effective_name) ORDER BY e.finished_at DESC,e.id DESC) AS ordinal
 FROM adapter_effects e JOIN adapter_outbox o ON o.id=e.job_id AND o.org_id=e.org_id AND o.project_id=e.project_id AND o.environment_id=e.environment_id
 WHERE e.target_id=sqlc.arg(target_id) AND e.org_id=sqlc.arg(chain_org) AND e.project_id=sqlc.arg(chain_project) AND e.environment_id=sqlc.arg(environment_id) AND o.generation=sqlc.arg(generation) AND e.outcome IS NOT NULL
 ) ranked WHERE ordinal=1 AND finding<>'' ORDER BY surface,effective_name;
