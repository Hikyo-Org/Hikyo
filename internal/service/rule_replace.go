package service

import (
	"context"
	"fmt"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// ReplaceRulesSpec is one atomic change to a person's access card. Rule IDs
// are immutable, so a missing ID refuses a stale edit rather than reapplying it.
type ReplaceRulesSpec struct {
	Org    domain.OrgID
	Target domain.PrincipalID
	Revoke []string
	Create []RuleSpec
}

// Replace authorizes the entire batch against the original state. No mutation
// or session invalidation happens until every authorization has succeeded.
func (s *Rules) Replace(ctx context.Context, actor Actor, spec ReplaceRulesSpec) ([]RuleView, error) {
	if len(spec.Revoke)+len(spec.Create) == 0 || len(spec.Revoke) > 32 || len(spec.Create) > 32 {
		return nil, fmt.Errorf("%w: rule replacement requires 1 to 32 additions or removals", domain.ErrInvalid)
	}
	for i, id := range spec.Revoke {
		if id == "" || slices.Contains(spec.Revoke[:i], id) {
			return nil, fmt.Errorf("%w: duplicate or empty removal", domain.ErrInvalid)
		}
	}
	for _, add := range spec.Create {
		if add.Target != spec.Target || add.Org != spec.Org {
			return nil, fmt.Errorf("%w: replacement must name one person and organisation", domain.ErrInvalid)
		}
	}
	var out []RuleView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		type removal struct {
			stored authz.StoredRule
			caller authz.Identity
			proof  authz.Proof
		}
		type addition struct {
			rule   domain.Rule
			caller authz.Identity
			proof  authz.Proof
			unheld bool
		}
		removes := make([]removal, 0, len(spec.Revoke))
		adds := make([]addition, 0, len(spec.Create))
		for _, id := range spec.Revoke {
			stored, caller, proof, err := prepareRuleRevoke(ctx, az, actor, spec.Org, id, now)
			if err != nil {
				return err
			}
			if stored.Rule.Principal != spec.Target {
				return ErrNoSuchRule
			}
			removes = append(removes, removal{stored, caller, proof})
		}
		for _, add := range spec.Create {
			rule, caller, proof, unheld, err := prepareRuleCreate(ctx, az, actor, add, now)
			if err != nil {
				return err
			}
			adds = append(adds, addition{rule, caller, proof, unheld})
		}
		existing, err := az.RuleIDsForOrg(ctx, spec.Org)
		if err != nil {
			return err
		}
		if len(existing)-len(removes)+len(adds) > MaxRulesPerOrg {
			return fmt.Errorf("%w: an organization holds at most %d rules", domain.ErrLimitExceeded, MaxRulesPerOrg)
		}
		name, err := newPrincipalNames().get(ctx, az, spec.Target)
		if err != nil {
			return err
		}
		// Prepared proofs remain valid for this transaction. In particular, self
		// changes cannot cancel authorization of later rows in the same card.
		for _, remove := range removes {
			if err := revokeRule(ctx, r, az, remove.proof, remove.caller.Principal, remove.stored, "revoked"); err != nil {
				return err
			}
		}
		out = make([]RuleView, 0, len(adds))
		for _, add := range adds {
			id, err := newID("rul")
			if err != nil {
				return err
			}
			add.rule.ID = id
			if err := az.CreateRule(ctx, add.rule, add.caller.Principal, now, func() (string, error) { return newID("rli") }); err != nil {
				return err
			}
			payload := rulePayload(add.rule)
			payload["unheld"] = add.unheld
			event, err := domainEvent(ctx, audit.EventRuleCreated, add.caller.Principal, audit.Object{Type: "rule", ID: id}, payload)
			if err != nil {
				return err
			}
			if err := r.Audit().InsertTenant(ctx, add.proof, event); err != nil {
				return err
			}
			out = append(out, RuleView{ID: id, Principal: spec.Target, PrincipalName: name, Capability: add.rule.Capability, Org: spec.Org, Where: add.rule.Where, CreatedBy: add.caller.Principal, CreatedAt: now})
		}
		if len(adds) > 0 {
			return invalidateGrantChange(ctx, az, spec.Target)
		}
		return nil
	})
	return out, err
}
