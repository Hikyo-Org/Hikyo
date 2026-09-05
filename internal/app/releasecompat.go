package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// ReleaseCompatibilityRequest is build tooling input, never runtime admission.
// Sources come from a reviewed source-owned declaration. PostgreSQLDSN must
// identify an isolated empty scratch database; nonempty targets refuse writes.
type ReleaseCompatibilityRequest struct {
	Profile       releaseidentity.Profile
	Version       string
	Sequence      uint64
	Commit        string
	Sources       map[releaseidentity.Engine][]upgradecompat.SourceEdge
	PostgreSQLDSN string
}

// GenerateReleaseCompatibility applies actual embedded migrations to both
// scratch engines and uses the same canonical domain catalog inspector as boot.
func GenerateReleaseCompatibility(ctx context.Context, request ReleaseCompatibilityRequest) ([]byte, error) {
	if err := releaseidentity.ValidateTarget(request.Profile, request.Version, request.Sequence, request.Commit); err != nil {
		return nil, err
	}
	if request.PostgreSQLDSN == "" || len(request.Sources) != 2 {
		return nil, errors.New("release generation requires both engine source declarations and isolated PostgreSQL")
	}
	dir, err := os.MkdirTemp("", "hikyo-release-schema-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	declaration := upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: request.Profile, Version: request.Version, Sequence: request.Sequence, Commit: request.Commit, Engines: []upgradecompat.EngineDeclaration{}}
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		sources, ok := request.Sources[engine]
		if !ok || sources == nil {
			return nil, errors.New("missing source-owned engine edges")
		}
		manifest, err := releaseidentity.BuildMigrationManifest(store.MigrationsFS, "migrations/"+string(engine), engine)
		if err != nil {
			return nil, err
		}
		cfg := upgrade.Config{Engine: engine, Path: filepath.Join(dir, "schema.db"), DSN: request.PostgreSQLDSN}
		initial, catalog, err := upgrade.BuildScratchSchema(ctx, cfg, store.MigrationsFS, "migrations/"+string(engine))
		if err != nil {
			return nil, err
		}
		if len(catalog.Applied) != len(manifest.Entries)+1 || catalog.Applied[0] != 0 {
			return nil, errors.New("generated schema applied set differs from embedded migrations")
		}
		for i, migration := range manifest.Entries {
			if catalog.Applied[i+1] != int64(migration.Version) {
				return nil, errors.New("generated schema omitted or substituted a migration")
			}
		}
		for _, source := range sources {
			if source.Source.Genesis == releaseidentity.LegacyGenesisV1 {
				legacy, err := upgrade.PinnedLegacyManifest(engine)
				if err != nil {
					return nil, err
				}
				legacyDigest, err := legacy.Digest()
				if err != nil {
					return nil, err
				}
				sourceDigest, err := source.Migrations.Digest()
				if err != nil {
					return nil, err
				}
				legacySchema, err := upgrade.PinnedLegacySchemaDigest(engine)
				if err != nil {
					return nil, err
				}
				if sourceDigest != legacyDigest || source.SchemaSHA256 != legacySchema {
					return nil, errors.New("legacy source differs from pinned approved genesis")
				}
			}
			if source.Source.Genesis == releaseidentity.FreshGenesisV1 && source.SchemaSHA256 != initial.Digest() {
				return nil, errors.New("fresh source digest differs from actual empty catalog")
			}
			if len(source.Migrations.Entries) > len(manifest.Entries) {
				return nil, errors.New("source migration inventory is ahead of target")
			}
			for i, entry := range source.Migrations.Entries {
				if entry != manifest.Entries[i] {
					return nil, fmt.Errorf("target changes previously applied migration %d", entry.Version)
				}
			}
		}
		declaration.Engines = append(declaration.Engines, upgradecompat.EngineDeclaration{Migrations: manifest, SchemaSHA256: catalog.Digest(), Sources: sources})
	}
	if err := declaration.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(declaration)
}
