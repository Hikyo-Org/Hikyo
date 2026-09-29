package server

import (
	"context"
	"fmt"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// The member access rule transport (member-access-rules ADR). The org is the
// path; the projects are the body's `where`, and the service authorizes
// manage-members on every one of them, so no body field widens the path's
// authority. Refusals are bare errors for the uniform writer, exactly as the
// grant transport's.

// RuleService is the domain surface this transport exposes.
type RuleService interface {
	Create(ctx context.Context, actor service.Actor, spec service.RuleSpec) (service.RuleView, error)
	Revoke(ctx context.Context, actor service.Actor, org domain.OrgID, id string) error
	List(ctx context.Context, actor service.Actor, scope domain.Scope) ([]service.RuleView, error)
}

// errRuleKeyItem refuses a key item that names neither or both of a folder
// and a key; it is decided from the body alone, before any tenant lookup.
var errRuleKeyItem = fmt.Errorf("%w: a rule key item names exactly one of folder or key", domain.ErrInvalid)

// ruleWhere turns the wire `where` into the domain's per-project form.
func ruleWhere(w apigen.RuleWhere) (domain.Where, error) {
	out := domain.Where{
		EnvMode: domain.AxisMode(w.Environments.Mode), KeyMode: domain.AxisMode(w.Keys.Mode),
		Envs: map[domain.ProjectID][]domain.EnvID{}, Keys: map[domain.ProjectID][]domain.RuleKeyItem{},
	}
	for _, p := range w.Projects {
		out.Projects = append(out.Projects, domain.ProjectID(p))
	}
	for _, it := range w.Environments.Items {
		p := domain.ProjectID(it.Project)
		out.Envs[p] = append(out.Envs[p], domain.EnvID(it.Environment))
	}
	for _, it := range w.Keys.Items {
		p := domain.ProjectID(it.Project)
		switch {
		case it.Folder != nil && it.Key == nil:
			out.Keys[p] = append(out.Keys[p], domain.RuleKeyItem{Folder: *it.Folder, IsFolder: true})
		case it.Key != nil && it.Folder == nil:
			out.Keys[p] = append(out.Keys[p], domain.RuleKeyItem{KeyID: *it.Key})
		default:
			return domain.Where{}, errRuleKeyItem
		}
	}
	return out, nil
}

func wireRuleWhere(w domain.Where) apigen.RuleWhere {
	out := apigen.RuleWhere{
		Projects:     []apigen.ID{},
		Environments: apigen.RuleEnvironmentAxis{Mode: apigen.RuleAxisMode(w.EnvMode), Items: []apigen.RuleEnvironmentItem{}},
		Keys:         apigen.RuleKeyAxis{Mode: apigen.RuleAxisMode(w.KeyMode), Items: []apigen.RuleKeyItem{}},
	}
	for _, p := range w.Projects {
		out.Projects = append(out.Projects, string(p))
		for _, e := range w.Envs[p] {
			out.Environments.Items = append(out.Environments.Items, apigen.RuleEnvironmentItem{Project: string(p), Environment: string(e)})
		}
		for _, k := range w.Keys[p] {
			item := apigen.RuleKeyItem{Project: string(p)}
			if k.IsFolder {
				folder := k.Folder
				item.Folder = &folder
			} else {
				key := k.KeyID
				item.Key = &key
			}
			out.Keys.Items = append(out.Keys.Items, item)
		}
	}
	return out
}

func wireAccessRule(v service.RuleView) apigen.Rule {
	return apigen.Rule{
		Id: v.ID, PrincipalId: string(v.Principal), PrincipalName: optStr(v.PrincipalName),
		Capability: apigen.RuleCapability(v.Capability), Where: wireRuleWhere(v.Where),
		OtherProjects: v.OtherProjects, CreatedBy: string(v.CreatedBy), CreatedAt: v.CreatedAt,
	}
}

func wireAccessRules(views []service.RuleView) apigen.RuleList {
	items := make([]apigen.Rule, 0, len(views))
	for _, v := range views {
		items = append(items, wireAccessRule(v))
	}
	return apigen.RuleList{Items: items, Count: len(items)}
}

func (a *API) ListOrgRules(ctx context.Context, req apigen.ListOrgRulesRequestObject) (apigen.ListOrgRulesResponseObject, error) {
	views, err := a.Rules.List(ctx, service.Bearer(bearer(ctx)), domain.Scope{Org: domain.OrgID(req.Org)})
	if err != nil {
		return nil, err
	}
	return apigen.ListOrgRules200JSONResponse(wireAccessRules(views)), nil
}

func (a *API) ListProjectRules(ctx context.Context, req apigen.ListProjectRulesRequestObject) (apigen.ListProjectRulesResponseObject, error) {
	views, err := a.Rules.List(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project))
	if err != nil {
		return nil, err
	}
	return apigen.ListProjectRules200JSONResponse(wireAccessRules(views)), nil
}

func (a *API) CreateRule(ctx context.Context, req apigen.CreateRuleRequestObject) (apigen.CreateRuleResponseObject, error) {
	where, err := ruleWhere(req.Body.Where)
	if err != nil {
		return nil, err
	}
	view, err := a.Rules.Create(ctx, service.Bearer(bearer(ctx)), service.RuleSpec{
		Target: domain.PrincipalID(req.Body.Principal), Capability: domain.Capability(req.Body.Capability),
		Org: domain.OrgID(req.Org), Where: where,
	})
	if err != nil {
		return nil, err
	}
	return apigen.CreateRule201JSONResponse(wireAccessRule(view)), nil
}

func (a *API) RevokeRule(ctx context.Context, req apigen.RevokeRuleRequestObject) (apigen.RevokeRuleResponseObject, error) {
	if err := a.Rules.Revoke(ctx, service.Bearer(bearer(ctx)), domain.OrgID(req.Org), req.Rule); err != nil {
		return nil, err
	}
	return apigen.RevokeRule204Response{}, nil
}

// confirmWidening maps the optional wire confirmation onto principal ids.
func confirmWidening(in *apigen.ConfirmWidening) []domain.PrincipalID {
	if in == nil {
		return nil
	}
	out := make([]domain.PrincipalID, 0, len(*in))
	for _, id := range *in {
		out = append(out, domain.PrincipalID(id))
	}
	return out
}

// wireWidening renders a folder-move widening refusal's structured member.
func wireWidening(count int, gains []service.WideningGain) *apigen.WideningRefusal {
	out := &apigen.WideningRefusal{Count: count}
	if len(gains) == 0 {
		return out
	}
	gainers := make([]apigen.WideningGainer, 0, len(gains))
	for _, g := range gains {
		envs := make([]apigen.ID, 0, len(g.Envs))
		for _, e := range g.Envs {
			envs = append(envs, string(e))
		}
		gainers = append(gainers, apigen.WideningGainer{
			PrincipalId: string(g.Principal), PrincipalName: optStr(g.Name),
			Capability: apigen.RuleCapability(g.Capability), Environments: envs,
		})
	}
	out.Gainers = &gainers
	return out
}
