-- +goose Up
ALTER TABLE adapter_effects ADD COLUMN finding TEXT NOT NULL DEFAULT ''
    CHECK (finding IN ('', 'possible_capture', 'owned_missing', 'crash_window'));

-- Preserve existing findings without depending on audit retention for future reads.
UPDATE adapter_effects SET finding=COALESCE((
    SELECT json_extract(a.payload,'$.finding') FROM audit_tenant_events a
    WHERE a.id=adapter_effects.outcome_audit_id
      AND a.org_id=adapter_effects.org_id
      AND json_extract(a.payload,'$.finding') IN ('possible_capture','owned_missing','crash_window')
), '');
