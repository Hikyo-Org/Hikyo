package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r sqliteRetention) Diagnostics(ctx context.Context, p authz.Proof, now time.Time) (OpsMetadata, error) {
	if _, err := authz.Verify(p, authz.StoreOpsDiagnosticsRead, r.tok); err != nil {
		return OpsMetadata{}, err
	}
	row, err := r.q.GetOpsDiagnostics(ctx, sqlitegen.GetOpsDiagnosticsParams{Now: CanonTime(now).Format(timeFormat), DayAt: CanonTime(now.Add(24 * time.Hour)).Format(timeFormat), WeekAt: CanonTime(now.Add(7 * 24 * time.Hour)).Format(timeFormat), MonthAt: CanonTime(now.Add(30 * 24 * time.Hour)).Format(timeFormat)})
	if err != nil {
		return OpsMetadata{}, err
	}
	out := OpsMetadata{EscrowInstanceID: row.EscrowInstanceID, EscrowIncarnation: row.EscrowIncarnation, EscrowRootEpoch: row.EscrowRootEpoch, RootEpoch: row.RootEpoch, RootWrappers: row.RootWrappers, RetiringScopes: row.RetiringScopes, PinsExpired: row.PinsExpired, PinsDay: row.PinsDay, PinsWeek: row.PinsWeek, PinsMonth: row.PinsMonth}
	if row.EscrowVerifiedAt.Valid {
		out.EscrowVerifiedAt, err = parseTime("ops diagnostics", "escrow", row.EscrowVerifiedAt.String)
		if err != nil {
			return OpsMetadata{}, err
		}
	}
	if row.LastReencryptSuccessAt.Valid {
		out.LastReencryptSuccess, err = parseTime("ops diagnostics", "reencrypt", row.LastReencryptSuccessAt.String)
		if err != nil {
			return OpsMetadata{}, err
		}
	}
	return out, nil
}
func (r sqliteRetention) RecordEscrow(ctx context.Context, p authz.Proof, rec EscrowRecord) error {
	if _, err := authz.Verify(p, authz.StoreEscrowVerificationWrite, r.tok); err != nil {
		return err
	}
	return r.q.SetEscrowVerification(ctx, sqlitegen.SetEscrowVerificationParams{VerifiedAt: sql.NullString{String: CanonTime(rec.At).Format(timeFormat), Valid: true}, InstanceID: rec.InstanceID, Incarnation: rec.Incarnation, RootEpoch: rec.RootEpoch})
}
func (r sqliteRetention) RecordReencryptSuccess(ctx context.Context, p authz.Proof, at time.Time) error {
	if _, err := authz.Verify(p, authz.StoreReencryptSuccessWrite, r.tok); err != nil {
		return err
	}
	return r.q.SetReencryptSuccess(ctx, sql.NullString{String: CanonTime(at).Format(timeFormat), Valid: true})
}

func (r pgRetention) Diagnostics(ctx context.Context, p authz.Proof, now time.Time) (OpsMetadata, error) {
	if _, err := authz.Verify(p, authz.StoreOpsDiagnosticsRead, r.tok); err != nil {
		return OpsMetadata{}, err
	}
	row, err := r.q.GetOpsDiagnostics(ctx, pggen.GetOpsDiagnosticsParams{Now: pgtype.Timestamptz{Time: CanonTime(now), Valid: true}, DayAt: pgtype.Timestamptz{Time: CanonTime(now.Add(24 * time.Hour)), Valid: true}, WeekAt: pgtype.Timestamptz{Time: CanonTime(now.Add(7 * 24 * time.Hour)), Valid: true}, MonthAt: pgtype.Timestamptz{Time: CanonTime(now.Add(30 * 24 * time.Hour)), Valid: true}})
	if err != nil {
		return OpsMetadata{}, err
	}
	out := OpsMetadata{EscrowInstanceID: row.EscrowInstanceID, EscrowIncarnation: row.EscrowIncarnation, EscrowRootEpoch: row.EscrowRootEpoch, RootEpoch: row.RootEpoch, RootWrappers: row.RootWrappers, RetiringScopes: row.RetiringScopes, PinsExpired: row.PinsExpired, PinsDay: row.PinsDay, PinsWeek: row.PinsWeek, PinsMonth: row.PinsMonth}
	if row.EscrowVerifiedAt.Valid {
		out.EscrowVerifiedAt = row.EscrowVerifiedAt.Time.UTC()
	}
	if row.LastReencryptSuccessAt.Valid {
		out.LastReencryptSuccess = row.LastReencryptSuccessAt.Time.UTC()
	}
	return out, nil
}
func (r pgRetention) RecordEscrow(ctx context.Context, p authz.Proof, rec EscrowRecord) error {
	if _, err := authz.Verify(p, authz.StoreEscrowVerificationWrite, r.tok); err != nil {
		return err
	}
	return r.q.SetEscrowVerification(ctx, pggen.SetEscrowVerificationParams{VerifiedAt: pgtype.Timestamptz{Time: CanonTime(rec.At), Valid: true}, InstanceID: rec.InstanceID, Incarnation: rec.Incarnation, RootEpoch: rec.RootEpoch})
}
func (r pgRetention) RecordReencryptSuccess(ctx context.Context, p authz.Proof, at time.Time) error {
	if _, err := authz.Verify(p, authz.StoreReencryptSuccessWrite, r.tok); err != nil {
		return err
	}
	return r.q.SetReencryptSuccess(ctx, pgtype.Timestamptz{Time: CanonTime(at), Valid: true})
}
