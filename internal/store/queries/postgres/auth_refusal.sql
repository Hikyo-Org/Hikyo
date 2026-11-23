-- hikyo:reason refusal accounting bounds its own lock wait and statements, commit included, server-side so an abandoned transaction cannot hold the singleton row past the caller's deadline
-- hikyo:authn-resolution
-- name: BoundAuthRefusalTransaction :exec
SELECT set_config('lock_timeout', sqlc.arg(timeout_ms)::text, true), set_config('statement_timeout', sqlc.arg(timeout_ms)::text, true);

-- hikyo:reason singleton pre-auth refusal accounting reads datastore time and bounded state under its transaction owner
-- hikyo:authn-resolution
-- name: ReadAuthRefusalWindow :one
SELECT window_start, state, floor(extract(epoch FROM clock_timestamp()))::bigint AS now_seconds
FROM auth_refusal_window WHERE id = 1 FOR UPDATE;

-- hikyo:reason approved refusal writer atomically advances only the singleton accumulator
-- hikyo:authn-resolution
-- name: SaveAuthRefusalWindow :exec
UPDATE auth_refusal_window SET window_start = $1, state = $2 WHERE id = 1;

-- Only the untouched staging seed may be replaced by a restore.
-- hikyo:reason restore target admission preserves pending authentication refusal evidence
-- hikyo:authn-resolution
-- name: RestoreAuthRefusalSeedOccupied :one
SELECT EXISTS (SELECT 1 FROM auth_refusal_window WHERE id <> 1 OR window_start <> 0 OR state <> '{"counts":[0,0,0,0,0],"samples":[]}');
