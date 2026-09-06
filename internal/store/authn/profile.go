package authn

import (
	"context"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// AccountProfile is contact metadata, separate from credential and provider keys.
type AccountProfile struct {
	Username         string
	DisplayName      string
	Email            string
	Managed          bool
	UsernameEditable bool
}

func (r *Resolver) AccountProfile(ctx context.Context, accountID string) (AccountProfile, error) {
	if r.sq != nil {
		row, err := r.sq.GetAccountProfile(ctx, accountID)
		if isNoRows(err) {
			return AccountProfile{}, domain.ErrNotFound
		}
		return AccountProfile{row.Username, row.DisplayName, row.Email, row.Managed, row.HasPassword || row.HasTotp}, err
	}
	row, err := r.pg.GetAccountProfile(ctx, accountID)
	if isNoRows(err) {
		return AccountProfile{}, domain.ErrNotFound
	}
	return AccountProfile{row.Username, row.DisplayName, row.Email, row.Managed, row.HasPassword || row.HasTotp}, err
}

// UpdateAccountProfile is the authenticated self-service writer. Its service
// caller locks the principal and consumes current account proof before calling.
func (r *Resolver) UpdateAccountProfile(ctx context.Context, accountID string, profile AccountProfile) error {
	if r.sq != nil {
		return accountConstraint(r.sq.UpdateAccountProfile(ctx, sqlitegen.UpdateAccountProfileParams{AccountID: accountID, Username: profile.Username, DisplayName: profile.DisplayName, Email: profile.Email}))
	}
	return accountConstraint(r.pg.UpdateAccountProfile(ctx, pggen.UpdateAccountProfileParams{AccountID: accountID, Username: profile.Username, DisplayName: profile.DisplayName, Email: profile.Email}))
}
