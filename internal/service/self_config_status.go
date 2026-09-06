package service

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

type SelfConfigBindingView struct {
	OrgID, ProjectID, EnvironmentID string
	SchemaVersion                   int
}
type SelfConfigNodeView struct {
	NodeID           string
	ActiveGeneration int64
	ActiveRevision   *int64
	State            string
	UpdatedAt        time.Time
	Error            string
}
type SelfConfigJobView struct {
	ID, State                                    string
	Revision, Generation                         int64
	CreatedAt                                    time.Time
	CompletedAt                                  *time.Time
	Error                                        string
	PlanDigest                                   string
	Prepared                                     bool
	DeploymentRestorePending, DeploymentRestored bool
}
type SelfConfigStatus struct {
	OwnerInstanceID                 string
	Managed                         bool
	Binding                         *SelfConfigBindingView
	Generation                      int64
	DesiredRevision, LatestRevision *int64
	State                           string
	Nodes                           []SelfConfigNodeView
	Job                             *SelfConfigJobView
}

func selfConfigScope(b store.SelfConfigBinding) domain.Scope {
	return domain.Scope{Org: domain.OrgID(b.OrgID), Project: domain.ProjectID(b.ProjectID), Env: domain.EnvID(b.EnvironmentID)}
}

func (s *SelfConfig) Status(ctx context.Context, actor Actor) (SelfConfigStatus, error) {
	return s.status(ctx, actor, "")
}

func selfConfigJobView(j store.SelfConfigJob) *SelfConfigJobView {
	state := map[string]string{"preparing": "preparing", "applying": "pending", "partial": "partial", "applied": "completed", "aborted": "failed", "superseded": "failed"}[j.Status]
	view := &SelfConfigJobView{ID: j.ID, State: state, Revision: j.Revision, Generation: j.Generation, CreatedAt: j.CreatedAt, Error: j.ErrorCode}
	if j.Status == "applied" || j.Status == "aborted" || j.Status == "superseded" {
		completed := j.UpdatedAt
		view.CompletedAt = &completed
	}
	return view
}

func (s *SelfConfig) status(ctx context.Context, actor Actor, jobID string) (SelfConfigStatus, error) {
	var out SelfConfigStatus
	at, err := s.runtimeTimestamp(ctx)
	if err != nil {
		return out, err
	}
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpSelfConfigStatus, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		owner, err := az.InstanceIdentity(ctx)
		if err != nil {
			return err
		}
		out = SelfConfigStatus{OwnerInstanceID: owner, State: "unmanaged", Nodes: []SelfConfigNodeView{}}
		b, err := r.SelfConfig().Binding(ctx, p)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if err == nil {
			liveOwner, liveIncarnation, err := s.DB.RecoveryIdentity()
			if err != nil {
				return err
			}
			if liveOwner != b.OwnerInstanceID || liveIncarnation != b.Incarnation {
				b.Suspended = true
			}
			readProof, err := az.Authorize(ctx, caller, authz.OpRevisionList, selfConfigScope(b))
			if err != nil {
				return err
			}
			latest, err := r.Snapshots().ListPage(ctx, readProof, math.MaxInt64, 1)
			if err != nil {
				return err
			}
			nodes, err := r.SelfConfig().Nodes(ctx, p)
			if err != nil {
				return err
			}
			jobs, err := r.SelfConfig().Jobs(ctx, p)
			if err != nil {
				return err
			}
			out.Managed = true
			out.Generation = b.Generation
			out.DesiredRevision = &b.DesiredRevision
			out.State = "active"
			out.Binding = &SelfConfigBindingView{OrgID: b.OrgID, ProjectID: b.ProjectID, EnvironmentID: b.EnvironmentID, SchemaVersion: int(b.SchemaVersion)}
			if len(latest) > 0 {
				out.LatestRevision = &latest[0].Revision
			}
			for _, n := range nodes {
				view := SelfConfigNodeView{NodeID: n.NodeID, ActiveGeneration: n.ActiveGeneration, UpdatedAt: n.UpdatedAt, Error: n.ErrorCode, State: "pending"}
				// A failed candidate did not change the durable target. Its error
				// belongs to the failed job while the previously active node stays
				// usable; do not label that old bundle as an activation failure.
				if len(jobs) > 0 && jobs[0].Status == "aborted" && n.JobID == jobs[0].ID && n.ActiveGeneration == b.Generation && n.ActiveRevision == b.DesiredRevision {
					view.Error = ""
				}
				if n.ActiveRevision > 0 {
					revision := n.ActiveRevision
					view.ActiveRevision = &revision
				}
				switch {
				case b.Suspended || n.Incarnation != b.Incarnation:
					view.State = "fenced"
				case at.Sub(n.UpdatedAt) > 30*time.Second:
					view.State = "unknown"
				case n.ActiveGeneration == b.Generation && n.ActiveRevision == b.DesiredRevision && view.Error == "":
					view.State = "active"
				}
				if view.State != "active" {
					out.State = "pending"
				}
				out.Nodes = append(out.Nodes, view)
			}
			if len(nodes) == 0 {
				out.State = "pending"
			}
			if len(jobs) > 0 {
				j := jobs[0]
				out.Job = selfConfigJobView(j)
				if j.Status == "preparing" || j.Status == "applying" {
					out.State = "pending"
				}
				if j.Status == "partial" {
					out.State = "partial"
				}
			}
			if jobID != "" {
				requested, err := r.SelfConfig().Job(ctx, p, jobID)
				if err != nil {
					return err
				}
				out.Job = selfConfigJobView(requested)
			}
			if b.Suspended {
				out.State = "recovery_required"
			}
			if out.Job != nil {
				out.Job.Prepared = out.Job.State == "preparing" && len(nodes) > 0 && at.Sub(out.Job.CreatedAt) < store.SelfConfigPreparationTTL
				for _, node := range nodes {
					if node.JobID != out.Job.ID || !node.Prepared || node.ErrorCode != "" || node.SchemaVersion != b.SchemaVersion || node.Incarnation != b.Incarnation || node.UpdatedAt.After(at) || at.Sub(node.UpdatedAt) >= 30*time.Second {
						out.Job.Prepared = false
					}
				}
				rollout, err := r.SelfConfig().Rollout(ctx, p, out.Job.ID)
				if err == nil {
					out.Job.DeploymentRestored = rollout.ExternalPhase == "restored"
					if command, err := decodeRolloutCommand(rollout.CommandJSON); err == nil {
						out.Job.DeploymentRestorePending = command.Command.Action == "restore" && !out.Job.DeploymentRestored
					}
					if rollout.Incarnation == b.Incarnation {
						out.Job.PlanDigest = rollout.PlanDigest
					}
					out.Job.Prepared = out.Job.Prepared && out.Job.PlanDigest != ""
				} else if !errors.Is(err, domain.ErrNotFound) {
					return err
				}
			}
		}
		ev, err := newAuditEvent(ctx, audit.EventSelfConfigStatusRead, caller.Principal, audit.Object{Type: "instance", ID: owner}, audit.OutcomeSuccess, "", audit.Payload{"owner_instance_id": owner, "generation": out.Generation})
		if err != nil {
			return err
		}
		return r.Audit().InsertInstance(ctx, p, ev)
	})
	return out, err
}
