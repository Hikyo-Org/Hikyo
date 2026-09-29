package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Member access rules (member-access-rules ADR, stage A: the backend core).
//
// A rule is stored whole and never modified in place: an edit is a revoke
// plus a create, so the session invalidation is exactly the grant surface's.
// Rules are human-only; machines keep their legacy grants and allowlists.
// Only authorize() reads rules. Every other predicate stays blind to them and
// therefore conservative.

var (
	// ErrRuleMachine refuses a rule for a machine principal. The machine
	// widening gate diffs per-environment reach and would undercount a
	// key-narrowed machine reveal, so machines keep legacy grants only.
	ErrRuleMachine = fmt.Errorf("%w: service: member access rules are for people; machine principals keep their grants", domain.ErrInvalid)
	// ErrNoSuchRule is a revoke of a rule that does not exist (or that the
	// caller may not reach, indistinguishably).
	ErrNoSuchRule = fmt.Errorf("%w: service: no such rule", domain.ErrNotFound)
)

// MoveWideningError refuses a key folder move that gives people access
// through their rules without an explicit confirmation naming exactly them
// (ADR D9). It is a conflict: the caller is authorized, the resulting state
// needs their consent.
//
// Who gains is administrative information: Principals and Gains are set only
// when the caller holds legacy manage-members on the project (the audience of
// the rule listing). Anyone else learns only Count, and cannot confirm the
// move.
type MoveWideningError struct {
	KeyID      string
	Count      int
	Principals []domain.PrincipalID
	Gains      []WideningGain
}

// WideningGain is one capability a person gains on the moved key, with the
// environments it newly reaches (empty for a project-wide atom only).
type WideningGain struct {
	Principal  domain.PrincipalID
	Name       string
	Capability domain.Capability
	Envs       []domain.EnvID
}

func (e *MoveWideningError) Error() string {
	return fmt.Sprintf("%v: %s", domain.ErrConflict, e.SafeDetail())
}

func (e *MoveWideningError) Unwrap() error { return domain.ErrConflict }

// SafeDetail names the principals only when they were disclosed to the caller.
func (e *MoveWideningError) SafeDetail() string {
	if len(e.Principals) == 0 {
		return fmt.Sprintf("moving key %s widens access for %d people through their access rules; a member manager must confirm it", e.KeyID, e.Count)
	}
	ids := make([]string, 0, len(e.Principals))
	for _, p := range e.Principals {
		ids = append(ids, string(p))
	}
	return fmt.Sprintf("moving key %s widens access for %s; confirm naming exactly them", e.KeyID, strings.Join(ids, ", "))
}

// Widening is the structured half of the refusal the transport renders; nil
// unless the gainers were disclosed.
func (e *MoveWideningError) Widening() (count int, gains []WideningGain) { return e.Count, e.Gains }

// moveWideningError builds the refusal for gains, naming the people only to a
// caller holding legacy manage-members on the project. The grant read happens
// only on this refusal path.
func moveWideningError(ctx context.Context, az *authz.TxAuthorizer, caller domain.PrincipalID, scope domain.Scope,
	keyID string, gains []WideningGain) error {
	rows, err := az.GrantRowsForPrincipal(ctx, caller)
	if err != nil {
		return err
	}
	gained := gainedPrincipals(gains)
	e := &MoveWideningError{KeyID: keyID, Count: len(gained)}
	if !holds(rows, domain.CapManageMembers, domain.Scope{Org: scope.Org, Project: scope.Project}) {
		return e
	}
	names := newPrincipalNames()
	e.Principals = gained
	for _, g := range gains {
		if g.Name, err = names.get(ctx, az, g.Principal); err != nil {
			return err
		}
		e.Gains = append(e.Gains, g)
	}
	return e
}

// mayConfirmWidening reports whether caller may confirm a widening move in
// scope's project: only a holder of legacy manage-members on the project, the
// same people the refusal names the gainers to. Anyone else learns only the
// count and cannot confirm, even by resending ids learned elsewhere.
func mayConfirmWidening(ctx context.Context, az *authz.TxAuthorizer, caller domain.PrincipalID, scope domain.Scope) (bool, error) {
	rows, err := az.GrantRowsForPrincipal(ctx, caller)
	if err != nil {
		return false, err
	}
	return holds(rows, domain.CapManageMembers, domain.Scope{Org: scope.Org, Project: scope.Project}), nil
}

// gainedPrincipals is the sorted set of people gains name.
func gainedPrincipals(gains []WideningGain) []domain.PrincipalID {
	var out []domain.PrincipalID
	for _, g := range gains {
		if !slices.Contains(out, g.Principal) {
			out = append(out, g.Principal)
		}
	}
	slices.Sort(out)
	return out
}

// mergeGains unions b into a per (principal, capability), keeping the order
// moveWidening produces.
func mergeGains(a, b []WideningGain) []WideningGain {
	for _, g := range b {
		i := slices.IndexFunc(a, func(x WideningGain) bool { return x.Principal == g.Principal && x.Capability == g.Capability })
		if i < 0 {
			a = append(a, WideningGain{Principal: g.Principal, Capability: g.Capability, Envs: slices.Clone(g.Envs)})
			continue
		}
		for _, env := range g.Envs {
			if !slices.Contains(a[i].Envs, env) {
				a[i].Envs = append(a[i].Envs, env)
			}
		}
		slices.Sort(a[i].Envs)
	}
	slices.SortFunc(a, func(x, y WideningGain) int {
		if c := strings.Compare(string(x.Principal), string(y.Principal)); c != 0 {
			return c
		}
		return strings.Compare(string(x.Capability), string(y.Capability))
	})
	return a
}

// MaxRulesPerOrg is the loud sanity cap on rule rows per organization,
// mirroring MaxGrantsPerOrg: it makes runaway rule minting loud.
const MaxRulesPerOrg = 1000

// Rules owns the rule surface.
type Rules struct {
	DB  *store.DB
	Now func() time.Time
}

func (s *Rules) now() time.Time { return nowOr(s.Now) }

// RuleSpec names one rule: who gets which capability, where.
type RuleSpec struct {
	Target     domain.PrincipalID
	Capability domain.Capability
	Org        domain.OrgID
	Where      domain.Where
}

// Create writes one rule. The grantor must hold legacy manage-members on
// every project the rule names; a project-scope member manager may grant only
// a capability it holds (as a grant) on each of those whole projects. Rules
// never satisfy manage-members, so rule-based member management grants
// nothing and never counts in the lockout census.
func (s *Rules) Create(ctx context.Context, actor Actor, spec RuleSpec) (RuleView, error) {
	rule := domain.Rule{Principal: spec.Target, Capability: spec.Capability, Org: spec.Org, Where: spec.Where}
	if err := rule.Validate(); err != nil {
		return RuleView{}, err
	}
	for _, items := range spec.Where.Keys {
		for _, it := range items {
			if it.IsFolder {
				if err := checkKeyFolderPath(it.Folder); err != nil {
					return RuleView{}, err
				}
			}
		}
	}
	var out RuleView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		first := domain.Scope{Org: spec.Org, Project: spec.Where.Projects[0]}
		caller, p, err := authorize(ctx, az, actor, authz.OpRuleCreate, first, now)
		if err != nil {
			return err
		}
		for _, project := range spec.Where.Projects[1:] {
			if _, err := az.Authorize(ctx, caller, authz.OpRuleCreate, domain.Scope{Org: spec.Org, Project: project}); err != nil {
				return err
			}
		}
		if err := az.LockTargetPrincipal(ctx, spec.Target); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return ErrUnknownPrincipal
			}
			return err
		}
		class, err := az.PrincipalClass(ctx, spec.Target)
		if errors.Is(err, domain.ErrNotFound) {
			return ErrUnknownPrincipal
		}
		if err != nil {
			return err
		}
		if class != domain.ClassHuman {
			return ErrRuleMachine
		}
		if err := checkRuleReferences(ctx, az, rule); err != nil {
			return err
		}
		// The grantor bound, on legacy grants only (rules are invisible here):
		// org- or instance-scope manage-members may grant what it does not
		// hold; project-scope manage-members only what it holds as a grant on
		// the whole project, which is at least the rule's reach there.
		grantorGrants, err := az.GrantRowsForPrincipal(ctx, caller.Principal)
		if err != nil {
			return err
		}
		unheld := false
		for _, project := range spec.Where.Projects {
			scope := domain.Scope{Org: spec.Org, Project: project}
			if holds(grantorGrants, spec.Capability, scope) {
				continue
			}
			if !mayGrantUnheld(grantorGrants, scope) {
				return ErrGrantorLacksCapability
			}
			unheld = true
		}
		existing, err := az.RuleIDsForOrg(ctx, spec.Org)
		if err != nil {
			return err
		}
		if len(existing) >= MaxRulesPerOrg {
			return fmt.Errorf("%w: an organization holds at most %d rules", domain.ErrLimitExceeded, MaxRulesPerOrg)
		}
		id, err := newID("rul")
		if err != nil {
			return err
		}
		rule.ID = id
		if err := az.CreateRule(ctx, rule, caller.Principal, now, func() (string, error) { return newID("rli") }); err != nil {
			return err
		}
		if err := invalidateGrantChange(ctx, az, spec.Target); err != nil {
			return err
		}
		payload := rulePayload(rule)
		payload["unheld"] = unheld
		ev, err := domainEvent(ctx, audit.EventRuleCreated, caller.Principal, audit.Object{Type: "rule", ID: id}, payload)
		if err != nil {
			return err
		}
		name, err := newPrincipalNames().get(ctx, az, spec.Target)
		if err != nil {
			return err
		}
		out = RuleView{ID: id, Principal: spec.Target, PrincipalName: name, Capability: spec.Capability,
			Org: spec.Org, Where: rule.Where, CreatedBy: caller.Principal, CreatedAt: now}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
	return out, err
}

// RuleView is one rule on the listing surface. On a project listing Where
// holds only the addressed project's part and OtherProjects reports that the
// rule also names projects the listing does not show. Environments and keys
// are ids (and folder paths): their names need `read`, which a member manager
// may not hold.
type RuleView struct {
	ID            string
	Principal     domain.PrincipalID
	PrincipalName string
	Capability    domain.Capability
	Org           domain.OrgID
	Where         domain.Where
	OtherProjects bool
	CreatedBy     domain.PrincipalID
	CreatedAt     time.Time
}

// ruleListOps is the rule listing's operation per addressed depth, mirroring
// the grant listing.
var ruleListOps = map[domain.Level]authz.Operation{
	domain.LevelOrg:     authz.OpRuleListOrg,
	domain.LevelProject: authz.OpRuleListProject,
}

// List returns the rules in the addressed org, or those naming the addressed
// project (reading only that project's items), audited as a membership read
// like the grant listing.
func (s *Rules) List(ctx context.Context, actor Actor, scope domain.Scope) ([]RuleView, error) {
	level, err := scope.Level()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrInvalid, err)
	}
	op, ok := ruleListOps[level]
	if !ok {
		return nil, fmt.Errorf("%w: rules are listed at org or project scope", domain.ErrInvalid)
	}
	var out []RuleView
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, op, scope, s.now())
		if err != nil {
			return err
		}
		var lines []authz.RuleLine
		if level == domain.LevelProject {
			lines, err = az.RuleLinesInProject(ctx, scope)
		} else {
			lines, err = az.RuleLinesInOrg(ctx, scope.Org)
		}
		if err != nil {
			return err
		}
		names := newPrincipalNames()
		out = make([]RuleView, 0, len(lines))
		for _, line := range lines {
			name, err := names.get(ctx, az, line.Principal)
			if err != nil {
				return err
			}
			where := whereFromItems(line.Items)
			where.EnvMode, where.KeyMode = line.EnvMode, line.KeyMode
			out = append(out, RuleView{
				ID: line.ID, Principal: line.Principal, PrincipalName: name, Capability: line.Capability,
				Org: scope.Org, Where: where, OtherProjects: line.OtherProjects,
				CreatedBy: line.CreatedBy, CreatedAt: line.CreatedAt,
			})
		}
		return insertGrantEvent(ctx, r, p, caller.Principal, level, grantEventInput{
			typ:     audit.EventGrantMembershipRead,
			object:  audit.Object{Type: "scope", ID: renderScope(scope)},
			payload: audit.Payload{"scope": renderScope(scope), "row_count": len(out)},
		})
	})
	return out, err
}

// checkRuleReferences refuses, with a clear error rather than a foreign-key
// violation, a rule naming a project, environment or key outside its org.
func checkRuleReferences(ctx context.Context, az *authz.TxAuthorizer, rule domain.Rule) error {
	for _, project := range rule.Where.Projects {
		scope := domain.Scope{Org: rule.Org, Project: project}
		if _, err := az.ResolveChain(ctx, scope); err != nil {
			return fmt.Errorf("%w: project %s is not in organization %s", domain.ErrInvalid, project, rule.Org)
		}
		for _, env := range rule.Where.Envs[project] {
			if _, err := az.ResolveChain(ctx, domain.Scope{Org: rule.Org, Project: project, Env: env}); err != nil {
				return fmt.Errorf("%w: environment %s is not in project %s", domain.ErrInvalid, env, project)
			}
		}
		for _, it := range rule.Where.Keys[project] {
			if it.IsFolder {
				continue
			}
			if err := az.RuleKeyExists(ctx, scope, it.KeyID); err != nil {
				return fmt.Errorf("%w: key %s is not in project %s", domain.ErrInvalid, it.KeyID, project)
			}
		}
	}
	return nil
}

// Revoke removes one rule, killing its holder's sessions in the same
// transaction. A rule the caller cannot reach answers exactly like a missing
// one.
func (s *Rules) Revoke(ctx context.Context, actor Actor, org domain.OrgID, id string) error {
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		stored, _, err := az.GetRule(ctx, id)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		projects := storedProjects(stored)
		if err != nil || stored.Rule.Org != org || len(projects) == 0 {
			return refuseMissingRule(ctx, az, actor, org, id, now)
		}
		caller, p, err := authorize(ctx, az, actor, authz.OpRuleRevoke, domain.Scope{Org: org, Project: projects[0]}, now)
		if err != nil {
			return err
		}
		for _, project := range projects[1:] {
			if _, err := az.Authorize(ctx, caller, authz.OpRuleRevoke, domain.Scope{Org: org, Project: project}); err != nil {
				return err
			}
		}
		return revokeRule(ctx, r, az, p, caller.Principal, stored, "revoked")
	})
}

// refuseMissingRule answers a revoke of a rule that does not exist in org
// exactly like one the caller cannot reach: the chokepoint is asked about a
// project scope that cannot resolve (rule ids never name a project), so the
// same bare not-found comes back and the denial is captured the same way.
func refuseMissingRule(ctx context.Context, az *authz.TxAuthorizer, actor Actor, org domain.OrgID, id string, now time.Time) error {
	if _, _, err := authorize(ctx, az, actor, authz.OpRuleRevoke, domain.Scope{Org: org, Project: domain.ProjectID("rule:" + id)}, now); err != nil {
		return err
	}
	return domain.ErrNotFound
}

// storedProjects lists the projects a stored rule names, from its items, so
// even a rule that no longer validates can be authorized and removed.
func storedProjects(s authz.StoredRule) []domain.ProjectID {
	var out []domain.ProjectID
	for _, it := range s.Items {
		if !slices.Contains(out, it.Project) {
			out = append(out, it.Project)
		}
	}
	return out
}

// revokeRule deletes a rule, invalidates its holder's sessions and records
// rule.revoked under the caller's proof.
func revokeRule(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, actor domain.PrincipalID, stored authz.StoredRule, cause string) error {
	deleted, err := az.DeleteRule(ctx, stored.Rule.ID, stored.Rule.Principal)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrNoSuchRule
	}
	if err := invalidateGrantChange(ctx, az, stored.Rule.Principal); err != nil {
		return err
	}
	// A rule that no longer validates carries no Where; render what it
	// covered from its stored items so the trail still says it.
	shown := stored.Rule
	if len(shown.Where.Projects) == 0 {
		shown.Where = whereFromItems(stored.Items)
	}
	payload := rulePayload(shown)
	payload["cause"] = cause
	ev, err := domainEvent(ctx, audit.EventRuleRevoked, actor, audit.Object{Type: "rule", ID: stored.Rule.ID}, payload)
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, p, ev)
}

// whereFromItems rebuilds a Where from stored items for rendering only; it is
// never evaluated.
func whereFromItems(items []authz.StoredRuleItem) domain.Where {
	w := domain.Where{Envs: map[domain.ProjectID][]domain.EnvID{}, Keys: map[domain.ProjectID][]domain.RuleKeyItem{}}
	for _, it := range items {
		if !slices.Contains(w.Projects, it.Project) {
			w.Projects = append(w.Projects, it.Project)
		}
		switch {
		case it.Env != "":
			w.Envs[it.Project] = append(w.Envs[it.Project], it.Env)
		case it.KeyID != "":
			w.Keys[it.Project] = append(w.Keys[it.Project], domain.RuleKeyItem{KeyID: it.KeyID})
		case it.Axis == "folder":
			w.Keys[it.Project] = append(w.Keys[it.Project], domain.RuleKeyItem{Folder: it.Folder, IsFolder: true})
		}
	}
	return w
}

// rulePayload renders a rule for its lifecycle events, as stable ids.
func rulePayload(rule domain.Rule) audit.Payload {
	projects := []string{}
	envs := []string{}
	keys := []string{}
	for _, project := range rule.Where.Projects {
		projects = append(projects, string(project))
		for _, e := range rule.Where.Envs[project] {
			envs = append(envs, string(e))
		}
		for _, k := range rule.Where.Keys[project] {
			if k.IsFolder {
				keys = append(keys, "folder:"+string(project)+"/"+audit.SanitizeFreeText(k.Folder))
			} else {
				keys = append(keys, "key:"+k.KeyID)
			}
		}
	}
	envMode, keyMode := string(rule.Where.EnvMode), string(rule.Where.KeyMode)
	if envMode == "" {
		envMode = string(domain.AxisAll)
	}
	if keyMode == "" {
		keyMode = string(domain.AxisAll)
	}
	return audit.Payload{
		"target_principal": string(rule.Principal),
		"capability":       string(rule.Capability),
		"projects":         projects,
		"env_mode":         envMode,
		"envs":             envs,
		"key_mode":         keyMode,
		"keys":             keys,
	}
}

// pruneRules is the deletion paths' half: for every rule in ids, drop the
// items `gone` matches (they name an object being deleted), and delete, with
// rule.revoked, every rule that no longer validates as a result (an only-list
// that lost its last item reaches nothing). Pruning an item naming a deleted
// object never changes what a rule reaches among objects that still exist,
// and ids are never reused, so nothing left behind can match a new object.
func pruneRules(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, actor domain.PrincipalID,
	ids []string, gone func(authz.StoredRuleItem) bool) error {
	for _, id := range ids {
		stored, _, err := az.GetRule(ctx, id)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		for _, it := range stored.Items {
			if gone(it) {
				if err := az.DeleteRuleItem(ctx, id, it.ID, stored.Rule.Principal); err != nil {
					return err
				}
			}
		}
		after, valid, err := az.GetRule(ctx, id)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			// Every item went: remove the bare row.
			if err := revokeRule(ctx, r, az, p, actor, authz.StoredRule{Rule: stored.Rule}, "emptied-by-deletion"); err != nil {
				return err
			}
		case err != nil:
			return err
		case !valid:
			if err := revokeRule(ctx, r, az, p, actor, after, "emptied-by-deletion"); err != nil {
				return err
			}
		}
	}
	return nil
}

// releaseEnvironmentRules prunes an environment being deleted out of every
// rule naming it.
func releaseEnvironmentRules(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, actor domain.PrincipalID, scope domain.Scope) error {
	ids, err := az.RuleIDsForEnvironment(ctx, scope)
	if err != nil {
		return err
	}
	return pruneRules(ctx, r, az, p, actor, ids, func(it authz.StoredRuleItem) bool {
		return it.Project == scope.Project && it.Env == scope.Env
	})
}

// releaseKeyRules prunes a key being deleted out of every rule naming it.
func releaseKeyRules(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, actor domain.PrincipalID, scope domain.Scope, keyID string) error {
	project := domain.Scope{Org: scope.Org, Project: scope.Project}
	ids, err := az.RuleIDsForKey(ctx, project, keyID)
	if err != nil {
		return err
	}
	return pruneRules(ctx, r, az, p, actor, ids, func(it authz.StoredRuleItem) bool {
		return it.Project == scope.Project && it.KeyID == keyID
	})
}

// releaseProjectRules prunes a project being deleted out of every rule
// naming it (its environments and keys are already gone).
func releaseProjectRules(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, actor domain.PrincipalID, scope domain.Scope) error {
	ids, err := az.RuleIDsForProject(ctx, scope)
	if err != nil {
		return err
	}
	return pruneRules(ctx, r, az, p, actor, ids, func(it authz.StoredRuleItem) bool {
		return it.Project == scope.Project
	})
}

// releaseOrgRules removes every rule in an org being deleted.
func releaseOrgRules(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, actor domain.PrincipalID, org domain.OrgID) error {
	ids, err := az.RuleIDsForOrg(ctx, org)
	if err != nil {
		return err
	}
	for _, id := range ids {
		stored, _, err := az.GetRule(ctx, id)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if err := revokeRule(ctx, r, az, p, actor, stored, "scope-deleted"); err != nil {
			return err
		}
	}
	return nil
}

// moveWideningCaps are the capabilities a folder move can widen through a
// rule: the key-shaped ones (read and pin are never key-narrowed, and
// manage-members is inert on rules).
var moveWideningCaps = []domain.Capability{
	domain.CapEdit, domain.CapPublish, domain.CapReveal, domain.CapRevealHistory, domain.CapDefinitionsEdit,
}

// moveWidening computes, inside the moving transaction, what people gain on
// the key through their RULES when it moves from one folder to another (ADR
// D9): each capability with the environments it newly reaches. Legacy grants
// are folder-blind and cannot change with a move, so only rules are consulted.
func moveWidening(ctx context.Context, az *authz.TxAuthorizer, scope domain.Scope, envIDs []string, keyID, from, to string) ([]WideningGain, error) {
	if from == to {
		return nil, nil
	}
	rules, err := az.RulesForProject(ctx, scope.Org, scope.Project)
	if err != nil {
		return nil, err
	}
	type atom struct {
		principal  domain.PrincipalID
		capability domain.Capability
		env        domain.EnvID // "" for the project-wide atom
	}
	reach := func(folder string) map[atom]bool {
		out := map[atom]bool{}
		key := &domain.RuleKey{ID: keyID, Folder: folder}
		for _, rule := range rules {
			for _, c := range moveWideningCaps {
				if rule.Reaches(c, domain.LevelProject, domain.Scope{Org: scope.Org, Project: scope.Project}, key) {
					out[atom{rule.Principal, c, ""}] = true
				}
				for _, env := range envIDs {
					s := domain.Scope{Org: scope.Org, Project: scope.Project, Env: domain.EnvID(env)}
					if rule.Reaches(c, domain.LevelEnv, s, key) {
						out[atom{rule.Principal, c, domain.EnvID(env)}] = true
					}
				}
			}
		}
		return out
	}
	before, after := reach(from), reach(to)
	var gains []WideningGain
	for a := range after {
		if before[a] {
			continue
		}
		g := WideningGain{Principal: a.principal, Capability: a.capability}
		if a.env != "" {
			g.Envs = []domain.EnvID{a.env}
		}
		gains = mergeGains(gains, []WideningGain{g})
	}
	return gains, nil
}

// confirmMoveWidening refuses a widening move unless the confirmation names
// exactly the gaining set, and records the confirmed set when it does.
func confirmMoveWidening(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, actor domain.PrincipalID,
	scope domain.Scope, keyID, from, to string, gains []WideningGain, confirmed []domain.PrincipalID) error {
	gained := gainedPrincipals(gains)
	if len(gained) == 0 {
		return nil
	}
	want := slices.Clone(confirmed)
	slices.Sort(want)
	want = slices.Compact(want)
	if !slices.Equal(want, gained) {
		return moveWideningError(ctx, az, actor, scope, keyID, gains)
	}
	if ok, err := mayConfirmWidening(ctx, az, actor, scope); err != nil {
		return err
	} else if !ok {
		return moveWideningError(ctx, az, actor, scope, keyID, gains)
	}
	names := make([]string, 0, len(gained))
	for _, g := range gained {
		names = append(names, string(g))
	}
	ev, err := domainEvent(ctx, audit.EventRuleMoveWideningConfirmed, actor, audit.Object{Type: "key", ID: keyID}, audit.Payload{
		"key_id":      keyID,
		"from_folder": audit.SanitizeFreeText(from),
		"to_folder":   audit.SanitizeFreeText(to),
		"principals":  names,
	})
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, p, ev)
}
