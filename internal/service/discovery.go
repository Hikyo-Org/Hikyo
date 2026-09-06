package service

import (
	"context"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Discovery exposes the stable instance identity without tenant metadata.
// It is the same persisted identity used by the pinned remote-add check.
type Discovery struct{ DB *store.DB }

func (s *Discovery) InstanceIdentity(ctx context.Context) (string, error) {
	var identity string
	err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		var err error
		identity, err = az.InstanceIdentity(ctx)
		return err
	})
	return identity, err
}
