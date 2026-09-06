package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

const selfConfigConvergenceTimeout = 30 * time.Second
const selfConfigReconcileInterval = 2 * time.Second

var ErrSelfConfigUnavailable = errors.New("runtime configuration is not ready; retry after reconciliation")

type selfConfigActive struct {
	bundle                                    *runtimeconfig.Bundle
	owner, incarnation, snapshotID, seedToken string
	generation, revision                      int64
}

// Capture performs only a metadata read on the request stack. It never mints
// runtime payload authority or silently falls back to a stale configuration.
func (s *SelfConfig) Capture(ctx context.Context) (*runtimeconfig.Bundle, error) {
	metadata, err := s.DB.Coordination().CurrentSelfConfigGeneration(ctx)
	if err != nil {
		return nil, ErrSelfConfigUnavailable
	}
	active := s.active.Load()
	if active == nil {
		return nil, ErrSelfConfigUnavailable
	}
	if metadata.Managed {
		if metadata.Topology != nil && (metadata.Topology.After.NodeID != s.NodeID || metadata.Topology.After.HA != s.HAMode || s.Deployment == nil || s.Deployment.Identity().TemplateStamp != metadata.TopologyStamp) {
			return nil, ErrSelfConfigUnavailable
		}
		owner, incarnation, err := s.DB.RecoveryIdentity()
		if err != nil || owner != metadata.OwnerInstanceID || incarnation != metadata.Incarnation || metadata.Suspended || metadata.DeploymentRestoring || active.owner != metadata.OwnerInstanceID || active.generation != metadata.Generation || active.incarnation != metadata.Incarnation {
			return nil, ErrSelfConfigUnavailable
		}
	} else if active.generation != 0 {
		return nil, ErrSelfConfigUnavailable
	}
	return active.bundle, nil
}

// LoadRuntime is a boot/worker entrypoint, never an HTTP handler operation.
func (s *SelfConfig) LoadRuntime(ctx context.Context) error {
	metadata, err := s.DB.Coordination().CurrentSelfConfigGeneration(ctx)
	if err != nil {
		return err
	}
	if metadata.Managed {
		return s.ReconcileRuntime(ctx)
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if current := s.active.Load(); current != nil && current.generation == 0 {
		return s.attestSeed(ctx, current)
	}
	seed, err := s.prepareSeed()
	if err != nil {
		return err
	}
	bundle, err := runtimeconfig.Prepare(seed.values)
	if err != nil {
		return err
	}
	if err := s.activateInstallation(ctx, "seed:"+seed.token, seed.incarnation, bundle); err != nil {
		return err
	}
	active := &selfConfigActive{bundle: bundle, owner: seed.owner, incarnation: seed.incarnation, seedToken: seed.token}
	s.active.Store(active)
	s.installed.Store(active)
	return s.attestSeed(ctx, active)
}

func (s *SelfConfig) attestSeed(ctx context.Context, active *selfConfigActive) error {
	if s.SeedNode != nil {
		seed, err := s.prepareSeed()
		if err != nil {
			return err
		}
		return s.attestNodeSeed(ctx, seed)
	}
	at, err := s.runtimeTimestamp(ctx)
	if err != nil {
		return err
	}
	return s.DB.Coordination().SelfConfigSeedAttest(ctx, s.NodeID, runtimeconfig.SchemaVersion, active.seedToken, at)
}

func (s *SelfConfig) runtimeTimestamp(ctx context.Context) (time.Time, error) {
	if s.DB.Engine() == store.EnginePostgres {
		return s.DB.Coordination().Now(ctx)
	}
	return s.now(), nil
}

// Run reconciles committed targets independently of the initiating request's
// lifetime or its actor's later grant changes. Its context must be app-owned.
func (s *SelfConfig) Run(ctx context.Context) {
	defer func() {
		if err := s.CloseRuntime(); err != nil {
			s.logSelfConfigFailure(context.Background(), "preparation_cleanup_failed")
		}
	}()
	outcomesDone := make(chan struct{})
	go func() { defer close(outcomesDone); s.runMailOutcomes(ctx) }()
	defer func() { <-outcomesDone }()
	ticker := time.NewTicker(selfConfigReconcileInterval)
	defer ticker.Stop()
	failed := false
	for {
		err := s.ReconcileRuntime(ctx)
		if err != nil && !failed && ctx.Err() == nil {
			s.logSelfConfigFailure(ctx, "reconciliation_failed")
		}
		failed = err != nil
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *SelfConfig) logSelfConfigFailure(ctx context.Context, code string) {
	if s.Auth != nil && s.Auth.Log != nil {
		s.Auth.Log.ErrorContext(ctx, "self-configuration operation failed", "error_code", code)
	}
}

func (s *SelfConfig) ReconcileRuntime(ctx context.Context) error {
	metadata, err := s.DB.Coordination().CurrentSelfConfigGeneration(ctx)
	if err != nil {
		return err
	}
	if !metadata.Managed {
		return s.LoadRuntime(ctx)
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	var binding store.SelfConfigBinding
	var jobs []store.SelfConfigJob
	var nodes []store.SelfConfigNode
	err = tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		binding, err = r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		jobs, err = r.SelfConfig().Jobs(ctx, p)
		if err != nil {
			return err
		}
		nodes, err = r.SelfConfig().Nodes(ctx, p)
		return err
	})
	if err != nil {
		return err
	}
	owner, incarnation, err := s.DB.RecoveryIdentity()
	if err != nil {
		return err
	}
	if owner != binding.OwnerInstanceID {
		s.active.Store(nil)
		return errors.Join(ErrSelfConfigUnavailable, s.closePrepared())
	}
	if incarnation != binding.Incarnation {
		s.active.Store(nil)
		if err := s.closePrepared(); err != nil {
			return err
		}
		at, err := s.runtimeTimestamp(ctx)
		if err != nil {
			return err
		}
		return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			p, err := az.SelfConfigRuntimeAuthority(ctx, "")
			if err != nil {
				return err
			}
			if err := r.SelfConfig().FenceRestored(ctx, p, incarnation, at); err != nil {
				return err
			}
			ev, err := newAuditEvent(ctx, audit.EventSelfConfigRecoveryFenced, "", audit.Object{Type: "environment", ID: binding.EnvironmentID}, audit.OutcomeSuccess, "", audit.Payload{"owner_instance_id": binding.OwnerInstanceID, "revision": binding.DesiredRevision, "generation": binding.Generation, "error_code": "restored"})
			if err != nil {
				return err
			}
			return r.Audit().InsertTenant(ctx, p, ev)
		})
	}
	var job store.SelfConfigJob
	for _, j := range jobs {
		if j.Status == "preparing" || j.Status == "applying" || j.Status == "partial" {
			job = j
			break
		}
	}
	deploymentApplied := true
	if job.Status == "applying" || job.Status == "partial" {
		installed, applied, err := s.reconcileDeployment(ctx, binding, job, nodes)
		if err != nil {
			s.active.Store(nil)
			return errors.Join(err, s.recordRuntimeRefusal(ctx, binding, nodes, "transport_failed"))
		}
		if !installed {
			s.active.Store(nil)
			at, err := s.runtimeTimestamp(ctx)
			if err != nil {
				return err
			}
			if job.Status == "applying" && at.Sub(job.UpdatedAt) >= selfConfigConvergenceTimeout {
				return s.recordRuntimeRefusal(ctx, binding, nodes, "convergence_timeout")
			}
			return nil
		}
		deploymentApplied = applied
	}
	// A prepared graph only belongs to its exact candidate. Aborted jobs and
	// superseded snapshots release their resources before another candidate.
	if s.prepared != nil && s.prepared.snapshotID != binding.DesiredSnapshotID &&
		(job.ID == "" || s.prepared.snapshotID != job.SnapshotID) {
		if err := s.closePrepared(); err != nil {
			return err
		}
	}
	active := s.active.Load()
	if binding.Suspended {
		s.active.Store(nil)
		if job.Status != "preparing" {
			return s.closePrepared()
		}
		active = &selfConfigActive{}
	} else if job.Status == "preparing" && (active == nil || active.snapshotID != binding.DesiredSnapshotID || active.generation != binding.Generation || active.incarnation != binding.Incarnation) {
		// A repair revision must be preparable even if the committed target
		// cannot install. Keep business work fenced and report only the graph
		// actually installed; preparation is not an acknowledgement of either
		// the failed target or the repair candidate.
		s.active.Store(nil)
		active = &selfConfigActive{}
	} else if active == nil || active.snapshotID != binding.DesiredSnapshotID || active.generation != binding.Generation || active.incarnation != binding.Incarnation {
		bundle, err := s.prepareRuntimeSnapshot(ctx, binding, binding.DesiredSnapshotID, binding.DesiredRevision)
		if err != nil {
			s.active.Store(nil)
			code := "invalid_config"
			if binding.SchemaVersion != runtimeconfig.SchemaVersion {
				code = "incompatible_schema"
			}
			return errors.Join(err, s.recordRuntimeRefusal(ctx, binding, nodes, code))
		}
		if bundle.BootstrapSources() != (config.ManagedBootstrapSources{}) {
			if !s.deploymentMatches(binding) {
				s.active.Store(nil)
				return ErrDeploymentSourcesPending
			}
			if err := s.Deployment.VerifyInstalled(ctx, bundle); err != nil {
				s.active.Store(nil)
				return err
			}
		}
		metadata, err := s.DB.Coordination().CurrentSelfConfigGeneration(ctx)
		if err != nil {
			return err
		}
		if metadata.Topology != nil && (metadata.Topology.After.NodeID != s.NodeID || metadata.Topology.After.HA != s.HAMode || s.Deployment == nil || s.Deployment.Identity().TemplateStamp != metadata.TopologyStamp || metadata.DeploymentRestoring) {
			s.active.Store(nil)
			return ErrDeploymentSourcesPending
		}
		if err := s.activateInstallation(ctx, binding.DesiredSnapshotID, binding.Incarnation, bundle); err != nil {
			s.active.Store(nil)
			return errors.Join(err, s.recordRuntimeRefusal(ctx, binding, nodes, "activation_failed"))
		}
		active = &selfConfigActive{bundle: bundle, owner: binding.OwnerInstanceID, incarnation: binding.Incarnation, snapshotID: binding.DesiredSnapshotID, generation: binding.Generation, revision: binding.DesiredRevision}
		s.active.Store(active)
		s.installed.Store(active)
	}
	var local store.SelfConfigNode
	for _, n := range nodes {
		if n.NodeID == s.NodeID {
			local = n
			break
		}
	}
	if local.NodeID == "" {
		if job.ID != "" {
			return nil
		} // Fixed membership excludes late-joining acknowledgements.
		local = store.SelfConfigNode{NodeID: s.NodeID, Incarnation: binding.Incarnation}
	}
	local.ActiveGeneration = active.generation
	local.ActiveRevision = active.revision
	local.SchemaVersion = runtimeconfig.SchemaVersion
	local.ErrorCode = ""
	if job.Status == "preparing" {
		local.Prepared = false
		if job.SchemaVersion != runtimeconfig.SchemaVersion {
			local.ErrorCode = "incompatible_schema"
		} else if candidate, err := s.prepareRuntimeSnapshot(ctx, binding, job.SnapshotID, job.Revision); err != nil {
			local.ErrorCode = "invalid_config"
		} else if err := validateSelfConfigParticipants(candidate, nodes); err != nil {
			local.ErrorCode = "invalid_config"
		} else if ready, err := s.prepareDeployment(ctx, binding, job, candidate); err != nil {
			local.ErrorCode = "preparation_failed"
		} else if !ready {
			// Deployment preparation is pending; no component acknowledgement.
		} else if err := s.prepareInstallation(ctx, job.SnapshotID, binding.Incarnation, candidate); err != nil {
			local.ErrorCode = "preparation_failed"
		} else if err := s.prepareOriginReview(ctx, binding, job, candidate); err != nil {
			local.ErrorCode = "preparation_failed"
		} else {
			local.Prepared = true
		}
	}
	at, err := s.runtimeTimestamp(ctx)
	if err != nil {
		return err
	}
	local.UpdatedAt = at
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		b, err := r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		if b.Generation != binding.Generation || b.Incarnation != binding.Incarnation || (b.Suspended && job.Status != "preparing") {
			return domain.ErrConflict
		}
		if err := r.SelfConfig().PutNode(ctx, p, local); err != nil {
			return err
		}
		if job.ID == "" {
			return nil
		}
		current, err := r.SelfConfig().Job(ctx, p, job.ID)
		if err != nil {
			return err
		}
		participants, err := r.SelfConfig().Nodes(ctx, p)
		if err != nil {
			return err
		}
		if current.Status == "preparing" {
			for _, n := range participants {
				if n.ErrorCode != "" {
					return s.finishRuntimeFailure(ctx, r, p, b, current, "aborted", n.ErrorCode, at)
				}
			}
			if at.Sub(current.CreatedAt) >= store.SelfConfigPreparationTTL {
				return s.finishRuntimeFailure(ctx, r, p, b, current, "aborted", "preparation_timeout", at)
			}
			return nil
		}
		if current.Status != "applying" && current.Status != "partial" {
			return nil
		}
		complete := deploymentApplied && len(participants) > 0
		for _, n := range participants {
			if n.ActiveGeneration != b.Generation || n.ActiveRevision != b.DesiredRevision || n.Incarnation != b.Incarnation || n.ErrorCode != "" {
				complete = false
			}
		}
		if complete {
			if err := r.SelfConfig().FinishJob(ctx, p, current.ID, "applied", "", at); err != nil {
				return err
			}
			ev, err := newAuditEvent(ctx, audit.EventSelfConfigApplied, "", audit.Object{Type: "environment", ID: b.EnvironmentID}, audit.OutcomeSuccess, current.ID, audit.Payload{"owner_instance_id": b.OwnerInstanceID, "revision": b.DesiredRevision, "generation": b.Generation, "job_id": current.ID, "node_id": s.NodeID})
			if err != nil {
				return err
			}
			return r.Audit().InsertTenant(ctx, p, ev)
		}
		if current.Status == "applying" && at.Sub(current.UpdatedAt) >= selfConfigConvergenceTimeout {
			return s.finishRuntimeFailure(ctx, r, p, b, current, "partial", "convergence_timeout", at)
		}
		return nil
	})
}

func (s *SelfConfig) finishRuntimeFailure(ctx context.Context, r store.Repos, p authz.Proof, b store.SelfConfigBinding, job store.SelfConfigJob, status, code string, at time.Time) error {
	if err := r.SelfConfig().FinishJob(ctx, p, job.ID, status, code, at); err != nil {
		return err
	}
	ev, err := newAuditEvent(ctx, audit.EventSelfConfigApplied, "", audit.Object{Type: "environment", ID: b.EnvironmentID}, audit.OutcomeFailure, job.ID, audit.Payload{"owner_instance_id": b.OwnerInstanceID, "revision": job.Revision, "generation": job.Generation, "job_id": job.ID, "error_code": code})
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, p, ev)
}

func (s *SelfConfig) recordRuntimeRefusal(ctx context.Context, b store.SelfConfigBinding, nodes []store.SelfConfigNode, code string) error {
	at, err := s.runtimeTimestamp(ctx)
	if err != nil {
		return err
	}
	node := store.SelfConfigNode{NodeID: s.NodeID, SchemaVersion: runtimeconfig.SchemaVersion, Incarnation: b.Incarnation, ErrorCode: code, UpdatedAt: at}
	for _, existing := range nodes {
		if existing.NodeID == s.NodeID {
			node.JobID = existing.JobID
			break
		}
	}
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
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
		if err := r.SelfConfig().PutNode(ctx, p, node); err != nil {
			return err
		}
		if node.JobID == "" {
			return nil
		}
		job, err := r.SelfConfig().Job(ctx, p, node.JobID)
		if err != nil {
			return err
		}
		// An explicit installation refusal is already partial convergence.
		// Record it here because the failed installation cannot reach the
		// successful reconciliation path's timeout handling.
		if job.Status == "applying" && job.Generation == current.Generation {
			return s.finishRuntimeFailure(ctx, r, p, current, job, "partial", code, at)
		}
		return nil
	})
}

func validateSelfConfigParticipants(bundle *runtimeconfig.Bundle, nodes []store.SelfConfigNode) error {
	if topology := bundle.BootstrapSources().Topology; topology.NodeID != "" {
		if len(nodes) != 1 {
			return domain.ErrConflict
		}
		return bundle.ValidateNodeMembership([]string{topology.NodeID})
	}
	if !bundle.HasNodeValues() {
		return nil
	}
	ids := make([]string, len(nodes))
	for i, node := range nodes {
		ids[i] = node.NodeID
	}
	return bundle.ValidateNodeMembership(ids)
}

func (s *SelfConfig) prepareRuntimeSnapshot(ctx context.Context, b store.SelfConfigBinding, snapshotID string, revision int64) (*runtimeconfig.Bundle, error) {
	if b.SchemaVersion != runtimeconfig.SchemaVersion {
		return nil, fmt.Errorf("%w: incompatible runtime schema", domain.ErrConflict)
	}
	sealer, err := s.Keyring.ForProject(ctx, b.OrgID, b.ProjectID)
	if err != nil {
		return nil, errors.New("runtime configuration cannot open its project key")
	}
	var bundle *runtimeconfig.Bundle
	err = tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, snapshotID)
		if err != nil {
			return err
		}
		current, err := r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		if current.OwnerInstanceID != b.OwnerInstanceID || current.ProjectID != b.ProjectID || current.Incarnation != b.Incarnation || current.SchemaVersion != runtimeconfig.SchemaVersion {
			return domain.ErrConflict
		}
		bundle, err = prepareSelfConfigSnapshot(ctx, r.Snapshots(), r.Catalogue(), p, sealer, revision)
		return err
	})
	return bundle, err
}

func prepareSelfConfigSnapshot(ctx context.Context, snapshots store.SnapshotReader, catalogue store.CatalogueReader, p authz.Proof, sealer *crypto.ProjectSealer, revision int64) (*runtimeconfig.Bundle, error) {
	keys, err := catalogue.List(ctx, p)
	if err != nil {
		return nil, err
	}
	expected := runtimeconfig.Catalogue()
	if len(keys) != len(expected) {
		return nil, invalidDetail("runtime configuration catalogue has changed")
	}
	byName := make(map[string]store.CatalogueKey, len(keys))
	for _, key := range keys {
		byName[key.Name] = key
	}
	for _, key := range expected {
		stored, ok := byName[key.Name]
		if !ok || stored.Classification != string(key.Classification) || stored.RequiredMode != string(schema.PresenceNone) || stored.ForbiddenMode != string(schema.PresenceNone) || stored.GroupID != "" || stored.FolderPath != "" {
			return nil, invalidDetail("runtime configuration catalogue has changed")
		}
		compiled, err := schema.CompileClassified(key.Classification, key.Declaration)
		if err != nil {
			return nil, err
		}
		canonical, err := compiled.Canonical()
		if err != nil {
			return nil, err
		}
		if string(canonical) != stored.Declaration {
			return nil, invalidDetail("runtime configuration schema has changed")
		}
	}
	snapshot, err := snapshots.AtRevision(ctx, p, revision)
	if err != nil {
		return nil, err
	}
	if !snapshot.PayloadPresent() {
		return nil, collectedRevisionError(snapshot)
	}
	entries, err := snapshots.Entries(ctx, p, snapshot)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, ok := byName[entry.KeyName]
		if !ok || key.ID != entry.KeyID || key.Classification != entry.Classification {
			return nil, invalidDetail("runtime snapshot does not match its catalogue")
		}
		if _, duplicate := values[entry.KeyName]; duplicate {
			return nil, invalidDetail("runtime snapshot contains duplicate keys")
		}
		plain, err := sealer.OpenField(snapshotAAD(entry.OrgID, entry.ProjectID, entry.EnvironmentID, entry.KeyID, entry.SnapshotID, entry.ID), entry.Ciphertext)
		if err != nil {
			return nil, errors.New("runtime configuration cannot decrypt a snapshot value")
		}
		values[entry.KeyName] = string(plain)
		crypto.Zero(plain)
	}
	return runtimeconfig.Prepare(values)
}
