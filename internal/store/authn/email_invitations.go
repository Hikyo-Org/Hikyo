package authn

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// Email invitations of existing accounts (#1207, docs/spec/email-invitations.md).
// The table is class=authn and deliberately outside PrincipalFootprintOrgs: a
// pending invitation is not membership. Every method here runs after the
// owning operation authorized, or for the session's own account binding.

// EmailInvitation is one pending row. AccountID is empty when the address
// matched no account; it is never rendered to an administrator.
type EmailInvitation struct {
	ID                 string
	OrgID              domain.OrgID
	Email              string
	Template           string
	InviterPrincipalID domain.PrincipalID
	AccountID          string
	TokenVerifier      []byte
	CredentialEpoch    int64
	CreatedAt          time.Time
	ExpiresAt          time.Time
}

// EmailInvitationListing is the administrator's view of a pending row.
type EmailInvitationListing struct {
	ID                 string
	Email              string
	Template           string
	InviterPrincipalID domain.PrincipalID
	CreatedAt          time.Time
	ExpiresAt          time.Time
}

// UpsertOrgEmailInvitation writes or replaces the organisation's one pending
// row for the address and returns the surviving row id.
func (r *Resolver) UpsertOrgEmailInvitation(ctx context.Context, n EmailInvitation) (string, error) {
	if r.sq != nil {
		return r.sq.UpsertOrgEmailInvitation(ctx, sqlitegen.UpsertOrgEmailInvitationParams{
			ID: n.ID, OrgID: string(n.OrgID), Email: n.Email, Template: n.Template,
			InviterPrincipalID: string(n.InviterPrincipalID),
			AccountID:          sql.NullString{String: n.AccountID, Valid: n.AccountID != ""},
			TokenVerifier:      n.TokenVerifier, CredentialEpoch: n.CredentialEpoch,
			CreatedAt: encodeTime(n.CreatedAt), ExpiresAt: encodeTime(n.ExpiresAt),
		})
	}
	return r.pg.UpsertOrgEmailInvitation(ctx, pggen.UpsertOrgEmailInvitationParams{
		ID: n.ID, OrgID: string(n.OrgID), Email: n.Email, Template: n.Template,
		InviterPrincipalID: string(n.InviterPrincipalID),
		AccountID:          pgtype.Text{String: n.AccountID, Valid: n.AccountID != ""},
		TokenVerifier:      n.TokenVerifier, CredentialEpoch: n.CredentialEpoch,
		CreatedAt: pgTimestamp(n.CreatedAt), ExpiresAt: pgTimestamp(n.ExpiresAt),
	})
}

// CountLiveOrgEmailInvitations counts the organisation's unexpired rows.
func (r *Resolver) CountLiveOrgEmailInvitations(ctx context.Context, org domain.OrgID, at time.Time) (int64, error) {
	if r.sq != nil {
		return r.sq.CountLiveOrgEmailInvitations(ctx, sqlitegen.CountLiveOrgEmailInvitationsParams{OrgID: string(org), ExpiresAt: encodeTime(at)})
	}
	return r.pg.CountLiveOrgEmailInvitations(ctx, pggen.CountLiveOrgEmailInvitationsParams{OrgID: string(org), ExpiresAt: pgTimestamp(at)})
}

// ListLiveOrgEmailInvitations lists the organisation's unexpired rows without
// the matched account.
func (r *Resolver) ListLiveOrgEmailInvitations(ctx context.Context, org domain.OrgID, at time.Time) ([]EmailInvitationListing, error) {
	var out []EmailInvitationListing
	if r.sq != nil {
		rows, err := r.sq.ListLiveOrgEmailInvitations(ctx, sqlitegen.ListLiveOrgEmailInvitationsParams{OrgID: string(org), ExpiresAt: encodeTime(at)})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			created, err := decodeTime(row.CreatedAt)
			if err != nil {
				return nil, err
			}
			expires, err := decodeTime(row.ExpiresAt)
			if err != nil {
				return nil, err
			}
			out = append(out, EmailInvitationListing{ID: row.ID, Email: row.Email, Template: row.Template, InviterPrincipalID: domain.PrincipalID(row.InviterPrincipalID), CreatedAt: created, ExpiresAt: expires})
		}
		return out, nil
	}
	rows, err := r.pg.ListLiveOrgEmailInvitations(ctx, pggen.ListLiveOrgEmailInvitationsParams{OrgID: string(org), ExpiresAt: pgTimestamp(at)})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out = append(out, EmailInvitationListing{ID: row.ID, Email: row.Email, Template: row.Template, InviterPrincipalID: domain.PrincipalID(row.InviterPrincipalID), CreatedAt: canon(row.CreatedAt.Time), ExpiresAt: canon(row.ExpiresAt.Time)})
	}
	return out, nil
}

// OrgEmailInvitationInOrg reads one row of the addressed organisation.
func (r *Resolver) OrgEmailInvitationInOrg(ctx context.Context, id string, org domain.OrgID) (EmailInvitation, error) {
	if r.sq != nil {
		row, err := r.sq.GetOrgEmailInvitationInOrg(ctx, sqlitegen.GetOrgEmailInvitationInOrgParams{ID: id, OrgID: string(org)})
		if err != nil {
			return EmailInvitation{}, notFoundOr(err)
		}
		return sqliteEmailInvitation(row)
	}
	row, err := r.pg.GetOrgEmailInvitationInOrg(ctx, pggen.GetOrgEmailInvitationInOrgParams{ID: id, OrgID: string(org)})
	if err != nil {
		return EmailInvitation{}, notFoundOr(err)
	}
	return pgEmailInvitation(row), nil
}

// OrgEmailInvitationByVerifier resolves an opaque bearer verifier.
func (r *Resolver) OrgEmailInvitationByVerifier(ctx context.Context, verifier []byte) (EmailInvitation, error) {
	if r.sq != nil {
		row, err := r.sq.GetOrgEmailInvitationByVerifier(ctx, verifier)
		if err != nil {
			return EmailInvitation{}, notFoundOr(err)
		}
		return sqliteEmailInvitation(row)
	}
	row, err := r.pg.GetOrgEmailInvitationByVerifier(ctx, verifier)
	if err != nil {
		return EmailInvitation{}, notFoundOr(err)
	}
	return pgEmailInvitation(row), nil
}

// OrgEmailInvitationOrgName reads the organisation's display name for a row
// the session's own account already resolved. domain.ErrNotFound when gone.
func (r *Resolver) OrgEmailInvitationOrgName(ctx context.Context, org domain.OrgID) (string, error) {
	id, err := r.orgIdentity(ctx, org)
	return id.Name, err
}

// DeleteOrgEmailInvitationInOrg revokes one row of the addressed organisation.
func (r *Resolver) DeleteOrgEmailInvitationInOrg(ctx context.Context, id string, org domain.OrgID) (bool, error) {
	var n int64
	var err error
	if r.sq != nil {
		n, err = r.sq.DeleteOrgEmailInvitationInOrg(ctx, sqlitegen.DeleteOrgEmailInvitationInOrgParams{ID: id, OrgID: string(org)})
	} else {
		n, err = r.pg.DeleteOrgEmailInvitationInOrg(ctx, pggen.DeleteOrgEmailInvitationInOrgParams{ID: id, OrgID: string(org)})
	}
	return n == 1, err
}

// ConsumeOrgEmailInvitation deletes the row by id and verifier; false means a
// concurrent acceptance or revocation already took it.
func (r *Resolver) ConsumeOrgEmailInvitation(ctx context.Context, id string, verifier []byte) (bool, error) {
	var n int64
	var err error
	if r.sq != nil {
		n, err = r.sq.ConsumeOrgEmailInvitation(ctx, sqlitegen.ConsumeOrgEmailInvitationParams{ID: id, TokenVerifier: verifier})
	} else {
		n, err = r.pg.ConsumeOrgEmailInvitation(ctx, pggen.ConsumeOrgEmailInvitationParams{ID: id, TokenVerifier: verifier})
	}
	return n == 1, err
}

// PruneExpiredOrgEmailInvitations deletes every row past its expiry.
func (r *Resolver) PruneExpiredOrgEmailInvitations(ctx context.Context, at time.Time) (int64, error) {
	if r.sq != nil {
		return r.sq.PruneExpiredOrgEmailInvitations(ctx, encodeTime(at))
	}
	return r.pg.PruneExpiredOrgEmailInvitations(ctx, pgTimestamp(at))
}

func (r *Resolver) eraseOrgEmailInvitations(ctx context.Context, account string) error {
	if r.sq != nil {
		return r.sq.PrivacyEraseOrgEmailInvitations(ctx, sql.NullString{String: account, Valid: true})
	}
	return r.pg.PrivacyEraseOrgEmailInvitations(ctx, pgtype.Text{String: account, Valid: true})
}

// restoreEmailInvitationNextEpoch supports archives predating the table: it
// outruns every epoch stamp a restored pending invitation carries.
func (r *Resolver) restoreEmailInvitationNextEpoch(ctx context.Context, next int64) (int64, error) {
	var n int64
	var err error
	if r.sq != nil {
		err = r.sqdb.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'org_email_invitations'`).Scan(&n)
	} else {
		err = r.pgdb.QueryRow(ctx, `SELECT COUNT(*) FROM pg_catalog.pg_class WHERE relkind IN ('r', 'p') AND oid = to_regclass('org_email_invitations')`).Scan(&n)
	}
	if err != nil {
		return 0, fmt.Errorf("authn: probe restored email invitations: %w", err)
	}
	if n == 0 {
		return next, nil
	}
	var raw any
	if r.sq != nil {
		raw, err = r.sq.OrgEmailInvitationMaxCredentialEpoch(ctx)
	} else {
		raw, err = r.pg.OrgEmailInvitationMaxCredentialEpoch(ctx)
	}
	if err != nil {
		return 0, fmt.Errorf("authn: read restored email invitation epochs: %w", err)
	}
	v, err := nextEpoch(raw)
	if err != nil {
		return 0, err
	}
	return max(next, v), nil
}

func sqliteEmailInvitation(row sqlitegen.OrgEmailInvitation) (EmailInvitation, error) {
	created, err := decodeTime(row.CreatedAt)
	if err != nil {
		return EmailInvitation{}, err
	}
	expires, err := decodeTime(row.ExpiresAt)
	if err != nil {
		return EmailInvitation{}, err
	}
	return EmailInvitation{
		ID: row.ID, OrgID: domain.OrgID(row.OrgID), Email: row.Email, Template: row.Template,
		InviterPrincipalID: domain.PrincipalID(row.InviterPrincipalID), AccountID: row.AccountID.String,
		TokenVerifier: row.TokenVerifier, CredentialEpoch: row.CredentialEpoch, CreatedAt: created, ExpiresAt: expires,
	}, nil
}

func pgEmailInvitation(row pggen.OrgEmailInvitation) EmailInvitation {
	return EmailInvitation{
		ID: row.ID, OrgID: domain.OrgID(row.OrgID), Email: row.Email, Template: row.Template,
		InviterPrincipalID: domain.PrincipalID(row.InviterPrincipalID), AccountID: row.AccountID.String,
		TokenVerifier: row.TokenVerifier, CredentialEpoch: row.CredentialEpoch,
		CreatedAt: canon(row.CreatedAt.Time), ExpiresAt: canon(row.ExpiresAt.Time),
	}
}
