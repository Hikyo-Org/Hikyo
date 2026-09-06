package service

import (
	"context"
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

func TestDeploymentRenewalRejectsChangedDecisionAndStaleRow(t *testing.T) {
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
