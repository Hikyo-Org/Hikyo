package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Incident ledger service (incident-containment ADR, O5 option B; #1039 S2).
//
// Opening an incident enumerates what an incident leaves behind and writes it
// as one ledger. Resume re-reads the ledger and re-runs only the open items
// through their registered owners. The ledger orders and records; it never
// revokes anything itself, and it never writes `done` unless the owner of the
// object confirmed. Authorization is recomputed in every transaction: nothing
// the caller was allowed a moment ago is cached.

// ErrIncidentOpenItems refuses to close an incident that still has open items
// unless the caller asks for the explicit residual status.
var ErrIncidentOpenItems = fmt.Errorf("%w: incident still has open items; close with residuals to list them", domain.ErrConflict)

// IncidentOwnerResult is what an owner function reports for one item. The zero
// value is the safe one: unconfirmed never becomes done.
type IncidentOwnerResult int

const (
	// OwnerUnconfirmed means the owner could not confirm the object reached its
	// target state. The item becomes unknown.
	OwnerUnconfirmed IncidentOwnerResult = iota
	// OwnerConfirmed means the owner confirmed the object is in its target
	// state. Only this result writes done.
	OwnerConfirmed
	// OwnerFailed means the owner reported a definite failure. The item stays
	// pending and the failure is recorded in the trail.
	OwnerFailed
	// OwnerBlocked means the caller lacks the owner's capability. The item is
	// blocked: another operator must act, and nothing is performed on the
	// caller's behalf.
	OwnerBlocked
)

// IncidentOwner runs one owner function for one item. It authorizes under its
// own capability and must be idempotent: resume calls it again for an item
// whose earlier outcome never reached the ledger.
type IncidentOwner func(ctx context.Context, actor Actor, item authz.IncidentItem) (IncidentOwnerResult, error)

// IncidentTarget names what an incident is opened on.
type IncidentTarget struct {
	Kind domain.IncidentScopeKind
	// Principal is the account for an account incident.
	Principal domain.PrincipalID
	// Scope carries the org for an org incident, org and project for a project
	// incident, and the same for an adapter incident.
	Scope     domain.Scope
	AdapterID string
}

// IncidentView is an incident with its outcome counts.
type IncidentView struct {
	Incident authz.Incident
	Counts   map[domain.ItemOutcome]int64
	// Reopened is true when the scope already had an open incident, so the
	// enumeration ran again and added only the objects the ledger lacked.
	Reopened bool
	Added    int
}

// ResumeResult counts what one resume pass did.
type ResumeResult struct {
	Examined    int
	Confirmed   int
	Unconfirmed int
	Failed      int
	Blocked     int
	// Stale counts items whose owner result could not be recorded because the
	// ledger row moved; the item keeps its recorded state.
	Stale int
	// Skipped counts open items whose owner function is not registered.
	Skipped int
}

// Incidents owns the incident ledger service.
type Incidents struct {
	DB *store.DB
	// Adapters, Dynamic and SCIM are the existing tenant readers the
	// enumeration reuses. Each authorizes under its own capability in its own
	// transaction, so an object its capability does not cover is recorded as
	// blocked rather than read on the caller's behalf.
	Adapters *Adapters
	Dynamic  *Dynamic
	SCIM     *SCIM
	// Owners maps an owner function to the code that performs and confirms it.
	// S2 registers none: the revocations arrive with the slices that own them.
	Owners map[string]IncidentOwner
	Now    func() time.Time
}

func (s *Incidents) now() time.Time { return nowOr(s.Now) }

// incidentAccess is the outcome of authorizing one incident target.
type incidentAccess struct {
	caller    authz.Identity
	kind      domain.IncidentScopeKind
	scopeID   string
	org       domain.OrgID
	scope     domain.Scope
	adapterID string
	account   authz.Account
	footprint []domain.OrgID
}

// authorizeOp authorizes the incident operation for an incident whose owning
// org is org (empty for an incident that spans orgs).
func authorizeIncidentOp(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, org domain.OrgID) (authz.Proof, error) {
	if org == "" {
		return az.Authorize(ctx, caller, authz.OpIncidentInstance, domain.Scope{})
	}
	return az.Authorize(ctx, caller, authz.OpIncidentOrg, domain.Scope{Org: org})
}

// authorizeTarget resolves the caller and decides which incident operation
// covers the target. It refuses uniformly (not found) for anything the caller
// may not open, so the answer is not an oracle for the target's grant shape.
func (s *Incidents) authorizeTarget(ctx context.Context, az *authz.TxAuthorizer, actor Actor, t IncidentTarget, now time.Time) (incidentAccess, error) {
	caller, err := actor.resolve(ctx, az, now)
	if err != nil {
		return incidentAccess{}, err
	}
	az.SetClock(now)
	out, err := s.authorizeResolved(ctx, az, caller, t)
	if err != nil && t.Kind == domain.IncidentScopeAccount && errors.Is(err, domain.ErrUnauthorized) {
		// One answer for an unknown target, a refused one and an unauthorized
		// caller: the response is no oracle for the target's grant shape.
		return incidentAccess{}, domain.ErrNotFound
	}
	return out, err
}

func (s *Incidents) authorizeResolved(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, t IncidentTarget) (incidentAccess, error) {
	out := incidentAccess{caller: caller, kind: t.Kind}
	switch t.Kind {
	case domain.IncidentScopeAccount:
		if t.Principal == "" {
			return incidentAccess{}, domain.ErrInvalid
		}
		out.scopeID = string(t.Principal)
		account, err := az.AccountByPrincipal(ctx, t.Principal)
		if err != nil {
			// An unknown principal and a machine principal answer alike; so does
			// a contained account, which the account gates treat as absent until
			// the owner decides otherwise (reported, not decided, in S2).
			if errors.Is(err, domain.ErrNotFound) {
				if _, aerr := az.Authorize(ctx, caller, authz.OpIncidentInstance, domain.Scope{}); aerr != nil {
					return incidentAccess{}, aerr
				}
			}
			return incidentAccess{}, err
		}
		out.account = account
		grants, err := az.GrantsForResetTarget(ctx, t.Principal)
		if err != nil {
			return incidentAccess{}, err
		}
		footprint, err := az.PrincipalFootprintOrgs(ctx, t.Principal)
		if err != nil {
			return incidentAccess{}, err
		}
		out.footprint = footprint
		org, instanceCap, orgCount := classifyResetTarget(grants, footprint)
		if instanceCap {
			// Instance-capability targets are host-local in every variant (ADR):
			// authorize first so a non-holder is refused identically, then refuse.
			if _, err := az.Authorize(ctx, caller, authz.OpIncidentInstance, domain.Scope{}); err != nil {
				return incidentAccess{}, err
			}
			return incidentAccess{}, domain.ErrNotFound
		}
		if orgCount == 1 {
			out.org = domain.OrgID(org)
		}
		if _, err := authorizeIncidentOp(ctx, az, caller, out.org); err != nil {
			return incidentAccess{}, err
		}
	case domain.IncidentScopeOrg:
		if t.Scope.Org == "" || t.Scope.Project != "" {
			return incidentAccess{}, domain.ErrInvalid
		}
		out.org, out.scope, out.scopeID = t.Scope.Org, domain.Scope{Org: t.Scope.Org}, string(t.Scope.Org)
		if _, err := az.Authorize(ctx, caller, authz.OpIncidentOrg, out.scope); err != nil {
			return incidentAccess{}, err
		}
	case domain.IncidentScopeProject, domain.IncidentScopeAdapter:
		if t.Scope.Org == "" || t.Scope.Project == "" || t.Scope.Env != "" {
			return incidentAccess{}, domain.ErrInvalid
		}
		out.org, out.scope = t.Scope.Org, domain.Scope{Org: t.Scope.Org, Project: t.Scope.Project}
		out.scopeID = string(t.Scope.Project)
		if _, err := az.Authorize(ctx, caller, authz.OpIncidentOrg, domain.Scope{Org: t.Scope.Org}); err != nil {
			return incidentAccess{}, err
		}
		// The project listing's own authorization also proves the project exists
		// inside the org; a project of another org answers as absent.
		if _, err := az.Authorize(ctx, caller, authz.OpIncidentLeasesProject, out.scope); err != nil {
			return incidentAccess{}, err
		}
		if t.Kind == domain.IncidentScopeAdapter {
			if t.AdapterID == "" {
				return incidentAccess{}, domain.ErrInvalid
			}
			// The adapter is verified through its owner's read, so only a holder
			// of that capability can open an adapter incident (reported in S2).
			holds, err := az.CallerHolds(ctx, caller, authz.OpAdapterInspect, out.scope)
			if err != nil {
				return incidentAccess{}, err
			}
			if !holds {
				return incidentAccess{}, domain.ErrNotFound
			}
			out.adapterID, out.scopeID = t.AdapterID, t.AdapterID
		}
	default:
		return incidentAccess{}, domain.ErrInvalid
	}
	return out, nil
}

// incidentByID loads an incident and authorizes the caller for it. Every
// failure answers not found, so an incident id is not an existence oracle.
func (s *Incidents) incidentByID(ctx context.Context, az *authz.TxAuthorizer, actor Actor, id string, now time.Time) (authz.Identity, authz.Incident, error) {
	caller, err := actor.resolve(ctx, az, now)
	if err != nil {
		return authz.Identity{}, authz.Incident{}, err
	}
	az.SetClock(now)
	inc, err := az.IncidentByID(ctx, id)
	if err != nil {
		return authz.Identity{}, authz.Incident{}, err
	}
	if _, err := authorizeIncidentOp(ctx, az, caller, inc.OrgID); err != nil {
		if errors.Is(err, domain.ErrUnauthorized) {
			return authz.Identity{}, authz.Incident{}, domain.ErrNotFound
		}
		return authz.Identity{}, authz.Incident{}, err
	}
	return caller, inc, nil
}

// incidentEvent builds an incident event. The incident id is the correlation id.
func incidentEvent(ctx context.Context, typ audit.EventType, actor domain.PrincipalID, incidentID string, outcome audit.Outcome, payload audit.Payload) (audit.Event, error) {
	return newAuditEvent(ctx, typ, actor, audit.Object{Type: "incident", ID: incidentID}, outcome, incidentID, payload)
}

func countsPayload(counts map[domain.ItemOutcome]int64) audit.Payload {
	return audit.Payload{
		"pending": int(counts[domain.ItemPending]), "done": int(counts[domain.ItemDone]),
		"unknown": int(counts[domain.ItemUnknown]), "attested": int(counts[domain.ItemAttested]),
		"blocked": int(counts[domain.ItemBlocked]), "out_of_scope": int(counts[domain.ItemOutOfScope]),
	}
}

func itemPayload(it authz.IncidentItem) audit.Payload {
	return audit.Payload{
		"item_id": it.ID, "owner_function": it.OwnerFunction, "object_kind": it.ObjectKind, "object_id": it.ObjectID,
	}
}

func reasonPayload(r domain.ItemReason) string {
	if r == domain.ReasonNone {
		return "none"
	}
	return string(r)
}

// Open opens an incident on a scope, enumerates what it leaves behind and
// writes the enumeration. Opening a scope that already has an open incident
// enumerates again and adds only the objects the ledger does not hold, so a
// repeat never resets a recorded outcome.
//
// The enumeration reads in three steps so no transaction waits on another:
// the proofless readers inside one authorizing transaction, then each existing
// tenant reader in its own, then one writing transaction that authorizes again.
func (s *Incidents) Open(ctx context.Context, actor Actor, target IncidentTarget) (IncidentView, error) {
	now := s.now()
	var found *collector
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		access, err := s.authorizeTarget(ctx, az, actor, target, now)
		if err != nil {
			return err
		}
		found, err = s.collect(ctx, r, az, access, now)
		return err
	})
	if err != nil {
		return IncidentView{}, err
	}
	if err := s.readTenantSources(ctx, actor, found); err != nil {
		return IncidentView{}, err
	}

	incidentID, err := newID("inc")
	if err != nil {
		return IncidentView{}, err
	}
	var out IncidentView
	err = tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		access, err := s.authorizeTarget(ctx, az, actor, target, now)
		if err != nil {
			return err
		}
		inc, created, err := az.OpenIncident(ctx, authz.NewIncident{
			ID: incidentID, ScopeKind: access.kind, ScopeID: access.scopeID, OrgID: access.org,
			OpenedBy: access.caller.Principal, OpenedAt: now,
		})
		if err != nil {
			return err
		}
		added := 0
		for _, c := range found.cands {
			itemID, err := newID("itm")
			if err != nil {
				return err
			}
			ok, err := az.AddIncidentItem(ctx, authz.NewIncidentItem{
				ID: itemID, IncidentID: inc.ID, OwnerFunction: c.owner, ObjectKind: c.kind, ObjectID: c.id,
				Scope: c.scope, Outcome: c.outcome, Reason: c.reason, At: now,
			})
			if err != nil {
				return err
			}
			if ok {
				added++
			}
		}
		counts, err := az.IncidentItemCounts(ctx, inc.ID)
		if err != nil {
			return err
		}
		payload := countsPayload(counts)
		payload["scope_kind"], payload["scope_id"] = string(inc.ScopeKind), inc.ScopeID
		if inc.OrgID != "" {
			payload["org"] = string(inc.OrgID)
		}
		payload["reopened"], payload["added"] = !created, added
		e, err := incidentEvent(ctx, audit.EventIncidentOpened, access.caller.Principal, inc.ID, audit.OutcomeSuccess, payload)
		if err != nil {
			return err
		}
		if err := az.RecordAuthEvent(ctx, e); err != nil {
			return err
		}
		out = IncidentView{Incident: inc, Counts: counts, Reopened: !created, Added: added}
		return nil
	})
	return out, err
}

// Get reads one incident with its outcome counts.
func (s *Incidents) Get(ctx context.Context, actor Actor, id string) (IncidentView, error) {
	now := s.now()
	var out IncidentView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		_, inc, err := s.incidentByID(ctx, az, actor, id, now)
		if err != nil {
			return err
		}
		counts, err := az.IncidentItemCounts(ctx, inc.ID)
		if err != nil {
			return err
		}
		out = IncidentView{Incident: inc, Counts: counts}
		return nil
	})
	return out, err
}

// Items pages one incident's items in id order. openOnly keeps the items that
// still need work.
func (s *Incidents) Items(ctx context.Context, actor Actor, id, afterID string, limit int, openOnly bool) ([]authz.IncidentItem, error) {
	now := s.now()
	var out []authz.IncidentItem
	err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		_, inc, err := s.incidentByID(ctx, az, actor, id, now)
		if err != nil {
			return err
		}
		out, err = az.IncidentItems(ctx, inc.ID, afterID, limit, openOnly)
		return err
	})
	return out, err
}

const resumePage = 100

// Resume re-reads the ledger and re-runs only the open items through their
// registered owners. Each item records its intent before the owner runs and its
// outcome after, in separate transactions, so a crash between the owner's own
// commit and the ledger update leaves the item open: the next resume asks the
// owner again, and the owner's own confirmation is what writes done.
func (s *Incidents) Resume(ctx context.Context, actor Actor, id string) (ResumeResult, error) {
	var res ResumeResult
	after := ""
	for {
		var page []authz.IncidentItem
		err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			_, inc, err := s.incidentByID(ctx, az, actor, id, s.now())
			if err != nil {
				return err
			}
			if inc.Status != domain.IncidentOpen {
				return fmt.Errorf("%w: incident is closed", domain.ErrConflict)
			}
			page, err = az.IncidentItems(ctx, inc.ID, after, resumePage, true)
			return err
		})
		if err != nil {
			return res, err
		}
		for _, it := range page {
			if err := s.runItem(ctx, actor, id, it.ID, &res); err != nil {
				return res, err
			}
		}
		if len(page) < resumePage {
			return res, nil
		}
		after = page[len(page)-1].ID
	}
}

// runItem is one item's intent, owner call and outcome.
func (s *Incidents) runItem(ctx context.Context, actor Actor, incidentID, itemID string, res *ResumeResult) error {
	var item authz.IncidentItem
	var owner IncidentOwner
	skip := false
	err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		caller, inc, err := s.incidentByID(ctx, az, actor, incidentID, s.now())
		if err != nil {
			return err
		}
		item, err = az.IncidentItemByID(ctx, inc.ID, itemID)
		if err != nil {
			return err
		}
		owner = s.Owners[item.OwnerFunction]
		if !item.Outcome.Open() || owner == nil || inc.Status != domain.IncidentOpen {
			skip = true
			return nil
		}
		e, err := incidentEvent(ctx, audit.EventIncidentItemIntent, caller.Principal, inc.ID, audit.OutcomeIntent, itemPayload(item))
		if err != nil {
			return err
		}
		return az.RecordAuthEvent(ctx, e)
	})
	if err != nil {
		return err
	}
	if skip {
		if owner == nil && item.Outcome.Open() {
			res.Skipped++
		}
		return nil
	}
	res.Examined++

	// The owner commits its own change in its own transaction. Whatever it
	// reports, only OwnerConfirmed can become done.
	result, ownerErr := owner(ctx, actor, item)
	if ownerErr != nil {
		result = OwnerUnconfirmed
	}

	return tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		caller, inc, err := s.incidentByID(ctx, az, actor, incidentID, now)
		if err != nil {
			return err
		}
		next, reason, outcome, resultName := domain.ItemUnknown, domain.ReasonOwnerUnconfirmed, audit.OutcomeUnknown, "unknown"
		switch result {
		case OwnerConfirmed:
			next, reason, outcome, resultName = domain.ItemDone, domain.ReasonNone, audit.OutcomeSuccess, "done"
		case OwnerBlocked:
			next, reason, outcome, resultName = domain.ItemBlocked, domain.ReasonNeedsAnotherOperator, audit.OutcomeDenied, "blocked"
		case OwnerFailed:
			// A definite failure leaves the item as it is: pending, to be run again.
			next, reason, outcome, resultName = domain.ItemPending, domain.ReasonOwnerFailed, audit.OutcomeFailure, "pending"
		}
		applied := true
		if next != domain.ItemPending {
			applied, err = az.TransitionIncidentItem(ctx, authz.ItemTransition{
				ID: item.ID, IncidentID: inc.ID, ExpectedVersion: item.RowVersion, Outcome: next, Reason: reason, At: now,
			})
			if err != nil {
				return err
			}
		}
		switch {
		case !applied:
			// The ledger row moved or the incident closed while the owner ran. The
			// item keeps its recorded state and the trail says the result is unknown.
			next, reason, outcome, resultName = domain.ItemUnknown, domain.ReasonOwnerUnconfirmed, audit.OutcomeUnknown, "unknown"
			res.Stale++
		case result == OwnerConfirmed:
			res.Confirmed++
		case result == OwnerBlocked:
			res.Blocked++
		case result == OwnerFailed:
			res.Failed++
		default:
			res.Unconfirmed++
		}
		payload := itemPayload(item)
		payload["result"], payload["reason"] = resultName, reasonPayload(reason)
		e, err := incidentEvent(ctx, audit.EventIncidentItemOutcome, caller.Principal, inc.ID, outcome, payload)
		if err != nil {
			return err
		}
		return az.RecordAuthEvent(ctx, e)
	})
}

// Attest records that an operator states an open item resolved outside Hikyo
// (an upstream revocation, a consumer adopting a new credential). It records who
// and when and carries no evidence text.
func (s *Incidents) Attest(ctx context.Context, actor Actor, incidentID, itemID string) error {
	var stale error
	err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		caller, inc, err := s.incidentByID(ctx, az, actor, incidentID, now)
		if err != nil {
			return err
		}
		item, err := az.IncidentItemByID(ctx, inc.ID, itemID)
		if err != nil {
			return err
		}
		if !item.Outcome.Open() || inc.Status != domain.IncidentOpen {
			return fmt.Errorf("%w: item is not open", domain.ErrConflict)
		}
		e, err := incidentEvent(ctx, audit.EventIncidentItemIntent, caller.Principal, inc.ID, audit.OutcomeIntent, itemPayload(item))
		if err != nil {
			return err
		}
		if err := az.RecordAuthEvent(ctx, e); err != nil {
			return err
		}
		ok, err := az.TransitionIncidentItem(ctx, authz.ItemTransition{
			ID: item.ID, IncidentID: inc.ID, ExpectedVersion: item.RowVersion, Outcome: domain.ItemAttested,
			Reason: domain.ReasonNone, AttestedBy: caller.Principal, At: now,
		})
		if err != nil {
			return err
		}
		outcome, result := audit.OutcomeSuccess, "attested"
		if !ok {
			outcome, result = audit.OutcomeUnknown, "unknown"
		}
		payload := itemPayload(item)
		payload["result"], payload["reason"] = result, "none"
		if !ok {
			payload["reason"] = reasonPayload(domain.ReasonOwnerUnconfirmed)
		}
		oe, err := incidentEvent(ctx, audit.EventIncidentItemOutcome, caller.Principal, inc.ID, outcome, payload)
		if err != nil {
			return err
		}
		if err := az.RecordAuthEvent(ctx, oe); err != nil {
			return err
		}
		if !ok {
			// The unknown result is committed; the caller is told the row moved.
			stale = fmt.Errorf("%w: item changed while attesting", domain.ErrConflict)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return stale
}

// Close closes an incident. With open items it needs the explicit residual
// status, which lists them; a clean close with open items is refused.
func (s *Incidents) Close(ctx context.Context, actor Actor, id string, withResiduals bool) (IncidentView, error) {
	var out IncidentView
	var staleErr error
	err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		caller, inc, err := s.incidentByID(ctx, az, actor, id, now)
		if err != nil {
			return err
		}
		if inc.Status != domain.IncidentOpen {
			return fmt.Errorf("%w: incident is already closed", domain.ErrConflict)
		}
		counts, err := az.IncidentItemCounts(ctx, inc.ID)
		if err != nil {
			return err
		}
		open := counts[domain.ItemPending] + counts[domain.ItemUnknown] + counts[domain.ItemBlocked]
		if open > 0 && !withResiduals {
			staleErr = ErrIncidentOpenItems
			return nil
		}
		status := domain.IncidentClosed
		residuals := []string{}
		if open > 0 {
			status = domain.IncidentClosedWithResiduals
			after := ""
			for len(residuals) < 1000 {
				page, err := az.IncidentItems(ctx, inc.ID, after, authzIncidentPage, true)
				if err != nil {
					return err
				}
				for _, it := range page {
					if len(residuals) < 1000 {
						residuals = append(residuals, it.ID)
					}
				}
				if len(page) < authzIncidentPage {
					break
				}
				after = page[len(page)-1].ID
			}
		}
		ok, err := az.CloseIncident(ctx, inc.ID, inc.RowVersion, status, caller.Principal, now)
		if err != nil {
			return err
		}
		if !ok {
			staleErr = fmt.Errorf("%w: incident changed while closing", domain.ErrConflict)
			return nil
		}
		payload := countsPayload(counts)
		payload["status"], payload["residuals"] = string(status), residuals
		e, err := incidentEvent(ctx, audit.EventIncidentClosed, caller.Principal, inc.ID, audit.OutcomeSuccess, payload)
		if err != nil {
			return err
		}
		if err := az.RecordAuthEvent(ctx, e); err != nil {
			return err
		}
		inc.Status, inc.ClosedBy, inc.ClosedAt = status, caller.Principal, now
		out = IncidentView{Incident: inc, Counts: counts}
		return nil
	})
	if err != nil {
		return IncidentView{}, err
	}
	if staleErr != nil {
		return IncidentView{}, staleErr
	}
	return out, nil
}

// authzIncidentPage is the page size the close reads residuals with.
const authzIncidentPage = 500

// NonTerminalLeases lists one page of dynamic leases that are not revoked,
// expired or failed, in id order. A project scope lists that project's leases
// and needs credential-reset at its org; the empty scope lists every org's and
// needs credential-reset at instance scope.
func (s *Incidents) NonTerminalLeases(ctx context.Context, actor Actor, scope domain.Scope, afterID string, limit int) ([]store.DynamicLeaseRef, error) {
	if limit < 1 || limit > store.MaxDynamicLeasePage || scope.Env != "" || scope.Org == "" && scope.Project != "" {
		return nil, domain.ErrInvalid
	}
	now := s.now()
	var out []store.DynamicLeaseRef
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, now)
		if err != nil {
			return err
		}
		az.SetClock(now)
		if scope == (domain.Scope{}) {
			proof, err := az.Authorize(ctx, caller, authz.OpIncidentLeasesInstance, domain.Scope{})
			if err != nil {
				return err
			}
			out, err = r.Dynamic().ListNonTerminalLeasesInInstance(ctx, proof, afterID, limit)
			return err
		}
		if scope.Project == "" {
			return domain.ErrInvalid
		}
		proof, err := az.Authorize(ctx, caller, authz.OpIncidentLeasesProject, scope)
		if err != nil {
			return err
		}
		out, err = r.Dynamic().ListNonTerminalLeasesInProject(ctx, proof, afterID, limit)
		return err
	})
	return out, err
}

// DelegationsInOrg lists one page of the live developer delegations minted
// inside an org, of every principal, in id order. It needs credential-reset at
// the org.
func (s *Incidents) DelegationsInOrg(ctx context.Context, actor Actor, org domain.OrgID, afterID string, limit int) ([]authz.DeveloperCredential, error) {
	if org == "" || limit < 1 || limit > 500 {
		return nil, domain.ErrInvalid
	}
	now := s.now()
	var out []authz.DeveloperCredential
	err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, now)
		if err != nil {
			return err
		}
		az.SetClock(now)
		if _, err := az.Authorize(ctx, caller, authz.OpIncidentOrg, domain.Scope{Org: org}); err != nil {
			return err
		}
		out, err = az.LiveDeveloperCredentialsInOrg(ctx, org, "", afterID, limit, now)
		return err
	})
	return out, err
}
