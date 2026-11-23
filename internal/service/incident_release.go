package service

import (
	"context"
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
// establishment has completed since the containment was recorded, for an
// account with a local password, and (b) every linked identity of the account
// has an attested ledger item in the containing incident. It reactivates no
// revoked delegation or credential and regrants nothing: the store setter
// advances the session generation again so nothing minted before containment
// revives.
func (s *Incidents) ReleasePrincipal(ctx context.Context, actor Actor, incidentID string, target domain.PrincipalID) error {
	return tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		caller, inc, err := s.incidentByID(ctx, az, actor, incidentID, now)
		if err != nil {
			return err
		}
		// One answer for an unknown principal, one this incident did not
		// contain, and the incident's own account.
		contained, err := az.PrincipalContainedIncident(ctx, target)
		if err != nil || contained != inc.ID || ownIncident(caller, inc) {
			return domain.ErrNotFound
		}
		at, ok, err := az.PrincipalContainedAt(ctx, target)
		if err != nil {
			return err
		}
		if !ok {
			return domain.ErrNotFound
		}
		hasPassword, err := az.PrincipalHasPassword(ctx, target)
		if err != nil {
			return err
		}
		if hasPassword {
			established, err := az.CredentialEstablishedSince(ctx, target, at)
			if err != nil {
				return err
			}
			if !established {
				return fmt.Errorf("%w: no credential establishment has completed since containment", ErrReleaseNotReady)
			}
		}
		pending, total, err := identityItemsNotAttested(ctx, az, inc.ID)
		if err != nil {
			return err
		}
		switch {
		case pending > 0:
			return fmt.Errorf("%w: %d linked identity item(s) are not attested", ErrReleaseNotReady, pending)
		case !hasPassword && total == 0:
			// No password and no linked identity to attest: nothing records that a
			// way to sign in was replaced, so release is refused.
			return fmt.Errorf("%w: the account has no password and no attested linked identity", ErrReleaseNotReady)
		}
		if err := az.ReleasePrincipalContainment(ctx, target); err != nil {
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

// identityItemsNotAttested counts the incident's linked-identity items that are
// not attested, and how many it has.
func identityItemsNotAttested(ctx context.Context, az *authz.TxAuthorizer, incidentID string) (pending, total int, err error) {
	after := ""
	for {
		page, err := az.IncidentItems(ctx, incidentID, after, releasePageSize, false)
		if err != nil {
			return 0, 0, err
		}
		for _, it := range page {
			if it.ObjectKind != domain.ObjectKindExternalIdentity {
				continue
			}
			total++
			if it.Outcome != domain.ItemAttested {
				pending++
			}
		}
		if len(page) < releasePageSize {
			return pending, total, nil
		}
		after = page[len(page)-1].ID
	}
}
