-- +goose Up
-- Incident ledger (incident-containment ADR, O5 option B; #1039 slice S2). One
-- incident row and one row per affected object, each with an outcome from a
-- closed set. This is mutable state, kept apart from the append-only audit
-- tables as auth_refusal_window is; every transition is also an instance-trail
-- event whose correlation id is the incident id.
--
-- Rows hold Hikyo-generated identifiers and closed vocabulary only: no secret,
-- no free text, and no attacker-supplied string (an external issuer or subject,
-- a username, an address, a remote or adapter name never appears here). Scope
-- and object ids carry no foreign key on purpose: the ledger is evidence and
-- must outlive the object it names (an erased account stays a pseudonymous
-- reference, ADR - Interaction with the privacy-restriction safeguards).
-- hikyo:table incidents class=authn chain=-
CREATE TABLE incidents (
    id TEXT PRIMARY KEY,
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('account', 'org', 'project', 'adapter')),
    scope_id TEXT NOT NULL CHECK (scope_id <> ''),
    org_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed', 'closed_with_residuals')),
    opened_by TEXT NOT NULL CHECK (opened_by <> ''),
    opened_at TEXT NOT NULL,
    closed_by TEXT,
    closed_at TEXT,
    row_version BIGINT NOT NULL DEFAULT 1,
    CHECK ((status = 'open') = (closed_at IS NULL))
);
-- At most one open incident per scope: this is what makes opening idempotent.
CREATE UNIQUE INDEX incidents_one_open_per_scope ON incidents (scope_kind, scope_id) WHERE status = 'open';

-- hikyo:table incident_items class=authn chain=-
CREATE TABLE incident_items (
    id TEXT PRIMARY KEY,
    incident_id TEXT NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    owner_function TEXT NOT NULL CHECK (owner_function <> ''),
    object_kind TEXT NOT NULL CHECK (object_kind <> ''),
    object_id TEXT NOT NULL CHECK (object_id <> ''),
    scope TEXT NOT NULL,
    outcome TEXT NOT NULL DEFAULT 'pending' CHECK (outcome IN ('pending', 'done', 'unknown', 'attested', 'blocked', 'out_of_scope')),
    reason TEXT NOT NULL DEFAULT '' CHECK (reason IN ('', 'needs_another_operator', 'owner_unconfirmed', 'owner_failed', 'enumeration_truncated', 'no_revoke_owner', 'delivered_plaintext', 'cache', 'running_process', 'external_session', 'issued_certificate_in_use', 'upstream_not_observable')),
    attested_by TEXT,
    attested_at TEXT,
    row_version BIGINT NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (incident_id, owner_function, object_kind, object_id),
    CHECK ((outcome = 'attested') = (attested_by IS NOT NULL AND attested_at IS NOT NULL)),
    CHECK (outcome <> 'out_of_scope' OR reason IN ('no_revoke_owner', 'delivered_plaintext', 'cache', 'running_process', 'external_session', 'issued_certificate_in_use'))
);
CREATE INDEX incident_items_outcome ON incident_items (incident_id, outcome, id);

-- +goose Down
DROP TABLE incident_items;
DROP TABLE incidents;
