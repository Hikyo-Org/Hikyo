-- +goose Up
-- Contact metadata only; email is never an authentication or linking key.
ALTER TABLE accounts ADD COLUMN email TEXT NOT NULL DEFAULT '';
