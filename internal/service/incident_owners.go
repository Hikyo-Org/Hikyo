package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Incident owner functions (incident-containment ADR, "Credential replacement";
// #1039 S3, #1471). Each one performs the revocation of one ledger kind through
// the existing owner, under that owner's own capability and in its own
// transaction, and then re-reads the owner's current state (a same-transaction or
// later read, never a cached one). Only a re-read that
// shows the target state returns OwnerConfirmed, so a crash, a lost response or
// an unreachable provider leaves the item open. An owner is idempotent: resume
// runs it again for an item whose earlier outcome never reached the ledger, and
// an object that is already in its target state confirms without a second write.
//
// The ledger never reads an owner's error text. A refusal for want of the
// owner's capability is OwnerBlocked; a definite refusal (the last manager, an
// owner that will not clear a credential it still needs) is OwnerFailed; every
// other error leaves the item unknown.

// ErrIncidentLastManager refuses a containment that would leave an org without
// an uncontained manage-members holder (ADR O3: refuse, no override).
var ErrIncidentLastManager = fmt.Errorf("%w: assign another manage-members holder before containing this principal", domain.ErrConflict)

// ErrIncidentSelfContainment refuses containing the operator's own account: the
// person acting is the one account that must stay reachable to finish the work.
var ErrIncidentSelfContainment = fmt.Errorf("%w: an operator cannot contain their own account", domain.ErrConflict)

// parseIncidentScope is renderScope's inverse for a ledger scope string. A
// string that is not one the ledger renders is invalid, never a wider scope.
func parseIncidentScope(s string) (domain.Scope, error) {
	if s == "instance" {
		return domain.Scope{}, nil
	}
	parts := strings.Split(s, "/")
	if len(parts) < 1 || len(parts) > 3 {
		return domain.Scope{}, domain.ErrInvalid
	}
	for _, p := range parts {
		if !validIncidentObjectID(p) {
			return domain.Scope{}, domain.ErrInvalid
		}
	}
	out := domain.Scope{Org: domain.OrgID(parts[0])}
	if len(parts) > 1 {
		out.Project = domain.ProjectID(parts[1])
	}
	if len(parts) > 2 {
		out.Env = domain.EnvID(parts[2])
	}
	return out, nil
}

// IncidentOwnerServices are the existing owners the owner functions call. A nil
// service leaves its owner functions unregistered, so a deployment that does
// not wire one never reports its items done.
type IncidentOwnerServices struct {
	Identities *Identities
	Invites    *EmailInvitations
	SCIM       *SCIM
	Dynamic    *Dynamic
	Adapters   *Adapters
}

// NewIncidentOwners returns the owner functions the incident ledger resumes
// through. Owners that need a service the caller did not wire are omitted. No
// production code builds an Incidents service before the operator surface
// (slice 4), which installs this map in the app wiring and tests it there.
func NewIncidentOwners(s *Incidents, svc IncidentOwnerServices) map[string]IncidentOwner {
	o := &incidentOwners{s: s, svc: svc}
	out := map[string]IncidentOwner{
		domain.OwnerAccountContain:  o.accountContain,
		domain.OwnerSessionRevoke:   o.sessionRevoke,
		domain.OwnerDeveloperRevoke: o.developerRevoke,
		domain.OwnerRotationHalt:    o.rotationHalt,
	}
	if svc.Identities != nil {
		out[domain.OwnerMachineCredential] = o.machineCredentialRevoke
	}
	if svc.Invites != nil {
		out[domain.OwnerEmailInvitation] = o.invitationRevoke
	}
	if svc.SCIM != nil {
		out[domain.OwnerSCIMCredential] = o.scimCredentialRevoke
	}
	if svc.Dynamic != nil {
		out[domain.OwnerLeaseRevoke] = o.leaseRevoke
	}
	if svc.Adapters != nil {
		out[domain.OwnerAdapterTargetPause] = o.targetPause
	}
	return out
}

type incidentOwners struct {
	s   *Incidents
	svc IncidentOwnerServices
}

// ownerRun is what an owner function sees inside its authorizing transaction.
type ownerRun struct {
	r      store.Repos
	az     *authz.TxAuthorizer
	caller authz.Identity
	inc    authz.Incident
	item   authz.IncidentItem
	scope  domain.Scope
	now    time.Time
}

// inTx authorizes the incident again, binds the item to it and runs fn in one
// transaction. The item must belong to the incident, the incident must be open,
// and an item scoped to an org must lie inside the incident's own org, so an
// owner never acts outside what the incident was opened and authorized on. A
// refusal for want of the owner's capability is blocked, not an error.
func (o *incidentOwners) inTx(ctx context.Context, actor Actor, item authz.IncidentItem, kind string, fn func(ownerRun) (IncidentOwnerResult, error)) (IncidentOwnerResult, error) {
	if item.ObjectKind != kind {
		return OwnerUnconfirmed, domain.ErrInvalid
	}
	scope, err := parseIncidentScope(item.Scope)
	if err != nil {
		return OwnerUnconfirmed, err
	}
	var res IncidentOwnerResult
	err = tx.Write(ctx, o.s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		res = OwnerUnconfirmed
		now := store.CanonTime(o.s.now())
		caller, inc, err := o.s.incidentByID(ctx, az, actor, item.IncidentID, now)
		if err != nil {
			return err
		}
		if inc.Status != domain.IncidentOpen {
			return fmt.Errorf("%w: incident is closed", domain.ErrConflict)
		}
		if inc.OrgID != "" && scope.Org != "" && scope.Org != inc.OrgID {
			return domain.ErrNotFound
		}
		res, err = fn(ownerRun{r: r, az: az, caller: caller, inc: inc, item: item, scope: scope, now: now})
		return err
	})
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		return OwnerBlocked, nil
	case errors.Is(err, ErrIncidentLastManager), errors.Is(err, ErrIncidentSelfContainment):
		return OwnerFailed, nil
	}
	return res, err
}

// confirm is the one place a re-read becomes a result.
func confirm(done bool) IncidentOwnerResult {
	if done {
		return OwnerConfirmed
	}
	return OwnerUnconfirmed
}

// ---- account.contain -------------------------------------------------------

// accountContain contains the account an item names (an org incident's member,
// or an account incident's own target). ADR O1 B: the reversible contained
// state, so nothing is deleted and no grant is removed.
func (o *incidentOwners) accountContain(ctx context.Context, actor Actor, item authz.IncidentItem) (IncidentOwnerResult, error) {
	return o.inTx(ctx, actor, item, domain.ObjectKindAccount, func(w ownerRun) (IncidentOwnerResult, error) {
		return o.s.containAccount(ctx, w, domain.PrincipalID(item.ObjectID))
	})
}

// containAccount is the containment write, run inside the caller's transaction.
// It checks the incident boundary and authorizes first, then locks the principal
// row (the lock reset, grant writers and privacy take), refuses the last manager
// of any org, then sets the state, which
// advances the generation and consumes outstanding authorities in the same
// transaction. It ends the principal's sessions and revokes its developer
// delegations like reset does, so nothing minted before the commit survives it.
func (s *Incidents) containAccount(ctx context.Context, w ownerRun, target domain.PrincipalID) (IncidentOwnerResult, error) {
	if target == "" {
		return OwnerUnconfirmed, domain.ErrInvalid
	}
	// The target is the account incident's own principal, or a member of the org
	// the incident was opened on. Anything else is outside the incident.
	switch w.inc.ScopeKind {
	case domain.IncidentScopeAccount:
		if string(target) != w.inc.ScopeID {
			return OwnerUnconfirmed, domain.ErrNotFound
		}
	case domain.IncidentScopeOrg:
	default:
		return OwnerUnconfirmed, domain.ErrInvalid
	}
	// Owner decision (incident-containment ADR, Owner decisions): an operator never
	// contains their own account.
	if target == w.caller.Principal {
		return OwnerUnconfirmed, ErrIncidentSelfContainment
	}
	// The boundary comes before any lock or containment read, so a principal
	// outside the incident answers not found whatever its state. An org incident's
	// boundary is its member selection; its kind and privacy reads come before the
	// authorization below and reveal nothing the incident's own listing does not
	// (inTx already refused a caller who cannot read the incident).
	if w.inc.ScopeKind == domain.IncidentScopeOrg {
		selected, err := incidentSelectsMember(ctx, w.az, w.inc.OrgID, target)
		if err != nil {
			return OwnerUnconfirmed, err
		}
		if !selected {
			return OwnerUnconfirmed, domain.ErrNotFound
		}
	}
	footprint, err := w.az.PrincipalFootprintOrgs(ctx, target)
	if err != nil {
		return OwnerUnconfirmed, err
	}
	grants, err := w.az.GrantsForResetTarget(ctx, target)
	if err != nil {
		return OwnerUnconfirmed, err
	}
	org, instanceCap, orgCount := classifyResetTarget(grants, footprint)
	if instanceCap {
		// An instance-capability target is contained host-local only (ADR O2, O3).
		return OwnerBlocked, nil
	}
	owning := domain.OrgID("")
	if orgCount == 1 {
		owning = domain.OrgID(org)
	}
	if _, err := authorizeIncidentOp(ctx, w.az, w.caller, owning); err != nil {
		return OwnerUnconfirmed, err
	}
	// The target and the managers of every org it manages are locked in one sorted
	// pass, so two concurrent containments of the last two managers cannot deadlock.
	// The census it returns was read in this transaction's serializable snapshot,
	// the same snapshot as the grants above.
	_, holders, err := lockTargetAndManagers(ctx, w.az, target)
	if err != nil {
		return OwnerUnconfirmed, err
	}
	if _, contained, err := w.az.PrincipalContainedAt(ctx, target); err != nil {
		return OwnerUnconfirmed, err
	} else if contained {
		return OwnerConfirmed, nil
	}
	if _, err := w.az.AccountByPrincipal(ctx, target); err != nil {
		return OwnerUnconfirmed, err
	}
	if !hasAlternativeManager(target, holders) {
		return OwnerUnconfirmed, ErrIncidentLastManager
	}
	if err := w.az.ContainPrincipal(ctx, target, w.now, w.inc.ID); err != nil {
		return OwnerUnconfirmed, err
	}
	if err := w.az.RevokeAllSessionsFor(ctx, target); err != nil {
		return OwnerUnconfirmed, err
	}
	if err := revokeDeveloperScope(ctx, w.az, w.caller.Principal, target, domain.Scope{}, "incident-containment", w.now); err != nil {
		return OwnerUnconfirmed, err
	}
	e, err := incidentEvent(ctx, audit.EventIncidentPrincipalContained, w.caller.Principal, w.inc.ID, audit.OutcomeSuccess,
		audit.Payload{"principal": string(target)})
	if err != nil {
		return OwnerUnconfirmed, err
	}
	if err := w.az.RecordAuthEvent(ctx, e); err != nil {
		return OwnerUnconfirmed, err
	}
	_, contained, err := w.az.PrincipalContainedAt(ctx, target)
	return confirm(contained), err
}

// ---- session.revoke and developer_credential.revoke ------------------------

// sessionRevoke ends one session of the account an incident was opened on. The
// principal is the incident's, never the item's, so a ledger row cannot name
// another person's session. A revocation this call makes finishes as the account
// holder's own explicit revocation does, in the same transaction: the developer
// delegations minted from the session end, and the session event is recorded
// under the incident's correlation.
func (o *incidentOwners) sessionRevoke(ctx context.Context, actor Actor, item authz.IncidentItem) (IncidentOwnerResult, error) {
	return o.inTx(ctx, actor, item, domain.ObjectKindSession, func(w ownerRun) (IncidentOwnerResult, error) {
		if w.inc.ScopeKind != domain.IncidentScopeAccount {
			return OwnerUnconfirmed, domain.ErrInvalid
		}
		principal := domain.PrincipalID(w.inc.ScopeID)
		held, err := w.az.SessionsForPrincipal(ctx, principal)
		if err != nil {
			return OwnerUnconfirmed, err
		}
		if i := slices.IndexFunc(held, func(s authz.SessionSummary) bool { return s.ID == item.ObjectID }); i >= 0 {
			did, err := w.az.RevokeSessionForPrincipal(ctx, item.ObjectID, principal)
			if err != nil {
				return OwnerUnconfirmed, err
			}
			if did {
				if err := recordSessionRevocation(ctx, w.az, w.caller.Principal, held[i], w.now); err != nil {
					return OwnerUnconfirmed, err
				}
			}
		}
		left, err := w.az.SessionsForPrincipal(ctx, principal)
		if err != nil {
			return OwnerUnconfirmed, err
		}
		if slices.ContainsFunc(left, func(s authz.SessionSummary) bool { return s.ID == item.ObjectID }) {
			return OwnerUnconfirmed, nil
		}
		// Gone from the principal. Gone from the instance, it admits no one; held by
		// another principal, it is outside the item and stays live.
		elsewhere, err := w.az.SessionExistsInInstance(ctx, item.ObjectID)
		if err != nil {
			return OwnerUnconfirmed, err
		}
		if elsewhere {
			return OwnerUnconfirmed, domain.ErrNotFound
		}
		return OwnerConfirmed, nil
	})
}

// developerRevoke revokes one developer delegation. An org-level delegation is
// revoked under the credential-reset the incident was authorized with; a
// project-level one needs the project owner's own revoke, as its enumeration
// probed. The delegation is read by id at the item's exact placement and, on an
// account incident, under the incident's authority, so one outside the incident
// is not found and stays live, and the cost does not depend on the table size.
func (o *incidentOwners) developerRevoke(ctx context.Context, actor Actor, item authz.IncidentItem) (IncidentOwnerResult, error) {
	return o.inTx(ctx, actor, item, domain.ObjectKindDeveloperCred, func(w ownerRun) (IncidentOwnerResult, error) {
		var proof authz.Proof
		if w.scope.Project != "" {
			p, err := w.az.Authorize(ctx, w.caller, authz.OpDeveloperCredentialRevokeProject, domain.Scope{Org: w.scope.Org, Project: w.scope.Project})
			if err != nil {
				return OwnerUnconfirmed, err
			}
			proof = p
		}
		// An account incident revokes only the person's own delegations, so a ledger
		// row cannot name another person's; an org incident has no single authority.
		authority := domain.PrincipalID("")
		if w.inc.ScopeKind == domain.IncidentScopeAccount {
			authority = domain.PrincipalID(w.inc.ScopeID)
		}
		c, revoked, err := w.az.RevokeDeveloperCredentialInScope(ctx, authority, w.scope, item.ObjectID, w.now)
		if err != nil {
			return OwnerUnconfirmed, err
		}
		if revoked {
			e, err := developerRevocationEvent(ctx, w.caller.Principal, c, "incident-containment")
			if err != nil {
				return OwnerUnconfirmed, err
			}
			if proof != nil {
				err = w.r.Audit().InsertTenant(ctx, proof, e)
			} else {
				err = w.az.RecordAuthEvent(ctx, e)
			}
			if err != nil {
				return OwnerUnconfirmed, err
			}
		}
		// Confirm by re-reading the delegation live: one no longer live holds no
		// authority, whether this call revoked it or it was already revoked or expired.
		live, err := developerCredentialLive(ctx, w.az, w.scope.Org, authority, item.ObjectID, w.now)
		return confirm(err == nil && !live), err
	})
}

// developerCredentialLive is the confirming re-read: whether a live delegation of
// the org (and, when given, authority) has the id. It reads one row by id, so its
// cost does not depend on how many delegations the org or the instance holds.
func developerCredentialLive(ctx context.Context, az *authz.TxAuthorizer, org domain.OrgID, authority domain.PrincipalID, id string, now time.Time) (bool, error) {
	_, err := az.LiveDeveloperCredentialInOrg(ctx, org, authority, id, now)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// ---- rotation.halt (#1471) -------------------------------------------------

// rotationHalt halts a scheduled rotation whose recorded authority principal is
// the account an incident selects (the incident's own account, or a member the org
// incident selects). It uses the fixed reason incident_contained, so a halt by an
// incident is never read as the executor's authority_lost. It halts an active or
// a paused rotation and does not depend on the account.contain item: items are
// independent owners that each read their own state (ADR, Concurrency and
// fencing). It never resumes: a release of the principal leaves the rotation
// halted, and the resume stays an explicit operator act with the rotation's own
// authorization. The halt bumps the generation. A run that has not yet published
// stops: its gate sees the halted state, and on PostgreSQL a halt that commits
// inside the publish transaction aborts that publish whole (SERIALIZABLE, the
// halt writes the row the publish writes), so the rerun finds the rotation
// halted. A run already past its publish is not undone: an in-flight revoke
// finishes under the executor's own fence and the credential already delivered
// keeps its expiry. The item is confirmed only when the rotation is halted or gone;
// a rotation a restore holds stays open. A rotation that no longer exists answers
// confirmed (it schedules nothing), while one whose authority this incident does
// not select answers not found. The two answers differ, and only the operator who
// holds the incident can tell them apart: the first is a fact about the rotation,
// the second about the incident.
func (o *incidentOwners) rotationHalt(ctx context.Context, actor Actor, item authz.IncidentItem) (IncidentOwnerResult, error) {
	return o.inTx(ctx, actor, item, domain.ObjectKindRotation, func(w ownerRun) (IncidentOwnerResult, error) {
		if w.inc.ScopeKind != domain.IncidentScopeAccount && w.inc.ScopeKind != domain.IncidentScopeOrg {
			return OwnerUnconfirmed, domain.ErrInvalid
		}
		if w.scope.Env == "" {
			return OwnerUnconfirmed, domain.ErrInvalid
		}
		proof, err := w.az.Authorize(ctx, w.caller, authz.OpIncidentRotationHalt, w.scope)
		if err != nil {
			return OwnerUnconfirmed, err
		}
		cur, err := w.r.Rotations().Get(ctx, proof, item.ObjectID)
		if errors.Is(err, domain.ErrNotFound) {
			// Gone from the instance, a rotation schedules nothing; placed anywhere
			// else (another environment or org), it is outside the item and stays live.
			orgProof, err := w.az.Authorize(ctx, w.caller, authz.OpIncidentRotationsOrg, domain.Scope{Org: w.scope.Org})
			if err != nil {
				return OwnerUnconfirmed, err
			}
			elsewhere, err := w.r.Rotations().ExistsInInstance(ctx, orgProof, item.ObjectID)
			if err == nil && elsewhere {
				err = domain.ErrNotFound
			}
			if err != nil {
				return OwnerUnconfirmed, err
			}
			return OwnerConfirmed, nil
		}
		if err != nil {
			return OwnerUnconfirmed, err
		}
		// The recorded authority must be an account this incident selects: the
		// incident's own, or a member the org incident selects (incidentSelectsMember).
		// A principal tied to the org only through other rows, such as a SCIM user,
		// is not selected. The check reads no containment: the halt must not depend
		// on whether account.contain ran first.
		authority := domain.PrincipalID(cur.AuthorityPrincipalID)
		if w.inc.ScopeKind == domain.IncidentScopeAccount {
			if string(authority) != w.inc.ScopeID {
				return OwnerUnconfirmed, domain.ErrNotFound
			}
		} else {
			selected, err := incidentSelectsMember(ctx, w.az, w.inc.OrgID, authority)
			if err != nil {
				return OwnerUnconfirmed, err
			}
			if !selected {
				return OwnerUnconfirmed, domain.ErrNotFound
			}
		}
		halted, err := w.r.Rotations().HaltForIncident(ctx, proof, cur.ID, rotHaltIncident, w.now)
		if err != nil {
			return OwnerUnconfirmed, err
		}
		if halted {
			cur.State, cur.Generation = store.RotationStateHalted, cur.Generation+1
			if err := insertRotationConfigured(ctx, w.r, proof, w.caller.Principal, rotationActionHalted, rotHaltIncident, cur); err != nil {
				return OwnerUnconfirmed, err
			}
			return OwnerConfirmed, nil
		}
		now, err := w.r.Rotations().Get(ctx, proof, cur.ID)
		if err != nil {
			return OwnerUnconfirmed, err
		}
		return confirm(now.State == store.RotationStateHalted || now.State == store.RotationStateDisabled), nil
	})
}
