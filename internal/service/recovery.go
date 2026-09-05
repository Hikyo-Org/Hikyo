package service

import (
	"context"
	"fmt"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Recovery is the bounded operator drill surface. Its database cannot be used
// by ordinary services and every operation retains the active scratch guard.
type Recovery struct{ DB *store.RecoveryDB }

func (s *Recovery) Status(ctx context.Context) (Status, error) {
	return restoreStatus(ctx, func(ctx context.Context, fn tx.ReadFn) error { return tx.RecoveryRead(ctx, s.DB, fn) })
}
func (s *Recovery) Reconcile(ctx context.Context, principal domain.PrincipalID) (Status, error) {
	return restoreReconcile(ctx, func(ctx context.Context, fn tx.RestoreFn) error { return tx.RecoveryReconcile(ctx, s.DB, fn) }, principal)
}
func (s *Recovery) ProveValuesReadable(ctx context.Context, kr *crypto.Keyring) (bool, error) {
	return proveValuesReadable(ctx, func(ctx context.Context, fn tx.ReadFn) error { return tx.RecoveryRead(ctx, s.DB, fn) }, kr)
}

// MintAndRevoke exercises the same authorization, policy, aggregate writes and
// audit events as the runtime identity surface. No credential is returned.
func (s *Recovery) MintAndRevoke(ctx context.Context, kr *crypto.Keyring, principal domain.PrincipalID, scope domain.Scope) error {
	ids := &Identities{Auth: &Auth{Keyring: kr}}
	actor := LocalPrincipal(principal)
	write := func(ctx context.Context, fn tx.WriteFn) error { return tx.RecoveryWrite(ctx, s.DB, fn) }
	sa, err := ids.createServiceAccount(ctx, write, actor, scope, "drill-verification", domain.ClassWorkload)
	if err != nil {
		return fmt.Errorf("create drill service account: %w", err)
	}
	if _, err := ids.mintCredential(ctx, write, actor, scope, sa.ID, MintRequest{}); err != nil {
		return fmt.Errorf("mint drill credential: %w", err)
	}
	if err := ids.deleteServiceAccount(ctx, write, actor, scope, sa.ID); err != nil {
		return fmt.Errorf("revoke drill credential: %w", err)
	}
	return nil
}
