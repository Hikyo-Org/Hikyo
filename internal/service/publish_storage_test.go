package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestSchemaPublishStorageChecksEverySnapshotAndRollsBack(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "storage.db")}
			if engine == store.EnginePostgres {
				cfg = selfConfigPostgres(t)
			}
			s, actor := selfConfigFixtureConfig(t, cfg, map[string]string{"HIKYO_UPDATE_CHANNEL": "nightly"})
			status, err := s.Status(t.Context(), actor)
			if err != nil {
				t.Fatal(err)
			}
			scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
			sealer, err := s.Keyring.ForProject(t.Context(), string(scope.Org), string(scope.Project))
			if err != nil {
				t.Fatal(err)
			}
			var total, snapshotBytes int64
			err = tx.Read(t.Context(), s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
				_, p, err := authorize(ctx, az, actor, authz.OpValuePublish, scope, time.Now())
				if err != nil {
					return err
				}
				values, err := r.Values().PayloadBytesForProject(ctx, p)
				if err != nil {
					return err
				}
				snapshotBytes, err = r.Snapshots().PayloadBytesForProject(ctx, p)
				total = values + snapshotBytes
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			if snapshotBytes == 0 {
				t.Fatal("fixture has no stored snapshot")
			}
			// Exactly one more snapshot reaches the limit. The next check must see
			// those newly inserted bytes and roll back the entire transaction.
			write := func(count int) error {
				return tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
					caller, _, err := authorize(ctx, az, actor, authz.OpValuePublish, scope, time.Now())
					if err != nil {
						return err
					}
					storage := &schemaPublishStorage{limit: total + snapshotBytes}
					groups := &groupIndexPhase{}
					for range count {
						if _, err := republishWithStorage(ctx, r, az, caller, sealer, s.Keyring, scope, time.Now(), "declare", groups, storage); err != nil {
							return err
						}
					}
					return nil
				})
			}
			if err := write(2); !errors.Is(err, domain.ErrLimitExceeded) {
				t.Fatalf("second snapshot must hit exact storage limit: %v", err)
			}
			err = tx.Read(t.Context(), s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
				_, p, err := authorize(ctx, az, actor, authz.OpValuePublish, scope, time.Now())
				if err != nil {
					return err
				}
				latest, err := r.Snapshots().Latest(ctx, p)
				if err != nil {
					return err
				}
				if latest.Revision != 1 {
					t.Fatalf("failed fan-out committed revision %d", latest.Revision)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			// A new attempt starts accounting from the rolled-back database state.
			if err := write(1); err != nil {
				t.Fatalf("fresh attempt retained aborted byte accounting: %v", err)
			}
		})
	}
}
