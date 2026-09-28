package authn

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// Historical constructors are confined to tx/recovery.go. They may be selected
// only from the verified source manifest under guarded RecoveryDB authority.
// Compatibility uses pre-47 privacy, pre-50 profile, and pre-66 access
// projections, omits PKI retention queries only before schema64, and reads no
// member access rules before schema70 (no rule table exists there). These
// constructors add no login path and preserve restore reconciliation.
func NewHistoricalRecoverySQLite(db sqlitegen.DBTX, version uint64) *Resolver {
	r := NewSQLite(db)
	r.historicalRecoveryBeforePrivacy = version < 47
	r.historicalRecoveryBeforeSelfConfig = version < 50
	r.historicalRecoveryBeforeAccess = version < 66
	r.historicalRecoveryBeforePKI = version < 64
	r.historicalRecoveryBeforeRules = version < 70
	return r
}

// NewHistoricalRecoveryPG binds a resolver to a verified source schema version
// for guarded recovery. Versions before 47, 50, 64, and 66 use the corresponding
// privacy, profile, PKI-retention, and temporary-access compatibility behavior.
func NewHistoricalRecoveryPG(db pggen.DBTX, version uint64) *Resolver {
	r := NewPG(db)
	r.historicalRecoveryBeforePrivacy = version < 47
	r.historicalRecoveryBeforeSelfConfig = version < 50
	r.historicalRecoveryBeforeAccess = version < 66
	r.historicalRecoveryBeforePKI = version < 64
	r.historicalRecoveryBeforeRules = version < 70
	return r
}
func (r *Resolver) recoveryGrantsBeforePrivacy(ctx context.Context, p domain.PrincipalID) ([]domain.Grant, error) {
	if r.sq != nil {
		rows, err := r.sq.RecoveryListGrantsBeforePrivacy(ctx, string(p))
		return grantsFromRows(rows, err, func(row sqlitegen.RecoveryListGrantsBeforePrivacyRow) (string, string, string, string) {
			return row.Capability, row.OrgID.String, row.ProjectID.String, row.EnvID.String
		})
	}
	rows, err := r.pg.RecoveryListGrantsBeforePrivacy(ctx, string(p))
	return grantsFromRows(rows, err, func(row pggen.RecoveryListGrantsBeforePrivacyRow) (string, string, string, string) {
		return row.Capability, row.OrgID.String, row.ProjectID.String, row.EnvID.String
	})
}

func (r *Resolver) recoveryGrantsBeforeSelfConfig(ctx context.Context, p domain.PrincipalID) ([]domain.Grant, error) {
	if r.sq != nil {
		rows, err := r.sq.RecoveryListGrantsBeforeSelfConfig(ctx, string(p))
		return grantsFromRows(rows, err, func(row sqlitegen.RecoveryListGrantsBeforeSelfConfigRow) (string, string, string, string) {
			return row.Capability, row.OrgID.String, row.ProjectID.String, row.EnvID.String
		})
	}
	rows, err := r.pg.RecoveryListGrantsBeforeSelfConfig(ctx, string(p))
	return grantsFromRows(rows, err, func(row pggen.RecoveryListGrantsBeforeSelfConfigRow) (string, string, string, string) {
		return row.Capability, row.OrgID.String, row.ProjectID.String, row.EnvID.String
	})
}

// recoveryGrantsBeforeAccess is the chokepoint projection for verified source
// schemas 50 through 65, which have no access_grants table (#152).
func (r *Resolver) recoveryGrantsBeforeAccess(ctx context.Context, p domain.PrincipalID) ([]domain.Grant, error) {
	if r.sq != nil {
		rows, err := r.sq.RecoveryListGrantsBeforeAccess(ctx, string(p))
		for _, row := range rows {
			r.selfConfigOrgID = domain.OrgID(row.SelfConfigOrgID)
		}
		return grantsFromRows(rows, err, func(row sqlitegen.RecoveryListGrantsBeforeAccessRow) (string, string, string, string) {
			return row.Capability, row.OrgID.String, row.ProjectID.String, row.EnvID.String
		})
	}
	rows, err := r.pg.RecoveryListGrantsBeforeAccess(ctx, string(p))
	for _, row := range rows {
		r.selfConfigOrgID = domain.OrgID(row.SelfConfigOrgID)
	}
	return grantsFromRows(rows, err, func(row pggen.RecoveryListGrantsBeforeAccessRow) (string, string, string, string) {
		return row.Capability, row.OrgID.String, row.ProjectID.String, row.EnvID.String
	})
}

// grantsFromRows maps one engine's historical grant projection through
// grantFrom; columns names the row's capability, org, project and env.
func grantsFromRows[R any](rows []R, err error, columns func(R) (string, string, string, string)) ([]domain.Grant, error) {
	if err != nil {
		return nil, err
	}
	out := make([]domain.Grant, 0, len(rows))
	for _, row := range rows {
		g, err := grantFrom(columns(row))
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}
