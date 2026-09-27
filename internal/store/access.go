package store

import (
	"context"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
)

// Approval-mediated temporary access (#152). The store aggregate behind access
// policies, requests and votes. Every method is authorized against its own
// registered store operation and binds the chain columns exclusively from the
// verified proof's resolved chain. The time-bound grant rows themselves are
// NOT here: authorize() reads them, so they live on the resolution surface
// (authz.TxAuthorizer.CreateAccessGrant) beside `grants`.

// AccessRequestState is the closed request lifecycle. open and granted are the
// two ACTIVE states (resolved_at is NULL for both); the rest are terminal.
type AccessRequestState string

const (
	AccessStateOpen        AccessRequestState = "open"
	AccessStateGranted     AccessRequestState = "granted"
	AccessStateRejected    AccessRequestState = "rejected"
	AccessStateCancelled   AccessRequestState = "cancelled"
	AccessStateExpired     AccessRequestState = "expired"
	AccessStateInvalidated AccessRequestState = "invalidated"
	AccessStateRevoked     AccessRequestState = "revoked"
)

// AccessPolicy is one scoped access policy. EnvironmentID is "" for a
// project-wide policy.
type AccessPolicy struct {
	ID                 string
	EnvironmentID      string
	Capabilities       []string
	MaxDurationSeconds int
	MinApprovals       int
	AllowSelfApproval  bool
	RequestTTLSeconds  int
	Enabled            bool
	Version            int64
	CreatedBy          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// NewAccessPolicy carries the caller-suppliable fields of a policy insert.
type NewAccessPolicy struct {
	ID                 string
	EnvironmentID      string
	Capabilities       []string
	MaxDurationSeconds int
	MinApprovals       int
	AllowSelfApproval  bool
	RequestTTLSeconds  int
	Enabled            bool
	CreatedBy          string
	CreatedAt          time.Time
}

// AccessPolicyUpdate carries the mutable fields of a policy update. The
// environment is immutable; version bumps in SQL.
type AccessPolicyUpdate struct {
	ID                 string
	Capabilities       []string
	MaxDurationSeconds int
	MinApprovals       int
	AllowSelfApproval  bool
	RequestTTLSeconds  int
	Enabled            bool
	UpdatedAt          time.Time
}

// AccessRequest is one request row. GrantedAt/ExpiresAt are set once granted;
// ResolvedAt is set for every terminal state.
type AccessRequest struct {
	ID                   string
	EnvironmentID        string
	PolicyID             string
	PolicyVersion        int64
	RequesterPrincipalID string
	Capabilities         []string
	DurationSeconds      int
	Reason               string
	Bypassed             bool
	State                AccessRequestState
	InvalidatedCause     string
	ResolvedBy           string
	CreatedAt            time.Time
	ReviewExpiresAt      time.Time
	GrantedAt            *time.Time
	ExpiresAt            *time.Time
	ResolvedAt           *time.Time
}

// NewAccessRequest carries the fields of a request insert. An ordinary request
// is written open with GrantedAt/ExpiresAt nil; an emergency request is
// written granted with both set.
type NewAccessRequest struct {
	ID                   string
	EnvironmentID        string
	PolicyID             string
	PolicyVersion        int64
	RequesterPrincipalID string
	Capabilities         []string
	DurationSeconds      int
	Reason               string
	Bypassed             bool
	State                AccessRequestState
	ResolvedBy           string
	CreatedAt            time.Time
	ReviewExpiresAt      time.Time
	GrantedAt            *time.Time
	ExpiresAt            *time.Time
}

// AccessResolution is one terminal transition, guarded by the state the caller
// read so a racing resolution is detectable.
type AccessResolution struct {
	ID         string
	From       AccessRequestState
	To         AccessRequestState
	Cause      string
	ResolvedBy string
	ResolvedAt time.Time
}

// AccessVote is one approver's decision on one request.
type AccessVote struct {
	ID          string
	RequestID   string
	PrincipalID string
	Decision    ApprovalVoteDecision
	CreatedAt   time.Time
}

// DueAccessRequest is one row the installation-wide sweep found: an open
// request past its review window or a granted request past its expiry.
type DueAccessRequest struct {
	ID                   string
	OrgID                string
	ProjectID            string
	EnvironmentID        string
	PolicyID             string
	RequesterPrincipalID string
	State                AccessRequestState
}

// AccessReader exposes only label-free operational counts to read transactions.
type AccessReader interface {
	OperationalCounts(ctx context.Context, p authz.Proof, now time.Time) (open, active int64, err error)
}

// AccessRepo is the proof-bound surface of the temporary-access engine. The
// approver and bypasser rows reuse the #151 row shapes.
type AccessRepo interface {
	AccessReader
	InsertPolicy(ctx context.Context, p authz.Proof, policy NewAccessPolicy) error
	GetPolicy(ctx context.Context, p authz.Proof, id string) (AccessPolicy, error)
	// CoveringPolicy returns the policy governing requests to envID: the
	// exact-environment policy if one exists, else the project-wide one,
	// enabled or not. The bool is false when none exists.
	CoveringPolicy(ctx context.Context, p authz.Proof, envID string) (AccessPolicy, bool, error)
	ListPolicies(ctx context.Context, p authz.Proof) ([]AccessPolicy, error)
	UpdatePolicy(ctx context.Context, p authz.Proof, update AccessPolicyUpdate) (bool, error)
	DeletePolicy(ctx context.Context, p authz.Proof, id string) (bool, error)

	InsertApprover(ctx context.Context, p authz.Proof, approver NewApprovalApprover) error
	ListApprovers(ctx context.Context, p authz.Proof, policyID string) ([]ApprovalApprover, error)
	ClearApprovers(ctx context.Context, p authz.Proof, policyID string) (int64, error)

	InsertBypasser(ctx context.Context, p authz.Proof, bypasser NewApprovalBypasser) error
	ListBypassers(ctx context.Context, p authz.Proof, policyID string) ([]ApprovalBypasser, error)
	ClearBypassers(ctx context.Context, p authz.Proof, policyID string) (int64, error)
	IsBypasser(ctx context.Context, p authz.Proof, policyID, principalID string) (bool, error)

	InsertRequest(ctx context.Context, p authz.Proof, request NewAccessRequest) error
	GetRequest(ctx context.Context, p authz.Proof, id string) (AccessRequest, error)
	ListRequests(ctx context.Context, p authz.Proof) ([]AccessRequest, error)
	// GrantRequest moves an open request to granted; false when it was no
	// longer open.
	GrantRequest(ctx context.Context, p authz.Proof, id string, grantedAt, expiresAt time.Time) (bool, error)
	// ResolveRequest applies one guarded terminal transition; false when the
	// request had already moved.
	ResolveRequest(ctx context.Context, p authz.Proof, res AccessResolution) (bool, error)

	InsertVote(ctx context.Context, p authz.Proof, vote AccessVote) error
	GetVote(ctx context.Context, p authz.Proof, requestID, principalID string) (AccessVote, error)
	ListVotes(ctx context.Context, p authz.Proof, requestID string) ([]AccessVote, error)

	// SelectDue returns one bounded installation-wide sweep batch.
	SelectDue(ctx context.Context, p authz.Proof, now time.Time) ([]DueAccessRequest, error)
	MarkExpired(ctx context.Context, p authz.Proof, id string, from AccessRequestState, now time.Time) (bool, error)
}
