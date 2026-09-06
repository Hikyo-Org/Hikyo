package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"k8s.io/apimachinery/pkg/types"
)

func rolloutIntent(b store.SelfConfigBinding, j store.SelfConfigJob) configrollout.Intent {
	return configrollout.Intent{JobID: j.ID, OwnerInstanceID: b.OwnerInstanceID, Incarnation: b.Incarnation, SnapshotID: j.SnapshotID, Revision: j.Revision, CatalogueVersion: int(j.SchemaVersion), ExpectedGeneration: j.ExpectedGeneration, Generation: j.Generation}
}

func (s *SelfConfig) deploymentMatches(b store.SelfConfigBinding) bool {
	if s.Deployment == nil {
		return false
	}
	id := s.Deployment.Identity()
	return id.OwnerInstanceID == b.OwnerInstanceID && id.Incarnation == b.Incarnation && id.EnrollmentID != "" && id.DeploymentUID != ""
}

func decodeRolloutCommand(raw string) (configrollout.SignedCommand, error) {
	var command configrollout.SignedCommand
	if err := json.Unmarshal([]byte(raw), &command); err != nil {
		return command, domain.ErrConflict
	}
	canonical, err := json.Marshal(command)
	if err != nil || string(canonical) != raw {
		return command, domain.ErrConflict
	}
	return command, nil
}

func encodeRolloutCommand(command configrollout.SignedCommand) (string, error) {
	// This journal is alias-only. Ordinary values must never enter its metadata.
	if len(command.Command.Changes) != 0 {
		return "", domain.ErrInvalid
	}
	raw, err := json.Marshal(command)
	if err != nil {
		return "", domain.ErrInvalid
	}
	return string(raw), nil
}

func (s *SelfConfig) prepareDeployment(ctx context.Context, b store.SelfConfigBinding, j store.SelfConfigJob, bundle *runtimeconfig.Bundle) (bool, error) {
	sources := bundle.BootstrapSources()
	previous, err := s.prepareRuntimeSnapshot(ctx, b, b.DesiredSnapshotID, b.DesiredRevision)
	if err != nil {
		return false, err
	}
	if previous.BootstrapSources().Topology.NodeID != "" && sources.Topology.NodeID == "" {
		return false, domain.ErrInvalid
	}
	if sources == (config.ManagedBootstrapSources{}) {
		if previous.BootstrapSources() != (config.ManagedBootstrapSources{}) {
			return false, domain.ErrInvalid
		}
		return true, nil
	}
	if !s.deploymentMatches(b) {
		return false, configrollout.ErrUnsupported
	}
	if s.Deployment.VerifyInstalled(ctx, bundle) == nil {
		metadata, err := s.DB.Coordination().CurrentSelfConfigGeneration(ctx)
		if err != nil {
			return false, err
		}
		if !metadata.TopologyRestoring {
			return true, nil
		}
	}
	var row store.SelfConfigRollout
	var sequence int64
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		row, err = r.SelfConfig().Rollout(ctx, p, j.ID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		nodes, err := r.SelfConfig().Nodes(ctx, p)
		if err != nil {
			return err
		}
		if len(nodes) != 1 || nodes[0].NodeID != s.NodeID {
			return configrollout.ErrUnsupported
		}
		sequence, err = r.SelfConfig().NextRolloutSequence(ctx, p, s.Deployment.Identity().EnrollmentID)
		return err
	})
	if err != nil {
		return false, err
	}
	if row.JobID == "" {
		command, err := s.Deployment.PrepareCommand(ctx, rolloutIntent(b, j), bundle, uint64(sequence))
		if err != nil {
			return false, err
		}
		raw, err := encodeRolloutCommand(command)
		if err != nil {
			return false, err
		}
		row = store.SelfConfigRollout{JobID: j.ID, EnrollmentID: s.Deployment.Identity().EnrollmentID, Incarnation: b.Incarnation, CommandJSON: raw, Sequence: sequence}
		err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			p, err := az.SelfConfigRuntimeAuthority(ctx, "")
			if err != nil {
				return err
			}
			if err := r.SelfConfig().PutRollout(ctx, p, row); err != nil {
				return err
			}
			row.RowVersion = 1
			return nil
		})
		if err != nil {
			return false, err
		}
	}
	command, err := decodeRolloutCommand(row.CommandJSON)
	if err != nil {
		return false, err
	}
	if row.Incarnation != b.Incarnation || row.EnrollmentID != s.Deployment.Identity().EnrollmentID || command.Command.Intent != rolloutIntent(b, j) || command.Command.Action != configrollout.ActionPrepare {
		return false, domain.ErrConflict
	}
	if row.PlanDigest != "" {
		return true, nil
	}
	if err := s.Deployment.Send(ctx, command); err != nil {
		return false, err
	}
	response, err := s.Deployment.Response(ctx, command)
	if errors.Is(err, configrollout.ErrNotSubmitted) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if response.Outcome != "complete" || response.PlanDigest == "" || response.TemplateStamp == "" {
		return false, configrollout.ErrUnsupported
	}
	raw, err := json.Marshal(response)
	if err != nil {
		return false, err
	}
	row.PlanDigest, row.ResponseJSON = response.PlanDigest, string(raw)
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		return r.SelfConfig().PutRollout(ctx, p, row)
	})
	return err == nil, err
}

// prepareDeploymentCommit holds the signed command only in memory. Source proof
// uses its own connection, so it must run outside the final transaction even
// when a node's PostgreSQL pool has one connection. Only the final exact-MFA
// transaction may make these bytes durable and visible to the sender.
type selfConfigDeploymentSubmission struct {
	store.SelfConfigRollout
	rootWrapper *crypto.WrappedKey
	rootEpoch   uint32
}

func (s *SelfConfig) prepareDeploymentCommit(ctx context.Context, actor Actor, b store.SelfConfigBinding, j store.SelfConfigJob, digest string) (*selfConfigDeploymentSubmission, error) {
	var row store.SelfConfigRollout
	var sequence int64
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		_, p, err := authorize(ctx, az, actor, authz.OpSelfConfigApply, selfConfigScope(b), s.now())
		if err != nil {
			return err
		}
		row, err = r.SelfConfig().Rollout(ctx, p, j.ID)
		if errors.Is(err, domain.ErrNotFound) {
			if digest != "" {
				return domain.ErrConflict
			}
			return nil
		}
		if err != nil {
			return err
		}
		current, err := r.SelfConfig().Job(ctx, p, j.ID)
		if err != nil {
			return err
		}
		if current.Status != "preparing" || row.PlanDigest == "" || row.PlanDigest != digest || row.Incarnation != b.Incarnation {
			return domain.ErrConflict
		}
		sequence, err = r.SelfConfig().NextRolloutSequence(ctx, p, row.EnrollmentID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if row.JobID == "" {
		return nil, nil
	}
	if !s.deploymentMatches(b) {
		return nil, configrollout.ErrUnsupported
	}
	command, err := decodeRolloutCommand(row.CommandJSON)
	if err != nil {
		return nil, err
	}
	if command.Command.Intent != rolloutIntent(b, j) || command.Command.Action != configrollout.ActionPrepare {
		return nil, domain.ErrConflict
	}
	submitted, err := s.Deployment.DecisionCommand(ctx, command, configrollout.ActionSubmit, uint64(sequence), row.PlanDigest, nil)
	if err != nil {
		return nil, err
	}
	row.CommandJSON, err = encodeRolloutCommand(submitted)
	if err != nil {
		return nil, err
	}
	row.Sequence = sequence
	wrapper, err := s.Deployment.RootPreparation(ctx, command)
	if err != nil {
		return nil, err
	}
	var epoch uint32
	if command.Command.Bootstrap != nil && command.Command.Bootstrap.Root != nil {
		rootEpoch := command.Command.Bootstrap.Root.RootEpoch
		if rootEpoch < 2 || rootEpoch > math.MaxUint32 {
			return nil, domain.ErrConflict
		}
		epoch = uint32(rootEpoch)
	}
	return &selfConfigDeploymentSubmission{SelfConfigRollout: row, rootWrapper: wrapper, rootEpoch: epoch}, nil
}

// reconcileDeployment sends only commands already committed in the owner's DB.
// Runtime installation can precede Kubernetes readiness: readiness itself needs
// the installed generation. Deployment completion additionally requires the exact
// fixed application acknowledgements and the executor's Applied receipt.
func (s *SelfConfig) reconcileDeployment(ctx context.Context, b store.SelfConfigBinding, j store.SelfConfigJob, nodes []store.SelfConfigNode) (bool, bool, error) {
	var row store.SelfConfigRollout
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		row, err = r.SelfConfig().Rollout(ctx, p, j.ID)
		return err
	})
	if errors.Is(err, domain.ErrNotFound) {
		return true, true, nil
	}
	if err != nil {
		return false, false, err
	}
	if !s.deploymentMatches(b) || row.Incarnation != b.Incarnation {
		return false, false, configrollout.ErrUnsupported
	}
	if row.ExternalPhase == "restored" {
		return false, false, nil
	}
	command, err := decodeRolloutCommand(row.CommandJSON)
	if err != nil {
		return false, false, err
	}
	if command.Command.Intent != rolloutIntent(b, j) || command.Command.Action == configrollout.ActionPrepare || command.Command.PlanDigest != row.PlanDigest {
		return false, false, domain.ErrConflict
	}
	row, command, err = s.renewDeploymentDelivery(ctx, b, j, row, command)
	if err != nil {
		return false, false, err
	}
	if err := s.Deployment.Send(ctx, command); err != nil {
		return false, false, err
	}
	response, responseErr := s.Deployment.Response(ctx, command)
	if responseErr != nil && !errors.Is(responseErr, configrollout.ErrNotSubmitted) {
		return false, false, responseErr
	}
	if responseErr == nil && response.Outcome == "complete" && response.Receipt != nil && response.Receipt.Intent == rolloutIntent(b, j) && response.Receipt.PlanDigest == row.PlanDigest && response.Receipt.DeploymentUID == types.UID(s.Deployment.Identity().DeploymentUID) && response.Receipt.Phase == configrollout.Restored && command.Command.Action == configrollout.ActionRestore {
		return false, false, s.recordDeploymentTerminal(ctx, row, "restored")
	}
	var preparation configrollout.Response
	if json.Unmarshal([]byte(row.ResponseJSON), &preparation) != nil || preparation.PlanDigest != row.PlanDigest {
		return false, false, domain.ErrConflict
	}
	installed := command.Command.Action != configrollout.ActionRestore && s.Deployment.Identity().TemplateStamp == preparation.TemplateStamp
	bundle, err := s.prepareRuntimeSnapshot(ctx, b, b.DesiredSnapshotID, b.DesiredRevision)
	if err != nil {
		return false, false, err
	}
	installed = installed && s.Deployment.VerifyInstalled(ctx, bundle) == nil
	complete := installed && len(nodes) > 0
	for _, n := range nodes {
		if n.JobID != j.ID || n.Incarnation != b.Incarnation || n.ActiveGeneration != b.Generation || n.ActiveRevision != b.DesiredRevision || n.ErrorCode != "" {
			complete = false
		}
	}
	if responseErr == nil && response.Outcome == "complete" && response.Receipt != nil && response.Receipt.Intent == rolloutIntent(b, j) && response.Receipt.PlanDigest == row.PlanDigest && response.Receipt.DeploymentUID == types.UID(s.Deployment.Identity().DeploymentUID) && response.Receipt.Phase == configrollout.Applied && response.Receipt.ApplicationAcknowledged {
		return installed, complete, s.recordDeploymentTerminal(ctx, row, "applied")
	}
	if responseErr != nil {
		return installed, false, nil
	}
	if response.Outcome != "complete" {
		return installed, false, configrollout.ErrUnsupported
	}
	// Each observation has a fresh durable sequence, while retaining the exact
	// authorized plan. An acknowledgement is added only after all fixed nodes agree.
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		current, err := r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		if current.Generation != b.Generation || current.Incarnation != b.Incarnation {
			return domain.ErrConflict
		}
		sequence, err := r.SelfConfig().NextRolloutSequence(ctx, p, row.EnrollmentID)
		if err != nil {
			return err
		}
		var ack *configrollout.ApplicationAcknowledgement
		if complete {
			ack = &configrollout.ApplicationAcknowledgement{Intent: rolloutIntent(b, j), PlanDigest: row.PlanDigest, DeploymentUID: types.UID(s.Deployment.Identity().DeploymentUID), ReadyReplicas: int32(len(nodes))}
		}
		next, err := s.Deployment.DecisionCommand(ctx, command, configrollout.ActionObserve, uint64(sequence), row.PlanDigest, ack)
		if err != nil {
			return err
		}
		row.CommandJSON, err = encodeRolloutCommand(next)
		if err != nil {
			return err
		}
		row.Sequence = sequence
		return r.SelfConfig().PutRollout(ctx, p, row)
	})
	return installed, false, err
}

func (s *SelfConfig) abortExpiredDeployment(ctx context.Context, actor Actor, b store.SelfConfigBinding, j store.SelfConfigJob) error {
	at, err := s.runtimeTimestamp(ctx)
	if err != nil {
		return err
	}
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpSelfConfigApply, selfConfigScope(b), s.now())
		if err != nil {
			return err
		}
		current, err := r.SelfConfig().Job(ctx, p, j.ID)
		if err != nil {
			return err
		}
		if current.Status != "preparing" {
			return nil
		}
		if err := r.SelfConfig().FinishJob(ctx, p, j.ID, "aborted", "preparation_failed", at); err != nil {
			return err
		}
		ev, err := newAuditEvent(ctx, audit.EventSelfConfigApplyRequested, caller.Principal, audit.Object{Type: "environment", ID: b.EnvironmentID}, audit.OutcomeFailure, j.ID, audit.Payload{"owner_instance_id": b.OwnerInstanceID, "revision": j.Revision, "generation": j.Generation, "job_id": j.ID, "error_code": "preparation_failed"})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
}

func (s *SelfConfig) recordDeploymentTerminal(ctx context.Context, row store.SelfConfigRollout, phase string) error {
	if row.ExternalPhase == phase {
		return nil
	}
	row.ExternalPhase = phase
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		return r.SelfConfig().PutRollout(ctx, p, row)
	})
}

func (s *SelfConfig) commitDeploymentRoot(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, caller authz.Identity, submission *selfConfigDeploymentSubmission, at time.Time) error {
	if submission.rootEpoch == 0 {
		return nil
	}
	p, err := az.Authorize(ctx, caller, authz.OpRotateRootKey, domain.Scope{})
	if err != nil {
		return err
	}
	if submission.rootWrapper == nil {
		return r.Keys().AssertRootKeyEpoch(ctx, p, submission.rootEpoch)
	}
	wrapper := *submission.rootWrapper
	if wrapper.RootKeyEpoch != submission.rootEpoch {
		return domain.ErrConflict
	}
	wrapper.CreatedAt = store.CanonTime(at)
	if err := r.Keys().RootKeyRotatePrepare(ctx, p, wrapper); err != nil {
		return err
	}
	ev, err := newAuditEvent(ctx, audit.EventRootKeyRotationPrepared, caller.Principal, audit.Object{Type: "instance", ID: "instance"}, audit.OutcomeSuccess, submission.JobID, audit.Payload{"root_key_epoch": int64(wrapper.RootKeyEpoch)})
	if err != nil {
		return err
	}
	return r.Audit().InsertInstance(ctx, p, ev)
}
