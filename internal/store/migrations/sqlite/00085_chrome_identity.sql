-- +goose Up
-- Editable chrome identity: a hue, short mark and uploaded image per org and project.
-- Defaults keep every existing row rendering exactly as before (hue 195, name-derived mark).
ALTER TABLE orgs ADD COLUMN identity_hue INTEGER NOT NULL DEFAULT 195
    CHECK (identity_hue BETWEEN 0 AND 359);
ALTER TABLE orgs ADD COLUMN identity_mark TEXT;
ALTER TABLE orgs ADD COLUMN identity_image BLOB;
ALTER TABLE orgs ADD COLUMN identity_image_type TEXT;
ALTER TABLE orgs ADD COLUMN identity_image_version TEXT;
ALTER TABLE projects ADD COLUMN identity_hue INTEGER NOT NULL DEFAULT 195
    CHECK (identity_hue BETWEEN 0 AND 359);
ALTER TABLE projects ADD COLUMN identity_mark TEXT;
ALTER TABLE projects ADD COLUMN identity_image BLOB;
ALTER TABLE projects ADD COLUMN identity_image_type TEXT;
ALTER TABLE projects ADD COLUMN identity_image_version TEXT;
