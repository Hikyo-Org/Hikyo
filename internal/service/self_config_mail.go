package service

import (
	"context"
	"errors"
	stdmail "net/mail"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mail"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

var ErrSelfConfigMailLimited = errors.New("configuration test email rate or concurrency limit reached")

type SelfConfigMailTestRequest struct {
	Revision, ExpectedGeneration int64
	SchemaVersion                int
	To                           string
}
type SelfConfigMailTestResult struct {
	Revision int64
	Sent     bool
}

// Only TestMail can construct these records, after a committed intent and a
// real transport attempt. The app-owned worker audits the outcome even if the
// initiating session or its grants were revoked during the SMTP exchange.
type selfConfigMailOutcome struct {
	binding              store.SelfConfigBinding
	principal            domain.PrincipalID
	correlation          string
	revision, generation int64
	sent                 bool
	recorded             chan error
}

func (s *SelfConfig) outcomeQueue() chan selfConfigMailOutcome {
	s.mailOnce.Do(func() { s.mailOutcomes = make(chan selfConfigMailOutcome, 1) })
	return s.mailOutcomes
}

func (s *SelfConfig) runMailOutcomes(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case outcome := <-s.outcomeQueue():
			recordCtx, cancel := context.WithTimeout(ctx, selfConfigConvergenceTimeout)
			for {
				err := s.recordMailOutcome(recordCtx, outcome)
				if err == nil {
					outcome.recorded <- nil
					break
				}
				if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) || recordCtx.Err() != nil {
					s.logSelfConfigFailure(ctx, "mail_outcome_unrecorded")
					outcome.recorded <- ErrSelfConfigUnavailable
					break
				}
				select {
				case <-ctx.Done():
					cancel()
					outcome.recorded <- ErrSelfConfigUnavailable
					return
				case <-time.After(selfConfigReconcileInterval):
				}
			}
			cancel()
		}
	}
}

func (s *SelfConfig) recordMailOutcome(ctx context.Context, outcome selfConfigMailOutcome) error {
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		b, err := r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		if b.OwnerInstanceID != outcome.binding.OwnerInstanceID || b.EnvironmentID != outcome.binding.EnvironmentID {
			return domain.ErrConflict
		}
		status, code := audit.OutcomeSuccess, "none"
		if !outcome.sent {
			status = audit.OutcomeFailure
			code = "transport_failed"
		}
		if b.Incarnation != outcome.binding.Incarnation {
			code = "restored"
		}
		ev, err := newAuditEvent(ctx, audit.EventSelfConfigTestCompleted, outcome.principal, audit.Object{Type: "environment", ID: b.EnvironmentID}, status, outcome.correlation, audit.Payload{"owner_instance_id": b.OwnerInstanceID, "revision": outcome.revision, "generation": outcome.generation, "error_code": code})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
}

func (s *SelfConfig) TestMail(ctx context.Context, actor Actor, req SelfConfigMailTestRequest) (SelfConfigMailTestResult, error) {
	result := SelfConfigMailTestResult{Revision: req.Revision}
	if req.Revision < 1 || req.ExpectedGeneration < 1 || req.SchemaVersion != runtimeconfig.SchemaVersion || strings.ContainsAny(req.To, "\r\n") {
		return result, domain.ErrInvalid
	}
	if _, err := stdmail.ParseAddress(req.To); err != nil {
		return result, domain.ErrInvalid
	}
	b, err := s.bindingForActor(ctx, actor, authz.OpSelfConfigTest)
	if err != nil {
		return result, err
	}
	sealer, err := sealerFor(ctx, s.DB, s.Keyring, actor, authz.OpSelfConfigTest, selfConfigScope(b))
	if err != nil {
		return result, err
	}
	var principal domain.PrincipalID
	err = tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		caller, _, err := authorize(ctx, az, actor, authz.OpSelfConfigTest, selfConfigScope(b), s.now())
		principal = caller.Principal
		return err
	})
	if err != nil {
		return result, err
	}
	coord := s.DB.Coordination()
	now, err := coord.Now(ctx)
	if err != nil {
		return result, err
	}
	callID, err := newID("smt")
	if err != nil {
		return result, err
	}
	// One reservation across the owning instance, including all HA replicas.
	started := time.Now()
	fence, held, err := coord.ClaimLease(ctx, "self-config-mail-test", callID, now, now.Add(mail.SendTimeout))
	if err != nil {
		return result, err
	}
	if !held {
		return result, ErrSelfConfigMailLimited
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = coord.ReleaseLease(cleanup, "self-config-mail-test", callID, fence)
	}()
	count, err := coord.BumpWindow(ctx, "mail-test", string(principal), now.Truncate(time.Hour))
	if err != nil {
		return result, err
	}
	if count > 5 {
		return result, ErrSelfConfigMailLimited
	}
	ctx, cancel := context.WithDeadline(ctx, started.Add(mail.SendTimeout))
	defer cancel()
	var bundle *runtimeconfig.Bundle
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpSelfConfigTest, selfConfigScope(b), s.now())
		if err != nil {
			return err
		}
		current, err := r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		if current.Generation != req.ExpectedGeneration || current.SchemaVersion != int64(req.SchemaVersion) || current.Suspended {
			return domain.ErrConflict
		}
		bundle, err = prepareSelfConfigSnapshot(ctx, r.Snapshots(), r.Catalogue(), p, sealer, req.Revision)
		if err != nil {
			return err
		}
		if !bundle.MailConfigured() {
			return mail.ErrDisabled
		}
		intent, err := NewSelfConfigReauthIntent(SelfConfigReauthTarget{Action: "mail-test", OwnerInstanceID: b.OwnerInstanceID, Revision: req.Revision, SchemaVersion: req.SchemaVersion, ExpectedGeneration: req.ExpectedGeneration, To: req.To})
		if err != nil {
			return err
		}
		if s.Auth == nil {
			return errors.New("service: configuration test email requires reauthentication")
		}
		if err := s.Auth.ConsumeSelfConfigReauth(ctx, az, caller, intent, s.now()); err != nil {
			return err
		}
		ev, err := newAuditEvent(ctx, audit.EventSelfConfigTestRequested, caller.Principal, audit.Object{Type: "environment", ID: b.EnvironmentID}, audit.OutcomeSuccess, callID, audit.Payload{"owner_instance_id": b.OwnerInstanceID, "revision": req.Revision, "generation": req.ExpectedGeneration})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
	if err != nil {
		return result, err
	}
	deliveryErr := bundle.Send(ctx, req.To, "Hikyo configuration test", "This test email was sent using the selected published Hikyo configuration revision.")
	outcomeCtx, outcomeCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer outcomeCancel()
	outcome := selfConfigMailOutcome{binding: b, principal: principal, correlation: callID, revision: req.Revision, generation: req.ExpectedGeneration, sent: deliveryErr == nil, recorded: make(chan error, 1)}
	select {
	case s.outcomeQueue() <- outcome:
	case <-outcomeCtx.Done():
		return result, ErrSelfConfigUnavailable
	}
	select {
	case err := <-outcome.recorded:
		if err != nil {
			return result, err
		}
	case <-outcomeCtx.Done():
		return result, ErrSelfConfigUnavailable
	}
	if deliveryErr != nil {
		return result, mail.ErrDelivery
	}
	result.Sent = true
	return result, nil
}
