package service

import (
	"context"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mail"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// InstanceMail uses the active runtime bundle, including managed-source fences.
// Test sends share the self-configuration test's installation-wide lease/bucket.
type InstanceMail struct {
	DB      *store.DB
	Auth    *Auth
	Capture func(context.Context) (*runtimeconfig.Bundle, error)
}

func (s *InstanceMail) Configured(ctx context.Context, actor Actor) (bool, error) {
	var configured bool
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpMailGet, domain.Scope{}, time.Now().UTC())
		if err != nil {
			return err
		}
		configured, err = mailPredicate(s.Capture)(ctx)
		if err != nil {
			return err
		}
		ev, err := newAuditEvent(ctx, audit.EventRegistrationMailStatusRead, caller.Principal, audit.Object{Type: "mailer"}, audit.OutcomeSuccess, "", audit.Payload{"configured": configured})
		if err != nil {
			return err
		}
		return r.Audit().InsertInstance(ctx, p, ev)
	})
	return configured, err
}

func (s *InstanceMail) Test(ctx context.Context, actor Actor, to, proof string) error {
	address, err := domain.CanonicalEmail(to)
	if err != nil {
		return domain.ErrInvalid
	}
	if s.Auth == nil || s.Capture == nil {
		return ErrSelfConfigUnavailable
	}
	var principal domain.PrincipalID
	err = tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		caller, _, err := authorize(ctx, az, actor, authz.OpMailTest, domain.Scope{}, time.Now().UTC())
		principal = caller.Principal
		return err
	})
	if err != nil {
		return err
	}
	evidence := ReauthEvidence{kind: reauthEvidenceExempt}
	if actor.bearer != "" {
		evidence, err = s.Auth.VerifyReauthProof(ctx, actor.bearer, proof)
		if err != nil {
			return err
		}
	}
	bundle, err := s.Capture(ctx)
	if err != nil {
		return err
	}
	if !bundle.MailConfigured() {
		return mail.ErrDisabled
	}
	coord := s.DB.Coordination()
	now, err := coord.Now(ctx)
	if err != nil {
		return err
	}
	id, err := newID("smt")
	if err != nil {
		return err
	}
	started := time.Now()
	fence, held, err := coord.ClaimLease(ctx, "self-config-mail-test", id, now, now.Add(mail.SendTimeout))
	if err != nil {
		return err
	}
	if !held {
		return &admission.RateLimitedError{Cause: ErrSelfConfigMailLimited, Wait: mail.SendTimeout}
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = coord.ReleaseLease(cleanup, "self-config-mail-test", id, fence)
	}()
	count, err := coord.BumpWindow(ctx, "mail-test", string(principal), now.Truncate(time.Hour))
	if err != nil {
		return err
	}
	if count > 5 {
		return &admission.RateLimitedError{Cause: ErrSelfConfigMailLimited, Wait: now.Truncate(time.Hour).Add(time.Hour).Sub(now)}
	}
	sendCtx, cancel := context.WithDeadline(ctx, started.Add(mail.SendTimeout))
	defer cancel()
	var intentID string
	err = tx.Write(sendCtx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpMailTest, domain.Scope{}, time.Now().UTC())
		if err != nil {
			return err
		}
		if evidence.kind != reauthEvidenceExempt {
			if err := s.Auth.ConsumeReauthEvidence(ctx, az, evidence, caller.Principal); err != nil {
				return err
			}
		}
		// Re-capture inside the authorized write so a restore fence cannot pass unnoticed.
		bundle, err = s.Capture(ctx)
		if err != nil {
			return err
		}
		if !bundle.MailConfigured() {
			return mail.ErrDisabled
		}
		ev, err := newAuditEvent(ctx, audit.EventRegistrationMailIntent, caller.Principal, audit.Object{Type: "mailer"}, audit.OutcomeIntent, "", audit.Payload{"kind": "test", "recipient": audit.SanitizeFreeText(address)})
		if err != nil {
			return err
		}
		intentID = ev.ID
		return r.Audit().InsertInstance(ctx, p, ev)
	})
	if err != nil {
		return err
	}
	deliveryErr := bundle.Send(sendCtx, address, "Hikyo mail test", "This test email was sent using the active Hikyo mail configuration.")
	outcomeCtx, outcomeCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer outcomeCancel()
	err = tx.Write(outcomeCtx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		status := audit.OutcomeSuccess
		if deliveryErr != nil {
			status = audit.OutcomeFailure
		}
		ev, err := newAuditEvent(ctx, audit.EventRegistrationMailOutcome, principal, audit.Object{Type: "mailer"}, status, "", audit.Payload{"intent_id": intentID})
		if err != nil {
			return err
		}
		return az.RecordAuthEvent(ctx, ev)
	})
	if err != nil {
		return err
	}
	if deliveryErr != nil {
		return mail.ErrDelivery
	}
	return nil
}
