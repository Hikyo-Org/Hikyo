package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// releasePageSize bounds one page of the incident's items read for the
// linked-identity condition.
const releasePageSize = 500

// ErrReleaseNotReady refuses a release whose O4 A condition is not met. It is a
// conflict: the principal stays contained and the operator can satisfy the
// condition and ask again.
var ErrReleaseNotReady = fmt.Errorf("%w: release condition not met", domain.ErrConflict)

// ReleasePrincipal ends one contained account's containment (ADR O4 A). It is a
// separate audited act, never part of closing an incident, and it names the
// incident that contained the principal so the operator who holds that
// incident's authority is the one who releases it.
//
// Release returns the principal to none only when (a) a credential
// establishment has completed during the containing incident, for an
// account with a local password, and (b) every linked identity of the account
// has an attested ledger item in the containing incident. It reactivates no
// revoked delegation or credential and regrants nothing: the store setter
// advances the session generation again so nothing minted before containment
// revives.
//
// Release lifts containment, so it needs the same fresh account-security proof as
// Resume (ADR O2): the incident is authorized first, the proof is verified for a
// session actor, and the evidence is spent in the release transaction, so a refused
// release spends nothing. A host-local actor is exempt, as for Resume.
func (s *Incidents) ReleasePrincipal(ctx context.Context, actor Actor, incidentID string, target domain.PrincipalID, proof string) error {
	evidence, err := s.incidentProof(ctx, actor, incidentID, proof)
	if err != nil {
		return err
	}
	return tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		caller, inc, err := s.incidentByID(ctx, az, actor, incidentID, now)
		if err != nil {
			return err
		}
		if evidence.kind != reauthEvidenceExempt {
			if err := s.Auth.ConsumeReauthEvidence(ctx, az, evidence, caller.Principal); err != nil {
				return err
			}
		}
		// One answer for an unknown principal, one this incident did not
		// contain, and the incident's own account.
		contained, err := az.PrincipalContainedIncident(ctx, target)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err // a serialization failure must reach the retry loop
		}
		if err != nil || contained != inc.ID || ownIncident(caller, inc) {
			return domain.ErrNotFound
		}
		// The same classification containment ran: a target in several orgs needs
		// instance scope, and an instance-capability target is host-local.
		if err := authorizeReleaseTarget(ctx, az, caller, target); err != nil {
			return err
		}
		// Another open incident that also recorded this principal contained keeps its
		// own claim on the state, so its operator closes or releases it first.
		if other, err := az.OtherOpenIncidentHolding(ctx, target, inc.ID); err != nil {
			return err
		} else if other != "" {
			return otherHolderRefusal(ctx, az, caller, other)
		}
		hasPassword, err := az.PrincipalHasPassword(ctx, target)
		if err != nil {
			return err
		}
		if hasPassword {
			established, err := az.CredentialEstablishedIn(ctx, target, inc.ID)
			if err != nil {
				return err
			}
			if !established {
				return fmt.Errorf("%w: no credential establishment has completed during this incident's containment", ErrReleaseNotReady)
			}
		}
		pending, total, containOpen, err := identitiesNotAttested(ctx, az, inc.ID, target)
		if err != nil {
			return err
		}
		switch {
		case containOpen:
			// The ledger still owes this containment: a later resume would contain the
			// account again, so release waits until the item is terminal.
			return fmt.Errorf("%w: the incident's account containment item is still open; resume it first", ErrReleaseNotReady)
		case pending > 0:
			return fmt.Errorf("%w: %d linked identity item(s) are not attested", ErrReleaseNotReady, pending)
		case !hasPassword && total == 0:
			// No password and no linked identity to attest: nothing records that a
			// way to sign in was replaced, so release is refused.
			return fmt.Errorf("%w: the account has no password and no attested linked identity", ErrReleaseNotReady)
		}
		if err := az.ReleasePrincipalContainment(ctx, target, inc.ID); err != nil {
			return err
		}
		e, err := incidentEvent(ctx, audit.EventIncidentPrincipalReleased, caller.Principal, inc.ID, audit.OutcomeSuccess,
			audit.Payload{"principal": string(target)})
		if err != nil {
			return err
		}
		return az.RecordAuthEvent(ctx, e)
	})
}

// authorizeReleaseTarget applies containment's own classification to a release:
// an instance-capability target answers not found, one inside exactly one org
// needs the incident capability there, and any other needs instance scope. The
// grants that classify the target are still read while it is contained.
func authorizeReleaseTarget(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, target domain.PrincipalID) error {
	grants, err := az.GrantsForResetTarget(ctx, target)
	if err != nil {
		return err
	}
	footprint, err := az.PrincipalFootprintOrgs(ctx, target)
	if err != nil {
		return err
	}
	org, instanceCap, orgCount := classifyResetTarget(grants, footprint)
	if instanceCap {
		return domain.ErrNotFound
	}
	owning := domain.OrgID("")
	if orgCount == 1 {
		owning = domain.OrgID(org)
	}
	if _, err := authorizeIncidentOp(ctx, az, caller, owning); err != nil {
		if errors.Is(err, domain.ErrUnauthorized) {
			return domain.ErrNotFound
		}
		return err
	}
	return nil
}

// otherHolderRefusal names the other open incident only to a caller who can read
// it, so the refusal is no existence oracle for an incident outside their scope.
func otherHolderRefusal(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, other string) error {
	if o, err := az.IncidentByID(ctx, other); err == nil {
		// A probe that records nothing: authorizing would write a false denial into the
		// other org's tenant audit.
		var holds bool
		if o.OrgID == "" {
			holds, err = az.HoldsInstanceCapability(ctx, caller, authz.OpIncidentInstance)
		} else {
			holds, err = az.CallerHolds(ctx, caller, authz.OpIncidentOrg, domain.Scope{Org: o.OrgID})
		}
		if err != nil {
			return err
		}
		if holds {
			return fmt.Errorf("%w: incident %s also recorded this principal contained and is still open", ErrReleaseNotReady, other)
		}
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	return fmt.Errorf("%w: another open incident also recorded this principal contained", ErrReleaseNotReady)
}

// identitiesNotAttested reads the target's own linked identities and counts the
// ones the incident has no attested item for, so the condition holds for an org
// incident as well as an account one. An unattested enumeration-truncated row
// keyed to the target counts too (the cut means identities went unlisted), and
// an attested one clears the hold but never covers an identity: coverage is decided
// from the ledger alone, with no clock, so an identity with no item is always pending
// (reopen the incident to list it, then attest it). total is how many identities the
// account holds. containOpen reports an account.contain item for the target that is
// still open.
func identitiesNotAttested(ctx context.Context, az *authz.TxAuthorizer, incidentID string, target domain.PrincipalID) (pending, total int, containOpen bool, err error) {
	accountID, err := az.ContainedAccountID(ctx, target)
	if err != nil {
		return 0, 0, false, err
	}
	identities, err := az.ExternalIdentitiesForAccount(ctx, accountID)
	if err != nil {
		return 0, 0, false, err
	}
	attested, listed, cutPending := map[string]bool{}, map[string]bool{}, false
	after := ""
	for {
		page, err := az.IncidentItems(ctx, incidentID, after, releasePageSize, false)
		if err != nil {
			return 0, 0, false, err
		}
		for _, it := range page {
			if it.OwnerFunction == domain.OwnerAccountContain && it.ObjectID == string(target) && it.Outcome.Open() {
				containOpen = true
			}
			if it.ObjectKind != domain.ObjectKindExternalIdentity && it.OwnerFunction != domain.OwnerExternalIdentity {
				continue
			}
			switch {
			case it.ObjectKind == domain.ObjectKindEnumeration: // attesting rewrites the reason, so the kind marks the cut
				// A cut keyed to another member leaves this target's identities listed.
				if it.ObjectID != string(target) {
					continue
				}
				if it.Outcome != domain.ItemAttested {
					cutPending = true
				}
			default:
				listed[it.ObjectID] = true
				attested[it.ObjectID] = it.Outcome == domain.ItemAttested
			}
		}
		if len(page) < releasePageSize {
			break
		}
		after = page[len(page)-1].ID
	}
	// An identity needs its own attested ledger item. One with no item (listed past a
	// cut, or linked after the enumeration) is pending whatever any clock reads.
	for _, x := range identities {
		if !listed[x.ID] || !attested[x.ID] {
			pending++
		}
	}
	if cutPending {
		pending++
	}
	return pending, len(identities), containOpen, nil
}
