-- +goose Up
-- All-projects member access rules (#1446, member-access-rules ADR D3 and the
-- owner decisions of 2026-11-20 and 2026-11-25). A rule's project axis gains a
-- mode: 'only' (the default, every existing rule) names its projects through
-- project items; 'all' names no project item and covers every project of its
-- org, including projects created later. The mode is stored, never expanded
-- into a project list.
--
-- An all-projects rule may narrow keys by folder path only, and a path means
-- the same in every project, so its folder items carry no project: project_id
-- becomes nullable, and only a folder item may leave it NULL. The composite
-- foreign keys are MATCH SIMPLE, so a NULL project_id skips them. Whether a
-- NULL-project item belongs to an all-projects rule is checked on every read
-- (domain.Rule.Validate): a mismatched row set reaches nothing.
ALTER TABLE rules ADD COLUMN project_mode TEXT NOT NULL DEFAULT 'only' CHECK (project_mode IN ('all', 'only'));
ALTER TABLE rule_items ALTER COLUMN project_id DROP NOT NULL;
ALTER TABLE rule_items ADD CONSTRAINT rule_items_project_bound CHECK (project_id IS NOT NULL OR axis = 'folder');
CREATE INDEX rules_org_project_mode ON rules (org_id, project_mode);
