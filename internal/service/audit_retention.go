package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// sweepAudit completes all eligible batches before the shared pruner health
// can advance. No rows are removed unless their receipt commits atomically.
func (s *Retention) sweepAudit(ctx context.Context, now time.Time) error {
	policy := s.AuditPolicy
	if policy == (store.AuditRetentionPolicy{}) {
		policy = store.AuditRetentionPolicy{AccessDays: 90, SecurityDays: 365}
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	// Persist configuration separately, before deletion. This records initial
	// bounded-policy adoption as well as subsequent operator changes.
	if err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
		if err != nil {
			return err
		}
		before, err := r.Retention().AuditPolicy(ctx, p)
		if err != nil {
			return err
		}
		if before == policy {
			return nil
		}
		if err := r.Retention().SetAuditPolicy(ctx, p, policy); err != nil {
			return err
		}
		ev, err := newAuditEvent(ctx, audit.EventAuditRetentionChanged, "", audit.Object{Type: "audit_retention", ID: "instance"}, audit.OutcomeSuccess, "", audit.Payload{
			"previous_access_days": before.AccessDays, "previous_security_days": before.SecurityDays,
			"access_days": policy.AccessDays, "security_days": policy.SecurityDays,
		})
		if err != nil {
			return err
		}
		ev.Actor.Class = audit.ActorSystem
		return r.Audit().InsertInstance(ctx, p, ev)
	}); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, err := tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (int, error) {
			p, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
			if err != nil {
				return 0, err
			}
			current, err := r.Retention().AuditPolicy(ctx, p)
			if err != nil {
				return 0, err
			}
			if current != policy {
				return 0, fmt.Errorf("audit retention configuration changed during sweep")
			}
			rows, err := r.Retention().PruneAudit(ctx, p, now.Add(-time.Duration(policy.AccessDays)*24*time.Hour), now.Add(-time.Duration(policy.SecurityDays)*24*time.Hour))
			if err != nil {
				return 0, err
			}
			type receipt struct {
				trail, category string
				count           int
				from, through   time.Time
			}
			receipts := map[string]*receipt{}
			for _, row := range rows {
				category, _, _ := strings.Cut(row.Type, ".")
				key := row.Trail + "/" + category
				rec := receipts[key]
				if rec == nil {
					rec = &receipt{trail: row.Trail, category: category, from: row.RecordedAt, through: row.RecordedAt}
					receipts[key] = rec
				}
				rec.count++
				if row.RecordedAt.Before(rec.from) {
					rec.from = row.RecordedAt
				}
				if row.RecordedAt.After(rec.through) {
					rec.through = row.RecordedAt
				}
			}
			keys := make([]string, 0, len(receipts))
			for key := range receipts {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				rec := receipts[key]
				ev, err := newAuditEvent(ctx, audit.EventAuditRetentionPruned, "", audit.Object{Type: "audit_retention", ID: "instance"}, audit.OutcomeSuccess, "", audit.Payload{
					"trail": rec.trail, "category": rec.category, "deleted": rec.count,
					"from_time": audit.FormatTime(rec.from), "through_time": audit.FormatTime(rec.through),
					"access_days": policy.AccessDays, "security_days": policy.SecurityDays,
				})
				if err != nil {
					return 0, err
				}
				ev.Actor.Class = audit.ActorSystem
				if err := r.Audit().InsertInstance(ctx, p, ev); err != nil {
					return 0, err
				}
			}
			return len(rows), nil
		})
		if err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
	}
}
