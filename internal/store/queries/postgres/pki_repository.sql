-- hikyo:reason StorePKIIssuersList verifies instance-config authority; CA issuer versions are instance-wide public metadata ordered by name/version.
-- hikyo:instance-scoped
-- name: PKIListIssuers :many
SELECT id,name,version,kind,origin,COALESCE(parent_id,'') AS parent_id,state,key_algorithm,key_fingerprint,CAST(CASE WHEN encrypted_private_key IS NULL THEN 0 ELSE 1 END AS INTEGER) AS key_present,certificate_der,csr_der,chain_pem,subject_cn,subject_org,not_before,not_after,crl_distribution_url,restore_hold,issued_count,crl_der,crl_number,revocation_seq,crl_this_update,crl_next_update,row_version,created_by,created_at,updated_at FROM pki_issuers ORDER BY name,version;

-- hikyo:reason StorePKIIssuersGet verifies instance-config, or StorePKIIssuersPublic permits the existing global public CA projection; this query excludes sealed keys.
-- hikyo:instance-scoped
-- name: PKIGetIssuer :one
SELECT id,name,version,kind,origin,COALESCE(parent_id,'') AS parent_id,state,key_algorithm,key_fingerprint,CAST(CASE WHEN encrypted_private_key IS NULL THEN 0 ELSE 1 END AS INTEGER) AS key_present,certificate_der,csr_der,chain_pem,subject_cn,subject_org,not_before,not_after,crl_distribution_url,restore_hold,issued_count,crl_der,crl_number,revocation_seq,crl_this_update,crl_next_update,row_version,created_by,created_at,updated_at FROM pki_issuers WHERE id=sqlc.arg(id);

-- hikyo:reason StorePKIIssuersActivePublic permits the existing instance-wide active CA projection for a verified environment operation; sealed keys are excluded.
-- hikyo:instance-scoped
-- name: PKIActiveIssuer :one
SELECT id,name,version,kind,origin,COALESCE(parent_id,'') AS parent_id,state,key_algorithm,key_fingerprint,CAST(CASE WHEN encrypted_private_key IS NULL THEN 0 ELSE 1 END AS INTEGER) AS key_present,certificate_der,csr_der,chain_pem,subject_cn,subject_org,not_before,not_after,crl_distribution_url,restore_hold,issued_count,crl_der,crl_number,revocation_seq,crl_this_update,crl_next_update,row_version,created_by,created_at,updated_at FROM pki_issuers WHERE name=sqlc.arg(name) AND state='active';

-- hikyo:reason StorePKIIssuersSigning verifies signing authority before reading the active global CA public row; issuer fencing remains required before issuance.
-- hikyo:instance-scoped
-- name: PKISigningIssuer :one
SELECT id,name,version,kind,origin,COALESCE(parent_id,'') AS parent_id,state,key_algorithm,key_fingerprint,CAST(CASE WHEN encrypted_private_key IS NULL THEN 0 ELSE 1 END AS INTEGER) AS key_present,certificate_der,csr_der,chain_pem,subject_cn,subject_org,not_before,not_after,crl_distribution_url,restore_hold,issued_count,crl_der,crl_number,revocation_seq,crl_this_update,crl_next_update,row_version,created_by,created_at,updated_at FROM pki_issuers WHERE id=sqlc.arg(id) AND state='active';

-- hikyo:reason StorePKIIssuersKey or StorePKIIssuersSigning verifies the closed in-process signer capability before reading sealed CA key material.
-- hikyo:instance-scoped
-- name: PKIIssuerKey :one
SELECT encrypted_private_key,dek_version FROM pki_issuers WHERE id=sqlc.arg(id) AND encrypted_private_key IS NOT NULL AND dek_version IS NOT NULL;

-- hikyo:reason StorePKIIssuersCreate verifies instance-config authority before creating an instance-wide CA key version.
-- hikyo:instance-scoped
-- name: PKICreateIssuer :exec
INSERT INTO pki_issuers (id,name,version,kind,origin,parent_id,state,key_algorithm,key_fingerprint,encrypted_private_key,dek_version,certificate_der,csr_der,chain_pem,subject_cn,subject_org,not_before,not_after,crl_distribution_url,created_by,created_at,updated_at) VALUES (sqlc.arg(id),sqlc.arg(name),sqlc.arg(version),sqlc.arg(kind),sqlc.arg(origin),sqlc.arg(parent_id),sqlc.arg(state),sqlc.arg(key_algorithm),sqlc.arg(key_fingerprint),sqlc.arg(encrypted_private_key),sqlc.arg(dek_version),sqlc.arg(certificate_der),sqlc.arg(csr_der),sqlc.arg(chain_pem),sqlc.arg(subject_cn),sqlc.arg(subject_org),sqlc.arg(not_before),sqlc.arg(not_after),sqlc.arg(crl_distribution_url),sqlc.arg(created_by),sqlc.arg(at),sqlc.arg(at));

-- hikyo:reason StorePKIIssuersInstall verifies instance-config authority; pending CA activation retains the original state and row-version CAS.
-- hikyo:instance-scoped
-- name: PKIInstallIssuer :execrows
UPDATE pki_issuers SET state='active',certificate_der=sqlc.arg(certificate_der),chain_pem=sqlc.arg(chain_pem),not_before=sqlc.arg(not_before),not_after=sqlc.arg(not_after),row_version=row_version+1,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND state='pending' AND row_version=sqlc.arg(row_version);

-- hikyo:reason StorePKIIssuersTransition verifies instance-config authority; global CA transition requires prior state and row-version CAS.
-- hikyo:instance-scoped
-- name: PKITransitionIssuer :execrows
UPDATE pki_issuers SET state=sqlc.arg(next_state),row_version=row_version+1,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND state=sqlc.arg(prior_state) AND row_version=sqlc.arg(row_version);

-- hikyo:reason StorePKIIssuersDestroyKey verifies instance-config authority; terminal CA transition destroys the sealed key under state/version CAS.
-- hikyo:instance-scoped
-- name: PKIDestroyIssuerKey :execrows
UPDATE pki_issuers SET state=sqlc.arg(next_state),encrypted_private_key=NULL,dek_version=NULL,row_version=row_version+1,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND state=sqlc.arg(prior_state) AND row_version=sqlc.arg(row_version);

-- hikyo:reason Called only after successful StorePKIIssuersDestroyKey revocation CAS in the same transaction; the global parent CA revocation sequence covers the revoked child.
-- hikyo:instance-scoped
-- name: PKIRevokeParent :exec
UPDATE pki_issuers SET revocation_seq=revocation_seq+1 WHERE id=(SELECT child.parent_id FROM pki_issuers child WHERE child.id=sqlc.arg(id) AND child.certificate_der IS NOT NULL) AND state IN ('active','retiring');

-- hikyo:reason StorePKIIssuersHold verifies instance-config authority; global CA restore hold requires the original row-version CAS.
-- hikyo:instance-scoped
-- name: PKISetIssuerHold :execrows
UPDATE pki_issuers SET restore_hold=sqlc.arg(hold),row_version=row_version+1,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND row_version=sqlc.arg(row_version);

-- hikyo:reason StorePKICertificatesCountLive verifies instance-config authority; retiring a global CA must count all dependent unexpired tenant leaves and child CAs.
-- hikyo:instance-scoped
-- name: PKICountLiveCertificates :one
SELECT CAST((SELECT COUNT(*) FROM pki_certificates c WHERE c.issuer_id=sqlc.arg(issuer_id) AND c.state IN ('issuing','issued','renewed','unknown','revoked') AND c.not_after>sqlc.arg(now)) + (SELECT COUNT(*) FROM pki_issuers child WHERE child.parent_id=sqlc.arg(issuer_id) AND child.certificate_der IS NOT NULL AND child.not_after>sqlc.arg(now)) AS BIGINT) AS total;

-- hikyo:reason StorePKICertificatesRevokeLive verifies instance-config authority; compromised global CA revocation must cover all dependent tenant leaves.
-- hikyo:instance-scoped
-- name: PKIRevokeLiveCertificates :execrows
UPDATE pki_certificates SET state='revoked',revoked_at=sqlc.arg(at),revocation_reason=sqlc.arg(reason),row_version=row_version+1,updated_at=sqlc.arg(at) WHERE issuer_id=sqlc.arg(issuer_id) AND state IN ('issuing','issued','renewed','unknown');

-- hikyo:reason Called only after successful StorePKICertificatesRevokeLive changes in the same transaction; global CA sequence forces CRL refresh for those leaves.
-- hikyo:instance-scoped
-- name: PKIBumpIssuerRevocation :exec
UPDATE pki_issuers SET revocation_seq=revocation_seq+1 WHERE id=sqlc.arg(id);

-- hikyo:reason Called only after a proof-scoped certificate revoke CAS or closed PKIRuntime unknown-state CAS succeeds in the same transaction; bumps that certificate issuer CRL sequence.
-- hikyo:instance-scoped
-- name: PKIBumpCertificateRevocation :exec
UPDATE pki_issuers SET revocation_seq=revocation_seq+1 WHERE id=(SELECT c.issuer_id FROM pki_certificates c WHERE c.id=sqlc.arg(id));

-- hikyo:reason StorePKICertificatesRevokedEntries verifies instance-config, or closed PKIRuntime CRL assembly reads all unexpired revoked/unknown leaves for one global CA.
-- hikyo:instance-scoped
-- name: PKIRevokedEntries :many
SELECT serial,COALESCE(revoked_at,updated_at) AS revoked_at,COALESCE(revocation_reason,'unspecified') AS reason FROM pki_certificates WHERE issuer_id=sqlc.arg(issuer_id) AND state IN ('revoked','unknown') AND not_after>sqlc.arg(now) ORDER BY serial;

-- hikyo:reason Same verified instance-config or closed PKIRuntime CRL assembly authority; a global parent CRL must include revoked child CA certificates.
-- hikyo:instance-scoped
-- name: PKIRevokedChildren :many
SELECT certificate_der,updated_at FROM pki_issuers WHERE parent_id=sqlc.arg(issuer_id) AND state='revoked' AND certificate_der IS NOT NULL AND not_after>sqlc.arg(now) ORDER BY id;

-- hikyo:reason StorePKIIssuersPublishCRL verifies instance-config, or closed PKIRuntime publishes a signed global CA CRL; original active/retiring state and prior-number CAS remain.
-- hikyo:instance-scoped
-- name: PKIPublishCRL :execrows
UPDATE pki_issuers SET crl_der=sqlc.arg(der),crl_number=sqlc.arg(number),crl_revocation_seq=sqlc.arg(revocation_seq),crl_this_update=sqlc.arg(this_update),crl_next_update=sqlc.arg(next_update) WHERE id=sqlc.arg(id) AND crl_number=sqlc.arg(previous_number) AND restore_hold=0 AND state IN ('active','retiring');

-- hikyo:reason StorePKIProfilesList verifies instance-config authority; certificate policy definitions are instance-wide and contain no tenant certificate rows.
-- hikyo:instance-scoped
-- name: PKIListProfiles :many
SELECT id,name,policy,row_version,created_by,created_at,updated_at FROM pki_profiles ORDER BY name;

-- hikyo:reason StorePKIProfilesGet verifies instance-config authority; resolves an instance-wide policy definition by name.
-- hikyo:instance-scoped
-- name: PKIGetProfile :one
SELECT id,name,policy,row_version,created_by,created_at,updated_at FROM pki_profiles WHERE name=sqlc.arg(name);

-- hikyo:reason StorePKIProfilesCreate verifies instance-config authority before creating a global certificate policy definition.
-- hikyo:instance-scoped
-- name: PKICreateProfile :exec
INSERT INTO pki_profiles (id,name,policy,created_by,created_at,updated_at) VALUES (sqlc.arg(id),sqlc.arg(name),sqlc.arg(policy),sqlc.arg(created_by),sqlc.arg(at),sqlc.arg(at));

-- hikyo:reason StorePKIProfilesUpdate verifies instance-config authority; global policy update retains its row-version CAS and service narrowing checks.
-- hikyo:instance-scoped
-- name: PKIUpdateProfile :execrows
UPDATE pki_profiles SET policy=sqlc.arg(policy),row_version=row_version+1,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND row_version=sqlc.arg(row_version);

-- hikyo:reason StorePKIProfilesDelete verifies instance-config authority; removes an operator-owned global policy reach before deleting that policy in the same transaction.
-- hikyo:instance-scoped
-- name: PKIDeleteProfileBindings :exec
DELETE FROM pki_profile_bindings WHERE profile_id=sqlc.arg(id);

-- hikyo:reason StorePKIProfilesDelete verifies instance-config authority; deletes the global policy after its bindings are removed in the same transaction.
-- hikyo:instance-scoped
-- name: PKIDeleteProfile :execrows
DELETE FROM pki_profiles WHERE id=sqlc.arg(id);

-- hikyo:reason StorePKIBindingsList verifies instance-config authority; operator reads explicit tenant reaches of one global policy.
-- hikyo:instance-scoped
-- name: PKIListBindings :many
SELECT id,profile_id,org_id,project_id,COALESCE(environment_id,'') AS environment_id,created_by,created_at FROM pki_profile_bindings WHERE profile_id=sqlc.arg(profile_id) ORDER BY org_id,project_id,environment_id;

-- hikyo:reason StorePKIBindingsCreate verifies instance-config authority; operator explicitly names a policy reach, with existing tenant foreign-key and duplicate constraints.
-- hikyo:instance-scoped
-- name: PKICreateBinding :exec
INSERT INTO pki_profile_bindings (id,profile_id,org_id,project_id,environment_id,created_by,created_at) VALUES (sqlc.arg(id),sqlc.arg(profile_id),sqlc.arg(org_id),sqlc.arg(project_id),sqlc.arg(environment_id),sqlc.arg(created_by),sqlc.arg(at));

-- hikyo:reason StorePKIBindingsDelete verifies instance-config authority; operator deletes only the named binding belonging to the named global policy.
-- hikyo:instance-scoped
-- name: PKIDeleteBinding :execrows
DELETE FROM pki_profile_bindings WHERE id=sqlc.arg(id) AND profile_id=sqlc.arg(profile_id);

-- name: PKIBoundProfile :one
SELECT id,name,policy,row_version,created_by,created_at,updated_at FROM pki_profiles WHERE name=sqlc.arg(name) AND id IN (SELECT profile_id FROM pki_profile_bindings WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND (environment_id IS NULL OR environment_id=sqlc.arg(chain_env)));

-- name: PKIBoundProfiles :many
SELECT id,name,policy,row_version,created_by,created_at,updated_at FROM pki_profiles WHERE id IN (SELECT profile_id FROM pki_profile_bindings WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND (environment_id IS NULL OR environment_id=sqlc.arg(chain_env))) ORDER BY name;

-- name: PKIGetCertificate :one
SELECT id,environment_id,profile_id,profile_name,issuer_id,serial,state,key_source,key_algorithm,key_fingerprint,common_name,sans,not_before,not_after,certificate_der,principal_id,principal_class,COALESCE(renewed_from,'') AS renewed_from,COALESCE(renewed_by,'') AS renewed_by,revoked_at,COALESCE(revocation_reason,'') AS revocation_reason,issuing_deadline,row_version,created_at,updated_at FROM pki_certificates WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: PKIListCertificates :many
SELECT id,environment_id,profile_id,profile_name,issuer_id,serial,state,key_source,key_algorithm,key_fingerprint,common_name,sans,not_before,not_after,certificate_der,principal_id,principal_class,COALESCE(renewed_from,'') AS renewed_from,COALESCE(renewed_by,'') AS renewed_by,revoked_at,COALESCE(revocation_reason,'') AS revocation_reason,issuing_deadline,row_version,created_at,updated_at FROM pki_certificates WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) ORDER BY created_at DESC,id DESC LIMIT 500;

-- hikyo:reason StorePKIIssuersFence verifies issuance authority; writes the global CA counter only while active and not held, preserving the cross-node signing fence.
-- hikyo:instance-scoped
-- name: PKIFenceIssuance :execrows
UPDATE pki_issuers SET issued_count=issued_count+1 WHERE id=sqlc.arg(id) AND state='active' AND restore_hold=0;

-- name: PKICreateCertificate :exec
INSERT INTO pki_certificates (id,org_id,project_id,environment_id,profile_id,profile_name,issuer_id,serial,state,key_source,key_algorithm,key_fingerprint,common_name,sans,not_before,not_after,principal_id,principal_class,renewed_from,issuing_deadline,created_at,updated_at) VALUES (sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(profile_id),sqlc.arg(profile_name),sqlc.arg(issuer_id),sqlc.arg(serial),'issuing',sqlc.arg(key_source),sqlc.arg(key_algorithm),sqlc.arg(key_fingerprint),sqlc.arg(common_name),sqlc.arg(sans),sqlc.arg(not_before),sqlc.arg(not_after),sqlc.arg(principal_id),sqlc.arg(principal_class),sqlc.arg(renewed_from),sqlc.arg(issuing_deadline),sqlc.arg(at),sqlc.arg(at));

-- name: PKIFinishCertificate :execrows
UPDATE pki_certificates SET state='issued',certificate_der=sqlc.arg(der),updated_at=sqlc.arg(at),row_version=row_version+1 WHERE id=sqlc.arg(id) AND state='issuing' AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: PKIFailCertificate :execrows
UPDATE pki_certificates SET state='failed',updated_at=sqlc.arg(at),row_version=row_version+1 WHERE id=sqlc.arg(id) AND state='issuing' AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: PKIClaimRenewal :execrows
UPDATE pki_certificates SET renewed_by=sqlc.arg(successor_id),updated_at=sqlc.arg(at),row_version=row_version+1 WHERE id=sqlc.arg(id) AND state='issued' AND renewed_by IS NULL AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: PKICompleteRenewal :execrows
UPDATE pki_certificates SET state='renewed',updated_at=sqlc.arg(at),row_version=row_version+1 WHERE id=sqlc.arg(id) AND state='issued' AND renewed_by=sqlc.arg(successor_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: PKIReleaseRenewal :execrows
UPDATE pki_certificates SET renewed_by=NULL,updated_at=sqlc.arg(at),row_version=row_version+1 WHERE id=sqlc.arg(id) AND renewed_by=sqlc.arg(successor_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: PKIRevokeCertificate :execrows
UPDATE pki_certificates SET state='revoked',revoked_at=sqlc.arg(at),revocation_reason=sqlc.arg(reason),updated_at=sqlc.arg(at),row_version=row_version+1 WHERE id=sqlc.arg(id) AND state IN ('issued','renewed','unknown') AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);
