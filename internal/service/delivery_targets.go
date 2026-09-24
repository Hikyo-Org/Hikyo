package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/deliverytarget"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Delivery-target condition reporting (#788, k8s-condition-reporting ADR).
//
// The report and tombstone ride the delivery surface because they ride its
// credential: the SAME workload credential (bearer or federated) that fetches,
// resolved through the same pre-transaction artifact branch. They are not
// operator-special; any integration presenting a machine credential holding
// `report-delivery-status` on the environment may report.
//
// Refusal placement is the whole design, so it is stated once here:
//
//   - Authorization (a principal without the atom, another tenant, a removed
//     grant) is the chokepoint's uniform nonexistent answer and its own
//     recorded denial. It never touches a row.
//   - A revoked or expired credential is the chokepoint's 401.
//   - Size (413) is decided before the body is parsed, so it is never tied to
//     a row: RefuseOversizeReport authorizes and audits without a key.
//   - Vocabulary (422) refuses the whole report; the only thing stored is the
//     closed cause on an EXISTING row.
//   - Ordering (409) is audited and dropped, never recorded on the row.
//   - Quota (named 409) records the principal's notice; there is no row.
//
// Every in-transaction refusal COMMITS its audit event (and its closed
// row/notice mark) and is returned only after the transaction ends: returned
// from inside, the rollback would take the record with it.

// ErrReportVocabulary is the whole-report vocabulary refusal (422). The
// wrapping error's SafeDetail names the refused JSON member, never its value.
var ErrReportVocabulary = errors.New("service: the report carries a value outside the advertised vocabulary")

// ErrReportTooLarge is the size refusal (413).
var ErrReportTooLarge = fmt.Errorf("service: a delivery-target report is at most %d bytes", deliverytarget.MaxReportBytes)

// deliveryTargetPurgeBatch is SelectExpiredDeliveryTargetReports' LIMIT: a
// shorter batch means the backlog is drained.
const deliveryTargetPurgeBatch = 100

// DeliveryTargetKey names one target of the caller's own: the principal is
// always the caller, never the body.
type DeliveryTargetKey struct {
	ClusterID   string
	InstanceUID string
	UID         string
}

// DeliveryTargetList is one environment's delivery-target view (ADR D2, D5):
// the server-observed layer per reporting principal and the controller-
// reported rows, never merged into one health bit.
type DeliveryTargetList struct {
	Reporters []DeliveryTargetReporter
	Targets   []DeliveryTarget
}

// DeliveryTargetReporter is the server-observed layer for one principal that
// owns rows here. A zero time is "never".
type DeliveryTargetReporter struct {
	PrincipalID string
	// LastContactAt is the last identity.delivery_fetched in this environment.
	LastContactAt time.Time
	// QuotaRefusedAt is the closed quota-refused notice.
	QuotaRefusedAt time.Time
}

// DeliveryTarget is one row with its read-time derived state.
type DeliveryTarget struct {
	ID          string
	PrincipalID string
	Report      deliverytarget.Report
	ReceivedAt  time.Time
	State       string
	// RefusalCause and RefusedAt are set only while State is `refused`.
	RefusalCause string
	RefusedAt    time.Time
}

// ReportTarget authenticates the presented artifact and applies one report.
func (s *Delivery) ReportTarget(ctx context.Context, presented string, scope domain.Scope, report deliverytarget.Report) error {
	actor, err := s.callerActor(ctx, presented)
	if err != nil {
		return err
	}
	return s.ReportTargetAs(ctx, actor, scope, report)
}

// ReportTargetAs upserts the caller's row for one target, or refuses.
func (s *Delivery) ReportTargetAs(ctx context.Context, actor Actor, scope domain.Scope, report deliverytarget.Report) error {
	// Grammar is decided before authorization and discloses nothing.
	if err := deliverytarget.CheckShape(report); err != nil {
		return invalidDetail("%s", err)
	}
	var (
		charged bool
		refusal error
	)
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		refusal = nil
		now := s.now()
		caller, p, err := authorize(ctx, az, actor, authz.OpDeliveryTargetReport, scope, now)
		if err != nil {
			return err
		}
		if err := s.Budget.chargeOnce(&charged, budgetDeliveryTarget, budgetKeys{Principal: caller.Principal, Org: scope.Org}); err != nil {
			return err
		}
		refuse := func(cause, field, targetID string, err error) error {
			refusal = err
			return insertTargetRefusal(ctx, r, p, caller, scope, cause, field, targetID)
		}
		key := store.DeliveryTargetKey{
			PrincipalID: string(caller.Principal), ClusterID: report.Target.ClusterID,
			InstanceUID: report.Target.InstanceUID, TargetUID: report.Target.UID,
		}
		existing, err := r.DeliveryTargets().Get(ctx, p, key)
		found := err == nil
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}

		var vocab *deliverytarget.VocabularyError
		if err := deliverytarget.CheckVocabulary(report); errors.As(err, &vocab) {
			targetID := ""
			if found {
				targetID = existing.ID
				if _, err := r.DeliveryTargets().RecordRefusal(ctx, p, existing.ID, deliverytarget.RefusalVocabulary, now); err != nil {
					return err
				}
			}
			return refuse(deliverytarget.RefusalVocabulary, vocab.Field, targetID,
				&detailErr{detail: vocab.Field, err: fmt.Errorf("%w: %s", ErrReportVocabulary, vocab.Field)})
		} else if err != nil {
			return err
		}

		if deliverytarget.InFuture(report.ReportedAt, now) {
			return refuse(deliverytarget.RefusalOrdering, "", existing.ID,
				conflictDetail("reported_at is more than %s in the future", deliverytarget.MaxFutureSkew))
		}
		row := store.DeliveryTargetReport{
			PrincipalID: string(caller.Principal), Target: report.Target, Vocabulary: report.Vocabulary,
			Generation: report.Generation, ObservedGeneration: report.ObservedGeneration,
			ReportedAt: report.ReportedAt, ReceivedAt: now,
			ReportIntervalSeconds: int64(deliverytarget.ClampInterval(report.ReportIntervalSeconds) / time.Second),
			Lifecycle:             report.Lifecycle, Conditions: report.Conditions,
			Reporter: report.Reporter, ReporterVersion: report.ReporterVersion,
		}
		if found {
			switch deliverytarget.Order(existing.ObservedGeneration, existing.ReportedAt, report.ObservedGeneration, report.ReportedAt) {
			case deliverytarget.OlderGeneration:
				return refuse(deliverytarget.RefusalOrdering, "", existing.ID,
					conflictDetail("observed_generation is lower than the accepted report's"))
			case deliverytarget.NotLater:
				return refuse(deliverytarget.RefusalOrdering, "", existing.ID,
					conflictDetail("reported_at is not later than the accepted report's in the same generation"))
			}
			// An accepted repeat report emits nothing (ADR D8).
			row.ID = existing.ID
			updated, err := r.DeliveryTargets().Update(ctx, p, row)
			if err == nil && !updated {
				err = fmt.Errorf("service: delivery target %s vanished inside its own transaction", existing.ID)
			}
			return err
		}

		count, err := r.DeliveryTargets().CountForPrincipal(ctx, p, string(caller.Principal))
		if err != nil {
			return err
		}
		if count >= deliverytarget.MaxRowsPerPrincipal {
			if err := r.DeliveryTargets().RecordQuotaRefusal(ctx, p, string(caller.Principal), now); err != nil {
				return err
			}
			return refuse(deliverytarget.RefusalQuota, "", "",
				fmt.Errorf("%w: a service account reports at most %d delivery targets", domain.ErrLimitExceeded, deliverytarget.MaxRowsPerPrincipal))
		}
		if row.ID, err = newID("dtr"); err != nil {
			return err
		}
		row.CreatedAt = now
		if err := r.DeliveryTargets().Insert(ctx, p, row); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventDeliveryTargetCreated, caller.Principal,
			audit.Object{Type: "delivery-target", ID: row.ID}, audit.Payload{
				"target_id": row.ID, "cluster_id": row.Target.ClusterID, "instance_uid": row.Target.InstanceUID,
				"target_uid": row.Target.UID, "namespace": row.Target.Namespace, "name": row.Target.Name,
				"reporter": row.Reporter, "vocabulary": row.Vocabulary,
				"credential_id": caller.CredentialID, "scope": renderScope(scope),
			})
		if err != nil {
			return err
		}
		ev.Actor.CredentialID = caller.CredentialID
		return r.Audit().InsertTenant(ctx, p, ev)
	})
	if err != nil {
		return s.recordUnbound(ctx, actor, err)
	}
	return refusal
}

// TombstoneTarget authenticates the presented artifact and deletes one row.
func (s *Delivery) TombstoneTarget(ctx context.Context, presented string, scope domain.Scope, key DeliveryTargetKey) error {
	actor, err := s.callerActor(ctx, presented)
	if err != nil {
		return err
	}
	return s.TombstoneTargetAs(ctx, actor, scope, key)
}

// TombstoneTargetAs deletes the caller's row for one target after its
// tombstone event (ADR D6). A tombstone naming no row of the caller's is the
// uniform nonexistent answer.
func (s *Delivery) TombstoneTargetAs(ctx context.Context, actor Actor, scope domain.Scope, key DeliveryTargetKey) error {
	if err := deliverytarget.CheckTarget(deliverytarget.Target{
		ClusterID: key.ClusterID, InstanceUID: key.InstanceUID, UID: key.UID,
		// The display labels are not part of the key; any grammatical value
		// passes the shared check.
		Namespace: "tombstone", Name: "tombstone",
	}); err != nil {
		return invalidDetail("%s", err)
	}
	var charged bool
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpDeliveryTargetTombstone, scope, s.now())
		if err != nil {
			return err
		}
		if err := s.Budget.chargeOnce(&charged, budgetDeliveryTarget, budgetKeys{Principal: caller.Principal, Org: scope.Org}); err != nil {
			return err
		}
		existing, err := r.DeliveryTargets().Get(ctx, p, store.DeliveryTargetKey{
			PrincipalID: string(caller.Principal), ClusterID: key.ClusterID, InstanceUID: key.InstanceUID, TargetUID: key.UID,
		})
		if errors.Is(err, store.ErrNotFound) {
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventDeliveryTargetTombstoned, caller.Principal,
			audit.Object{Type: "delivery-target", ID: existing.ID}, audit.Payload{
				"target_id": existing.ID, "credential_id": caller.CredentialID, "scope": renderScope(scope),
			})
		if err != nil {
			return err
		}
		ev.Actor.CredentialID = caller.CredentialID
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return err
		}
		deleted, err := r.DeliveryTargets().Delete(ctx, p, existing.ID)
		if err == nil && !deleted {
			err = fmt.Errorf("service: delivery target %s vanished inside its own transaction", existing.ID)
		}
		return err
	})
	if err != nil {
		return s.recordUnbound(ctx, actor, err)
	}
	return nil
}

// RefuseOversizeReport is the size refusal (413). The transport calls it
// INSTEAD of parsing a body over deliverytarget.MaxReportBytes: the caller is
// authenticated and authorized like any report, so an unauthorized caller
// still gets the uniform nonexistent answer, and the refusal is audited under
// no row, because the server never reads a key out of an over-size body.
func (s *Delivery) RefuseOversizeReport(ctx context.Context, presented string, scope domain.Scope) error {
	actor, err := s.callerActor(ctx, presented)
	if err != nil {
		return err
	}
	var charged bool
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpDeliveryTargetReport, scope, s.now())
		if err != nil {
			return err
		}
		if err := s.Budget.chargeOnce(&charged, budgetDeliveryTarget, budgetKeys{Principal: caller.Principal, Org: scope.Org}); err != nil {
			return err
		}
		return insertTargetRefusal(ctx, r, p, caller, scope, deliverytarget.RefusalSize, "", "")
	})
	if err != nil {
		return s.recordUnbound(ctx, actor, err)
	}
	return ErrReportTooLarge
}

// ListTargets is the human view of one environment (ADR D7: `read` on it).
// Rows of an environment the caller cannot read are absent: the whole list is
// the uniform nonexistent answer.
func (s *Delivery) ListTargets(ctx context.Context, actor Actor, scope domain.Scope) (DeliveryTargetList, error) {
	var out DeliveryTargetList
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		out = DeliveryTargetList{Reporters: []DeliveryTargetReporter{}, Targets: []DeliveryTarget{}}
		now := s.now()
		_, p, err := authorize(ctx, az, actor, authz.OpDeliveryTargetList, scope, now)
		if err != nil {
			return err
		}
		rows, err := r.DeliveryTargets().List(ctx, p)
		if err != nil {
			return err
		}
		// Reporter liveness is per principal, so it is evaluated once each.
		live := map[string]bool{}
		for _, row := range rows {
			if _, seen := live[row.PrincipalID]; !seen {
				ok, err := az.DeliveryReporterLive(ctx, domain.PrincipalID(row.PrincipalID), scope, now)
				if err != nil {
					return err
				}
				live[row.PrincipalID] = ok
				reporter := DeliveryTargetReporter{PrincipalID: row.PrincipalID}
				if reporter.LastContactAt, _, err = r.DeliveryTargets().LastFetchAt(ctx, p, row.PrincipalID); err != nil {
					return err
				}
				if reporter.QuotaRefusedAt, _, err = r.DeliveryTargets().QuotaNotice(ctx, p, row.PrincipalID); err != nil {
					return err
				}
				out.Reporters = append(out.Reporters, reporter)
			}
			target := DeliveryTarget{
				ID: row.ID, PrincipalID: row.PrincipalID, ReceivedAt: row.ReceivedAt,
				Report: deliverytarget.Report{
					Vocabulary: row.Vocabulary, Target: row.Target, Generation: row.Generation,
					ObservedGeneration: row.ObservedGeneration, ReportedAt: row.ReportedAt,
					ReportIntervalSeconds: row.ReportIntervalSeconds, Lifecycle: row.Lifecycle,
					Conditions: row.Conditions, Reporter: row.Reporter, ReporterVersion: row.ReporterVersion,
				},
				State: deliverytarget.Derive(deliverytarget.Row{
					ReceivedAt:     row.ReceivedAt,
					ReportInterval: time.Duration(row.ReportIntervalSeconds) * time.Second,
					RefusedAt:      row.RefusedAt,
				}, live[row.PrincipalID], now),
			}
			if target.State == deliverytarget.StateRefused {
				target.RefusalCause, target.RefusedAt = row.RefusalCause, row.RefusedAt
			}
			out.Targets = append(out.Targets, target)
		}
		return nil
	})
	if err != nil {
		return DeliveryTargetList{}, err
	}
	return out, nil
}

// PurgeExpiredTargets is the hourly 30-day purge (ADR D6), across every
// tenant: rows with no accepted report for 30 days, one committed batch at a
// time until the backlog is drained. Each purge emits its tenant-trail event
// under scoped scheduler authority, in the transaction that deletes the row.
func (s *Delivery) PurgeExpiredTargets(ctx context.Context) error {
	for {
		selected, err := s.purgeTargetBatch(ctx)
		if err != nil || selected < deliveryTargetPurgeBatch {
			return err
		}
	}
}

func (s *Delivery) purgeTargetBatch(ctx context.Context) (int, error) {
	now := store.CanonTime(s.now())
	cutoff := now.Add(-deliverytarget.PurgeAfter)
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (int, error) {
		p, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
		if err != nil {
			return 0, err
		}
		due, err := r.DeliveryTargets().SelectExpired(ctx, p, cutoff)
		if err != nil {
			return 0, err
		}
		for _, row := range due {
			purged, err := r.DeliveryTargets().Purge(ctx, p, row.ID, cutoff)
			if err != nil {
				return 0, err
			}
			if !purged {
				continue // a report was accepted since the select
			}
			scoped, err := az.ScopedSystemAuthority(ctx, authz.SiteScheduler, domain.Scope{
				Org: domain.OrgID(row.OrgID), Project: domain.ProjectID(row.ProjectID), Env: domain.EnvID(row.EnvironmentID),
			})
			if err != nil {
				return 0, err
			}
			ev, err := domainEvent(ctx, audit.EventDeliveryTargetPurged, "",
				audit.Object{Type: "delivery-target", ID: row.ID}, audit.Payload{
					"target_id": row.ID, "principal_id": row.PrincipalID,
					"last_received_at": audit.FormatTime(row.ReceivedAt),
				})
			if err != nil {
				return 0, err
			}
			ev.Actor.Class = audit.ActorSystem
			ev.OccurredAt = now
			if err := r.Audit().InsertTenant(ctx, scoped, ev); err != nil {
				return 0, err
			}
		}
		return len(due), nil
	})
}

// insertTargetRefusal records one refused report (ADR D8) under the closed
// cause. targetID is set only when the refusal is tied to an existing row.
func insertTargetRefusal(ctx context.Context, r store.Repos, p authz.Proof, caller authz.Identity, scope domain.Scope, cause, field, targetID string) error {
	payload := audit.Payload{"cause": cause, "credential_id": caller.CredentialID, "scope": renderScope(scope)}
	if field != "" {
		payload["field"] = field
	}
	obj := audit.Object{Type: "environment", ID: string(scope.Env)}
	if targetID != "" {
		payload["target_id"] = targetID
		obj = audit.Object{Type: "delivery-target", ID: targetID}
	}
	ev, err := newAuditEvent(ctx, audit.EventDeliveryTargetRefused, caller.Principal, obj, audit.OutcomeDenied, "", payload)
	if err != nil {
		return err
	}
	ev.Actor.CredentialID = caller.CredentialID
	return r.Audit().InsertTenant(ctx, p, ev)
}

// conflictDetail is a domain.ErrConflict whose message names the ordering
// rule the report broke. It is decided after authorization, about the
// caller's own row.
func conflictDetail(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return &detailErr{detail: msg, err: fmt.Errorf("%w: %s", domain.ErrConflict, msg)}
}
