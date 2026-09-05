package upgrade

import "context"

// ReconcileFreshInstance binds migration 20's seeded singleton to the identity
// atomically created before goose. It is restricted to the first schema-applied
// hop of a proven empty genesis, before any organization or principal exists.
// Existing installations, later route hops and ordinary restarts cannot rename
// an instance through this operation.
func (s *Session) ReconcileFreshInstance(ctx context.Context, expected State) error {
	if expected.Validate() != nil || expected.Applied.Genesis != FreshGenesis || expected.Generation != 1 || !expected.Maintenance || expected.Pending.Invalidated || expected.Pending.Phase != SchemaApplied || expected.Pending.Hop != 0 || expected.Pending.Source.Genesis != FreshGenesis || expected.Pending.RouteSource.Genesis != FreshGenesis || expected.RestoreEpoch != 0 {
		return ErrConflict
	}
	return s.transaction(ctx, func() error {
		current, err := s.Resume(ctx, expected)
		if err != nil {
			return err
		}
		catalog, err := s.DomainCatalog(ctx)
		if err != nil {
			return err
		}
		if catalog.Digest() != current.Pending.TargetSchemaDigest {
			return ErrConflict
		}
		var credential, restored int64
		if err := s.conn.QueryRowContext(ctx, `SELECT credential_epoch,restore_epoch FROM auth_instance_state WHERE id=1`).Scan(&credential, &restored); err != nil {
			return err
		}
		if credential != 1 || restored != 0 {
			return ErrConflict
		}
		for _, table := range []string{"orgs", "principals"} {
			var count int64
			if err := s.conn.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return ErrConflict
			}
		}
		var identity string
		if err := s.conn.QueryRowContext(ctx, `SELECT identity FROM instance_identity WHERE id=1`).Scan(&identity); err != nil {
			return err
		}
		if identity == current.InstanceID {
			return nil
		}
		result, err := s.conn.ExecContext(ctx, `UPDATE instance_identity SET identity=$1 WHERE id=1 AND identity=$2`, current.InstanceID, identity)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrConflict
		}
		return nil
	})
}
