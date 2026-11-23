package service

import (
	"context"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// The answer states, re-exported so handlers classify an answer without
// importing the chokepoint.
// AccessState is the answer state type.
type AccessState = authz.AccessState

const (
	AccessCan      = authz.AccessCan
	AccessExcepted = authz.AccessExcepted
	AccessNeedsSee = authz.AccessNeedsSee
	AccessNone     = authz.AccessNone
)

// AccessView is one principal of a "Who can...?" answer (member-access-rules
// ADR D10), with the principal's display name.
type AccessView struct {
	authz.AccessAnswer
	Name string
}

// Resolve answers who can use one permission in one environment, optionally
// for one key, from the chokepoint's own evaluation. The answer discloses
// membership, so it needs the authority of the rule listing (`manage-members`
// at the project). The population is the project's grant lines, the org-level
// grant lines of the asked permission (an org admin reaches the project
// through them), and the project rules the
// caller may manage (see Rules.List). Sibling projects' lines are never read,
// and instance holders are not listed, as on the Members page; the returned
// grants stop at org scope for the same reason.
// It is audited as the same membership read.
func (s *Rules) Resolve(ctx context.Context, actor Actor, scope domain.Scope, capability domain.Capability, keyID string) ([]AccessView, error) {
	var out []AccessView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpRuleResolve, scope, s.now())
		if err != nil {
			return err
		}
		var key *authz.KeyTarget
		if keyID != "" {
			target := authz.KeyByID(keyID)
			key = &target
		}
		// Never the org's rows (sibling projects): see Grants.List.
		grantLines, err := az.GrantLinesInProject(ctx, string(scope.Org), string(scope.Project))
		if err != nil {
			return err
		}
		orgLines, err := az.GrantLinesAtOrgLevel(ctx, string(scope.Org), capability)
		if err != nil {
			return err
		}
		grantLines = append(grantLines, orgLines...)
		ruleLines, err := manageableProjectRules(ctx, az, caller, scope)
		if err != nil {
			return err
		}
		pop := authz.AccessPopulation{Rules: map[string]bool{}}
		for _, l := range grantLines {
			pop.Principals = append(pop.Principals, l.Principal)
		}
		for _, l := range ruleLines {
			pop.Principals = append(pop.Principals, l.Principal)
			pop.Rules[l.ID] = true
		}
		answers, err := az.ResolveAccess(ctx, p, pop, capability, key)
		if err != nil {
			return err
		}
		names := newPrincipalNames()
		out = make([]AccessView, 0, len(answers))
		for _, a := range answers {
			name, err := names.get(ctx, az, a.Principal)
			if err != nil {
				return err
			}
			// Instance scope would reveal operator status the project listing hides.
			a.Grants = slices.DeleteFunc(a.Grants, func(g domain.Scope) bool { return g.Org == "" })
			out = append(out, AccessView{AccessAnswer: a, Name: name})
		}
		return insertGrantEvent(ctx, r, p, caller.Principal, domain.LevelEnv, grantEventInput{
			typ:     audit.EventGrantMembershipRead,
			object:  audit.Object{Type: "scope", ID: renderScope(scope)},
			payload: audit.Payload{"scope": renderScope(scope), "row_count": len(out)},
		})
	})
	return out, err
}
