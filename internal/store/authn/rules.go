package authn

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// The member access rule surface (member-access-rules ADR). It lives on the
// enumerated resolution surface for the same reason grants do: authorize()
// reads rules to mint a proof. Every mutating method takes the principal-row
// lock (lint.CheckGrantLock) and is named in lint.ResolutionSurfaceWriters.
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
	id, principal, capability, org, envMode, keyMode string
	item                                             StoredRuleItem
}

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
					EnvMode: domain.AxisMode(row.envMode), KeyMode: domain.AxisMode(row.keyMode),
					Envs: map[domain.ProjectID][]domain.EnvID{}, Keys: map[domain.ProjectID][]domain.RuleKeyItem{},
				},
			}}
		}
		it := row.item
		w := &cur.Rule.Where
		switch it.Axis {
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
	if r.historicalRecoveryBeforeRules {
		return nil, nil
	}
	var rows []ruleRow
	if r.sq != nil {
		got, err := r.sq.ListRulesForPrincipal(ctx, string(p))
		if err != nil {
			return nil, err
		}
		for _, g := range got {
			rows = append(rows, ruleRow{id: g.ID, principal: string(p), capability: g.Capability, org: g.OrgID,
				envMode: g.EnvMode, keyMode: g.KeyMode,
				item: StoredRuleItem{Axis: g.Axis, Project: domain.ProjectID(g.ProjectID), Env: domain.EnvID(g.EnvID), KeyID: g.KeyID, Folder: g.FolderPath}})
		}
	} else {
		got, err := r.pg.ListRulesForPrincipal(ctx, string(p))
		if err != nil {
			return nil, err
		}
		for _, g := range got {
			rows = append(rows, ruleRow{id: g.ID, principal: string(p), capability: g.Capability, org: g.OrgID,
				envMode: g.EnvMode, keyMode: g.KeyMode,
				item: StoredRuleItem{Axis: g.Axis, Project: domain.ProjectID(g.ProjectID), Env: domain.EnvID(g.EnvID), KeyID: g.KeyID, Folder: g.FolderPath}})
		}
	}
	return rulesOf(foldRules(rows)), nil
}

// RulesForProject returns every valid rule naming one project, under the
// chokepoint's gates, for the folder-move widening census.
func (r *Resolver) RulesForProject(ctx context.Context, org, project string) ([]domain.Rule, error) {
	if r.historicalRecoveryBeforeRules {
		return nil, nil
	}
	var rows []ruleRow
	if r.sq != nil {
		got, err := r.sq.ListRulesForProject(ctx, sqlitegen.ListRulesForProjectParams{OrgID: org, ProjectID: project})
		if err != nil {
			return nil, err
		}
		for _, g := range got {
			rows = append(rows, ruleRow{id: g.ID, principal: g.PrincipalID, capability: g.Capability, org: g.OrgID,
				envMode: g.EnvMode, keyMode: g.KeyMode,
				item: StoredRuleItem{Axis: g.Axis, Project: domain.ProjectID(g.ProjectID), Env: domain.EnvID(g.EnvID), KeyID: g.KeyID, Folder: g.FolderPath}})
		}
	} else {
		got, err := r.pg.ListRulesForProject(ctx, pggen.ListRulesForProjectParams{OrgID: org, ProjectID: project})
		if err != nil {
			return nil, err
		}
		for _, g := range got {
			rows = append(rows, ruleRow{id: g.ID, principal: g.PrincipalID, capability: g.Capability, org: g.OrgID,
				envMode: g.EnvMode, keyMode: g.KeyMode,
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
				envMode: g.EnvMode, keyMode: g.KeyMode,
				item: StoredRuleItem{ID: g.ItemID, Axis: g.Axis, Project: domain.ProjectID(g.ProjectID), Env: domain.EnvID(g.EnvID), KeyID: g.KeyID, Folder: g.FolderPath}})
		}
	} else {
		got, err := r.pg.GetRule(ctx, id)
		if err != nil {
			return StoredRule{}, false, err
		}
		for _, g := range got {
			rows = append(rows, ruleRow{id: g.ID, principal: g.PrincipalID, capability: g.Capability, org: g.OrgID,
				envMode: g.EnvMode, keyMode: g.KeyMode,
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
		items = append(items, row.item)
	}
	first := rows[0]
	return StoredRule{Rule: domain.Rule{ID: first.id, Principal: domain.PrincipalID(first.principal),
		Capability: domain.Capability(first.capability), Org: domain.OrgID(first.org)}, Items: items}, false, nil
}

// RuleIDsForEnvironment, RuleIDsForKey, RuleIDsForProject and RuleIDsForOrg
// are the deletion paths' censuses.
func (r *Resolver) RuleIDsForEnvironment(ctx context.Context, org, project, env string) ([]string, error) {
	if r.sq != nil {
		return r.sq.ListRuleIDsForEnvironment(ctx, sqlitegen.ListRuleIDsForEnvironmentParams{OrgID: org, ProjectID: project, EnvID: nullString(env)})
	}
	return r.pg.ListRuleIDsForEnvironment(ctx, pggen.ListRuleIDsForEnvironmentParams{OrgID: org, ProjectID: project, EnvID: pgText(env)})
}

func (r *Resolver) RuleIDsForKey(ctx context.Context, org, project, key string) ([]string, error) {
	if r.sq != nil {
		return r.sq.ListRuleIDsForKey(ctx, sqlitegen.ListRuleIDsForKeyParams{OrgID: org, ProjectID: project, KeyID: nullString(key)})
	}
	return r.pg.ListRuleIDsForKey(ctx, pggen.ListRuleIDsForKeyParams{OrgID: org, ProjectID: project, KeyID: pgText(key)})
}

func (r *Resolver) RuleIDsForProject(ctx context.Context, org, project string) ([]string, error) {
	if r.sq != nil {
		return r.sq.ListRuleIDsForProject(ctx, sqlitegen.ListRuleIDsForProjectParams{OrgID: org, ProjectID: project})
	}
	return r.pg.ListRuleIDsForProject(ctx, pggen.ListRuleIDsForProjectParams{OrgID: org, ProjectID: project})
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
			EnvMode: string(rule.Where.EnvMode), KeyMode: string(rule.Where.KeyMode), CreatedBy: string(createdBy), CreatedAt: encodeTime(at),
		}); err != nil {
			return err
		}
	} else if err := r.pg.InsertRule(ctx, pggen.InsertRuleParams{
		ID: rule.ID, PrincipalID: string(rule.Principal), Capability: string(rule.Capability), OrgID: string(rule.Org),
		EnvMode: string(rule.Where.EnvMode), KeyMode: string(rule.Where.KeyMode), CreatedBy: string(createdBy), CreatedAt: pgTimestamp(at),
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
				ID: id, RuleID: rule.ID, OrgID: string(rule.Org), Axis: it.Axis, ProjectID: string(it.Project),
				EnvID: nullString(string(it.Env)), KeyID: nullString(it.KeyID),
				FolderPath: sql.NullString{String: it.Folder, Valid: it.Axis == ruleAxisFolder},
			})
		} else {
			err = r.pg.InsertRuleItem(ctx, pggen.InsertRuleItemParams{
				ID: id, RuleID: rule.ID, OrgID: string(rule.Org), Axis: it.Axis, ProjectID: string(it.Project),
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

// itemsOf flattens a Where into stored items, projects first.
func itemsOf(w domain.Where) []StoredRuleItem {
	var out []StoredRuleItem
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
