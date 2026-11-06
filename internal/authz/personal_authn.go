package authz

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// PersonalValueOwners lists every principal holding a personal value, so a
// restore purges those owners rather than visiting every principal.
func (a *TxAuthorizer) PersonalValueOwners(ctx context.Context) ([]domain.PrincipalID, error) {
	return a.r.PersonalValueOwners(ctx)
}
