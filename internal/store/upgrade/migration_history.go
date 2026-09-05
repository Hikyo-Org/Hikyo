package upgrade

import (
	"context"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// Preflight runs on the exclusion-owning physical connection before goose can
// create its table or execute SQL. The authenticated source digest must name an
// exact prefix of immutable target bytes. Crash progress may extend that prefix,
// but cannot omit an original migration, introduce a hole or name unknown SQL.
func (s *Session) checkMigrationHistory(ctx context.Context, state State, target releaseidentity.MigrationManifest) error {
	sourceCount := -1
	for count := 0; count <= len(target.Entries); count++ {
		prefix := releaseidentity.MigrationManifest{Engine: target.Engine, Entries: target.Entries[:count]}
		digest, err := prefix.Digest()
		if err != nil {
			return err
		}
		if digest == state.Pending.SourceMigrationDigest {
			sourceCount = count
			break
		}
	}
	if sourceCount < 0 {
		return ErrConflict
	}
	applied, err := inspectAppliedWith(ctx, func(ctx context.Context, q string, args ...any) (catalogRows, error) {
		rows, err := s.conn.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		return sqlCatalogRows{rows}, nil
	}, s.engine)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		if sourceCount == 0 && state.Pending.Source.Genesis == FreshGenesis {
			return nil
		}
		return ErrConflict
	}
	if len(applied)-1 < sourceCount || len(applied)-1 > len(target.Entries) {
		return ErrConflict
	}
	for i, version := range applied[1:] {
		if version != int64(target.Entries[i].Version) {
			return ErrConflict
		}
	}
	return nil
}
