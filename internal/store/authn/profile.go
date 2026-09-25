package authn

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// AccountProfile is the account's self-service labels plus its email (nil when
// none). EmailVerified tells the two kinds apart: a verified email is the login
// email, which only verified local sign-up writes (#608); an unverified one is
// a legacy 00049 contact address, display-only and never an authentication,
// linking or uniqueness key.
type AccountProfile struct {
	Username         string
	DisplayName      string
	Email            *string
	EmailVerified    bool
	Managed          bool
	UsernameEditable bool
}

func (r *Resolver) AccountProfile(ctx context.Context, accountID string) (AccountProfile, error) {
	if r.sq != nil {
		row, err := r.sq.GetAccountProfile(ctx, accountID)
		if isNoRows(err) {
			return AccountProfile{}, domain.ErrNotFound
		}
		return AccountProfile{row.Username, row.DisplayName, nullStringPtr(row.Email), row.EmailVerifiedAt.Valid, row.Managed, row.HasPassword || row.HasTotp}, err
	}
	row, err := r.pg.GetAccountProfile(ctx, accountID)
	if isNoRows(err) {
		return AccountProfile{}, domain.ErrNotFound
	}
	return AccountProfile{row.Username, row.DisplayName, pgTextPtr(row.Email), row.EmailVerifiedAt.Valid, row.Managed, row.HasPassword || row.HasTotp}, err
}

// UpdateAccountProfile is the authenticated self-service writer. Its service
// caller locks the principal and consumes current account proof before calling.
// It writes the username and display name only: the email is never profile data.
func (r *Resolver) UpdateAccountProfile(ctx context.Context, accountID string, profile AccountProfile) error {
	if r.sq != nil {
		return accountConstraint(r.sq.UpdateAccountProfile(ctx, sqlitegen.UpdateAccountProfileParams{AccountID: accountID, Username: profile.Username, DisplayName: profile.DisplayName}))
	}
	return accountConstraint(r.pg.UpdateAccountProfile(ctx, pggen.UpdateAccountProfileParams{AccountID: accountID, Username: profile.Username, DisplayName: profile.DisplayName}))
}
