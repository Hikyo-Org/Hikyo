package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func unmanagedSelfConfigFixture(t *testing.T, cfg store.Config) (*SelfConfig, Actor) {
	t.Helper()
	db, err := openServiceFixture(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	root := serviceFixtureRoot(t, db)
	defer crypto.Zero(root)
	kr, err := crypto.LoadKeyring(t.Context(), &keyring.Store{DB: db}, root)
	if err != nil {
		t.Fatal(err)
	}
	auth := &Auth{DB: db, Keyring: kr}
	boot, err := auth.BootstrapAdmin(t.Context(), "operator", "Operator", "file")
	if err != nil {
		t.Fatal(err)
	}
	s := &SelfConfig{DB: db, Keyring: kr, Auth: auth, NodeID: "local"}
	auth.SelfConfig = s
	return s, LocalPrincipal(boot.PrincipalID)
}

func TestSelfConfigExistingInstanceAdoptionIsExplicitAndDurable(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			cfg := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "adopt.db")}
			if engine == "postgres" {
				cfg = selfConfigPostgres(t)
			}
			s, local := unmanagedSelfConfigFixture(t, cfg)
			orgs := &Orgs{DB: s.DB}
			existingOrg, err := orgs.Create(t.Context(), local, "Hikyo", true, json.RawMessage(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			seedReads := 0
			s.Seed = func() (map[string]string, error) {
				seedReads++
				return map[string]string{"HIKYO_UPDATE_CHANNEL": "off"}, nil
			}
			actor, session := selfConfigSession(t, s, local)
			preview, err := s.PreviewAdoption(t.Context(), actor)
			if err != nil {
				t.Fatal(err)
			}
			request := SelfConfigAdoptRequest{PreviewToken: preview.PreviewToken, IdempotencyKey: "one-time-adoption"}
			if _, err := s.Adopt(t.Context(), actor, request); !errors.Is(err, ErrNoReauthWindow) {
				t.Fatalf("adoption without exact reauth: %v", err)
			}
			status, err := s.Status(t.Context(), actor)
			if err != nil || status.Managed {
				t.Fatalf("refused adoption changed binding: %+v, %v", status, err)
			}
			selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "adopt", OwnerInstanceID: preview.OwnerInstanceID, SchemaVersion: runtimeconfig.SchemaVersion, PreviewToken: preview.PreviewToken})
			adopted, err := s.Adopt(t.Context(), actor, request)
			if err != nil {
				t.Fatal(err)
			}
			if !adopted.Managed || adopted.Binding == nil {
				t.Fatal("adoption did not commit binding")
			}
			if adopted.Binding.OrgID == existingOrg.ID {
				t.Fatal("adoption claimed an unrelated organization by name")
			}
			// Grant changes revoke the old browser session; retry with a fresh login.
			actor, _ = selfConfigSession(t, s, local)
			retry, err := s.Adopt(t.Context(), actor, request)
			if err != nil {
				t.Fatal(err)
			}
			if retry.Binding.ProjectID != adopted.Binding.ProjectID || retry.Generation != 1 || seedReads != 1 {
				t.Fatalf("adoption retried provisioning or file imports: %+v, reads=%d", retry, seedReads)
			}
			s.Seed = func() (map[string]string, error) {
				t.Fatal("managed boot evaluated stale environment/file settings")
				return nil, nil
			}
			fresh := &SelfConfig{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth, NodeID: "local", Seed: s.Seed}
			if err := fresh.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			bundle, err := fresh.Capture(t.Context())
			if err != nil || bundle.UpdateChannel() != "off" {
				t.Fatalf("managed restart did not use committed revision: %v", err)
			}
		})
	}
}

func TestSelfConfigHostRecoveryNeedsQuiescenceAndRejectsNetwork(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			cfg := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "recover.db")}
			if engine == "postgres" {
				cfg = selfConfigPostgres(t)
			}
			s, _ := selfConfigFixtureConfig(t, cfg, map[string]string{"HIKYO_UPDATE_CHANNEL": "off"})
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := s.LocalStatus(operation.WithNetwork(t.Context())); !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatalf("network status bypass: %v", err)
			}
			if _, err := s.Recover(operation.WithNetwork(t.Context()), 1); !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatalf("network recovery bypass: %v", err)
			}
			if _, err := s.Recover(t.Context(), 1); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("live consumer recovery: %v", err)
			}
			if engine == "postgres" {
				// Host recovery must use database time even when its own HA
				// flag is false and its wall clock is ahead of live replicas.
				later := time.Now().Add(time.Hour)
				s.Now = func() time.Time { return later }
				if _, err := s.Recover(t.Context(), 1); !errors.Is(err, domain.ErrConflict) {
					t.Fatalf("host clock skew bypassed live-consumer fence: %v", err)
				}
			}
			err := tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				p, err := az.SelfConfigRuntimeAuthority(ctx, "")
				if err != nil {
					return err
				}
				nodes, err := r.SelfConfig().Nodes(ctx, p)
				if err != nil {
					return err
				}
				for _, node := range nodes {
					node.UpdatedAt = time.Now().Add(-31 * time.Second)
					if err := r.SelfConfig().PutNode(ctx, p, node); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			status, err := s.Recover(t.Context(), 1)
			if err != nil {
				t.Fatal(err)
			}
			if !status.Managed || status.Generation != 2 || status.DesiredRevision != 1 {
				t.Fatalf("wrong host recovery target: %+v", status)
			}
			if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatalf("old bundle survived recovery fence: %v", err)
			}
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Capture(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSelfConfigIndependentOwnersKeepSeparateProjectsAndRuntime(t *testing.T) {
	first, local := selfConfigFixture(t)
	second, secondActor := selfConfigFixtureConfig(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "remote.db")}, map[string]string{"HIKYO_UPDATE_CHANNEL": "off"})
	if err := first.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := second.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	a, err := first.Status(t.Context(), local)
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.Status(t.Context(), secondActor)
	if err != nil {
		t.Fatal(err)
	}
	if a.OwnerInstanceID == b.OwnerInstanceID || a.Binding.OrgID == b.Binding.OrgID || a.Binding.ProjectID == b.Binding.ProjectID {
		t.Fatal("independent owners shared a binding")
	}
	firstBundle, err := first.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	secondBundle, err := second.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if firstBundle.UpdateChannel() != "nightly" || secondBundle.UpdateChannel() != "off" {
		t.Fatal("independent runtime settings were shared")
	}
}

func TestSelfConfigNormalKeyRotationPreservesRuntimeSnapshots(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			cfg := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "rotation.db")}
			if engine == "postgres" {
				cfg = selfConfigPostgres(t)
			}
			s, actor := selfConfigFixtureConfig(t, cfg, map[string]string{"HIKYO_UPDATE_CHANNEL": "off"})
			status, err := s.Status(t.Context(), actor)
			if err != nil {
				t.Fatal(err)
			}
			rotation := &Rotation{DB: s.DB, Keyring: s.Keyring}
			rotated, err := rotation.RotateDEK(t.Context(), actor, DEKScope{OrgID: status.Binding.OrgID, ProjectID: status.Binding.ProjectID})
			if err != nil {
				t.Fatal(err)
			}
			if rotated.Version != 2 {
				t.Fatalf("wrong successor: %+v", rotated)
			}
			reencrypt := &Reencrypt{DB: s.DB, Keyring: s.Keyring}
			result, err := reencrypt.ReencryptProject(t.Context(), actor, status.Binding.OrgID, status.Binding.ProjectID)
			if err != nil {
				t.Fatal(err)
			}
			if result.RowsMoved < 2 {
				t.Fatalf("value and runtime snapshot were not reencrypted: %+v", result)
			}
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			bundle, err := s.Capture(t.Context())
			if err != nil || bundle.UpdateChannel() != "off" {
				t.Fatalf("rotated runtime unreadable: %v", err)
			}
		})
	}
}

func TestSelfConfigHAAdoptionRejectsDifferentSeedsAtomically(t *testing.T) {
	s, local := unmanagedSelfConfigFixture(t, selfConfigPostgres(t))
	s.NodeID = "replica-a"
	s.Seed = func() (map[string]string, error) { return map[string]string{"HIKYO_UPDATE_CHANNEL": "stable"}, nil }
	second := &SelfConfig{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth, NodeID: "replica-b", Seed: func() (map[string]string, error) { return map[string]string{"HIKYO_UPDATE_CHANNEL": "off"}, nil }}
	now, err := s.DB.Coordination().Now(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{s.NodeID, second.NodeID} {
		if err := s.DB.Coordination().UpsertNode(t.Context(), store.HANode{NodeID: id, BinaryVersion: "self-config-test", SchemaVersion: 1, RootKeyFingerprint: "shared-test-root", StartedAt: now, HeartbeatAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := second.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	actor, session := selfConfigSession(t, s, local)
	preview, err := s.PreviewAdoption(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	request := SelfConfigAdoptRequest{PreviewToken: preview.PreviewToken, IdempotencyKey: "HA-adoption"}
	selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "adopt", OwnerInstanceID: preview.OwnerInstanceID, SchemaVersion: runtimeconfig.SchemaVersion, PreviewToken: preview.PreviewToken})
	// A fast host clock must not remove a live disagreeing replica from the
	// datastore-clock membership check. The existing reauth window is still fresh.
	s.Now = func() time.Time { return time.Now().Add(40 * time.Second) }
	if _, err := s.Adopt(t.Context(), actor, request); !errors.Is(err, store.ErrSelfConfigSeedDisagreement) {
		t.Fatalf("different admitted seeds were accepted: %v", err)
	}
	status, err := s.Status(t.Context(), actor)
	if err != nil || status.Managed {
		t.Fatalf("failed adoption changed binding or invalidated session: %+v %v", status, err)
	}
	orgs, err := (&Orgs{DB: s.DB}).List(t.Context(), local)
	if err != nil {
		t.Fatal(err)
	}
	if len(orgs) != 0 {
		t.Fatal("failed adoption left a partial organization")
	}
	// Simulate restarting the second replica with the reviewed seed. Retry
	// retains the original unspent ceremony and commits exactly one hierarchy.
	second = &SelfConfig{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth, NodeID: "replica-b", Seed: s.Seed}
	if err := second.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Adopt(t.Context(), actor, request); err != nil {
		t.Fatal(err)
	}
	orgs, err = (&Orgs{DB: s.DB}).List(t.Context(), local)
	if err != nil || len(orgs) != 1 {
		t.Fatalf("agreed adoption did not create exactly one organization: count=%d err=%v", len(orgs), err)
	}
}
