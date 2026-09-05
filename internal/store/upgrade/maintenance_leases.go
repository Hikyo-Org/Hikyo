package upgrade

import (
	"context"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"math"
)

// Lease invalidation shares the control/pending transaction. A stale worker
// never regains its old lease when maintenance later clears.
func (s *Session) invalidateSingletonLeases(ctx context.Context, state State) error {
	if state.Pending.Phase != Prepared || state.Pending.Source.Genesis == FreshGenesis {
		return nil
	}
	empty := releaseidentity.MigrationManifest{Engine: s.engine, Entries: []releaseidentity.Migration{}}
	emptyDigest, err := empty.Digest()
	if err != nil {
		return err
	}
	if state.Pending.SourceMigrationDigest == emptyDigest {
		catalog, err := s.DomainCatalog(ctx)
		if err != nil {
			return err
		}
		if report, err := validateGenesis(catalog, empty); err == nil && report.Genesis == FreshGenesis {
			return nil
		}
	}
	return s.revokeSingletonLeases(ctx)
}

func (s *Session) revokeSingletonLeases(ctx context.Context) error {
	var overflow int64
	if err := s.conn.QueryRowContext(ctx, `SELECT count(*) FROM singleton_leases WHERE fence_token >= $1`, int64(math.MaxInt64)).Scan(&overflow); err != nil {
		return err
	}
	if overflow != 0 {
		return ErrConflict
	}
	stamp := "'1970-01-01T00:00:00Z'"
	if s.engine == releaseidentity.Postgres {
		stamp = "TIMESTAMPTZ '1970-01-01 00:00:00+00'"
	}
	_, err := s.conn.ExecContext(ctx, `UPDATE singleton_leases SET fence_token=fence_token+1,expires_at=`+stamp)
	return err
}
