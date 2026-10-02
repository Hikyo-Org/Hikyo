-- name: AdapterOriginCurrentRoute :one
SELECT a.provider,a.origin,t.destination_kind,t.repository_id,t.destination_id,t.destination_scope
FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id
WHERE t.id=sqlc.arg(target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.environment_id=sqlc.arg(chain_env);

-- hikyo:reason Authorized adapter custody checks read only held origin metadata within the exact provider and resolved destination, repository, scope, surface and normalized name, including the current target's historical claim. Canonical-origin comparison stays in the private store owner; paged candidates do not expose tenant identifiers or values and prevent legacy aliases from bypassing the global held-custody constraint.
-- hikyo:instance-scoped
-- name: AdapterOriginHeldCandidates :many
SELECT l.id,l.target_id,l.provider_origin
FROM adapter_ledger l
JOIN adapter_targets t ON t.id=l.target_id AND t.org_id=l.org_id AND t.project_id=l.project_id AND t.environment_id=l.environment_id
JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id
WHERE a.provider=sqlc.arg(provider)
AND (l.destination_kind=sqlc.arg(destination_kind) OR (a.provider='aws-secrets-manager' AND l.destination_kind IN ('json-object','per-key') AND (sqlc.arg(destination_kind)='json-object' OR sqlc.arg(destination_kind)='per-key')))
AND l.repository_id=sqlc.arg(repository_id) AND l.destination_id=sqlc.arg(destination_id)
AND l.destination_scope=sqlc.arg(destination_scope) AND l.surface=sqlc.arg(surface) AND l.normalized_name=sqlc.arg(normalized_name)
AND l.state<>'released' AND l.id>sqlc.arg(cursor)
AND (l.target_id=sqlc.arg(target_id) OR
    l.provider_origin='' OR
    a.provider='aws-secrets-manager' OR
    length(CAST(substr(l.provider_origin,9,instr(substr(l.provider_origin,9)||'/', '/')-1) AS BLOB))<>length(substr(l.provider_origin,9,instr(substr(l.provider_origin,9)||'/', '/')-1)) OR
    (CAST(sqlc.arg(ipv6) AS INTEGER)=1 AND LOWER(l.provider_origin) LIKE 'https://[%') OR
    (
        (LOWER(l.provider_origin)=sqlc.arg(authority) OR like(sqlc.arg(authority_root),LOWER(l.provider_origin),'!')=1
        OR like(sqlc.arg(authority_port),LOWER(l.provider_origin),'!')=1
        OR LOWER(l.provider_origin)=sqlc.arg(authority_dot) OR like(sqlc.arg(authority_dot_root),LOWER(l.provider_origin),'!')=1
        OR like(sqlc.arg(authority_dot_port),LOWER(l.provider_origin),'!')=1)))
ORDER BY l.id LIMIT 256;
