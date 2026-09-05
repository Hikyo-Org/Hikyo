package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// AuditRetentionPolicy is a bounded instance policy. Zero is only the
// unconfigured state; it is never accepted by a persisted policy write.
type AuditRetentionPolicy struct{ AccessDays, SecurityDays int }

func (p AuditRetentionPolicy) Validate() error {
	if p.AccessDays < 1 || p.SecurityDays < p.AccessDays || p.SecurityDays > 3650 {
		return fmt.Errorf("audit retention requires 1 <= access days <= security days <= 3650")
	}
	return nil
}

// AuditPrunedRow contains only the facts needed for the pruning receipt.
type AuditPrunedRow struct {
	Trail, Type string
	RecordedAt  time.Time
}

func auditAccessTypes() ([]byte, error) {
	types := []string{}
	for _, typ := range audit.Types() {
		spec, _ := audit.Spec(typ)
		if spec.Retention == audit.RetentionAccess {
			types = append(types, string(typ))
		}
	}
	return json.Marshal(types)
}

func (r sqliteRetention) AuditPolicy(ctx context.Context, p authz.Proof) (AuditRetentionPolicy, error) {
	if _, err := authz.Verify(p, authz.StoreRetentionAuditPolicy, r.tok); err != nil {
		return AuditRetentionPolicy{}, err
	}
	row, err := r.q.GetAuditRetentionPolicy(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return AuditRetentionPolicy{}, nil
	}
	return AuditRetentionPolicy{int(row.AccessDays), int(row.SecurityDays)}, err
}
func (r pgRetention) AuditPolicy(ctx context.Context, p authz.Proof) (AuditRetentionPolicy, error) {
	if _, err := authz.Verify(p, authz.StoreRetentionAuditPolicy, r.tok); err != nil {
		return AuditRetentionPolicy{}, err
	}
	row, err := r.q.GetAuditRetentionPolicy(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuditRetentionPolicy{}, nil
	}
	return AuditRetentionPolicy{int(row.AccessDays), int(row.SecurityDays)}, err
}
func (r sqliteRetention) SetAuditPolicy(ctx context.Context, p authz.Proof, policy AuditRetentionPolicy) error {
	if _, err := authz.Verify(p, authz.StoreRetentionSetAuditPolicy, r.tok); err != nil {
		return err
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	return r.q.SetAuditRetentionPolicy(ctx, sqlitegen.SetAuditRetentionPolicyParams{AccessDays: int64(policy.AccessDays), SecurityDays: int64(policy.SecurityDays)})
}
func (r pgRetention) SetAuditPolicy(ctx context.Context, p authz.Proof, policy AuditRetentionPolicy) error {
	if _, err := authz.Verify(p, authz.StoreRetentionSetAuditPolicy, r.tok); err != nil {
		return err
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	return r.q.SetAuditRetentionPolicy(ctx, pggen.SetAuditRetentionPolicyParams{AccessDays: int32(policy.AccessDays), SecurityDays: int32(policy.SecurityDays)})
}

// PruneAudit removes at most 100 correlation units per trail. Correlated rows are retained until every member expires,
// including mixed-class units. An envelope can never be split from its keys.
// The caller MUST insert the receipt in this same transaction.
func (r sqliteRetention) PruneAudit(ctx context.Context, p authz.Proof, accessCutoff, securityCutoff time.Time) ([]AuditPrunedRow, error) {
	if _, err := authz.Verify(p, authz.StoreRetentionPruneAudit, r.tok); err != nil {
		return nil, err
	}
	types, err := auditAccessTypes()
	if err != nil {
		return nil, err
	}
	tenant, err := r.q.PruneTenantAuditRetention(ctx, sqlitegen.PruneTenantAuditRetentionParams{AccessTypes: string(types), AccessCutoffTime: audit.FormatTime(accessCutoff), SecurityCutoffTime: audit.FormatTime(securityCutoff)})
	if err != nil {
		return nil, err
	}
	instance, err := r.q.PruneInstanceAuditRetention(ctx, sqlitegen.PruneInstanceAuditRetentionParams{AccessTypes: string(types), AccessCutoffTime: audit.FormatTime(accessCutoff), SecurityCutoffTime: audit.FormatTime(securityCutoff)})
	if err != nil {
		return nil, err
	}
	out := make([]AuditPrunedRow, 0, len(tenant)+len(instance))
	for _, row := range tenant {
		at, err := parseTime("audit retention", "recorded_at", row.RecordedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, AuditPrunedRow{"tenant", row.Type, at})
	}
	for _, row := range instance {
		at, err := parseTime("audit retention", "recorded_at", row.RecordedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, AuditPrunedRow{"instance", row.Type, at})
	}
	return out, nil
}
func (r pgRetention) PruneAudit(ctx context.Context, p authz.Proof, accessCutoff, securityCutoff time.Time) ([]AuditPrunedRow, error) {
	if _, err := authz.Verify(p, authz.StoreRetentionPruneAudit, r.tok); err != nil {
		return nil, err
	}
	types, err := auditAccessTypes()
	if err != nil {
		return nil, err
	}
	access, security := pgtype.Timestamptz{Time: accessCutoff, Valid: true}, pgtype.Timestamptz{Time: securityCutoff, Valid: true}
	tenant, err := r.q.PruneTenantAuditRetention(ctx, pggen.PruneTenantAuditRetentionParams{AccessTypes: string(types), AccessCutoffTime: access, SecurityCutoffTime: security})
	if err != nil {
		return nil, err
	}
	instance, err := r.q.PruneInstanceAuditRetention(ctx, pggen.PruneInstanceAuditRetentionParams{AccessTypes: string(types), AccessCutoffTime: access, SecurityCutoffTime: security})
	if err != nil {
		return nil, err
	}
	out := make([]AuditPrunedRow, 0, len(tenant)+len(instance))
	for _, row := range tenant {
		out = append(out, AuditPrunedRow{"tenant", row.Type, row.RecordedAt.Time})
	}
	for _, row := range instance {
		out = append(out, AuditPrunedRow{"instance", row.Type, row.RecordedAt.Time})
	}
	return out, nil
}

// ErrAuditRetentionChanged prevents an export from reporting success after
// policy expiry removed rows from its fixed snapshot between pages.
var ErrAuditRetentionChanged = errors.New("audit retention pruned during export; retry the export")

func (a sqliteAudit) guardRetentionSnapshot(ctx context.Context, since time.Time) error {
	pruned, err := a.q.AuditPrunedSince(ctx, audit.FormatTime(since))
	if err != nil {
		return err
	}
	if pruned {
		return ErrAuditRetentionChanged
	}
	return nil
}
func (a pgAudit) guardRetentionSnapshot(ctx context.Context, since time.Time) error {
	pruned, err := a.q.AuditPrunedSince(ctx, pgtype.Timestamptz{Time: since, Valid: true})
	if err != nil {
		return err
	}
	if pruned {
		return ErrAuditRetentionChanged
	}
	return nil
}
