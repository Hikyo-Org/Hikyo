package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Approval-mediated temporary access (#152).
//
// A human who already has an identity asks for named capabilities in ONE
// environment, for a bounded duration, with a reason. The environment's access
// policy says which capabilities are requestable, the longest duration, who
// approves and how many must, and who may take emergency access. Approval
// writes time-bound grant rows with an absolute expiry.
//
// The load-bearing invariant, stated once: temporary access is never a
// permanent grant. The rows live in access_grants, not in `grants`, so no
// origin movement can promote one; the chokepoint's own grant lookup admits a
// row only while its expires_at is after the transaction clock, so expiry
// takes effect on the next protected operation of every session on every node
// with no sweep and no client cooperation; and the grant writer's grantor
// bound reads `grants` alone, so temporary authority can be used but never
// re-granted. The hourly sweep is bookkeeping: it releases the rows, rotates
// the holder's sessions and records the evidence.

const (
	// maxAccessReasonBytes bounds the requester's free-text reason.
	maxAccessReasonBytes = 512
	// defaultEmergencyAccess is the emergency duration when none is named; the
	// policy's maximum still caps it.
	defaultEmergencyAccess = time.Hour
)

// requestableCapabilities is the closed set an access policy may offer. Only
// environment-deep, non-administrative capabilities: no manage-*, no
// instance or project capability, nothing that could itself confer authority
// over grants.
var requestableCapabilities = []domain.Capability{
	domain.CapRead, domain.CapReveal, domain.CapRevealHistory,
	domain.CapEdit, domain.CapPublish, domain.CapPin,
}

// RequestableCapabilities returns the closed set in its canonical order.
func RequestableCapabilities() []string {
	out := make([]string, 0, len(requestableCapabilities))
	for _, c := range requestableCapabilities {
		out = append(out, string(c))
	}
	return out
}

var (
	// ErrAccessNotRequestable refuses a request in an environment no enabled
	// access policy covers.
	ErrAccessNotRequestable = fmt.Errorf("%w: service: no enabled access policy covers this environment", domain.ErrConflict)
	// ErrAccessExceedsPolicy refuses a request for a capability or duration the
	// covering policy does not offer.
	ErrAccessExceedsPolicy = fmt.Errorf("%w: service: the request exceeds what the access policy offers", domain.ErrInvalid)
	// ErrAccessHumanOnly refuses a machine or session-less caller: temporary
	// access is for humans who can be held to a reason and a reauthentication.
	ErrAccessHumanOnly = fmt.Errorf("%w: service: temporary access is requested and decided by humans in an interactive session", domain.ErrUnauthorized)
	// ErrAccessNotApprover refuses a vote by a principal outside the policy's
	// current approver set.
	ErrAccessNotApprover = fmt.Errorf("%w: service: not an approver of this access policy", domain.ErrUnauthorized)
	// ErrAccessApproverCannotGrant refuses an approval by an approver who could
	// not grant every requested capability themselves: approval never grants
	// what the approver could not.
	ErrAccessApproverCannotGrant = fmt.Errorf("%w: service: an approver may approve only capabilities they could grant", domain.ErrUnauthorized)
	// ErrAccessSelfApproval refuses a requester approving their own request
	// under a policy that does not permit it.
	ErrAccessSelfApproval = fmt.Errorf("%w: service: the requester cannot approve their own access under this policy", domain.ErrUnauthorized)
	// ErrAccessNotBypasser refuses emergency access by a principal the policy
	// does not name.
	ErrAccessNotBypasser = fmt.Errorf("%w: service: not a named emergency-access principal of this policy", domain.ErrUnauthorized)
	// ErrAccessNotYours refuses a cancel by anyone but the requester, and a
	// revoke by anyone but the holder, an approver or a member manager.
	ErrAccessNotYours = fmt.Errorf("%w: service: only the requester, an approver or a member manager may do this", domain.ErrUnauthorized)
)

// Access is the temporary-access service.
type Access struct {
	DB   *store.DB
	Auth *Auth
	Now  func() time.Time
}

// now returns the injected or wall clock in UTC, truncated to microseconds.
func (s *Access) now() time.Time {
	return store.CanonTime(nowOr(s.Now))
}

// AccessPolicyInput is a policy create/update from the admin surface.
type AccessPolicyInput struct {
	EnvironmentID      string // "" = every environment in the project
	Capabilities       []string
	MaxDurationSeconds int
	MinApprovals       int
	AllowSelfApproval  bool
	RequestTTLSeconds  int
	Enabled            bool
	Approvers          []ApprovalApproverSpec
	Bypassers          []string
}

// AccessPolicyView is a policy as the admin surface reads it.
type AccessPolicyView struct {
	PrincipalNames     map[string]string
	ID                 string
	EnvironmentID      string
	Capabilities       []string
	MaxDurationSeconds int
	MinApprovals       int
	AllowSelfApproval  bool
	RequestTTLSeconds  int
	Enabled            bool
	Version            int64
	Approvers          []ApprovalApproverSpec
	Bypassers          []string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// AccessOffer is what a requester may ask for in one environment: the
// covering policy's requestable surface, without its approver roster.
type AccessOffer struct {
	PolicyID           string
	PolicyVersion      int64
	Capabilities       []string
	MaxDurationSeconds int
	MinApprovals       int
	Enabled            bool
	// CallerMayBypass reports whether the caller is a named emergency-access
	// principal, so a surface can offer the emergency path only to them.
	CallerMayBypass bool
}

// AccessVoteView is one recorded vote.
type AccessVoteView struct {
	PrincipalName string
	PrincipalID   string
	Decision      string
	CreatedAt     time.Time
}

// AccessRequestView is a request as the review surface reads it.
type AccessRequestView struct {
	CanApprove       bool
	RequesterName    string
	ID               string
	EnvironmentID    string
	PolicyID         string
	PolicyVersion    int64
	Requester        string
	Capabilities     []string
	DurationSeconds  int
	Reason           string
	Bypassed         bool
	State            string
	InvalidatedCause string
	ResolvedBy       string
	CreatedAt        time.Time
	ReviewExpiresAt  time.Time
	GrantedAt        *time.Time
	ExpiresAt        *time.Time
	ResolvedAt       *time.Time
	MinApprovals     int
	Approvals        int
	Votes            []AccessVoteView
}

// AccessQueue is an environment's request queue with the offer that governs it.
type AccessQueue struct {
	Offer    *AccessOffer
	Requests []AccessRequestView
}

// AccessRequestInput is a request from the requester.
type AccessRequestInput struct {
	Capabilities    []string
	DurationSeconds int
	Reason          string
}

// --- policy administration ---

// CreatePolicy adds an access policy. Project-scoped, manage-members.
func (s *Access) CreatePolicy(ctx context.Context, actor Actor, scope domain.Scope, input AccessPolicyInput) (AccessPolicyView, error) {
	caps, err := validateAccessPolicyInput(input)
	if err != nil {
		return AccessPolicyView{}, err
	}
	now := s.now()
	var view AccessPolicyView
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpAccessPolicyWrite, scope, now)
		if err != nil {
			return err
		}
		if err := validatePolicyEnvironment(ctx, az, input.EnvironmentID); err != nil {
			return err
		}
		if input.Enabled && len(input.Bypassers) > 0 {
			if err := requireEmergencyGrantBound(ctx, az, caller.Principal, scope, input.EnvironmentID, caps); err != nil {
				return err
			}
		}
		id, err := newID("xpol")
		if err != nil {
			return err
		}
		if err := r.Access().InsertPolicy(ctx, p, store.NewAccessPolicy{
			ID: id, EnvironmentID: input.EnvironmentID, Capabilities: caps,
			MaxDurationSeconds: input.MaxDurationSeconds, MinApprovals: input.MinApprovals,
			AllowSelfApproval: input.AllowSelfApproval, RequestTTLSeconds: input.RequestTTLSeconds,
			Enabled: input.Enabled, CreatedBy: string(caller.Principal), CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := writeAccessPolicyMembers(ctx, r, p, id, input); err != nil {
			return err
		}
		if err := recordAccessPolicyChange(ctx, r, p, caller.Principal, id, "created", input.EnvironmentID, caps, input, len(input.Approvers), len(input.Bypassers)); err != nil {
			return err
		}
		view, err = loadAccessPolicyView(ctx, r, az, p, id)
		return err
	})
	return view, err
}

// UpdatePolicy replaces a policy's fields and member sets and bumps its
// version, making open requests pinned to the old version stale for new votes.
// Granted access keeps its absolute expiry: a policy change never extends it.
func (s *Access) UpdatePolicy(ctx context.Context, actor Actor, scope domain.Scope, id string, input AccessPolicyInput) (AccessPolicyView, error) {
	caps, err := validateAccessPolicyInput(input)
	if err != nil {
		return AccessPolicyView{}, err
	}
	now := s.now()
	var view AccessPolicyView
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpAccessPolicyWrite, scope, now)
		if err != nil {
			return err
		}
		current, err := r.Access().GetPolicy(ctx, p, id)
		if err != nil {
			return err
		}
		if input.EnvironmentID != current.EnvironmentID {
			return fmt.Errorf("%w: an access policy's environment cannot be changed; create a replacement policy", domain.ErrInvalid)
		}
		// An emergency allowlist is a standing delegation. Creating or widening
		// it obeys the same permanent grantor bound as a direct grant.
		if input.Enabled && len(input.Bypassers) > 0 {
			bypassers, err := r.Access().ListBypassers(ctx, p, id)
			if err != nil {
				return err
			}
			widening := !current.Enabled || input.MaxDurationSeconds > current.MaxDurationSeconds
			for _, capability := range caps {
				widening = widening || !slices.Contains(current.Capabilities, capability)
			}
			for _, principal := range input.Bypassers {
				widening = widening || !slices.ContainsFunc(bypassers, func(b store.ApprovalBypasser) bool { return b.PrincipalID == principal })
			}
			if widening {
				if err := requireEmergencyGrantBound(ctx, az, caller.Principal, scope, input.EnvironmentID, caps); err != nil {
					return err
				}
			}
		}
		updated, err := r.Access().UpdatePolicy(ctx, p, store.AccessPolicyUpdate{
			ID: id, Capabilities: caps, MaxDurationSeconds: input.MaxDurationSeconds,
			MinApprovals: input.MinApprovals, AllowSelfApproval: input.AllowSelfApproval,
			RequestTTLSeconds: input.RequestTTLSeconds, Enabled: input.Enabled, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		if !updated {
			return domain.ErrNotFound
		}
		if _, err := r.Access().ClearApprovers(ctx, p, id); err != nil {
			return err
		}
		if _, err := r.Access().ClearBypassers(ctx, p, id); err != nil {
			return err
		}
		if err := writeAccessPolicyMembers(ctx, r, p, id, input); err != nil {
			return err
		}
		if err := recordAccessPolicyChange(ctx, r, p, caller.Principal, id, "updated", input.EnvironmentID, caps, input, len(input.Approvers), len(input.Bypassers)); err != nil {
			return err
		}
		view, err = loadAccessPolicyView(ctx, r, az, p, id)
		return err
	})
	return view, err
}

// DeletePolicy removes a policy. Open requests under it fail closed at their
// next decision; granted access keeps its absolute expiry.
func (s *Access) DeletePolicy(ctx context.Context, actor Actor, scope domain.Scope, id string) error {
	now := s.now()
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpAccessPolicyWrite, scope, now)
		if err != nil {
			return err
		}
		policy, err := r.Access().GetPolicy(ctx, p, id)
		if err != nil {
			return err
		}
		// The deletion record states what was removed: read the member sets
		// before the cascade takes them.
		approvers, err := r.Access().ListApprovers(ctx, p, id)
		if err != nil {
			return err
		}
		bypassers, err := r.Access().ListBypassers(ctx, p, id)
		if err != nil {
			return err
		}
		deleted, err := r.Access().DeletePolicy(ctx, p, id)
		if err != nil {
			return err
		}
		if !deleted {
			return domain.ErrNotFound
		}
		return recordAccessPolicyChange(ctx, r, p, caller.Principal, id, "deleted", policy.EnvironmentID,
			policy.Capabilities, AccessPolicyInput{
				MaxDurationSeconds: policy.MaxDurationSeconds, MinApprovals: policy.MinApprovals,
				AllowSelfApproval: policy.AllowSelfApproval, Enabled: policy.Enabled,
			}, len(approvers), len(bypassers))
	})
}

// ListPolicies returns the project's access policies.
func (s *Access) ListPolicies(ctx context.Context, actor Actor, scope domain.Scope) ([]AccessPolicyView, error) {
	now := s.now()
	var out []AccessPolicyView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpAccessPolicyRead, scope, now)
		if err != nil {
			return err
		}
		policies, err := r.Access().ListPolicies(ctx, p)
		if err != nil {
			return err
		}
		out = make([]AccessPolicyView, 0, len(policies))
		for _, policy := range policies {
			view, err := accessPolicyViewWithMembers(ctx, r, az, p, policy)
			if err != nil {
				return err
			}
			out = append(out, view)
		}
		ev, err := domainEvent(ctx, audit.EventAccessPolicyRead, caller.Principal,
			audit.Object{Type: "project", ID: string(scope.Project)}, audit.Payload{"policy_count": len(policies)})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
	return out, err
}

// --- requests ---

// Queue returns up to 200 of an environment's requests, newest first, and its
// covering offer. Offer is nil when no policy covers the environment; a disabled
// environment policy still takes precedence over a project-wide policy.
// Approval counts are recomputed for open requests without resolving stale ones.
// Authorization, storage, and view-loading errors are returned to the caller.
func (s *Access) Queue(ctx context.Context, actor Actor, scope domain.Scope) (AccessQueue, error) {
	now := s.now()
	var out AccessQueue
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpAccessRequestRead, scope, now)
		if err != nil {
			return err
		}
		policy, ok, err := r.Access().CoveringPolicy(ctx, p, string(scope.Env))
		if err != nil {
			return err
		}
		if ok {
			bypasser, err := r.Access().IsBypasser(ctx, p, policy.ID, string(caller.Principal))
			if err != nil {
				return err
			}
			out.Offer = &AccessOffer{
				PolicyID: policy.ID, PolicyVersion: policy.Version,
				Capabilities: slices.Clone(policy.Capabilities), MaxDurationSeconds: policy.MaxDurationSeconds,
				MinApprovals: policy.MinApprovals, Enabled: policy.Enabled,
				CallerMayBypass: bypasser && policy.Enabled,
			}
		}
		requests, err := r.Access().ListRequests(ctx, p)
		if err != nil {
			return err
		}
		out.Requests = make([]AccessRequestView, 0, len(requests))
		views := newAccessRequestViews()
		if ok {
			views.policies[policy.ID] = &accessPolicyVotes{policy: policy, eligible: make(map[domain.PrincipalID]bool)}
		}
		for _, req := range requests {
			view, err := views.view(ctx, r, az, p, scope, req)
			if err != nil {
				return err
			}
			if req.State == store.AccessStateOpen && interactiveHuman(caller) && now.Before(req.ReviewExpiresAt) {
				policy, err := views.policy(ctx, r, p, req.PolicyID)
				if err != nil {
					return err
				}
				if policy != nil && policy.policy.Enabled && policy.policy.Version == req.PolicyVersion &&
					(req.RequesterPrincipalID != string(caller.Principal) || policy.policy.AllowSelfApproval) {
					view.CanApprove, err = views.canApprove(ctx, r, az, p, scope, policy, caller.Principal, req.Capabilities)
					if err != nil {
						return err
					}
				}
			}
			out.Requests = append(out.Requests, view)
		}
		return nil
	})
	return out, err
}

// Request files a request for temporary access in the addressed environment.
// The request is immutable: capabilities, duration, reason and the policy
// version it was filed under are written once and never updated.
// The caller must be a human in a session with read access to the environment.
// DurationSeconds must be positive and within the covering policy's maximum;
// capabilities must be a nonempty subset of its offer. The sanitized reason
// must contain 1 to 512 bytes. A successful request records an access.requested event.
// Missing or disabled policies return ErrAccessNotRequestable; invalid capability
// or duration choices wrap ErrAccessExceedsPolicy, and invalid reasons wrap
// domain.ErrInvalid. Authorization, storage, and audit errors are propagated.
func (s *Access) Request(ctx context.Context, actor Actor, scope domain.Scope, input AccessRequestInput) (AccessRequestView, error) {
	now := s.now()
	var view AccessRequestView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpAccessRequestCreate, scope, now)
		if err != nil {
			return err
		}
		if !interactiveHuman(caller) {
			return ErrAccessHumanOnly
		}
		policy, ok, err := r.Access().CoveringPolicy(ctx, p, string(scope.Env))
		if err != nil {
			return err
		}
		if !ok || !policy.Enabled {
			return ErrAccessNotRequestable
		}
		caps, err := accessCapabilitiesWithin(input.Capabilities, policy.Capabilities)
		if err != nil {
			return err
		}
		if input.DurationSeconds <= 0 || input.DurationSeconds > policy.MaxDurationSeconds {
			return fmt.Errorf("%w: duration must be between 1 and %d seconds", ErrAccessExceedsPolicy, policy.MaxDurationSeconds)
		}
		reason, err := accessReason(input.Reason)
		if err != nil {
			return err
		}
		id, err := newID("xreq")
		if err != nil {
			return err
		}
		req := store.NewAccessRequest{
			ID: id, EnvironmentID: string(scope.Env), PolicyID: policy.ID, PolicyVersion: policy.Version,
			RequesterPrincipalID: string(caller.Principal), Capabilities: caps,
			DurationSeconds: input.DurationSeconds, Reason: reason, State: store.AccessStateOpen,
			CreatedAt: now, ReviewExpiresAt: now.Add(time.Duration(policy.RequestTTLSeconds) * time.Second),
		}
		if err := r.Access().InsertRequest(ctx, p, req); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventAccessRequested, caller.Principal,
			audit.Object{Type: "access-request", ID: id}, audit.Payload{
				"policy_id": policy.ID, "policy_version": policy.Version,
				"capabilities": caps, "duration_seconds": input.DurationSeconds, "reason": reason,
			})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return err
		}
		stored, err := r.Access().GetRequest(ctx, p, id)
		if err != nil {
			return err
		}
		view, err = accessRequestViewWithVotes(ctx, r, az, p, scope, stored)
		return err
	})
	return view, err
}

// Vote records one approver's decision. The approver must be a
// currently eligible member of the policy's approver set. An approval also
// requires authority to grant every requested capability and permission for
// self-approval when the caller is the requester. The approve that reaches the quorum
// writes the time-bound grant rows in the same transaction. A repeated
// identical decision returns the current view without requiring renewed approver
// eligibility; a conflicting one wraps domain.ErrConflict. A rejection resolves
// the request immediately. New votes require an open, unexpired review window.
// A missing, disabled, or changed policy commits invalidation before returning
// domain.ErrConflict. Invalid decisions wrap domain.ErrInvalid; human-session,
// eligibility, authorization, storage, and audit errors are propagated.
func (s *Access) Vote(ctx context.Context, actor Actor, scope domain.Scope, requestID, decisionRaw string) (AccessRequestView, error) {
	decision := store.ApprovalVoteDecision(decisionRaw)
	if decision != store.ApprovalDecisionApprove && decision != store.ApprovalDecisionReject {
		return AccessRequestView{}, fmt.Errorf("%w: a vote is approve or reject", domain.ErrInvalid)
	}
	now := s.now()
	var view AccessRequestView
	var refusal error
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		refusal = nil
		caller, p, err := authorize(ctx, az, actor, authz.OpAccessVote, scope, now)
		if err != nil {
			return err
		}
		if !interactiveHuman(caller) {
			return ErrAccessHumanOnly
		}
		req, err := r.Access().GetRequest(ctx, p, requestID)
		if err != nil {
			return err
		}
		existing, existingErr := r.Access().GetVote(ctx, p, requestID, string(caller.Principal))
		if existingErr != nil && !errors.Is(existingErr, domain.ErrNotFound) {
			return existingErr
		}
		if existingErr == nil {
			if existing.Decision != decision {
				return fmt.Errorf("%w: this approver already cast a %s vote", domain.ErrConflict, existing.Decision)
			}
			view, err = accessRequestViewWithVotes(ctx, r, az, p, scope, req)
			return err
		}
		if req.State != store.AccessStateOpen {
			return fmt.Errorf("%w: the request is %s", domain.ErrConflict, req.State)
		}
		if !now.Before(req.ReviewExpiresAt) {
			return fmt.Errorf("%w: the request's review window has lapsed", domain.ErrConflict)
		}
		policy, err := r.Access().GetPolicy(ctx, p, req.PolicyID)
		cause := ""
		switch {
		case errors.Is(err, domain.ErrNotFound):
			cause = "policy_changed"
		case err != nil:
			return err
		case !policy.Enabled:
			cause = "policy_disabled"
		case policy.Version != req.PolicyVersion:
			cause = "policy_changed"
		}
		if cause != "" {
			// Commit the invalidation, refuse after the transaction.
			if err := commitAccessInvalidation(ctx, r, p, caller.Principal, req, cause, now); err != nil {
				return err
			}
			refusal = accessStaleRefusal(cause)
			return nil
		}
		approvers, err := r.Access().ListApprovers(ctx, p, req.PolicyID)
		if err != nil {
			return err
		}
		eligible, err := approverEligible(ctx, r, az, p, approvers, caller.Principal)
		if err != nil {
			return err
		}
		if !eligible {
			return ErrAccessNotApprover
		}
		self := caller.Principal == domain.PrincipalID(req.RequesterPrincipalID)
		if decision == store.ApprovalDecisionApprove {
			if self && !policy.AllowSelfApproval {
				return ErrAccessSelfApproval
			}
			can, err := canGrantAll(ctx, az, caller.Principal, req.Capabilities, scope)
			if err != nil {
				return err
			}
			if !can {
				return ErrAccessApproverCannotGrant
			}
		}
		voteID, err := newID("xvote")
		if err != nil {
			return err
		}
		if err := r.Access().InsertVote(ctx, p, store.AccessVote{
			ID: voteID, RequestID: requestID, PrincipalID: string(caller.Principal), Decision: decision, CreatedAt: now,
		}); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventAccessVoted, caller.Principal,
			audit.Object{Type: "access-request", ID: req.ID}, audit.Payload{
				"decision": string(decision), "self_approval": self,
			})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return err
		}
		if decision == store.ApprovalDecisionReject {
			if _, err := r.Access().ResolveRequest(ctx, p, store.AccessResolution{
				ID: req.ID, From: store.AccessStateOpen, To: store.AccessStateRejected,
				ResolvedBy: string(caller.Principal), ResolvedAt: now,
			}); err != nil {
				return err
			}
		} else {
			approvals, err := countAccessApprovals(ctx, r, az, p, scope, approvers, req, policy)
			if err != nil {
				return err
			}
			if approvals >= policy.MinApprovals {
				if err := grantAccess(ctx, r, az, p, scope, caller.Principal, req, policy, approvals, now); err != nil {
					return err
				}
			}
		}
		fresh, err := r.Access().GetRequest(ctx, p, requestID)
		if err != nil {
			return err
		}
		view, err = accessRequestViewWithVotes(ctx, r, az, p, scope, fresh)
		return err
	})
	if err != nil {
		return AccessRequestView{}, err
	}
	if refusal != nil {
		return AccessRequestView{}, refusal
	}
	return view, nil
}

// Cancel withdraws the caller's own open request. Idempotent on a request the
// caller already cancelled.
func (s *Access) Cancel(ctx context.Context, actor Actor, scope domain.Scope, requestID string) (AccessRequestView, error) {
	now := s.now()
	var view AccessRequestView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpAccessCancel, scope, now)
		if err != nil {
			return err
		}
		req, err := r.Access().GetRequest(ctx, p, requestID)
		if err != nil {
			return err
		}
		if caller.Principal != domain.PrincipalID(req.RequesterPrincipalID) {
			return ErrAccessNotYours
		}
		switch req.State {
		case store.AccessStateCancelled:
			view = accessRequestView(req)
			return nil
		case store.AccessStateOpen:
		default:
			return fmt.Errorf("%w: the request is %s", domain.ErrConflict, req.State)
		}
		moved, err := r.Access().ResolveRequest(ctx, p, store.AccessResolution{
			ID: req.ID, From: store.AccessStateOpen, To: store.AccessStateCancelled,
			ResolvedBy: string(caller.Principal), ResolvedAt: now,
		})
		if err != nil {
			return err
		}
		if !moved {
			return fmt.Errorf("%w: the request was resolved concurrently", domain.ErrConflict)
		}
		ev, err := domainEvent(ctx, audit.EventAccessCancelled, caller.Principal,
			audit.Object{Type: "access-request", ID: req.ID}, audit.Payload{})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return err
		}
		fresh, err := r.Access().GetRequest(ctx, p, requestID)
		if err != nil {
			return err
		}
		view = accessRequestView(fresh)
		return nil
	})
	return view, err
}

// Revoke ends granted access before its expiry: the rows are released, the
// holder's sessions rotate, and the request resolves revoked. The holder may
// relinquish their own access; an eligible approver of the policy or a member
// manager of the project may revoke anyone's. Idempotent on an already revoked
// request.
func (s *Access) Revoke(ctx context.Context, actor Actor, scope domain.Scope, requestID string) (AccessRequestView, error) {
	now := s.now()
	var view AccessRequestView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpAccessRevoke, scope, now)
		if err != nil {
			return err
		}
		req, err := r.Access().GetRequest(ctx, p, requestID)
		if err != nil {
			return err
		}
		holder := domain.PrincipalID(req.RequesterPrincipalID)
		self := caller.Principal == holder
		if !self {
			allowed, err := mayRevokeAccess(ctx, r, az, p, caller, scope, req)
			if err != nil {
				return err
			}
			if !allowed {
				return ErrAccessNotYours
			}
		}
		switch req.State {
		case store.AccessStateRevoked:
			view = accessRequestView(req)
			return nil
		case store.AccessStateGranted:
		default:
			return fmt.Errorf("%w: the request is %s", domain.ErrConflict, req.State)
		}
		released, err := az.DeleteAccessGrantsForRequest(ctx, holder, req.ID)
		if err != nil {
			return err
		}
		moved, err := r.Access().ResolveRequest(ctx, p, store.AccessResolution{
			ID: req.ID, From: store.AccessStateGranted, To: store.AccessStateRevoked,
			ResolvedBy: string(caller.Principal), ResolvedAt: now,
		})
		if err != nil {
			return err
		}
		if !moved {
			return fmt.Errorf("%w: the request was resolved concurrently", domain.ErrConflict)
		}
		// Narrowing authority rotates the holder's sessions in the same
		// transaction, exactly as a grant revocation does.
		if err := invalidateGrantChange(ctx, az, holder); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventAccessRevoked, caller.Principal,
			audit.Object{Type: "access-request", ID: req.ID}, audit.Payload{
				"target_principal": string(holder), "capabilities": slices.Clone(req.Capabilities),
				"released_rows": released, "self_revoked": self,
			})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return err
		}
		fresh, err := r.Access().GetRequest(ctx, p, requestID)
		if err != nil {
			return err
		}
		view = accessRequestView(fresh)
		return nil
	})
	return view, err
}

// EmergencyAccess takes the policy's capabilities without the quorum. Only a
// named emergency-access principal of the covering, enabled policy may; it
// takes a current reauthentication ceremony bound to the environment and a
// reason, and it is time-bound like every temporary grant. Capabilities must be
// a nonempty subset of the offer. A zero DurationSeconds defaults to one hour
// capped by the policy; an explicit duration outside the policy bounds wraps
// ErrAccessExceedsPolicy. It never touches the policy.
// Human-session, policy, capability, reason, and authorization checks return
// the same errors as Request; an unlisted principal gets ErrAccessNotBypasser.
// Missing Auth returns ErrNoCeremonySeam. Reauthentication, storage, and audit
// errors are propagated; success consumes a single-decision window or refreshes
// a sliding window, and records a granted request, temporary grants, and an
// access.bypassed event.
func (s *Access) EmergencyAccess(ctx context.Context, actor Actor, scope domain.Scope, input AccessRequestInput) (AccessRequestView, error) {
	now := s.now()
	var view AccessRequestView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpAccessBypass, scope, now)
		if err != nil {
			return err
		}
		if !interactiveHuman(caller) {
			return ErrAccessHumanOnly
		}
		policy, ok, err := r.Access().CoveringPolicy(ctx, p, string(scope.Env))
		if err != nil {
			return err
		}
		if !ok || !policy.Enabled {
			return ErrAccessNotRequestable
		}
		bypasser, err := r.Access().IsBypasser(ctx, p, policy.ID, string(caller.Principal))
		if err != nil {
			return err
		}
		if !bypasser {
			return ErrAccessNotBypasser
		}
		caps, err := accessCapabilitiesWithin(input.Capabilities, policy.Capabilities)
		if err != nil {
			return err
		}
		// Bound seconds before conversion, which can overflow time.Duration.
		if input.DurationSeconds < 0 || input.DurationSeconds > policy.MaxDurationSeconds {
			return fmt.Errorf("%w: duration must be between 1 and %d seconds", ErrAccessExceedsPolicy, policy.MaxDurationSeconds)
		}
		maxDuration := time.Duration(policy.MaxDurationSeconds) * time.Second
		duration := time.Duration(input.DurationSeconds) * time.Second
		if input.DurationSeconds == 0 {
			duration = min(defaultEmergencyAccess, maxDuration)
		}
		if duration <= 0 || duration > maxDuration {
			return fmt.Errorf("%w: duration must be between 1 and %d seconds", ErrAccessExceedsPolicy, policy.MaxDurationSeconds)
		}
		reason, err := accessReason(input.Reason)
		if err != nil {
			return err
		}
		if s.Auth == nil {
			return ErrNoCeremonySeam
		}
		intent, err := NewAccessBypassReauthIntent(string(scope.Env))
		if err != nil {
			return err
		}
		// Called directly, not through requireCeremony: the unit is the
		// environment alone, and an empty key set must not waive the ceremony.
		// The window refusals surface as the ceremony's own 403s, exactly as the
		// change-approval bypass does, so clients drive the same reauthentication.
		if err := s.Auth.ConsumeReauthWindow(ctx, az, caller.SessionID, intent, s.Auth.now()); err != nil {
			return err
		}
		id, err := newID("xreq")
		if err != nil {
			return err
		}
		expires := now.Add(duration)
		durationSeconds := int(duration / time.Second)
		if err := r.Access().InsertRequest(ctx, p, store.NewAccessRequest{
			ID: id, EnvironmentID: string(scope.Env), PolicyID: policy.ID, PolicyVersion: policy.Version,
			RequesterPrincipalID: string(caller.Principal), Capabilities: caps,
			DurationSeconds: durationSeconds, Reason: reason, Bypassed: true,
			State: store.AccessStateGranted, ResolvedBy: string(caller.Principal),
			CreatedAt: now, ReviewExpiresAt: now, GrantedAt: &now, ExpiresAt: &expires,
		}); err != nil {
			return err
		}
		if err := writeAccessGrantRows(ctx, az, caller.Principal, scope, id, caps, now, expires); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventAccessBypassed, caller.Principal,
			audit.Object{Type: "access-request", ID: id}, audit.Payload{
				"policy_id": policy.ID, "policy_version": policy.Version, "capabilities": caps,
				"duration_seconds": durationSeconds, "expires_at": expires.Format(time.RFC3339Nano),
				"reason": reason,
			})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return err
		}
		stored, err := r.Access().GetRequest(ctx, p, id)
		if err != nil {
			return err
		}
		view = accessRequestView(stored)
		return nil
	})
	return view, err
}

// ExpireDue processes at most 100 due requests and returns the number expired
// in the committed batch, or zero and the transaction error. Requests are due
// at or after their deadline. Open requests whose review window lapsed resolve
// expired; granted requests past their absolute expiry release
// their rows, rotate the holder's sessions and resolve expired. The chokepoint
// already stopped honoring the rows at expires_at; this is the bookkeeping and
// the evidence. Under HA the scheduler context's lease fences the batch.
func (s *Access) ExpireDue(ctx context.Context) (int, error) {
	now := s.now()
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (int, error) {
		p, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
		if err != nil {
			return 0, err
		}
		due, err := r.Access().SelectDue(ctx, p, now)
		if err != nil {
			return 0, err
		}
		expired := 0
		for _, row := range due {
			holder := domain.PrincipalID(row.RequesterPrincipalID)
			var released int64
			phase := "review"
			if row.State == store.AccessStateGranted {
				phase = "grant"
				released, err = az.DeleteAccessGrantsForRequest(ctx, holder, row.ID)
				if err != nil {
					return expired, err
				}
			}
			marked, err := r.Access().MarkExpired(ctx, p, row.ID, row.State, now)
			if err != nil {
				return expired, err
			}
			if !marked {
				continue
			}
			if phase == "grant" {
				if err := invalidateGrantChange(ctx, az, holder); err != nil {
					return expired, err
				}
			}
			scoped, err := az.ScopedSystemAuthority(ctx, authz.SiteScheduler, domain.Scope{
				Org: domain.OrgID(row.OrgID), Project: domain.ProjectID(row.ProjectID), Env: domain.EnvID(row.EnvironmentID),
			})
			if err != nil {
				return expired, err
			}
			ev, err := newAuditEvent(ctx, audit.EventAccessExpired, "",
				audit.Object{Type: "access-request", ID: row.ID}, audit.OutcomeSuccess, "", audit.Payload{
					"phase": phase, "target_principal": string(holder), "released_rows": released,
					"expired_at": now.Format(time.RFC3339Nano),
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
}

// DrainExpired runs at most 100 expiry batches, stopping when fewer than 100
// requests expire in a batch. It returns the first batch error; reaching the
// batch limit returns nil even if a backlog remains.
func (s *Access) DrainExpired(ctx context.Context) error {
	for range 100 {
		n, err := s.ExpireDue(ctx)
		if err != nil {
			return err
		}
		if n < 100 {
			return nil
		}
	}
	return nil
}

// OperationalCounts returns the installation-wide open-request and
// active-grant counts for the label-free /metrics gauges. Open includes requests
// awaiting the expiry sweep. Active counts granted requests whose expiry is
// strictly after now, rather than individual capability rows. Transaction and
// query errors are returned; counts should be used only when err is nil.
func (s *Access) OperationalCounts(ctx context.Context) (open, active int64, err error) {
	now := s.now()
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, pErr := authz.SystemAuthority(authz.SiteScheduler, az.Token())
		if pErr != nil {
			return pErr
		}
		open, active, pErr = r.Access().OperationalCounts(ctx, p, now)
		return pErr
	})
	return open, active, err
}

// --- helpers ---

// interactiveHuman reports whether the caller is a human in a session.
// Temporary access is requested, decided and taken by people who can be held
// to a reason; local host authority and machine identities are refused.
func interactiveHuman(caller authz.Identity) bool {
	return caller.SessionID != "" && caller.Class == domain.ClassHuman
}

// validateAccessPolicyInput checks duration, review, and membership bounds and
// returns sorted, unique requestable capabilities. Invalid policy fields wrap
// domain.ErrInvalid; invalid capabilities wrap ErrAccessExceedsPolicy.
func validateAccessPolicyInput(input AccessPolicyInput) ([]string, error) {
	if input.MaxDurationSeconds <= 0 || input.MaxDurationSeconds > math.MaxInt32 {
		return nil, fmt.Errorf("%w: max_duration_seconds must be between 1 and %d", domain.ErrInvalid, math.MaxInt32)
	}
	if err := validatePolicyInput(ApprovalPolicyInput{
		MinApprovals: input.MinApprovals, RequestTTLSeconds: input.RequestTTLSeconds,
		Approvers: input.Approvers, Bypassers: input.Bypassers,
	}); err != nil {
		return nil, err
	}
	if len(input.Approvers) == 0 {
		return nil, fmt.Errorf("%w: an access policy needs at least one approver", domain.ErrInvalid)
	}
	return accessCapabilitiesWithin(input.Capabilities, RequestableCapabilities())
}

// accessCapabilitiesWithin canonicalizes a capability list and refuses any
// member outside allowed, an empty list, or an unknown capability.
func accessCapabilitiesWithin(requested, allowed []string) ([]string, error) {
	out := sortedUnique(requested)
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: name at least one capability", ErrAccessExceedsPolicy)
	}
	for _, c := range out {
		if !slices.Contains(RequestableCapabilities(), c) {
			return nil, fmt.Errorf("%w: %q is not a requestable capability", ErrAccessExceedsPolicy, c)
		}
		if !slices.Contains(allowed, c) {
			return nil, fmt.Errorf("%w: %q is not offered here", ErrAccessExceedsPolicy, c)
		}
	}
	return out, nil
}

// accessReason sanitizes free text for audit storage and returns it if it has
// 1 to 512 bytes. An empty or longer sanitized result wraps domain.ErrInvalid.
func accessReason(raw string) (string, error) {
	reason := audit.SanitizeFreeText(raw)
	if reason == "" {
		return "", fmt.Errorf("%w: a reason is required", domain.ErrInvalid)
	}
	if len(reason) > maxAccessReasonBytes {
		return "", fmt.Errorf("%w: the reason is too long", domain.ErrInvalid)
	}
	return reason, nil
}

// writeAccessPolicyMembers inserts the supplied approvers and emergency-access
// principals without clearing existing members. ID-generation and store errors
// are returned for the caller to roll back the transaction.
func writeAccessPolicyMembers(ctx context.Context, r store.Repos, p authz.Proof, policyID string, input AccessPolicyInput) error {
	for _, a := range input.Approvers {
		id, err := newID("xapr")
		if err != nil {
			return err
		}
		if err := r.Access().InsertApprover(ctx, p, store.NewApprovalApprover{
			ID: id, PolicyID: policyID, Kind: store.ApprovalApproverKind(a.Kind),
			SubjectID: a.SubjectID, ScopeBindingID: a.BindingID,
		}); err != nil {
			return err
		}
	}
	for _, principalID := range input.Bypassers {
		id, err := newID("xbyp")
		if err != nil {
			return err
		}
		if err := r.Access().InsertBypasser(ctx, p, store.NewApprovalBypasser{
			ID: id, PolicyID: policyID, PrincipalID: principalID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// recordAccessPolicyChange records the policy settings and member counts in a
// tenant audit event, propagating event-construction and audit-storage errors.
func recordAccessPolicyChange(ctx context.Context, r store.Repos, p authz.Proof, principal domain.PrincipalID,
	policyID, action, envID string, caps []string, input AccessPolicyInput, approvers, bypassers int) error {
	ev, err := domainEvent(ctx, audit.EventAccessPolicyChanged, principal,
		audit.Object{Type: "access-policy", ID: policyID}, audit.Payload{
			"action": action, "environment": envID, "capabilities": slices.Clone(caps),
			"max_duration_seconds": input.MaxDurationSeconds, "min_approvals": input.MinApprovals,
			"self_approval": input.AllowSelfApproval, "enabled": input.Enabled,
			"approver_count": approvers, "bypasser_count": bypassers,
		})
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, p, ev)
}

// commitAccessInvalidation attempts to resolve an open request as invalidated
// and records the cause in its audit event. It returns write errors; the caller
// must commit the transaction before returning the stale-request refusal.
func commitAccessInvalidation(ctx context.Context, r store.Repos, p authz.Proof, principal domain.PrincipalID,
	req store.AccessRequest, cause string, now time.Time) error {
	if _, err := r.Access().ResolveRequest(ctx, p, store.AccessResolution{
		ID: req.ID, From: store.AccessStateOpen, To: store.AccessStateInvalidated, Cause: cause,
		ResolvedBy: string(principal), ResolvedAt: now,
	}); err != nil {
		return err
	}
	ev, err := domainEvent(ctx, audit.EventAccessInvalidated, principal,
		audit.Object{Type: "access-request", ID: req.ID}, audit.Payload{"cause": cause})
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, p, ev)
}

// accessStaleRefusal wraps domain.ErrConflict with the committed invalidation cause.
func accessStaleRefusal(cause string) error {
	return fmt.Errorf("%w: the access request was invalidated (%s)", domain.ErrConflict, cause)
}

// requireEmergencyGrantBound applies the ordinary grantor bound to a
// standing emergency delegation. A project-wide policy must be backed by
// authority at project scope, including environments created in the future.
func requireEmergencyGrantBound(ctx context.Context, az *authz.TxAuthorizer, principal domain.PrincipalID, scope domain.Scope, env string, caps []string) error {
	scope.Env = domain.EnvID(env)
	can, err := canGrantAll(ctx, az, principal, caps, scope)
	if err != nil {
		return err
	}
	if !can {
		return ErrGrantorLacksCapability
	}
	return nil
}

// canGrantAll reports whether principal could grant every capability at scope
// through the ordinary grant surface: it holds the capability there, or holds
// org- or instance-scope manage-members. Only PERMANENT grants count
// (GrantRowsForPrincipal reads `grants`), so temporary authority never
// approves more temporary authority.
func canGrantAll(ctx context.Context, az *authz.TxAuthorizer, principal domain.PrincipalID, caps []string, scope domain.Scope) (bool, error) {
	rows, err := az.GrantRowsForPrincipal(ctx, principal)
	if err != nil {
		return false, err
	}
	return canGrantAccessCapabilities(rows, caps, scope), nil
}

func canGrantAccessCapabilities(rows []authz.GrantRow, caps []string, scope domain.Scope) bool {
	if mayGrantUnheld(rows, scope) {
		return true
	}
	for _, c := range caps {
		if !holds(rows, domain.Capability(c), scope) {
			return false
		}
	}
	return true
}

// countAccessApprovals counts approve votes from principals who are STILL
// eligible approvers, still hold the vote operation, and could still grant
// every requested capability; the requester counts only under a policy that
// permits self-approval. Recomputed live at every decision.
func countAccessApprovals(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof,
	scope domain.Scope, approvers []store.ApprovalApprover, req store.AccessRequest, policy store.AccessPolicy) (int, error) {
	votes, err := r.Access().ListVotes(ctx, p, req.ID)
	if err != nil {
		return 0, err
	}
	return countAccessApprovalVotes(votes, req, policy, func(voter domain.PrincipalID) (bool, error) {
		eligible, err := approverEligible(ctx, r, az, p, approvers, voter)
		if err != nil || !eligible {
			return false, err
		}
		holdsVote, err := az.CallerHolds(ctx, authz.Identity{Principal: voter}, authz.OpAccessVote, scope)
		if err != nil || !holdsVote {
			return false, err
		}
		return canGrantAll(ctx, az, voter, req.Capabilities, scope)
	})
}

func countAccessApprovalVotes(votes []store.AccessVote, req store.AccessRequest, policy store.AccessPolicy,
	canApprove func(domain.PrincipalID) (bool, error)) (int, error) {
	count := 0
	for _, v := range votes {
		if v.Decision != store.ApprovalDecisionApprove || (v.PrincipalID == req.RequesterPrincipalID && !policy.AllowSelfApproval) {
			continue
		}
		can, err := canApprove(domain.PrincipalID(v.PrincipalID))
		if err != nil {
			return 0, err
		}
		if can {
			count++
		}
	}
	return count, nil
}

// grantAccess moves an open request to granted and writes one time-bound row
// per capability. The expiry is absolute and never beyond the policy maximum
// at the pinned version.
func grantAccess(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, scope domain.Scope, approver domain.PrincipalID,
	req store.AccessRequest, policy store.AccessPolicy, approvals int, now time.Time) error {
	duration := min(req.DurationSeconds, policy.MaxDurationSeconds)
	expires := now.Add(time.Duration(duration) * time.Second)
	moved, err := r.Access().GrantRequest(ctx, p, req.ID, now, expires)
	if err != nil {
		return err
	}
	if !moved {
		return fmt.Errorf("%w: the request was resolved concurrently", domain.ErrConflict)
	}
	holder := domain.PrincipalID(req.RequesterPrincipalID)
	if err := writeAccessGrantRows(ctx, az, holder, scope, req.ID, req.Capabilities, now, expires); err != nil {
		return err
	}
	ev, err := domainEvent(ctx, audit.EventAccessGranted, approver,
		audit.Object{Type: "access-request", ID: req.ID}, audit.Payload{
			"target_principal": string(holder), "capabilities": slices.Clone(req.Capabilities),
			"expires_at": expires.Format(time.RFC3339Nano), "approvals": approvals,
		})
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, p, ev)
}

// writeAccessGrantRows adds one environment grant per capability, all sharing
// the request and absolute expiry. It returns the first ID-generation or grant
// write error; the caller owns rollback of any rows already written.
func writeAccessGrantRows(ctx context.Context, az *authz.TxAuthorizer, holder domain.PrincipalID, scope domain.Scope,
	requestID string, caps []string, now, expires time.Time) error {
	for _, c := range caps {
		id, err := newID("xgrt")
		if err != nil {
			return err
		}
		if err := az.CreateAccessGrant(ctx, id, holder, domain.Grant{Capability: domain.Capability(c), Scope: scope},
			requestID, now, expires); err != nil {
			return err
		}
	}
	return nil
}

// mayRevokeAccess reports whether a caller other than the holder may end the
// access: an eligible approver of the request's policy (if it still exists) or
// a member manager of the project.
func mayRevokeAccess(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof,
	caller authz.Identity, scope domain.Scope, req store.AccessRequest) (bool, error) {
	manager, err := az.CallerHolds(ctx, caller, authz.OpAccessPolicyWrite, domain.Scope{Org: scope.Org, Project: scope.Project})
	if err != nil || manager {
		return manager, err
	}
	if _, err := r.Access().GetPolicy(ctx, p, req.PolicyID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	approvers, err := r.Access().ListApprovers(ctx, p, req.PolicyID)
	if err != nil {
		return false, err
	}
	return approverEligible(ctx, r, az, p, approvers, caller.Principal)
}

// --- views ---

// accessPolicyViewWithMembers adds approver and emergency-access rosters and
// available principal names to a policy view. Member and name lookup errors
// are propagated; principals without a name are omitted from PrincipalNames.
func accessPolicyViewWithMembers(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, policy store.AccessPolicy) (AccessPolicyView, error) {
	approvers, err := r.Access().ListApprovers(ctx, p, policy.ID)
	if err != nil {
		return AccessPolicyView{}, err
	}
	bypassers, err := r.Access().ListBypassers(ctx, p, policy.ID)
	if err != nil {
		return AccessPolicyView{}, err
	}
	view := AccessPolicyView{
		ID: policy.ID, EnvironmentID: policy.EnvironmentID, Capabilities: slices.Clone(policy.Capabilities),
		MaxDurationSeconds: policy.MaxDurationSeconds, MinApprovals: policy.MinApprovals,
		AllowSelfApproval: policy.AllowSelfApproval, RequestTTLSeconds: policy.RequestTTLSeconds,
		Enabled: policy.Enabled, Version: policy.Version, CreatedAt: policy.CreatedAt, UpdatedAt: policy.UpdatedAt,
		PrincipalNames: map[string]string{},
	}
	names := newPrincipalNames()
	ids := []string{}
	for _, a := range approvers {
		view.Approvers = append(view.Approvers, ApprovalApproverSpec{Kind: string(a.Kind), SubjectID: a.SubjectID, BindingID: a.ScopeBindingID})
		if a.Kind == store.ApprovalApproverPrincipal {
			ids = append(ids, a.SubjectID)
		}
	}
	for _, b := range bypassers {
		view.Bypassers = append(view.Bypassers, b.PrincipalID)
		ids = append(ids, b.PrincipalID)
	}
	for _, id := range ids {
		name, err := names.get(ctx, az, domain.PrincipalID(id))
		if err != nil {
			return AccessPolicyView{}, err
		}
		if name != "" {
			view.PrincipalNames[id] = name
		}
	}
	return view, nil
}

// loadAccessPolicyView loads a policy and its member view, propagating missing
// policy, member, and principal-name lookup errors.
func loadAccessPolicyView(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, id string) (AccessPolicyView, error) {
	policy, err := r.Access().GetPolicy(ctx, p, id)
	if err != nil {
		return AccessPolicyView{}, err
	}
	return accessPolicyViewWithMembers(ctx, r, az, p, policy)
}

// accessRequestView copies stored request fields and capabilities into a view
// without loading names, votes, or approval counts.
func accessRequestView(req store.AccessRequest) AccessRequestView {
	return AccessRequestView{
		ID: req.ID, EnvironmentID: req.EnvironmentID, PolicyID: req.PolicyID, PolicyVersion: req.PolicyVersion,
		Requester: req.RequesterPrincipalID, Capabilities: slices.Clone(req.Capabilities),
		DurationSeconds: req.DurationSeconds, Reason: req.Reason, Bypassed: req.Bypassed,
		State: string(req.State), InvalidatedCause: req.InvalidatedCause, ResolvedBy: req.ResolvedBy,
		CreatedAt: req.CreatedAt, ReviewExpiresAt: req.ReviewExpiresAt,
		GrantedAt: req.GrantedAt, ExpiresAt: req.ExpiresAt, ResolvedAt: req.ResolvedAt,
	}
}

// accessRequestViewWithVotes adds names and recorded votes, and recomputes
// eligible approvals for open requests against the current policy. A missing
// policy leaves approval counts at zero; other lookup errors propagate. It does
// not invalidate requests whose policy version or review deadline has changed.
func accessRequestViewWithVotes(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof,
	scope domain.Scope, req store.AccessRequest) (AccessRequestView, error) {
	return newAccessRequestViews().view(ctx, r, az, p, scope, req)
}

// accessRequestViews lives only within one Queue transaction and environment.
// Mutation responses get a fresh instance, so decisions never reuse old authority.
type accessRequestViews struct {
	names    *principalNames
	policies map[string]*accessPolicyVotes
	voters   map[domain.PrincipalID]accessVoterAuthority
}

type accessPolicyVotes struct {
	approversLoaded bool
	policy          store.AccessPolicy
	approvers       []store.ApprovalApprover
	eligible        map[domain.PrincipalID]bool
}

type accessVoterAuthority struct {
	holdsVote bool
	grants    []authz.GrantRow
}

func newAccessRequestViews() *accessRequestViews {
	return &accessRequestViews{names: newPrincipalNames(), policies: make(map[string]*accessPolicyVotes), voters: make(map[domain.PrincipalID]accessVoterAuthority)}
}

func (b *accessRequestViews) policy(ctx context.Context, r store.Repos, p authz.Proof, id string) (*accessPolicyVotes, error) {
	entry, loaded := b.policies[id]
	if loaded && (entry == nil || entry.approversLoaded) {
		return entry, nil
	}
	if !loaded {
		policy, err := r.Access().GetPolicy(ctx, p, id)
		if errors.Is(err, domain.ErrNotFound) {
			b.policies[id] = nil
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		entry = &accessPolicyVotes{policy: policy, eligible: make(map[domain.PrincipalID]bool)}
	}
	approvers, err := r.Access().ListApprovers(ctx, p, id)
	if err != nil {
		return nil, err
	}
	entry.approvers, entry.approversLoaded = approvers, true
	b.policies[id] = entry
	return entry, nil
}

func (b *accessRequestViews) canApprove(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof,
	scope domain.Scope, policy *accessPolicyVotes, voter domain.PrincipalID, caps []string) (bool, error) {
	eligible, ok := policy.eligible[voter]
	if !ok {
		var err error
		eligible, err = approverEligible(ctx, r, az, p, policy.approvers, voter)
		if err != nil {
			return false, err
		}
		policy.eligible[voter] = eligible
	}
	if !eligible {
		return false, nil
	}
	authority, ok := b.voters[voter]
	if !ok {
		var err error
		authority.holdsVote, err = az.CallerHolds(ctx, authz.Identity{Principal: voter}, authz.OpAccessVote, scope)
		if err != nil {
			return false, err
		}
		if authority.holdsVote {
			authority.grants, err = az.GrantRowsForPrincipal(ctx, voter)
			if err != nil {
				return false, err
			}
		}
		b.voters[voter] = authority
	}
	if !authority.holdsVote {
		return false, nil
	}
	return canGrantAccessCapabilities(authority.grants, caps, scope), nil
}

func (b *accessRequestViews) view(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof,
	scope domain.Scope, req store.AccessRequest) (AccessRequestView, error) {
	view := accessRequestView(req)
	var err error
	view.RequesterName, err = b.names.get(ctx, az, domain.PrincipalID(req.RequesterPrincipalID))
	if err != nil {
		return AccessRequestView{}, err
	}
	votes, err := r.Access().ListVotes(ctx, p, req.ID)
	if err != nil {
		return AccessRequestView{}, err
	}
	for _, v := range votes {
		name, err := b.names.get(ctx, az, domain.PrincipalID(v.PrincipalID))
		if err != nil {
			return AccessRequestView{}, err
		}
		view.Votes = append(view.Votes, AccessVoteView{PrincipalID: v.PrincipalID, PrincipalName: name,
			Decision: string(v.Decision), CreatedAt: v.CreatedAt})
	}
	if req.State == store.AccessStateOpen {
		policy, err := b.policy(ctx, r, p, req.PolicyID)
		if err != nil {
			return AccessRequestView{}, err
		}
		if policy != nil {
			view.MinApprovals = policy.policy.MinApprovals
			view.Approvals, err = countAccessApprovalVotes(votes, req, policy.policy, func(voter domain.PrincipalID) (bool, error) {
				return b.canApprove(ctx, r, az, p, scope, policy, voter, req.Capabilities)
			})
			if err != nil {
				return AccessRequestView{}, err
			}
		}
	}
	return view, nil
}
