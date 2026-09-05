package upgrade

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func testConfig(t *testing.T, engine releaseidentity.Engine) Config {
	t.Helper()
	if engine == releaseidentity.SQLite {
		return Config{Engine: engine, Path: filepath.Join(t.TempDir(), "ledger.db")}
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
	return Config{Engine: engine, DSN: u.String()}
}

func both(t *testing.T, fn func(*testing.T, Config)) {
	t.Helper()
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) { fn(t, testConfig(t, engine)) })
	}
}
func emptyManifest(engine releaseidentity.Engine) releaseidentity.MigrationManifest {
	return releaseidentity.MigrationManifest{Engine: engine, Entries: []releaseidentity.Migration{}}
}
func target(sequence uint64) releaseidentity.Identity {
	return releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: fmt.Sprintf("1.0.%d", sequence), Sequence: sequence, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compatibility")), ManifestSHA256: releaseidentity.Hash([]byte(fmt.Sprint(sequence)))}
}
func operation(source Source, manifest releaseidentity.MigrationManifest) Operation {
	digest, err := manifest.Digest()
	if err != nil {
		panic(err)
	}
	catalog := Catalog{Format: "hikyo-schema/v1", Engine: manifest.Engine, Objects: []string{}, Applied: []int64{}}
	if manifest.Engine == releaseidentity.Postgres {
		catalog.Objects = []string{`["schema", "public"]`}
	}
	op := Operation{SourceSchemaDigest: catalog.Digest(), TargetSchemaDigest: releaseidentity.Hash([]byte("target schema")), Acceptance: fixtureAcceptance(), RouteSource: source, Source: source, Target: target(1), SourceMigrationDigest: digest, TargetMigrationDigest: releaseidentity.Hash([]byte("target migrations")), RouteDigest: releaseidentity.Hash([]byte("route")), Generation: 1, RouteLength: 1, Phase: Prepared, BackupID: "backup_fixture"}
	if source.Genesis == LegacyGenesis {
		op.RecoveryIncarnation[0] = 1
	} // Explicit fixture proposal, never live-state proof.
	return op
}
func bootstrap(t *testing.T, cfg Config) State {
	t.Helper()
	var state State
	err := WithLock(t.Context(), cfg, func(s *Session) error {
		var err error
		state, err = s.Bootstrap(t.Context(), emptyManifest(cfg.Engine), operation(Source{Genesis: FreshGenesis}, emptyManifest(cfg.Engine)), Production)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}
func query(t *testing.T, cfg Config, sqlText string, args ...any) {
	t.Helper()
	db, err := open(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(t.Context(), sqlText, args...); err != nil {
		t.Fatal(err)
	}
}

func TestFreshBootstrapAndAllPhases(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		manifest := emptyManifest(cfg.Engine)
		report, err := Inspect(t.Context(), cfg, manifest)
		if err != nil || report.Genesis != FreshGenesis || report.RequiresBackup {
			t.Fatalf("inspection=%+v err=%v", report, err)
		}
		state := bootstrap(t, cfg)
		if state.RecoveryIncarnation == (Incarnation{}) || state.Generation != 1 || !state.Maintenance {
			t.Fatal(state)
		}
		err = WithLock(t.Context(), cfg, func(s *Session) error {
			resumed, err := s.Resume(t.Context(), state)
			if err != nil || !reflect.DeepEqual(resumed, state) {
				return fmt.Errorf("resume: %v", err)
			}
			for _, phase := range []Phase{SchemaWriteStarted, SchemaApplied, Healthy} {
				prior := state
				state, err = s.Advance(t.Context(), state, phase)
				if err != nil {
					return err
				}
				if _, err := s.Advance(t.Context(), prior, phase); !errors.Is(err, ErrConflict) {
					return fmt.Errorf("replay accepted: %v", err)
				}
			}
			if state.Maintenance || state.Applied.Release != target(1) {
				return errors.New("healthy identity wrong")
			}
			op := operation(state.Applied, manifest)
			op.Target = target(2)
			op.SourceMigrationDigest = state.MigrationDigest
			op.SourceSchemaDigest = state.SchemaDigest
			op.Generation = 2
			op.RecoveryIncarnation = state.RecoveryIncarnation
			op.Acceptance.Attestation = fixtureAttestation(state, op)
			state, err = s.Prepare(t.Context(), state, op)
			if err != nil {
				return err
			}
			_, err = s.Advance(t.Context(), state, RestoreRequired)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestLegacyGenesisAndMigrationTamper(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		if err := migrateFixture(t, cfg); err != nil {
			t.Fatal(err)
		}
		manifest, err := releaseidentity.BuildMigrationManifest(os.DirFS(".."), "migrations/"+string(cfg.Engine), cfg.Engine)
		if err != nil {
			t.Fatal(err)
		}
		report, err := Inspect(t.Context(), cfg, manifest)
		if err != nil || report.Genesis != LegacyGenesis || !report.RequiresBackup || !report.RequiresLegacyStop {
			t.Fatalf("inspection=%+v err=%v", report, err)
		}
		pinnedSchema, err := PinnedLegacySchemaDigest(cfg.Engine)
		if err != nil || pinnedSchema != report.CatalogDigest {
			t.Fatalf("published legacy declaration differs from actual source: %v", err)
		}
		tampered := manifest.Clone()
		tampered.Entries[0].SHA256 = releaseidentity.Hash([]byte("tampered SQL"))
		if _, err := Inspect(t.Context(), cfg, tampered); !errors.Is(err, ErrGenesis) {
			t.Fatalf("tampered manifest: %v", err)
		}
		err = WithLock(t.Context(), cfg, func(s *Session) error {
			_, err := s.Bootstrap(t.Context(), manifest, legacyOperation(t, cfg, manifest), Production)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func fixtureAcceptance() Acceptance {
	return Acceptance{Floor: releaseidentity.SnapshotFloor{MetadataSequence: 1, MetadataSHA256: releaseidentity.Hash([]byte("metadata")), HighestReleaseSequence: 100, CatalogSequence: 1, CatalogSHA256: releaseidentity.Hash([]byte("catalog"))}, ReleaseRootDigest: releaseidentity.Hash([]byte("pinned root"))}
}
func fixtureAttestation(state State, op Operation) *AttestationUse {
	now := time.Now().UTC().Add(-time.Second)
	var nonce Incarnation
	nonce[0] = byte(op.Generation)
	nonce[1] = 17
	return &AttestationUse{Authority: "applied-ledger/v1", Nonce: nonce, EvidenceDigest: releaseidentity.Hash([]byte("verified evidence fixture")), OperatorKeyID: releaseidentity.Hash([]byte("operator public key")), InstanceID: state.InstanceID, RestoreEpoch: state.RestoreEpoch, RecoveryIncarnation: state.RecoveryIncarnation, RouteGeneration: op.Generation, RouteDigest: op.RouteDigest, IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
}
func legacyOperation(t *testing.T, cfg Config, manifest releaseidentity.MigrationManifest) Operation {
	t.Helper()
	op := operation(Source{Genesis: LegacyGenesis}, manifest)
	declaration, err := legacyDeclaration(cfg.Engine)
	if err != nil {
		t.Fatal(err)
	}
	op.SourceSchemaDigest = declaration.Catalog.Digest()
	db, err := open(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var instance string
	var epoch int64
	if err := db.QueryRowContext(t.Context(), `SELECT identity FROM instance_identity WHERE id=1`).Scan(&instance); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT credential_epoch FROM auth_instance_state WHERE id=1`).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	op.Acceptance.Attestation = fixtureAttestation(State{InstanceID: instance, RestoreEpoch: epoch, RecoveryIncarnation: op.RecoveryIncarnation}, op)
	op.Acceptance.Attestation.Authority = "legacy-proposal/v1"
	return op
}
