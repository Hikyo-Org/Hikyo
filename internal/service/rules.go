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

// MoveWideningError refuses a key folder move that gives the listed people
// access through their rules without an explicit confirmation naming exactly
// them (ADR D9). It is a conflict: the caller is authorized, the resulting
// state needs their consent.
type MoveWideningError struct {
	KeyID      string
	Principals []domain.PrincipalID
}

func (e *MoveWideningError) Error() string {
	return fmt.Sprintf("%v: %s", domain.ErrConflict, e.SafeDetail())
}

func (e *MoveWideningError) Unwrap() error { return domain.ErrConflict }

// SafeDetail names the principals; the caller administers this project's keys
// and every id here is one its own rule list would show.
func (e *MoveWideningError) SafeDetail() string {
	ids := make([]string, 0, len(e.Principals))
	for _, p := range e.Principals {
		ids = append(ids, string(p))
	}
	return fmt.Sprintf("moving key %s widens access for %s; confirm naming exactly them", e.KeyID, strings.Join(ids, ", "))
}

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
func (s *Rules) Create(ctx context.Context, actor Actor, spec RuleSpec) (string, error) {
	rule := domain.Rule{Principal: spec.Target, Capability: spec.Capability, Org: spec.Org, Where: spec.Where}
	if err := rule.Validate(); err != nil {
		return "", err
	}
	for _, items := range spec.Where.Keys {
		for _, it := range items {
			if it.IsFolder {
				if err := checkKeyFolderPath(it.Folder); err != nil {
					return "", err
				}
			}
		}
	}
	var out string
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
		out = id
		return r.Audit().InsertTenant(ctx, p, ev)
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
		if errors.Is(err, domain.ErrNotFound) {
			return ErrNoSuchRule
		}
		if err != nil {
			return err
		}
		projects := storedProjects(stored)
		if stored.Rule.Org != org || len(projects) == 0 {
			return ErrNoSuchRule
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
	payload := rulePayload(stored.Rule)
	payload["cause"] = cause
	ev, err := domainEvent(ctx, audit.EventRuleRevoked, actor, audit.Object{Type: "rule", ID: stored.Rule.ID}, payload)
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, p, ev)
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

// moveWidening computes, inside the moving transaction, the principals who
// gain any capability on the key in any environment through their RULES when
// it moves from one folder to another (ADR D9). Legacy grants are folder-blind
// and cannot change with a move, so only rules are consulted.
func moveWidening(ctx context.Context, az *authz.TxAuthorizer, scope domain.Scope, envIDs []string, keyID, from, to string) ([]domain.PrincipalID, error) {
	if from == to {
		return nil, nil
	}
	rules, err := az.RulesForProject(ctx, scope.Org, scope.Project)
	if err != nil {
		return nil, err
	}
	reach := func(folder string) map[string]bool {
		out := map[string]bool{}
		key := &domain.RuleKey{ID: keyID, Folder: folder}
		for _, rule := range rules {
			for _, c := range moveWideningCaps {
				if rule.Reaches(c, domain.LevelProject, domain.Scope{Org: scope.Org, Project: scope.Project}, key) {
					out[string(rule.Principal)+"|"+string(c)+"|*"] = true
				}
				for _, env := range envIDs {
					s := domain.Scope{Org: scope.Org, Project: scope.Project, Env: domain.EnvID(env)}
					if rule.Reaches(c, domain.LevelEnv, s, key) {
						out[string(rule.Principal)+"|"+string(c)+"|"+env] = true
					}
				}
			}
		}
		return out
	}
	before, after := reach(from), reach(to)
	var gained []domain.PrincipalID
	for entry := range after {
		if before[entry] {
			continue
		}
		p := domain.PrincipalID(strings.SplitN(entry, "|", 2)[0])
		if !slices.Contains(gained, p) {
			gained = append(gained, p)
		}
	}
	slices.Sort(gained)
	return gained, nil
}

// confirmMoveWidening refuses a widening move unless the confirmation names
// exactly the gaining set, and records the confirmed set when it does.
func confirmMoveWidening(ctx context.Context, r store.Repos, p authz.Proof, actor domain.PrincipalID,
	keyID, from, to string, gained, confirmed []domain.PrincipalID) error {
	if len(gained) == 0 {
		return nil
	}
	want := slices.Clone(confirmed)
	slices.Sort(want)
	want = slices.Compact(want)
	if !slices.Equal(want, gained) {
		return &MoveWideningError{KeyID: keyID, Principals: gained}
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
