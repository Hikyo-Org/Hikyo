package authn

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// The member access rule surface (member-access-rules ADR). It lives on the
// enumerated resolution surface for the same reason grants do: authorize()
// reads rules to mint a proof. Every mutating method takes the principal-row
// lock (the lint grantlock analyzer) and is named in lint.ResolutionSurfaceWriters.
//
// Every read parses and validates: a row set that does not form a valid rule
// is dropped, so the worst a corrupt row can do is deny.

// Rule item axes as stored.
const (
	ruleAxisProject = "project"
	ruleAxisEnv     = "env"
	ruleAxisKey     = "key"
	ruleAxisFolder  = "folder"
)

// StoredRuleItem is one selector row, with its id so a deletion path can
// prune exactly the item naming the object being deleted.
type StoredRuleItem struct {
	ID      string
	Axis    string
	Project domain.ProjectID
	Env     domain.EnvID
	KeyID   string
	Folder  string
}

// StoredRule is one rule as stored, ungated, for revocation and pruning.
type StoredRule struct {
	Rule  domain.Rule
	Items []StoredRuleItem
}

// ruleRow is one engine-neutral row of a rule read.
type ruleRow struct {
	id, principal, capability, org, projectMode, envMode, keyMode string
	item                                                          StoredRuleItem
}

// ruleProjectAll is the stored all-projects project mode (#1446).
const ruleProjectAll = "all"

// foldRules groups rows by rule id (rows arrive ordered by rule id) and keeps
// only the rules that validate. A row set with an unknown axis is dropped.
func foldRules(rows []ruleRow) []StoredRule {
	var out []StoredRule
	var cur *StoredRule
	bad := false
	flush := func() {
		if cur != nil && !bad && cur.Rule.Validate() == nil {
			out = append(out, *cur)
		}
	}
	for _, row := range rows {
		if cur == nil || cur.Rule.ID != row.id {
			flush()
			bad = false
			cur = &StoredRule{Rule: domain.Rule{
				ID: row.id, Principal: domain.PrincipalID(row.principal),
				Capability: domain.Capability(row.capability), Org: domain.OrgID(row.org),
				Where: domain.Where{
					AllProjects: row.projectMode == ruleProjectAll,
					EnvMode:     domain.AxisMode(row.envMode), KeyMode: domain.AxisMode(row.keyMode),
					Envs: map[domain.ProjectID][]domain.EnvID{}, Keys: map[domain.ProjectID][]domain.RuleKeyItem{},
				},
			}}
		}
		it := row.item
		w := &cur.Rule.Where
		switch it.Axis {
		case "":
			// An item-less rule (an all-projects rule without key items) reads
			// one LEFT JOIN row with no item.
			continue
		case ruleAxisProject:
			w.Projects = append(w.Projects, it.Project)
		case ruleAxisEnv:
			w.Envs[it.Project] = append(w.Envs[it.Project], it.Env)
		case ruleAxisKey:
			w.Keys[it.Project] = append(w.Keys[it.Project], domain.RuleKeyItem{KeyID: it.KeyID})
		case ruleAxisFolder:
			w.Keys[it.Project] = append(w.Keys[it.Project], domain.RuleKeyItem{Folder: it.Folder, IsFolder: true})
		default:
			bad = true
		}
		cur.Items = append(cur.Items, it)
	}
	flush()
	return out
}

func rulesOf(stored []StoredRule) []domain.Rule {
	out := make([]domain.Rule, 0, len(stored))
	for _, s := range stored {
		out = append(out, s.Rule)
	}
	return out
}

// Rules returns the principal's valid rules for formula evaluation, under the
// same gates as Grants (active, reconciled) plus human kind. A recovery
// resolver bound to a schema older than the rule tables answers none.
func (r *Resolver) Rules(ctx context.Context, p domain.PrincipalID) ([]domain.Rule, error) {
	return r.principalRules(ctx, p, false)
}

// MachineRules returns a machine principal's rules under the same gates as
// Rules. The caller applies domain.CheckMachineRule to every rule it uses.
func (r *Resolver) MachineRules(ctx context.Context, p domain.PrincipalID) ([]domain.Rule, error) {
	return r.principalRules(ctx, p, true)
}

func (r *Resolver) principalRules(ctx context.Context, p domain.PrincipalID, machine bool) ([]domain.Rule, error) {
	if r.historicalRecoveryBeforeRules {
		return nil, nil
	}
	var rows []ruleRow
	if r.sq != nil {
		var got []sqlitegen.ListRulesForPrincipalRow
		var err error
		switch {
		case r.historicalRecoveryBeforeContainment:
			// Source schemas 66 through 88 have no containment column.
			var old []sqlitegen.RecoveryListRulesBeforeContainmentRow
			old, err = r.sq.RecoveryListRulesBeforeContainment(ctx, string(p))
			for _, g := range old {
				got = append(got, ruleRowBeforeAllProjectsSQLite(g))
			}
		case r.historicalRecoveryBeforeAllProjects && machine:
			// Source schemas 89 through 98 have no rules.project_mode.
			var old []sqlitegen.RecoveryListMachineRulesBeforeAllProjectsRow
			old, err = r.sq.RecoveryListMachineRulesBeforeAllProjects(ctx, string(p))
			for _, g := range old {
				got = append(got, ruleRowBeforeAllProjectsSQLite(sqlitegen.RecoveryListRulesBeforeContainmentRow(g)))
			}
		case r.historicalRecoveryBeforeAllProjects:
			var old []sqlitegen.RecoveryListRulesBeforeAllProjectsRow
			old, err = r.sq.RecoveryListRulesBeforeAllProjects(ctx, string(p))
			for _, g := range old {
				got = append(got, ruleRowBeforeAllProjectsSQLite(sqlitegen.RecoveryListRulesBeforeContainmentRow(g)))
			}
		case machine:
			var machineRows []sqlitegen.ListMachineRulesForPrincipalRow
			machineRows, err = r.sq.ListMachineRulesForPrincipal(ctx, string(p))
			for _, g := range machineRows {
				got = append(got, sqlitegen.ListRulesForPrincipalRow(g))
			}
		default:
			got, err = r.sq.ListRulesForPrincipal(ctx, string(p))
		}
		if err != nil {
			return nil, err
		}
		for _, g := range got {
			rows = append(rows, ruleRow{id: g.ID, principal: string(p), capability: g.Capability, org: g.OrgID,
				projectMode: g.ProjectMode, envMode: g.EnvMode, keyMode: g.KeyMode,
				item: StoredRuleItem{Axis: g.Axis, Project: domain.ProjectID(g.ProjectID), Env: domain.EnvID(g.EnvID), KeyID: g.KeyID, Folder: g.FolderPath}})
		}
	} else {
		var got []pggen.ListRulesForPrincipalRow
		var err error
		switch {
		case r.historicalRecoveryBeforeContainment:
			// Source schemas 66 through 88 have no containment column.
			var old []pggen.RecoveryListRulesBeforeContainmentRow
			old, err = r.pg.RecoveryListRulesBeforeContainment(ctx, string(p))
			for _, g := range old {
				got = append(got, ruleRowBeforeAllProjectsPG(g))
			}
		case r.historicalRecoveryBeforeAllProjects && machine:
			// Source schemas 89 through 98 have no rules.project_mode.
			var old []pggen.RecoveryListMachineRulesBeforeAllProjectsRow
			old, err = r.pg.RecoveryListMachineRulesBeforeAllProjects(ctx, string(p))
			for _, g := range old {
				got = append(got, ruleRowBeforeAllProjectsPG(pggen.RecoveryListRulesBeforeContainmentRow(g)))
			}
		case r.historicalRecoveryBeforeAllProjects:
			var old []pggen.RecoveryListRulesBeforeAllProjectsRow
			old, err = r.pg.RecoveryListRulesBeforeAllProjects(ctx, string(p))
			for _, g := range old {
				got = append(got, ruleRowBeforeAllProjectsPG(pggen.RecoveryListRulesBeforeContainmentRow(g)))
			}
		case machine:
			var machineRows []pggen.ListMachineRulesForPrincipalRow
			machineRows, err = r.pg.ListMachineRulesForPrincipal(ctx, string(p))
			for _, g := range machineRows {
				got = append(got, pggen.ListRulesForPrincipalRow(g))
			}
		default:
			got, err = r.pg.ListRulesForPrincipal(ctx, string(p))
		}
		if err != nil {
			return nil, err
		}
		for _, g := range got {
			rows = append(rows, ruleRow{id: g.ID, principal: string(p), capability: g.Capability, org: g.OrgID,
				projectMode: g.ProjectMode, envMode: g.EnvMode, keyMode: g.KeyMode,
				item: StoredRuleItem{Axis: g.Axis, Project: domain.ProjectID(g.ProjectID), Env: domain.EnvID(g.EnvID), KeyID: g.KeyID, Folder: g.FolderPath}})
		}
	}
	return rulesOf(foldRules(rows)), nil
}

// ruleRowBeforeAllProjectsSQLite maps a historical rule row (a source schema
// before 99, which has no rules.project_mode) to the current row shape: every
// rule there names its projects, so each reads as 'only'.
func ruleRowBeforeAllProjectsSQLite(g sqlitegen.RecoveryListRulesBeforeContainmentRow) sqlitegen.ListRulesForPrincipalRow {
	return sqlitegen.ListRulesForPrincipalRow{ID: g.ID, Capability: g.Capability, OrgID: g.OrgID,
		ProjectMode: "only", EnvMode: g.EnvMode, KeyMode: g.KeyMode, Axis: g.Axis, ProjectID: g.ProjectID,
		EnvID: g.EnvID, KeyID: g.KeyID, FolderPath: g.FolderPath}
}

// ruleRowBeforeAllProjectsPG is the postgres twin of ruleRowBeforeAllProjectsSQLite.
func ruleRowBeforeAllProjectsPG(g pggen.RecoveryListRulesBeforeContainmentRow) pggen.ListRulesForPrincipalRow {
	return pggen.ListRulesForPrincipalRow{ID: g.ID, Capability: g.Capability, OrgID: g.OrgID,
		ProjectMode: "only", EnvMode: g.EnvMode, KeyMode: g.KeyMode, Axis: g.Axis, ProjectID: g.ProjectID,
		EnvID: g.EnvID, KeyID: g.KeyID, FolderPath: g.FolderPath}
}

// RulesForProject returns every valid rule of a human principal naming one
// project, for the folder-move widening census. It is deliberately wider than
// the chokepoint's lookup (no privacy or restore-epoch gate): the census must
// name everyone who could ever hold the gained access.
func (r *Resolver) RulesForProject(ctx context.Context, org, project string) ([]domain.Rule, error) {
	if r.historicalRecoveryBeforeRules {
		return nil, nil
	}
	var rows []ruleRow
	if r.sq != nil {
		var got []sqlitegen.ListRulesForProjectRow
		var err error
		if r.historicalRecoveryBeforeAllProjects {
			// Source schemas before 99 have no rules.project_mode: every rule is project-bound.
			var old []sqlitegen.RecoveryListRulesForProjectBeforeAllProjectsRow
			old, err = r.sq.RecoveryListRulesForProjectBeforeAllProjects(ctx, sqlitegen.RecoveryListRulesForProjectBeforeAllProjectsParams{OrgID: org, ProjectID: nullString(project)})
			for _, g := range old {
				got = append(got, sqlitegen.ListRulesForProjectRow{ID: g.ID, PrincipalID: g.PrincipalID, Capability: g.Capability, OrgID: g.OrgID,
					ProjectMode: "only", EnvMode: g.EnvMode, KeyMode: g.KeyMode, Axis: g.Axis, ProjectID: g.ProjectID,
					EnvID: g.EnvID, KeyID: g.KeyID, FolderPath: g.FolderPath})
			}
		} else {
			got, err = r.sq.ListRulesForProject(ctx, sqlitegen.ListRulesForProjectParams{OrgID: org, ProjectID: nullString(project)})
		}
		if err != nil {
			return nil, err
		}
		for _, g := range got {
			rows = append(rows, ruleRow{id: g.ID, principal: g.PrincipalID, capability: g.Capability, org: g.OrgID,
				projectMode: g.ProjectMode, envMode: g.EnvMode, keyMode: g.KeyMode,
				item: StoredRuleItem{Axis: g.Axis, Project: domain.ProjectID(g.ProjectID), Env: domain.EnvID(g.EnvID), KeyID: g.KeyID, Folder: g.FolderPath}})
		}
	} else {
		var got []pggen.ListRulesForProjectRow
		var err error
		if r.historicalRecoveryBeforeAllProjects {
			var old []pggen.RecoveryListRulesForProjectBeforeAllProjectsRow
			old, err = r.pg.RecoveryListRulesForProjectBeforeAllProjects(ctx, pggen.RecoveryListRulesForProjectBeforeAllProjectsParams{OrgID: org, ProjectID: pgText(project)})
			for _, g := range old {
				got = append(got, pggen.ListRulesForProjectRow{ID: g.ID, PrincipalID: g.PrincipalID, Capability: g.Capability, OrgID: g.OrgID,
					ProjectMode: "only", EnvMode: g.EnvMode, KeyMode: g.KeyMode, Axis: g.Axis, ProjectID: g.ProjectID,
					EnvID: g.EnvID, KeyID: g.KeyID, FolderPath: g.FolderPath})
			}
		} else {
			got, err = r.pg.ListRulesForProject(ctx, pggen.ListRulesForProjectParams{OrgID: org, ProjectID: pgText(project)})
		}
		if err != nil {
			return nil, err
		}
		for _, g := range got {
			rows = append(rows, ruleRow{id: g.ID, principal: g.PrincipalID, capability: g.Capability, org: g.OrgID,
				projectMode: g.ProjectMode, envMode: g.EnvMode, keyMode: g.KeyMode,
				item: StoredRuleItem{Axis: g.Axis, Project: domain.ProjectID(g.ProjectID), Env: domain.EnvID(g.EnvID), KeyID: g.KeyID, Folder: g.FolderPath}})
		}
	}
	return rulesOf(foldRules(rows)), nil
}

// GetRule reads one stored rule, ungated. A rule whose rows no longer form a
// valid rule (every only-item pruned) is returned with Valid false so a
// deletion path can remove it; ErrNotFound when no row exists.
func (r *Resolver) GetRule(ctx context.Context, id string) (StoredRule, bool, error) {
	var rows []ruleRow
	if r.sq != nil {
		got, err := r.sq.GetRule(ctx, id)
		if err != nil {
			return StoredRule{}, false, err
		}
		for _, g := range got {
			rows = append(rows, ruleRow{id: g.ID, principal: g.PrincipalID, capability: g.Capability, org: g.OrgID,
				projectMode: g.ProjectMode, envMode: g.EnvMode, keyMode: g.KeyMode,
				item: StoredRuleItem{ID: g.ItemID, Axis: g.Axis, Project: domain.ProjectID(g.ProjectID), Env: domain.EnvID(g.EnvID), KeyID: g.KeyID, Folder: g.FolderPath}})
		}
	} else {
		got, err := r.pg.GetRule(ctx, id)
		if err != nil {
			return StoredRule{}, false, err
		}
		for _, g := range got {
			rows = append(rows, ruleRow{id: g.ID, principal: g.PrincipalID, capability: g.Capability, org: g.OrgID,
				projectMode: g.ProjectMode, envMode: g.EnvMode, keyMode: g.KeyMode,
				item: StoredRuleItem{ID: g.ItemID, Axis: g.Axis, Project: domain.ProjectID(g.ProjectID), Env: domain.EnvID(g.EnvID), KeyID: g.KeyID, Folder: g.FolderPath}})
		}
	}
	if len(rows) == 0 {
		return StoredRule{}, false, domain.ErrNotFound
	}
	valid := foldRules(rows)
	if len(valid) == 1 {
		return valid[0], true, nil
	}
	// Invalid as stored: hand back the identity and items so the caller can
	// delete it, never a Where anything could evaluate.
	items := make([]StoredRuleItem, 0, len(rows))
	for _, row := range rows {
		if row.item.Axis != "" {
			items = append(items, row.item)
		}
	}
	first := rows[0]
	return StoredRule{Rule: domain.Rule{ID: first.id, Principal: domain.PrincipalID(first.principal),
		Capability: domain.Capability(first.capability), Org: domain.OrgID(first.org),
		Where: domain.Where{AllProjects: first.projectMode == ruleProjectAll}}, Items: items}, false, nil
}

// RuleIDsForEnvironment, RuleIDsForKey, RuleIDsForProject and RuleIDsForOrg
// are the deletion paths' censuses.
func (r *Resolver) RuleIDsForEnvironment(ctx context.Context, org, project, env string) ([]string, error) {
	if r.sq != nil {
		return r.sq.ListRuleIDsForEnvironment(ctx, sqlitegen.ListRuleIDsForEnvironmentParams{OrgID: org, ProjectID: nullString(project), EnvID: nullString(env)})
	}
	return r.pg.ListRuleIDsForEnvironment(ctx, pggen.ListRuleIDsForEnvironmentParams{OrgID: org, ProjectID: pgText(project), EnvID: pgText(env)})
}

func (r *Resolver) RuleIDsForKey(ctx context.Context, org, project, key string) ([]string, error) {
	if r.sq != nil {
		return r.sq.ListRuleIDsForKey(ctx, sqlitegen.ListRuleIDsForKeyParams{OrgID: org, ProjectID: nullString(project), KeyID: nullString(key)})
	}
	return r.pg.ListRuleIDsForKey(ctx, pggen.ListRuleIDsForKeyParams{OrgID: org, ProjectID: pgText(project), KeyID: pgText(key)})
}

func (r *Resolver) RuleIDsForProject(ctx context.Context, org, project string) ([]string, error) {
	if r.sq != nil {
		return r.sq.ListRuleIDsForProject(ctx, sqlitegen.ListRuleIDsForProjectParams{OrgID: org, ProjectID: nullString(project)})
	}
	return r.pg.ListRuleIDsForProject(ctx, pggen.ListRuleIDsForProjectParams{OrgID: org, ProjectID: pgText(project)})
}

// RuleOrgProjects lists the org's current project ids. lock takes them
// FOR SHARE on postgres for an all-projects rule write, so a concurrent
// deletion of the org's last project serializes with it.
func (r *Resolver) RuleOrgProjects(ctx context.Context, org string, lock bool) ([]domain.ProjectID, error) {
	var ids []string
	var err error
	switch {
	case r.sq != nil && lock:
		ids, err = r.sq.LockRuleOrgProjectIDs(ctx, org)
	case r.sq != nil:
		ids, err = r.sq.ListRuleOrgProjectIDs(ctx, org)
	case lock:
		ids, err = r.pg.LockRuleOrgProjectIDs(ctx, org)
	default:
		ids, err = r.pg.ListRuleOrgProjectIDs(ctx, org)
	}
	if err != nil {
		return nil, err
	}
	out := make([]domain.ProjectID, 0, len(ids))
	for _, id := range ids {
		out = append(out, domain.ProjectID(id))
	}
	return out, nil
}

// LockOrgProjectsForDelete lists the org's project ids for a project
// deletion or creation, FOR UPDATE on postgres, so concurrent deletions of the
// org's last projects, creations at an all-projects rule's expansion bound and
// an all-projects rule create serialize with it.
func (r *Resolver) LockOrgProjectsForDelete(ctx context.Context, org string) ([]domain.ProjectID, error) {
	var ids []string
	var err error
	if r.sq != nil {
		ids, err = r.sq.LockOrgProjectIDsForDelete(ctx, org)
	} else {
		ids, err = r.pg.LockOrgProjectIDsForDelete(ctx, org)
	}
	if err != nil {
		return nil, err
	}
	out := make([]domain.ProjectID, 0, len(ids))
	for _, id := range ids {
		out = append(out, domain.ProjectID(id))
	}
	return out, nil
}

// AllProjectsRulesInOrg lists the org's all-projects rules with their stored
// item counts: an org's last project cannot be deleted while one exists, and
// a project cannot be created that would list one past the v1 bounds.
func (r *Resolver) AllProjectsRulesInOrg(ctx context.Context, org string) ([]domain.RuleItemCount, error) {
	var out []domain.RuleItemCount
	if r.sq != nil {
		rows, err := r.sq.ListAllProjectsRuleItemCountsInOrg(ctx, org)
		for _, row := range rows {
			out = append(out, domain.RuleItemCount{ID: row.ID, Items: int(row.Items)})
		}
		return out, err
	}
	rows, err := r.pg.ListAllProjectsRuleItemCountsInOrg(ctx, org)
	for _, row := range rows {
		out = append(out, domain.RuleItemCount{ID: row.ID, Items: int(row.Items)})
	}
	return out, err
}

func (r *Resolver) RuleIDsForOrg(ctx context.Context, org string) ([]string, error) {
	if r.sq != nil {
		return r.sq.ListRuleIDsForOrg(ctx, org)
	}
	return r.pg.ListRuleIDsForOrg(ctx, org)
}

// CreateRule writes one validated rule and its items under the principal-row
// lock. newItemID mints the item ids.
func (r *Resolver) CreateRule(ctx context.Context, rule domain.Rule, createdBy domain.PrincipalID, at time.Time, newItemID func() (string, error)) error {
	if err := rule.Validate(); err != nil {
		return err
	}
	if rule.ID == "" || rule.Principal == "" || createdBy == "" {
		return fmt.Errorf("authn: rule is missing its id, principal or grantor")
	}
	if err := r.LockPrincipalRow(ctx, rule.Principal); err != nil {
		return err
	}
	if r.sq != nil {
		if err := r.sq.InsertRule(ctx, sqlitegen.InsertRuleParams{
			ID: rule.ID, PrincipalID: string(rule.Principal), Capability: string(rule.Capability), OrgID: string(rule.Org),
			ProjectMode: projectModeOf(rule.Where), EnvMode: string(rule.Where.EnvMode), KeyMode: string(rule.Where.KeyMode), CreatedBy: string(createdBy), CreatedAt: encodeTime(at),
		}); err != nil {
			return err
		}
	} else if err := r.pg.InsertRule(ctx, pggen.InsertRuleParams{
		ID: rule.ID, PrincipalID: string(rule.Principal), Capability: string(rule.Capability), OrgID: string(rule.Org),
		ProjectMode: projectModeOf(rule.Where), EnvMode: string(rule.Where.EnvMode), KeyMode: string(rule.Where.KeyMode), CreatedBy: string(createdBy), CreatedAt: pgTimestamp(at),
	}); err != nil {
		return err
	}
	for _, it := range itemsOf(rule.Where) {
		id, err := newItemID()
		if err != nil {
			return err
		}
		if r.sq != nil {
			err = r.sq.InsertRuleItem(ctx, sqlitegen.InsertRuleItemParams{
				ID: id, RuleID: rule.ID, OrgID: string(rule.Org), Axis: it.Axis, ProjectID: nullString(string(it.Project)),
				EnvID: nullString(string(it.Env)), KeyID: nullString(it.KeyID),
				FolderPath: sql.NullString{String: it.Folder, Valid: it.Axis == ruleAxisFolder},
			})
		} else {
			err = r.pg.InsertRuleItem(ctx, pggen.InsertRuleItemParams{
				ID: id, RuleID: rule.ID, OrgID: string(rule.Org), Axis: it.Axis, ProjectID: pgText(string(it.Project)),
				EnvID: pgText(string(it.Env)), KeyID: pgText(it.KeyID),
				FolderPath: pgtype.Text{String: it.Folder, Valid: it.Axis == ruleAxisFolder},
			})
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// projectModeOf is the stored project mode of a Where.
func projectModeOf(w domain.Where) string {
	if w.AllProjects {
		return ruleProjectAll
	}
	return "only"
}

// itemsOf flattens a Where into stored items, projects first. An
// all-projects Where stores only its folder items, with no project.
func itemsOf(w domain.Where) []StoredRuleItem {
	var out []StoredRuleItem
	if w.AllProjects {
		for _, k := range w.Keys[domain.AnyProject] {
			out = append(out, StoredRuleItem{Axis: ruleAxisFolder, Folder: k.Folder})
		}
		return out
	}
	for _, p := range w.Projects {
		out = append(out, StoredRuleItem{Axis: ruleAxisProject, Project: p})
	}
	for _, p := range w.Projects {
		for _, e := range w.Envs[p] {
			out = append(out, StoredRuleItem{Axis: ruleAxisEnv, Project: p, Env: e})
		}
		for _, k := range w.Keys[p] {
			if k.IsFolder {
				out = append(out, StoredRuleItem{Axis: ruleAxisFolder, Project: p, Folder: k.Folder})
			} else {
				out = append(out, StoredRuleItem{Axis: ruleAxisKey, Project: p, KeyID: k.KeyID})
			}
		}
	}
	return out
}

// DeleteRule removes a rule and its items under the principal-row lock,
// reporting whether the rule row existed.
func (r *Resolver) DeleteRule(ctx context.Context, id string, p domain.PrincipalID) (bool, error) {
	if err := r.LockPrincipalRow(ctx, p); err != nil {
		return false, err
	}
	if r.sq != nil {
		if _, err := r.sq.DeleteRuleItems(ctx, id); err != nil {
			return false, err
		}
		n, err := r.sq.DeleteRuleRow(ctx, sqlitegen.DeleteRuleRowParams{ID: id, PrincipalID: string(p)})
		return n == 1, err
	}
	if _, err := r.pg.DeleteRuleItems(ctx, id); err != nil {
		return false, err
	}
	n, err := r.pg.DeleteRuleRow(ctx, pggen.DeleteRuleRowParams{ID: id, PrincipalID: string(p)})
	return n == 1, err
}

// DeleteRuleItem prunes one selector item under the principal-row lock. Only
// deletion paths call it, for an item naming an object that is going away, so
// it never changes what the rule reaches among objects that still exist.
func (r *Resolver) DeleteRuleItem(ctx context.Context, ruleID, itemID string, p domain.PrincipalID) error {
	if err := r.LockPrincipalRow(ctx, p); err != nil {
		return err
	}
	if r.sq != nil {
		_, err := r.sq.DeleteRuleItem(ctx, sqlitegen.DeleteRuleItemParams{ID: itemID, RuleID: ruleID})
		return err
	}
	_, err := r.pg.DeleteRuleItem(ctx, pggen.DeleteRuleItemParams{ID: itemID, RuleID: ruleID})
	return err
}

// ResolveRuleKey reads the key a key-aware authorization addresses, by id or
// by name, inside the already-resolved chain. ErrNotFound when absent.
func (r *Resolver) ResolveRuleKey(ctx context.Context, org, project, id, name string) (domain.RuleKey, error) {
	if (id == "") == (name == "") {
		return domain.RuleKey{}, fmt.Errorf("authn: a key is resolved by exactly one of id or name")
	}
	var got domain.RuleKey
	var err error
	switch {
	case r.sq != nil && id != "":
		var row sqlitegen.ResolveKeyByIDRow
		row, err = r.sq.ResolveKeyByID(ctx, sqlitegen.ResolveKeyByIDParams{OrgID: org, ProjectID: project, ID: id})
		got = domain.RuleKey{ID: row.ID, Folder: row.FolderPath}
	case r.sq != nil:
		var row sqlitegen.ResolveKeyByNameRow
		row, err = r.sq.ResolveKeyByName(ctx, sqlitegen.ResolveKeyByNameParams{OrgID: org, ProjectID: project, Name: name})
		got = domain.RuleKey{ID: row.ID, Folder: row.FolderPath}
	case id != "":
		var row pggen.ResolveKeyByIDRow
		row, err = r.pg.ResolveKeyByID(ctx, pggen.ResolveKeyByIDParams{OrgID: org, ProjectID: project, ID: id})
		got = domain.RuleKey{ID: row.ID, Folder: row.FolderPath}
	default:
		var row pggen.ResolveKeyByNameRow
		row, err = r.pg.ResolveKeyByName(ctx, pggen.ResolveKeyByNameParams{OrgID: org, ProjectID: project, Name: name})
		got = domain.RuleKey{ID: row.ID, Folder: row.FolderPath}
	}
	if err != nil {
		return domain.RuleKey{}, notFoundOr(err)
	}
	return got, nil
}

// RuleLine is one rule on the listing surface, as stored and never evaluated:
// a listing renders what a manager may revoke, so it neither validates nor
// drops. Items holds only the addressed project's items on a project listing,
// where OtherProjects reports that the rule also names projects the listing
// does not show.
type RuleLine struct {
	ID            string
	Principal     domain.PrincipalID
	Capability    domain.Capability
	AllProjects   bool
	EnvMode       domain.AxisMode
	KeyMode       domain.AxisMode
	CreatedBy     domain.PrincipalID
	CreatedAt     time.Time
	Items         []StoredRuleItem
	OtherProjects bool
}

type ruleLineRow struct {
	id, principal, capability, projectMode, envMode, keyMode, createdBy string
	at                                                                  time.Time
	other                                                               bool
	item                                                                StoredRuleItem
}

// foldRuleLines groups rows (ordered by rule id) into lines.
func foldRuleLines(rows []ruleLineRow) []RuleLine {
	var out []RuleLine
	for _, row := range rows {
		if len(out) == 0 || out[len(out)-1].ID != row.id {
			out = append(out, RuleLine{
				ID: row.id, Principal: domain.PrincipalID(row.principal), Capability: domain.Capability(row.capability),
				AllProjects: row.projectMode == ruleProjectAll, EnvMode: domain.AxisMode(row.envMode), KeyMode: domain.AxisMode(row.keyMode),
				CreatedBy: domain.PrincipalID(row.createdBy), CreatedAt: row.at, OtherProjects: row.other,
			})
		}
		last := &out[len(out)-1]
		if row.item.Axis != "" {
			last.Items = append(last.Items, row.item)
		}
	}
	return out
}

// RuleLinesInOrg lists every stored rule in one org.
func (r *Resolver) RuleLinesInOrg(ctx context.Context, org string) ([]RuleLine, error) {
	var rows []ruleLineRow
	if r.sq != nil {
		got, err := r.sq.ListRuleLinesInOrg(ctx, org)
		if err != nil {
			return nil, err
		}
		for _, g := range got {
			at, err := decodeTime(g.CreatedAt)
			if err != nil {
				return nil, err
			}
			rows = append(rows, ruleLineRow{id: g.ID, principal: g.PrincipalID, capability: g.Capability,
				projectMode: g.ProjectMode, envMode: g.EnvMode, keyMode: g.KeyMode, createdBy: g.CreatedBy, at: at,
				item: StoredRuleItem{Axis: g.Axis, Project: domain.ProjectID(g.ProjectID), Env: domain.EnvID(g.EnvID), KeyID: g.KeyID, Folder: g.FolderPath}})
		}
	} else {
		got, err := r.pg.ListRuleLinesInOrg(ctx, org)
		if err != nil {
			return nil, err
		}
		for _, g := range got {
			rows = append(rows, ruleLineRow{id: g.ID, principal: g.PrincipalID, capability: g.Capability,
				projectMode: g.ProjectMode, envMode: g.EnvMode, keyMode: g.KeyMode, createdBy: g.CreatedBy, at: g.CreatedAt.Time,
				item: StoredRuleItem{Axis: g.Axis, Project: domain.ProjectID(g.ProjectID), Env: domain.EnvID(g.EnvID), KeyID: g.KeyID, Folder: g.FolderPath}})
		}
	}
	return foldRuleLines(rows), nil
}

// RuleLinesInProject lists every stored rule naming one project, with only
// that project's items.
func (r *Resolver) RuleLinesInProject(ctx context.Context, org, project string) ([]RuleLine, error) {
	var rows []ruleLineRow
	if r.sq != nil {
		got, err := r.sq.ListRuleLinesInProject(ctx, sqlitegen.ListRuleLinesInProjectParams{OrgID: org, ProjectID: nullString(project)})
		if err != nil {
			return nil, err
		}
		for _, g := range got {
			at, err := decodeTime(g.CreatedAt)
			if err != nil {
				return nil, err
			}
			rows = append(rows, ruleLineRow{id: g.ID, principal: g.PrincipalID, capability: g.Capability,
				projectMode: g.ProjectMode, envMode: g.EnvMode, keyMode: g.KeyMode, createdBy: g.CreatedBy, at: at, other: g.OtherItems > 0,
				item: StoredRuleItem{Axis: g.Axis, Project: domain.ProjectID(g.ProjectID), Env: domain.EnvID(g.EnvID), KeyID: g.KeyID, Folder: g.FolderPath}})
		}
	} else {
		got, err := r.pg.ListRuleLinesInProject(ctx, pggen.ListRuleLinesInProjectParams{OrgID: org, ProjectID: pgText(project)})
		if err != nil {
			return nil, err
		}
		for _, g := range got {
			rows = append(rows, ruleLineRow{id: g.ID, principal: g.PrincipalID, capability: g.Capability,
				projectMode: g.ProjectMode, envMode: g.EnvMode, keyMode: g.KeyMode, createdBy: g.CreatedBy, at: g.CreatedAt.Time, other: g.OtherItems > 0,
				item: StoredRuleItem{Axis: g.Axis, Project: domain.ProjectID(g.ProjectID), Env: domain.EnvID(g.EnvID), KeyID: g.KeyID, Folder: g.FolderPath}})
		}
	}
	return foldRuleLines(rows), nil
}

// ResolveRulePendingKey resolves only metadata for an owned live draft in the
// addressed environment. No value material crosses the pre-authorization path.
func (r *Resolver) ResolveRulePendingKey(ctx context.Context, scope domain.Scope, principal domain.PrincipalID, id string) (domain.RuleKey, error) {
	if r.sq != nil {
		row, err := r.sq.ResolveRulePendingKey(ctx, sqlitegen.ResolveRulePendingKeyParams{OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvID: string(scope.Env), OwnerID: string(principal), ID: id})
		if err != nil {
			return domain.RuleKey{}, rulePendingNotFound(err)
		}
		return domain.RuleKey{ID: row.ID, Folder: row.FolderPath}, nil
	}
	row, err := r.pg.ResolveRulePendingKey(ctx, pggen.ResolveRulePendingKeyParams{OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvID: string(scope.Env), OwnerID: string(principal), ID: id})
	if err != nil {
		return domain.RuleKey{}, rulePendingNotFound(err)
	}
	return domain.RuleKey{ID: row.ID, Folder: row.FolderPath}, nil
}

func rulePendingNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

// ResolveRuleApprovalKey reads only pinned key-id metadata, then resolves the
// first key inside the same chain. Policy eligibility and all-key authority
// must still be proved before rendering details or changing the request.
func (r *Resolver) ResolveRuleApprovalKey(ctx context.Context, scope domain.Scope, id string) (domain.RuleKey, error) {
	var encoded string
	if r.sq != nil {
		value, err := r.sq.ResolveRuleApprovalKeys(ctx, sqlitegen.ResolveRuleApprovalKeysParams{OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvID: string(scope.Env), ID: id})
		if err != nil {
			if errors.Is(rulePendingNotFound(err), domain.ErrNotFound) {
				return r.refuseRuleApprovalKey(ctx, scope)
			}
			return domain.RuleKey{}, rulePendingNotFound(err)
		}
		encoded = value
	} else {
		value, err := r.pg.ResolveRuleApprovalKeys(ctx, pggen.ResolveRuleApprovalKeysParams{OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvID: string(scope.Env), ID: id})
		if err != nil {
			if errors.Is(rulePendingNotFound(err), domain.ErrNotFound) {
				return r.refuseRuleApprovalKey(ctx, scope)
			}
			return domain.RuleKey{}, rulePendingNotFound(err)
		}
		encoded = value
	}
	var ids []string
	if json.Unmarshal([]byte(encoded), &ids) != nil || len(ids) == 0 {
		return r.refuseRuleApprovalKey(ctx, scope)
	}
	for i, keyID := range ids {
		if keyID == "" || slices.Contains(ids[:i], keyID) {
			return r.refuseRuleApprovalKey(ctx, scope)
		}
	}
	return r.ResolveRuleKey(ctx, string(scope.Org), string(scope.Project), ids[0], "")
}

func (r *Resolver) refuseRuleApprovalKey(ctx context.Context, scope domain.Scope) (domain.RuleKey, error) {
	// Match the metadata-plus-key work of an existing inaccessible request.
	// The decoy result never becomes authority, even if an imported database
	// happens to contain that identifier.
	_, err := r.ResolveRuleKey(ctx, string(scope.Org), string(scope.Project), "approval-no-key", "")
	if err != nil && !errors.Is(rulePendingNotFound(err), domain.ErrNotFound) {
		return domain.RuleKey{}, err
	}
	return domain.RuleKey{}, domain.ErrNotFound
}

// RuleKeyHasValue answers only whether the addressed key has a published
// value. Internal metadata-only schema authorization cannot use it as value
// material or as a fallback for a failed populated-environment permission.
func (r *Resolver) RuleKeyHasValue(ctx context.Context, scope domain.Scope, keyID string) (bool, error) {
	if r.sq != nil {
		count, err := r.sq.RuleKeyValueCount(ctx, sqlitegen.RuleKeyValueCountParams{OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvID: string(scope.Env), KeyID: keyID})
		return count > 0, err
	}
	count, err := r.pg.RuleKeyValueCount(ctx, pggen.RuleKeyValueCountParams{OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvID: string(scope.Env), KeyID: keyID})
	return count > 0, err
}

// RuleKeyHasConflictingDraft checks initial-cell ownership without opening drafts.
func (r *Resolver) RuleKeyHasConflictingDraft(ctx context.Context, scope domain.Scope, keyID string, principal domain.PrincipalID) (bool, error) {
	if r.sq != nil {
		count, err := r.sq.RuleKeyConflictingDraftCount(ctx, sqlitegen.RuleKeyConflictingDraftCountParams{OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvID: string(scope.Env), KeyID: keyID, OwnerID: string(principal)})
		return count > 0, err
	}
	count, err := r.pg.RuleKeyConflictingDraftCount(ctx, pggen.RuleKeyConflictingDraftCountParams{OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvID: string(scope.Env), KeyID: keyID, OwnerID: string(principal)})
	return count > 0, err
}
