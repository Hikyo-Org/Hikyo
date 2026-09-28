package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// Approval-mediated temporary access (#152). Same binding discipline as every
// other proof-bound repo: each method verifies its own registered store
// operation and binds the chain columns exclusively from the verified proof.
// Timestamps are written in the fixed-width form because the sweep
// range-filters review_expires_at and expires_at lexically on sqlite.

// marshalCapabilities encodes capabilities as a JSON array, treating nil as empty.
func marshalCapabilities(caps []string) (string, error) {
	if caps == nil {
		caps = []string{}
	}
	b, err := json.Marshal(caps)
	if err != nil {
		return "", fmt.Errorf("store: encode access capabilities: %w", err)
	}
	return string(b), nil
}

// unmarshalCapabilities decodes a stored capability list; JSON errors include
// the row kind and ID. Capability names are not validated here.
func unmarshalCapabilities(kind, id, raw string) ([]string, error) {
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("store: %s %s capabilities %q: %w", kind, id, raw, err)
	}
	return out, nil
}

// sqliteStampPtr encodes a canonical timestamp, or SQL NULL for a nil pointer.
func sqliteStampPtr(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: fixedStamp(*t), Valid: true}
}

// pgStamp returns a valid PostgreSQL timestamp in UTC at microsecond precision.
func pgStamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: CanonTime(t), Valid: true}
}

// pgStampPtr encodes a canonical timestamp, or SQL NULL for a nil pointer.
func pgStampPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgStamp(*t)
}

// parseStampField parses a stored timestamp into UTC, wrapping parse errors
// with the row kind, ID, and field name.
func parseStampField(kind, id, field, raw string) (time.Time, error) {
	t, err := parseStamp(raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: %s %s %s %q: %w", kind, id, field, raw, err)
	}
	return t.UTC(), nil
}

// parseStampPtr returns nil for SQL NULL, otherwise parsing a UTC timestamp
// and propagating errors with row and field context.
func parseStampPtr(kind, id, field string, raw sql.NullString) (*time.Time, error) {
	if !raw.Valid {
		return nil, nil
	}
	t, err := parseStampField(kind, id, field, raw.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// --- sqlite ---

type sqliteAccess struct {
	q   *sqlitegen.Queries
	tok *authz.TxToken
}

func (r sqliteRepos) Access() AccessRepo {
	return sqliteAccess{q: sqlitegen.New(r.db), tok: r.tok}
}

func accessPolicyFromSqlite(row sqlitegen.AccessPolicy) (AccessPolicy, error) {
	caps, err := unmarshalCapabilities("access policy", row.ID, row.Capabilities)
	if err != nil {
		return AccessPolicy{}, err
	}
	created, err := parseStampField("access policy", row.ID, "created_at", row.CreatedAt)
	if err != nil {
		return AccessPolicy{}, err
	}
	updated, err := parseStampField("access policy", row.ID, "updated_at", row.UpdatedAt)
	if err != nil {
		return AccessPolicy{}, err
	}
	return AccessPolicy{
		ID: row.ID, EnvironmentID: row.EnvironmentID, Capabilities: caps,
		MaxDurationSeconds: int(row.MaxDurationSeconds), MinApprovals: int(row.MinApprovals),
		AllowSelfApproval: row.AllowSelfApproval != 0, RequestTTLSeconds: int(row.RequestTtlSeconds),
		Enabled: row.Enabled != 0, Version: row.Version, CreatedBy: row.CreatedBy,
		CreatedAt: created, UpdatedAt: updated,
	}, nil
}

func (r sqliteAccess) InsertPolicy(ctx context.Context, p authz.Proof, policy NewAccessPolicy) error {
	chain, err := authz.Verify(p, authz.StoreAccessPolicyInsert, r.tok)
	if err != nil {
		return err
	}
	caps, err := marshalCapabilities(policy.Capabilities)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertAccessPolicy(ctx, sqlitegen.InsertAccessPolicyParams{
		ID: policy.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project),
		EnvironmentID: policy.EnvironmentID, Capabilities: caps,
		MaxDurationSeconds: int64(policy.MaxDurationSeconds), MinApprovals: int64(policy.MinApprovals),
		AllowSelfApproval: boolToInt(policy.AllowSelfApproval), RequestTtlSeconds: int64(policy.RequestTTLSeconds),
		Enabled: boolToInt(policy.Enabled), Version: 1, CreatedBy: policy.CreatedBy,
		CreatedAt: fixedStamp(policy.CreatedAt), UpdatedAt: fixedStamp(policy.CreatedAt),
	}))
}

// GetPolicy returns a policy in the proof's project, or ErrNotFound if absent.
// Proof verification, query, and decoding errors propagate.
func (r sqliteAccess) GetPolicy(ctx context.Context, p authz.Proof, id string) (AccessPolicy, error) {
	chain, err := authz.Verify(p, authz.StoreAccessPolicyGet, r.tok)
	if err != nil {
		return AccessPolicy{}, err
	}
	row, err := r.q.GetAccessPolicy(ctx, sqlitegen.GetAccessPolicyParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), ID: id,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return AccessPolicy{}, ErrNotFound
	}
	if err != nil {
		return AccessPolicy{}, err
	}
	return accessPolicyFromSqlite(row)
}

// CoveringPolicy prefers the exact environment policy, including a disabled
// one, over the project-wide policy. No match returns false without an error;
// proof verification, query, and decoding errors propagate.
func (r sqliteAccess) CoveringPolicy(ctx context.Context, p authz.Proof, envID string) (AccessPolicy, bool, error) {
	chain, err := authz.Verify(p, authz.StoreAccessPolicyCovering, r.tok)
	if err != nil {
		return AccessPolicy{}, false, err
	}
	for _, env := range []string{envID, ""} {
		row, err := r.q.GetAccessPolicyForEnvironment(ctx, sqlitegen.GetAccessPolicyForEnvironmentParams{
			OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env,
		})
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return AccessPolicy{}, false, err
		}
		policy, cErr := accessPolicyFromSqlite(row)
		return policy, cErr == nil, cErr
	}
	return AccessPolicy{}, false, nil
}

func (r sqliteAccess) ListPolicies(ctx context.Context, p authz.Proof) ([]AccessPolicy, error) {
	chain, err := authz.Verify(p, authz.StoreAccessPolicyList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListAccessPolicies(ctx, sqlitegen.ListAccessPoliciesParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project),
	})
	if err != nil {
		return nil, err
	}
	out := make([]AccessPolicy, 0, len(rows))
	for _, row := range rows {
		policy, err := accessPolicyFromSqlite(row)
		if err != nil {
			return nil, err
		}
		out = append(out, policy)
	}
	return out, nil
}

// UpdatePolicy replaces mutable fields and increments the version in the
// proof's project. It reports whether a row matched; recognized constraint
// violations wrap ErrConflict, and other errors propagate.
func (r sqliteAccess) UpdatePolicy(ctx context.Context, p authz.Proof, update AccessPolicyUpdate) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreAccessPolicyUpdate, r.tok)
	if err != nil {
		return false, err
	}
	caps, err := marshalCapabilities(update.Capabilities)
	if err != nil {
		return false, err
	}
	n, err := r.q.UpdateAccessPolicy(ctx, sqlitegen.UpdateAccessPolicyParams{
		Capabilities: caps, MaxDurationSeconds: int64(update.MaxDurationSeconds),
		MinApprovals: int64(update.MinApprovals), AllowSelfApproval: boolToInt(update.AllowSelfApproval),
		RequestTtlSeconds: int64(update.RequestTTLSeconds), Enabled: boolToInt(update.Enabled),
		UpdatedAt: fixedStamp(update.UpdatedAt),
		OrgID:     string(chain.Org), ProjectID: string(chain.Project), ID: update.ID,
	})
	return n > 0, constraint(err)
}

func (r sqliteAccess) DeletePolicy(ctx context.Context, p authz.Proof, id string) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreAccessPolicyDelete, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.DeleteAccessPolicy(ctx, sqlitegen.DeleteAccessPolicyParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), ID: id,
	})
	return n > 0, err
}

func (r sqliteAccess) InsertApprover(ctx context.Context, p authz.Proof, a NewApprovalApprover) error {
	chain, err := authz.Verify(p, authz.StoreAccessApproverInsert, r.tok)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertAccessPolicyApprover(ctx, sqlitegen.InsertAccessPolicyApproverParams{
		ID: a.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: a.PolicyID,
		Kind: string(a.Kind), SubjectID: a.SubjectID, ScopeBindingID: a.ScopeBindingID,
	}))
}

func (r sqliteAccess) ListApprovers(ctx context.Context, p authz.Proof, policyID string) ([]ApprovalApprover, error) {
	chain, err := authz.Verify(p, authz.StoreAccessApproverList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListAccessPolicyApprovers(ctx, sqlitegen.ListAccessPolicyApproversParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: policyID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ApprovalApprover, 0, len(rows))
	for _, row := range rows {
		out = append(out, ApprovalApprover{ID: row.ID, PolicyID: row.PolicyID,
			Kind: ApprovalApproverKind(row.Kind), SubjectID: row.SubjectID, ScopeBindingID: row.ScopeBindingID})
	}
	return out, nil
}

func (r sqliteAccess) ClearApprovers(ctx context.Context, p authz.Proof, policyID string) (int64, error) {
	chain, err := authz.Verify(p, authz.StoreAccessApproverClear, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeleteAccessPolicyApprovers(ctx, sqlitegen.DeleteAccessPolicyApproversParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: policyID,
	})
}

func (r sqliteAccess) InsertBypasser(ctx context.Context, p authz.Proof, b NewApprovalBypasser) error {
	chain, err := authz.Verify(p, authz.StoreAccessBypasserInsert, r.tok)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertAccessPolicyBypasser(ctx, sqlitegen.InsertAccessPolicyBypasserParams{
		ID: b.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: b.PolicyID, PrincipalID: b.PrincipalID,
	}))
}

func (r sqliteAccess) ListBypassers(ctx context.Context, p authz.Proof, policyID string) ([]ApprovalBypasser, error) {
	chain, err := authz.Verify(p, authz.StoreAccessBypasserList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListAccessPolicyBypassers(ctx, sqlitegen.ListAccessPolicyBypassersParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: policyID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ApprovalBypasser, 0, len(rows))
	for _, row := range rows {
		out = append(out, ApprovalBypasser{ID: row.ID, PolicyID: row.PolicyID, PrincipalID: row.PrincipalID})
	}
	return out, nil
}

func (r sqliteAccess) ClearBypassers(ctx context.Context, p authz.Proof, policyID string) (int64, error) {
	chain, err := authz.Verify(p, authz.StoreAccessBypasserClear, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeleteAccessPolicyBypassers(ctx, sqlitegen.DeleteAccessPolicyBypassersParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: policyID,
	})
}

// IsBypasser reports membership in the policy's emergency-access roster.
// A missing entry returns false without an error; proof and query errors propagate.
func (r sqliteAccess) IsBypasser(ctx context.Context, p authz.Proof, policyID, principalID string) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreAccessBypasserGet, r.tok)
	if err != nil {
		return false, err
	}
	_, err = r.q.GetAccessPolicyBypasser(ctx, sqlitegen.GetAccessPolicyBypasserParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: policyID, PrincipalID: principalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func accessRequestFromSqlite(row sqlitegen.AccessRequest) (AccessRequest, error) {
	caps, err := unmarshalCapabilities("access request", row.ID, row.Capabilities)
	if err != nil {
		return AccessRequest{}, err
	}
	created, err := parseStampField("access request", row.ID, "created_at", row.CreatedAt)
	if err != nil {
		return AccessRequest{}, err
	}
	review, err := parseStampField("access request", row.ID, "review_expires_at", row.ReviewExpiresAt)
	if err != nil {
		return AccessRequest{}, err
	}
	granted, err := parseStampPtr("access request", row.ID, "granted_at", row.GrantedAt)
	if err != nil {
		return AccessRequest{}, err
	}
	expires, err := parseStampPtr("access request", row.ID, "expires_at", row.ExpiresAt)
	if err != nil {
		return AccessRequest{}, err
	}
	resolved, err := parseStampPtr("access request", row.ID, "resolved_at", row.ResolvedAt)
	if err != nil {
		return AccessRequest{}, err
	}
	return AccessRequest{
		ID: row.ID, EnvironmentID: row.EnvironmentID, PolicyID: row.PolicyID, PolicyVersion: row.PolicyVersion,
		RequesterPrincipalID: row.RequesterPrincipalID, Capabilities: caps, DurationSeconds: int(row.DurationSeconds),
		Reason: row.Reason, Bypassed: row.Bypassed != 0, State: AccessRequestState(row.State),
		InvalidatedCause: row.InvalidatedCause, ResolvedBy: row.ResolvedBy, CreatedAt: created,
		ReviewExpiresAt: review, GrantedAt: granted, ExpiresAt: expires, ResolvedAt: resolved,
	}, nil
}

// InsertRequest writes the request in the proof's environment, ignoring
// req.EnvironmentID. Recognized constraint violations wrap ErrConflict;
// proof verification, encoding, and other database errors propagate.
func (r sqliteAccess) InsertRequest(ctx context.Context, p authz.Proof, req NewAccessRequest) error {
	chain, err := authz.Verify(p, authz.StoreAccessRequestInsert, r.tok)
	if err != nil {
		return err
	}
	caps, err := marshalCapabilities(req.Capabilities)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertAccessRequest(ctx, sqlitegen.InsertAccessRequestParams{
		ID: req.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env),
		PolicyID: req.PolicyID, PolicyVersion: req.PolicyVersion, RequesterPrincipalID: req.RequesterPrincipalID,
		Capabilities: caps, DurationSeconds: int64(req.DurationSeconds), Reason: req.Reason,
		Bypassed: boolToInt(req.Bypassed), State: string(req.State), ResolvedBy: req.ResolvedBy,
		CreatedAt: fixedStamp(req.CreatedAt), ReviewExpiresAt: fixedStamp(req.ReviewExpiresAt),
		GrantedAt: sqliteStampPtr(req.GrantedAt), ExpiresAt: sqliteStampPtr(req.ExpiresAt),
	}))
}

// GetRequest returns a request in the proof's environment, or ErrNotFound
// if absent. Proof verification, query, and decoding errors propagate.
func (r sqliteAccess) GetRequest(ctx context.Context, p authz.Proof, id string) (AccessRequest, error) {
	chain, err := authz.Verify(p, authz.StoreAccessRequestGet, r.tok)
	if err != nil {
		return AccessRequest{}, err
	}
	row, err := r.q.GetAccessRequest(ctx, sqlitegen.GetAccessRequestParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env), ID: id,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return AccessRequest{}, ErrNotFound
	}
	if err != nil {
		return AccessRequest{}, err
	}
	return accessRequestFromSqlite(row)
}

// ListRequests returns at most 200 requests in the proof's environment,
// ordered by creation time descending then ID. Proof verification, query,
// and decoding errors propagate.
func (r sqliteAccess) ListRequests(ctx context.Context, p authz.Proof) ([]AccessRequest, error) {
	chain, err := authz.Verify(p, authz.StoreAccessRequestList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListAccessRequestsForEnvironment(ctx, sqlitegen.ListAccessRequestsForEnvironmentParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env),
	})
	if err != nil {
		return nil, err
	}
	out := make([]AccessRequest, 0, len(rows))
	for _, row := range rows {
		req, err := accessRequestFromSqlite(row)
		if err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, nil
}

// GrantRequest sets grant and expiry times only for an open request in the
// proof's environment. A missing or non-open request returns false without an
// error; proof and database errors propagate. It does not write grant rows.
func (r sqliteAccess) GrantRequest(ctx context.Context, p authz.Proof, id string, grantedAt, expiresAt time.Time) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreAccessRequestGrant, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.GrantAccessRequest(ctx, sqlitegen.GrantAccessRequestParams{
		GrantedAt: sqliteStampPtr(&grantedAt), ExpiresAt: sqliteStampPtr(&expiresAt),
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env), ID: id,
		State: string(AccessStateOpen),
	})
	return n > 0, err
}

// ResolveRequest applies res only when the scoped request still has state
// res.From. A missing or changed request returns false without an error;
// proof and database errors propagate.
func (r sqliteAccess) ResolveRequest(ctx context.Context, p authz.Proof, res AccessResolution) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreAccessRequestResolve, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.ResolveAccessRequest(ctx, sqlitegen.ResolveAccessRequestParams{
		ToState: string(res.To), Cause: res.Cause, ResolvedBy: res.ResolvedBy,
		ResolvedAt: sqliteStampPtr(&res.ResolvedAt),
		OrgID:      string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env),
		ID: res.ID, FromState: string(res.From),
	})
	return n > 0, err
}

func accessVoteFromSqlite(row sqlitegen.AccessVote) (AccessVote, error) {
	created, err := parseStampField("access vote", row.ID, "created_at", row.CreatedAt)
	if err != nil {
		return AccessVote{}, err
	}
	return AccessVote{ID: row.ID, RequestID: row.RequestID, PrincipalID: row.PrincipalID,
		Decision: ApprovalVoteDecision(row.Decision), CreatedAt: created}, nil
}

func (r sqliteAccess) InsertVote(ctx context.Context, p authz.Proof, vote AccessVote) error {
	chain, err := authz.Verify(p, authz.StoreAccessVoteInsert, r.tok)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertAccessVote(ctx, sqlitegen.InsertAccessVoteParams{
		ID: vote.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env),
		RequestID: vote.RequestID, PrincipalID: vote.PrincipalID, Decision: string(vote.Decision),
		CreatedAt: fixedStamp(vote.CreatedAt),
	}))
}

// GetVote returns the principal's vote on the scoped request, or ErrNotFound
// if absent. Proof verification, query, and decoding errors propagate.
func (r sqliteAccess) GetVote(ctx context.Context, p authz.Proof, requestID, principalID string) (AccessVote, error) {
	chain, err := authz.Verify(p, authz.StoreAccessVoteGet, r.tok)
	if err != nil {
		return AccessVote{}, err
	}
	row, err := r.q.GetAccessVote(ctx, sqlitegen.GetAccessVoteParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env),
		RequestID: requestID, PrincipalID: principalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return AccessVote{}, ErrNotFound
	}
	if err != nil {
		return AccessVote{}, err
	}
	return accessVoteFromSqlite(row)
}

func (r sqliteAccess) ListVotes(ctx context.Context, p authz.Proof, requestID string) ([]AccessVote, error) {
	chain, err := authz.Verify(p, authz.StoreAccessVoteList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListAccessVotes(ctx, sqlitegen.ListAccessVotesParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env), RequestID: requestID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]AccessVote, 0, len(rows))
	for _, row := range rows {
		v, err := accessVoteFromSqlite(row)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// SelectDue returns at most 100 open or granted requests across the installation,
// ordered by ID, whose review or grant expiry is at or before now. It requires the
// scheduler proof; proof verification and query errors propagate.
func (r sqliteAccess) SelectDue(ctx context.Context, p authz.Proof, now time.Time) ([]DueAccessRequest, error) {
	if _, err := authz.Verify(p, authz.StoreAccessRequestSelectDue, r.tok); err != nil {
		return nil, err
	}
	rows, err := r.q.SelectDueAccessRequests(ctx, fixedStamp(now))
	if err != nil {
		return nil, err
	}
	out := make([]DueAccessRequest, 0, len(rows))
	for _, row := range rows {
		out = append(out, DueAccessRequest{ID: row.ID, OrgID: row.OrgID, ProjectID: row.ProjectID,
			EnvironmentID: row.EnvironmentID, PolicyID: row.PolicyID,
			RequesterPrincipalID: row.RequesterPrincipalID, State: AccessRequestState(row.State)})
	}
	return out, nil
}

// MarkExpired records expiry only if the request still has state from. It
// requires the scheduler proof and does not recheck the deadline. A missing
// or changed request returns false; proof and database errors propagate.
func (r sqliteAccess) MarkExpired(ctx context.Context, p authz.Proof, id string, from AccessRequestState, now time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StoreAccessRequestMarkExpired, r.tok); err != nil {
		return false, err
	}
	n, err := r.q.MarkAccessRequestExpired(ctx, sqlitegen.MarkAccessRequestExpiredParams{
		ResolvedAt: sqliteStampPtr(&now), ID: id, FromState: string(from),
	})
	return n > 0, err
}

// OperationalCounts returns installation-wide open requests and granted
// requests expiring strictly after now. Open requests awaiting a sweep still
// count. It requires the scheduler proof; proof and query errors propagate.
func (r sqliteAccess) OperationalCounts(ctx context.Context, p authz.Proof, now time.Time) (int64, int64, error) {
	if _, err := authz.Verify(p, authz.StoreAccessRequestCounts, r.tok); err != nil {
		return 0, 0, err
	}
	open, err := r.q.CountOpenAccessRequests(ctx)
	if err != nil {
		return 0, 0, err
	}
	active, err := r.q.CountActiveAccessGrants(ctx, sqliteStampPtr(&now))
	if err != nil {
		return 0, 0, err
	}
	return open, active, nil
}

// --- postgres ---

type pgAccess struct {
	q   *pggen.Queries
	tok *authz.TxToken
}

func (r pgRepos) Access() AccessRepo {
	return pgAccess{q: pggen.New(r.db), tok: r.tok}
}

func accessPolicyFromPg(row pggen.AccessPolicy) (AccessPolicy, error) {
	caps, err := unmarshalCapabilities("access policy", row.ID, row.Capabilities)
	if err != nil {
		return AccessPolicy{}, err
	}
	created, updated := row.CreatedAt.Time.UTC(), row.UpdatedAt.Time.UTC()
	return AccessPolicy{
		ID: row.ID, EnvironmentID: row.EnvironmentID, Capabilities: caps,
		MaxDurationSeconds: int(row.MaxDurationSeconds), MinApprovals: int(row.MinApprovals),
		AllowSelfApproval: row.AllowSelfApproval, RequestTTLSeconds: int(row.RequestTtlSeconds),
		Enabled: row.Enabled, Version: row.Version, CreatedBy: row.CreatedBy,
		CreatedAt: created, UpdatedAt: updated,
	}, nil
}

func (r pgAccess) InsertPolicy(ctx context.Context, p authz.Proof, policy NewAccessPolicy) error {
	chain, err := authz.Verify(p, authz.StoreAccessPolicyInsert, r.tok)
	if err != nil {
		return err
	}
	caps, err := marshalCapabilities(policy.Capabilities)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertAccessPolicy(ctx, pggen.InsertAccessPolicyParams{
		ID: policy.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project),
		EnvironmentID: policy.EnvironmentID, Capabilities: caps,
		MaxDurationSeconds: int32(policy.MaxDurationSeconds), MinApprovals: int32(policy.MinApprovals),
		AllowSelfApproval: (policy.AllowSelfApproval), RequestTtlSeconds: int32(policy.RequestTTLSeconds),
		Enabled: (policy.Enabled), Version: 1, CreatedBy: policy.CreatedBy,
		CreatedAt: pgStamp(policy.CreatedAt), UpdatedAt: pgStamp(policy.CreatedAt),
	}))
}

// GetPolicy returns a policy in the proof's project, or ErrNotFound if absent.
// Proof verification, query, and decoding errors propagate.
func (r pgAccess) GetPolicy(ctx context.Context, p authz.Proof, id string) (AccessPolicy, error) {
	chain, err := authz.Verify(p, authz.StoreAccessPolicyGet, r.tok)
	if err != nil {
		return AccessPolicy{}, err
	}
	row, err := r.q.GetAccessPolicy(ctx, pggen.GetAccessPolicyParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), ID: id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessPolicy{}, ErrNotFound
	}
	if err != nil {
		return AccessPolicy{}, err
	}
	return accessPolicyFromPg(row)
}

// CoveringPolicy prefers the exact environment policy, including a disabled
// one, over the project-wide policy. No match returns false without an error;
// proof verification, query, and decoding errors propagate.
func (r pgAccess) CoveringPolicy(ctx context.Context, p authz.Proof, envID string) (AccessPolicy, bool, error) {
	chain, err := authz.Verify(p, authz.StoreAccessPolicyCovering, r.tok)
	if err != nil {
		return AccessPolicy{}, false, err
	}
	for _, env := range []string{envID, ""} {
		row, err := r.q.GetAccessPolicyForEnvironment(ctx, pggen.GetAccessPolicyForEnvironmentParams{
			OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return AccessPolicy{}, false, err
		}
		policy, cErr := accessPolicyFromPg(row)
		return policy, cErr == nil, cErr
	}
	return AccessPolicy{}, false, nil
}

func (r pgAccess) ListPolicies(ctx context.Context, p authz.Proof) ([]AccessPolicy, error) {
	chain, err := authz.Verify(p, authz.StoreAccessPolicyList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListAccessPolicies(ctx, pggen.ListAccessPoliciesParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project),
	})
	if err != nil {
		return nil, err
	}
	out := make([]AccessPolicy, 0, len(rows))
	for _, row := range rows {
		policy, err := accessPolicyFromPg(row)
		if err != nil {
			return nil, err
		}
		out = append(out, policy)
	}
	return out, nil
}

// UpdatePolicy replaces mutable fields and increments the version in the
// proof's project. It reports whether a row matched; recognized constraint
// violations wrap ErrConflict, and other errors propagate.
func (r pgAccess) UpdatePolicy(ctx context.Context, p authz.Proof, update AccessPolicyUpdate) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreAccessPolicyUpdate, r.tok)
	if err != nil {
		return false, err
	}
	caps, err := marshalCapabilities(update.Capabilities)
	if err != nil {
		return false, err
	}
	n, err := r.q.UpdateAccessPolicy(ctx, pggen.UpdateAccessPolicyParams{
		Capabilities: caps, MaxDurationSeconds: int32(update.MaxDurationSeconds),
		MinApprovals: int32(update.MinApprovals), AllowSelfApproval: (update.AllowSelfApproval),
		RequestTtlSeconds: int32(update.RequestTTLSeconds), Enabled: (update.Enabled),
		UpdatedAt: pgStamp(update.UpdatedAt),
		OrgID:     string(chain.Org), ProjectID: string(chain.Project), ID: update.ID,
	})
	return n > 0, constraint(err)
}

func (r pgAccess) DeletePolicy(ctx context.Context, p authz.Proof, id string) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreAccessPolicyDelete, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.DeleteAccessPolicy(ctx, pggen.DeleteAccessPolicyParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), ID: id,
	})
	return n > 0, err
}

func (r pgAccess) InsertApprover(ctx context.Context, p authz.Proof, a NewApprovalApprover) error {
	chain, err := authz.Verify(p, authz.StoreAccessApproverInsert, r.tok)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertAccessPolicyApprover(ctx, pggen.InsertAccessPolicyApproverParams{
		ID: a.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: a.PolicyID,
		Kind: string(a.Kind), SubjectID: a.SubjectID, ScopeBindingID: a.ScopeBindingID,
	}))
}

func (r pgAccess) ListApprovers(ctx context.Context, p authz.Proof, policyID string) ([]ApprovalApprover, error) {
	chain, err := authz.Verify(p, authz.StoreAccessApproverList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListAccessPolicyApprovers(ctx, pggen.ListAccessPolicyApproversParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: policyID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ApprovalApprover, 0, len(rows))
	for _, row := range rows {
		out = append(out, ApprovalApprover{ID: row.ID, PolicyID: row.PolicyID,
			Kind: ApprovalApproverKind(row.Kind), SubjectID: row.SubjectID, ScopeBindingID: row.ScopeBindingID})
	}
	return out, nil
}

func (r pgAccess) ClearApprovers(ctx context.Context, p authz.Proof, policyID string) (int64, error) {
	chain, err := authz.Verify(p, authz.StoreAccessApproverClear, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeleteAccessPolicyApprovers(ctx, pggen.DeleteAccessPolicyApproversParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: policyID,
	})
}

func (r pgAccess) InsertBypasser(ctx context.Context, p authz.Proof, b NewApprovalBypasser) error {
	chain, err := authz.Verify(p, authz.StoreAccessBypasserInsert, r.tok)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertAccessPolicyBypasser(ctx, pggen.InsertAccessPolicyBypasserParams{
		ID: b.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: b.PolicyID, PrincipalID: b.PrincipalID,
	}))
}

func (r pgAccess) ListBypassers(ctx context.Context, p authz.Proof, policyID string) ([]ApprovalBypasser, error) {
	chain, err := authz.Verify(p, authz.StoreAccessBypasserList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListAccessPolicyBypassers(ctx, pggen.ListAccessPolicyBypassersParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: policyID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ApprovalBypasser, 0, len(rows))
	for _, row := range rows {
		out = append(out, ApprovalBypasser{ID: row.ID, PolicyID: row.PolicyID, PrincipalID: row.PrincipalID})
	}
	return out, nil
}

func (r pgAccess) ClearBypassers(ctx context.Context, p authz.Proof, policyID string) (int64, error) {
	chain, err := authz.Verify(p, authz.StoreAccessBypasserClear, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeleteAccessPolicyBypassers(ctx, pggen.DeleteAccessPolicyBypassersParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: policyID,
	})
}

// IsBypasser reports membership in the policy's emergency-access roster.
// A missing entry returns false without an error; proof and query errors propagate.
func (r pgAccess) IsBypasser(ctx context.Context, p authz.Proof, policyID, principalID string) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreAccessBypasserGet, r.tok)
	if err != nil {
		return false, err
	}
	_, err = r.q.GetAccessPolicyBypasser(ctx, pggen.GetAccessPolicyBypasserParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: policyID, PrincipalID: principalID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func accessRequestFromPg(row pggen.AccessRequest) (AccessRequest, error) {
	caps, err := unmarshalCapabilities("access request", row.ID, row.Capabilities)
	if err != nil {
		return AccessRequest{}, err
	}
	created, review := row.CreatedAt.Time.UTC(), row.ReviewExpiresAt.Time.UTC()
	granted, expires, resolved := nullableTimePg(row.GrantedAt), nullableTimePg(row.ExpiresAt), nullableTimePg(row.ResolvedAt)
	return AccessRequest{
		ID: row.ID, EnvironmentID: row.EnvironmentID, PolicyID: row.PolicyID, PolicyVersion: row.PolicyVersion,
		RequesterPrincipalID: row.RequesterPrincipalID, Capabilities: caps, DurationSeconds: int(row.DurationSeconds),
		Reason: row.Reason, Bypassed: row.Bypassed, State: AccessRequestState(row.State),
		InvalidatedCause: row.InvalidatedCause, ResolvedBy: row.ResolvedBy, CreatedAt: created,
		ReviewExpiresAt: review, GrantedAt: granted, ExpiresAt: expires, ResolvedAt: resolved,
	}, nil
}

// InsertRequest writes the request in the proof's environment, ignoring
// req.EnvironmentID. Recognized constraint violations wrap ErrConflict;
// proof verification, encoding, and other database errors propagate.
func (r pgAccess) InsertRequest(ctx context.Context, p authz.Proof, req NewAccessRequest) error {
	chain, err := authz.Verify(p, authz.StoreAccessRequestInsert, r.tok)
	if err != nil {
		return err
	}
	caps, err := marshalCapabilities(req.Capabilities)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertAccessRequest(ctx, pggen.InsertAccessRequestParams{
		ID: req.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env),
		PolicyID: req.PolicyID, PolicyVersion: req.PolicyVersion, RequesterPrincipalID: req.RequesterPrincipalID,
		Capabilities: caps, DurationSeconds: int32(req.DurationSeconds), Reason: req.Reason,
		Bypassed: (req.Bypassed), State: string(req.State), ResolvedBy: req.ResolvedBy,
		CreatedAt: pgStamp(req.CreatedAt), ReviewExpiresAt: pgStamp(req.ReviewExpiresAt),
		GrantedAt: pgStampPtr(req.GrantedAt), ExpiresAt: pgStampPtr(req.ExpiresAt),
	}))
}

// GetRequest returns a request in the proof's environment, or ErrNotFound
// if absent. Proof verification, query, and decoding errors propagate.
func (r pgAccess) GetRequest(ctx context.Context, p authz.Proof, id string) (AccessRequest, error) {
	chain, err := authz.Verify(p, authz.StoreAccessRequestGet, r.tok)
	if err != nil {
		return AccessRequest{}, err
	}
	row, err := r.q.GetAccessRequest(ctx, pggen.GetAccessRequestParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env), ID: id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessRequest{}, ErrNotFound
	}
	if err != nil {
		return AccessRequest{}, err
	}
	return accessRequestFromPg(row)
}

// ListRequests returns at most 200 requests in the proof's environment,
// ordered by creation time descending then ID. Proof verification, query,
// and decoding errors propagate.
func (r pgAccess) ListRequests(ctx context.Context, p authz.Proof) ([]AccessRequest, error) {
	chain, err := authz.Verify(p, authz.StoreAccessRequestList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListAccessRequestsForEnvironment(ctx, pggen.ListAccessRequestsForEnvironmentParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env),
	})
	if err != nil {
		return nil, err
	}
	out := make([]AccessRequest, 0, len(rows))
	for _, row := range rows {
		req, err := accessRequestFromPg(row)
		if err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, nil
}

// GrantRequest sets grant and expiry times only for an open request in the
// proof's environment. A missing or non-open request returns false without an
// error; proof and database errors propagate. It does not write grant rows.
func (r pgAccess) GrantRequest(ctx context.Context, p authz.Proof, id string, grantedAt, expiresAt time.Time) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreAccessRequestGrant, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.GrantAccessRequest(ctx, pggen.GrantAccessRequestParams{
		GrantedAt: pgStampPtr(&grantedAt), ExpiresAt: pgStampPtr(&expiresAt),
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env), ID: id,
		State: string(AccessStateOpen),
	})
	return n > 0, err
}

// ResolveRequest applies res only when the scoped request still has state
// res.From. A missing or changed request returns false without an error;
// proof and database errors propagate.
func (r pgAccess) ResolveRequest(ctx context.Context, p authz.Proof, res AccessResolution) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreAccessRequestResolve, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.ResolveAccessRequest(ctx, pggen.ResolveAccessRequestParams{
		ToState: string(res.To), Cause: res.Cause, ResolvedBy: res.ResolvedBy,
		ResolvedAt: pgStampPtr(&res.ResolvedAt),
		OrgID:      string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env),
		ID: res.ID, FromState: string(res.From),
	})
	return n > 0, err
}

func accessVoteFromPg(row pggen.AccessVote) (AccessVote, error) {
	created := row.CreatedAt.Time.UTC()
	return AccessVote{ID: row.ID, RequestID: row.RequestID, PrincipalID: row.PrincipalID,
		Decision: ApprovalVoteDecision(row.Decision), CreatedAt: created}, nil
}

func (r pgAccess) InsertVote(ctx context.Context, p authz.Proof, vote AccessVote) error {
	chain, err := authz.Verify(p, authz.StoreAccessVoteInsert, r.tok)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertAccessVote(ctx, pggen.InsertAccessVoteParams{
		ID: vote.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env),
		RequestID: vote.RequestID, PrincipalID: vote.PrincipalID, Decision: string(vote.Decision),
		CreatedAt: pgStamp(vote.CreatedAt),
	}))
}

// GetVote returns the principal's vote on the scoped request, or ErrNotFound
// if absent. Proof verification, query, and decoding errors propagate.
func (r pgAccess) GetVote(ctx context.Context, p authz.Proof, requestID, principalID string) (AccessVote, error) {
	chain, err := authz.Verify(p, authz.StoreAccessVoteGet, r.tok)
	if err != nil {
		return AccessVote{}, err
	}
	row, err := r.q.GetAccessVote(ctx, pggen.GetAccessVoteParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env),
		RequestID: requestID, PrincipalID: principalID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessVote{}, ErrNotFound
	}
	if err != nil {
		return AccessVote{}, err
	}
	return accessVoteFromPg(row)
}

func (r pgAccess) ListVotes(ctx context.Context, p authz.Proof, requestID string) ([]AccessVote, error) {
	chain, err := authz.Verify(p, authz.StoreAccessVoteList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListAccessVotes(ctx, pggen.ListAccessVotesParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: string(chain.Env), RequestID: requestID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]AccessVote, 0, len(rows))
	for _, row := range rows {
		v, err := accessVoteFromPg(row)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// SelectDue returns at most 100 open or granted requests across the installation,
// ordered by ID, whose review or grant expiry is at or before now. It requires the
// scheduler proof; proof verification and query errors propagate.
func (r pgAccess) SelectDue(ctx context.Context, p authz.Proof, now time.Time) ([]DueAccessRequest, error) {
	if _, err := authz.Verify(p, authz.StoreAccessRequestSelectDue, r.tok); err != nil {
		return nil, err
	}
	rows, err := r.q.SelectDueAccessRequests(ctx, pgStamp(now))
	if err != nil {
		return nil, err
	}
	out := make([]DueAccessRequest, 0, len(rows))
	for _, row := range rows {
		out = append(out, DueAccessRequest{ID: row.ID, OrgID: row.OrgID, ProjectID: row.ProjectID,
			EnvironmentID: row.EnvironmentID, PolicyID: row.PolicyID,
			RequesterPrincipalID: row.RequesterPrincipalID, State: AccessRequestState(row.State)})
	}
	return out, nil
}

// MarkExpired records expiry only if the request still has state from. It
// requires the scheduler proof and does not recheck the deadline. A missing
// or changed request returns false; proof and database errors propagate.
func (r pgAccess) MarkExpired(ctx context.Context, p authz.Proof, id string, from AccessRequestState, now time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StoreAccessRequestMarkExpired, r.tok); err != nil {
		return false, err
	}
	n, err := r.q.MarkAccessRequestExpired(ctx, pggen.MarkAccessRequestExpiredParams{
		ResolvedAt: pgStampPtr(&now), ID: id, FromState: string(from),
	})
	return n > 0, err
}

// OperationalCounts returns installation-wide open requests and granted
// requests expiring strictly after now. Open requests awaiting a sweep still
// count. It requires the scheduler proof; proof and query errors propagate.
func (r pgAccess) OperationalCounts(ctx context.Context, p authz.Proof, now time.Time) (int64, int64, error) {
	if _, err := authz.Verify(p, authz.StoreAccessRequestCounts, r.tok); err != nil {
		return 0, 0, err
	}
	open, err := r.q.CountOpenAccessRequests(ctx)
	if err != nil {
		return 0, 0, err
	}
	active, err := r.q.CountActiveAccessGrants(ctx, pgStampPtr(&now))
	if err != nil {
		return 0, 0, err
	}
	return open, active, nil
}

func (r sqliteReadRepos) Access() AccessReader { return r.r.Access() }
func (r pgReadRepos) Access() AccessReader     { return r.r.Access() }
