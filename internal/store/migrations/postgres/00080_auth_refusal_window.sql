-- +goose Up
-- Approved #1034 bounded mutable staging, separate from append-only trails.
-- hikyo:table auth_refusal_window class=authn chain=-
CREATE TABLE auth_refusal_window (
 id INTEGER PRIMARY KEY CHECK (id = 1),
 window_start BIGINT NOT NULL CHECK (window_start >= 0),
 state TEXT NOT NULL CHECK (octet_length(state) <= 16384)
);
INSERT INTO auth_refusal_window (id, window_start, state)
VALUES (1, 0, '{"counts":[0,0,0,0,0],"samples":[]}');
