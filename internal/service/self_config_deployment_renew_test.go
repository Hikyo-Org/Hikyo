package service

import (
	"context"
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"k8s.io/apimachinery/pkg/types"
)

type renewalDeploymentProbe struct {
	*deploymentProbe
	service   *SelfConfig
	delivered []configrollout.SignedCommand
	tamper    bool
}

func (p *renewalDeploymentProbe) RenewCommand(ctx context.Context, command configrollout.SignedCommand, sequence uint64) (configrollout.SignedCommand, error) {
	renewed, err := p.deploymentProbe.RenewCommand(ctx, command, sequence)
	if p.tamper {
		renewed.Command.Intent.SnapshotID = "another-snapshot"
	}
	return renewed, err
}

func (p *renewalDeploymentProbe) Send(ctx context.Context, command configrollout.SignedCommand) error {
	if !time.Now().Before(command.Command.ExpiresAt) {
		return errors.New("executor rejected expired first delivery")
	}
	err := tx.Read(ctx, p.service.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		proof, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		row, err := r.SelfConfig().Rollout(ctx, proof, command.Command.Intent.JobID)
		if err != nil {
			return err
		}
		raw, err := encodeRolloutCommand(command)
		if err != nil {
			return err
		}
		if raw != row.CommandJSON || int64(command.Command.Sequence) != row.Sequence {
			return errors.New("delivery preceded durable renewal")
		}
		return nil
	})
	if err != nil {
		return err
	}
	p.delivered = append(p.delivered, command)
	return p.deploymentProbe.Send(ctx, command)
}

func (p *renewalDeploymentProbe) Response(ctx context.Context, command configrollout.SignedCommand) (configrollout.Response, error) {
	if command.Command.Action != configrollout.ActionRestore {
		return p.deploymentProbe.Response(ctx, command)
	}
	return configrollout.Response{Outcome: "complete", Receipt: &configrollout.Receipt{Intent: command.Command.Intent, PlanDigest: command.Command.PlanDigest, DeploymentUID: types.UID(p.identity.DeploymentUID), Phase: configrollout.Restored}}, nil
}

func expireCommittedDelivery(t *testing.T, s *SelfConfig, jobID string) (store.SelfConfigBinding, store.SelfConfigJob, store.SelfConfigRollout, configrollout.SignedCommand) {
	t.Helper()
	var b store.SelfConfigBinding
	var job store.SelfConfigJob
	var row store.SelfConfigRollout
	var command configrollout.SignedCommand
	err := tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		b, err = r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		job, err = r.SelfConfig().Job(ctx, p, jobID)
		if err != nil {
			return err
		}
		row, err = r.SelfConfig().Rollout(ctx, p, jobID)
		if err != nil {
			return err
		}
		command, err = decodeRolloutCommand(row.CommandJSON)
		if err != nil {
			return err
		}
		sequence, err := r.SelfConfig().NextRolloutSequence(ctx, p, row.EnrollmentID)
		if err != nil {
			return err
		}
		command.Command.Sequence = uint64(sequence)
		command.Command.IssuedAt = time.Now().Add(-10 * time.Minute).UTC()
		command.Command.ExpiresAt = time.Now().Add(-5 * time.Minute).UTC()
		row.CommandJSON, err = encodeRolloutCommand(command)
		if err != nil {
			return err
		}
		row.Sequence = sequence
		if err := r.SelfConfig().PutRollout(ctx, p, row); err != nil {
			return err
		}
		row.RowVersion++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b, job, row, command
}

func TestCommittedDeploymentRenewsUnseenSubmitAndRestoreWithoutNewMFA(t *testing.T) {
	t.Parallel()
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		for _, action := range []configrollout.Action{configrollout.ActionSubmit, configrollout.ActionRestore} {
			t.Run(string(engine)+"/"+string(action), func(t *testing.T) {
				s, local, actor, session, base, restore := committedDeploymentFixture(t, engine, action == configrollout.ActionRestore)
				if action == configrollout.ActionRestore {
					selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "rollout-restore", OwnerInstanceID: base.identity.OwnerInstanceID, Revision: restore.Revision, ExpectedGeneration: restore.ExpectedGeneration, SchemaVersion: restore.SchemaVersion, PlanDigest: restore.PlanDigest})
					if _, err := s.RestoreDeployment(t.Context(), actor, restore); err != nil {
						t.Fatal(err)
					}
				}
				status, err := s.Status(t.Context(), local)
				if err != nil {
					t.Fatal(err)
				}
				_, _, row, expired := expireCommittedDelivery(t, s, status.Job.ID)
				probe := &renewalDeploymentProbe{deploymentProbe: base, service: s}
				s.Deployment = probe
				// All original ceremonies have already been consumed by commit.
				if err := s.ReconcileRuntime(t.Context()); err != nil {
					t.Fatal(err)
				}
				if len(probe.delivered) != 1 {
					t.Fatal("expired committed command was not delivered")
				}
				got := probe.delivered[0].Command
				if got.Sequence <= uint64(row.Sequence) || got.Action != action || got.Intent != expired.Command.Intent || got.PlanDigest != row.PlanDigest {
					t.Fatal("renewal changed authority")
				}
				if action == configrollout.ActionRestore {
					status, err = s.Status(t.Context(), local)
					if err != nil || !status.Job.DeploymentRestored {
						t.Fatalf("renewed restore did not finish: %v", err)
					}
				}
			})
		}
	}
}

// The persisted command is the strict envelope: two signatures, the trust
// binding and about 5.7 KB of JSON in a TEXT column. Both engines must store
// it, return it byte-identically (decodeRolloutCommand demands a canonical
// round trip), and renew it without changing the binding or the decision.
func TestCommittedDeploymentCarriesStrictEnvelopeAcrossRenewalBothEngines(t *testing.T) {
	t.Parallel()
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		for _, action := range []configrollout.Action{configrollout.ActionSubmit, configrollout.ActionRestore} {
			t.Run(string(engine)+"/"+string(action), func(t *testing.T) {
				signers, keys := strictSigners(t)
				enrollment := configrollout.Enrollment{ID: "enrolled", Target: configrollout.Target{Namespace: "hikyo", Deployment: "hikyo"}}
				ids := keys.KeyIDs()
				enrollment.Authority = &configrollout.Authority{Profile: "pqc-auth/1", Generation: 2, Required: []string{configrollout.AlgEd25519, configrollout.AlgMLDSA65}, KeyIDs: []string{ids[configrollout.AlgEd25519], ids[configrollout.AlgMLDSA65]}}
				sign := func(command configrollout.Command) (configrollout.SignedCommand, error) {
					return configrollout.SignCommandFor(t.Context(), enrollment, signers, command)
				}
				s, local, actor, session, base, restore := committedDeploymentFixtureSigned(t, engine, action == configrollout.ActionRestore, sign)
				if action == configrollout.ActionRestore {
					selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "rollout-restore", OwnerInstanceID: base.identity.OwnerInstanceID, Revision: restore.Revision, ExpectedGeneration: restore.ExpectedGeneration, SchemaVersion: restore.SchemaVersion, PlanDigest: restore.PlanDigest})
					if _, err := s.RestoreDeployment(t.Context(), actor, restore); err != nil {
						t.Fatal(err)
					}
				}
				status, err := s.Status(t.Context(), local)
				if err != nil {
					t.Fatal(err)
				}
				_, _, row, expired := expireCommittedDelivery(t, s, status.Job.ID)
				if expired.Command.Authority == nil || len(expired.Signatures) != 2 || len(expired.Signature) != 0 {
					t.Fatalf("persisted command is not the strict envelope: %+v", expired)
				}
				probe := &renewalDeploymentProbe{deploymentProbe: base, service: s}
				s.Deployment = probe
				if err := s.ReconcileRuntime(t.Context()); err != nil {
					t.Fatal(err)
				}
				if len(probe.delivered) != 1 {
					t.Fatal("expired strict command was not renewed and delivered")
				}
				got := probe.delivered[0]
				if got.Command.Sequence <= uint64(row.Sequence) || got.Command.Action != action || got.Command.Intent != expired.Command.Intent || got.Command.PlanDigest != row.PlanDigest {
					t.Fatal("renewal changed authority")
				}
				if got.Command.Authority == nil || got.Command.Authority.Generation != 2 || len(got.Signatures) != 2 {
					t.Fatalf("renewal dropped the trust binding or a signature: %+v", got)
				}
				if len(got.Signatures[1].Signature) != 3309 {
					t.Fatalf("ML-DSA-65 signature is %d bytes", len(got.Signatures[1].Signature))
				}
			})
		}
	}
}

func TestDeploymentRenewalRejectsChangedDecisionAndStaleRow(t *testing.T) {
	t.Parallel()
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, _, _, base, _ := committedDeploymentFixture(t, engine, false)
			status, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			b, job, row, command := expireCommittedDelivery(t, s, status.Job.ID)
			probe := &renewalDeploymentProbe{deploymentProbe: base, service: s, tamper: true}
			s.Deployment = probe
			if _, _, err := s.renewDeploymentDelivery(t.Context(), b, job, row, command); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("altered renewal accepted: %v", err)
			}
			probe.tamper = false
			expireCommittedDelivery(t, s, status.Job.ID)
			if _, _, err := s.renewDeploymentDelivery(t.Context(), b, job, row, command); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("stale renewal replaced newer command: %v", err)
			}
			if len(probe.delivered) != 0 {
				t.Fatal("refused renewal escaped")
			}
		})
	}
}

func strictSigners(t *testing.T) (configrollout.Signers, configrollout.PublicKeys) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pq, err := mldsa.GenerateKey(mldsa.MLDSA65())
	if err != nil {
		t.Fatal(err)
	}
	signers := configrollout.Signers{Ed25519: private, MLDSA65: pq}
	return signers, signers.Public()
}

// rolloverRefusingProbe is the provider after a re-enrollment: it cannot verify
// the persisted pre-rollover command, so it refuses every operation on it, as
// the real provider does (see the app test for a pre-rollover row).
type rolloverRefusingProbe struct{ *deploymentProbe }

func (rolloverRefusingProbe) RenewCommand(context.Context, configrollout.SignedCommand, uint64) (configrollout.SignedCommand, error) {
	return configrollout.SignedCommand{}, configrollout.ErrInvalid
}
func (rolloverRefusingProbe) DecisionCommand(context.Context, configrollout.SignedCommand, configrollout.Action, uint64, string, *configrollout.ApplicationAcknowledgement) (configrollout.SignedCommand, error) {
	return configrollout.SignedCommand{}, configrollout.ErrInvalid
}
func (rolloverRefusingProbe) Send(context.Context, configrollout.SignedCommand) error {
	return configrollout.ErrInvalid
}
func (rolloverRefusingProbe) Response(context.Context, configrollout.SignedCommand) (configrollout.Response, error) {
	return configrollout.Response{}, configrollout.ErrInvalid
}

// An open job whose persisted command predates a re-enrollment cannot be
// delivered, answered, renewed or restored. Reconciliation clears the active
// runtime and returns the error, the job stays open, and app.Boot treats that
// error as fatal unless sources are pending (internal/app/app.go, LoadRuntime).
// This is registered gap G-2; the test pins the service half of it.
func TestOpenJobAcrossAReEnrollmentStaysOpenAndClearsTheRuntime(t *testing.T) {
	t.Parallel()
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			signers, keys := strictSigners(t)
			ids := keys.KeyIDs()
			enrollment := configrollout.Enrollment{ID: "enrolled", Target: configrollout.Target{Namespace: "hikyo", Deployment: "hikyo"}, Authority: &configrollout.Authority{Profile: "pqc-auth/1", Generation: 1, Required: []string{configrollout.AlgEd25519, configrollout.AlgMLDSA65}, KeyIDs: []string{ids[configrollout.AlgEd25519], ids[configrollout.AlgMLDSA65]}}}
			sign := func(command configrollout.Command) (configrollout.SignedCommand, error) {
				return configrollout.SignCommandFor(t.Context(), enrollment, signers, command)
			}
			s, local, actor, session, base, restore := committedDeploymentFixtureSigned(t, engine, false, sign)
			before, err := s.Status(t.Context(), local)
			if err != nil || before.Job == nil || before.Job.CompletedAt != nil {
				t.Fatalf("fixture job is not open: %v %+v", err, before.Job)
			}
			s.Deployment = rolloverRefusingProbe{base}
			err = s.ReconcileRuntime(t.Context())
			if !errors.Is(err, configrollout.ErrInvalid) {
				t.Fatalf("reconciliation across a re-enrollment: %v", err)
			}
			if s.active.Load() != nil {
				t.Fatal("the active runtime survived the refusal")
			}
			after, err := s.Status(t.Context(), local)
			if err != nil || after.Job == nil || after.Job.ID != before.Job.ID || after.Job.State != "partial" || after.Job.Error != "transport_failed" || after.Job.CompletedAt != nil {
				t.Fatalf("the job did not become an open partial job: %v %+v", err, after.Job)
			}
			// Restore, the only way out of a partial job, needs a fresh decision
			// command signed on the old row. The provider refuses it.
			owner, _, err := s.DB.RecoveryIdentity()
			if err != nil {
				t.Fatal(err)
			}
			selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "rollout-restore", OwnerInstanceID: owner, Revision: restore.Revision, ExpectedGeneration: restore.ExpectedGeneration, SchemaVersion: restore.SchemaVersion, PlanDigest: restore.PlanDigest})
			if _, err := s.RestoreDeployment(t.Context(), actor, restore); !errors.Is(err, configrollout.ErrInvalid) {
				t.Fatalf("restore across a re-enrollment: %v", err)
			}
		})
	}
}
