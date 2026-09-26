-- +goose Up
-- Generic file synchronization (#164): the server half of a client-pull file
-- destination.
--
-- hikyo:table file_targets class=environment chain=org_id,project_id
-- hikyo:table file_target_keys class=environment chain=org_id,project_id

-- A file target binds one environment, one key selection and one workload
-- service account. It carries NO destination: the path, format, ownership and
-- mode live only in the client's local config, so the server never receives
-- or stores a host path. There is no outbox job, ledger or provider lease; the
-- client pulls through the delivery surface with `target=<id>`.
--
-- The service account is bound to at most one target (UNIQUE), and a bound
-- account's delivery is narrowed to the target's keys, which is what makes
-- the identity target-scoped. Deleting the service account deletes the target
-- (cascade), so an unbound account can never silently regain the wider
-- environment delivery the binding withheld.
--
-- The report columns are the client's last value-free assertion: the revision
-- it applied, its keyed generation stamp, a closed state, the target
-- generation it rendered and its own clock. `received_at` is the server clock.
CREATE TABLE file_targets (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    name TEXT NOT NULL,
    service_account_id TEXT NOT NULL UNIQUE,
    principal_id TEXT NOT NULL UNIQUE,
    generation BIGINT NOT NULL CHECK (generation >= 1),
    authority_principal_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    report_state TEXT CHECK (report_state IN ('applied', 'current', 'offline', 'refused', 'failed')),
    report_revision BIGINT CHECK (report_revision >= 0),
    report_stamp TEXT,
    report_generation BIGINT CHECK (report_generation >= 1),
    reported_at TIMESTAMPTZ,
    received_at TIMESTAMPTZ,
    CHECK ((report_state IS NULL) = (received_at IS NULL)),
    UNIQUE (org_id, project_id, environment_id, name),
    UNIQUE (org_id, project_id, environment_id, id),
    FOREIGN KEY (org_id, project_id, environment_id)
        REFERENCES environments (org_id, project_id, id) ON DELETE CASCADE,
    FOREIGN KEY (service_account_id) REFERENCES service_accounts (id) ON DELETE CASCADE,
    FOREIGN KEY (principal_id) REFERENCES principals (id) ON DELETE CASCADE
);

CREATE INDEX file_targets_project ON file_targets (org_id, project_id);

-- Membership by immutable key id, resolved from the #157 key selection at
-- save. Deleting a key removes it from every selection; the client then
-- refuses by name the file that still lists it, rather than rendering less.
CREATE TABLE file_target_keys (
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    key_id TEXT NOT NULL,
    PRIMARY KEY (target_id, key_id),
    FOREIGN KEY (org_id, project_id, environment_id, target_id)
        REFERENCES file_targets (org_id, project_id, environment_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, project_id, key_id) REFERENCES keys (org_id, project_id, id) ON DELETE CASCADE
);
