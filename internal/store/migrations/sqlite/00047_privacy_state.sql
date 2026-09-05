-- +goose Up
-- Durable host-local privacy restriction. Historical foreign keys retain the principal.
ALTER TABLE principals ADD COLUMN privacy_state TEXT NOT NULL DEFAULT 'active' CHECK (privacy_state IN ('active', 'restricted', 'erased'));
