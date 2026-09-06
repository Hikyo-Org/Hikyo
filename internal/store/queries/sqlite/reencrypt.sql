-- Temporary node bootstrap inputs use the instance DEK, not a project key.
-- name: ListSelfConfigSeedInputsForReencrypt :many
SELECT node_id, ciphertext, dek_version, row_version FROM self_config_seed_inputs
WHERE node_id > sqlc.arg(cursor) ORDER BY node_id LIMIT sqlc.arg(page_limit);

-- The ciphertext guard also rejects delete/reinsert at the same row version.
-- name: ReencryptSelfConfigSeedInput :execrows
UPDATE self_config_seed_inputs
SET ciphertext=sqlc.arg(ct), dek_version=sqlc.arg(dek_version), row_version=row_version+1
WHERE node_id=sqlc.arg(node_id) AND row_version=sqlc.arg(row_version) AND ciphertext=sqlc.arg(old_ct);
