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

// Secret-change approvals (#151). Same binding discipline as every other
// proof-bound repo: each method verifies its own registered store operation at
// the boundary and binds the chain columns (org, project, and where the
// statement is environment-addressed, environment) exclusively from the
// verified proof's resolved chain. version_ids / closed_version_ids are JSON
// arrays of pending-change version ids, marshalled here and never trusted as a
// chain value.

func marshalIDs(ids []string) (string, error) {
	if ids == nil {
		ids = []string{}
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return "", fmt.Errorf("store: encode approval version ids: %w", err)
	}
	return string(b), nil
}

func unmarshalIDs(kind, id, raw string) ([]string, error) {
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("store: %s %s version ids %q: %w", kind, id, raw, err)
	}
	return out, nil
}

func nullableTime(raw sql.NullString) (*time.Time, error) {
	if !raw.Valid {
		return nil, nil
	}
	t, err := time.Parse(timeFormat, raw.String)
	if err != nil {
		return nil, fmt.Errorf("store: approval resolved_at %q: %w", raw.String, err)
	}
	utc := t.UTC()
	return &utc, nil
}

func nullableTimePg(raw pgtype.Timestamptz) *time.Time {
	if !raw.Valid {
		return nil
	}
	utc := raw.Time.UTC()
	return &utc
}

func sqliteResolvedAt(resolvedAt *time.Time) sql.NullString {
	if resolvedAt == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: CanonTime(*resolvedAt).Format(timeFormat), Valid: true}
}

func pgResolvedAt(resolvedAt *time.Time) pgtype.Timestamptz {
	if resolvedAt == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: CanonTime(*resolvedAt), Valid: true}
}

// --- sqlite ---

type sqliteApprovals struct {
	q   *sqlitegen.Queries
	tok *authz.TxToken
}

func (r sqliteRepos) Approvals() ApprovalRepo {
	return sqliteApprovals{q: sqlitegen.New(r.db), tok: r.tok}
}

func (r sqliteApprovals) InsertPolicy(ctx context.Context, p authz.Proof, policy NewApprovalPolicy) error {
	chain, err := authz.Verify(p, authz.StoreApprovalPolicyInsert, r.tok)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertApprovalPolicy(ctx, sqlitegen.InsertApprovalPolicyParams{
		ID:                policy.ID,
		OrgID:             string(chain.Org),
		ProjectID:         string(chain.Project),
		EnvironmentID:     policy.EnvironmentID,
		MinApprovals:      int64(policy.MinApprovals),
		AllowSelfApproval: boolToInt(policy.AllowSelfApproval),
		RequestTtlSeconds: int64(policy.RequestTTLSeconds),
		Enabled:           boolToInt(policy.Enabled),
		Version:           1,
		CreatedBy:         policy.CreatedBy,
		CreatedAt:         CanonTime(policy.CreatedAt).Format(timeFormat),
		UpdatedAt:         CanonTime(policy.CreatedAt).Format(timeFormat),
	}))
}

func policyFromSqlite(row sqlitegen.ApprovalPolicy) (ApprovalPolicy, error) {
	created, err := time.Parse(timeFormat, row.CreatedAt)
	if err != nil {
		return ApprovalPolicy{}, fmt.Errorf("store: approval policy %s created_at %q: %w", row.ID, row.CreatedAt, err)
	}
	updated, err := time.Parse(timeFormat, row.UpdatedAt)
	if err != nil {
		return ApprovalPolicy{}, fmt.Errorf("store: approval policy %s updated_at %q: %w", row.ID, row.UpdatedAt, err)
	}
	return ApprovalPolicy{
		ID:                row.ID,
		EnvironmentID:     row.EnvironmentID,
		MinApprovals:      int(row.MinApprovals),
		AllowSelfApproval: row.AllowSelfApproval != 0,
		RequestTTLSeconds: int(row.RequestTtlSeconds),
		Enabled:           row.Enabled != 0,
		Version:           row.Version,
		CreatedBy:         row.CreatedBy,
		CreatedAt:         created.UTC(),
		UpdatedAt:         updated.UTC(),
	}, nil
}

func (r sqliteApprovals) GetPolicy(ctx context.Context, p authz.Proof, id string) (ApprovalPolicy, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalPolicyGet, r.tok)
	if err != nil {
		return ApprovalPolicy{}, err
	}
	row, err := r.q.GetApprovalPolicy(ctx, sqlitegen.GetApprovalPolicyParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), ID: id,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovalPolicy{}, ErrNotFound
	}
	if err != nil {
		return ApprovalPolicy{}, err
	}
	return policyFromSqlite(row)
}

func (r sqliteApprovals) CoveringPolicy(ctx context.Context, p authz.Proof, envID string) (ApprovalPolicy, bool, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalPolicyCovering, r.tok)
	if err != nil {
		return ApprovalPolicy{}, false, err
	}
	exact, err := r.q.GetApprovalPolicyForEnvironment(ctx, sqlitegen.GetApprovalPolicyForEnvironmentParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: envID,
	})
	if err == nil {
		policy, cErr := policyFromSqlite(exact)
		return policy, cErr == nil, cErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ApprovalPolicy{}, false, err
	}
	wide, err := r.q.GetApprovalPolicyProjectWide(ctx, sqlitegen.GetApprovalPolicyProjectWideParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovalPolicy{}, false, nil
	}
	if err != nil {
		return ApprovalPolicy{}, false, err
	}
	policy, cErr := policyFromSqlite(wide)
	return policy, cErr == nil, cErr
}

func (r sqliteApprovals) ListPolicies(ctx context.Context, p authz.Proof) ([]ApprovalPolicy, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalPolicyList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListApprovalPolicies(ctx, sqlitegen.ListApprovalPoliciesParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project),
	})
	if err != nil {
		return nil, err
	}
	out := make([]ApprovalPolicy, 0, len(rows))
	for _, row := range rows {
		policy, err := policyFromSqlite(row)
		if err != nil {
			return nil, err
		}
		out = append(out, policy)
	}
	return out, nil
}

func (r sqliteApprovals) UpdatePolicy(ctx context.Context, p authz.Proof, update ApprovalPolicyUpdate) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalPolicyUpdate, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.UpdateApprovalPolicy(ctx, sqlitegen.UpdateApprovalPolicyParams{
		MinApprovals:      int64(update.MinApprovals),
		AllowSelfApproval: boolToInt(update.AllowSelfApproval),
		RequestTtlSeconds: int64(update.RequestTTLSeconds),
		Enabled:           boolToInt(update.Enabled),
		UpdatedAt:         CanonTime(update.UpdatedAt).Format(timeFormat),
		OrgID:             string(chain.Org), ProjectID: string(chain.Project), ID: update.ID,
	})
	return n > 0, constraint(err)
}

func (r sqliteApprovals) DeletePolicy(ctx context.Context, p authz.Proof, id string) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalPolicyDelete, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.DeleteApprovalPolicy(ctx, sqlitegen.DeleteApprovalPolicyParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), ID: id,
	})
	return n > 0, err
}

func (r sqliteApprovals) InsertApprover(ctx context.Context, p authz.Proof, approver NewApprovalApprover) error {
	chain, err := authz.Verify(p, authz.StoreApprovalApproverInsert, r.tok)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertApprovalPolicyApprover(ctx, sqlitegen.InsertApprovalPolicyApproverParams{
		ID: approver.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project),
		PolicyID: approver.PolicyID, Kind: string(approver.Kind),
		SubjectID: approver.SubjectID, ScopeBindingID: approver.ScopeBindingID,
	}))
}

func (r sqliteApprovals) ListApprovers(ctx context.Context, p authz.Proof, policyID string) ([]ApprovalApprover, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalApproverList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListApprovalPolicyApprovers(ctx, sqlitegen.ListApprovalPolicyApproversParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: policyID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ApprovalApprover, 0, len(rows))
	for _, row := range rows {
		out = append(out, ApprovalApprover{
			ID: row.ID, PolicyID: row.PolicyID, Kind: ApprovalApproverKind(row.Kind),
			SubjectID: row.SubjectID, ScopeBindingID: row.ScopeBindingID,
		})
	}
	return out, nil
}

func (r sqliteApprovals) ClearApprovers(ctx context.Context, p authz.Proof, policyID string) (int64, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalApproverClear, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeleteApprovalPolicyApprovers(ctx, sqlitegen.DeleteApprovalPolicyApproversParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: policyID,
	})
}

func (r sqliteApprovals) InsertBypasser(ctx context.Context, p authz.Proof, bypasser NewApprovalBypasser) error {
	chain, err := authz.Verify(p, authz.StoreApprovalBypasserInsert, r.tok)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertApprovalPolicyBypasser(ctx, sqlitegen.InsertApprovalPolicyBypasserParams{
		ID: bypasser.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project),
		PolicyID: bypasser.PolicyID, PrincipalID: bypasser.PrincipalID,
	}))
}

func (r sqliteApprovals) ListBypassers(ctx context.Context, p authz.Proof, policyID string) ([]ApprovalBypasser, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalBypasserList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListApprovalPolicyBypassers(ctx, sqlitegen.ListApprovalPolicyBypassersParams{
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

func (r sqliteApprovals) ClearBypassers(ctx context.Context, p authz.Proof, policyID string) (int64, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalBypasserClear, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeleteApprovalPolicyBypassers(ctx, sqlitegen.DeleteApprovalPolicyBypassersParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), PolicyID: policyID,
	})
}

func (r sqliteApprovals) IsBypasser(ctx context.Context, p authz.Proof, policyID, principalID string) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalBypasserGet, r.tok)
	if err != nil {
		return false, err
	}
	_, err = r.q.GetApprovalPolicyBypasser(ctx, sqlitegen.GetApprovalPolicyBypasserParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project),
		PolicyID: policyID, PrincipalID: principalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r sqliteApprovals) InsertRequest(ctx context.Context, p authz.Proof, request NewApprovalRequest) error {
	chain, err := authz.Verify(p, authz.StoreApprovalRequestInsert, r.tok)
	if err != nil {
		return err
	}
	env, err := envOf(chain, authz.StoreApprovalRequestInsert)
	if err != nil {
		return err
	}
	versionIDs, err := marshalIDs(request.VersionIDs)
	if err != nil {
		return err
	}
	closedIDs, err := marshalIDs(request.ClosedVersionIDs)
	if err != nil {
		return err
	}
	keyIDs, err := marshalIDs(request.KeyIDs)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertApprovalRequest(ctx, sqlitegen.InsertApprovalRequestParams{
		ID: request.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env,
		PolicyID: request.PolicyID, PolicyVersion: request.PolicyVersion,
		RequesterPrincipalID: request.RequesterPrincipalID,
		VersionIds:           versionIDs, ClosedVersionIds: closedIDs, KeyIds: keyIDs,
		PreviewTokenDigest: request.PreviewTokenDigest, BaseRevision: request.BaseRevision,
		Purpose: request.Purpose, State: string(ApprovalStateOpen),
		CreatedAt: CanonTime(request.CreatedAt).Format(timeFormat),
		ExpiresAt: CanonTime(request.ExpiresAt).Format(timeFormat),
	}))
}

func requestFromSqlite(row sqlitegen.ApprovalRequest) (ApprovalRequest, error) {
	created, err := time.Parse(timeFormat, row.CreatedAt)
	if err != nil {
		return ApprovalRequest{}, fmt.Errorf("store: approval request %s created_at %q: %w", row.ID, row.CreatedAt, err)
	}
	expires, err := time.Parse(timeFormat, row.ExpiresAt)
	if err != nil {
		return ApprovalRequest{}, fmt.Errorf("store: approval request %s expires_at %q: %w", row.ID, row.ExpiresAt, err)
	}
	resolved, err := nullableTime(row.ResolvedAt)
	if err != nil {
		return ApprovalRequest{}, err
	}
	versionIDs, err := unmarshalIDs("approval request", row.ID, row.VersionIds)
	if err != nil {
		return ApprovalRequest{}, err
	}
	closedIDs, err := unmarshalIDs("approval request", row.ID, row.ClosedVersionIds)
	if err != nil {
		return ApprovalRequest{}, err
	}
	keyIDs, err := unmarshalIDs("approval request", row.ID, row.KeyIds)
	if err != nil {
		return ApprovalRequest{}, err
	}
	return ApprovalRequest{
		ID: row.ID, EnvironmentID: row.EnvironmentID, PolicyID: row.PolicyID,
		PolicyVersion: row.PolicyVersion, RequesterPrincipalID: row.RequesterPrincipalID,
		VersionIDs: versionIDs, ClosedVersionIDs: closedIDs, KeyIDs: keyIDs,
		PreviewTokenDigest: row.PreviewTokenDigest, BaseRevision: row.BaseRevision,
		Purpose: row.Purpose, State: ApprovalRequestState(row.State),
		InvalidatedCause: row.InvalidatedCause, CreatedAt: created.UTC(),
		ExpiresAt: expires.UTC(), ResolvedAt: resolved,
	}, nil
}

func (r sqliteApprovals) GetRequest(ctx context.Context, p authz.Proof, id string) (ApprovalRequest, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalRequestGet, r.tok)
	if err != nil {
		return ApprovalRequest{}, err
	}
	env, err := envOf(chain, authz.StoreApprovalRequestGet)
	if err != nil {
		return ApprovalRequest{}, err
	}
	row, err := r.q.GetApprovalRequest(ctx, sqlitegen.GetApprovalRequestParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env, ID: id,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovalRequest{}, ErrNotFound
	}
	if err != nil {
		return ApprovalRequest{}, err
	}
	return requestFromSqlite(row)
}

func (r sqliteApprovals) ListRequests(ctx context.Context, p authz.Proof) ([]ApprovalRequest, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalRequestList, r.tok)
	if err != nil {
		return nil, err
	}
	env, err := envOf(chain, authz.StoreApprovalRequestList)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListApprovalRequestsForEnvironment(ctx, sqlitegen.ListApprovalRequestsForEnvironmentParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env,
	})
	if err != nil {
		return nil, err
	}
	return requestsFromSqlite(rows)
}

func requestsFromSqlite(rows []sqlitegen.ApprovalRequest) ([]ApprovalRequest, error) {
	out := make([]ApprovalRequest, 0, len(rows))
	for _, row := range rows {
		req, err := requestFromSqlite(row)
		if err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, nil
}

func (r sqliteApprovals) UpdateRequestState(ctx context.Context, p authz.Proof, id string, state ApprovalRequestState, cause string, resolvedAt *time.Time) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalRequestUpdateState, r.tok)
	if err != nil {
		return false, err
	}
	env, err := envOf(chain, authz.StoreApprovalRequestUpdateState)
	if err != nil {
		return false, err
	}
	n, err := r.q.UpdateApprovalRequestState(ctx, sqlitegen.UpdateApprovalRequestStateParams{
		State: string(state), InvalidatedCause: cause, ResolvedAt: sqliteResolvedAt(resolvedAt),
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env, ID: id,
	})
	return n > 0, err
}

func (r sqliteApprovals) InsertVote(ctx context.Context, p authz.Proof, vote NewApprovalVote) error {
	chain, err := authz.Verify(p, authz.StoreApprovalVoteInsert, r.tok)
	if err != nil {
		return err
	}
	env, err := envOf(chain, authz.StoreApprovalVoteInsert)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertApprovalVote(ctx, sqlitegen.InsertApprovalVoteParams{
		ID: vote.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env,
		RequestID: vote.RequestID, PrincipalID: vote.PrincipalID,
		Decision: string(vote.Decision), CreatedAt: CanonTime(vote.CreatedAt).Format(timeFormat),
	}))
}

func voteFromSqlite(row sqlitegen.ApprovalVote) (ApprovalVote, error) {
	created, err := time.Parse(timeFormat, row.CreatedAt)
	if err != nil {
		return ApprovalVote{}, fmt.Errorf("store: approval vote %s created_at %q: %w", row.ID, row.CreatedAt, err)
	}
	return ApprovalVote{
		ID: row.ID, RequestID: row.RequestID, PrincipalID: row.PrincipalID,
		Decision: ApprovalVoteDecision(row.Decision), CreatedAt: created.UTC(),
	}, nil
}

func (r sqliteApprovals) GetVote(ctx context.Context, p authz.Proof, requestID, principalID string) (ApprovalVote, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalVoteGet, r.tok)
	if err != nil {
		return ApprovalVote{}, err
	}
	env, err := envOf(chain, authz.StoreApprovalVoteGet)
	if err != nil {
		return ApprovalVote{}, err
	}
	row, err := r.q.GetApprovalVote(ctx, sqlitegen.GetApprovalVoteParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env,
		RequestID: requestID, PrincipalID: principalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovalVote{}, ErrNotFound
	}
	if err != nil {
		return ApprovalVote{}, err
	}
	return voteFromSqlite(row)
}

func (r sqliteApprovals) ListVotes(ctx context.Context, p authz.Proof, requestID string) ([]ApprovalVote, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalVoteList, r.tok)
	if err != nil {
		return nil, err
	}
	env, err := envOf(chain, authz.StoreApprovalVoteList)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListApprovalVotes(ctx, sqlitegen.ListApprovalVotesParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env, RequestID: requestID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ApprovalVote, 0, len(rows))
	for _, row := range rows {
		vote, err := voteFromSqlite(row)
		if err != nil {
			return nil, err
		}
		out = append(out, vote)
	}
	return out, nil
}

func (r sqliteApprovals) SelectExpired(ctx context.Context, p authz.Proof, now time.Time) ([]ExpiredApprovalRequest, error) {
	if _, err := authz.Verify(p, authz.StoreApprovalRequestSelectExpiry, r.tok); err != nil {
		return nil, err
	}
	rows, err := r.q.SelectExpiredApprovalRequests(ctx, CanonTime(now).Format(timeFormat))
	if err != nil {
		return nil, err
	}
	out := make([]ExpiredApprovalRequest, 0, len(rows))
	for _, row := range rows {
		expires, err := time.Parse(timeFormat, row.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("store: approval request %s expires_at %q: %w", row.ID, row.ExpiresAt, err)
		}
		out = append(out, ExpiredApprovalRequest{
			ID: row.ID, OrgID: row.OrgID, ProjectID: row.ProjectID, EnvironmentID: row.EnvironmentID,
			PolicyID: row.PolicyID, RequesterPrincipalID: row.RequesterPrincipalID, ExpiresAt: expires.UTC(),
		})
	}
	return out, nil
}

func (r sqliteApprovals) MarkExpired(ctx context.Context, p authz.Proof, id string, now time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StoreApprovalRequestMarkExpired, r.tok); err != nil {
		return false, err
	}
	n, err := r.q.MarkApprovalRequestExpired(ctx, sqlitegen.MarkApprovalRequestExpiredParams{
		ResolvedAt: sql.NullString{String: CanonTime(now).Format(timeFormat), Valid: true}, ID: id,
	})
	return n > 0, err
}

// --- postgres ---

type pgApprovals struct {
	q   *pggen.Queries
	tok *authz.TxToken
}

func (r pgRepos) Approvals() ApprovalRepo {
	return pgApprovals{q: pggen.New(r.db), tok: r.tok}
}

func (r pgApprovals) InsertPolicy(ctx context.Context, p authz.Proof, policy NewApprovalPolicy) error {
	chain, err := authz.Verify(p, authz.StoreApprovalPolicyInsert, r.tok)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertApprovalPolicy(ctx, pggen.InsertApprovalPolicyParams{
		ID:                policy.ID,
		ChainOrgID:        string(chain.Org),
		ChainProjectID:    string(chain.Project),
		EnvironmentID:     policy.EnvironmentID,
		MinApprovals:      int32(policy.MinApprovals),
		AllowSelfApproval: policy.AllowSelfApproval,
		RequestTtlSeconds: int32(policy.RequestTTLSeconds),
		Enabled:           policy.Enabled,
		Version:           1,
		CreatedBy:         policy.CreatedBy,
		CreatedAt:         pgtype.Timestamptz{Time: CanonTime(policy.CreatedAt), Valid: true},
		UpdatedAt:         pgtype.Timestamptz{Time: CanonTime(policy.CreatedAt), Valid: true},
	}))
}

func policyFromPg(row pggen.ApprovalPolicy) ApprovalPolicy {
	return ApprovalPolicy{
		ID:                row.ID,
		EnvironmentID:     row.EnvironmentID,
		MinApprovals:      int(row.MinApprovals),
		AllowSelfApproval: row.AllowSelfApproval,
		RequestTTLSeconds: int(row.RequestTtlSeconds),
		Enabled:           row.Enabled,
		Version:           row.Version,
		CreatedBy:         row.CreatedBy,
		CreatedAt:         row.CreatedAt.Time.UTC(),
		UpdatedAt:         row.UpdatedAt.Time.UTC(),
	}
}

func (r pgApprovals) GetPolicy(ctx context.Context, p authz.Proof, id string) (ApprovalPolicy, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalPolicyGet, r.tok)
	if err != nil {
		return ApprovalPolicy{}, err
	}
	row, err := r.q.GetApprovalPolicy(ctx, pggen.GetApprovalPolicyParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ID: id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ApprovalPolicy{}, ErrNotFound
	}
	if err != nil {
		return ApprovalPolicy{}, err
	}
	return policyFromPg(row), nil
}

func (r pgApprovals) CoveringPolicy(ctx context.Context, p authz.Proof, envID string) (ApprovalPolicy, bool, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalPolicyCovering, r.tok)
	if err != nil {
		return ApprovalPolicy{}, false, err
	}
	exact, err := r.q.GetApprovalPolicyForEnvironment(ctx, pggen.GetApprovalPolicyForEnvironmentParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), EnvironmentID: envID,
	})
	if err == nil {
		return policyFromPg(exact), true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ApprovalPolicy{}, false, err
	}
	wide, err := r.q.GetApprovalPolicyProjectWide(ctx, pggen.GetApprovalPolicyProjectWideParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ApprovalPolicy{}, false, nil
	}
	if err != nil {
		return ApprovalPolicy{}, false, err
	}
	return policyFromPg(wide), true, nil
}

func (r pgApprovals) ListPolicies(ctx context.Context, p authz.Proof) ([]ApprovalPolicy, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalPolicyList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListApprovalPolicies(ctx, pggen.ListApprovalPoliciesParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
	})
	if err != nil {
		return nil, err
	}
	out := make([]ApprovalPolicy, 0, len(rows))
	for _, row := range rows {
		out = append(out, policyFromPg(row))
	}
	return out, nil
}

func (r pgApprovals) UpdatePolicy(ctx context.Context, p authz.Proof, update ApprovalPolicyUpdate) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalPolicyUpdate, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.UpdateApprovalPolicy(ctx, pggen.UpdateApprovalPolicyParams{
		MinApprovals:      int32(update.MinApprovals),
		AllowSelfApproval: update.AllowSelfApproval,
		RequestTtlSeconds: int32(update.RequestTTLSeconds),
		Enabled:           update.Enabled,
		UpdatedAt:         pgtype.Timestamptz{Time: CanonTime(update.UpdatedAt), Valid: true},
		ChainOrgID:        string(chain.Org), ChainProjectID: string(chain.Project), ID: update.ID,
	})
	return n > 0, constraint(err)
}

func (r pgApprovals) DeletePolicy(ctx context.Context, p authz.Proof, id string) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalPolicyDelete, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.DeleteApprovalPolicy(ctx, pggen.DeleteApprovalPolicyParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ID: id,
	})
	return n > 0, err
}

func (r pgApprovals) InsertApprover(ctx context.Context, p authz.Proof, approver NewApprovalApprover) error {
	chain, err := authz.Verify(p, authz.StoreApprovalApproverInsert, r.tok)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertApprovalPolicyApprover(ctx, pggen.InsertApprovalPolicyApproverParams{
		ID: approver.ID, ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
		PolicyID: approver.PolicyID, Kind: string(approver.Kind),
		SubjectID: approver.SubjectID, ScopeBindingID: approver.ScopeBindingID,
	}))
}

func (r pgApprovals) ListApprovers(ctx context.Context, p authz.Proof, policyID string) ([]ApprovalApprover, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalApproverList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListApprovalPolicyApprovers(ctx, pggen.ListApprovalPolicyApproversParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), PolicyID: policyID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ApprovalApprover, 0, len(rows))
	for _, row := range rows {
		out = append(out, ApprovalApprover{
			ID: row.ID, PolicyID: row.PolicyID, Kind: ApprovalApproverKind(row.Kind),
			SubjectID: row.SubjectID, ScopeBindingID: row.ScopeBindingID,
		})
	}
	return out, nil
}

func (r pgApprovals) ClearApprovers(ctx context.Context, p authz.Proof, policyID string) (int64, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalApproverClear, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeleteApprovalPolicyApprovers(ctx, pggen.DeleteApprovalPolicyApproversParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), PolicyID: policyID,
	})
}

func (r pgApprovals) InsertBypasser(ctx context.Context, p authz.Proof, bypasser NewApprovalBypasser) error {
	chain, err := authz.Verify(p, authz.StoreApprovalBypasserInsert, r.tok)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertApprovalPolicyBypasser(ctx, pggen.InsertApprovalPolicyBypasserParams{
		ID: bypasser.ID, ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
		PolicyID: bypasser.PolicyID, PrincipalID: bypasser.PrincipalID,
	}))
}

func (r pgApprovals) ListBypassers(ctx context.Context, p authz.Proof, policyID string) ([]ApprovalBypasser, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalBypasserList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListApprovalPolicyBypassers(ctx, pggen.ListApprovalPolicyBypassersParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), PolicyID: policyID,
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

func (r pgApprovals) ClearBypassers(ctx context.Context, p authz.Proof, policyID string) (int64, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalBypasserClear, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeleteApprovalPolicyBypassers(ctx, pggen.DeleteApprovalPolicyBypassersParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), PolicyID: policyID,
	})
}

func (r pgApprovals) IsBypasser(ctx context.Context, p authz.Proof, policyID, principalID string) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalBypasserGet, r.tok)
	if err != nil {
		return false, err
	}
	_, err = r.q.GetApprovalPolicyBypasser(ctx, pggen.GetApprovalPolicyBypasserParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
		PolicyID: policyID, PrincipalID: principalID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r pgApprovals) InsertRequest(ctx context.Context, p authz.Proof, request NewApprovalRequest) error {
	chain, err := authz.Verify(p, authz.StoreApprovalRequestInsert, r.tok)
	if err != nil {
		return err
	}
	env, err := envOf(chain, authz.StoreApprovalRequestInsert)
	if err != nil {
		return err
	}
	versionIDs, err := marshalIDs(request.VersionIDs)
	if err != nil {
		return err
	}
	closedIDs, err := marshalIDs(request.ClosedVersionIDs)
	if err != nil {
		return err
	}
	keyIDs, err := marshalIDs(request.KeyIDs)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertApprovalRequest(ctx, pggen.InsertApprovalRequestParams{
		ID: request.ID, ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		PolicyID: request.PolicyID, PolicyVersion: request.PolicyVersion,
		RequesterPrincipalID: request.RequesterPrincipalID,
		VersionIds:           versionIDs, ClosedVersionIds: closedIDs, KeyIds: keyIDs,
		PreviewTokenDigest: request.PreviewTokenDigest, BaseRevision: request.BaseRevision,
		Purpose: request.Purpose, State: string(ApprovalStateOpen),
		CreatedAt: pgtype.Timestamptz{Time: CanonTime(request.CreatedAt), Valid: true},
		ExpiresAt: pgtype.Timestamptz{Time: CanonTime(request.ExpiresAt), Valid: true},
	}))
}

func requestFromPg(row pggen.ApprovalRequest) (ApprovalRequest, error) {
	versionIDs, err := unmarshalIDs("approval request", row.ID, row.VersionIds)
	if err != nil {
		return ApprovalRequest{}, err
	}
	closedIDs, err := unmarshalIDs("approval request", row.ID, row.ClosedVersionIds)
	if err != nil {
		return ApprovalRequest{}, err
	}
	keyIDs, err := unmarshalIDs("approval request", row.ID, row.KeyIds)
	if err != nil {
		return ApprovalRequest{}, err
	}
	return ApprovalRequest{
		ID: row.ID, EnvironmentID: row.EnvironmentID, PolicyID: row.PolicyID,
		PolicyVersion: row.PolicyVersion, RequesterPrincipalID: row.RequesterPrincipalID,
		VersionIDs: versionIDs, ClosedVersionIDs: closedIDs, KeyIDs: keyIDs,
		PreviewTokenDigest: row.PreviewTokenDigest, BaseRevision: row.BaseRevision,
		Purpose: row.Purpose, State: ApprovalRequestState(row.State),
		InvalidatedCause: row.InvalidatedCause, CreatedAt: row.CreatedAt.Time.UTC(),
		ExpiresAt: row.ExpiresAt.Time.UTC(), ResolvedAt: nullableTimePg(row.ResolvedAt),
	}, nil
}

func (r pgApprovals) GetRequest(ctx context.Context, p authz.Proof, id string) (ApprovalRequest, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalRequestGet, r.tok)
	if err != nil {
		return ApprovalRequest{}, err
	}
	env, err := envOf(chain, authz.StoreApprovalRequestGet)
	if err != nil {
		return ApprovalRequest{}, err
	}
	row, err := r.q.GetApprovalRequest(ctx, pggen.GetApprovalRequestParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, ID: id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ApprovalRequest{}, ErrNotFound
	}
	if err != nil {
		return ApprovalRequest{}, err
	}
	return requestFromPg(row)
}

func (r pgApprovals) ListRequests(ctx context.Context, p authz.Proof) ([]ApprovalRequest, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalRequestList, r.tok)
	if err != nil {
		return nil, err
	}
	env, err := envOf(chain, authz.StoreApprovalRequestList)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListApprovalRequestsForEnvironment(ctx, pggen.ListApprovalRequestsForEnvironmentParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
	})
	if err != nil {
		return nil, err
	}
	return requestsFromPg(rows)
}

func requestsFromPg(rows []pggen.ApprovalRequest) ([]ApprovalRequest, error) {
	out := make([]ApprovalRequest, 0, len(rows))
	for _, row := range rows {
		req, err := requestFromPg(row)
		if err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, nil
}

func (r pgApprovals) UpdateRequestState(ctx context.Context, p authz.Proof, id string, state ApprovalRequestState, cause string, resolvedAt *time.Time) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalRequestUpdateState, r.tok)
	if err != nil {
		return false, err
	}
	env, err := envOf(chain, authz.StoreApprovalRequestUpdateState)
	if err != nil {
		return false, err
	}
	n, err := r.q.UpdateApprovalRequestState(ctx, pggen.UpdateApprovalRequestStateParams{
		State: string(state), InvalidatedCause: cause, ResolvedAt: pgResolvedAt(resolvedAt),
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, ID: id,
	})
	return n > 0, err
}

func (r pgApprovals) InsertVote(ctx context.Context, p authz.Proof, vote NewApprovalVote) error {
	chain, err := authz.Verify(p, authz.StoreApprovalVoteInsert, r.tok)
	if err != nil {
		return err
	}
	env, err := envOf(chain, authz.StoreApprovalVoteInsert)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertApprovalVote(ctx, pggen.InsertApprovalVoteParams{
		ID: vote.ID, ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		RequestID: vote.RequestID, PrincipalID: vote.PrincipalID,
		Decision: string(vote.Decision), CreatedAt: pgtype.Timestamptz{Time: CanonTime(vote.CreatedAt), Valid: true},
	}))
}

func voteFromPg(row pggen.ApprovalVote) ApprovalVote {
	return ApprovalVote{
		ID: row.ID, RequestID: row.RequestID, PrincipalID: row.PrincipalID,
		Decision: ApprovalVoteDecision(row.Decision), CreatedAt: row.CreatedAt.Time.UTC(),
	}
}

func (r pgApprovals) GetVote(ctx context.Context, p authz.Proof, requestID, principalID string) (ApprovalVote, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalVoteGet, r.tok)
	if err != nil {
		return ApprovalVote{}, err
	}
	env, err := envOf(chain, authz.StoreApprovalVoteGet)
	if err != nil {
		return ApprovalVote{}, err
	}
	row, err := r.q.GetApprovalVote(ctx, pggen.GetApprovalVoteParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		RequestID: requestID, PrincipalID: principalID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ApprovalVote{}, ErrNotFound
	}
	if err != nil {
		return ApprovalVote{}, err
	}
	return voteFromPg(row), nil
}

func (r pgApprovals) ListVotes(ctx context.Context, p authz.Proof, requestID string) ([]ApprovalVote, error) {
	chain, err := authz.Verify(p, authz.StoreApprovalVoteList, r.tok)
	if err != nil {
		return nil, err
	}
	env, err := envOf(chain, authz.StoreApprovalVoteList)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListApprovalVotes(ctx, pggen.ListApprovalVotesParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, RequestID: requestID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ApprovalVote, 0, len(rows))
	for _, row := range rows {
		out = append(out, voteFromPg(row))
	}
	return out, nil
}

func (r pgApprovals) SelectExpired(ctx context.Context, p authz.Proof, now time.Time) ([]ExpiredApprovalRequest, error) {
	if _, err := authz.Verify(p, authz.StoreApprovalRequestSelectExpiry, r.tok); err != nil {
		return nil, err
	}
	rows, err := r.q.SelectExpiredApprovalRequests(ctx, pgtype.Timestamptz{Time: CanonTime(now), Valid: true})
	if err != nil {
		return nil, err
	}
	out := make([]ExpiredApprovalRequest, 0, len(rows))
	for _, row := range rows {
		out = append(out, ExpiredApprovalRequest{
			ID: row.ID, OrgID: row.OrgID, ProjectID: row.ProjectID, EnvironmentID: row.EnvironmentID,
			PolicyID: row.PolicyID, RequesterPrincipalID: row.RequesterPrincipalID, ExpiresAt: row.ExpiresAt.Time.UTC(),
		})
	}
	return out, nil
}

func (r pgApprovals) MarkExpired(ctx context.Context, p authz.Proof, id string, now time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StoreApprovalRequestMarkExpired, r.tok); err != nil {
		return false, err
	}
	n, err := r.q.MarkApprovalRequestExpired(ctx, pggen.MarkApprovalRequestExpiredParams{
		ResolvedAt: pgtype.Timestamptz{Time: CanonTime(now), Valid: true}, ID: id,
	})
	return n > 0, err
}
