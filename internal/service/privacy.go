package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// PrivacyExport is a reviewed subject-access starting point, not an assertion
// that arbitrary secret payloads and third-party records have been inspected.
type PrivacyExport struct {
	Sessions    []authz.PrivacySession   `json:"sessions"`
	Version     int                      `json:"version"`
	ExportedAt  time.Time                `json:"exported_at"`
	Account     authz.PrivacyAccountView `json:"account"`
	Identities  []PrivacyIdentity        `json:"identities"`
	Grants      []domain.Grant           `json:"grants"`
	Activity    []authz.PrivacyActivity  `json:"activity"`
	Limitations []string                 `json:"limitations"`
}
type PrivacyIdentity struct {
	Kind      string    `json:"kind"`
	Issuer    string    `json:"issuer"`
	Subject   string    `json:"subject"`
	CreatedAt time.Time `json:"created_at"`
}

// PrivacyReceipt belongs outside the database backup retention boundary. Reapply
// each approved receipt to a restored database before restoring principal access.
type PrivacyReceipt struct {
	InstanceID  string    `json:"instance_id"`
	Version     int       `json:"version"`
	PrincipalID string    `json:"principal_id"`
	AccountID   string    `json:"account_id"`
	Action      string    `json:"action"`
	AppliedAt   time.Time `json:"applied_at"`
}

var ErrPrivacyLastManager = errors.New("privacy: assign another manage-members holder before restricting this principal")

// ExportPrivacySubject is reachable only under local host authority. It reads
// explicit subject predicates and cannot disclose stored values or verifiers.
func (s *Auth) ExportPrivacySubject(ctx context.Context, principal string) (PrivacyExport, error) {
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) (PrivacyExport, error) {
		out := PrivacyExport{Version: 1, ExportedAt: s.now(), Identities: []PrivacyIdentity{}, Limitations: []string{
			"Audit payloads, third-party references, arbitrary secret values, SCIM attributes, and external destinations require operator review; this export is not a complete subject-access response.",
			"Audit activity includes records where this principal is the actor; records naming the subject elsewhere require reviewed audit search.",
		}}
		a, err := az.PrivacyAccount(ctx, principal)
		if err != nil {
			return PrivacyExport{}, err
		}
		out.Account = a
		ids, err := az.ExternalIdentitiesForAccount(ctx, a.ID)
		if err != nil {
			return PrivacyExport{}, err
		}
		for _, i := range ids {
			out.Identities = append(out.Identities, PrivacyIdentity{i.Kind, i.Issuer, i.Subject, i.CreatedAt})
		}
		out.Grants, err = az.GrantsForResetTarget(ctx, domain.PrincipalID(principal))
		if err != nil {
			return PrivacyExport{}, err
		}
		out.Sessions, err = az.PrivacySessions(ctx, principal)
		if err != nil {
			return PrivacyExport{}, err
		}
		out.Activity, err = az.PrivacyActivity(ctx, principal)
		if err != nil {
			return PrivacyExport{}, err
		}
		if err := privacyEvent(ctx, az, a, audit.EventPrivacySubjectExported); err != nil {
			return PrivacyExport{}, err
		}
		return out, nil
	})
}

// ApplyPrivacySubject permanently restricts login/authorization. Erasure also
// destroys authentication custody and replaces direct identity with a tombstone.
// expectedAccount is required when reapplying a receipt, preventing an accidental
// receipt/target mismatch. Empty expectedAccount is the first local application.
func (s *Auth) ApplyPrivacySubject(ctx context.Context, principal, action, expectedAccount string) (PrivacyReceipt, error) {
	if principal == "" || (action != "restrict" && action != "erase" && action != "release") {
		return PrivacyReceipt{}, errors.New("privacy: principal and restrict, erase or release action required")
	}
	var out PrivacyReceipt
	err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		if err := az.LockTargetPrincipal(ctx, domain.PrincipalID(principal)); err != nil {
			return err
		}
		a, err := az.PrivacyAccount(ctx, principal)
		if err != nil {
			return err
		}
		if expectedAccount != "" && expectedAccount != a.ID {
			return errors.New("privacy: receipt account does not match principal")
		}
		grants, err := az.GrantsForResetTarget(ctx, domain.PrincipalID(principal))
		if err != nil {
			return err
		}
		if action != "erase" && a.State == "erased" {
			return errors.New("privacy: erased accounts cannot be released")
		}
		if a.State == "active" && action != "release" {
			seen := map[string]bool{}
			for _, g := range grants {
				if g.Capability != domain.CapManageMembers || g.Scope.Project != "" {
					continue
				}
				org := string(g.Scope.Org)
				if seen[org] {
					continue
				}
				seen[org] = true
				holders, err := az.ManageMembersHolders(ctx, org)
				if err != nil {
					return err
				}
				slices.Sort(holders)
				for _, h := range holders {
					if err := az.LockTargetPrincipal(ctx, h); err != nil {
						return err
					}
				}
				holders, err = az.ManageMembersHolders(ctx, org)
				if err != nil {
					return err
				}
				alternative := false
				for _, p := range holders {
					if string(p) != principal {
						alternative = true
					}
				}
				if !alternative {
					return ErrPrivacyLastManager
				}
			}
		}
		state := "restricted"
		typ := audit.EventPrivacySubjectRestricted
		if action == "release" {
			state = "active"
			typ = audit.EventPrivacySubjectReleased
		}
		if action == "erase" {
			state = "erased"
			typ = audit.EventPrivacySubjectErased
		}
		if err := az.RestrictPrivacyPrincipal(ctx, principal, state); err != nil {
			return err
		}
		if err := az.RevokeAllSessionsFor(ctx, domain.PrincipalID(principal)); err != nil {
			return err
		}
		if action == "erase" {
			// Random tombstone prevents collisions with a deliberately registered login
			// handle. It contains no source username or external identity information.
			tombstone, err := newID("erased")
			if err != nil {
				return err
			}
			if err := az.ErasePrivacyAccount(ctx, a.ID, principal, tombstone); err != nil {
				return fmt.Errorf("privacy erasure: %w", err)
			}
		}
		if err := privacyEvent(ctx, az, a, typ); err != nil {
			return err
		}
		instance, err := az.InstanceIdentity(ctx)
		if err != nil {
			return err
		}
		out = PrivacyReceipt{InstanceID: instance, Version: 1, PrincipalID: principal, AccountID: a.ID, Action: action, AppliedAt: s.now()}
		return nil
	})
	return out, err
}
func privacyEvent(ctx context.Context, az *authz.TxAuthorizer, a authz.PrivacyAccountView, typ audit.EventType) error {
	e, err := newAuditEvent(ctx, typ, "", audit.Object{Type: "account", ID: a.ID}, audit.OutcomeSuccess, "", audit.Payload{"target_principal": a.PrincipalID, "authority": "local-host"})
	if err != nil {
		return err
	}
	e.Actor.Class = audit.ActorBreakGlass
	e.Origin = audit.OriginCLI
	return az.RecordAuthEvent(ctx, e)
}

// ReapplyPrivacyReceipt checks the source instance before any mutation. The
// caller must keep the restored server isolated until all receipts are replayed.
func (s *Auth) ReapplyPrivacyReceipt(ctx context.Context, receipt PrivacyReceipt) (PrivacyReceipt, error) {
	if receipt.Version != 1 || receipt.InstanceID == "" || receipt.AccountID == "" || receipt.PrincipalID == "" || receipt.AppliedAt.IsZero() || (receipt.Action != "erase" && receipt.Action != "restrict") {
		return PrivacyReceipt{}, errors.New("privacy: invalid receipt")
	}
	err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		instance, err := az.InstanceIdentity(ctx)
		if err != nil {
			return err
		}
		if instance != receipt.InstanceID {
			return errors.New("privacy: receipt belongs to another instance")
		}
		return nil
	})
	if err != nil {
		return PrivacyReceipt{}, err
	}
	return s.ApplyPrivacySubject(ctx, receipt.PrincipalID, receipt.Action, receipt.AccountID)
}

// CorrectPrivacySubject rectifies local account labels without changing identity
// provider subject keys. All sessions are revoked so clients refresh identity.
func (s *Auth) CorrectPrivacySubject(ctx context.Context, principal, username, displayName string) (PrivacyReceipt, error) {
	if principal == "" || strings.TrimSpace(username) != username || username == "" || len(username) > 256 || len(displayName) > 256 || !utf8.ValidString(username) || !utf8.ValidString(displayName) {
		return PrivacyReceipt{}, errors.New("privacy: username required, labels must be valid UTF-8 and at most 256 bytes, username must have no surrounding whitespace")
	}
	for _, value := range []string{username, displayName} {
		for _, r := range value {
			if unicode.IsControl(r) {
				return PrivacyReceipt{}, errors.New("privacy: identity labels must not contain control characters")
			}
		}
	}
	var out PrivacyReceipt
	err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		if err := az.LockTargetPrincipal(ctx, domain.PrincipalID(principal)); err != nil {
			return err
		}
		account, err := az.PrivacyAccount(ctx, principal)
		if err != nil {
			return err
		}
		if account.State == "erased" {
			return errors.New("privacy: erased accounts cannot be corrected")
		}
		if err := az.CorrectPrivacyAccount(ctx, account.ID, username, displayName); err != nil {
			return err
		}
		if err := az.AdvanceGeneration(ctx, domain.PrincipalID(principal)); err != nil {
			return err
		}
		if err := az.RevokeAllSessionsFor(ctx, domain.PrincipalID(principal)); err != nil {
			return err
		}
		if err := privacyEvent(ctx, az, account, audit.EventPrivacySubjectCorrected); err != nil {
			return err
		}
		instance, err := az.InstanceIdentity(ctx)
		if err != nil {
			return err
		}
		out = PrivacyReceipt{InstanceID: instance, Version: 1, PrincipalID: principal, AccountID: account.ID, Action: "correct", AppliedAt: s.now()}
		return nil
	})
	return out, err
}
