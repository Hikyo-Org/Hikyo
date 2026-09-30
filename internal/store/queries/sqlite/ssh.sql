-- name: SSHCreateCA :execrows
INSERT INTO ssh_cas (id,org_id,project_id,environment_id,name,state,authority_principal_id,created_at) VALUES (sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(name), 'active', sqlc.arg(authority_principal_id), sqlc.arg(at));

-- name: SSHInsertCAKey :execrows
INSERT INTO ssh_ca_keys (id,org_id,project_id,environment_id,ca_id,algorithm,public_key,fingerprint,origin,private_key_ciphertext,state,created_at)
SELECT sqlc.arg(id),ca.org_id,ca.project_id,ca.environment_id,ca.id,sqlc.arg(algorithm),sqlc.arg(public_key),sqlc.arg(fingerprint),sqlc.arg(origin),sqlc.arg(ciphertext), 'active', sqlc.arg(at) FROM ssh_cas ca WHERE ca.id=sqlc.arg(ca_id) AND ca.org_id=sqlc.arg(chain_org) AND ca.project_id=sqlc.arg(chain_project) AND ca.environment_id=sqlc.arg(chain_env) AND ca.state='active';

-- name: SSHRetireActiveCAKey :execrows
UPDATE ssh_ca_keys SET state='retiring',private_key_ciphertext=NULL,retiring_at=sqlc.arg(at),retire_after=sqlc.arg(retire_after) WHERE id=sqlc.arg(key_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='active';

-- name: SSHRetireCAKey :execrows
UPDATE ssh_ca_keys SET state='retired',retired_at=sqlc.arg(at) WHERE id=sqlc.arg(key_id) AND ca_id=sqlc.arg(ca_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='retiring';

-- name: SSHDeleteCA :execrows
UPDATE ssh_cas SET state='tombstoned' WHERE id=sqlc.arg(ca_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='active';

-- name: SSHRetireAllCAKeys :execrows
UPDATE ssh_ca_keys SET state='retired',private_key_ciphertext=NULL,retired_at=sqlc.arg(at) WHERE ca_id=sqlc.arg(ca_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'retired';

-- name: SSHDeleteProfile :execrows
UPDATE ssh_profiles SET state='tombstoned',updated_at=sqlc.arg(at) WHERE id=sqlc.arg(profile_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'tombstoned';

-- name: SSHRevokeCertificate :execrows
UPDATE ssh_certificates SET state='revoked',revoked_at=sqlc.arg(at),revocation_reason=sqlc.arg(reason) WHERE id=sqlc.arg(certificate_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='issued';

-- name: SSHReencryptCAKey :execrows
UPDATE ssh_ca_keys SET private_key_ciphertext=sqlc.arg(new_ciphertext) WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND id=sqlc.arg(id) AND private_key_ciphertext=sqlc.arg(old_ciphertext);

-- name: SSHCreateProfile :execrows
INSERT INTO ssh_profiles (id,org_id,project_id,environment_id,ca_id,name,principals,force_command,source_addresses,extensions,key_algorithms,default_ttl_seconds,max_ttl_seconds,state,created_at,updated_at)
SELECT sqlc.arg(id),ca.org_id,ca.project_id,ca.environment_id,ca.id,sqlc.arg(name),sqlc.arg(principals),sqlc.arg(force_command),sqlc.arg(source_addresses),sqlc.arg(extensions),sqlc.arg(key_algorithms),sqlc.arg(default_ttl_seconds),sqlc.arg(max_ttl_seconds),sqlc.arg(state),sqlc.arg(at),sqlc.arg(at) FROM ssh_cas ca WHERE ca.id=sqlc.arg(ca_id) AND ca.org_id=sqlc.arg(chain_org) AND ca.project_id=sqlc.arg(chain_project) AND ca.environment_id=sqlc.arg(chain_env) AND ca.state='active';

-- name: SSHUpdateProfile :execrows
UPDATE ssh_profiles SET name=sqlc.arg(name),principals=sqlc.arg(principals),force_command=sqlc.arg(force_command),source_addresses=sqlc.arg(source_addresses),extensions=sqlc.arg(extensions),key_algorithms=sqlc.arg(key_algorithms),default_ttl_seconds=sqlc.arg(default_ttl_seconds),max_ttl_seconds=sqlc.arg(max_ttl_seconds),state=sqlc.arg(state),updated_at=sqlc.arg(at)
WHERE id=sqlc.arg(id) AND ca_id=sqlc.arg(ca_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'tombstoned';

-- name: SSHDeleteRequesters :execrows
DELETE FROM ssh_profile_requesters WHERE profile_id=sqlc.arg(profile_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: SSHInsertRequester :execrows
INSERT INTO ssh_profile_requesters (org_id,project_id,environment_id,profile_id,principal_id,created_at) VALUES (sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(profile_id),sqlc.arg(principal),sqlc.arg(at));

-- name: SSHInsertCertificate :execrows
INSERT INTO ssh_certificates (id,org_id,project_id,environment_id,ca_id,ca_key_id,profile_id,serial,key_id,principals,public_key_fingerprint,key_algorithm,key_origin,valid_after,valid_before,requester_principal_id,requester_class,state,created_at)
SELECT sqlc.arg(id),k.org_id,k.project_id,k.environment_id,k.ca_id,k.id,pr.id,sqlc.arg(serial),sqlc.arg(key_id),sqlc.arg(principals),sqlc.arg(public_key_fingerprint),sqlc.arg(key_algorithm),sqlc.arg(key_origin),sqlc.arg(valid_after),sqlc.arg(valid_before),sqlc.arg(requester_principal_id),sqlc.arg(requester_class), 'issued', sqlc.arg(at)
FROM ssh_ca_keys k JOIN ssh_profiles pr ON pr.ca_id=k.ca_id AND pr.org_id=k.org_id AND pr.project_id=k.project_id AND pr.environment_id=k.environment_id
WHERE k.id=sqlc.arg(ca_key_id) AND k.ca_id=sqlc.arg(ca_id) AND k.state='active' AND pr.id=sqlc.arg(profile_id) AND pr.state='enabled' AND k.org_id=sqlc.arg(chain_org) AND k.project_id=sqlc.arg(chain_project) AND k.environment_id=sqlc.arg(chain_env);

-- name: SSHCAKeys :many
SELECT id,ca_id,algorithm,public_key,fingerprint,origin,state,created_at,retiring_at,retire_after,retired_at FROM ssh_ca_keys WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) ORDER BY created_at DESC, id DESC;

-- name: SSHGetCA :one
SELECT id,name,state,authority_principal_id,created_at FROM ssh_cas WHERE id=sqlc.arg(ca_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='active';

-- name: SSHListCAs :many
SELECT id,name,state,authority_principal_id,created_at FROM ssh_cas WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='active' ORDER BY name, id;

-- name: SSHActiveCAKey :one
SELECT k.id,k.algorithm,k.public_key,k.private_key_ciphertext FROM ssh_ca_keys k JOIN ssh_cas c ON c.id=k.ca_id AND c.org_id=k.org_id AND c.project_id=k.project_id AND c.environment_id=k.environment_id
WHERE k.ca_id=sqlc.arg(ca_id) AND k.org_id=sqlc.arg(chain_org) AND k.project_id=sqlc.arg(chain_project) AND k.environment_id=sqlc.arg(chain_env) AND k.state='active' AND c.state='active';

-- name: SSHActiveCAKeyID :one
SELECT id FROM ssh_ca_keys WHERE ca_id=sqlc.arg(ca_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='active';

-- name: SSHCAKeyExists :one
SELECT COUNT(*) FROM ssh_ca_keys WHERE id=sqlc.arg(key_id) AND ca_id=sqlc.arg(ca_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: SSHCountLiveProfiles :one
SELECT COUNT(*) FROM ssh_profiles WHERE ca_id=sqlc.arg(ca_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'tombstoned';

-- name: SSHActiveKeyLastExpiry :one
SELECT c.valid_before AS last_expiry FROM ssh_certificates c
JOIN ssh_ca_keys k ON k.id=c.ca_key_id AND k.org_id=c.org_id AND k.project_id=c.project_id AND k.environment_id=c.environment_id
WHERE c.ca_id=sqlc.arg(ca_id) AND c.org_id=sqlc.arg(chain_org) AND c.project_id=sqlc.arg(chain_project) AND c.environment_id=sqlc.arg(chain_env) AND k.state='active' AND c.state='issued' AND c.valid_before>sqlc.arg(now) ORDER BY c.valid_before DESC LIMIT 1;

-- name: SSHRequesters :many
SELECT profile_id,principal_id FROM ssh_profile_requesters WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) ORDER BY profile_id, principal_id;

-- name: SSHGetProfile :one
SELECT id,ca_id,name,principals,force_command,source_addresses,extensions,key_algorithms,default_ttl_seconds,max_ttl_seconds,state,created_at,updated_at FROM ssh_profiles WHERE id=sqlc.arg(profile_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'tombstoned';

-- name: SSHListProfiles :many
SELECT id,ca_id,name,principals,force_command,source_addresses,extensions,key_algorithms,default_ttl_seconds,max_ttl_seconds,state,created_at,updated_at FROM ssh_profiles WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'tombstoned' ORDER BY name, id;

-- name: SSHIsRequester :one
SELECT COUNT(*) FROM ssh_profile_requesters WHERE profile_id=sqlc.arg(profile_id) AND principal_id=sqlc.arg(principal_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: SSHGetCertificate :one
SELECT c.id,c.ca_id,c.ca_key_id,c.profile_id,c.serial,c.key_id,c.principals,c.public_key_fingerprint,c.key_algorithm,c.key_origin,c.valid_after,c.valid_before,c.requester_principal_id,c.requester_class,c.state,c.revoked_at,c.revocation_reason,c.created_at,k.state AS ca_key_state,k.retire_after AS ca_key_retire_after,a.state AS ca_state FROM ssh_certificates c
JOIN ssh_ca_keys k ON k.id=c.ca_key_id AND k.org_id=c.org_id AND k.project_id=c.project_id AND k.environment_id=c.environment_id
JOIN ssh_cas a ON a.id=c.ca_id AND a.org_id=c.org_id AND a.project_id=c.project_id AND a.environment_id=c.environment_id WHERE c.id=sqlc.arg(certificate_id) AND c.org_id=sqlc.arg(chain_org) AND c.project_id=sqlc.arg(chain_project) AND c.environment_id=sqlc.arg(chain_env);

-- name: SSHListCertificates :many
SELECT c.id,c.ca_id,c.ca_key_id,c.profile_id,c.serial,c.key_id,c.principals,c.public_key_fingerprint,c.key_algorithm,c.key_origin,c.valid_after,c.valid_before,c.requester_principal_id,c.requester_class,c.state,c.revoked_at,c.revocation_reason,c.created_at,k.state AS ca_key_state,k.retire_after AS ca_key_retire_after,a.state AS ca_state FROM ssh_certificates c
JOIN ssh_ca_keys k ON k.id=c.ca_key_id AND k.org_id=c.org_id AND k.project_id=c.project_id AND k.environment_id=c.environment_id
JOIN ssh_cas a ON a.id=c.ca_id AND a.org_id=c.org_id AND a.project_id=c.project_id AND a.environment_id=c.environment_id WHERE c.org_id=sqlc.arg(chain_org) AND c.project_id=sqlc.arg(chain_project) AND c.environment_id=sqlc.arg(chain_env) ORDER BY c.created_at DESC, c.id DESC LIMIT sqlc.arg(page_limit);

-- name: SSHProfileRevocationCandidates :many
SELECT id,serial,requester_principal_id FROM ssh_certificates WHERE profile_id=sqlc.arg(profile_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='issued' AND valid_before>sqlc.arg(at) ORDER BY id;

-- name: SSHRevokedSerials :many
SELECT k.public_key,c.serial FROM ssh_certificates c
JOIN ssh_ca_keys k ON k.id=c.ca_key_id AND k.org_id=c.org_id AND k.project_id=c.project_id AND k.environment_id=c.environment_id
WHERE c.ca_id=sqlc.arg(ca_id) AND c.org_id=sqlc.arg(chain_org) AND c.project_id=sqlc.arg(chain_project) AND c.environment_id=sqlc.arg(chain_env) AND c.state='revoked' AND c.valid_before>sqlc.arg(now)
AND (k.state='active' OR (k.state='retiring' AND k.retire_after>sqlc.arg(now)))
ORDER BY k.id, c.serial LIMIT sqlc.arg(page_limit);

-- name: SSHListKeysForReencrypt :many
SELECT id, private_key_ciphertext FROM ssh_ca_keys WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND id>sqlc.arg(cursor) AND private_key_ciphertext IS NOT NULL ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: SSHCountLiveCAs :one
SELECT COUNT(*) FROM ssh_cas WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='active';

-- name: SSHPurgeCertificates :exec
DELETE FROM ssh_certificates WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: SSHPurgeRequesters :exec
DELETE FROM ssh_profile_requesters WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: SSHPurgeProfiles :exec
DELETE FROM ssh_profiles WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: SSHPurgeCAKeys :exec
DELETE FROM ssh_ca_keys WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: SSHPurgeCAs :exec
DELETE FROM ssh_cas WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);
