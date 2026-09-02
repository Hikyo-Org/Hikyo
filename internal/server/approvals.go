package server

import (
	"context"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// The change-approval transport (#151). Like every other file here it
// TRANSLATES and decides nothing: policy administration is project-scoped, the
// review queue and voting are environment-scoped, and the merge/bypass DECISION
// rides publishPendingChanges (revisions.go), never a second mutation path.

// ApprovalService is the domain surface this transport exposes.
type ApprovalService interface {
	ListPolicies(ctx context.Context, actor service.Actor, scope domain.Scope) ([]service.ApprovalPolicyView, error)
	CreatePolicy(ctx context.Context, actor service.Actor, scope domain.Scope, input service.ApprovalPolicyInput) (service.ApprovalPolicyView, error)
	UpdatePolicy(ctx context.Context, actor service.Actor, scope domain.Scope, id string, input service.ApprovalPolicyInput) (service.ApprovalPolicyView, error)
	DeletePolicy(ctx context.Context, actor service.Actor, scope domain.Scope, id string) error
	ListRequests(ctx context.Context, actor service.Actor, scope domain.Scope) ([]service.ApprovalRequestView, error)
	CeremonyBinding(ctx context.Context, actor service.Actor, scope domain.Scope, requestID string) (service.ApprovalCeremonyBinding, error)
	Vote(ctx context.Context, actor service.Actor, scope domain.Scope, requestID string, decision string) (service.ApprovalRequestView, error)
}

func policyInput(body apigen.ApprovalPolicyInput) service.ApprovalPolicyInput {
	in := service.ApprovalPolicyInput{
		MinApprovals:      int(body.MinApprovals),
		RequestTTLSeconds: int(body.RequestTtlSeconds),
		Enabled:           body.Enabled,
	}
	if body.EnvironmentId != nil {
		in.EnvironmentID = *body.EnvironmentId
	}
	if body.AllowSelfApproval != nil {
		in.AllowSelfApproval = *body.AllowSelfApproval
	}
	if body.Approvers != nil {
		for _, a := range *body.Approvers {
			spec := service.ApprovalApproverSpec{Kind: string(a.Kind), SubjectID: string(a.SubjectId)}
			if a.BindingId != nil {
				spec.BindingID = string(*a.BindingId)
			}
			in.Approvers = append(in.Approvers, spec)
		}
	}
	if body.Bypassers != nil {
		for _, b := range *body.Bypassers {
			in.Bypassers = append(in.Bypassers, string(b))
		}
	}
	return in
}

func wireApprovalPolicy(p service.ApprovalPolicyView) apigen.ApprovalPolicy {
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
	return apigen.ApprovalPolicy{
		Id: p.ID, EnvironmentId: p.EnvironmentID, MinApprovals: int32(p.MinApprovals),
		AllowSelfApproval: p.AllowSelfApproval, RequestTtlSeconds: int32(p.RequestTTLSeconds),
		Enabled: p.Enabled, Version: p.Version, Approvers: approvers, Bypassers: bypassers,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

func wireApprovalRequest(r service.ApprovalRequestView) apigen.ApprovalRequest {
	votes := make([]apigen.ApprovalVote, 0, len(r.Votes))
	for _, v := range r.Votes {
		votes = append(votes, apigen.ApprovalVote{
			PrincipalId: v.PrincipalID, Decision: apigen.ApprovalVoteDecision(v.Decision), CreatedAt: v.CreatedAt,
		})
	}
	out := apigen.ApprovalRequest{
		Id: r.ID, EnvironmentId: r.EnvironmentID, PolicyId: r.PolicyID, PolicyVersion: r.PolicyVersion,
		Requester: r.Requester, ChangeCount: int32(r.ChangeCount), BaseRevision: r.BaseRevision,
		Purpose: r.Purpose, State: apigen.ApprovalRequestState(r.State),
		InvalidatedCause: apigen.ApprovalRequestInvalidatedCause(r.InvalidatedCause),
		MinApprovals:     int32(r.MinApprovals), Approvals: int32(r.Approvals), Votes: votes,
		KeyIds:    r.KeyIDs,
		CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
	}
	if r.ResolvedAt != nil {
		out.ResolvedAt = r.ResolvedAt
	}
	return out
}

func (a *API) ListApprovalPolicies(ctx context.Context, req apigen.ListApprovalPoliciesRequestObject) (apigen.ListApprovalPoliciesResponseObject, error) {
	policies, err := a.Approvals.ListPolicies(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project))
	if err != nil {
		return nil, err
	}
	items := make([]apigen.ApprovalPolicy, 0, len(policies))
	for _, p := range policies {
		items = append(items, wireApprovalPolicy(p))
	}
	return apigen.ListApprovalPolicies200JSONResponse(apigen.ApprovalPolicyList{Items: items}), nil
}

func (a *API) CreateApprovalPolicy(ctx context.Context, req apigen.CreateApprovalPolicyRequestObject) (apigen.CreateApprovalPolicyResponseObject, error) {
	policy, err := a.Approvals.CreatePolicy(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project), policyInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.CreateApprovalPolicy200JSONResponse(wireApprovalPolicy(policy)), nil
}

func (a *API) UpdateApprovalPolicy(ctx context.Context, req apigen.UpdateApprovalPolicyRequestObject) (apigen.UpdateApprovalPolicyResponseObject, error) {
	policy, err := a.Approvals.UpdatePolicy(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project), string(req.Policy), policyInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.UpdateApprovalPolicy200JSONResponse(wireApprovalPolicy(policy)), nil
}

func (a *API) DeleteApprovalPolicy(ctx context.Context, req apigen.DeleteApprovalPolicyRequestObject) (apigen.DeleteApprovalPolicyResponseObject, error) {
	if err := a.Approvals.DeletePolicy(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project), string(req.Policy)); err != nil {
		return nil, err
	}
	return apigen.DeleteApprovalPolicy204Response{}, nil
}

func (a *API) ListApprovalRequests(ctx context.Context, req apigen.ListApprovalRequestsRequestObject) (apigen.ListApprovalRequestsResponseObject, error) {
	requests, err := a.Approvals.ListRequests(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment))
	if err != nil {
		return nil, err
	}
	items := make([]apigen.ApprovalRequest, 0, len(requests))
	for _, r := range requests {
		items = append(items, wireApprovalRequest(r))
	}
	return apigen.ListApprovalRequests200JSONResponse(apigen.ApprovalRequestList{Items: items}), nil
}

func (a *API) GetApprovalCeremony(ctx context.Context, req apigen.GetApprovalCeremonyRequestObject) (apigen.GetApprovalCeremonyResponseObject, error) {
	binding, err := a.Approvals.CeremonyBinding(ctx, service.Bearer(bearer(ctx)),
		envScope(req.Org, req.Project, req.Environment), string(req.ApprovalRequest))
	if err != nil {
		return nil, err
	}
	window := apigen.RevealWindow{
		EffectiveWindowSeconds: binding.Window.EffectiveWindowSeconds,
		Protected:              binding.Window.Protected, TotpOffered: binding.Window.TOTPOffered,
		Live: binding.Window.Live, SingleDecision: binding.Window.SingleDecision,
		CanReveal: binding.Window.CanReveal,
	}
	if !binding.Window.ExpiresAt.IsZero() {
		window.ExpiresAt = &binding.Window.ExpiresAt
	}
	return apigen.GetApprovalCeremony200JSONResponse(apigen.ApprovalCeremonyBinding{
		KeyIds: binding.KeyIDs, Window: window,
	}), nil
}

func (a *API) VoteApprovalRequest(ctx context.Context, req apigen.VoteApprovalRequestRequestObject) (apigen.VoteApprovalRequestResponseObject, error) {
	view, err := a.Approvals.Vote(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment),
		string(req.ApprovalRequest), string(req.Body.Decision))
	if err != nil {
		return nil, err
	}
	return apigen.VoteApprovalRequest200JSONResponse(wireApprovalRequest(view)), nil
}
