package service

import (
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// CheckPasswordConfiguration prevents a new serving configuration from making
// existing passwords unusable. Public verification retains one admitted cost
// for known and unknown accounts. Restored, superseded credentials are inert
// already and are not a reason to reject the operator's recovery configuration.
func (s *Auth) CheckPasswordConfiguration(ctx context.Context) error {
	return tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		count, err := az.IncompatiblePasswordKDFCount(ctx, authz.KDFParams(s.KDF))
		if err != nil {
			return err
		}
		if count != 0 {
			return errors.New("Argon2 parameters differ from existing password credentials; restore the previous Argon2 configuration before serving")
		}
		return nil
	})
}
