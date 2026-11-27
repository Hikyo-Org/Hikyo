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
	saID, live, err := o.machineCredentialState(ctx, actor, scope, item.ObjectID)
	if err != nil {
		if res, ok := ownerRefused(err); ok {
			return res, nil
		}
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
	_, live, err = o.machineCredentialState(ctx, actor, scope, item.ObjectID)
	if res, ok := ownerRefused(err); ok {
		return res, nil
	}
	return confirm(err == nil && !live), err
}

// machineCredentialState finds the credential's service account in the project
// and whether the credential is still live. An empty account id means no service
// account of the instance holds it; one held anywhere else (another project, or
// another org) is ErrNotFound. The reads run under the incident's org operation.
func (o *incidentOwners) machineCredentialState(ctx context.Context, actor Actor, scope domain.Scope, id string) (saID string, live bool, err error) {
	err = tx.Read(ctx, o.s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		saID, live = "", false
		if _, err := o.authorizeIncidentOrg(ctx, az, actor, scope.Org); err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		c, err := az.MachineCredentialInProject(ctx, scope, id)
		if errors.Is(err, domain.ErrNotFound) {
			// Absent in this project. Gone from the instance, it holds no authority;
			// held by any other account, it is outside the item and stays live.
			elsewhere, err := az.MachineCredentialExistsInInstance(ctx, id)
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
	err = tx.Read(ctx, o.s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		present = false
		if _, err := o.authorizeIncidentOrg(ctx, az, actor, scope.Org); err != nil {
			return err
		}
		_, err := az.OrgEmailInvitationInOrg(ctx, item.ObjectID, scope.Org)
		present = err == nil
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		// Absent from the org. Gone from the instance, it admits no one; held by
		// another org, it is outside the item and stays live.
		elsewhere, err := az.OrgEmailInvitationExistsInInstance(ctx, item.ObjectID)
		if err == nil && elsewhere {
			err = domain.ErrNotFound
		}
		return err
	})
	if res, ok := ownerRefused(err); ok {
		return res, nil
	}
	return confirm(err == nil && !present), err
}

// scimCredentialRevoke revokes one SCIM bearer credential. The binding is read
// from the credential itself, by id within the org.
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
	if res, ok := ownerRefused(err); ok {
		return res, nil
	}
	return confirm(err == nil && !live), err
}

// scimCredentialState reads the one credential the item names, by id within the
// org, under the owner's own operation: its binding and whether it is live. An
// empty binding means the instance holds no such credential; one held by another
// org is ErrNotFound. The read writes no audit row and refreshes no binding state.
func (o *incidentOwners) scimCredentialState(ctx context.Context, actor Actor, org domain.OrgID, id string) (binding string, live bool, err error) {
	err = tx.Write(ctx, o.s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		binding, live = "", false
		_, proof, err := authorize(ctx, az, actor, authz.OpSCIMCredentialRevoke, domain.Scope{Org: org}, o.s.now())
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("%w: authority over the SCIM credential was lost", domain.ErrUnauthorized)
		}
		if err != nil {
			return err
		}
		c, err := r.SCIM().CredentialInOrg(ctx, proof, id)
		if errors.Is(err, domain.ErrNotFound) {
			elsewhere, err := az.SCIMCredentialExistsInInstance(ctx, id)
			if err == nil && elsewhere {
				err = domain.ErrNotFound
			}
			return err
		}
		if err != nil {
			return err
		}
		binding, live = c.BindingID, c.Live(o.s.now())
		return nil
	})
	return binding, live, err
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
			state, absent, err := o.svc.Dynamic.LeaseStateIfAuthorized(ctx, actor, scope, item.ObjectID)
			if err == nil && absent {
				err = o.goneFromInstance(ctx, actor, scope.Org, func(ctx context.Context, r store.ReadRepos, p authz.Proof) (bool, error) {
					return r.Dynamic().LeaseExistsInInstance(ctx, p, item.ObjectID)
				})
			}
			return state, absent, err
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
		func() (bool, error) {
			absent, err := o.svc.Adapters.TargetAbsentIfAuthorized(ctx, actor, scope, item.ObjectID)
			if err == nil && absent {
				err = o.goneFromInstance(ctx, actor, scope.Org, func(ctx context.Context, r store.ReadRepos, p authz.Proof) (bool, error) {
					return r.Adapters().TargetExistsInInstance(ctx, p, item.ObjectID)
				})
			}
			return absent, err
		},
	)
}

// goneFromInstance settles an object absent from the item's placement. Gone from
// the instance, it holds nothing and the item is confirmed (nil); placed anywhere
// else (another project or another org), it is outside the item and stays live
// (ErrNotFound). The probe runs under the incident's own org operation, so a
// refusal is ErrUnauthorized.
func (o *incidentOwners) goneFromInstance(ctx context.Context, actor Actor, org domain.OrgID, exists func(context.Context, store.ReadRepos, authz.Proof) (bool, error)) error {
	return tx.Read(ctx, o.s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		proof, err := o.authorizeIncidentOrg(ctx, az, actor, org)
		if err != nil {
			return err
		}
		elsewhere, err := exists(ctx, r, proof)
		if err == nil && elsewhere {
			err = domain.ErrNotFound
		}
		return err
	})
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

// authorizeIncidentOrg authorizes the incident's org operation for an owner's own
// probe. The authorizer masks a refusal as not found; here it is ErrUnauthorized,
// so the item ends blocked rather than read as absent.
func (o *incidentOwners) authorizeIncidentOrg(ctx context.Context, az *authz.TxAuthorizer, actor Actor, org domain.OrgID) (authz.Proof, error) {
	_, proof, err := authorize(ctx, az, actor, authz.OpIncidentOrg, domain.Scope{Org: org}, o.s.now())
	if errors.Is(err, domain.ErrNotFound) {
		err = fmt.Errorf("%w: authority over the incident org was lost", domain.ErrUnauthorized)
	}
	return proof, err
}
