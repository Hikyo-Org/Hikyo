package authn

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// Historical constructors are confined to tx/recovery.go. They may be selected
// only from the verified source manifest under guarded RecoveryDB authority.
// Their compatibility differences are the pre-47 privacy and pre-48 profile projections; they do
// not add a session/login path or remove the restore reconciliation gate.
func NewHistoricalRecoverySQLite(db sqlitegen.DBTX, version uint64) *Resolver {
	r := NewSQLite(db)
	r.historicalRecoveryBeforePrivacy = version < 47
	r.historicalRecoveryBeforeSelfConfig = version < 48
	return r
}
func NewHistoricalRecoveryPG(db pggen.DBTX, version uint64) *Resolver {
	r := NewPG(db)
	r.historicalRecoveryBeforePrivacy = version < 47
	r.historicalRecoveryBeforeSelfConfig = version < 48
	return r
}
func (r *Resolver) recoveryGrantsBeforePrivacy(ctx context.Context, p domain.PrincipalID) ([]domain.Grant, error) {
	out := []domain.Grant{}
	if r.sq != nil {
		rows, err := r.sq.RecoveryListGrantsBeforePrivacy(ctx, string(p))
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			g, err := grantFrom(row.Capability, row.OrgID.String, row.ProjectID.String, row.EnvID.String)
			if err != nil {
				return nil, err
			}
			out = append(out, g)
		}
	} else {
		rows, err := r.pg.RecoveryListGrantsBeforePrivacy(ctx, string(p))
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			g, err := grantFrom(row.Capability, row.OrgID.String, row.ProjectID.String, row.EnvID.String)
			if err != nil {
				return nil, err
			}
			out = append(out, g)
		}
	}
	return out, nil
}

func (r *Resolver) recoveryGrantsBeforeSelfConfig(ctx context.Context, p domain.PrincipalID) ([]domain.Grant, error) {
	out := []domain.Grant{}
	if r.sq != nil {
		rows, err := r.sq.RecoveryListGrantsBeforeSelfConfig(ctx, string(p))
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			g, err := grantFrom(row.Capability, row.OrgID.String, row.ProjectID.String, row.EnvID.String)
			if err != nil {
				return nil, err
			}
			out = append(out, g)
		}
	} else {
		rows, err := r.pg.RecoveryListGrantsBeforeSelfConfig(ctx, string(p))
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			g, err := grantFrom(row.Capability, row.OrgID.String, row.ProjectID.String, row.EnvID.String)
			if err != nil {
				return nil, err
			}
			out = append(out, g)
		}
	}
	return out, nil
}
