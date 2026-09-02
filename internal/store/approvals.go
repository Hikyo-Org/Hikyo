package store

import (
	"context"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
)

// Secret-change approvals (#151). The store aggregate behind the policy-bound
// review-and-merge engine. Every method is authorized against its own
// registered store operation and binds the chain columns (org, project, and
// where the statement is environment-addressed, environment) exclusively from
// the verified proof's resolved chain. Policy, request and vote identity, the
// approver subjects and the vote decision are caller data, never chain values.
//
// An approval authorises one exact reviewed change set: a request pins the
// publish preview-token digest over the closed selection plus the requester's
// principal generation, and merge re-derives and compares it. This layer only
// stores and returns the rows; the pinning, quorum and merge live in the
// service.

// ApprovalApproverKind is the closed set of approver subject kinds.
type ApprovalApproverKind string

const (
	// ApprovalApproverPrincipal names one principal directly.
	ApprovalApproverPrincipal ApprovalApproverKind = "principal"
	// ApprovalApproverSCIMGroup names a SCIM group whose current active members
	// are eligible; ScopeBindingID says which binding to resolve them through.
	ApprovalApproverSCIMGroup ApprovalApproverKind = "scim_group"
)

// ApprovalRequestState is the closed request lifecycle. open and approved are
// the two ACTIVE states (resolved_at is NULL for both); the rest are terminal.
type ApprovalRequestState string

const (
	ApprovalStateOpen        ApprovalRequestState = "open"
	ApprovalStateApproved    ApprovalRequestState = "approved"
	ApprovalStateMerged      ApprovalRequestState = "merged"
	ApprovalStateRejected    ApprovalRequestState = "rejected"
	ApprovalStateExpired     ApprovalRequestState = "expired"
	ApprovalStateInvalidated ApprovalRequestState = "invalidated"
	ApprovalStateBypassed    ApprovalRequestState = "bypassed"
)

// ApprovalVoteDecision is the closed set of vote decisions.
type ApprovalVoteDecision string

const (
	ApprovalDecisionApprove ApprovalVoteDecision = "approve"
	ApprovalDecisionReject  ApprovalVoteDecision = "reject"
)

// ApprovalPolicy is one scoped policy row. EnvironmentID is "" for a
// project-wide policy (covers every environment) and a concrete environment id
// otherwise. Version increments on every update so a request pinned to an older
// version fails closed.
type ApprovalPolicy struct {
	ID                string
	EnvironmentID     string
	MinApprovals      int
	AllowSelfApproval bool
	RequestTTLSeconds int
	Enabled           bool
	Version           int64
	CreatedBy         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// NewApprovalPolicy carries the caller-suppliable fields of a policy insert; the
// chain columns are bound from the proof and Version starts at 1.
type NewApprovalPolicy struct {
	ID                string
	EnvironmentID     string
	MinApprovals      int
	AllowSelfApproval bool
	RequestTTLSeconds int
	Enabled           bool
	CreatedBy         string
	CreatedAt         time.Time
}

// ApprovalPolicyUpdate carries the mutable fields of a policy update. Version
// bumps in SQL; the scope (org, project, env) is immutable once created.
type ApprovalPolicyUpdate struct {
	ID                string
	MinApprovals      int
	AllowSelfApproval bool
	RequestTTLSeconds int
	Enabled           bool
	UpdatedAt         time.Time
}

// ApprovalApprover is one approver-set row.
type ApprovalApprover struct {
	ID             string
	PolicyID       string
	Kind           ApprovalApproverKind
	SubjectID      string
	ScopeBindingID string
}

// NewApprovalApprover carries the caller-suppliable fields of an approver
// insert.
type NewApprovalApprover struct {
	ID             string
	PolicyID       string
	Kind           ApprovalApproverKind
	SubjectID      string
	ScopeBindingID string
}

// ApprovalBypasser is one emergency-bypasser row.
type ApprovalBypasser struct {
	ID          string
	PolicyID    string
	PrincipalID string
}

// NewApprovalBypasser carries the caller-suppliable fields of a bypasser insert.
type NewApprovalBypasser struct {
	ID          string
	PolicyID    string
	PrincipalID string
}

// ApprovalRequest is one immutable request pinned to an exact change set.
// ResolvedAt is nil while the request is active (open or approved).
type ApprovalRequest struct {
	ID                   string
	EnvironmentID        string
	PolicyID             string
	PolicyVersion        int64
	RequesterPrincipalID string
	VersionIDs           []string
	ClosedVersionIDs     []string
	KeyIDs               []string
	PreviewTokenDigest   string
	BaseRevision         int64
	Purpose              string
	State                ApprovalRequestState
	InvalidatedCause     string
	CreatedAt            time.Time
	ExpiresAt            time.Time
	ResolvedAt           *time.Time
}

// NewApprovalRequest carries the caller-suppliable fields of a request insert.
// State is always ApprovalStateOpen at creation and ResolvedAt is NULL.
type NewApprovalRequest struct {
	ID                   string
	EnvironmentID        string
	PolicyID             string
	PolicyVersion        int64
	RequesterPrincipalID string
	VersionIDs           []string
	ClosedVersionIDs     []string
	KeyIDs               []string
	PreviewTokenDigest   string
	BaseRevision         int64
	Purpose              string
	CreatedAt            time.Time
	ExpiresAt            time.Time
}

// ApprovalVote is one approver's decision on one request.
type ApprovalVote struct {
	ID          string
	RequestID   string
	PrincipalID string
	Decision    ApprovalVoteDecision
	CreatedAt   time.Time
}

// NewApprovalVote carries the caller-suppliable fields of a vote insert.
type NewApprovalVote struct {
	ID          string
	RequestID   string
	PrincipalID string
	Decision    ApprovalVoteDecision
	CreatedAt   time.Time
}

// ExpiredApprovalRequest is one row the installation-wide expiry sweep found.
// It carries the chain so the scheduler can emit a per-request audit event.
type ExpiredApprovalRequest struct {
	ID                   string
	OrgID                string
	ProjectID            string
	EnvironmentID        string
	PolicyID             string
	RequesterPrincipalID string
	ExpiresAt            time.Time
}

// ApprovalRepo is the proof-bound surface of the change-approval engine. It is
// on the WRITE bundle only: the coverage lookup that admits a publish runs
// inside the same publish transaction, and the read surfaces run beside their
// own lifecycle events, so a read-only twin would have no caller.
type ApprovalRepo interface {
	InsertPolicy(ctx context.Context, p authz.Proof, policy NewApprovalPolicy) error
	GetPolicy(ctx context.Context, p authz.Proof, id string) (ApprovalPolicy, error)
	// CoveringPolicy returns the enabled policy governing publishes to envID:
	// the exact-environment policy if one exists, else the project-wide policy.
	// The bool is false when no policy covers the environment.
	CoveringPolicy(ctx context.Context, p authz.Proof, envID string) (ApprovalPolicy, bool, error)
	ListPolicies(ctx context.Context, p authz.Proof) ([]ApprovalPolicy, error)
	UpdatePolicy(ctx context.Context, p authz.Proof, update ApprovalPolicyUpdate) (bool, error)
	DeletePolicy(ctx context.Context, p authz.Proof, id string) (bool, error)

	InsertApprover(ctx context.Context, p authz.Proof, approver NewApprovalApprover) error
	ListApprovers(ctx context.Context, p authz.Proof, policyID string) ([]ApprovalApprover, error)
	ClearApprovers(ctx context.Context, p authz.Proof, policyID string) (int64, error)

	InsertBypasser(ctx context.Context, p authz.Proof, bypasser NewApprovalBypasser) error
	ListBypassers(ctx context.Context, p authz.Proof, policyID string) ([]ApprovalBypasser, error)
	ClearBypassers(ctx context.Context, p authz.Proof, policyID string) (int64, error)
	IsBypasser(ctx context.Context, p authz.Proof, policyID, principalID string) (bool, error)

	InsertRequest(ctx context.Context, p authz.Proof, request NewApprovalRequest) error
	GetRequest(ctx context.Context, p authz.Proof, id string) (ApprovalRequest, error)
	ListRequests(ctx context.Context, p authz.Proof) ([]ApprovalRequest, error)
	// UpdateRequestState transitions one request. resolvedAt is nil for the
	// approved state and set for every terminal state. It reports whether a row
	// changed, so a caller can tell a real transition from a race that already
	// resolved the request.
	UpdateRequestState(ctx context.Context, p authz.Proof, id string, state ApprovalRequestState, cause string, resolvedAt *time.Time) (bool, error)

	InsertVote(ctx context.Context, p authz.Proof, vote NewApprovalVote) error
	GetVote(ctx context.Context, p authz.Proof, requestID, principalID string) (ApprovalVote, error)
	ListVotes(ctx context.Context, p authz.Proof, requestID string) ([]ApprovalVote, error)

	// SelectExpired returns one bounded installation-wide expiry batch. Repeated
	// scheduler runs commit progress under system authority.
	SelectExpired(ctx context.Context, p authz.Proof, now time.Time) ([]ExpiredApprovalRequest, error)
	MarkExpired(ctx context.Context, p authz.Proof, id string, now time.Time) (bool, error)

	// OperationalCounts returns the installation-wide count of active requests
	// (awaiting review) and expired requests, for the label-free /metrics
	// gauges. Cross-tenant; runs under scheduler authority.
	OperationalCounts(ctx context.Context, p authz.Proof) (active, expired int64, err error)
}
