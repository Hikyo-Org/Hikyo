package upgrade

import (
	"context"
	"database/sql"
	"errors"
	"os"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// InspectControl reads stored trust floors and pending phase without claiming
// that the domain schema matches Applied. After schema writes, the gate must
// inspect the actual source/target separately under migration exclusion.
// Missing files and genuinely absent control objects return ErrAbsent; partial
// or malformed control storage refuses. This path never creates or migrates.
func InspectControl(ctx context.Context, cfg Config) (State, error) {
	if err := cfg.Engine.Validate(); err != nil {
		return State{}, err
	}
	if cfg.Engine == releaseidentity.SQLite {
		path, err := canonicalSQLite(cfg.Path)
		if err != nil {
			return State{}, err
		}
		cfg.Path = path
		if _, err := checkedFile(path); errors.Is(err, os.ErrNotExist) {
			return State{}, ErrAbsent
		} else if err != nil {
			return State{}, err
		}
	}
	db, err := open(cfg, true)
	if err != nil {
		return State{}, err
	}
	defer db.Close()
	options := &sql.TxOptions{ReadOnly: true}
	if cfg.Engine == releaseidentity.Postgres {
		options.Isolation = sql.LevelRepeatableRead
	}
	tx, err := db.BeginTx(ctx, options)
	if err != nil {
		return State{}, err
	}
	defer tx.Rollback()
	catalog, err := inspectCatalogObjectsWith(ctx, func(ctx context.Context, query string, args ...any) (catalogRows, error) {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		return sqlCatalogRows{rows}, nil
	}, cfg.Engine)
	if err != nil {
		return State{}, err
	}
	_, control, err := splitControl(catalog)
	if err != nil {
		return State{}, err
	}
	if len(control) == 0 {
		return State{}, ErrAbsent
	}
	if _, err := withoutControl(catalog); err != nil {
		return State{}, err
	}
	state, err := ReadSQLiteSnapshot(ctx, tx)
	if err != nil {
		return State{}, err
	}
	if err := tx.Commit(); err != nil {
		return State{}, err
	}
	return state, nil
}
