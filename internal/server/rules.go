package server

import (
	"context"
	"fmt"
	"slices"

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
	Replace(ctx context.Context, actor service.Actor, spec service.ReplaceRulesSpec) ([]service.RuleView, error)
	Create(ctx context.Context, actor service.Actor, spec service.RuleSpec) (service.RuleView, error)
	Revoke(ctx context.Context, actor service.Actor, org domain.OrgID, id string) error
	List(ctx context.Context, actor service.Actor, scope domain.Scope) ([]service.RuleView, error)
	Resolve(ctx context.Context, actor service.Actor, scope domain.Scope, capability domain.Capability, keyID string) ([]service.AccessView, error)
}

// errRuleKeyItem refuses a key item that names neither or both of a folder
// and a key; it is decided from the body alone, before any tenant lookup.
var errRuleKeyItem = fmt.Errorf("%w: a rule key item names exactly one of folder or key", domain.ErrInvalid)

// errRuleAllProjectsFolders refuses an all-projects create whose folder items
// differ between the listed projects: the rule stores one folder list that
// applies in every project, so the request must repeat it under each.
var errRuleAllProjectsFolders = fmt.Errorf("%w: a rule over all projects repeats the same folder items under every listed project", domain.ErrInvalid)

// ruleRequest reads a create request's `where`. With project_mode `all` the
// client lists the org's current projects, which the service compares with
// the org's project set inside the create transaction (owner decision R2,
// #1446); the stored rule keeps only the mode.
func ruleRequest(org string, req apigen.CreateRuleRequest) (service.RuleSpec, error) {
	where, err := ruleWhere(req.Where)
	if err != nil {
		return service.RuleSpec{}, err
	}
	spec := service.RuleSpec{
		Target: domain.PrincipalID(req.Principal), Capability: domain.Capability(req.Capability),
		Org: domain.OrgID(org), Where: where,
	}
	if req.Where.ProjectMode == nil || *req.Where.ProjectMode == apigen.RuleProjectModeOnly {
		return spec, nil
	}
	if *req.Where.ProjectMode != apigen.RuleProjectModeAll {
		return service.RuleSpec{}, fmt.Errorf("%w: project mode %q", domain.ErrInvalid, *req.Where.ProjectMode)
	}
	folders, err := allProjectsFolders(where)
	if err != nil {
		return service.RuleSpec{}, err
	}
	spec.ClaimedProjects = where.Projects
	spec.Where.AllProjects, spec.Where.Projects = true, nil
	spec.Where.Keys = map[domain.ProjectID][]domain.RuleKeyItem{}
	if len(folders) > 0 {
		spec.Where.Keys[domain.AnyProject] = folders
	}
	return spec, nil
}

// allProjectsFolders collapses an all-projects request's key items, repeated
// under every listed project, into the one folder list the rule stores.
// Environment items are left in place for the domain's refusal (#1447).
func allProjectsFolders(w domain.Where) ([]domain.RuleKeyItem, error) {
	var first []domain.RuleKeyItem
	for i, p := range w.Projects {
		items := w.Keys[p]
		for j, it := range items {
			if !it.IsFolder {
				return nil, domain.ErrRuleAllProjectsKeys
			}
			if slices.Contains(items[:j], it) {
				return nil, errRuleAllProjectsFolders
			}
		}
		if i == 0 {
			first = items
			continue
		}
		// Same length, no duplicates and every item in first: the same set.
		if len(items) != len(first) || slices.ContainsFunc(items, func(it domain.RuleKeyItem) bool { return !slices.Contains(first, it) }) {
			return nil, errRuleAllProjectsFolders
		}
	}
	for p := range w.Keys {
		if !slices.Contains(w.Projects, p) {
			return nil, fmt.Errorf("%w: a key item names a project the rule does not list", domain.ErrInvalid)
		}
	}
	return first, nil
}

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

// wireRuleWhere renders a rule's `where`. An all-projects rule is expanded at
// read time over covers, the projects it reaches now, with its folder items
// repeated under each, so `projects` keeps its v1 meaning (#1446).
func wireRuleWhere(w domain.Where, covers []domain.ProjectID) apigen.RuleWhere {
	mode := apigen.RuleProjectModeOnly
	if w.AllProjects {
		mode = apigen.RuleProjectModeAll
		keys := w.Keys[domain.AnyProject]
		w.Projects, w.Keys = covers, map[domain.ProjectID][]domain.RuleKeyItem{}
		for _, p := range covers {
			w.Keys[p] = keys
		}
	}
	out := apigen.RuleWhere{
		ProjectMode:  &mode,
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
			out.Keys.Items = append(out.Keys.Items, wireRuleKeyItem(p, k))
		}
	}
	return out
}

func wireRuleKeyItem(project domain.ProjectID, k domain.RuleKeyItem) apigen.RuleKeyItem {
	item := apigen.RuleKeyItem{Project: string(project)}
	if k.IsFolder {
		folder := k.Folder
		item.Folder = &folder
	} else {
		key := k.KeyID
		item.Key = &key
	}
	return item
}

func wireAccessRule(v service.RuleView) apigen.Rule {
	return apigen.Rule{
		Id: v.ID, PrincipalId: string(v.Principal), PrincipalName: optStr(v.PrincipalName),
		Capability: apigen.RuleCapability(v.Capability), Where: wireRuleWhere(v.Where, v.Covers),
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

func (a *API) ResolveAccess(ctx context.Context, req apigen.ResolveAccessRequestObject) (apigen.ResolveAccessResponseObject, error) {
	// An empty `?key=` never gets here: the contract validator refuses it with
	// 400 (the ID schema's minLength), as for every ID-typed query parameter.
	var keyID string
	if req.Params.Key != nil {
		keyID = *req.Params.Key
	}
	views, err := a.Rules.Resolve(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment),
		domain.Capability(req.Params.Permission), keyID)
	if err != nil {
		return nil, err
	}
	out := apigen.AccessResolution{
		Permission: req.Params.Permission, Environment: req.Environment, Key: req.Params.Key,
		Can: []apigen.AccessPrincipal{}, Excepted: []apigen.AccessPrincipal{},
		NeedsSee: []apigen.AccessPrincipal{}, None: []apigen.AccessPrincipal{},
	}
	for _, v := range views {
		p, err := wireAccessPrincipal(domain.ProjectID(req.Project), v)
		if err != nil {
			return nil, err
		}
		switch v.State {
		case service.AccessCan:
			out.Can = append(out.Can, p)
		case service.AccessExcepted:
			out.Excepted = append(out.Excepted, p)
		case service.AccessNeedsSee:
			out.NeedsSee = append(out.NeedsSee, p)
		case service.AccessNone:
			out.None = append(out.None, p)
		default:
			return nil, fmt.Errorf("rule resolve: unknown access state %q", v.State)
		}
	}
	return apigen.ResolveAccess200JSONResponse(out), nil
}

func wireAccessPrincipal(project domain.ProjectID, v service.AccessView) (apigen.AccessPrincipal, error) {
	var kind apigen.PrincipalKind
	switch v.Class {
	case domain.ClassHuman:
		kind = apigen.Human
	case domain.ClassDeveloper, domain.ClassWorkload, domain.ClassAutomation, domain.ClassProvisioning, domain.ClassInstanceConn:
		kind = apigen.Machine
	default:
		return apigen.AccessPrincipal{}, fmt.Errorf("rule resolve: principal %s has unknown class %q", v.Principal, v.Class)
	}
	out := apigen.AccessPrincipal{
		PrincipalId: string(v.Principal), PrincipalName: optStr(v.Name), Kind: kind,
		Grants: []apigen.GrantScope{}, Rules: []apigen.ID{}, Excepts: []apigen.AccessExcept{},
	}
	for _, g := range v.Grants {
		out.Grants = append(out.Grants, wireGrantScope(g))
	}
	out.Rules = append(out.Rules, v.Rules...)
	for _, e := range v.Excepts {
		except := apigen.AccessExcept{Rule: e.RuleID, Environments: []apigen.ID{}, Keys: []apigen.RuleKeyItem{}, KeyLimited: e.KeyLimited}
		for _, env := range e.Envs {
			except.Environments = append(except.Environments, string(env))
		}
		for _, k := range e.Keys {
			except.Keys = append(except.Keys, wireRuleKeyItem(project, k))
		}
		out.Excepts = append(out.Excepts, except)
	}
	return out, nil
}

func (a *API) CreateRule(ctx context.Context, req apigen.CreateRuleRequestObject) (apigen.CreateRuleResponseObject, error) {
	spec, err := ruleRequest(req.Org, *req.Body)
	if err != nil {
		return nil, err
	}
	view, err := a.Rules.Create(ctx, service.Bearer(bearer(ctx)), spec)
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

func (a *API) ReplaceRules(ctx context.Context, req apigen.ReplaceRulesRequestObject) (apigen.ReplaceRulesResponseObject, error) {
	spec := service.ReplaceRulesSpec{Org: domain.OrgID(req.Org), Target: domain.PrincipalID(req.Body.Principal)}
	if req.Body.Revoke != nil {
		spec.Revoke = *req.Body.Revoke
	}
	var creates []apigen.CreateRuleRequest
	if req.Body.Create != nil {
		creates = *req.Body.Create
	}
	for _, add := range creates {
		one, err := ruleRequest(req.Org, add)
		if err != nil {
			return nil, err
		}
		spec.Create = append(spec.Create, one)
	}
	views, err := a.Rules.Replace(ctx, service.Bearer(bearer(ctx)), spec)
	if err != nil {
		return nil, err
	}
	return apigen.ReplaceRules200JSONResponse(wireAccessRules(views)), nil
}
