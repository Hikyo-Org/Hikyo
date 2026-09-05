package upgrade

import "context"

// PrepareRecovery begins a new explicitly same-release recovery operation.
// The old pending proof remains invalidated until NEW backup evidence for this
// restored incarnation is consumed atomically. One recovery operation has no
// migration edges and can never authorize ApplyMigrations or a downgrade.
func (s *Session) PrepareRecovery(ctx context.Context, expected State, operation Operation) (State, error) {
	if expected.Validate() != nil || expected.Pending == nil || !expected.Pending.Invalidated || expected.Pending.Phase != RestoreRequired || !expected.Maintenance || !expected.Applied.IsRelease() {
		return State{}, ErrConflict
	}
	generation, err := nextGeneration(expected.Generation)
	if err != nil {
		return State{}, err
	}
	if operation.Kind != RecoveryOperation || operation.Source != expected.Applied || operation.Target != expected.Applied.Release || operation.RouteSource != expected.Applied || operation.SourceSchemaDigest != expected.SchemaDigest || operation.SourceMigrationDigest != expected.MigrationDigest || operation.Generation != generation || operation.RecoveryIncarnation != expected.RecoveryIncarnation || operation.Phase != Prepared || operation.Hop != 0 || operation.RouteLength != 1 || operation.Invalidated || operation.BackupID == "" || operation.BackupID == expected.Pending.BackupID {
		return State{}, ErrConflict
	}
	next := expected
	next.Pending = &operation
	next.Generation = generation
	err = s.transaction(ctx, func() error {
		if err := s.compare(ctx, expected); err != nil {
			return err
		}
		if err := s.accept(ctx, &expected, &next); err != nil {
			return err
		}
		return s.persist(ctx, next, false)
	})
	if err != nil {
		return State{}, err
	}
	return next, nil
}

// ValidateRecoverySchema advances no goose migration. It confirms the actual
// restored source catalog under exclusion, then records readiness for the
// gate's existing-hierarchy and full configuration health checks.
func (s *Session) ValidateRecoverySchema(ctx context.Context, expected State) (State, error) {
	if expected.Validate() != nil || expected.Pending.Kind != RecoveryOperation || expected.Pending.Invalidated || expected.Pending.Phase != Prepared {
		return State{}, ErrConflict
	}
	next := expected
	pending := *expected.Pending
	pending.Phase = SchemaApplied
	next.Pending = &pending
	err := s.transaction(ctx, func() error {
		if err := s.compare(ctx, expected); err != nil {
			return err
		}
		catalog, err := s.DomainCatalog(ctx)
		if err != nil {
			return err
		}
		if catalog.Digest() != pending.TargetSchemaDigest {
			return ErrConflict
		}
		if err := checkInstanceEpoch(expected, func(q string) scanner { return s.conn.QueryRowContext(ctx, q) }); err != nil {
			return err
		}
		return s.persist(ctx, next, false)
	})
	if err != nil {
		return State{}, err
	}
	return next, nil
}
