-- hikyo:reason Closed PKIRuntime sweep authority selects globally overdue issuing rows; PostgreSQL retains FOR UPDATE OF c SKIP LOCKED and writes require state CAS.
-- hikyo:instance-scoped
-- name: RuntimePKIStaleIssuing :many
SELECT c.id,c.org_id,c.project_id,c.environment_id,c.serial,c.principal_id,i.name AS issuer_name,COALESCE(c.renewed_from,'') AS renewed_from FROM pki_certificates c JOIN pki_issuers i ON i.id=c.issuer_id WHERE c.state='issuing' AND c.issuing_deadline<sqlc.arg(now) ORDER BY c.issuing_deadline LIMIT CAST(sqlc.arg(page_limit) AS BIGINT) FOR UPDATE OF c SKIP LOCKED;

-- hikyo:reason Closed PKIRuntime sweep authority selects globally expired issued/renewed leaves; PostgreSQL retains FOR UPDATE OF c SKIP LOCKED and writes require state CAS.
-- hikyo:instance-scoped
-- name: RuntimePKIExpired :many
SELECT c.id,c.org_id,c.project_id,c.environment_id,c.serial,c.principal_id,i.name AS issuer_name,COALESCE(c.renewed_from,'') AS renewed_from FROM pki_certificates c JOIN pki_issuers i ON i.id=c.issuer_id WHERE c.state IN ('issued','renewed') AND c.not_after<=sqlc.arg(now) ORDER BY c.not_after LIMIT CAST(sqlc.arg(page_limit) AS BIGINT) FOR UPDATE OF c SKIP LOCKED;

-- hikyo:reason Closed PKIRuntime stale-issuing sweep writes only a selected certificate id/org still issuing; fail-closed unknown transition and audit share the transaction.
-- hikyo:instance-scoped
-- name: RuntimePKIMarkUnknown :execrows
UPDATE pki_certificates SET state='unknown',row_version=row_version+1,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(org_id) AND state='issuing';

-- hikyo:reason Closed PKIRuntime expiry sweep writes only a selected certificate id/org still issued/renewed; transition and tenant audit share the transaction.
-- hikyo:instance-scoped
-- name: RuntimePKIMarkExpired :execrows
UPDATE pki_certificates SET state='expired',row_version=row_version+1,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(org_id) AND state IN ('issued','renewed');

-- hikyo:reason Closed PKIRuntime releases only the matching predecessor id/org/successor claim after its issuing successor becomes unknown in the same transaction.
-- hikyo:instance-scoped
-- name: RuntimePKIReleaseRenewal :exec
UPDATE pki_certificates SET renewed_by=NULL,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(org_id) AND renewed_by=sqlc.arg(successor_id);

-- hikyo:reason Closed PKIRuntime appends the original system transition outcome using the selected certificate tenant chain, principal and correlation id in the same state-CAS transaction.
-- hikyo:instance-scoped
-- name: RuntimePKITransitionAudit :exec
INSERT INTO audit_tenant_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,scope_class,org_id,project_id,env_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES (sqlc.arg(id),'pki.certificate_transition_outcome',1,sqlc.arg(at),false,sqlc.arg(at),NULL,'system',sqlc.arg(authority_id),'env',sqlc.arg(org_id),sqlc.arg(project_id),sqlc.arg(env_id),'pki-certificate',sqlc.arg(certificate_id),sqlc.arg(outcome),sqlc.arg(certificate_id),'system',sqlc.arg(payload));

-- hikyo:reason Closed PKIRuntime selects global active/retiring CAs with signing keys whose CRLs are missing, half-life due or behind committed revocation sequences.
-- hikyo:instance-scoped
-- name: RuntimePKIDueCRLs :many
SELECT id,name,version,certificate_der,encrypted_private_key,dek_version,crl_number,revocation_seq FROM pki_issuers WHERE state IN ('active','retiring') AND encrypted_private_key IS NOT NULL AND dek_version IS NOT NULL AND certificate_der IS NOT NULL AND (crl_der IS NULL OR crl_next_update<=sqlc.arg(half_life) OR revocation_seq>crl_revocation_seq) ORDER BY id;

-- hikyo:reason Closed PKIRuntime appends the global CA publication audit only after successful prior-CRL-number CAS in the same transaction.
-- hikyo:instance-scoped
-- name: RuntimePKIPublishedAudit :exec
INSERT INTO audit_instance_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES (sqlc.arg(id),'pki.crl_published',1,sqlc.arg(at),false,sqlc.arg(at),NULL,'system',NULL,'pki-issuer',sqlc.arg(issuer_id),'success',NULL,'system',sqlc.arg(payload));

-- hikyo:reason Closed PKIRuntime scrape authority returns only the label-free global count of unexpired issued/renewed certificates.
-- hikyo:instance-scoped
-- name: RuntimePKICountLive :one
SELECT COUNT(*) FROM pki_certificates WHERE state IN ('issued','renewed') AND not_after>sqlc.arg(now);

-- hikyo:reason Closed PKIRuntime scrape authority returns only the label-free global count of unexpired unknown certificates.
-- hikyo:instance-scoped
-- name: RuntimePKICountUnknown :one
SELECT COUNT(*) FROM pki_certificates WHERE state='unknown' AND not_after>sqlc.arg(now);

-- hikyo:reason Closed PKIRuntime scrape authority returns only the label-free global count of held pending/active/retiring CAs.
-- hikyo:instance-scoped
-- name: RuntimePKICountHeld :one
SELECT COUNT(*) FROM pki_issuers WHERE restore_hold=1 AND state IN ('pending','active','retiring');
