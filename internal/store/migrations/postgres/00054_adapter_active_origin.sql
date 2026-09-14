-- +goose Up
-- #744: a tombstoned adapter must not reserve its origin forever. The
-- unconditional UNIQUE (org_id, project_id, origin) blocked recreating a
-- deleted adapter at the same origin ("The current state of this target
-- refuses the request."). Scope origin uniqueness to live adapters: active and
-- moving rows still collide, tombstoned history is kept and no longer reserves.
ALTER TABLE adapters DROP CONSTRAINT adapters_org_id_project_id_origin_key;
CREATE UNIQUE INDEX adapters_active_origin
    ON adapters (org_id, project_id, origin)
    WHERE state <> 'tombstoned';
