package upgrade

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/jackc/pgx/v5"
)

func signedAdmissionFixture(t *testing.T, cfg Config) (State, upgradecompat.VerifiedNode, Admission) {
	t.Helper()
	if err := migrateFixture(t, cfg); err != nil {
		t.Fatal(err)
	}
	manifest, err := PinnedLegacyManifest(cfg.Engine)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := PinnedLegacySchemaDigest(cfg.Engine)
	if err != nil {
		t.Fatal(err)
	}
	f := testfixture.New(t)
	declaration := upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.StableV1, Version: "1.0.0", Sequence: 1, Commit: strings.Repeat("a", 40), Engines: []upgradecompat.EngineDeclaration{{Migrations: manifest, SchemaSHA256: schema, Sources: []upgradecompat.SourceEdge{}}}}
	signed := f.AddStable(t, declaration.Version, 1, declaration.Commit, testfixture.JSON(t, declaration))
	snapshot := f.Snapshot(t)
	verified, err := releasetrust.VerifyStable(snapshot, signed.Material)
	if err != nil {
		t.Fatal(err)
	}
	node, err := upgradecompat.Bind(verified, signed.Material.Compatibility)
	if err != nil {
		t.Fatal(err)
	}
	op := legacyOperation(t, cfg, manifest)
	op.Target = node.Identity()
	op.TargetMigrationDigest, err = manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	op.TargetSchemaDigest = schema
	op.Acceptance.Floor = snapshot.Floor()
	op.Acceptance.ReleaseRootDigest = releaseidentity.Hash(f.Pinned.Root)
	var state State
	var admission Admission
	err = WithLock(t.Context(), cfg, func(s *Session) error {
		var err error
		state, err = s.Bootstrap(t.Context(), manifest, op, Production)
		if err != nil {
			return err
		}
		if _, err := s.Admit(t.Context(), state, node); !errors.Is(err, ErrConflict) {
			t.Fatalf("maintenance admitted: %v", err)
		}
		for _, phase := range []Phase{SchemaWriteStarted, SchemaApplied, Healthy} {
			state, err = s.Advance(t.Context(), state, phase)
			if err != nil {
				return err
			}
		}
		if _, err := s.Admit(t.Context(), state, upgradecompat.VerifiedNode{}); !errors.Is(err, ErrConflict) {
			t.Fatalf("unsigned state admitted: %v", err)
		}
		admission, err = s.Admit(t.Context(), state, node)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return state, node, admission
}

func TestRuntimeAdmissionDrainsTransactionsAndNeverRefreshesOldAuthority(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		state, _, admission := signedAdmissionFixture(t, cfg)
		raw, err := open(cfg, false)
		if err != nil {
			t.Fatal(err)
		}
		stamp := "2099-01-01T00:00:00Z"
		if _, err := raw.ExecContext(t.Context(), `INSERT INTO singleton_leases(name,owner,fence_token,acquired_at,expires_at) VALUES('probe','old-process',7,$1,$1)`, stamp); err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		var release func() error
		if cfg.Engine == releaseidentity.SQLite {
			guard, err := admission.LockSQLite(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			db, err := open(cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := guard.Check(t.Context(), tx); err != nil {
				t.Fatal(err)
			}
			release = func() error { return errors.Join(tx.Rollback(), guard.Close()) }
		} else {
			conn, err := pgx.Connect(t.Context(), cfg.DSN)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close(t.Context())
			tx, err := conn.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.Serializable})
			if err != nil {
				t.Fatal(err)
			}
			if err := admission.GuardPostgres(t.Context(), tx); err != nil {
				t.Fatal(err)
			}
			release = func() error { return tx.Rollback(t.Context()) }
		}
		nextOp := nextOperation(state)
		nextOp.Acceptance.ReleaseRootDigest = state.ReleaseRootDigest
		nextOp.Acceptance.Floor = state.Floor
		nextOp.Acceptance.Floor.HighestReleaseSequence = int64(nextOp.Target.Sequence)
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		err = WithLock(ctx, cfg, func(s *Session) error { _, err := s.Prepare(ctx, state, nextOp); return err })
		cancel()
		if err == nil {
			t.Fatal("maintenance completed before admitted transaction settled")
		}
		if err := release(); err != nil {
			t.Fatal(err)
		}
		err = WithLock(t.Context(), cfg, func(s *Session) error {
			next, err := s.Prepare(t.Context(), state, nextOp)
			if err != nil {
				return err
			}
			for _, phase := range []Phase{SchemaWriteStarted, SchemaApplied, Healthy} {
				next, err = s.Advance(t.Context(), next, phase)
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var token int64
		if err := raw.QueryRowContext(t.Context(), `SELECT fence_token FROM singleton_leases WHERE name='probe'`).Scan(&token); err != nil {
			t.Fatal(err)
		}
		if token != 8 {
			t.Fatalf("maintenance did not invalidate old singleton token atomically: %d", token)
		}
		if cfg.Engine == releaseidentity.SQLite {
			guard, err := admission.LockSQLite(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Close()
			db, err := open(cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := guard.Check(t.Context(), tx); !errors.Is(err, ErrConflict) {
				t.Fatalf("old process regained authority: %v", err)
			}
		} else {
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
			if err := admission.GuardPostgres(t.Context(), tx); !errors.Is(err, ErrConflict) {
				t.Fatalf("old process regained authority: %v", err)
			}
		}
	})
}
