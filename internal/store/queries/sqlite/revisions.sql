-- Revisions, drafts and publishing (#51). Tenant-scoped statements: the
-- reserved chain parameters are bound by the store's binding layer from proof
-- fields only - never from caller arguments; the SQL predicate analyzer
-- enforces the conjunct shape.
--
-- `environment_id` is an ordinary column on all four tables (see the
-- migration), and every environment-addressed statement below binds it from
-- the proof's resolved chain. The project-scoped statements are the ones that
-- must span environments: publish reads the publisher's whole working state
-- across the project before it knows which environments it touches, the matrix
-- signals are a project-wide question, and a key delete cascades across every
-- environment at once.

-- name: InsertPendingChange :exec
INSERT INTO pending_changes (
    id, org_id, project_id, environment_id, key_id, owner_id,
    operation, ciphertext, staged_from_revision, staged_from_entry, created_at, source, secret, material_secret
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- DeletePendingChangeForCell collects the superseded version. Editing a cell
-- mints a new version id rather than mutating the old row, and the old row is
-- removed in the same transaction: only the latest version per (owner, key,
-- environment) is publishable, so keeping the predecessor would store draft
-- material nothing may ever publish.
-- name: DeletePendingChangeForCell :execrows
DELETE FROM pending_changes
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND key_id = ? AND owner_id = ?;

-- name: DeletePendingChangeByID :execrows
DELETE FROM pending_changes
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND id = ?;

-- name: DeletePendingChangesForEnvironment :execrows
DELETE FROM pending_changes
WHERE org_id = ? AND project_id = ? AND environment_id = ?;

-- DeletePendingChangesForKey is the key-delete cascade: deleting a key
-- invalidates every pending change referencing it, so a publish naming one of
-- those versions is refused loudly instead of resurrecting a key the schema no
-- longer declares.
-- name: DeletePendingChangesForKey :execrows
DELETE FROM pending_changes
WHERE org_id = ? AND project_id = ? AND key_id = ?;

-- ListPendingChangesForOwner is the publish path's read: a publish carries
-- ONLY the publisher's own pending changes, so the query that returns draft
-- material is keyed on the owner and there is no statement that hands one
-- principal another's ciphertext.
-- name: ListPendingChangesForOwner :many
SELECT id, org_id, project_id, environment_id, key_id, owner_id,
       operation, ciphertext, staged_from_revision, staged_from_entry, created_at, source, secret, material_secret
FROM pending_changes
WHERE org_id = ? AND project_id = ? AND owner_id = ?
ORDER BY environment_id, key_id;

-- ListPendingChangesForOwnerInEnvironment is the preview read. Owner and
-- environment are both predicates in SQL, so the preview cannot hand one
-- principal another's ciphertext or material from another environment.
-- name: ListPendingChangesForOwnerInEnvironment :many
SELECT id, org_id, project_id, environment_id, key_id, owner_id,
       operation, ciphertext, staged_from_revision, staged_from_entry, created_at, source, secret, material_secret
FROM pending_changes
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND owner_id = ?
ORDER BY key_id;

-- ListPendingMarkers is the matrix signal's read and the group-closure
-- collision check's read. It returns NO ciphertext: what another principal's
-- draft may disclose is write-presence and nothing else, and the cheapest way
-- to hold that rule is a statement that cannot carry the material.
-- ListPendingChangesForOwnerInEnvironmentPage is the MCP-bounded keyset read
-- (#629). (env, key, owner) is UNIQUE, so key_id is a stable single-column
-- cursor for one owner's drafts in one environment. No JOIN: the caller resolves
-- each page key's name and classification under the same key.list authorization.
-- name: ListPendingChangesForOwnerInEnvironmentPage :many
SELECT id, org_id, project_id, environment_id, key_id, owner_id,
       operation, ciphertext, staged_from_revision, staged_from_entry, created_at, source, secret, material_secret
FROM pending_changes
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND owner_id = sqlc.arg(owner_id)
  AND key_id > sqlc.arg(after_key_id)
ORDER BY key_id LIMIT sqlc.arg(page_limit);

-- name: ListPendingMarkers :many
SELECT id, environment_id, key_id, owner_id, operation
FROM pending_changes
WHERE org_id = ? AND project_id = ?
ORDER BY environment_id, key_id, owner_id;

-- name: InsertSnapshot :exec
INSERT INTO snapshots (
    id, org_id, project_id, environment_id, revision,
    schema_revision, published_by, published_at, parameter_contract
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- GetLatestSnapshot is the delivery-shaped read: a workload fetch defaults to
-- the latest published snapshot for its (project, environment).
-- name: GetLatestSnapshot :one
SELECT id, org_id, project_id, environment_id, revision, schema_revision,
       published_by, published_at, payload_present, collected_at, collected_policy, parameter_contract
FROM snapshots
WHERE org_id = ? AND project_id = ? AND environment_id = ?
ORDER BY revision DESC
LIMIT 1;

-- name: GetSnapshotByRevision :one
SELECT id, org_id, project_id, environment_id, revision, schema_revision,
       published_by, published_at, payload_present, collected_at, collected_policy, parameter_contract
FROM snapshots
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND revision = ?;

-- ProjectSnapshotRevisions returns the latest published revision per
-- environment across the project, the definitions plan/apply pin (#70). The
-- aggregate transfers one row per environment rather than the lifetime history;
-- the analyzer proves the chain predicate through GROUP BY, which only groups
-- rows the chain conjuncts already confined.
-- name: ProjectSnapshotRevisions :many
SELECT environment_id, CAST(MAX(revision) AS INTEGER) AS revision FROM snapshots
WHERE org_id = ? AND project_id = ?
GROUP BY environment_id;

-- ListSnapshots is the environment's whole revision header set, newest first:
-- the pin retention-consequence read, which ranks one snapshot against every
-- sibling. Header projection only; the parameter contract has its own read.
-- name: ListSnapshots :many
SELECT id, org_id, project_id, environment_id, revision, schema_revision,
       published_by, published_at, payload_present, collected_at, collected_policy
FROM snapshots
WHERE org_id = ? AND project_id = ? AND environment_id = ?
ORDER BY revision DESC;

-- ListSnapshotsPage is the bounded keyset read behind revision history (#629).
-- revision is UNIQUE and monotonic per environment, so it is a stable
-- single-column cursor in descending order: the statement fetches strictly
-- below the last returned revision and never materializes the whole history to
-- slice a limit afterwards. It is a metadata projection: the parameter
-- contract is payload and has its own point read.
-- name: ListSnapshotsPage :many
SELECT id, org_id, project_id, environment_id, revision, schema_revision,
       published_by, published_at, payload_present, collected_at, collected_policy
FROM snapshots
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id)
  AND revision < sqlc.arg(before_revision)
ORDER BY revision DESC LIMIT sqlc.arg(page_limit);

-- name: DeleteSnapshotsForEnvironment :execrows
DELETE FROM snapshots
WHERE org_id = ? AND project_id = ? AND environment_id = ?;

-- name: InsertSnapshotEntry :exec
INSERT INTO snapshot_entries (
    id, org_id, project_id, environment_id, snapshot_id,
    key_id, key_name, classification, ciphertext, value_entry_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListSnapshotEntries :many
SELECT id, org_id, project_id, environment_id, snapshot_id,
       key_id, key_name, classification, ciphertext, value_entry_id
FROM snapshot_entries
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND snapshot_id = ?
ORDER BY key_name;

-- RecordSecretValueOccurrence is idempotent on the value-entry primary key:
-- a publish records each secret occurrence it materializes without first
-- enumerating the environment's lifetime occurrence history.
-- name: RecordSecretValueOccurrence :exec
INSERT INTO secret_value_occurrences (
    value_entry_id, org_id, project_id, environment_id
)
VALUES (?, ?, ?, ?) ON CONFLICT (value_entry_id) DO NOTHING;

-- CountSecretValueOccurrence is the point membership read of the sticky
-- sensitivity lineage: one value entry, in the proof's environment. Callers
-- ask about the handful of entries they hold, never the lifetime history.
-- name: CountSecretValueOccurrence :one
SELECT COUNT(*) FROM secret_value_occurrences
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND value_entry_id = ?;

-- name: DeleteSecretValueOccurrencesForEnvironment :execrows
DELETE FROM secret_value_occurrences
WHERE org_id = ? AND project_id = ? AND environment_id = ?;
-- name: DeleteSnapshotEntriesForEnvironment :execrows
DELETE FROM snapshot_entries
WHERE org_id = ? AND project_id = ? AND environment_id = ?;

-- name: InsertRevisionKeyChange :exec
INSERT INTO revision_key_changes (
    org_id, project_id, environment_id, revision, key_id, key_name, change
) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListRevisionKeyChanges :many
SELECT org_id, project_id, environment_id, revision, key_id, key_name, change
FROM revision_key_changes
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND revision = ?
ORDER BY key_name;

-- ListRevisionKeyChangesInRange is the history page's lineage read: every
-- change row of the revisions in [min_revision, max_revision], newest revision
-- first, so one statement serves a whole page instead of one per revision.
-- name: ListRevisionKeyChangesInRange :many
SELECT org_id, project_id, environment_id, revision, key_id, key_name, change
FROM revision_key_changes
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id)
  AND revision >= sqlc.arg(min_revision) AND revision <= sqlc.arg(max_revision)
ORDER BY revision DESC, key_name;

-- name: GetRevisionPinForWorkload :one
SELECT id, org_id, project_id, environment_id, workload_principal_id,
       snapshot_id, revision, authority_principal_id, expires_at, created_at,
       authorized_at, history_authorized, schema_override
FROM revision_pins
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND workload_principal_id = ?;

-- name: ListRevisionPins :many
SELECT id, org_id, project_id, environment_id, workload_principal_id,
       snapshot_id, revision, authority_principal_id, expires_at, created_at,
       authorized_at, history_authorized, schema_override
FROM revision_pins
WHERE org_id = ? AND project_id = ? AND environment_id = ?
ORDER BY workload_principal_id;

-- name: CountRevisionPinsForProject :one
SELECT COUNT(*) FROM revision_pins WHERE org_id = ? AND project_id = ?;

-- name: InsertRevisionPin :exec
INSERT INTO revision_pins (
    id, org_id, project_id, environment_id, workload_principal_id,
    snapshot_id, revision, authority_principal_id, expires_at, created_at,
    authorized_at, history_authorized, schema_override
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: DeleteRevisionPin :execrows
DELETE FROM revision_pins
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND workload_principal_id = ?;

-- name: DeleteRevisionPinsForEnvironment :execrows
DELETE FROM revision_pins
WHERE org_id = ? AND project_id = ? AND environment_id = ?;

-- name: DeleteRevisionKeyChangesForEnvironment :execrows
DELETE FROM revision_key_changes
WHERE org_id = ? AND project_id = ? AND environment_id = ?;

-- name: CountPendingChangesForProject :one
SELECT COUNT(*) FROM pending_changes
WHERE org_id = ? AND project_id = ?;

-- name: CountPendingChangeForCell :one
SELECT COUNT(*) FROM pending_changes
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND key_id = ? AND owner_id = ?;
-- Reencrypt walk (#75/#187): page and re-seal project_field ciphertext in place.
-- name: ListSnapshotEntriesForReencrypt :many
SELECT id, environment_id, snapshot_id, key_id, ciphertext FROM snapshot_entries
WHERE org_id = ? AND project_id = ? AND id > ? ORDER BY id LIMIT ?;

-- name: ReencryptSnapshotEntry :execrows
UPDATE snapshot_entries SET ciphertext = ?
WHERE org_id = ? AND project_id = ? AND id = ? AND ciphertext = ?;

-- pending_changes ciphertext is NULL for an `unset` draft; skip those rows.
-- name: ListPendingForReencrypt :many
SELECT id, environment_id, key_id, ciphertext FROM pending_changes
WHERE org_id = ? AND project_id = ? AND id > ?
ORDER BY id LIMIT ?;

-- name: ReencryptPendingChange :execrows
UPDATE pending_changes SET ciphertext = ?
WHERE org_id = ? AND project_id = ? AND id = ? AND ciphertext = ?;

-- SumSnapshotPayloadForProject totals the ciphertext bytes of a project's
-- published snapshot entries across every environment and revision. Paired with
-- SumValuePayloadForProject, it is the other half of the per-project storage
-- high-water accounting (ops-spec section 8 / section 141). Fully chain-scoped,
-- so no annotation is needed.
-- name: SumSnapshotPayloadForProject :one
SELECT CAST(COALESCE(SUM(LENGTH(ciphertext)), 0) AS INTEGER) FROM snapshot_entries
WHERE org_id = ? AND project_id = ?;

-- SumSnapshotPayloadByProject groups the published snapshot-entry ciphertext
-- bytes by owning project across the whole instance -- the operator storage
-- surface (doctor warn, metric). Cross-tenant by definition, so it is annotated
-- instance-scoped and content-pinned.
-- hikyo:instance-scoped
-- name: SumSnapshotPayloadByProject :many
-- Project sizes only, never whole encrypted payloads in a sorter.
-- LIMIT -1 OFFSET 0 prevents flattening this projection into the GROUP BY:
-- the temporary sorter must retain integer sizes, not ciphertext/contract blobs.
WITH payload_sizes AS (
    SELECT org_id, project_id, LENGTH(ciphertext) AS bytes FROM snapshot_entries
    UNION ALL
    SELECT org_id, project_id, LENGTH(CAST(parameter_contract AS BLOB)) AS bytes FROM snapshots
    WHERE parameter_contract <> '{}' LIMIT -1 OFFSET 0
)
SELECT org_id, project_id, CAST(COALESCE(SUM(bytes), 0) AS INTEGER) AS bytes
FROM payload_sizes GROUP BY org_id, project_id;

-- name: SumSnapshotContractForProject :one
SELECT CAST(COALESCE(SUM(CASE WHEN parameter_contract = '{}' THEN 0 ELSE LENGTH(CAST(parameter_contract AS BLOB)) END), 0) AS INTEGER) FROM snapshots
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id);

-- name: GetSnapshotParameterContract :one
SELECT parameter_contract FROM snapshots
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND id = sqlc.arg(snapshot_id);
