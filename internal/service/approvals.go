package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Secret-change approvals (#151). The policy-bound review-and-merge engine.
//
// The load-bearing invariant, stated once: an approval authorises ONE exact
// reviewed change set and is never a second mutation path. Merge and emergency
// bypass do not live here at all -- they are conjuncts inside the ordinary
// publish (see approvalGate in publish.go), so a merge is the same validated
// materialization any publish is, re-authorising every participant at commit.
// This file owns policy administration, the request/vote read surfaces, voting,
// the expiry sweep, and the shared gate helpers publish.go calls.

// maxApprovalPurposeBytes bounds the free-text purpose a requester may attach.
const maxApprovalPurposeBytes = 1024

// ErrApprovalRequired is the named refusal a publish returns when a policy
// covers the environment but no completed approval or bypass is presented. It
// wraps ErrConflict so the wire answers 409: the caller's selection is fine,
// but the environment's policy has not been satisfied yet.
var ErrApprovalRequired = fmt.Errorf("%w: service: this environment requires an approved change-request before publish", domain.ErrConflict)

// ErrApprovalNotSatisfied is returned when a named request cannot be merged:
// not enough approvals from currently-eligible approvers, or a reject stands.
var ErrApprovalNotSatisfied = fmt.Errorf("%w: service: the approval request has not reached its quorum", domain.ErrConflict)

// ErrApprovalGatedMultiEnv refuses a multi-environment publish that touches a
// policy-covered environment: a request pins exactly one environment's change
// set, so a covered publish must address that environment alone.
var ErrApprovalGatedMultiEnv = fmt.Errorf("%w: service: a publish into a policy-covered environment must address that environment alone", domain.ErrInvalid)

// Approvals is the change-approval service.
type Approvals struct {
	DB   *store.DB
	Auth *Auth
	// Keyring recomputes the publish preview token when creating and merging a
	// request, so the pinned digest is derived exactly as the publish path
	// derives it. Nil is a wiring fault.
	Keyring *crypto.Keyring
	Now     func() time.Time
}

func (s *Approvals) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

// ApprovalApproverSpec is one approver-set member as the admin surface states it.
type ApprovalApproverSpec struct {
	Kind      string // "principal" | "scim_group"
	SubjectID string
	BindingID string // SCIM binding, for a group approver
}

// ApprovalPolicyInput is a policy create/update request from the admin surface.
type ApprovalPolicyInput struct {
	EnvironmentID     string // "" = all environments in the project
	MinApprovals      int
	AllowSelfApproval bool
	RequestTTLSeconds int
	Enabled           bool
	Approvers         []ApprovalApproverSpec
	Bypassers         []string // principal ids
}

// ApprovalPolicyView is a policy as the admin surface reads it.
type ApprovalPolicyView struct {
	ID                string
	EnvironmentID     string
	MinApprovals      int
	AllowSelfApproval bool
	RequestTTLSeconds int
	Enabled           bool
	Version           int64
	Approvers         []ApprovalApproverSpec
	Bypassers         []string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ApprovalVoteView is one recorded vote.
type ApprovalVoteView struct {
	PrincipalID string
	Decision    string
	CreatedAt   time.Time
}

// ApprovalRequestView is a request as the review surface reads it.
type ApprovalRequestView struct {
	ID               string
	EnvironmentID    string
	PolicyID         string
	PolicyVersion    int64
	Requester        string
	ChangeCount      int
	BaseRevision     int64
	Purpose          string
	State            string
	InvalidatedCause string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	ResolvedAt       *time.Time
	MinApprovals     int
	Approvals        int
	Votes            []ApprovalVoteView
}

// approvalPreviewDigest is the pinned identity of a change set: the SHA-256 hex
// of the publish preview token. Storing the digest rather than the token keeps
// the request row free of the token's key material.
func approvalPreviewDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreatePolicy adds a scoped policy. Project-scoped authority.
func (s *Approvals) CreatePolicy(ctx context.Context, actor Actor, scope domain.Scope, input ApprovalPolicyInput) (ApprovalPolicyView, error) {
	if err := validatePolicyInput(input); err != nil {
		return ApprovalPolicyView{}, err
	}
	now := s.now()
	var view ApprovalPolicyView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, now)
		if err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, authz.OpApprovalPolicyWrite, scope)
		if err != nil {
			return err
		}
		if err := validatePolicyEnvironment(ctx, az, input.EnvironmentID); err != nil {
			return err
		}
		id, err := newID("apol")
		if err != nil {
			return err
		}
		if err := r.Approvals().InsertPolicy(ctx, p, store.NewApprovalPolicy{
			ID: id, EnvironmentID: input.EnvironmentID, MinApprovals: input.MinApprovals,
			AllowSelfApproval: input.AllowSelfApproval, RequestTTLSeconds: input.RequestTTLSeconds,
			Enabled: input.Enabled, CreatedBy: string(caller.Principal), CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := writePolicyMembers(ctx, r, p, id, input); err != nil {
			return err
		}
		if err := recordPolicyChange(ctx, r, p, caller.Principal, id, "created", input); err != nil {
			return err
		}
		view, err = loadPolicyView(ctx, r, p, id)
		return err
	})
	return view, err
}

// UpdatePolicy replaces a policy's fields and member sets, bumping its version
// (which fails every request pinned to the old version closed at merge/vote).
func (s *Approvals) UpdatePolicy(ctx context.Context, actor Actor, scope domain.Scope, id string, input ApprovalPolicyInput) (ApprovalPolicyView, error) {
	if err := validatePolicyInput(input); err != nil {
		return ApprovalPolicyView{}, err
	}
	now := s.now()
	var view ApprovalPolicyView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, now)
		if err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, authz.OpApprovalPolicyWrite, scope)
		if err != nil {
			return err
		}
		updated, err := r.Approvals().UpdatePolicy(ctx, p, store.ApprovalPolicyUpdate{
			ID: id, MinApprovals: input.MinApprovals, AllowSelfApproval: input.AllowSelfApproval,
			RequestTTLSeconds: input.RequestTTLSeconds, Enabled: input.Enabled, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		if !updated {
			return domain.ErrNotFound
		}
		if _, err := r.Approvals().ClearApprovers(ctx, p, id); err != nil {
			return err
		}
		if _, err := r.Approvals().ClearBypassers(ctx, p, id); err != nil {
			return err
		}
		if err := writePolicyMembers(ctx, r, p, id, input); err != nil {
			return err
		}
		if err := recordPolicyChange(ctx, r, p, caller.Principal, id, "updated", input); err != nil {
			return err
		}
		view, err = loadPolicyView(ctx, r, p, id)
		return err
	})
	return view, err
}

// DeletePolicy removes a policy and its member sets (FK cascade). Open requests
// pinned to it fail closed at merge/vote via the missing-coverage check.
func (s *Approvals) DeletePolicy(ctx context.Context, actor Actor, scope domain.Scope, id string) error {
	now := s.now()
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, now)
		if err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, authz.OpApprovalPolicyWrite, scope)
		if err != nil {
			return err
		}
		policy, err := r.Approvals().GetPolicy(ctx, p, id)
		if err != nil {
			return err
		}
		deleted, err := r.Approvals().DeletePolicy(ctx, p, id)
		if err != nil {
			return err
		}
		if !deleted {
			return domain.ErrNotFound
		}
		return recordPolicyChangeCounts(ctx, r, p, caller.Principal, id, "deleted", policy.EnvironmentID,
			policy.MinApprovals, policy.AllowSelfApproval, false, 0, 0)
	})
}

// ListPolicies returns the project's policies. Project-scoped read.
func (s *Approvals) ListPolicies(ctx context.Context, actor Actor, scope domain.Scope) ([]ApprovalPolicyView, error) {
	now := s.now()
	var out []ApprovalPolicyView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, now)
		if err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, authz.OpApprovalPolicyRead, scope)
		if err != nil {
			return err
		}
		policies, err := r.Approvals().ListPolicies(ctx, p)
		if err != nil {
			return err
		}
		out = make([]ApprovalPolicyView, 0, len(policies))
		for _, policy := range policies {
			view, err := policyViewWithMembers(ctx, r, p, policy)
			if err != nil {
				return err
			}
			out = append(out, view)
		}
		return nil
	})
	return out, err
}

// ListRequests returns an environment's approval requests with their votes.
func (s *Approvals) ListRequests(ctx context.Context, actor Actor, scope domain.Scope) ([]ApprovalRequestView, error) {
	now := s.now()
	var out []ApprovalRequestView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, now)
		if err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, authz.OpApprovalRequestRead, scope)
		if err != nil {
			return err
		}
		requests, err := r.Approvals().ListRequests(ctx, p)
		if err != nil {
			return err
		}
		out = make([]ApprovalRequestView, 0, len(requests))
		for _, req := range requests {
			view, err := requestViewWithVotes(ctx, r, p, req)
			if err != nil {
				return err
			}
			out = append(out, view)
		}
		return nil
	})
	return out, err
}

// Vote records one approver's decision. It re-authorises the voter as a
// currently-eligible approver, refuses self-approval unless the policy permits
// it, is idempotent on a repeated identical decision and a 409 on a conflicting
// one, and consumes a purpose-bound reauthentication ceremony first.
func (s *Approvals) Vote(ctx context.Context, actor Actor, scope domain.Scope, requestID string, decision store.ApprovalVoteDecision) (ApprovalRequestView, error) {
	if decision != store.ApprovalDecisionApprove && decision != store.ApprovalDecisionReject {
		return ApprovalRequestView{}, fmt.Errorf("%w: a vote is approve or reject", domain.ErrInvalid)
	}
	now := s.now()
	var view ApprovalRequestView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, now)
		if err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, authz.OpApprovalVote, scope)
		if err != nil {
			return err
		}
		req, err := r.Approvals().GetRequest(ctx, p, requestID)
		if err != nil {
			return err
		}
		// The request must be live: open or approved, not resolved, not expired.
		if req.State != store.ApprovalStateOpen && req.State != store.ApprovalStateApproved {
			return fmt.Errorf("%w: the request is %s", domain.ErrConflict, req.State)
		}
		if !now.Before(req.ExpiresAt) {
			return fmt.Errorf("%w: the request has expired", domain.ErrConflict)
		}
		policy, err := r.Approvals().GetPolicy(ctx, p, req.PolicyID)
		if err != nil {
			// A deleted policy cannot be voted under: fail closed.
			if errors.Is(err, domain.ErrNotFound) {
				return invalidateRequest(ctx, r, p, caller.Principal, req, "policy_changed")
			}
			return err
		}
		if policy.Version != req.PolicyVersion {
			return invalidateRequest(ctx, r, p, caller.Principal, req, "policy_changed")
		}
		approvers, err := r.Approvals().ListApprovers(ctx, p, req.PolicyID)
		if err != nil {
			return err
		}
		eligible, err := approverEligible(ctx, r, az, p, approvers, caller.Principal)
		if err != nil {
			return err
		}
		if !eligible {
			// Not an approver: the uniform forbidden, never a hint about the set.
			return domain.ErrUnauthorized
		}
		self := caller.Principal == domain.PrincipalID(req.RequesterPrincipalID)
		if decision == store.ApprovalDecisionApprove && self && !policy.AllowSelfApproval {
			return fmt.Errorf("%w: the requester cannot approve their own change under this policy", domain.ErrUnauthorized)
		}
		// Idempotency: a repeated identical decision is a no-op; a conflicting
		// one is a 409.
		if existing, err := r.Approvals().GetVote(ctx, p, requestID, string(caller.Principal)); err == nil {
			if existing.Decision == decision {
				view, err = requestViewWithVotes(ctx, r, p, req)
				return err
			}
			return fmt.Errorf("%w: this approver already cast a %s vote", domain.ErrConflict, existing.Decision)
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		// The vote is a reauthenticated decision over the request's key set.
		intent, err := NewApproveReauthIntent(req.EnvironmentID, req.KeyIDs)
		if err != nil {
			return err
		}
		if err := requireCeremony(ctx, s.Auth, az, caller, intent); err != nil {
			return err
		}
		voteID, err := newID("avote")
		if err != nil {
			return err
		}
		if err := r.Approvals().InsertVote(ctx, p, store.NewApprovalVote{
			ID: voteID, RequestID: requestID, PrincipalID: string(caller.Principal),
			Decision: decision, CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := recordVote(ctx, r, p, caller.Principal, req, decision, self); err != nil {
			return err
		}
		// A reject resolves the request immediately; an approve advances it to
		// approved once the quorum of currently-eligible approvers is reached.
		if decision == store.ApprovalDecisionReject {
			if _, err := r.Approvals().UpdateRequestState(ctx, p, requestID, store.ApprovalStateRejected, "", &now); err != nil {
				return err
			}
		} else {
			approvals, err := countEligibleApprovals(ctx, r, az, p, approvers, req, policy)
			if err != nil {
				return err
			}
			if approvals >= policy.MinApprovals && req.State != store.ApprovalStateApproved {
				if _, err := r.Approvals().UpdateRequestState(ctx, p, requestID, store.ApprovalStateApproved, "", nil); err != nil {
					return err
				}
			}
		}
		fresh, err := r.Approvals().GetRequest(ctx, p, requestID)
		if err != nil {
			return err
		}
		view, err = requestViewWithVotes(ctx, r, p, fresh)
		return err
	})
	return view, err
}

// ExpireDue is the scheduler job: it resolves every active request past its
// expiry, across all tenants, and emits a per-request expiry event on the
// tenant trail under scoped scheduler authority. It mirrors the retention GC.
func (s *Approvals) ExpireDue(ctx context.Context) error {
	now := store.CanonTime(s.now())
	_, err := tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (int, error) {
		p, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
		if err != nil {
			return 0, err
		}
		due, err := r.Approvals().SelectExpired(ctx, p, now)
		if err != nil {
			return 0, err
		}
		expired := 0
		for _, row := range due {
			marked, err := r.Approvals().MarkExpired(ctx, p, row.ID, now)
			if err != nil {
				return expired, err
			}
			if !marked {
				continue
			}
			scoped, err := az.ScopedSystemAuthority(ctx, authz.SiteScheduler, domain.Scope{
				Org: domain.OrgID(row.OrgID), Project: domain.ProjectID(row.ProjectID), Env: domain.EnvID(row.EnvironmentID),
			})
			if err != nil {
				return expired, err
			}
			ev, err := newAuditEvent(ctx, audit.EventApprovalExpired, "",
				audit.Object{Type: "approval-request", ID: row.ID}, audit.OutcomeSuccess, "", audit.Payload{
					"policy_id": row.PolicyID, "expired_at": now.Format(time.RFC3339Nano),
				})
			if err != nil {
				return expired, err
			}
			ev.Actor.Class = audit.ActorSystem
			ev.OccurredAt = now
			if err := r.Audit().InsertTenant(ctx, scoped, ev); err != nil {
				return expired, err
			}
			expired++
		}
		return expired, nil
	})
	return err
}

// --- shared helpers, also used by the publish gate ---

func validatePolicyInput(input ApprovalPolicyInput) error {
	if input.MinApprovals < 1 {
		return fmt.Errorf("%w: min_approvals must be at least 1", domain.ErrInvalid)
	}
	if input.RequestTTLSeconds <= 0 {
		return fmt.Errorf("%w: request_ttl_seconds must be positive", domain.ErrInvalid)
	}
	for _, a := range input.Approvers {
		switch a.Kind {
		case string(store.ApprovalApproverPrincipal):
			if a.SubjectID == "" {
				return fmt.Errorf("%w: a principal approver needs a subject", domain.ErrInvalid)
			}
		case string(store.ApprovalApproverSCIMGroup):
			if a.SubjectID == "" || a.BindingID == "" {
				return fmt.Errorf("%w: a SCIM-group approver needs a group and its binding", domain.ErrInvalid)
			}
		default:
			return fmt.Errorf("%w: unknown approver kind %q", domain.ErrInvalid, a.Kind)
		}
	}
	return nil
}

func validatePolicyEnvironment(ctx context.Context, az *authz.TxAuthorizer, envID string) error {
	if envID == "" {
		return nil // project-wide
	}
	if _, err := az.EnvironmentReauthSettings(ctx, envID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("%w: environment %q is not in this project", domain.ErrInvalid, envID)
		}
		return err
	}
	return nil
}

func writePolicyMembers(ctx context.Context, r store.Repos, p authz.Proof, policyID string, input ApprovalPolicyInput) error {
	for _, a := range input.Approvers {
		id, err := newID("aapr")
		if err != nil {
			return err
		}
		if err := r.Approvals().InsertApprover(ctx, p, store.NewApprovalApprover{
			ID: id, PolicyID: policyID, Kind: store.ApprovalApproverKind(a.Kind),
			SubjectID: a.SubjectID, ScopeBindingID: a.BindingID,
		}); err != nil {
			return err
		}
	}
	for _, principalID := range input.Bypassers {
		id, err := newID("abyp")
		if err != nil {
			return err
		}
		if err := r.Approvals().InsertBypasser(ctx, p, store.NewApprovalBypasser{
			ID: id, PolicyID: policyID, PrincipalID: principalID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func recordPolicyChange(ctx context.Context, r store.Repos, p authz.Proof, principal domain.PrincipalID, policyID, action string, input ApprovalPolicyInput) error {
	return recordPolicyChangeCounts(ctx, r, p, principal, policyID, action, input.EnvironmentID,
		input.MinApprovals, input.AllowSelfApproval, input.Enabled, len(input.Approvers), len(input.Bypassers))
}

func recordPolicyChangeCounts(ctx context.Context, r store.Repos, p authz.Proof, principal domain.PrincipalID,
	policyID, action, envID string, minApprovals int, selfApproval, enabled bool, approvers, bypassers int) error {
	ev, err := domainEvent(ctx, audit.EventApprovalPolicyChanged, principal,
		audit.Object{Type: "approval-policy", ID: policyID}, audit.Payload{
			"action": action, "environment": envID, "min_approvals": minApprovals,
			"self_approval": selfApproval, "enabled": enabled,
			"approver_count": approvers, "bypasser_count": bypassers,
		})
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, p, ev)
}

func recordVote(ctx context.Context, r store.Repos, p authz.Proof, principal domain.PrincipalID,
	req store.ApprovalRequest, decision store.ApprovalVoteDecision, self bool) error {
	ev, err := domainEvent(ctx, audit.EventApprovalVoted, principal,
		audit.Object{Type: "approval-request", ID: req.ID}, audit.Payload{
			"decision": string(decision), "self_approval": self,
		})
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, p, ev)
}

// invalidateRequest resolves a request as invalidated with a cause and emits
// the event. Used at vote and merge time when the pinned change set no longer
// matches (policy moved, drafts edited, environment advanced).
func invalidateRequest(ctx context.Context, r store.Repos, p authz.Proof, principal domain.PrincipalID,
	req store.ApprovalRequest, cause string) error {
	now := time.Now().UTC()
	if _, err := r.Approvals().UpdateRequestState(ctx, p, req.ID, store.ApprovalStateInvalidated, cause, &now); err != nil {
		return err
	}
	ev, err := domainEvent(ctx, audit.EventApprovalInvalidated, principal,
		audit.Object{Type: "approval-request", ID: req.ID}, audit.Payload{"cause": cause})
	if err != nil {
		return err
	}
	if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
		return err
	}
	return fmt.Errorf("%w: the request was invalidated (%s)", domain.ErrConflict, cause)
}

// approverEligible reports whether principal is a currently-eligible approver:
// named directly, or an active member of a named SCIM group. A machine
// principal (no account) is never eligible.
func approverEligible(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof,
	approvers []store.ApprovalApprover, principal domain.PrincipalID) (bool, error) {
	for _, a := range approvers {
		if a.Kind == store.ApprovalApproverPrincipal && a.SubjectID == string(principal) {
			return true, nil
		}
	}
	account, err := az.AccountByPrincipal(ctx, principal)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	for _, a := range approvers {
		if a.Kind != store.ApprovalApproverSCIMGroup {
			continue
		}
		user, err := r.SCIM().UserByAccount(ctx, p, a.ScopeBindingID, account.ID)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		if !user.Active {
			continue
		}
		members, err := r.SCIM().MembershipsForUser(ctx, p, a.ScopeBindingID, user.ID)
		if err != nil {
			return false, err
		}
		for _, m := range members {
			if m.GroupID == a.SubjectID {
				return true, nil
			}
		}
	}
	return false, nil
}

// countEligibleApprovals counts the approve votes cast by principals who are
// STILL eligible approvers, excluding the requester unless the policy permits
// self-approval. Recomputed live so an approver removed after voting no longer
// counts (a rejection would have already resolved the request).
func countEligibleApprovals(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof,
	approvers []store.ApprovalApprover, req store.ApprovalRequest, policy store.ApprovalPolicy) (int, error) {
	votes, err := r.Approvals().ListVotes(ctx, p, req.ID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, v := range votes {
		if v.Decision != store.ApprovalDecisionApprove {
			continue
		}
		if v.PrincipalID == req.RequesterPrincipalID && !policy.AllowSelfApproval {
			continue
		}
		eligible, err := approverEligible(ctx, r, az, p, approvers, domain.PrincipalID(v.PrincipalID))
		if err != nil {
			return 0, err
		}
		if eligible {
			count++
		}
	}
	return count, nil
}

// --- view builders ---

func policyViewWithMembers(ctx context.Context, r store.Repos, p authz.Proof, policy store.ApprovalPolicy) (ApprovalPolicyView, error) {
	approvers, err := r.Approvals().ListApprovers(ctx, p, policy.ID)
	if err != nil {
		return ApprovalPolicyView{}, err
	}
	bypassers, err := r.Approvals().ListBypassers(ctx, p, policy.ID)
	if err != nil {
		return ApprovalPolicyView{}, err
	}
	view := ApprovalPolicyView{
		ID: policy.ID, EnvironmentID: policy.EnvironmentID, MinApprovals: policy.MinApprovals,
		AllowSelfApproval: policy.AllowSelfApproval, RequestTTLSeconds: policy.RequestTTLSeconds,
		Enabled: policy.Enabled, Version: policy.Version, CreatedAt: policy.CreatedAt, UpdatedAt: policy.UpdatedAt,
	}
	for _, a := range approvers {
		view.Approvers = append(view.Approvers, ApprovalApproverSpec{Kind: string(a.Kind), SubjectID: a.SubjectID, BindingID: a.ScopeBindingID})
	}
	for _, b := range bypassers {
		view.Bypassers = append(view.Bypassers, b.PrincipalID)
	}
	return view, nil
}

func loadPolicyView(ctx context.Context, r store.Repos, p authz.Proof, id string) (ApprovalPolicyView, error) {
	policy, err := r.Approvals().GetPolicy(ctx, p, id)
	if err != nil {
		return ApprovalPolicyView{}, err
	}
	return policyViewWithMembers(ctx, r, p, policy)
}

func requestViewWithVotes(ctx context.Context, r store.Repos, p authz.Proof, req store.ApprovalRequest) (ApprovalRequestView, error) {
	votes, err := r.Approvals().ListVotes(ctx, p, req.ID)
	if err != nil {
		return ApprovalRequestView{}, err
	}
	view := requestView(req)
	for _, v := range votes {
		view.Votes = append(view.Votes, ApprovalVoteView{PrincipalID: v.PrincipalID, Decision: string(v.Decision), CreatedAt: v.CreatedAt})
		if v.Decision == store.ApprovalDecisionApprove {
			view.Approvals++
		}
	}
	// The policy's current quorum, best-effort: a deleted policy leaves it zero.
	if policy, err := r.Approvals().GetPolicy(ctx, p, req.PolicyID); err == nil {
		view.MinApprovals = policy.MinApprovals
	} else if !errors.Is(err, domain.ErrNotFound) {
		return ApprovalRequestView{}, err
	}
	return view, nil
}

func requestView(req store.ApprovalRequest) ApprovalRequestView {
	return ApprovalRequestView{
		ID: req.ID, EnvironmentID: req.EnvironmentID, PolicyID: req.PolicyID,
		PolicyVersion: req.PolicyVersion, Requester: req.RequesterPrincipalID,
		ChangeCount: len(req.VersionIDs) + len(req.ClosedVersionIDs), BaseRevision: req.BaseRevision,
		Purpose: req.Purpose, State: string(req.State), InvalidatedCause: req.InvalidatedCause,
		CreatedAt: req.CreatedAt, ExpiresAt: req.ExpiresAt, ResolvedAt: req.ResolvedAt,
	}
}

// sortedUnique returns the sorted, de-duplicated set.
func sortedUnique(in []string) []string {
	out := slices.Clone(in)
	sort.Strings(out)
	return slices.Compact(out)
}

// constantTimeEqual compares two digests without leaking length-independent
// timing.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// --- the publish gate (called from PublishPlanned) ---

// approvalOutcome is what approvalGate tells PublishPlanned to do. A zero value
// (gated=false) means no policy covers the publish and it proceeds normally.
type approvalOutcome struct {
	gated bool
	// created is non-nil when a request was created; the publish must NOT
	// materialize -- staging a request is the whole of what happened.
	created *store.ApprovalRequest
	// resolveID is set for a merge or bypass: after a successful materialize,
	// PublishPlanned flips this request to merged/bypassed and emits its event.
	resolveID     string
	bypass        bool
	reason        string
	previewDigest string
	coveredEnv    string
	approvals     int
}

// approvalGate is the live conjunct the permission-model amendment adds to
// publish: where a policy covers the addressed environment, a bare publish is
// refused, a named request is merged only if approved, and an emergency bypass
// is admitted only for a named bypasser with a reauthenticated reason. It runs
// inside the publish transaction on the publish proof, so approval is never a
// second mutation path.
func approvalGate(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, auth *Auth, keyring *crypto.Keyring,
	caller authz.Identity, scope domain.Scope, request PublishRequest, loadedReq *store.ApprovalRequest,
	selection map[string][]pendingApply, closed []string, proofs map[string]authz.Proof, now time.Time) (approvalOutcome, error) {
	// Find the covering policy. A gated publish must address exactly one
	// environment, so any covered environment in a multi-environment selection
	// is refused rather than partially gated.
	var coveredEnv string
	var policy store.ApprovalPolicy
	for envID, p := range proofs {
		pol, ok, err := r.Approvals().CoveringPolicy(ctx, p, envID)
		if err != nil {
			return approvalOutcome{}, err
		}
		if ok {
			coveredEnv = envID
			policy = pol
			break
		}
	}
	if coveredEnv == "" {
		return approvalOutcome{}, nil
	}
	if len(selection) > 1 {
		return approvalOutcome{}, ErrApprovalGatedMultiEnv
	}
	p := proofs[coveredEnv]
	keyIDs := selectionKeyIDs(selection[coveredEnv])
	token, err := publishPreviewToken(ctx, r, proofs, keyring, az, caller.Principal, scope, selection)
	if err != nil {
		return approvalOutcome{}, err
	}
	digest := approvalPreviewDigest(token)

	switch {
	case request.Bypass != nil:
		return approvalBypassGate(ctx, r, az, auth, p, caller, policy, loadedReq, request, coveredEnv, keyIDs, digest, now)
	case request.ApprovalRequestID != "":
		return approvalMergeGate(ctx, r, az, p, caller, policy, loadedReq, coveredEnv, digest, now)
	default:
		return approvalCreateGate(ctx, r, p, keyring, caller, policy, request, coveredEnv, keyIDs, digest, selection, closed, now)
	}
}

func approvalCreateGate(ctx context.Context, r store.Repos, p authz.Proof, _ *crypto.Keyring,
	caller authz.Identity, policy store.ApprovalPolicy, request PublishRequest, coveredEnv string,
	keyIDs []string, digest string, selection map[string][]pendingApply, closed []string, now time.Time) (approvalOutcome, error) {
	purpose, err := sanitizePurpose(request.Purpose)
	if err != nil {
		return approvalOutcome{}, err
	}
	base, err := currentRevision(ctx, r, p)
	if err != nil {
		return approvalOutcome{}, err
	}
	id, err := newID("areq")
	if err != nil {
		return approvalOutcome{}, err
	}
	req := store.NewApprovalRequest{
		ID: id, EnvironmentID: coveredEnv, PolicyID: policy.ID, PolicyVersion: policy.Version,
		RequesterPrincipalID: string(caller.Principal),
		VersionIDs:           sortedUnique(request.VersionIDs),
		ClosedVersionIDs:     sortedUnique(closed),
		KeyIDs:               keyIDs,
		PreviewTokenDigest:   digest, BaseRevision: base, Purpose: purpose,
		CreatedAt: now, ExpiresAt: now.Add(time.Duration(policy.RequestTTLSeconds) * time.Second),
	}
	if err := r.Approvals().InsertRequest(ctx, p, req); err != nil {
		return approvalOutcome{}, err
	}
	ev, err := domainEvent(ctx, audit.EventApprovalRequested, caller.Principal,
		audit.Object{Type: "approval-request", ID: id}, audit.Payload{
			"policy_id": policy.ID, "policy_version": policy.Version,
			"change_count":  len(req.VersionIDs) + len(req.ClosedVersionIDs),
			"base_revision": base, "preview_digest": digest,
		})
	if err != nil {
		return approvalOutcome{}, err
	}
	if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
		return approvalOutcome{}, err
	}
	stored := store.ApprovalRequest{
		ID: id, EnvironmentID: coveredEnv, PolicyID: policy.ID, PolicyVersion: policy.Version,
		RequesterPrincipalID: string(caller.Principal), VersionIDs: req.VersionIDs,
		ClosedVersionIDs: req.ClosedVersionIDs, KeyIDs: keyIDs, PreviewTokenDigest: digest,
		BaseRevision: base, Purpose: purpose, State: store.ApprovalStateOpen,
		CreatedAt: now, ExpiresAt: req.ExpiresAt,
	}
	return approvalOutcome{gated: true, created: &stored, coveredEnv: coveredEnv}, nil
}

func approvalMergeGate(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, caller authz.Identity,
	policy store.ApprovalPolicy, req *store.ApprovalRequest, coveredEnv, digest string, now time.Time) (approvalOutcome, error) {
	if req == nil {
		return approvalOutcome{}, fmt.Errorf("%w: no approval request was loaded to merge", domain.ErrInvalid)
	}
	if req.EnvironmentID != coveredEnv {
		return approvalOutcome{}, fmt.Errorf("%w: the request does not belong to this environment", domain.ErrConflict)
	}
	if req.State != store.ApprovalStateOpen && req.State != store.ApprovalStateApproved {
		return approvalOutcome{}, fmt.Errorf("%w: the request is %s", domain.ErrConflict, req.State)
	}
	if !now.Before(req.ExpiresAt) {
		return approvalOutcome{}, fmt.Errorf("%w: the request has expired", domain.ErrConflict)
	}
	// The merger must be the proposer: publish materializes the caller's OWN
	// drafts, so only the requester can commit the reviewed set.
	if caller.Principal != domain.PrincipalID(req.RequesterPrincipalID) {
		return approvalOutcome{}, fmt.Errorf("%w: only the proposer may merge their reviewed change", domain.ErrUnauthorized)
	}
	if policy.Version != req.PolicyVersion {
		return approvalOutcome{}, invalidateRequest(ctx, r, p, caller.Principal, *req, "policy_changed")
	}
	if !constantTimeEqual(digest, req.PreviewTokenDigest) {
		return approvalOutcome{}, invalidateRequest(ctx, r, p, caller.Principal, *req, driftCause(ctx, r, p, *req))
	}
	approvers, err := r.Approvals().ListApprovers(ctx, p, req.PolicyID)
	if err != nil {
		return approvalOutcome{}, err
	}
	approvals, err := countEligibleApprovals(ctx, r, az, p, approvers, *req, policy)
	if err != nil {
		return approvalOutcome{}, err
	}
	if approvals < policy.MinApprovals {
		return approvalOutcome{}, ErrApprovalNotSatisfied
	}
	return approvalOutcome{gated: true, resolveID: req.ID, previewDigest: digest, coveredEnv: coveredEnv, approvals: approvals}, nil
}

func approvalBypassGate(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, auth *Auth, p authz.Proof, caller authz.Identity,
	policy store.ApprovalPolicy, req *store.ApprovalRequest, request PublishRequest, coveredEnv string,
	keyIDs []string, digest string, now time.Time) (approvalOutcome, error) {
	if req == nil {
		return approvalOutcome{}, fmt.Errorf("%w: emergency bypass names an existing request", domain.ErrInvalid)
	}
	// Bypass is a human, interactive ceremony: a machine identity has no second
	// factor to re-present and cannot bypass.
	if skipsCeremony(caller) {
		return approvalOutcome{}, fmt.Errorf("%w: emergency bypass requires an interactive reauthenticated session", domain.ErrUnauthorized)
	}
	if req.EnvironmentID != coveredEnv {
		return approvalOutcome{}, fmt.Errorf("%w: the request does not belong to this environment", domain.ErrConflict)
	}
	if req.State != store.ApprovalStateOpen && req.State != store.ApprovalStateApproved {
		return approvalOutcome{}, fmt.Errorf("%w: the request is %s", domain.ErrConflict, req.State)
	}
	if !now.Before(req.ExpiresAt) {
		return approvalOutcome{}, fmt.Errorf("%w: the request has expired", domain.ErrConflict)
	}
	if caller.Principal != domain.PrincipalID(req.RequesterPrincipalID) {
		return approvalOutcome{}, fmt.Errorf("%w: only the proposer may commit their own change set", domain.ErrUnauthorized)
	}
	if policy.Version != req.PolicyVersion {
		return approvalOutcome{}, invalidateRequest(ctx, r, p, caller.Principal, *req, "policy_changed")
	}
	if !constantTimeEqual(digest, req.PreviewTokenDigest) {
		return approvalOutcome{}, invalidateRequest(ctx, r, p, caller.Principal, *req, driftCause(ctx, r, p, *req))
	}
	isBypasser, err := r.Approvals().IsBypasser(ctx, p, policy.ID, string(caller.Principal))
	if err != nil {
		return approvalOutcome{}, err
	}
	if !isBypasser {
		return approvalOutcome{}, fmt.Errorf("%w: not a named emergency bypasser of this policy", domain.ErrUnauthorized)
	}
	reason := audit.SanitizeFreeText(request.Bypass.Reason)
	if reason == "" {
		return approvalOutcome{}, fmt.Errorf("%w: emergency bypass requires a reason", domain.ErrInvalid)
	}
	if len(reason) > 512 {
		return approvalOutcome{}, fmt.Errorf("%w: the bypass reason is too long", domain.ErrInvalid)
	}
	intent, err := NewBypassReauthIntent(coveredEnv, keyIDs)
	if err != nil {
		return approvalOutcome{}, err
	}
	if err := requireCeremony(ctx, auth, az, caller, intent); err != nil {
		return approvalOutcome{}, err
	}
	return approvalOutcome{gated: true, resolveID: req.ID, bypass: true, reason: reason, previewDigest: digest, coveredEnv: coveredEnv}, nil
}

// resolveApprovalAfterPublish flips a merged or bypassed request to its
// terminal state and emits its event, AFTER the materialize has committed the
// revision. Called only when approvalOutcome.resolveID is set.
func resolveApprovalAfterPublish(ctx context.Context, r store.Repos, p authz.Proof, principal domain.PrincipalID,
	outcome approvalOutcome, revision int64, now time.Time) error {
	if outcome.bypass {
		if _, err := r.Approvals().UpdateRequestState(ctx, p, outcome.resolveID, store.ApprovalStateBypassed, "", &now); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventApprovalBypassed, principal,
			audit.Object{Type: "approval-request", ID: outcome.resolveID}, audit.Payload{
				"reason": outcome.reason, "revision": revision, "preview_digest": outcome.previewDigest,
			})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	}
	if _, err := r.Approvals().UpdateRequestState(ctx, p, outcome.resolveID, store.ApprovalStateMerged, "", &now); err != nil {
		return err
	}
	ev, err := domainEvent(ctx, audit.EventApprovalMerged, principal,
		audit.Object{Type: "approval-request", ID: outcome.resolveID}, audit.Payload{
			"revision": revision, "approvals": outcome.approvals, "preview_digest": outcome.previewDigest,
		})
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, p, ev)
}

// driftCause distinguishes an environment that advanced from a draft that was
// edited, for the invalidation cause. The base moving is the checkable one; a
// digest mismatch with an unchanged base means the drafts themselves changed.
func driftCause(ctx context.Context, r store.Repos, p authz.Proof, req store.ApprovalRequest) string {
	base, err := currentRevision(ctx, r, p)
	if err == nil && base != req.BaseRevision {
		return "env_advanced"
	}
	return "draft_edited"
}

func selectionKeyIDs(applies []pendingApply) []string {
	ids := make([]string, 0, len(applies))
	for _, a := range applies {
		ids = append(ids, a.keyID)
	}
	return sortedUnique(ids)
}

// sanitizePurpose bounds and cleans the requester's free-text purpose.
func sanitizePurpose(raw string) (string, error) {
	purpose := audit.SanitizeFreeText(raw)
	if len(purpose) > maxApprovalPurposeBytes {
		return "", fmt.Errorf("%w: the change purpose is too long", domain.ErrInvalid)
	}
	return purpose, nil
}
