package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mail"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// EmailInvitationLifetime is how long an email invitation stays pending
// (docs/spec/email-invitations.md D5, the ops-spec invitation expiry).
const EmailInvitationLifetime = 7 * 24 * time.Hour

// emailInvitationOutcomeTimeout bounds the outcome write of one send, which
// also runs while the process is stopping.
const emailInvitationOutcomeTimeout = 5 * time.Second

// MailSends is the owner-level home of the background sends. It outlives the
// generations that start them, so a replaced generation's late send is still
// joined, cancelled and counted. Concurrency is bounded by the per-organisation
// send budget, never by a drop.
type MailSends struct {
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
	failed atomic.Int64
}

// NewMailSends returns an empty set of background sends.
func NewMailSends() *MailSends {
	ctx, cancel := context.WithCancel(context.Background())
	return &MailSends{ctx: ctx, cancel: cancel}
}

// Failures is the number of sends that failed or were cancelled.
func (m *MailSends) Failures() float64 { return float64(m.failed.Load()) }

// Wait blocks until every send started so far has recorded its outcome.
func (m *MailSends) Wait() { m.wg.Wait() }

// Stop cancels the in-flight mail attempts, then waits for their failure
// outcomes to be recorded.
func (m *MailSends) Stop() {
	m.cancel()
	m.wg.Wait()
}

// EmailInvitations owns the email invitation of an existing account (#1207).
// The send never contacts the mail server inside the request: it writes the
// same rows whether or not the address matched an account, and one background
// task follows the commit for either: a mail attempt for a match, an outcome
// stating that no mail was sent otherwise.
type EmailInvitations struct {
	DB  *store.DB
	Now func() time.Time
	// ExternalOrigin builds the acceptance link.
	ExternalOrigin string
	// Mail captures the active runtime configuration; it is both the mailer
	// predicate and the transport.
	Mail   func(context.Context) (*runtimeconfig.Bundle, error)
	Budget *Budget
	// Sends is the owner-level set of background sends; required.
	Sends *MailSends
	// Log reports an outcome that could not be recorded; nil uses slog.Default.
	Log *slog.Logger
}

func (s *EmailInvitations) now() time.Time { return nowOr(s.Now) }

// EmailInvitationSendSpec is one send.
type EmailInvitationSendSpec struct {
	Org      domain.OrgID
	Email    string
	Template domain.Template
}

// EmailInvitationReceipt is what a send answers: only what the administrator
// typed plus the row's own facts.
type EmailInvitationReceipt struct {
	ID        string
	Email     string
	Template  domain.Template
	CreatedAt time.Time
	ExpiresAt time.Time
}

// EmailInvitationEntry is one pending row as the administrator sees it.
type EmailInvitationEntry struct {
	ID        string
	Email     string
	Template  domain.Template
	InvitedBy domain.PrincipalID
	CreatedAt time.Time
	ExpiresAt time.Time
}

// EmailInvitationOffer is what a valid token offers its holder.
type EmailInvitationOffer struct {
	Org       domain.OrgID
	OrgName   string
	Template  domain.Template
	ExpiresAt time.Time
}

// EmailInvitationAcceptance is the outcome of an acceptance. SignedOut is true
// when grants were created: the person's generation advanced and every session
// of theirs ended, the current one included.
type EmailInvitationAcceptance struct {
	Org           domain.OrgID
	OrgName       string
	Template      domain.Template
	GrantsCreated int
	SignedOut     bool
}

// Send records an invitation and, for a matched account, schedules one mail.
func (s *EmailInvitations) Send(ctx context.Context, actor Actor, spec EmailInvitationSendSpec) (EmailInvitationReceipt, error) {
	scope := domain.Scope{Org: spec.Org}
	err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		_, _, err := authorize(ctx, az, actor, authz.OpEmailInvitationSend, scope, s.now())
		return err
	})
	if err != nil {
		return EmailInvitationReceipt{}, err
	}
	if s.Sends == nil {
		return EmailInvitationReceipt{}, errors.New("email invitations: no background send set configured")
	}
	// The mailer predicate precedes the address so an unconfigured instance
	// answers every request alike.
	if s.Mail == nil {
		return EmailInvitationReceipt{}, preconditionRefusal(PreconditionMailerUnconfigured, "")
	}
	configured, err := mailPredicate(s.Mail)(ctx)
	if err != nil {
		return EmailInvitationReceipt{}, err
	}
	if !configured {
		return EmailInvitationReceipt{}, preconditionRefusal(PreconditionMailerUnconfigured, "")
	}
	email, err := domain.CanonicalEmail(spec.Email)
	if err != nil {
		return EmailInvitationReceipt{}, err
	}
	if _, err := domain.ExpandTemplate(spec.Template, domain.LevelOrg); err != nil {
		return EmailInvitationReceipt{}, fmt.Errorf("%w: %s", domain.ErrInvalid, err)
	}
	// Charged after authorization and before any write, whatever the address.
	if _, err := s.Budget.acquire(budgetEmailInvitation, budgetKeys{Org: spec.Org}); err != nil {
		return EmailInvitationReceipt{}, err
	}
	token, verifier, err := crypto.NewArtifact(crypto.ArtifactEmailInvitation)
	if err != nil {
		return EmailInvitationReceipt{}, err
	}
	candidateID, err := newID("ein")
	if err != nil {
		return EmailInvitationReceipt{}, err
	}
	var (
		receipt EmailInvitationReceipt
		matched bool
		intent  string
		orgName string
	)
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		caller, p, err := authorize(ctx, az, actor, authz.OpEmailInvitationSend, scope, now)
		if err != nil {
			return err
		}
		live, err := az.CountLiveOrgEmailInvitations(ctx, spec.Org, now)
		if err != nil {
			return err
		}
		if live >= EmailInvitationPendingCap {
			return fmt.Errorf("%w: service: the organisation has %d pending email invitations", admission.ErrOverloaded, EmailInvitationPendingCap)
		}
		matched = false
		account, err := az.AccountByEmail(ctx, email)
		switch {
		case err == nil:
			matched = true
		case errors.Is(err, domain.ErrNotFound):
			account = authz.Account{}
		default:
			return err
		}
		if orgName, err = az.OrgEmailInvitationOrgName(ctx, spec.Org); err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		expires := now.Add(EmailInvitationLifetime)
		id, err := az.UpsertOrgEmailInvitation(ctx, authz.EmailInvitation{
			ID: candidateID, OrgID: spec.Org, Email: email, Template: string(spec.Template),
			InviterPrincipalID: caller.Principal, AccountID: account.ID,
			TokenVerifier: verifier, CredentialEpoch: epoch, CreatedAt: now, ExpiresAt: expires,
		})
		if err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventMemberEmailInvited, caller.Principal,
			audit.Object{Type: "email-invitation", ID: id},
			audit.Payload{
				"invitation_id": id, "email": audit.SanitizeFreeText(email),
				"template": string(spec.Template), "expires_at": audit.FormatTime(expires),
			})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return err
		}
		// The dispatch intent is written for every send so the writes do not
		// differ with the match; only the mail attempt that follows does.
		dispatch, err := newAuditEvent(ctx, audit.EventMemberEmailInvitationDispatch, caller.Principal,
			audit.Object{Type: "email-invitation", ID: id}, audit.OutcomeIntent, "",
			audit.Payload{"invitation_id": id, "org_id": string(spec.Org), "matched": matched})
		if err != nil {
			return err
		}
		intent = dispatch.ID
		receipt = EmailInvitationReceipt{ID: id, Email: email, Template: spec.Template, CreatedAt: now, ExpiresAt: expires}
		return az.RecordAuthEvent(ctx, dispatch)
	})
	if err != nil {
		return EmailInvitationReceipt{}, err
	}
	s.dispatch(ctx, receipt, intent, orgName, token, matched)
	return receipt, nil
}

var emailInvitationMail = template.Must(template.ParseFS(signupMailTemplates, "mail_templates/email-invitation.txt"))

// dispatch starts the one background attempt that closes the dispatch intent,
// for every send. A matched account is mailed; an unmatched address is never
// dialled and its outcome states that no mail was sent. The request has already
// been answered by the time it matters: a lost send is recorded and counted,
// never reported to the administrator.
func (s *EmailInvitations) dispatch(ctx context.Context, receipt EmailInvitationReceipt, intent, orgName, token string, matched bool) {
	sends := s.Sends
	detached := context.WithoutCancel(ctx)
	sends.wg.Add(1)
	go func() {
		defer sends.wg.Done()
		sent := false
		var sendErr error
		if matched {
			// Every send gets its own attempt: a matched send in flight changes
			// nothing about whether others are attempted. The per-organisation
			// send budget, charged before the transaction, bounds how many.
			sendCtx, cancel := context.WithTimeout(detached, mail.SendTimeout)
			stopCancel := context.AfterFunc(sends.ctx, cancel)
			sendErr = s.deliver(sendCtx, receipt, orgName, token)
			stopCancel()
			cancel()
			sent = sendErr == nil
		}
		if sendErr != nil {
			sends.failed.Add(1)
		}
		outcomeCtx, stop := context.WithTimeout(detached, emailInvitationOutcomeTimeout)
		defer stop()
		err := tx.Write(outcomeCtx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			status := audit.OutcomeSuccess
			if sendErr != nil {
				status = audit.OutcomeFailure
			}
			ev, err := newAuditEvent(ctx, audit.EventMemberEmailInvitationMailResult, "",
				audit.Object{Type: "mail-intent", ID: intent}, status, "",
				audit.Payload{"intent_id": intent, "invitation_id": receipt.ID, "mail_sent": sent})
			if err != nil {
				return err
			}
			return az.RecordAuthEvent(ctx, ev)
		})
		if err != nil {
			log := s.Log
			if log == nil {
				log = slog.Default()
			}
			log.Error("email invitation mail outcome not recorded", "intent", intent, "err", err)
		}
	}()
}

func (s *EmailInvitations) deliver(ctx context.Context, receipt EmailInvitationReceipt, orgName, token string) error {
	bundle, err := s.Mail(ctx)
	if err != nil {
		return err
	}
	if !bundle.MailConfigured() {
		return mail.ErrDisabled
	}
	link := strings.TrimRight(s.ExternalOrigin, "/") + "/invitations/accept#" + url.Values{"token": {token}}.Encode()
	var body bytes.Buffer
	if err := emailInvitationMail.Execute(&body, struct{ Org, Link, Expires string }{
		Org: orgName, Link: link, Expires: receipt.ExpiresAt.UTC().Format("2006-01-02 15:04 UTC"),
	}); err != nil {
		return err
	}
	return bundle.Send(ctx, receipt.Email, "You are invited to join "+orgName+" on Hikyo", body.String())
}

// List returns the live pending invitations. The matched account is never part
// of the answer.
func (s *EmailInvitations) List(ctx context.Context, actor Actor, org domain.OrgID) ([]EmailInvitationEntry, error) {
	var out []EmailInvitationEntry
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		caller, p, err := authorize(ctx, az, actor, authz.OpEmailInvitationList, domain.Scope{Org: org}, now)
		if err != nil {
			return err
		}
		rows, err := az.ListLiveOrgEmailInvitations(ctx, org, now)
		if err != nil {
			return err
		}
		out = out[:0]
		for _, row := range rows {
			out = append(out, EmailInvitationEntry{
				ID: row.ID, Email: row.Email, Template: domain.Template(row.Template),
				InvitedBy: row.InviterPrincipalID, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt,
			})
		}
		ev, err := domainEvent(ctx, audit.EventGrantMembershipRead, caller.Principal,
			audit.Object{Type: "scope", ID: renderScope(domain.Scope{Org: org})},
			audit.Payload{"scope": renderScope(domain.Scope{Org: org}), "row_count": int64(len(out))})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
	return out, err
}

// Revoke deletes one pending invitation of the organisation.
func (s *EmailInvitations) Revoke(ctx context.Context, actor Actor, org domain.OrgID, id string) error {
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpEmailInvitationRevoke, domain.Scope{Org: org}, s.now())
		if err != nil {
			return err
		}
		row, err := az.OrgEmailInvitationInOrg(ctx, id, org)
		if err != nil {
			return err
		}
		gone, err := az.DeleteOrgEmailInvitationInOrg(ctx, id, org)
		if err != nil {
			return err
		}
		if !gone {
			return domain.ErrNotFound
		}
		ev, err := domainEvent(ctx, audit.EventMemberEmailInvitationRevoked, caller.Principal,
			audit.Object{Type: "email-invitation", ID: id},
			audit.Payload{"invitation_id": id, "email": audit.SanitizeFreeText(row.Email)})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
}

// emailInvitationRefusal is the recorded cause behind the one uniform 404.
type emailInvitationRefusal struct {
	cause, step, invitation string
	org                     domain.OrgID
}

// resolveEmailInvitation applies the validity rule shared by preview and
// accept. A nil refusal means the row is the session's own live invitation.
// The error is a fault, never a refusal.
func (s *EmailInvitations) resolveEmailInvitation(ctx context.Context, az *authz.TxAuthorizer, actor Actor, token, step string, now time.Time) (authz.Identity, authz.EmailInvitation, *emailInvitationRefusal, error) {
	refuse := func(cause string, row authz.EmailInvitation) *emailInvitationRefusal {
		return &emailInvitationRefusal{cause: cause, step: step, invitation: row.ID, org: row.OrgID}
	}
	identity, err := actor.resolveSelf(ctx, az, now)
	if err != nil {
		return identity, authz.EmailInvitation{}, nil, err
	}
	if (identity.Class != "" && identity.Class != domain.ClassHuman) || identity.Artifact == authz.WorkspaceArtifact {
		return identity, authz.EmailInvitation{}, refuse("session", authz.EmailInvitation{}), nil
	}
	if crypto.ParseArtifact(token, crypto.ArtifactEmailInvitation) != nil {
		return identity, authz.EmailInvitation{}, refuse("malformed", authz.EmailInvitation{}), nil
	}
	row, err := az.OrgEmailInvitationByVerifier(ctx, crypto.ArtifactVerifier(token))
	if errors.Is(err, domain.ErrNotFound) {
		return identity, row, refuse("unknown", authz.EmailInvitation{}), nil
	}
	if err != nil {
		return identity, row, nil, err
	}
	epoch, err := az.CredentialEpoch(ctx)
	if err != nil {
		return identity, row, nil, err
	}
	switch {
	case !now.Before(row.ExpiresAt):
		return identity, row, refuse("expired", row), nil
	case row.CredentialEpoch != epoch:
		return identity, row, refuse("epoch-superseded", row), nil
	case row.AccountID == "":
		return identity, row, refuse("unmatched", row), nil
	}
	account, err := az.AccountByPrincipal(ctx, identity.Principal)
	if errors.Is(err, domain.ErrNotFound) || err == nil && account.ID != row.AccountID {
		return identity, row, refuse("other-account", row), nil
	}
	return identity, row, nil, err
}

// recordEmailInvitationRefusal writes the cause in its own transaction so the
// refused request's rollback does not take it along.
func (s *EmailInvitations) recordRefusal(ctx context.Context, ref *emailInvitationRefusal) error {
	return tx.Write(context.WithoutCancel(ctx), s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		payload := audit.Payload{"cause": ref.cause, "step": ref.step}
		if ref.invitation != "" {
			payload["invitation_id"] = ref.invitation
		}
		if ref.org != "" {
			payload["org_id"] = string(ref.org)
		}
		ev, err := newAuditEvent(ctx, audit.EventMemberEmailInvitationRefused, "",
			audit.Object{Type: "email-invitation", ID: ref.invitation}, audit.OutcomeFailure, "", payload)
		if err != nil {
			return err
		}
		return az.RecordAuthEvent(ctx, ev)
	})
}

// checkInviter is the one side-effect-free test that the inviter still holds
// the authority, shared by preview and accept. It records nothing, so no
// outcome reaches the organisation's trail.
func checkInviter(ctx context.Context, az *authz.TxAuthorizer, row authz.EmailInvitation, step string) (*emailInvitationRefusal, error) {
	holds, err := az.RegistrationAuthorityHolds(ctx, row.InviterPrincipalID, authz.OpEmailInvitationAccept, domain.Scope{Org: row.OrgID})
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	if err != nil || !holds {
		return &emailInvitationRefusal{cause: "inviter-unauthorized", step: step, invitation: row.ID, org: row.OrgID}, nil
	}
	return nil, nil
}

// Preview shows what a token offers the signed-in person. It writes nothing
// except the cause of a refusal.
func (s *EmailInvitations) Preview(ctx context.Context, actor Actor, token string) (EmailInvitationOffer, error) {
	var (
		offer EmailInvitationOffer
		ref   *emailInvitationRefusal
	)
	err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		offer, ref = EmailInvitationOffer{}, nil
		now := s.now()
		_, row, refusal, err := s.resolveEmailInvitation(ctx, az, actor, token, "preview", now)
		if err != nil || refusal != nil {
			ref = refusal
			return err
		}
		if ref, err = checkInviter(ctx, az, row, "preview"); err != nil || ref != nil {
			return err
		}
		name, err := az.OrgEmailInvitationOrgName(ctx, row.OrgID)
		if errors.Is(err, domain.ErrNotFound) {
			ref = &emailInvitationRefusal{cause: "org-gone", step: "preview", invitation: row.ID, org: row.OrgID}
			return nil
		}
		if err != nil {
			return err
		}
		offer = EmailInvitationOffer{Org: row.OrgID, OrgName: name, Template: domain.Template(row.Template), ExpiresAt: row.ExpiresAt}
		return nil
	})
	if err != nil {
		return EmailInvitationOffer{}, err
	}
	if ref != nil {
		if err := s.recordRefusal(ctx, ref); err != nil {
			return EmailInvitationOffer{}, err
		}
		return EmailInvitationOffer{}, domain.ErrNotFound
	}
	return offer, nil
}

// Accept applies the invitation's template to the signed-in person, as the
// inviter, and consumes the row, in one transaction.
func (s *EmailInvitations) Accept(ctx context.Context, actor Actor, token string) (EmailInvitationAcceptance, error) {
	var (
		out EmailInvitationAcceptance
		ref *emailInvitationRefusal
	)
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		out, ref = EmailInvitationAcceptance{}, nil
		now := s.now()
		identity, row, refusal, err := s.resolveEmailInvitation(ctx, az, actor, token, "accept", now)
		if err != nil || refusal != nil {
			ref = refusal
			return nonNilRollback(err, refusal)
		}
		scope := domain.Scope{Org: row.OrgID}
		name, err := az.OrgEmailInvitationOrgName(ctx, row.OrgID)
		if errors.Is(err, domain.ErrNotFound) {
			ref = &emailInvitationRefusal{cause: "org-gone", step: "accept", invitation: row.ID, org: row.OrgID}
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}
		// The same side-effect-free check as Preview: a lost inviter must not
		// write a grant.denied to the organisation's trail, which only the
		// bound account's own token could trigger.
		if ref, err = checkInviter(ctx, az, row, "accept"); err != nil || ref != nil {
			return nonNilRollback(err, ref)
		}
		inviter := authz.Identity{Principal: row.InviterPrincipalID}
		az.SetClock(now)
		p, err := az.Authorize(ctx, inviter, authz.OpEmailInvitationAccept, scope)
		if err != nil {
			return err
		}
		consumed, err := az.ConsumeOrgEmailInvitation(ctx, row.ID, row.TokenVerifier)
		if err != nil {
			return err
		}
		if !consumed {
			ref = &emailInvitationRefusal{cause: "consumed", step: "accept", invitation: row.ID, org: row.OrgID}
			return domain.ErrNotFound
		}
		grants := &Grants{DB: s.DB, Now: func() time.Time { return now }, invitee: identity.Principal}
		results, err := grants.applyTemplate(ctx, r, az, p, inviter, domain.Template(row.Template), identity.Principal, scope, domain.LevelOrg)
		if err != nil {
			return err
		}
		created := 0
		for _, res := range results {
			if res.Outcome == GrantCreated() {
				created++
			}
		}
		ev, err := domainEvent(ctx, audit.EventMemberEmailInvitationAccepted, identity.Principal,
			audit.Object{Type: "email-invitation", ID: row.ID},
			audit.Payload{
				"invitation_id": row.ID, "principal_id": string(identity.Principal), "account_id": row.AccountID,
				"template": row.Template, "grants_created": created,
			})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return err
		}
		out = EmailInvitationAcceptance{
			Org: row.OrgID, OrgName: name, Template: domain.Template(row.Template),
			GrantsCreated: created, SignedOut: created > 0,
		}
		return nil
	})
	if ref != nil {
		if rerr := s.recordRefusal(ctx, ref); rerr != nil {
			return EmailInvitationAcceptance{}, rerr
		}
		return EmailInvitationAcceptance{}, domain.ErrNotFound
	}
	if err != nil {
		return EmailInvitationAcceptance{}, err
	}
	return out, nil
}

// nonNilRollback turns a refusal into the error that rolls the transaction
// back; a fault passes through unchanged.
func nonNilRollback(err error, refusal *emailInvitationRefusal) error {
	if err != nil {
		return err
	}
	if refusal != nil {
		return domain.ErrNotFound
	}
	return nil
}
