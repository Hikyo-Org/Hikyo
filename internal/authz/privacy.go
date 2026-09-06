package authz

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

type PrivacyAccountView = authn.PrivacyAccountView
type PrivacyActivity = authn.PrivacyActivity

func (a *TxAuthorizer) PrivacyAccount(ctx context.Context, p string) (PrivacyAccountView, error) {
	return a.r.PrivacyAccount(ctx, p)
}
func (a *TxAuthorizer) PrivacyActivity(ctx context.Context, p string) ([]PrivacyActivity, error) {
	return a.r.PrivacyActivity(ctx, p)
}
func (a *TxAuthorizer) RestrictPrivacyPrincipal(ctx context.Context, p, state string) error {
	return a.r.RestrictPrivacyPrincipal(ctx, p, state)
}
func (a *TxAuthorizer) ErasePrivacyAccount(ctx context.Context, account, p, username string) error {
	return a.r.ErasePrivacyAccount(ctx, account, p, username)
}

type PrivacySession = authn.PrivacySession

func (a *TxAuthorizer) PrivacySessions(ctx context.Context, p string) ([]PrivacySession, error) {
	return a.r.PrivacySessions(ctx, p)
}

func (a *TxAuthorizer) CorrectPrivacyAccount(ctx context.Context, account, username, displayName string) error {
	return a.r.CorrectPrivacyAccount(ctx, account, username, displayName)
}
