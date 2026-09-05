package keyring

import (
	"context"
	"errors"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// RecoveryStore opens existing restored keys under an active scratch capability.
// It cannot mint a missing hierarchy, even if LoadKeyring is called first.
type RecoveryStore struct{ DB *store.RecoveryDB }

var _ crypto.KeyStore = (*RecoveryStore)(nil)

func (s *RecoveryStore) operations() operations {
	return operations{
		read:  func(ctx context.Context, fn tx.ReadFn) error { return tx.RecoveryRead(ctx, s.DB, fn) },
		write: func(ctx context.Context, fn tx.WriteFn) error { return tx.RecoveryWrite(ctx, s.DB, fn) },
	}
}

func (s *RecoveryStore) ActiveMasterWrappers(ctx context.Context) ([]crypto.WrappedKey, error) {
	return s.operations().ActiveMasterWrappers(ctx)
}

func (s *RecoveryStore) ActiveTier3(ctx context.Context, p crypto.Purpose, orgID, projectID string) (crypto.WrappedKey, error) {
	return s.operations().ActiveTier3(ctx, p, orgID, projectID)
}

func (s *RecoveryStore) Tier3Versions(ctx context.Context, p crypto.Purpose, orgID, projectID string) ([]crypto.WrappedKey, error) {
	return s.operations().Tier3Versions(ctx, p, orgID, projectID)
}

func (s *RecoveryStore) AllOpenableTier3(ctx context.Context) ([]crypto.WrappedKey, error) {
	return s.operations().AllOpenableTier3(ctx)
}

func (s *RecoveryStore) CreateHierarchy(ctx context.Context, master crypto.WrappedKey, tier3 []crypto.WrappedKey) error {
	return errors.New("recovery requires the existing archived hierarchy")
}

func (s *RecoveryStore) CreateTier3(ctx context.Context, key crypto.WrappedKey) error {
	return s.operations().CreateTier3(ctx, key)
}
