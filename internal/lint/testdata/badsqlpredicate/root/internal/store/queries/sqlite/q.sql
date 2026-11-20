-- name: GetWidgetScoped :one
SELECT id FROM widgets WHERE org_id = ? AND project_id = ? AND id = ?;

-- name: GetWidgetUnscoped :one
SELECT id FROM widgets WHERE id = ?;
