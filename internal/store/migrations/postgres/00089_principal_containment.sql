-- +goose Up
-- Reversible incident containment (incident-containment ADR, O1 option B). A
-- contained principal cannot sign in and its grants and rules stop loading;
-- nothing is deleted. A column of its own, never privacy_state: the privacy
-- columns, receipts and audit types belong to a different feature.
-- contained_incident_id names the incident ledger row a later migration adds,
-- so it carries no foreign key here.
ALTER TABLE principals ADD COLUMN containment_state TEXT NOT NULL DEFAULT 'none' CHECK (containment_state IN ('none', 'contained'));
ALTER TABLE principals ADD COLUMN contained_at TIMESTAMPTZ;
ALTER TABLE principals ADD COLUMN contained_incident_id TEXT;
