-- name: GatedBoth :one
SELECT id FROM principals AS p WHERE p.privacy_state = 'active' AND p.containment_state = 'none';
