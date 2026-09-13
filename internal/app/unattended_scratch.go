package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

type unattendedScratchOwnership struct {
	Format       string                 `json:"format"`
	DSN          releaseidentity.Digest `json:"dsn"`
	Database     string                 `json:"database"`
	SourceSchema releaseidentity.Digest `json:"source_schema"`
}

// prepareUnattendedScratch never adopts populated operator-provided scratch.
// Its private ownership record pins the exact DSN and signed schema before the
// first restore, allowing only that dedicated target to be reused after crash.
func prepareUnattendedScratch(ctx context.Context, cfg *config.Config, directory string, sourceSchema releaseidentity.Digest) (store.Config, func() error, error) {
	if cfg.Store.Engine == config.EngineSQLite {
		work, err := os.MkdirTemp(directory, "scratch-")
		if err != nil {
			return store.Config{}, nil, err
		}
		return store.Config{Engine: store.EngineSQLite, Path: filepath.Join(work, "restore.db")}, func() error { return os.RemoveAll(work) }, nil
	}
	if cfg.Upgrade.ScratchPostgresDSN == "" {
		return store.Config{}, nil, errors.New("unattended PostgreSQL requires its configured scratch database")
	}
	scratchName, err := upgrade.SeparatePostgresScratch(ctx, upgrade.Config{Engine: releaseidentity.Postgres, DSN: cfg.Store.DSN}, upgrade.Config{Engine: releaseidentity.Postgres, DSN: cfg.Upgrade.ScratchPostgresDSN})
	if err != nil {
		return store.Config{}, nil, err
	}
	path := filepath.Join(directory, "scratch-ownership.json")
	var ownership unattendedScratchOwnership
	raw, err := readUnattendedPrivate(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return store.Config{}, nil, err
	}
	dsnDigest := releaseidentity.Hash([]byte(cfg.Upgrade.ScratchPostgresDSN))
	if exists && (definitions.DecodeStrict(raw, &ownership) != nil || ownership.Format != "hikyo.unattended-scratch/v1" || ownership.DSN != dsnDigest || ownership.Database != scratchName || ownership.SourceSchema.Validate() != nil) {
		return store.Config{}, nil, errors.New("scratch ownership differs from configured database; refusing cleanup")
	}
	database := upgrade.Config{Engine: releaseidentity.Postgres, DSN: cfg.Upgrade.ScratchPostgresDSN}
	empty := releaseidentity.MigrationManifest{Engine: releaseidentity.Postgres, Entries: []releaseidentity.Migration{}}
	inspected, inspectErr := upgrade.Inspect(ctx, database, empty)
	fresh := inspectErr == nil && inspected.Genesis == releaseidentity.FreshGenesisV1
	if !fresh {
		if !exists {
			return store.Config{}, nil, errors.New("scratch PostgreSQL database must be empty before first enrollment")
		}
		if err := upgrade.WithLock(ctx, database, func(session *upgrade.Session) error { return session.ResetOwnedScratch(ctx, ownership.SourceSchema) }); err != nil {
			return store.Config{}, nil, errors.New("owned scratch schema differs from authenticated source; refusing cleanup")
		}
		inspected, inspectErr = upgrade.Inspect(ctx, database, empty)
		if inspectErr != nil || inspected.Genesis != releaseidentity.FreshGenesisV1 {
			return store.Config{}, nil, errors.New("scratch reset did not restore a fresh PostgreSQL database")
		}
	}
	ownership = unattendedScratchOwnership{Format: "hikyo.unattended-scratch/v1", DSN: dsnDigest, Database: scratchName, SourceSchema: sourceSchema}
	raw, err = json.Marshal(ownership)
	if err != nil {
		return store.Config{}, nil, err
	}
	if err := writeAutomaticFile(path, raw, 0600); err != nil {
		return store.Config{}, nil, err
	}
	// PostgreSQL is retained until the next verified use; the ownership record
	// permits cleanup even when a process dies immediately after restoring.
	return store.Config{Engine: store.EnginePostgres, DSN: database.DSN}, func() error { return nil }, nil
}
