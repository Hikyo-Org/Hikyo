-- +goose Up
-- SQLite compares these adapter scheduling and lease timestamps lexically.
-- Normalize the RFC3339Nano forms written by repository paths to the fixed
-- six-digit UTC form used by the adapter runtime. CanonTime limits persisted
-- precision to microseconds, so padding these known forms is lossless.
UPDATE adapter_targets
SET provider_lease_expires_at = CASE
    WHEN length(provider_lease_expires_at) = 20
        THEN substr(provider_lease_expires_at, 1, 19) || '.000000Z'
    WHEN instr(provider_lease_expires_at, '.') = 20
         AND substr(provider_lease_expires_at, -1) = 'Z'
         AND length(provider_lease_expires_at) BETWEEN 22 AND 27
        THEN substr(provider_lease_expires_at, 1, 20)
             || substr(substr(provider_lease_expires_at, 21, length(provider_lease_expires_at) - 21) || '000000', 1, 6)
             || 'Z'
    ELSE provider_lease_expires_at
END
WHERE provider_lease_expires_at IS NOT NULL;

UPDATE adapter_outbox
SET next_attempt_at = CASE
        WHEN length(next_attempt_at) = 20
            THEN substr(next_attempt_at, 1, 19) || '.000000Z'
        WHEN instr(next_attempt_at, '.') = 20
             AND substr(next_attempt_at, -1) = 'Z'
             AND length(next_attempt_at) BETWEEN 22 AND 27
            THEN substr(next_attempt_at, 1, 20)
                 || substr(substr(next_attempt_at, 21, length(next_attempt_at) - 21) || '000000', 1, 6)
                 || 'Z'
        ELSE next_attempt_at
    END,
    lease_expires_at = CASE
        WHEN length(lease_expires_at) = 20
            THEN substr(lease_expires_at, 1, 19) || '.000000Z'
        WHEN instr(lease_expires_at, '.') = 20
             AND substr(lease_expires_at, -1) = 'Z'
             AND length(lease_expires_at) BETWEEN 22 AND 27
            THEN substr(lease_expires_at, 1, 20)
                 || substr(substr(lease_expires_at, 21, length(lease_expires_at) - 21) || '000000', 1, 6)
                 || 'Z'
        ELSE lease_expires_at
    END;
