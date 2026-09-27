package server

import (
	"context"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// The temporary-access transport (#152). It TRANSLATES and decides nothing:
// policy administration is project-scoped; requests, votes, cancellation,
// revocation and emergency access are environment-scoped.

// AccessService is the domain surface this transport exposes.
type AccessService interface {
	ListPolicies(ctx context.Context, actor service.Actor, scope domain.Scope) ([]service.AccessPolicyView, error)
	CreatePolicy(ctx context.Context, actor service.Actor, scope domain.Scope, input service.AccessPolicyInput) (service.AccessPolicyView, error)
	UpdatePolicy(ctx context.Context, actor service.Actor, scope domain.Scope, id string, input service.AccessPolicyInput) (service.AccessPolicyView, error)
	DeletePolicy(ctx context.Context, actor service.Actor, scope domain.Scope, id string) error
	Queue(ctx context.Context, actor service.Actor, scope domain.Scope) (service.AccessQueue, error)
	Request(ctx context.Context, actor service.Actor, scope domain.Scope, input service.AccessRequestInput) (service.AccessRequestView, error)
	Vote(ctx context.Context, actor service.Actor, scope domain.Scope, requestID, decision string) (service.AccessRequestView, error)
	Cancel(ctx context.Context, actor service.Actor, scope domain.Scope, requestID string) (service.AccessRequestView, error)
	Revoke(ctx context.Context, actor service.Actor, scope domain.Scope, requestID string) (service.AccessRequestView, error)
	EmergencyAccess(ctx context.Context, actor service.Actor, scope domain.Scope, input service.AccessRequestInput) (service.AccessRequestView, error)
}

func accessPolicyInput(body apigen.AccessPolicyInput) service.AccessPolicyInput {
	in := service.AccessPolicyInput{
		MaxDurationSeconds: int(body.MaxDurationSeconds),
		MinApprovals:       int(body.MinApprovals),
		RequestTTLSeconds:  int(body.RequestTtlSeconds),
		Enabled:            body.Enabled,
	}
	for _, c := range body.Capabilities {
		in.Capabilities = append(in.Capabilities, string(c))
	}
	if body.EnvironmentId != nil {
		in.EnvironmentID = *body.EnvironmentId
	}
	if body.AllowSelfApproval != nil {
		in.AllowSelfApproval = *body.AllowSelfApproval
	}
	for _, a := range body.Approvers {
		spec := service.ApprovalApproverSpec{Kind: string(a.Kind), SubjectID: string(a.SubjectId)}
		if a.BindingId != nil {
			spec.BindingID = string(*a.BindingId)
		}
		in.Approvers = append(in.Approvers, spec)
	}
	if body.Bypassers != nil {
		for _, b := range *body.Bypassers {
			in.Bypassers = append(in.Bypassers, string(b))
		}
	}
	return in
}

func accessRequestInput(body apigen.AccessRequestInput) service.AccessRequestInput {
	return service.AccessRequestInput{
		Capabilities: accessCapabilityStrings(body.Capabilities), Reason: body.Reason,
		DurationSeconds: int(body.DurationSeconds),
	}
}

// emergencyAccessInput maps the emergency body; an absent duration stays 0,
// which the service reads as its one-hour default capped by the policy.
func emergencyAccessInput(body apigen.EmergencyAccessInput) service.AccessRequestInput {
	in := service.AccessRequestInput{Capabilities: accessCapabilityStrings(body.Capabilities), Reason: body.Reason}
	if body.DurationSeconds != nil {
		in.DurationSeconds = int(*body.DurationSeconds)
	}
	return in
}

func accessCapabilityStrings(caps []apigen.AccessCapability) []string {
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		out = append(out, string(c))
	}
	return out
}

func wireAccessCapabilities(caps []string) []apigen.AccessCapability {
	out := make([]apigen.AccessCapability, 0, len(caps))
	for _, c := range caps {
		out = append(out, apigen.AccessCapability(c))
	}
	return out
}

func wireAccessPolicy(p service.AccessPolicyView) apigen.AccessPolicy {
	var principalNames *map[string]string
	if len(p.PrincipalNames) > 0 {
		principalNames = &p.PrincipalNames
	}
	approvers := make([]apigen.ApprovalApprover, 0, len(p.Approvers))
	for _, a := range p.Approvers {
		approver := apigen.ApprovalApprover{Kind: apigen.ApprovalApproverKind(a.Kind), SubjectId: a.SubjectID}
		if a.BindingID != "" {
			binding := a.BindingID
			approver.BindingId = &binding
		}
		approvers = append(approvers, approver)
	}
	bypassers := make([]string, 0, len(p.Bypassers))
	bypassers = append(bypassers, p.Bypassers...)
	return apigen.AccessPolicy{
		Id: p.ID, EnvironmentId: p.EnvironmentID, Capabilities: wireAccessCapabilities(p.Capabilities),
		MaxDurationSeconds: int32(p.MaxDurationSeconds), MinApprovals: int32(p.MinApprovals),
		AllowSelfApproval: p.AllowSelfApproval, RequestTtlSeconds: int32(p.RequestTTLSeconds),
		Enabled: p.Enabled, Version: p.Version, PrincipalNames: principalNames,
		Approvers: approvers, Bypassers: bypassers, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

func wireAccessRequest(r service.AccessRequestView) apigen.AccessRequest {
	votes := make([]apigen.ApprovalVote, 0, len(r.Votes))
	for _, v := range r.Votes {
		votes = append(votes, apigen.ApprovalVote{
			PrincipalId: v.PrincipalID, PrincipalName: optStr(v.PrincipalName),
			Decision: apigen.ApprovalVoteDecision(v.Decision), CreatedAt: v.CreatedAt,
		})
	}
	out := apigen.AccessRequest{
		Id: r.ID, EnvironmentId: r.EnvironmentID, PolicyId: r.PolicyID, PolicyVersion: r.PolicyVersion,
		Requester: r.Requester, RequesterName: optStr(r.RequesterName),
		Capabilities: wireAccessCapabilities(r.Capabilities), DurationSeconds: int32(r.DurationSeconds),
		Reason: r.Reason, Bypassed: r.Bypassed, State: apigen.AccessRequestState(r.State),
		InvalidatedCause: apigen.AccessRequestInvalidatedCause(r.InvalidatedCause),
		MinApprovals:     int32(r.MinApprovals), Approvals: int32(r.Approvals), Votes: votes,
		CreatedAt: r.CreatedAt, ReviewExpiresAt: r.ReviewExpiresAt,
		GrantedAt: r.GrantedAt, ExpiresAt: r.ExpiresAt, ResolvedAt: r.ResolvedAt,
	}
	if r.ResolvedBy != "" {
		resolvedBy := r.ResolvedBy
		out.ResolvedBy = &resolvedBy
	}
	return out
}

func (a *API) ListAccessPolicies(ctx context.Context, req apigen.ListAccessPoliciesRequestObject) (apigen.ListAccessPoliciesResponseObject, error) {
	policies, err := a.Access.ListPolicies(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project))
	if err != nil {
		return nil, err
	}
	items := make([]apigen.AccessPolicy, 0, len(policies))
	for _, p := range policies {
		items = append(items, wireAccessPolicy(p))
	}
	return apigen.ListAccessPolicies200JSONResponse(apigen.AccessPolicyList{Items: items}), nil
}

func (a *API) CreateAccessPolicy(ctx context.Context, req apigen.CreateAccessPolicyRequestObject) (apigen.CreateAccessPolicyResponseObject, error) {
	policy, err := a.Access.CreatePolicy(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project), accessPolicyInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.CreateAccessPolicy200JSONResponse(wireAccessPolicy(policy)), nil
}

func (a *API) UpdateAccessPolicy(ctx context.Context, req apigen.UpdateAccessPolicyRequestObject) (apigen.UpdateAccessPolicyResponseObject, error) {
	policy, err := a.Access.UpdatePolicy(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project),
		string(req.Policy), accessPolicyInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.UpdateAccessPolicy200JSONResponse(wireAccessPolicy(policy)), nil
}

func (a *API) DeleteAccessPolicy(ctx context.Context, req apigen.DeleteAccessPolicyRequestObject) (apigen.DeleteAccessPolicyResponseObject, error) {
	if err := a.Access.DeletePolicy(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project), string(req.Policy)); err != nil {
		return nil, err
	}
	return apigen.DeleteAccessPolicy204Response{}, nil
}

func (a *API) ListAccessRequests(ctx context.Context, req apigen.ListAccessRequestsRequestObject) (apigen.ListAccessRequestsResponseObject, error) {
	queue, err := a.Access.Queue(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment))
	if err != nil {
		return nil, err
	}
	out := apigen.AccessQueue{Items: make([]apigen.AccessRequest, 0, len(queue.Requests))}
	for _, r := range queue.Requests {
		out.Items = append(out.Items, wireAccessRequest(r))
	}
	if o := queue.Offer; o != nil {
		out.Offer = &apigen.AccessOffer{
			PolicyId: o.PolicyID, PolicyVersion: o.PolicyVersion, Capabilities: wireAccessCapabilities(o.Capabilities),
			MaxDurationSeconds: int32(o.MaxDurationSeconds), MinApprovals: int32(o.MinApprovals),
			Enabled: o.Enabled, CallerMayBypass: o.CallerMayBypass,
		}
	}
	return apigen.ListAccessRequests200JSONResponse(out), nil
}

func (a *API) CreateAccessRequest(ctx context.Context, req apigen.CreateAccessRequestRequestObject) (apigen.CreateAccessRequestResponseObject, error) {
	view, err := a.Access.Request(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment), accessRequestInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.CreateAccessRequest200JSONResponse(wireAccessRequest(view)), nil
}

func (a *API) EmergencyAccess(ctx context.Context, req apigen.EmergencyAccessRequestObject) (apigen.EmergencyAccessResponseObject, error) {
	view, err := a.Access.EmergencyAccess(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment), emergencyAccessInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.EmergencyAccess200JSONResponse(wireAccessRequest(view)), nil
}

func (a *API) VoteAccessRequest(ctx context.Context, req apigen.VoteAccessRequestRequestObject) (apigen.VoteAccessRequestResponseObject, error) {
	view, err := a.Access.Vote(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment),
		string(req.AccessRequest), string(req.Body.Decision))
	if err != nil {
		return nil, err
	}
	return apigen.VoteAccessRequest200JSONResponse(wireAccessRequest(view)), nil
}

func (a *API) CancelAccessRequest(ctx context.Context, req apigen.CancelAccessRequestRequestObject) (apigen.CancelAccessRequestResponseObject, error) {
	view, err := a.Access.Cancel(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment), string(req.AccessRequest))
	if err != nil {
		return nil, err
	}
	return apigen.CancelAccessRequest200JSONResponse(wireAccessRequest(view)), nil
}

func (a *API) RevokeAccessRequest(ctx context.Context, req apigen.RevokeAccessRequestRequestObject) (apigen.RevokeAccessRequestResponseObject, error) {
	view, err := a.Access.Revoke(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment), string(req.AccessRequest))
	if err != nil {
		return nil, err
	}
	return apigen.RevokeAccessRequest200JSONResponse(wireAccessRequest(view)), nil
}
