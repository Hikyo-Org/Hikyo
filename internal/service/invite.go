package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// InviteSpec is one invitation (#568): WHERE (an organisation, or the
// instance), WHO (the local login handle) and, optionally, WHAT they start
// with (a role template expanded at that scope).
type InviteSpec struct {
	// Scope is the organisation invited into; the zero Scope is instance scope.
	Scope       domain.Scope
	Username    string
	DisplayName string
	// Template is optional: "" invites an account that can sign in and see
	// nothing — the human-auth ADR's "provisioning and authorizing are
	// separate acts" default.
	Template domain.Template
	// Delivery names how the caller hands the authority over. It is recorded
	// in the mint event because delivery mode IS the security property.
	Delivery string
}

// InvitationResult carries the one-time authority out to the caller. The value
// is in memory only and is never re-displayed: if it lapses, reset the
// credential (a second invitation of the same username is a conflict).
type InvitationResult struct {
	PrincipalID   domain.PrincipalID
	AccountID     string
	Authority     string
	AuthorityID   string
	ExpiresAt     time.Time
	GrantsCreated int
}

// InviteMember is the human-auth ADR's named account-creation path (#568):
// "accounts are created by invitation from a manage-members holder".
//
// It is bootstrap minus the first-account check, under an authorization
// instead of host authority: create the principal and account, expand the
// optional template through the SAME writer applyOrgTemplate uses (so the
// grants, their events and §2.4's cure are indistinguishable from an ordinary
// template application), then mint a credential-establishment authority
// exactly as credential-reset does — same artifact, same lifetime, same
// establish endpoint — with `invitation` as the recorded issuer.
//
// Everything commits in one transaction (ADR § Identity linking: "invitation
// consumption, account creation and any initial grants occur in ONE
// transaction"), so a username collision — the store's UNIQUE constraint,
// surfaced as domain.ErrConflict — leaves nothing behind.
func (s *Grants) InviteMember(ctx context.Context, actor Actor, spec InviteSpec) (InvitationResult, error) {
	username := strings.TrimSpace(spec.Username)
	if username == "" {
		return InvitationResult{}, fmt.Errorf("%w: a username is required", domain.ErrInvalid)
	}
	displayName := strings.TrimSpace(spec.DisplayName)
	if displayName == "" {
		displayName = username
	}
	level, err := spec.Scope.Level()
	if err != nil {
		return InvitationResult{}, fmt.Errorf("%w: %s", domain.ErrInvalid, err)
	}
	var op authz.Operation
	switch level {
	case domain.LevelNone:
		op = authz.OpMemberInviteInstance
	case domain.LevelOrg:
		op = authz.OpMemberInviteOrg
	default:
		// Membership is an organisation or instance fact; a project has no
		// accounts of its own, so a deeper scope is a malformed address, not
		// a narrower invitation.
		return InvitationResult{}, fmt.Errorf("%w: invitations are addressed at organisation or instance scope", domain.ErrInvalid)
	}
	value, verifier, err := crypto.NewArtifact(crypto.ArtifactBootstrap)
	if err != nil {
		return InvitationResult{}, err
	}
	principalID, err := newID("prn")
	if err != nil {
		return InvitationResult{}, err
	}
	accountID, err := newID("acc")
	if err != nil {
		return InvitationResult{}, err
	}
	authorityID, err := newID("cea")
	if err != nil {
		return InvitationResult{}, err
	}
	now := s.now()
	expires := now.Add(ResetLifetime)
	var out InvitationResult
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, op, spec.Scope, now)
		if err != nil {
			return err
		}
		target := domain.PrincipalID(principalID)
		if err := az.CreateHumanPrincipal(ctx, target, now); err != nil {
			return err
		}
		// A UNIQUE violation on the username arrives as domain.ErrConflict and
		// aborts the whole transaction — no orphan principal survives.
		if err := az.CreateAccount(ctx, authz.Account{
			ID: accountID, PrincipalID: target,
			Username: username, DisplayName: displayName, CreatedAt: now,
		}); err != nil {
			return err
		}
		grantsCreated := 0
		if spec.Template != "" {
			results, err := s.applyTemplate(ctx, r, az, p, caller, spec.Template, target, spec.Scope, level)
			if err != nil {
				return err
			}
			for _, res := range results {
				if res.Outcome == GrantCreated() {
					grantsCreated++
				}
			}
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if err := az.MintAuthority(ctx, authz.NewCredentialAuthority{
			ID: authorityID, Verifier: verifier, AccountID: accountID,
			Purpose: "establish-credential", IssuedBy: "invitation",
			CredentialEpoch: epoch, ExpiresAt: expires, CreatedAt: now,
		}); err != nil {
			return err
		}
		// The mint is its own instance-trail record, like every other
		// authority issuance (factors MEDIUM-7).
		minted, err := newAuditEvent(ctx, audit.EventAuthAuthorityMinted, target,
			audit.Object{Type: "authority", ID: authorityID}, audit.OutcomeSuccess, "",
			audit.Payload{
				"authority_id": authorityID, "account_id": accountID,
				"issued_by": "invitation", "delivery": spec.Delivery,
			})
		if err != nil {
			return err
		}
		if err := az.RecordAuthEvent(ctx, minted); err != nil {
			return err
		}
		// The invitation itself lives on the scope's trail, bound to this
		// operation's proof, so an organisation administrator can answer
		// "who invited whom" from the org trail alone.
		payload := audit.Payload{
			"principal_id":   principalID,
			"account_id":     accountID,
			"username":       audit.SanitizeFreeText(username),
			"scope":          renderScope(spec.Scope),
			"grants_created": grantsCreated,
			"authority_id":   authorityID,
			"delivery":       spec.Delivery,
		}
		if spec.Template != "" {
			payload["template"] = string(spec.Template)
		}
		invited, err := domainEvent(ctx, audit.EventMemberInvited, caller.Principal,
			audit.Object{Type: "account", ID: accountID}, payload)
		if err != nil {
			return err
		}
		if level == domain.LevelNone {
			if err := r.Audit().InsertInstance(ctx, p, invited); err != nil {
				return err
			}
		} else if err := r.Audit().InsertTenant(ctx, p, invited); err != nil {
			return err
		}
		out = InvitationResult{
			PrincipalID: target, AccountID: accountID, AuthorityID: authorityID,
			ExpiresAt: expires, GrantsCreated: grantsCreated,
		}
		return nil
	})
	if err != nil {
		return InvitationResult{}, err
	}
	out.Authority = value
	return out, nil
}
