package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
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
	"github.com/jackc/pgx/v5"
)

func selfConfigFixture(t *testing.T) (*SelfConfig, Actor) {
	return selfConfigFixtureConfig(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "self-config.db")}, map[string]string{"HIKYO_UPDATE_CHANNEL": "nightly"})
}

func selfConfigFixtureConfig(t *testing.T, cfg store.Config, seed map[string]string) (*SelfConfig, Actor) {
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
	s := &SelfConfig{DB: db, Keyring: kr, NodeID: "local", Seed: func() (map[string]string, error) { return seed, nil }}
	s.Auth = &Auth{DB: db, Keyring: kr, SelfConfig: s}
	bootstrap, err := s.Auth.BootstrapAdmin(t.Context(), "operator", "Operator", "stdout")
	if err != nil {
		t.Fatal(err)
	}
	return s, LocalPrincipal(bootstrap.PrincipalID)
}

func selfConfigPostgres(t *testing.T) store.Config {
	t.Helper()
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires HIKYO_TEST_POSTGRES_DSN")
		}
		t.Skip("HIKYO_TEST_POSTGRES_DSN not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	database := fmt.Sprintf("hikyo_self_config_runtime_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{database}.Sanitize()
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+quoted); err != nil {
		_ = admin.Close(context.Background())
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+quoted+" WITH (FORCE)")
		_ = admin.Close(context.Background())
	})
	parsed.Path = "/" + database
	return store.Config{Engine: store.EnginePostgres, DSN: parsed.String()}
}

func selfConfigSession(t *testing.T, s *SelfConfig, local Actor) (Actor, string) {
	t.Helper()
	artifact, verifier, err := crypto.NewArtifact(crypto.ArtifactBrowserSession)
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := newID("ses")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	err = tx.Write(t.Context(), s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		generation, err := az.PrincipalGeneration(ctx, local.principal)
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		return az.MintSession(ctx, authz.NewSession{ID: sessionID, PrincipalID: local.principal, Verifier: verifier, Artifact: "browser", SessionGeneration: generation, CredentialEpoch: epoch, AuthMethod: "local-passkey", Factors: `["webauthn","mfa"]`, AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(24 * time.Hour), SourceIP: "127.0.0.1", UserAgent: "test"})
	})
	if err != nil {
		t.Fatal(err)
	}
	return Bearer(artifact), sessionID
}

func selfConfigReauthenticate(t *testing.T, s *SelfConfig, sessionID string, target SelfConfigReauthTarget) {
	t.Helper()
	intent, err := NewSelfConfigReauthIntent(target)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := intent.bindingFor("")
	if err != nil {
		t.Fatal(err)
	}
	id, err := newID("raw")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	err = tx.Write(t.Context(), s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return az.OpenReauthWindow(ctx, authz.NewReauthWindow{ID: id, SessionID: sessionID, EnvironmentID: intent.environmentID, CeremonyID: "test-ceremony", FactorClass: "totp", SingleDecision: true, AuthenticatedAt: now, WindowExpiresAt: now.Add(time.Minute), HardExpiresAt: now.Add(time.Minute), CredentialEpoch: 1, CreatedAt: now, BoundPurpose: string(binding.purpose), BoundOperation: string(binding.operation), BoundKeySet: binding.keySet, BoundEnvironmentSet: binding.environmentSet})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func runSelfConfig(t *testing.T, s *SelfConfig) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
}

func TestSelfConfigPublishNeedsExplicitReauthenticatedApply(t *testing.T) {
	s, local := selfConfigFixture(t)
	testSelfConfigLifecycle(t, s, local)
}

func TestSelfConfigPostgresLifecycle(t *testing.T) {
	s, local := selfConfigFixtureConfig(t, selfConfigPostgres(t), map[string]string{"HIKYO_UPDATE_CHANNEL": "nightly"})
	testSelfConfigLifecycle(t, s, local)
}

func testSelfConfigLifecycle(t *testing.T, s *SelfConfig, local Actor) {
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	actor, sessionID := selfConfigSession(t, s, local)
	status, err := s.Status(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
	values := &Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	staged, err := values.Set(t.Context(), local, scope, "HIKYO_UPDATE_CHANNEL", "off", nil)
	if err != nil {
		t.Fatal(err)
	}
	revisions := &Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	if _, err := revisions.PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
		t.Fatal(err)
	}
	bundle, err := s.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.UpdateChannel() != "nightly" {
		t.Fatal("publishing silently activated configuration")
	}
	runSelfConfig(t, s)
	req := SelfConfigApplyRequest{Revision: 2, ExpectedGeneration: 1, SchemaVersion: runtimeconfig.SchemaVersion, IdempotencyKey: "test-apply"}
	if _, err := s.Apply(t.Context(), actor, req); !errors.Is(err, ErrNoReauthWindow) {
		t.Fatalf("got %v, want exact reauthentication requirement", err)
	}
	selfConfigReauthenticate(t, s, sessionID, SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: status.OwnerInstanceID, Revision: 2, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 1})
	if _, err := s.Apply(t.Context(), actor, req); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	bundle, err = s.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.UpdateChannel() != "off" {
		t.Fatal("explicit apply did not activate the published revision")
	}
	if _, err := s.Capture(operation.WithNetwork(t.Context())); err != nil {
		t.Fatalf("network consumer could not capture active bundle: %v", err)
	}
	if err := s.ReconcileRuntime(operation.WithNetwork(t.Context())); err == nil {
		t.Fatal("network handler minted runtime payload authority")
	}
	status, err = s.Apply(t.Context(), actor, req)
	if err != nil {
		t.Fatal(err)
	}
	if status.Generation != 2 || status.Job == nil || status.Job.State != "completed" {
		t.Fatalf("idempotent retry changed activation: %+v", status)
	}
}

func TestSelfConfigBootstrapLoadsItsPublishedConfiguration(t *testing.T) {
	s, actor := selfConfigFixture(t)
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	bundle, err := s.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.UpdateChannel() != "nightly" {
		t.Fatal("runtime did not load bootstrapped project revision")
	}
	status, err := s.Status(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Managed || status.State != "active" || status.Generation != 1 || len(status.Nodes) != 1 {
		t.Fatalf("unexpected status: %+v", status)
	}
	s.Seed = func() (map[string]string, error) { return nil, errors.New("stale process settings must not be read") }
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSelfConfigSuspensionKeepsRecoveryInterfaceAvailable(t *testing.T) {
	s, actor := selfConfigFixture(t)
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	err := tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		return r.SelfConfig().FenceRestored(ctx, p, "restored-test-incarnation", time.Now())
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatalf("suspension prevented admin/recovery boot: %v", err)
	}
	if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
		t.Fatalf("got %v, want suspended consumer refusal", err)
	}
	status, err := s.Status(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "recovery_required" {
		t.Fatalf("got state %q", status.State)
	}
}

func publishSelfConfigChannel(t *testing.T, s *SelfConfig, local Actor, channel string) SelfConfigStatus {
	t.Helper()
	status, err := s.Status(t.Context(), local)
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
	values := &Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	draft, err := values.Set(t.Context(), local, scope, "HIKYO_UPDATE_CHANNEL", channel, nil)
	if err != nil {
		t.Fatal(err)
	}
	revisions := &Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	if _, err := revisions.PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{draft.VersionID}}); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestSelfConfigHARefusesStaleTrafficAndConvergesAfterActorRevocation(t *testing.T) {
	s, local := selfConfigFixtureConfig(t, selfConfigPostgres(t), map[string]string{"HIKYO_UPDATE_CHANNEL": "nightly"})
	second := &SelfConfig{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth, NodeID: "replica-b"}
	now := time.Now()
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
	status := publishSelfConfigChannel(t, s, local, "off")
	actor, sessionID := selfConfigSession(t, s, local)
	selfConfigReauthenticate(t, s, sessionID, SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: status.OwnerInstanceID, Revision: 2, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 1})
	runSelfConfig(t, s)
	applyCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.Apply(applyCtx, actor, SelfConfigApplyRequest{Revision: 2, ExpectedGeneration: 1, SchemaVersion: runtimeconfig.SchemaVersion, IdempotencyKey: "HA-apply"})
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, err := s.Status(t.Context(), local)
		if err != nil {
			t.Fatal(err)
		}
		if current.Job != nil && current.Job.State == "preparing" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("apply did not publish its preparation job")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := second.ReconcileRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("all prepared replicas did not permit commit")
	}
	if _, err := second.Capture(operation.WithNetwork(t.Context())); !errors.Is(err, ErrSelfConfigUnavailable) {
		t.Fatalf("stale replica accepted affected traffic: %v", err)
	}
	grants := &Grants{DB: s.DB}
	if err := grants.Revoke(t.Context(), local, GrantSpec{Target: local.principal, Capability: domain.CapInstanceConfig}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := second.ReconcileRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, node := range []*SelfConfig{s, second} {
		bundle, err := node.Capture(operation.WithNetwork(t.Context()))
		if err != nil {
			t.Fatal(err)
		}
		if bundle.UpdateChannel() != "off" {
			t.Fatal("committed target stopped reconciling after actor revocation")
		}
	}
}

func TestSelfConfigInvalidMailPublishRetainsActiveConfiguration(t *testing.T) {
	s, local := selfConfigFixture(t)
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err := s.Status(t.Context(), local)
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
	values := &Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	draft, err := values.Set(t.Context(), local, scope, "HIKYO_MAIL_FROM", "hikyo@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	revisions := &Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	if _, err := revisions.PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{draft.VersionID}}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid mail configuration published: %v", err)
	}
	status, err = s.Status(t.Context(), local)
	if err != nil {
		t.Fatal(err)
	}
	if status.Generation != 1 || status.Job != nil || status.State != "active" {
		t.Fatalf("invalid publication changed active status: %+v", status)
	}
	bundle, err := s.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.MailConfigured() || bundle.UpdateChannel() != "nightly" {
		t.Fatal("invalid candidate replaced the active bundle")
	}
}

func TestSelfConfigRestoreRequiresConfirmationBoundIntoReauthentication(t *testing.T) {
	s, local := selfConfigFixture(t)
	err := tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		return r.SelfConfig().FenceRestored(ctx, p, "previous-restored-incarnation", time.Now())
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	actor, sessionID := selfConfigSession(t, s, local)
	status, err := s.Status(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	req := SelfConfigApplyRequest{Revision: 1, ExpectedGeneration: 1, SchemaVersion: runtimeconfig.SchemaVersion, IdempotencyKey: "recovery-apply"}
	if _, err := s.Apply(t.Context(), actor, req); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unconfirmed recovery apply got %v", err)
	}
	req.ConfirmRestoredCredentials = true
	selfConfigReauthenticate(t, s, sessionID, SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: status.OwnerInstanceID, Revision: 1, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 1})
	runSelfConfig(t, s)
	if _, err := s.Apply(t.Context(), actor, req); !errors.Is(err, ErrReauthUnitMismatch) {
		t.Fatalf("unbound credential confirmation got %v", err)
	}
	if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
		t.Fatalf("unconfirmed credentials became active: %v", err)
	}
	selfConfigReauthenticate(t, s, sessionID, SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: status.OwnerInstanceID, Revision: 1, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 1, ConfirmRestoredCredentials: true})
	if _, err := s.Apply(t.Context(), actor, req); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Capture(t.Context()); err != nil {
		t.Fatal(err)
	}
	if status, err := s.Apply(t.Context(), actor, req); err != nil || status.Generation != 2 {
		t.Fatalf("recovery retry was not idempotent: generation=%d err=%v", status.Generation, err)
	}
}

func TestSelfConfigApplyRetryReturnsOriginalJobAfterCollection(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "retry.db")}
			if engine == store.EnginePostgres {
				cfg = selfConfigPostgres(t)
			}
			s, local := selfConfigFixtureConfig(t, cfg, map[string]string{"HIKYO_UPDATE_CHANNEL": "nightly"})
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			actor, sessionID := selfConfigSession(t, s, local)
			status, err := s.Status(t.Context(), actor)
			if err != nil {
				t.Fatal(err)
			}
			scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
			runSelfConfig(t, s)
			firstRequest := SelfConfigApplyRequest{Revision: 1, ExpectedGeneration: 1, SchemaVersion: runtimeconfig.SchemaVersion, IdempotencyKey: "retry-original"}
			var firstJob string
			for revision := int64(1); revision <= 3; revision++ {
				if revision > 1 {
					values := &Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
					channel := "off"
					if revision == 3 {
						channel = "stable"
					}
					staged, err := values.Set(t.Context(), local, scope, "HIKYO_UPDATE_CHANNEL", channel, nil)
					if err != nil {
						t.Fatal(err)
					}
					revisions := &Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
					if _, err := revisions.PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
						t.Fatal(err)
					}
				}
				req := firstRequest
				req.Revision, req.ExpectedGeneration = revision, revision
				if revision > 1 {
					req.IdempotencyKey = fmt.Sprintf("retry-newer-%d", revision)
				}
				selfConfigReauthenticate(t, s, sessionID, SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: status.OwnerInstanceID, Revision: revision, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: revision})
				applied, err := s.Apply(t.Context(), actor, req)
				if err != nil {
					t.Fatal(err)
				}
				if applied.Job == nil {
					t.Fatal("missing apply job")
				}
				if revision == 1 {
					firstJob = applied.Job.ID
				}
				if err := s.ReconcileRuntime(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			// The first target is neither desired nor previous and may now be collected.
			err = tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				_, proof, err := authorize(ctx, az, actor, authz.OpSelfConfigApply, scope, s.now())
				if err != nil {
					return err
				}
				job, err := r.SelfConfig().Job(ctx, proof, firstJob)
				if err != nil {
					return err
				}
				gcProof, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
				if err != nil {
					return err
				}
				collected, err := r.Retention().MarkCollected(ctx, gcProof, job.SnapshotID, "test", time.Now())
				if err != nil {
					return err
				}
				if !collected {
					return errors.New("released first target was unexpectedly retained")
				}
				_, err = r.Retention().DeleteCollectedEntries(ctx, gcProof, job.SnapshotID)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			retried, err := s.Apply(t.Context(), actor, firstRequest)
			if err != nil {
				t.Fatalf("retry after collection: %v", err)
			}
			if retried.Generation != 4 || retried.Job == nil || retried.Job.ID != firstJob || retried.Job.Revision != 1 || retried.Job.State != "completed" {
				t.Fatalf("retry lost original job or current binding: %+v", retried)
			}
			changedConfirmation := firstRequest
			changedConfirmation.ConfirmRestoredCredentials = true
			if _, err := s.Apply(t.Context(), actor, changedConfirmation); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("changed idempotent confirmation: %v", err)
			}
			changed := firstRequest
			changed.Revision = 2
			if _, err := s.Apply(t.Context(), actor, changed); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("changed idempotent request: %v", err)
			}
		})
	}
}

func TestSelfConfigExpiredApplyRetryDoesNotRestartPreparation(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "expired.db")}
			if engine == store.EnginePostgres {
				cfg = selfConfigPostgres(t)
			}
			s, local := selfConfigFixtureConfig(t, cfg, map[string]string{"HIKYO_UPDATE_CHANNEL": "nightly"})
			actor, _ := selfConfigSession(t, s, local)
			status, err := s.Status(t.Context(), actor)
			if err != nil {
				t.Fatal(err)
			}
			scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
			at, err := s.runtimeTimestamp(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			err = tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				caller, proof, err := authorize(ctx, az, actor, authz.OpSelfConfigApply, scope, s.now())
				if err != nil {
					return err
				}
				snapshot, err := r.Snapshots().AtRevision(ctx, proof, 1)
				if err != nil {
					return err
				}
				_, err = r.SelfConfig().BeginJob(ctx, proof, store.SelfConfigJob{ID: "scj_expired", IdempotencyKey: "expired-request", PrincipalID: string(caller.Principal), SnapshotID: snapshot.ID, Revision: 1, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 1, CreatedAt: at.Add(-store.SelfConfigPreparationTTL - time.Second), LocalNodeID: s.NodeID})
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			// No worker prepares the candidate. A retry must return its expired job,
			// rather than start another 30-second preparation wait.
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			retried, err := s.Apply(ctx, actor, SelfConfigApplyRequest{Revision: 1, ExpectedGeneration: 1, SchemaVersion: runtimeconfig.SchemaVersion, IdempotencyKey: "expired-request"})
			if err != nil {
				t.Fatalf("expired retry started another preparation wait: %v", err)
			}
			if retried.Generation != 1 || retried.Job == nil || retried.Job.ID != "scj_expired" {
				t.Fatalf("expired retry changed target: %+v", retried)
			}
		})
	}
}
