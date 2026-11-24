-- +goose Up
-- The moment a credential establishment consumed an authority (#1039 S5). A sweep
-- (containment, a newer reset) consumes an authority without establishing anything
-- and never sets it, so release reads establishment from this column alone.
ALTER TABLE credential_authorities ADD COLUMN established_at TEXT;
