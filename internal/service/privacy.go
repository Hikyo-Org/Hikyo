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
		// One sorted lock pass, shared with incident containment, so the two never
		// take the target and the managers in opposite orders; the grants it returns
		// were read before the locks, in this transaction's serializable snapshot. A
		// release only needs the target.
		var holders map[string][]domain.PrincipalID
		if action == "release" {
			if err := az.LockTargetPrincipal(ctx, domain.PrincipalID(principal)); err != nil {
				return err
			}
		} else {
			var err error
			if _, holders, err = lockTargetAndManagers(ctx, az, domain.PrincipalID(principal)); err != nil {
				return err
			}
		}
		a, err := az.PrivacyAccount(ctx, principal)
		if err != nil {
			return err
		}
		if expectedAccount != "" && expectedAccount != a.ID {
			return errors.New("privacy: receipt account does not match principal")
		}
		if action != "erase" && a.State == "erased" {
			return errors.New("privacy: erased accounts cannot be released")
		}
		if a.State == "active" && action != "release" {
			if !hasAlternativeManager(domain.PrincipalID(principal), holders) {
				return ErrPrivacyLastManager
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
		if action != "release" {
			if err := revokeDeveloperScope(ctx, az, "", domain.PrincipalID(principal), domain.Scope{}, "account-"+action, s.now()); err != nil {
				return err
			}
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
			deleted, err := az.ErasePrivacyAccount(ctx, a.ID, principal, tombstone)
			if err != nil {
				return fmt.Errorf("privacy erasure: %w", err)
			}
			if err := personalErasureEvent(ctx, az, principal, deleted); err != nil {
				return err
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

// personalErasureEvent records the personal values an erasure deleted, by
// count only: the erased subject keeps no key names.
func personalErasureEvent(ctx context.Context, az *authz.TxAuthorizer, principal string, deleted int64) error {
	if deleted == 0 {
		return nil
	}
	e, err := newAuditEvent(ctx, audit.EventPersonalValueDeleted, "", audit.Object{Type: "principal", ID: principal}, audit.OutcomeSuccess, "",
		audit.Payload{"owner_principal_id": principal, "cause": PersonalCauseErasure, "count": int(deleted)})
	if err != nil {
		return err
	}
	e.Actor.Class = audit.ActorBreakGlass
	e.Origin = audit.OriginCLI
	return az.RecordAuthEvent(ctx, e)
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
		if hasDirectionControl(value) {
			return PrivacyReceipt{}, errors.New("privacy: identity labels must not contain text direction controls")
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

// managerOrgs are the orgs in which the grants make the principal a manage-members
// holder, each once.
func managerOrgs(grants []domain.Grant) []string {
	var orgs []string
	for _, g := range grants {
		if g.Capability == domain.CapManageMembers && g.Scope.Project == "" && !slices.Contains(orgs, string(g.Scope.Org)) {
			orgs = append(orgs, string(g.Scope.Org))
		}
	}
	return orgs
}

// lockTargetAndManagers locks the target and every manage-members holder of each
// org the target manages, in one sorted pass, so two concurrent batches that touch
// the last managers of an org take their locks in the same order and cannot
// deadlock. It returns the grants and per-org holders it locked, which the caller's
// census (hasAlternativeManager) uses instead of reading again.
//
// Why the read before the lock is safe: write transactions run SERIALIZABLE on
// PostgreSQL (internal/store/tx) and serialized on SQLite. A grant or holder change
// committed after this transaction's snapshot cannot be observed by reading again
// under the lock (the snapshot is fixed); instead the read-write conflict with that
// writer aborts one of the two transactions, and tx.Write reruns the whole closure
// on a fresh snapshot. A stale census therefore never commits: the unsafe direction
// (a manager removed concurrently) fails serialization, and the other direction
// (a manager added concurrently) fails closed as a last-manager refusal.
// TestPrivacyConcurrentLastManagers exercises two concurrent last-manager restrictions.
func lockTargetAndManagers(ctx context.Context, az *authz.TxAuthorizer, target domain.PrincipalID) ([]domain.Grant, map[string][]domain.PrincipalID, error) {
	grants, err := az.GrantsForResetTarget(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	need := map[domain.PrincipalID]bool{target: true}
	byOrg := map[string][]domain.PrincipalID{}
	for _, org := range managerOrgs(grants) {
		holders, err := az.ManageMembersHolders(ctx, org)
		if err != nil {
			return nil, nil, err
		}
		byOrg[org] = holders
		for _, h := range holders {
			need[h] = true
		}
	}
	set := make([]domain.PrincipalID, 0, len(need))
	for p := range need {
		set = append(set, p)
	}
	slices.Sort(set)
	gone := map[domain.PrincipalID]bool{}
	for _, p := range set {
		// A holder whose principal row went away holds nothing (as in
		// remainingMemberManagers); the target itself must exist.
		if err := az.LockTargetPrincipal(ctx, p); errors.Is(err, domain.ErrNotFound) && p != target {
			gone[p] = true
		} else if err != nil {
			return nil, nil, err
		}
	}
	// A skipped holder must not count as a remaining manager.
	for org, holders := range byOrg {
		byOrg[org] = slices.DeleteFunc(holders, func(p domain.PrincipalID) bool { return gone[p] })
	}
	return grants, byOrg, nil
}

// hasAlternativeManager reports whether every org the target manages keeps another
// manage-members holder in the locked census lockTargetAndManagers returned.
// Contained and restricted principals are not holders (the reader excludes them),
// so the census is the one privacy restriction and incident containment share.
func hasAlternativeManager(target domain.PrincipalID, holdersByOrg map[string][]domain.PrincipalID) bool {
	for _, holders := range holdersByOrg {
		if !slices.ContainsFunc(holders, func(p domain.PrincipalID) bool { return p != target }) {
			return false
		}
	}
	return true
}
