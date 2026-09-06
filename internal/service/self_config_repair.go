package service

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// AuthorizeRepairScope permits access to the bound configuration hierarchy
// while an application generation is unavailable. Normal operation-specific
// authorization still runs in the handler. A stale graph never gains general
// tenant access through this recovery exception.
func (s *SelfConfig) AuthorizeRepairScope(ctx context.Context, actor Actor, scope domain.Scope) error {
	if scope.Org == "" {
		return ErrSelfConfigUnavailable
	}
	if _, err := scope.Level(); err != nil {
		return ErrSelfConfigUnavailable
	}
	return tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpSelfConfigStatus, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		binding, err := r.SelfConfig().Binding(ctx, proof)
		if err != nil {
			return err
		}
		if string(scope.Org) != binding.OrgID || (scope.Project != "" && string(scope.Project) != binding.ProjectID) || (scope.Env != "" && string(scope.Env) != binding.EnvironmentID) {
			return ErrSelfConfigUnavailable
		}
		return nil
	})
}
