package upgrade

import (
	"context"
	"database/sql"
	"errors"
	"os"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// InstalledSource describes observed source data. A pre-ledger source has no
// Ledger, incarnation or generation. Operator bootstrap proposals are separate
// evidence and must never be represented as already-installed authority.
type InstalledSource struct {
	Source             Source                 `json:"source"`
	MigrationDigest    releaseidentity.Digest `json:"migration_digest"`
	SchemaDigest       releaseidentity.Digest `json:"schema_digest"`
	InstanceID         string                 `json:"instance_id"`
	RestoreEpoch       int64                  `json:"restore_epoch"`
	Ledger             *State                 `json:"ledger,omitempty"`
	RequiresLegacyStop bool                   `json:"requires_legacy_stop"`
	RequiresBackup     bool                   `json:"requires_backup"`
}

// InspectSQLiteSource must run on one existing snapshot/transaction. It discloses
// no domain rows; the complete catalog is hashed, not returned as tenant data.
func InspectSQLiteSource(ctx context.Context, q SQLSnapshotQueries, manifest releaseidentity.MigrationManifest) (InstalledSource, error) {
	if manifest.Engine != releaseidentity.SQLite {
		return InstalledSource{}, ErrConflict
	}
	catalog, err := inspectCatalog(ctx, q, releaseidentity.SQLite)
	if err != nil {
		return InstalledSource{}, err
	}
	return inspectSource(catalog, manifest, func(query string, args ...any) scanner { return q.QueryRowContext(ctx, query, args...) })
}

func InspectPostgresSource(ctx context.Context, q PGSnapshotQueries, manifest releaseidentity.MigrationManifest) (InstalledSource, error) {
	if manifest.Engine != releaseidentity.Postgres {
		return InstalledSource{}, ErrConflict
	}
	catalog, err := inspectCatalogWith(ctx, func(ctx context.Context, query string, args ...any) (catalogRows, error) {
		return q.Query(ctx, query, args...)
	}, releaseidentity.Postgres)
	if err != nil {
		return InstalledSource{}, err
	}
	return inspectSource(catalog, manifest, func(query string, args ...any) scanner { return q.QueryRow(ctx, query, args...) })
}

func inspectSource(catalog Catalog, manifest releaseidentity.MigrationManifest, row func(string, ...any) scanner) (InstalledSource, error) {
	if controlPresent(catalog) {
		state, err := scanState(row(snapshotSQL))
		if err != nil {
			return InstalledSource{}, err
		}
		digest, err := manifest.Digest()
		if err != nil {
			return InstalledSource{}, err
		}
		if state.MigrationDigest != digest {
			return InstalledSource{}, ErrConflict
		}
		// Full control catalog validation and stripping is deliberately exact:
		// added columns, indexes or triggers on control are corruption too.
		catalog, err = withoutControl(catalog)
		if err != nil {
			return InstalledSource{}, err
		}
		if catalog.Digest() != state.SchemaDigest {
			return InstalledSource{}, ErrConflict
		}
		if !appliedMatches(catalog.Applied, manifest) {
			return InstalledSource{}, ErrConflict
		}
		if state.Applied.Genesis == LegacyGenesis {
			if _, err := validateGenesis(catalog, manifest); err != nil {
				return InstalledSource{}, err
			}
		}
		if err := checkInstanceEpoch(state, func(q string) scanner { return row(q) }); err != nil {
			return InstalledSource{}, err
		}
		return InstalledSource{Source: state.Applied, MigrationDigest: digest, SchemaDigest: catalog.Digest(), InstanceID: state.InstanceID, RestoreEpoch: state.RestoreEpoch, Ledger: &state}, nil
	}
	report, err := validateGenesis(catalog, manifest)
	if err != nil {
		return InstalledSource{}, err
	}
	out := InstalledSource{Source: Source{Genesis: report.Genesis}, MigrationDigest: report.MigrationDigest, SchemaDigest: report.CatalogDigest, RequiresLegacyStop: report.RequiresLegacyStop, RequiresBackup: report.RequiresBackup}
	if report.Genesis == LegacyGenesis {
		if err := row(`SELECT identity FROM instance_identity WHERE id=1`).Scan(&out.InstanceID); err != nil {
			return InstalledSource{}, err
		}
		var credential, restored int64
		if err := row(`SELECT credential_epoch,restore_epoch FROM auth_instance_state WHERE id=1`).Scan(&credential, &restored); err != nil {
			return InstalledSource{}, err
		}
		if out.InstanceID == "" || credential < 0 || restored < 0 {
			return InstalledSource{}, ErrCorrupt
		}
		out.RestoreEpoch = max(credential, restored)
	}
	return out, nil
}

func appliedMatches(applied []int64, manifest releaseidentity.MigrationManifest) bool {
	if len(applied) != len(manifest.Entries)+1 || applied[0] != 0 {
		return false
	}
	for i, entry := range manifest.Entries {
		if applied[i+1] != int64(entry.Version) {
			return false
		}
	}
	return true
}

// InspectInstalled opens one read-only snapshot for local preparation tooling.
// No SQLite database/goose/control table is created by this path.
func InspectInstalled(ctx context.Context, cfg Config, manifest releaseidentity.MigrationManifest) (InstalledSource, error) {
	if err := cfg.Engine.Validate(); err != nil {
		return InstalledSource{}, err
	}
	if cfg.Engine == releaseidentity.SQLite {
		path, err := canonicalSQLite(cfg.Path)
		if err != nil {
			return InstalledSource{}, err
		}
		cfg.Path = path
		if _, err := checkedFile(path); errors.Is(err, os.ErrNotExist) {
			catalog := Catalog{Format: "hikyo-schema/v1", Engine: cfg.Engine, Objects: []string{}, Applied: []int64{}}
			report, err := validateGenesis(catalog, manifest)
			if err != nil {
				return InstalledSource{}, err
			}
			return InstalledSource{Source: Source{Genesis: FreshGenesis}, MigrationDigest: report.MigrationDigest, SchemaDigest: report.CatalogDigest}, nil
		} else if err != nil {
			return InstalledSource{}, err
		}
	}
	db, err := open(cfg, true)
	if err != nil {
		return InstalledSource{}, err
	}
	defer db.Close()
	options := &sql.TxOptions{ReadOnly: true}
	if cfg.Engine == releaseidentity.Postgres {
		options.Isolation = sql.LevelRepeatableRead
	}
	tx, err := db.BeginTx(ctx, options)
	if err != nil {
		return InstalledSource{}, err
	}
	defer tx.Rollback()
	catalog, err := inspectCatalog(ctx, tx, cfg.Engine)
	if err != nil {
		return InstalledSource{}, err
	}
	out, err := inspectSource(catalog, manifest, func(query string, args ...any) scanner { return tx.QueryRowContext(ctx, query, args...) })
	if err != nil {
		return InstalledSource{}, err
	}
	if err := tx.Commit(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return InstalledSource{}, err
	}
	return out, nil
}

// The ledger epoch captures the strongest credential invalidation stamp, not
// only the restore-specific column. Credential-only rotation before adoption
// legitimately leaves credential_epoch greater than restore_epoch. Later normal
// credential revocation may advance it further without representing a restore.
func checkInstanceEpoch(state State, row func(string) scanner) error {
	var instance string
	if err := row(`SELECT identity FROM instance_identity WHERE id=1`).Scan(&instance); err != nil {
		return err
	}
	var credential, restored int64
	if err := row(`SELECT credential_epoch,restore_epoch FROM auth_instance_state WHERE id=1`).Scan(&credential, &restored); err != nil {
		return err
	}
	if instance != state.InstanceID || credential < state.RestoreEpoch || restored < 0 || restored > state.RestoreEpoch {
		return ErrConflict
	}
	return nil
}
