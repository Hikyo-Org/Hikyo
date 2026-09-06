package authn

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// SelfConfigBinding is the resolver's metadata-only view of the local binding.
// It cannot read configuration values. This surface exists solely to apply
// the protected-resource conjunction and mint confined runtime authority.
type SelfConfigBinding struct {
	Scope             domain.Scope
	DesiredSnapshotID string
	DesiredRevision   int64
}

func (r *Resolver) SelfConfigBinding(ctx context.Context) (SelfConfigBinding, error) {
	if r.historicalRecoveryBeforeSelfConfig {
		return SelfConfigBinding{}, domain.ErrNotFound
	}
	if r.sq != nil {
		b, e := r.sq.GetSelfConfigBinding(ctx)
		if e != nil {
			return SelfConfigBinding{}, notFoundOr(e)
		}
		return SelfConfigBinding{Scope: domain.Scope{Org: domain.OrgID(b.OrgID), Project: domain.ProjectID(b.ProjectID), Env: domain.EnvID(b.EnvironmentID)}, DesiredSnapshotID: b.DesiredSnapshotID, DesiredRevision: b.DesiredRevision}, nil
	}
	b, e := r.pg.GetSelfConfigBinding(ctx)
	if e != nil {
		return SelfConfigBinding{}, notFoundOr(e)
	}
	return SelfConfigBinding{Scope: domain.Scope{Org: domain.OrgID(b.OrgID), Project: domain.ProjectID(b.ProjectID), Env: domain.EnvID(b.EnvironmentID)}, DesiredSnapshotID: b.DesiredSnapshotID, DesiredRevision: b.DesiredRevision}, nil
}

func (r *Resolver) SelfConfigRetained(ctx context.Context, snapshotID string) (bool, error) {
	var ids []string
	var err error
	if r.sq != nil {
		ids, err = r.sq.ListSelfConfigRetained(ctx)
	} else {
		ids, err = r.pg.ListSelfConfigRetained(ctx)
	}
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == snapshotID {
			return true, nil
		}
	}
	return false, nil
}

// IsSelfConfigScope uses the profile returned by the same current grant read.
// Authorizers call Grants immediately before evaluating this conjunction.
func (r *Resolver) IsSelfConfigScope(_ context.Context, scope domain.Scope) (bool, error) {
	return r.selfConfigOrgID != "" && scope.Org == r.selfConfigOrgID, nil
}

func (r *Resolver) SelfConfigRecoverySnapshot(ctx context.Context, b SelfConfigBinding, revision int64) (string, error) {
	if r.sq != nil {
		row, err := r.sq.GetSnapshotByRevision(ctx, sqlitegen.GetSnapshotByRevisionParams{OrgID: string(b.Scope.Org), ProjectID: string(b.Scope.Project), EnvironmentID: string(b.Scope.Env), Revision: revision})
		if err != nil {
			return "", notFoundOr(err)
		}
		if row.PayloadPresent != 1 {
			return "", domain.ErrNotFound
		}
		return row.ID, nil
	}
	row, err := r.pg.GetSnapshotByRevision(ctx, pggen.GetSnapshotByRevisionParams{ChainOrgID: string(b.Scope.Org), ChainProjectID: string(b.Scope.Project), ChainEnvID: string(b.Scope.Env), Revision: revision})
	if err != nil {
		return "", notFoundOr(err)
	}
	if !row.PayloadPresent {
		return "", domain.ErrNotFound
	}
	return row.ID, nil
}
