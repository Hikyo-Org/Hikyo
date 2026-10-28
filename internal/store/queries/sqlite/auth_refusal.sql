-- hikyo:reason singleton pre-auth refusal accounting reads datastore time and bounded state under its transaction owner
-- hikyo:authn-resolution
-- name: ReadAuthRefusalWindow :one
SELECT window_start, state, CAST(unixepoch('now') AS INTEGER) AS now_seconds
FROM auth_refusal_window WHERE id = 1;

-- hikyo:reason approved refusal writer atomically advances only the singleton accumulator
-- hikyo:authn-resolution
-- name: SaveAuthRefusalWindow :exec
UPDATE auth_refusal_window SET window_start = ?, state = ? WHERE id = 1;
