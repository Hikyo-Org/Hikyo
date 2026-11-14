-- Incident ledger (incident-containment ADR, O5 option B; #1039 slice S2).
-- Mutable state kept apart from the append-only audit tables. Every statement
-- runs on the proof-free resolution surface AFTER the owning operation
-- authorized credential-reset at the incident's scope. ASCII only: multibyte
-- characters shift sqlite statement offsets.

-- A second open incident for the same scope conflicts on the partial unique
-- index and inserts nothing: opening is idempotent.
-- hikyo:reason Opening an incident writes its one ledger row after the operation authorized credential-reset at the incident scope; the partial unique index makes a repeated open insert nothing.
-- hikyo:authn-resolution
-- name: InsertIncident :execrows
INSERT INTO incidents (id, scope_kind, scope_id, org_id, status, opened_by, opened_at, row_version)
VALUES (sqlc.arg(id), sqlc.arg(scope_kind), sqlc.arg(scope_id), sqlc.arg(org_id), 'open', sqlc.arg(opened_by), sqlc.arg(opened_at), 1)
ON CONFLICT DO NOTHING;

-- hikyo:reason Reading an incident by its generated id happens after the operation authorized credential-reset at the incident scope, which the caller compares to the row.
-- hikyo:authn-resolution
-- name: GetIncident :one
SELECT id, scope_kind, scope_id, org_id, status, opened_by, opened_at, closed_by, closed_at, row_version
FROM incidents WHERE id = sqlc.arg(id);

-- hikyo:reason Re-opening an incident reads the one open row of the addressed scope after the operation authorized credential-reset at that scope.
-- hikyo:authn-resolution
-- name: GetOpenIncidentForScope :one
SELECT id, scope_kind, scope_id, org_id, status, opened_by, opened_at, closed_by, closed_at, row_version
FROM incidents WHERE scope_kind = sqlc.arg(scope_kind) AND scope_id = sqlc.arg(scope_id) AND status = 'open';

-- hikyo:reason Closing compare-and-sets the open incident row after the operation authorized credential-reset at the incident scope; a closed incident never reopens.
-- hikyo:authn-resolution
-- name: CloseIncident :execrows
UPDATE incidents
SET status = sqlc.arg(status), closed_by = sqlc.arg(closed_by), closed_at = sqlc.arg(closed_at), row_version = row_version + 1
WHERE id = sqlc.arg(id) AND row_version = sqlc.arg(expected_version) AND status = 'open';

-- A repeated enumeration inserts only the objects the ledger does not hold yet
-- and never touches an existing outcome.
-- hikyo:reason Enumeration writes one pending or out-of-scope row per affected object after the operation authorized credential-reset at the incident scope; the unique key makes a repeat insert nothing and keeps the recorded outcome.
-- hikyo:authn-resolution
-- name: InsertIncidentItem :execrows
INSERT INTO incident_items (id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, row_version, created_at, updated_at)
SELECT sqlc.arg(id), incidents.id, sqlc.arg(owner_function), sqlc.arg(object_kind), sqlc.arg(object_id), sqlc.arg(scope), sqlc.arg(outcome), sqlc.arg(reason), 1, sqlc.arg(at), sqlc.arg(at)
FROM incidents WHERE incidents.id = sqlc.arg(incident_id) AND incidents.status = 'open'
ON CONFLICT DO NOTHING;

-- Keyset paging in id order keeps every read bounded and stable.
-- hikyo:reason The ledger read pages one incident's items by id after the operation authorized credential-reset at the incident scope, bounded by the limit.
-- hikyo:authn-resolution
-- name: ListIncidentItems :many
SELECT id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, attested_by, attested_at, row_version, created_at, updated_at
FROM incident_items
WHERE incident_id = sqlc.arg(incident_id) AND id > sqlc.arg(after_id)
ORDER BY id
LIMIT sqlc.arg(row_limit);

-- hikyo:reason Resume pages the open items of one incident (pending, unknown, blocked) by id after the operation authorized credential-reset at the incident scope, bounded by the limit.
-- hikyo:authn-resolution
-- name: ListOpenIncidentItems :many
SELECT id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, attested_by, attested_at, row_version, created_at, updated_at
FROM incident_items
WHERE incident_id = sqlc.arg(incident_id) AND id > sqlc.arg(after_id) AND outcome IN ('pending', 'unknown', 'blocked')
ORDER BY id
LIMIT sqlc.arg(row_limit);

-- hikyo:reason Closing and the opened event count one incident's items by outcome after the operation authorized credential-reset at the incident scope.
-- hikyo:authn-resolution
-- name: CountIncidentItemsByOutcome :many
SELECT outcome, COUNT(*) AS item_count FROM incident_items WHERE incident_id = sqlc.arg(incident_id) GROUP BY outcome ORDER BY outcome;

-- Compare-and-set on the row version, and only while the incident is open and
-- the item is not yet terminal: a stale writer, a closed incident and a repeated
-- result each change no row.
-- hikyo:reason A recorded outcome compare-and-sets one item of an open incident from an open outcome after the operation authorized credential-reset at the incident scope; a stale version or a terminal item changes nothing.
-- hikyo:authn-resolution
-- name: TransitionIncidentItem :execrows
UPDATE incident_items
SET outcome = sqlc.arg(outcome), reason = sqlc.arg(reason),
    attested_by = sqlc.narg(attested_by), attested_at = sqlc.narg(attested_at),
    row_version = row_version + 1, updated_at = sqlc.arg(at)
WHERE incident_items.id = sqlc.arg(id) AND incident_items.incident_id = sqlc.arg(incident_id) AND incident_items.row_version = sqlc.arg(expected_version)
  AND incident_items.outcome IN ('pending', 'unknown', 'blocked')
  AND incident_items.incident_id IN (SELECT incidents.id FROM incidents WHERE incidents.id = sqlc.arg(incident_id) AND incidents.status = 'open');

-- hikyo:reason Resume re-reads one item by its generated id inside its incident after the operation authorized credential-reset at the incident scope, so a stale list never decides the next step.
-- hikyo:authn-resolution
-- name: GetIncidentItem :one
SELECT id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, attested_by, attested_at, row_version, created_at, updated_at
FROM incident_items WHERE incident_id = sqlc.arg(incident_id) AND id = sqlc.arg(id);
