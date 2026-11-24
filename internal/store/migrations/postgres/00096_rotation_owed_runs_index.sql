-- +goose Up
-- The owed-drop summary of a rotation (#148 S4) and the list of deleted rotations
-- that still owe a drop count and probe the runs in phase revoke_refused or
-- quarantine. They are a handful beside a rotation's whole history, so this partial
-- index lets both read only them, one rotation at a time, instead of every run.
CREATE INDEX rotation_runs_owed ON rotation_runs (org_id, project_id, environment_id, rotation_id) WHERE phase IN ('revoke_refused', 'quarantine');
