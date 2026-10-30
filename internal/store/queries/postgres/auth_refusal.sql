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
