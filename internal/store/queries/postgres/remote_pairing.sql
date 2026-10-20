-- Manual pairing metadata is authentication state; never a tenant lookup.
-- hikyo:reason Resolve and administer finite manual invitations only after capability or instance-config authorization.
-- hikyo:authn-resolution
-- name: CreateRemotePairingInvite :exec
INSERT INTO remote_pairing_invites(id,label,origin,spki_pin,capability_verifier,pake_sealed,credential_epoch,created_by,created_at,expires_at,credential_expires_at)
VALUES(sqlc.arg(id),sqlc.arg(label),sqlc.arg(origin),sqlc.arg(spki_pin),sqlc.arg(capability_verifier),sqlc.arg(pake_sealed),sqlc.arg(credential_epoch),sqlc.arg(created_by),sqlc.arg(created_at),sqlc.arg(expires_at),sqlc.arg(credential_expires_at));

-- hikyo:reason Resolve the unguessable invitation capability before invoking PAKE.
-- hikyo:authn-resolution
-- name: RemotePairingInviteByCapability :one
SELECT * FROM remote_pairing_invites WHERE capability_verifier=sqlc.arg(verifier);

-- hikyo:reason Exchange continuation resolves its recorded invitation inside the consuming transaction.
-- hikyo:authn-resolution
-- name: RemotePairingInviteByID :one
SELECT * FROM remote_pairing_invites WHERE id=sqlc.arg(id);

-- hikyo:reason Authorized instance custody listing returns metadata through its caller, never capability or PAKE records.
-- hikyo:authn-resolution
-- name: ListRemotePairingInvites :many
SELECT id,label,origin,spki_pin,credential_epoch,created_by,created_at,expires_at,credential_expires_at,revoked_at,consumed_at FROM remote_pairing_invites ORDER BY created_at DESC,id LIMIT 100;

-- hikyo:reason Serialize finite invitation admission and bound storage under the instance census lock.
-- hikyo:authn-resolution
-- name: CountLiveRemotePairingInvites :one
SELECT COUNT(*) FROM remote_pairing_invites WHERE revoked_at IS NULL AND consumed_at IS NULL AND expires_at>sqlc.arg(now) AND credential_epoch=(SELECT credential_epoch FROM auth_instance_state WHERE id=1);

-- hikyo:reason Revoke the invitation atomically with its administrative audit record.
-- hikyo:authn-resolution
-- name: RevokeRemotePairingInvite :execrows
UPDATE remote_pairing_invites SET revoked_at=sqlc.arg(now) WHERE id=sqlc.arg(id) AND revoked_at IS NULL AND consumed_at IS NULL;

-- hikyo:reason Single-use consumption is part of the same transaction as connection principal, grant and key enrollment.
-- hikyo:authn-resolution
-- name: ConsumeRemotePairingInvite :execrows
UPDATE remote_pairing_invites SET consumed_at=sqlc.arg(now) WHERE id=sqlc.arg(id) AND revoked_at IS NULL AND consumed_at IS NULL AND expires_at>sqlc.arg(now) AND credential_epoch=sqlc.arg(epoch);

-- hikyo:reason Persist sealed PAKE exchange state so continuation works across server nodes.
-- hikyo:authn-resolution
-- name: CreateRemotePairingExchange :exec
INSERT INTO remote_pairing_exchanges(id,invite_id,state_sealed,credential_epoch,created_at,expires_at) VALUES(sqlc.arg(id),sqlc.arg(invite_id),sqlc.arg(state_sealed),sqlc.arg(credential_epoch),sqlc.arg(created_at),sqlc.arg(expires_at));

-- hikyo:reason Resolve bounded exchange state before atomic invitation consumption.
-- hikyo:authn-resolution
-- name: RemotePairingExchangeByID :one
SELECT * FROM remote_pairing_exchanges WHERE id=sqlc.arg(id);

-- hikyo:reason Bound outstanding PAKE sessions per invitation before expensive server state creation.
-- hikyo:authn-resolution
-- name: CountLiveRemotePairingExchanges :one
SELECT COUNT(*) FROM remote_pairing_exchanges WHERE invite_id=sqlc.arg(invite_id) AND consumed_at IS NULL AND expires_at>sqlc.arg(now);

-- hikyo:reason Exchanges can be consumed once even when two nodes receive the same finish concurrently.
-- hikyo:authn-resolution
-- name: ConsumeRemotePairingExchange :execrows
UPDATE remote_pairing_exchanges SET consumed_at=sqlc.arg(now) WHERE id=sqlc.arg(id) AND consumed_at IS NULL AND expires_at>sqlc.arg(now) AND credential_epoch=sqlc.arg(epoch);

-- hikyo:reason Enrollment permanently marks this connection proof-bound; ordinary bearer resolution must reject it.
-- hikyo:authn-resolution
-- name: BindRemotePairedConnection :exec
INSERT INTO remote_paired_connections(connection_id,public_jwk,thumbprint,origin) VALUES(sqlc.arg(connection_id),sqlc.arg(public_jwk),sqlc.arg(thumbprint),sqlc.arg(origin));

-- hikyo:reason Authentication rechecks the enrolled public key inside every directory serve transaction.
-- hikyo:authn-resolution
-- name: RemotePairedConnectionByID :one
SELECT * FROM remote_paired_connections WHERE connection_id=sqlc.arg(id);

-- hikyo:reason Persist DPoP replay admission across nodes in the same transaction as directory authorization.
-- hikyo:authn-resolution
-- name: ClaimRemotePairingReplay :execrows
INSERT INTO remote_pairing_replays(thumbprint,jti_verifier,expires_at) VALUES(sqlc.arg(thumbprint),sqlc.arg(jti_verifier),sqlc.arg(expires_at)) ON CONFLICT DO NOTHING;

-- hikyo:reason Fail closed at the live replay cap rather than evicting proofs still replayable.
-- hikyo:authn-resolution
-- name: CountRemotePairingReplays :one
SELECT COUNT(*) FROM remote_pairing_replays;

-- hikyo:reason Bound one enrolled key's replay share before it can exhaust installation-wide admission for other peers.
-- hikyo:authn-resolution
-- name: CountRemotePairingReplaysForKey :one
SELECT COUNT(*) FROM remote_pairing_replays WHERE thumbprint=sqlc.arg(thumbprint);

-- hikyo:reason Remove only expired replay records before bounded admission.
-- hikyo:authn-resolution
-- name: PruneRemotePairingReplays :exec
DELETE FROM remote_pairing_replays WHERE expires_at<=sqlc.arg(now);

-- hikyo:reason Remove expired exchange secrets in bounded control-plane lifecycle work.
-- hikyo:authn-resolution
-- name: PruneRemotePairingExchanges :exec
DELETE FROM remote_pairing_exchanges WHERE expires_at<=sqlc.arg(now);

-- hikyo:reason Remove expired finite invitation state after retaining its audit trail separately.
-- hikyo:authn-resolution
-- name: PruneRemotePairingInvites :exec
DELETE FROM remote_pairing_invites WHERE expires_at<=sqlc.arg(now);

-- hikyo:reason A restored archive cannot choose an epoch that reactivates a future-stamped pairing artifact.
-- hikyo:authn-resolution
-- name: RemotePairingMaxCredentialEpoch :one
SELECT MAX(epoch) FROM (SELECT COALESCE(MAX(credential_epoch),0) AS epoch FROM remote_pairing_invites UNION ALL SELECT COALESCE(MAX(credential_epoch),0) AS epoch FROM remote_pairing_exchanges) AS stamps;

-- hikyo:reason Each valid capability admits at most eight expensive PAKE attempts across all nodes.
-- hikyo:authn-resolution
-- name: ClaimRemotePairingAttempt :execrows
UPDATE remote_pairing_invites SET attempts=attempts+1 WHERE id=sqlc.arg(id) AND attempts<8 AND revoked_at IS NULL AND consumed_at IS NULL AND expires_at>sqlc.arg(now);

-- Reencrypt preserves every retained pairing secret across instance-DEK retirement.
-- hikyo:reason Instance reencrypt authorization precedes this bounded ciphertext scan; the proof-gated repository verifies the exact transaction and operation.
-- hikyo:authn-resolution
-- name: ListPairingInvitesForReencrypt :many
SELECT id,pake_sealed FROM remote_pairing_invites WHERE id>sqlc.arg(cursor) ORDER BY id LIMIT sqlc.arg(page_limit);

-- hikyo:reason Instance reencrypt authorization precedes ciphertext replacement guarded by the old blob; no pairing consent or credential authority is widened.
-- hikyo:authn-resolution
-- name: ReencryptPairingInvites :execrows
UPDATE remote_pairing_invites SET pake_sealed=sqlc.arg(new_ct) WHERE id=sqlc.arg(id) AND pake_sealed=sqlc.arg(old_ct);

-- Reencrypt preserves every retained pairing secret across instance-DEK retirement.
-- hikyo:reason Instance reencrypt authorization precedes this bounded ciphertext scan; the proof-gated repository verifies the exact transaction and operation.
-- hikyo:authn-resolution
-- name: ListPairingExchangesForReencrypt :many
SELECT id,state_sealed FROM remote_pairing_exchanges WHERE id>sqlc.arg(cursor) ORDER BY id LIMIT sqlc.arg(page_limit);

-- hikyo:reason Instance reencrypt authorization precedes ciphertext replacement guarded by the old blob; no pairing consent or credential authority is widened.
-- hikyo:authn-resolution
-- name: ReencryptPairingExchanges :execrows
UPDATE remote_pairing_exchanges SET state_sealed=sqlc.arg(new_ct) WHERE id=sqlc.arg(id) AND state_sealed=sqlc.arg(old_ct);
