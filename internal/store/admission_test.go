package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/jackc/pgx/v5"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	gatefixture "github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
)

func TestRuntimeOpenRequiresActualGateAndSQLiteReadsCannotWrite(t *testing.T) {
	cfg := Config{Engine: EngineSQLite, Path: filepath.Join(t.TempDir(), "runtime.db")}
	if _, err := Open(t.Context(), cfg, upgrade.Admission{}); !errors.Is(err, upgrade.ErrConflict) {
		t.Fatalf("missing admission: %v", err)
	}
	if _, err := os.Stat(cfg.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing admission created datastore")
	}
	admission := gatefixture.Prepare(t, upgrade.Config{Engine: releaseidentity.SQLite, Path: cfg.Path}, MigrationsFS, "migrations/sqlite", bytes.Repeat([]byte{1}, 32))
	db, err := Open(t.Context(), cfg, admission)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginSQLite(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), `DELETE FROM principals`); err == nil {
		t.Fatal("admitted read transaction wrote domain data")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if n, err := db.Coordination().BumpWindow(t.Context(), IPBucket, "fixture", accountWindow); err != nil || n != 1 {
		t.Fatalf("admitted coordination: %v", err)
	}
}

func ownedAdmissionConfig(t *testing.T, engine Engine) Config {
	t.Helper()
	if engine == EngineSQLite {
		return Config{Engine: engine, Path: filepath.Join(t.TempDir(), "runtime.db")}
	}
	raw := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if raw == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires PostgreSQL runtime guard proof")
		}
		t.Skip("HIKYO_TEST_POSTGRES_DSN not set")
	}
	admin, err := pgx.Connect(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("hikyo_admission_%d", time.Now().UnixNano())
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		_ = admin.Close(context.Background())
	})
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	return Config{Engine: engine, DSN: parsed.String()}
}

func TestEveryDirectRuntimeFamilyRefusesMaintenanceAndOldGeneration(t *testing.T) {
	for _, engine := range []Engine{EngineSQLite, EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := ownedAdmissionConfig(t, engine)
			db, err := admittedStoreFixture(t, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			current, err := upgrade.InspectControl(t.Context(), upgradeConfig(cfg))
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			runtimeAdapter := NewAdapterRuntime(db, nil)
			runtimeDynamic := NewDynamicRuntime(db)
			probes := map[string]func() error{
				"readiness":         func() error { return db.CheckAdmission(t.Context()) },
				"node registration": func() error { return db.Coordination().UpsertNode(t.Context(), HANode{NodeID: "stale-worker"}) },
				"counter": func() error {
					_, err := db.Coordination().BumpWindow(t.Context(), IPBucket, "stale", accountWindow)
					return err
				},
				"lease claim": func() error {
					_, _, err := db.Coordination().ClaimLease(t.Context(), "worker", "old", now, now.Add(time.Minute))
					return err
				},
				"lease release": func() error { return db.Coordination().ReleaseLease(t.Context(), "worker", "old", 1) },
				"MCP release":   func() error { return db.Coordination().ReleaseMCP(t.Context(), "old-call") },
				"adapter claim": func() error {
					_, _, err := runtimeAdapter.ClaimDue(t.Context(), "old", now, now.Add(time.Minute))
					return err
				},
				"adapter activation read": func() error { _, err := runtimeAdapter.LoadActivation(t.Context(), adapter.Job{}); return err },
				"dynamic claim": func() error {
					_, _, err := runtimeDynamic.ClaimDueLease(t.Context(), "old", now, now.Add(time.Minute))
					return err
				},
				"dynamic gauges": func() error { _, _, err := runtimeDynamic.Gauges(t.Context()); return err },
				"backup": func() error {
					var out bytes.Buffer
					_, err := Export(t.Context(), db, &out, t.TempDir())
					if out.Len() != 0 {
						t.Fatal("refused backup leaked archive bytes")
					}
					return err
				},
			}
			operation := *current.Pending
			operation.Source = current.Applied
			operation.RouteSource = current.Applied
			operation.SourceSchemaDigest = current.SchemaDigest
			operation.SourceMigrationDigest = current.MigrationDigest
			operation.Target.Version = "1.0.1"
			operation.Target.Sequence++
			operation.Generation = current.Generation + 1
			operation.RouteDigest = releaseidentity.Hash([]byte("test next route"))
			operation.BackupID = "test-new-backup"
			operation.Phase = upgrade.Prepared
			operation.Hop = 0
			operation.RouteLength = 1
			operation.Acceptance.Floor.HighestReleaseSequence = int64(operation.Target.Sequence)
			var nonce upgrade.Incarnation
			if _, err := rand.Read(nonce[:]); err != nil {
				t.Fatal(err)
			}
			operation.Acceptance.Attestation = &upgrade.AttestationUse{Authority: "applied-ledger/v1", Nonce: nonce, EvidenceDigest: releaseidentity.Hash([]byte("storage test evidence")), OperatorKeyID: releaseidentity.Hash([]byte("test operator")), InstanceID: current.InstanceID, RestoreEpoch: current.RestoreEpoch, RecoveryIncarnation: current.RecoveryIncarnation, RouteGeneration: operation.Generation, RouteDigest: operation.RouteDigest, IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Hour)}
			// This transition tests stored fencing claims. Only the initial admission
			// comes from the complete signed development gate; the synthetic next
			// operation never mints runtime authority.
			err = upgrade.WithLock(t.Context(), upgradeConfig(cfg), func(session *upgrade.Session) error {
				var err error
				current, err = session.Prepare(t.Context(), current, operation)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			check := func() {
				for name, probe := range probes {
					t.Run(name, func(t *testing.T) {
						if err := probe(); !errors.Is(err, upgrade.ErrConflict) {
							t.Fatalf("runtime family bypassed upgrade fence: %v", err)
						}
					})
				}
			}
			check()
			err = upgrade.WithLock(t.Context(), upgradeConfig(cfg), func(session *upgrade.Session) error {
				for _, phase := range []upgrade.Phase{upgrade.SchemaWriteStarted, upgrade.SchemaApplied, upgrade.Healthy} {
					var err error
					current, err = session.Advance(t.Context(), current, phase)
					if err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			check()
		})
	}
}
