package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storekeyring "github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

type rootDeploymentProbe struct {
	*deploymentProbe
	wrapper      crypto.WrappedKey
	epoch        int64
	existing     bool
	beforeSubmit func(context.Context) error
}

func (p *rootDeploymentProbe) PrepareCommand(ctx context.Context, intent configrollout.Intent, bundle *runtimeconfig.Bundle, sequence uint64) (configrollout.SignedCommand, error) {
	command, err := p.deploymentProbe.PrepareCommand(ctx, intent, bundle, sequence)
	command.Command.Bootstrap = &configrollout.BootstrapChanges{Root: &configrollout.SourceProof{Alias: "replacement", RootEpoch: p.epoch}}
	return command, err
}
func (p *rootDeploymentProbe) DecisionCommand(ctx context.Context, prepared configrollout.SignedCommand, action configrollout.Action, sequence uint64, digest string, ack *configrollout.ApplicationAcknowledgement) (configrollout.SignedCommand, error) {
	if action == configrollout.ActionSubmit && p.beforeSubmit != nil {
		if err := p.beforeSubmit(ctx); err != nil {
			return configrollout.SignedCommand{}, err
		}
	}
	command, err := p.deploymentProbe.DecisionCommand(ctx, prepared, action, sequence, digest, ack)
	command.Command.Bootstrap = prepared.Command.Bootstrap
	return command, err
}
func (p *rootDeploymentProbe) RootPreparation(context.Context, configrollout.SignedCommand) (*crypto.WrappedKey, error) {
	if p.existing {
		return nil, nil
	}
	wrapper := p.wrapper
	wrapper.Blob = bytes.Clone(wrapper.Blob)
	return &wrapper, nil
}

func TestSelfConfigDeploymentRootPersistenceRequiresExactMFA(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		for _, epoch := range []int64{2, 0, 1, int64(math.MaxUint32) + 3} {
			name := string(engine) + "/valid"
			if epoch != 2 {
				name = string(engine) + "/invalid-epoch-" + fmt.Sprint(epoch)
			}
			t.Run(name, func(t *testing.T) {
				s, local, _ := installerFixture(t, engine)
				if err := s.LoadRuntime(t.Context()); err != nil {
					t.Fatal(err)
				}
				actor, session := selfConfigSession(t, s, local)
				status, err := s.Status(t.Context(), local)
				if err != nil {
					t.Fatal(err)
				}
				owner, inc, err := s.DB.RecoveryIdentity()
				if err != nil {
					t.Fatal(err)
				}
				root := bytes.Repeat([]byte{0x42}, crypto.KeySize)
				wrapper, err := s.Keyring.PrepareRootKeyRotation(t.Context(), root)
				if err != nil {
					t.Fatal(err)
				}
				probe := &rootDeploymentProbe{deploymentProbe: &deploymentProbe{identity: DeploymentIdentity{EnrollmentID: "enrolled", OwnerInstanceID: owner, Incarnation: inc, DeploymentUID: "deployment-uid", TemplateStamp: "original-template"}, sent: make(map[uint64]bool)}, wrapper: wrapper, epoch: epoch}
				s.Deployment = probe
				ks := &storekeyring.Store{DB: s.DB}
				assertWrappers := func(want int) {
					t.Helper()
					wrappers, err := ks.ActiveMasterWrappers(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					if len(wrappers) != want {
						t.Fatalf("active wrappers=%d, want %d", len(wrappers), want)
					}
					if want == 2 {
						found := false
						for _, stored := range wrappers {
							if stored.RootKeyEpoch == wrapper.RootKeyEpoch {
								found = true
								if !bytes.Equal(stored.Blob, wrapper.Blob) || stored.CreatedAt.IsZero() {
									t.Fatal("committed wrapper differs from prepared ciphertext or has no timestamp")
								}
							}
						}
						if !found {
							t.Fatal("prepared epoch not persisted")
						}
					}
				}
				scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
				draft, err := (&Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).Set(t.Context(), local, scope, config.ManagedBootstrapSourcesKey, `{"version":1,"root_source":"replacement"}`, nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := (&Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{draft.VersionID}}); err != nil {
					t.Fatal(err)
				}
				status, err = s.Status(t.Context(), local)
				if err != nil {
					t.Fatal(err)
				}
				req := installerRequest(status, "root-bootstrap-plan")
				req.PrepareOnly = true
				done := beginInstallerApply(t, s, actor, req)
				if err := s.ReconcileRuntime(t.Context()); err != nil {
					t.Fatal(err)
				}
				result := awaitInstallerApply(t, done)
				if result.err != nil {
					t.Fatal(result.err)
				}
				assertWrappers(1)
				req.PrepareOnly = false
				req.PlanDigest = result.status.Job.PlanDigest
				target := SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: owner, Revision: req.Revision, SchemaVersion: req.SchemaVersion, ExpectedGeneration: req.ExpectedGeneration, PlanDigest: req.PlanDigest}
				if epoch != 2 {
					selfConfigReauthenticate(t, s, session, target)
					if _, err := s.Apply(t.Context(), actor, req); !errors.Is(err, domain.ErrConflict) {
						t.Fatalf("out-of-range root epoch accepted: %v", err)
					}
					assertWrappers(1)
					return
				}
				wrong := target
				wrong.PlanDigest = strings.Repeat("b", 64)
				selfConfigReauthenticate(t, s, session, wrong)
				if _, err := s.Apply(t.Context(), actor, req); err == nil {
					t.Fatal("unbound MFA committed root wrapper")
				}
				assertWrappers(1)
				status, err = s.Status(t.Context(), local)
				if err != nil {
					t.Fatal(err)
				}
				if status.Generation != req.ExpectedGeneration {
					t.Fatal("failed MFA committed target generation")
				}
				selfConfigReauthenticate(t, s, session, target)
				if _, err := s.Apply(t.Context(), actor, req); err != nil {
					t.Fatal(err)
				}
				assertWrappers(2)
				status, err = s.Status(t.Context(), local)
				if err != nil {
					t.Fatal(err)
				}
				if status.Generation != req.ExpectedGeneration+1 {
					t.Fatal("wrapper persisted without target commit")
				}
				probe.mu.Lock()
				submissions := probe.submissions
				probe.mu.Unlock()
				if submissions != 0 {
					t.Fatal("root submission escaped before committed worker delivery")
				}
				if err := crypto.VerifyExistingHierarchy(t.Context(), ks, bytes.Clone(root)); err != nil {
					t.Fatalf("committed wrapper cannot load hierarchy under new root: %v", err)
				}
				if _, err := finalizeSelfConfigTestRoot(t.Context(), s, local); !errors.Is(err, store.ErrRootFinalizationPendingDeployment) {
					t.Fatalf("pending rollout retired rollback root: %v", err)
				}
				assertWrappers(2)
				rotation := &Rotation{DB: s.DB, Keyring: s.Keyring, RootKey: selfConfigTestRootSource(root)}
				if _, err := rotation.rootRotateFinalize(t.Context(), local); !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), "complete the configuration rollout or its repair") {
					t.Fatalf("pending finalization did not return actionable conflict: %v", err)
				}
				probe.mu.Lock()
				probe.installed = true
				probe.identity.TemplateStamp = "replacement-template"
				probe.mu.Unlock()
				for range 5 {
					if err := s.ReconcileRuntime(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
				completed, err := s.Status(t.Context(), local)
				if err != nil || completed.Job.State != "completed" {
					t.Fatalf("rollout did not reach exact applied receipt: %v", err)
				}
				if epoch, err := finalizeSelfConfigTestRoot(t.Context(), s, local); err != nil || epoch != 2 {
					t.Fatalf("applied rollout prevented explicit finalization: epoch=%d err=%v", epoch, err)
				}
				assertWrappers(1)

			})
		}
	}
}

func finalizeSelfConfigTestRoot(ctx context.Context, s *SelfConfig, actor Actor) (uint32, error) {
	var epoch uint32
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpRotateRootKey, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		epoch, err = r.Keys().RootKeyRotateFinalize(ctx, proof)
		return err
	})
	return epoch, err
}

func TestSelfConfigDeploymentConcurrentFinalizeInvalidatesPreparedRoot(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, _ := installerFixture(t, engine)
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			actor, session := selfConfigSession(t, s, local)
			status, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			owner, inc, err := s.DB.RecoveryIdentity()
			if err != nil {
				t.Fatal(err)
			}
			wrapper, err := s.Keyring.PrepareRootKeyRotation(t.Context(), bytes.Repeat([]byte{0x43}, crypto.KeySize))
			if err != nil {
				t.Fatal(err)
			}
			wrapper.CreatedAt = store.CanonTime(s.now())
			if err := tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				_, proof, err := authorize(ctx, az, local, authz.OpRotateRootKey, domain.Scope{}, s.now())
				if err != nil {
					return err
				}
				return r.Keys().RootKeyRotatePrepare(ctx, proof, wrapper)
			}); err != nil {
				t.Fatal(err)
			}
			probe := &rootDeploymentProbe{deploymentProbe: &deploymentProbe{identity: DeploymentIdentity{EnrollmentID: "enrolled", OwnerInstanceID: owner, Incarnation: inc, DeploymentUID: "deployment-uid", TemplateStamp: "original-template"}, sent: make(map[uint64]bool)}, wrapper: wrapper, epoch: 2, existing: true}
			s.Deployment = probe
			scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
			draft, err := (&Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).Set(t.Context(), local, scope, config.ManagedBootstrapSourcesKey, `{"version":1,"root_source":"replacement"}`, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (&Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{draft.VersionID}}); err != nil {
				t.Fatal(err)
			}
			status, err = s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			req := installerRequest(status, "concurrent-root-finalize")
			req.PrepareOnly = true
			done := beginInstallerApply(t, s, actor, req)
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			result := awaitInstallerApply(t, done)
			if result.err != nil {
				t.Fatal(result.err)
			}
			req.PrepareOnly = false
			req.PlanDigest = result.status.Job.PlanDigest
			selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: owner, Revision: req.Revision, SchemaVersion: req.SchemaVersion, ExpectedGeneration: req.ExpectedGeneration, PlanDigest: req.PlanDigest})
			// Another host process finalizes after source reproof, before the final
			// Apply transaction. Its hierarchy lock is independent of process memory.
			probe.beforeSubmit = func(ctx context.Context) error {
				epoch, err := finalizeSelfConfigTestRoot(ctx, s, local)
				if err == nil && epoch != 2 {
					return errors.New("unexpected finalized epoch")
				}
				return err
			}
			if _, err := s.Apply(t.Context(), actor, req); !errors.Is(err, crypto.ErrNotDualWrapped) {
				t.Fatalf("stale dual-wrapper proof committed: %v", err)
			}
			status, err = s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			if status.Generation != req.ExpectedGeneration {
				t.Fatal("failed root assertion advanced target generation")
			}
			if status.Job.State != "preparing" {
				t.Fatalf("failed root assertion changed prepared job: %s", status.Job.State)
			}
			probe.mu.Lock()
			submissions := probe.submissions
			probe.mu.Unlock()
			if submissions != 0 {
				t.Fatal("stale root proof escaped to deployment")
			}
		})
	}
}

type selfConfigTestRootSource []byte

func (r selfConfigTestRootSource) Current(context.Context) ([]byte, error) {
	return bytes.Clone(r), nil
}
func (r selfConfigTestRootSource) Next(context.Context) ([]byte, error) { return bytes.Clone(r), nil }

func TestSelfConfigRootFinalizationWaitsForRestoredRolloutRepair(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, actor, session, probe, restore := partialDeploymentFixture(t, engine)
			root := bytes.Repeat([]byte{0x44}, crypto.KeySize)
			wrapper, err := s.Keyring.PrepareRootKeyRotation(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}
			wrapper.CreatedAt = store.CanonTime(s.now())
			if err := tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				_, proof, err := authorize(ctx, az, local, authz.OpRotateRootKey, domain.Scope{}, s.now())
				if err != nil {
					return err
				}
				return r.Keys().RootKeyRotatePrepare(ctx, proof, wrapper)
			}); err != nil {
				t.Fatal(err)
			}
			s.Deployment = &restoredDeploymentProbe{probe}
			status, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			oldJobID := status.Job.ID
			scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
			draft, err := (&Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).Set(t.Context(), local, scope, config.ManagedBootstrapSourcesKey, `{"version":1,"database_source":"original"}`, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (&Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{draft.VersionID}}); err != nil {
				t.Fatal(err)
			}
			status, err = s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			repair := installerRequest(status, "repair-retained-root")
			repair.PrepareOnly = true
			selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "rollout-restore", OwnerInstanceID: status.OwnerInstanceID, Revision: restore.Revision, ExpectedGeneration: restore.ExpectedGeneration, SchemaVersion: restore.SchemaVersion, PlanDigest: restore.PlanDigest})
			if _, err := s.RestoreDeployment(t.Context(), actor, restore); err != nil {
				t.Fatal(err)
			}
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := finalizeSelfConfigTestRoot(t.Context(), s, local); !errors.Is(err, store.ErrRootFinalizationPendingDeployment) {
				t.Fatalf("restored but unrepaired target allowed finalization: %v", err)
			}
			pending := beginInstallerApply(t, s, actor, repair)
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			prepared := awaitInstallerApply(t, pending)
			if prepared.err != nil {
				t.Fatal(prepared.err)
			}
			if prepared.status.Job.PlanDigest != "" {
				t.Fatal("repair unexpectedly required new rollout")
			}
			old, err := s.status(t.Context(), local, oldJobID)
			if err != nil || old.Job.Error != "superseded" {
				t.Fatalf("fixture did not supersede restored job: %v", err)
			}
			if _, err := finalizeSelfConfigTestRoot(t.Context(), s, local); !errors.Is(err, store.ErrRootFinalizationPendingDeployment) {
				t.Fatalf("preparing repair released rollback root: %v", err)
			}
			repair.PrepareOnly = false
			selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: status.OwnerInstanceID, Revision: repair.Revision, ExpectedGeneration: repair.ExpectedGeneration, SchemaVersion: repair.SchemaVersion})
			if _, err := s.Apply(t.Context(), actor, repair); err != nil {
				t.Fatal(err)
			}
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			completed, err := s.Status(t.Context(), local)
			if err != nil || completed.Job.State != "completed" {
				t.Fatalf("repair did not complete: %v", err)
			}
			if epoch, err := finalizeSelfConfigTestRoot(t.Context(), s, local); err != nil || epoch != 2 {
				t.Fatalf("completed normal generation retained old rollout fence: epoch=%d err=%v", epoch, err)
			}
		})
	}
}
