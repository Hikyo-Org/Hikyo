-- +goose Up
CREATE TABLE master_keys(version BIGINT NOT NULL, root_key_epoch BIGINT NOT NULL, state TEXT NOT NULL, blob BYTEA NOT NULL, created_at TIMESTAMP WITH TIME ZONE NOT NULL);
CREATE TABLE tier3_keys(id TEXT NOT NULL, purpose TEXT NOT NULL, org_id TEXT NOT NULL, project_id TEXT NOT NULL, version BIGINT NOT NULL, master_key_version BIGINT NOT NULL, state TEXT NOT NULL, blob BYTEA NOT NULL, created_at TIMESTAMP WITH TIME ZONE NOT NULL);
