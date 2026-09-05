package upgrade

import (
	"context"
	"embed"
	"errors"
	"io/fs"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// BuildScratchSchema is exclusively for release build tooling. It returns
// canonical before/after catalogs, never runtime admission or driver handles.
// The actual database must be empty under the same lock and physical connection
// used for embedded migration execution. An existing control ledger is refused.
func BuildScratchSchema(ctx context.Context, cfg Config, source embed.FS, directory string) (initial Catalog, target Catalog, err error) {
	if _, err := releaseidentity.BuildMigrationManifest(source, directory, cfg.Engine); err != nil {
		return Catalog{}, Catalog{}, err
	}
	migrations, err := fs.Sub(source, directory)
	if err != nil {
		return Catalog{}, Catalog{}, err
	}
	err = WithLock(ctx, cfg, func(s *Session) error {
		var err error
		initial, err = inspectCatalog(ctx, s.conn, cfg.Engine)
		if err != nil {
			return err
		}
		empty := len(initial.Objects) == 0
		if cfg.Engine == releaseidentity.Postgres {
			empty = len(initial.Objects) == 1 && initial.Objects[0] == `["schema", "public"]`
		}
		if !empty || len(initial.Applied) != 0 {
			return errors.New("release schema generator refuses a nonempty database")
		}
		if err := s.applyEmbedded(ctx, migrations); err != nil {
			return err
		}
		target, err = inspectCatalog(ctx, s.conn, cfg.Engine)
		return err
	})
	return initial, target, err
}
