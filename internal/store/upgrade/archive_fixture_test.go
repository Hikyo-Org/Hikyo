package upgrade_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/jackc/pgx/v5"
)

func testConfig(t *testing.T, engine releaseidentity.Engine) upgrade.Config {
	t.Helper()
	if engine == releaseidentity.SQLite {
		return upgrade.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "ledger.db")}
	}
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires PostgreSQL ledger acceptance")
		}
		t.Skip("HIKYO_TEST_POSTGRES_DSN not set")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("ledger_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(t.Context(), `CREATE DATABASE "`+name+`"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := admin.ExecContext(context.Background(), `DROP DATABASE "`+name+`" WITH (FORCE)`)
		if err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	u.Path = "/" + name
	return upgrade.Config{Engine: engine, DSN: u.String()}
}

func both(t *testing.T, fn func(*testing.T, upgrade.Config)) {
	t.Helper()
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) { fn(t, testConfig(t, engine)) })
	}
}

func schemaConfig(cfg upgrade.Config) store.Config {
	return store.Config{Engine: store.Engine(cfg.Engine), Path: cfg.Path, DSN: cfg.DSN}
}

// Archive acceptance is an external integration harness: authority comes from
// actual source inspection, while production F5 must additionally verify the
// signed release/backup evidence represented by these structurally valid claims.
func legacyOperation(t *testing.T, cfg upgrade.Config, manifest releaseidentity.MigrationManifest) upgrade.Operation {
	t.Helper()
	source, err := upgrade.InspectInstalled(t.Context(), cfg, manifest)
	if err != nil {
		t.Fatal(err)
	}
	var incarnation, nonce upgrade.Incarnation
	incarnation[0] = 1
	nonce[0] = 17
	now := time.Now().UTC().Add(-time.Second)
	route := releaseidentity.Hash([]byte("route"))
	floor := releaseidentity.SnapshotFloor{MetadataSequence: 1, MetadataSHA256: releaseidentity.Hash([]byte("metadata")), HighestReleaseSequence: 100, CatalogSequence: 1, CatalogSHA256: releaseidentity.Hash([]byte("catalog"))}
	return upgrade.Operation{
		SourceSchemaDigest: source.SchemaDigest, TargetSchemaDigest: releaseidentity.Hash([]byte("target schema")),
		RouteSource: source.Source, Source: source.Source,
		Target:                releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.0.1", Sequence: 1, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compatibility")), ManifestSHA256: releaseidentity.Hash([]byte("1"))},
		SourceMigrationDigest: source.MigrationDigest, TargetMigrationDigest: releaseidentity.Hash([]byte("target migrations")),
		RouteDigest: route, Generation: 1, RouteLength: 1, Phase: upgrade.Prepared, BackupID: "backup_fixture", RecoveryIncarnation: incarnation,
		Acceptance: upgrade.Acceptance{Floor: floor, ReleaseRootDigest: releaseidentity.Hash([]byte("pinned root")), Attestation: &upgrade.AttestationUse{
			Authority: "legacy-proposal/v1", Nonce: nonce, EvidenceDigest: releaseidentity.Hash([]byte("verified evidence fixture")), OperatorKeyID: releaseidentity.Hash([]byte("operator public key")),
			InstanceID: source.InstanceID, RestoreEpoch: source.RestoreEpoch, RecoveryIncarnation: incarnation, RouteGeneration: 1, RouteDigest: route, IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		}},
	}
}

// The current root restore API checks table inventory before invoking its
// mutation callback. This fixture creates empty control tables first so the
// actual row import and authority reconciliation can be tested. F5 integration
// must move this same primitive into the owned restore transaction before load.
func prepareArchiveControlFixture(t *testing.T, cfg upgrade.Config, manifest releaseidentity.MigrationManifest, schema releaseidentity.Digest) {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(t.Context())
	tx, err := conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if err := upgrade.PreparePostgresRestoreControlSchema(t.Context(), tx, manifest, schema); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
}
