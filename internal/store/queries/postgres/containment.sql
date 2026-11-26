-- Reversible principal containment (incident-containment ADR, O1 option B).
-- The state is read by the gates in authn.sql, permission.sql, rules.sql,
-- personal_authz.sql, registration.sql and webauthn.sql; this file holds its
-- setters, its one read, the ONE recovery lookup that bypasses it, and the
-- pre-containment projections a recovery resolver bound to an older source
-- schema needs. ASCII only: multibyte characters shift sqlite statement
-- offsets.

-- Advancing the generation in the same statement ends every session, delegation and
-- step-up of the principal at the moment of containment, as privacy restriction does.
-- hikyo:reason Containment is a local-authority write: the caller proves the operation and takes the principal-row lock first, and a contained principal is denied everywhere the privacy gate already denies.
-- hikyo:authn-resolution
-- name: ContainPrincipal :execrows
UPDATE principals
SET containment_state = 'contained',
    session_generation = session_generation + 1,
    contained_at = sqlc.arg(contained_at),
    contained_incident_id = sqlc.narg(contained_incident_id)
WHERE id = sqlc.arg(principal_id) AND kind = 'human' AND containment_state = 'none';

-- A containment starts a new episode of release evidence: an establishment stamped
-- under an earlier containment of the same incident (contain, establish, release,
-- contain again) must not count for the next release. The sweep that follows
-- consumes any outstanding authority; this clears the stamps already on consumed ones.
-- hikyo:reason Containment clears the establishment stamps of the contained principal's own account in the same transaction as the containment, so release evidence never outlives the episode that earned it; it touches no secret and no other account.
-- hikyo:authn-resolution
-- name: ClearAuthorityEstablishmentStamps :execrows
UPDATE credential_authorities SET established_in_incident = NULL
WHERE account_id = sqlc.arg(account_id) AND established_in_incident IS NOT NULL;

-- The generation advances again on release, so no session or delegation that predates
-- containment ever becomes valid again: release restores access through a fresh sign-in.
-- hikyo:reason Release returns the principal to none and clears the incident reference; it advances the generation so nothing minted before containment revives, and it restores no authority.
-- hikyo:authn-resolution
-- name: ReleasePrincipalContainment :execrows
UPDATE principals
SET containment_state = 'none',
    session_generation = session_generation + 1,
    contained_at = NULL,
    contained_incident_id = NULL
WHERE id = sqlc.arg(principal_id) AND kind = 'human' AND containment_state = 'contained'
  AND contained_incident_id = sqlc.arg(contained_incident_id);

-- Release starts a fresh retention window (incident-containment ADR, O8): a
-- suspension recorded before containment must not survive a contain and release
-- that no sweep saw, or a later access loss would delete from the old clock. A
-- privacy-restricted or erased owner keeps privacy's running clock.
-- hikyo:reason Release clears the retention suspension of the released principal's own personal values in the same transaction, so a clock started before containment cannot expire a value after release; it touches no ciphertext, leaves a privacy-restricted owner's clock running and clears nothing while the principal is contained.
-- hikyo:authn-resolution
-- name: ClearPersonalSuspensionForOwner :execrows
UPDATE personal_values SET suspended_at = NULL
WHERE owner_principal_id = sqlc.arg(owner_principal_id)
  AND suspended_at IS NOT NULL
  AND owner_principal_id IN (SELECT principals.id FROM principals WHERE principals.privacy_state = 'active' AND principals.containment_state = 'none');

-- The containment sweep consumes the outstanding authorities of the principal's
-- account whatever its privacy state: restricted, contained, released and then
-- privacy-released must not leave a pre-containment authority redeemable.
-- hikyo:reason Containment resolves the account id of the principal it holds, whatever the principal's privacy state, so its outstanding credential authorities are consumed in the same transaction; it returns one identifier and no secret.
-- hikyo:authn-resolution
-- name: GetAccountIDForContainmentSweep :one
SELECT id FROM accounts WHERE principal_id = sqlc.arg(principal_id);

-- The one read of the state: the personal-value retention sweep and credential
-- establishment. The columns come back so the caller decides: only a principal
-- that privacy leaves active is held by containment, and a restricted or erased
-- one keeps privacy's behavior.
-- hikyo:reason The retention sweep reads one principal's two state columns to decide whether containment pauses its personal-value deletion clock; returns no identifiers or secrets.
-- hikyo:authn-resolution
-- name: GetPrincipalContainment :one
SELECT containment_state, privacy_state, contained_at FROM principals WHERE id = sqlc.arg(principal_id);

-- The ONE recovery lookup. Credential reset and establishment authenticate
-- with a reset authority and carry no grants, so a contained account must stay
-- reachable to them, while the shared lookups (GetAccountByUsername, ...ByID,
-- ...ByPrincipal) also serve sign-in, MFA and SSO and stay gated. This drops
-- ONLY the containment condition: a restricted or erased account stays
-- unreachable. Exactly one of the two arguments is set by the caller; the
-- other is empty and matches nothing. The lint recoverylookup analyzer confines its
-- callers to the reset and establishment flows.
-- hikyo:reason Reset issuance and credential establishment resolve the account of a principal containment may hold, with the containment condition dropped and the privacy gate kept; callable only from those flows.
-- hikyo:authn-resolution
-- name: GetAccountForRecovery :one
SELECT id, principal_id, username, display_name, created_at FROM accounts
WHERE (accounts.id = sqlc.arg(account_id) OR accounts.principal_id = sqlc.arg(principal_id))
  AND principal_id IN (SELECT principals.id FROM principals WHERE principals.privacy_state = 'active');

-- Verified source schemas 66 through 88 predate containment: the chokepoint
-- projections without the containment condition, for guarded historical
-- recovery only. A schema that old cannot hold a contained principal.
-- hikyo:reason Historical recovery of a source schema older than 89 reads the principal's grants without the containment condition, which that schema cannot hold; the privacy gate and restore epoch stay.
-- hikyo:authn-resolution
-- name: RecoveryListGrantsBeforeContainment :many
SELECT g.capability, g.org_id, g.project_id, g.env_id,
  CAST(COALESCE((SELECT org_id FROM self_config_binding WHERE id = 1), '') AS TEXT) AS self_config_org_id
FROM grants AS g
JOIN principals AS p ON p.id = g.principal_id
WHERE g.principal_id = sqlc.arg(principal_id)
  AND p.privacy_state = 'active'
  AND p.reconciled_epoch >= (SELECT restore_epoch FROM auth_instance_state WHERE auth_instance_state.id = 1)
UNION ALL
SELECT j.capability, j.org_id, j.project_id, j.env_id,
  CAST(COALESCE((SELECT org_id FROM self_config_binding WHERE id = 1), '') AS TEXT) AS self_config_org_id
FROM access_grants AS j
JOIN principals AS jp ON jp.id = j.principal_id
WHERE j.principal_id = sqlc.arg(principal_id)
  AND j.expires_at > sqlc.arg(now)
  AND jp.privacy_state = 'active'
  AND jp.reconciled_epoch >= (SELECT restore_epoch FROM auth_instance_state WHERE auth_instance_state.id = 1);

-- hikyo:reason Historical recovery of a source schema older than 89 reads the principal's member access rules without the containment condition, which that schema cannot hold; the privacy gate and restore epoch stay.
-- hikyo:authn-resolution
-- name: RecoveryListRulesBeforeContainment :many
SELECT r.id, r.capability, r.org_id, r.env_mode, r.key_mode,
  i.axis, i.project_id,
  CAST(COALESCE(i.env_id, '') AS TEXT) AS env_id,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
JOIN principals AS p ON p.id = r.principal_id
JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.principal_id = sqlc.arg(principal_id)
  AND p.kind = 'human'
  AND p.privacy_state = 'active'
  AND p.reconciled_epoch >= (SELECT restore_epoch FROM auth_instance_state WHERE auth_instance_state.id = 1)
ORDER BY r.id, i.id;

-- hikyo:reason Historical recovery of source schema 86 through 88 reads the owner fallback without the containment condition, which that schema cannot hold; the privacy gate, human kind and restore epoch stay.
-- hikyo:authn-resolution
-- name: RecoveryPersonalOwnerHoldsBeforeContainment :one
SELECT COUNT(*) FROM principals AS p
WHERE p.id = sqlc.arg(principal_id)
  AND p.kind = 'human'
  AND p.privacy_state = 'active'
  AND p.reconciled_epoch >= (SELECT restore_epoch FROM auth_instance_state WHERE auth_instance_state.id = 1)
  AND (EXISTS (
      SELECT 1 FROM personal_values AS v
      WHERE v.org_id = sqlc.arg(org_id) AND v.project_id = sqlc.arg(project_id)
        AND v.environment_id = sqlc.arg(env_id) AND v.owner_principal_id = p.id)
    OR EXISTS (
      SELECT 1 FROM personal_value_notices AS n
      WHERE n.org_id = sqlc.arg(org_id) AND n.project_id = sqlc.arg(project_id)
        AND n.environment_id = sqlc.arg(env_id) AND n.owner_principal_id = p.id));

-- Release evidence (incident-containment ADR, O4 A). A credential establishment
-- stamps the incident that contained the principal on the authority it consumes; a
-- sweep consumes without stamping, so only an establishment made while that
-- incident held the principal counts, and no node's clock is compared.
-- hikyo:reason Release counts the establishments of one principal's account stamped with one incident; it returns a count and no identifier or secret.
-- hikyo:authn-resolution
-- name: CountAuthorityEstablishmentsIn :one
SELECT CAST(COUNT(*) AS BIGINT) AS establishments FROM credential_authorities
WHERE credential_authorities.account_id = (SELECT accounts.id FROM accounts WHERE accounts.principal_id = sqlc.arg(principal_id)) AND credential_authorities.established_in_incident = sqlc.arg(incident_id);

-- hikyo:reason Release names the incident that contained a principal, to find the attested ledger item for its linked identities; it returns one incident id and nothing else.
-- hikyo:authn-resolution
-- name: GetPrincipalContainedIncident :one
SELECT contained_incident_id FROM principals WHERE id = sqlc.arg(principal_id) AND containment_state = 'contained';

-- Closing hands a principal over instead of stranding it: when the closing incident
-- holds a principal that another open incident also recorded as contained (done), the
-- state passes to the lowest such incident id. Only the incident reference changes; the
-- state, the instant and the generation stay, so the handover never lifts containment.
-- hikyo:reason Close hands each principal the closing incident holds to another open incident that recorded it contained; it changes only the incident reference and never the containment state.
-- hikyo:authn-resolution
-- name: HandOverContainedPrincipals :many
UPDATE principals
SET contained_incident_id = (
    SELECT MIN(incidents.id) FROM incidents
    JOIN incident_items ON incident_items.incident_id = incidents.id
    WHERE incidents.status = 'open' AND incidents.id <> sqlc.arg(incident_id)
      AND incident_items.owner_function = 'account.contain' AND incident_items.object_kind = 'account'
      AND incident_items.object_id = principals.id AND incident_items.outcome = 'done')
WHERE principals.contained_incident_id = sqlc.arg(incident_id) AND principals.containment_state = 'contained' AND principals.kind = 'human'
  AND EXISTS (
    SELECT 1 FROM incidents
    JOIN incident_items ON incident_items.incident_id = incidents.id
    WHERE incidents.status = 'open' AND incidents.id <> sqlc.arg(incident_id)
      AND incident_items.owner_function = 'account.contain' AND incident_items.object_kind = 'account'
      AND incident_items.object_id = principals.id AND incident_items.outcome = 'done')
RETURNING principals.id, principals.contained_incident_id;

-- hikyo:reason Close asks whether an incident still holds a contained principal, because closing would strand it with no ledger left to release it; it returns a count and nothing else.
-- hikyo:authn-resolution
-- name: CountPrincipalsContainedBy :one
SELECT CAST(COUNT(*) AS BIGINT) AS held FROM principals WHERE contained_incident_id = sqlc.arg(incident_id) AND containment_state = 'contained';

-- hikyo:reason Release reads whether a principal's account holds a local password, to decide between the credential-establishment and the identity-attestation condition; it returns a count and nothing else.
-- hikyo:authn-resolution
-- name: CountPasswordCredentialsForPrincipal :one
SELECT COUNT(*) FROM password_credentials WHERE account_id = (SELECT accounts.id FROM accounts WHERE accounts.principal_id = sqlc.arg(principal_id));

-- hikyo:reason Release names another open incident whose ledger recorded the principal's containment as done, because that incident's operator also holds a claim on the contained state; it returns one incident id and nothing else.
-- hikyo:authn-resolution
-- name: GetOtherOpenIncidentHoldingPrincipal :one
SELECT incidents.id FROM incidents
JOIN incident_items ON incident_items.incident_id = incidents.id
WHERE incidents.status = 'open' AND incidents.id <> sqlc.arg(incident_id)
  AND incident_items.owner_function = 'account.contain' AND incident_items.object_kind = 'account'
  AND incident_items.object_id = sqlc.arg(principal_id) AND incident_items.outcome = 'done'
ORDER BY incidents.id LIMIT 1;
