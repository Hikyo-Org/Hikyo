-- hikyo:reason SSHRuntime closed sweeper pages live certificate authority facts across tenants; each candidate is reauthorized before its immutable chain can be revoked.
-- hikyo:instance-scoped
-- name: RuntimeSSHListLiveCertificates :many
SELECT c.id,c.org_id,c.project_id,c.environment_id,c.profile_id,c.serial,c.requester_principal_id,c.requester_class,
CAST(CASE WHEN q.principal_id IS NULL THEN 0 ELSE 1 END AS INTEGER) AS requester_listed
FROM ssh_certificates c
LEFT JOIN ssh_profile_requesters q ON q.profile_id=c.profile_id AND q.principal_id=c.requester_principal_id AND q.org_id=c.org_id AND q.project_id=c.project_id AND q.environment_id=c.environment_id
WHERE c.state='issued' AND c.valid_before>sqlc.arg(now) AND c.id>sqlc.arg(after_id) ORDER BY c.id LIMIT sqlc.arg(page_limit);

-- name: RuntimeSSHRevokeForAuthority :execrows
UPDATE ssh_certificates SET state='revoked',revoked_at=sqlc.arg(at),revocation_reason='authority-withdrawn' WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='issued';

-- name: RuntimeSSHInsertAuthorityRevocationAudit :exec
INSERT INTO audit_tenant_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,scope_class,org_id,project_id,env_id,object_type,object_id,outcome,correlation_id,origin,payload)
VALUES (sqlc.arg(id),'ssh.certificate_revoked',1,sqlc.arg(at),0,sqlc.arg(at),NULL,'system',sqlc.arg(authority_id),'env',sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),'ssh-certificate',sqlc.arg(certificate_id),'success',sqlc.arg(certificate_id),'system',sqlc.arg(payload));

-- hikyo:reason SSHRuntime metrics scrape returns only the instance-wide count of issued unexpired certificates through the closed runtime boundary.
-- hikyo:instance-scoped
-- name: RuntimeSSHCountActive :one
SELECT COUNT(*) FROM ssh_certificates WHERE state='issued' AND valid_before>sqlc.arg(now);

-- hikyo:reason SSHRuntime metrics scrape returns only the instance-wide count of revoked unexpired certificates under still-trusted CA keys through the closed runtime boundary.
-- hikyo:instance-scoped
-- name: RuntimeSSHCountKRLEntries :one
SELECT COUNT(*) FROM ssh_certificates c JOIN ssh_ca_keys k ON k.id=c.ca_key_id
WHERE c.state='revoked' AND c.valid_before>sqlc.arg(now) AND (k.state='active' OR (k.state='retiring' AND k.retire_after>sqlc.arg(now)));
