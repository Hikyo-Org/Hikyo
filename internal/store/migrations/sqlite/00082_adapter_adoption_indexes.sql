-- +goose Up
-- Bound each plan/worker adoption lookup to its full chain and ledger name.
-- Evidence remains retained; these indexes do not change consent or outcomes.
CREATE INDEX adapter_conflicts_adoption_lookup ON adapter_conflicts
    (target_id, org_id, project_id, environment_id, target_generation,
     destination_id, repository_id, surface, UPPER(effective_name), julianday(adopted_at) DESC, id DESC)
    WHERE adopted_at IS NOT NULL AND julianday(adopted_at) IS NOT NULL;
CREATE INDEX adapter_effects_adoption_lookup ON adapter_effects
    (target_id, org_id, project_id, environment_id, surface, UPPER(effective_name), julianday(finished_at))
    WHERE disposition IN ('create','update') AND outcome='success';

-- +goose Down
DROP INDEX adapter_effects_adoption_lookup;
DROP INDEX adapter_conflicts_adoption_lookup;
