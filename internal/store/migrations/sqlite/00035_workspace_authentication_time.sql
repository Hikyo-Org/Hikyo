-- +goose Up
-- Preserve the approving browser session's actual authentication instant.
-- Approval time is not authentication time: treating it as such lets opening
-- a workspace launder an old session into a fresh one.
ALTER TABLE workspace_handoffs
ADD COLUMN authenticated_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00.000000Z';

DELETE FROM workspace_handoffs;
