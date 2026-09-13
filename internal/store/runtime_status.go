package store

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

// RuntimeUpgradeState reads only control metadata, including while admission
// refuses tenant transactions. It must never become a general query escape.
func (d *DB) RuntimeUpgradeState(ctx context.Context) (upgrade.State, error) {
	if d.engine == EnginePostgres {
		return upgrade.ReadPostgresSnapshot(ctx, d.pool.raw())
	}
	return upgrade.ReadSQLiteSnapshot(ctx, d.sqRead)
}
