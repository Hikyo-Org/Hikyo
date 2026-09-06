package authz

import (
	"context"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

type AccountProfile = authn.AccountProfile

func (a *TxAuthorizer) AccountProfile(ctx context.Context, accountID string) (AccountProfile, error) {
	return a.r.AccountProfile(ctx, accountID)
}
func (a *TxAuthorizer) UpdateAccountProfile(ctx context.Context, accountID string, profile AccountProfile) error {
	return a.r.UpdateAccountProfile(ctx, accountID, profile)
}
