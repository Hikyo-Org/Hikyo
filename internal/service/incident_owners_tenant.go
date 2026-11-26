package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// The tenant owner functions (#1039 S3). Each resolves the object in its own
// read, calls the existing service as the acting operator (so the owner's
// capability, audit event and retry behavior apply unchanged), then reads the
// object again. A service that answers not found is read again, since the
// object may already be gone, which is the target state.

// tenantScope parses an item's scope, authorizes the incident again and rejects
// an item outside the incident's org. It also probes the owner's own capability,
// so an operator without it gets blocked whatever way the owner's service words
// its refusal. The owner services open their own transactions and cannot join
// this one: the incident binding is checked here, the owner's authorization again
// inside the service, and a ledger row that moved or closed meanwhile is stale at
// the ledger's transition.
func (o *incidentOwners) tenantScope(ctx context.Context, actor Actor, item authz.IncidentItem, kind string, op authz.Operation) (scope domain.Scope, blocked bool, err error) {
	if item.ObjectKind != kind {
		return domain.Scope{}, false, domain.ErrInvalid
	}
	scope, err = parseIncidentScope(item.Scope)
	if err != nil {
		return domain.Scope{}, false, err
	}
	err = tx.Write(ctx, o.s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		caller, inc, err := o.s.incidentByID(ctx, az, actor, item.IncidentID, o.s.now())
		if err != nil {
			return err
		}
		if inc.Status != domain.IncidentOpen {
			return fmt.Errorf("%w: incident is closed", domain.ErrConflict)
		}
		if inc.OrgID != "" && scope.Org != inc.OrgID {
			return domain.ErrNotFound
		}
		holds, err := az.CallerHolds(ctx, caller, op, scope)
		blocked = err == nil && !holds
		return err
	})
	return scope, blocked, err
}

// ownerRefused maps an owner service's refusal for want of its capability to a
// blocked item. Every other error leaves the item unknown.
func ownerRefused(err error) (IncidentOwnerResult, bool) {
	if errors.Is(err, domain.ErrUnauthorized) {
		return OwnerBlocked, true
	}
	return OwnerUnconfirmed, false
}

// machineCredentialRevoke revokes one service-account credential. The item names
// the credential; the service account is found by reading the project's.
func (o *incidentOwners) machineCredentialRevoke(ctx context.Context, actor Actor, item authz.IncidentItem) (IncidentOwnerResult, error) {
	scope, blocked, err := o.tenantScope(ctx, actor, item, domain.ObjectKindMachineCredential, authz.OpCredentialRevoke)
	if err != nil {
		return OwnerUnconfirmed, err
	}
	if blocked {
		return OwnerBlocked, nil
	}
	if scope.Project == "" {
		return OwnerUnconfirmed, domain.ErrInvalid
	}
	saID, live, err := o.machineCredentialState(ctx, scope, item.ObjectID)
	if err != nil {
		return OwnerUnconfirmed, err
	}
	if saID == "" {
		return OwnerConfirmed, nil
	}
	if live {
		if err := o.svc.Identities.RevokeCredential(ctx, actor, scope, saID, item.ObjectID); err != nil && !errors.Is(err, ErrNoSuchCredential) {
			if res, ok := ownerRefused(err); ok {
				return res, nil
			}
			return OwnerUnconfirmed, err
		}
	}
	_, live, err = o.machineCredentialState(ctx, scope, item.ObjectID)
	return confirm(err == nil && !live), err
}

// machineCredentialState finds the credential's service account in the project
// and whether the credential is still live. An empty account id means no service
// account of the org holds it; one held in another project of the org is
// ErrNotFound.
func (o *incidentOwners) machineCredentialState(ctx context.Context, scope domain.Scope, id string) (saID string, live bool, err error) {
	err = tx.Write(ctx, o.s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		saID, live = "", false
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		c, err := az.MachineCredentialInProject(ctx, scope, id)
		if errors.Is(err, domain.ErrNotFound) {
			// Absent in this project. Gone from the org, it holds no authority; held by
			// another project's account, it is outside the item and stays live.
			elsewhere, err := az.MachineCredentialExistsInOrg(ctx, scope.Org, id)
			if err == nil && elsewhere {
				err = domain.ErrNotFound
			}
			return err
		}
		if err != nil {
			return err
		}
		saID, live = c.ServiceAccountID, c.Live(o.s.now(), epoch)
		return nil
	})
	return saID, live, err
}

// invitationRevoke deletes one pending email invitation of the org.
func (o *incidentOwners) invitationRevoke(ctx context.Context, actor Actor, item authz.IncidentItem) (IncidentOwnerResult, error) {
	scope, blocked, err := o.tenantScope(ctx, actor, item, domain.ObjectKindEmailInvitation, authz.OpEmailInvitationRevoke)
	if err != nil {
		return OwnerUnconfirmed, err
	}
	if blocked {
		return OwnerBlocked, nil
	}
	if scope.Org == "" || scope.Project != "" {
		return OwnerUnconfirmed, domain.ErrInvalid
	}
	err = o.svc.Invites.Revoke(ctx, actor, scope.Org, item.ObjectID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		if res, ok := ownerRefused(err); ok {
			return res, nil
		}
		return OwnerUnconfirmed, err
	}
	var present bool
	err = tx.Write(ctx, o.s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		_, err := az.OrgEmailInvitationInOrg(ctx, item.ObjectID, scope.Org)
		present = err == nil
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	})
	return confirm(err == nil && !present), err
}

// scimCredentialRevoke revokes one SCIM bearer credential. The binding is found
// through the org's own listings, in the acting operator's name.
func (o *incidentOwners) scimCredentialRevoke(ctx context.Context, actor Actor, item authz.IncidentItem) (IncidentOwnerResult, error) {
	scope, blocked, err := o.tenantScope(ctx, actor, item, domain.ObjectKindSCIMCredential, authz.OpSCIMCredentialRevoke)
	if err != nil {
		return OwnerUnconfirmed, err
	}
	if blocked {
		return OwnerBlocked, nil
	}
	if scope.Org == "" || scope.Project != "" {
		return OwnerUnconfirmed, domain.ErrInvalid
	}
	binding, live, err := o.scimCredentialState(ctx, actor, scope.Org, item.ObjectID)
	if err != nil {
		if res, ok := ownerRefused(err); ok {
			return res, nil
		}
		return OwnerUnconfirmed, err
	}
	if binding == "" {
		return OwnerConfirmed, nil
	}
	if live {
		if err := o.svc.SCIM.RevokeCredential(ctx, actor, scope.Org, binding, item.ObjectID); err != nil {
			if res, ok := ownerRefused(err); ok {
				return res, nil
			}
			return OwnerUnconfirmed, err
		}
	}
	_, live, err = o.scimCredentialState(ctx, actor, scope.Org, item.ObjectID)
	return confirm(err == nil && !live), err
}

func (o *incidentOwners) scimCredentialState(ctx context.Context, actor Actor, org domain.OrgID, id string) (binding string, live bool, err error) {
	bindings, err := o.svc.SCIM.ListBindings(ctx, actor, org)
	if err != nil {
		return "", false, err
	}
	for _, b := range bindings {
		creds, err := o.svc.SCIM.ListCredentials(ctx, actor, org, b.ID)
		if err != nil {
			return "", false, err
		}
		for _, c := range creds {
			if c.ID == id {
				return b.ID, c.Live, nil
			}
		}
	}
	return "", false, nil
}

// leaseRevoke asks the dynamic provider to revoke one lease. Revocation is
// asynchronous: the owner queues it and the lease worker drops the role, so the
// item is confirmed only when the lease itself reads revoked (or expired), and
// stays open, pending or unknown, until then.
func (o *incidentOwners) leaseRevoke(ctx context.Context, actor Actor, item authz.IncidentItem) (IncidentOwnerResult, error) {
	scope, blocked, err := o.tenantScope(ctx, actor, item, domain.ObjectKindLease, authz.OpLeaseRevoke)
	if err != nil {
		return OwnerUnconfirmed, err
	}
	if blocked {
		return OwnerBlocked, nil
	}
	if scope.Env == "" {
		return OwnerUnconfirmed, domain.ErrInvalid
	}
	return revokeLeaseFlow(
		func() (string, bool, error) {
			return o.svc.Dynamic.LeaseStateIfAuthorized(ctx, actor, scope, item.ObjectID)
		},
		func() error {
			_, err := o.svc.Dynamic.RevokeLease(ctx, actor, scope, item.ObjectID)
			return err
		},
	)
}

// revokeLeaseFlow drives one lease to revoked through two reads and one revoke. The
// reads authorize first, so ErrNotFound is not what tells absence apart: absent is
// the reads' own answer and a lost grant is ErrUnauthorized, which ends blocked at
// every step. The revoke masks a lost grant as not found too, so its not found is
// settled by the confirming read rather than trusted.
func revokeLeaseFlow(read func() (state string, absent bool, err error), revoke func() error) (IncidentOwnerResult, error) {
	fail := func(err error) (IncidentOwnerResult, error) {
		if res, ok := ownerRefused(err); ok {
			return res, nil
		}
		return OwnerUnconfirmed, err
	}
	state, absent, err := read()
	if err != nil {
		return fail(err)
	}
	if absent || leaseTerminal(state) {
		// A lease that no longer exists holds no role.
		return OwnerConfirmed, nil
	}
	if state != "revoking" {
		if err := revoke(); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fail(err)
		}
	}
	state, absent, err = read()
	if err != nil {
		return fail(err)
	}
	return confirm(absent || leaseTerminal(state)), nil
}

func leaseTerminal(state string) bool {
	return state == "revoked" || state == "expired"
}

// targetPause pauses one adapter target. The pause is reversible by the target's
// own resume and touches no ledger, so it is the containment form for an adapter.
func (o *incidentOwners) targetPause(ctx context.Context, actor Actor, item authz.IncidentItem) (IncidentOwnerResult, error) {
	scope, blocked, err := o.tenantScope(ctx, actor, item, domain.ObjectKindAdapterTarget, authz.OpAdapterConfigure)
	if err != nil {
		return OwnerUnconfirmed, err
	}
	if blocked {
		return OwnerBlocked, nil
	}
	if scope.Project == "" || scope.Env != "" {
		return OwnerUnconfirmed, domain.ErrInvalid
	}
	return pauseTargetFlow(
		func() (bool, error) {
			t, err := o.svc.Adapters.PauseTarget(ctx, actor, scope, item.ObjectID)
			return t.PausedAt != nil, err
		},
		func() (bool, error) { return o.svc.Adapters.TargetAbsentIfAuthorized(ctx, actor, scope, item.ObjectID) },
	)
}

// pauseTargetFlow pauses a target. The pause masks a lost grant as not found, so its
// not found is settled by a read that authorizes first: only a target that is
// really gone is confirmed (it runs nothing), a lost grant ends blocked, and a
// target that exists but could not be paused is a conflict, never a confirmation.
func pauseTargetFlow(pause func() (paused bool, err error), absent func() (bool, error)) (IncidentOwnerResult, error) {
	paused, err := pause()
	if errors.Is(err, domain.ErrNotFound) {
		gone, err := absent()
		if err == nil && gone {
			return OwnerConfirmed, nil
		}
		if err == nil {
			err = fmt.Errorf("%w: the target could not be paused and is not absent", domain.ErrConflict)
		}
		if res, ok := ownerRefused(err); ok {
			return res, nil
		}
		return OwnerUnconfirmed, err
	}
	if err != nil {
		if res, ok := ownerRefused(err); ok {
			return res, nil
		}
		return OwnerUnconfirmed, err
	}
	return confirm(paused), nil
}
