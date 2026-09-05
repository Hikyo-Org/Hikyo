package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/updater"
)

const updateRecentAuthentication = 5 * time.Minute

// Updates owns the authenticated release-notification read. No database
// transaction remains open across the bounded public release lookup.
type Updates struct {
	DB        *store.DB
	Source    updatecheck.Source
	Version   string
	Channel   updatecheck.Channel
	Control   updater.Control
	Now       func() time.Time
	Log       *slog.Logger
	outcomeMu sync.Mutex
}

// Run reconciles helper-owned terminal outcomes independently of any browser.
// The helper journal is durable, so startup catches outcomes completed while
// the server was stopped; the short poll records ordinary completions promptly.
func (s *Updates) Run(ctx context.Context) {
	if s.Control == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.ReconcileOutcomes(ctx); err != nil && ctx.Err() == nil {
			logger := s.Log
			if logger == nil {
				logger = slog.Default()
			}
			logger.Error("update outcome reconciliation failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ReconcileOutcomes records every terminal helper job under the scheduler's
// closed system authority, then acknowledges it. Browser departure, session
// expiry, and grant changes after the committed intent cannot erase outcome
// evidence for an already-authorized privileged action.
type pendingOutcomeControl interface {
	PendingOutcomes(context.Context) ([]updater.Job, error)
}

func (s *Updates) ReconcileOutcomes(ctx context.Context) error {
	control, ok := s.Control.(pendingOutcomeControl)
	if !ok {
		return nil
	}
	jobs, err := control.PendingOutcomes(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := s.recordSystemOutcome(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

// GetStatus authorizes an instance-config read, selects the newest admitted
// release, then re-authorizes and records the successful answer atomically.
func (s *Updates) GetStatus(ctx context.Context, actor Actor) (updatecheck.Status, error) {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, now())
		if err != nil {
			return err
		}
		_, err = az.Authorize(ctx, caller, authz.OpUpdateStatusRead, domain.Scope{})
		return err
	})
	if err != nil {
		return updatecheck.Status{}, err
	}
	var status updatecheck.Status
	if s.Channel == updatecheck.ChannelOff || s.Version == "dev" {
		status, err = updatecheck.Select(s.Version, s.Channel, nil)
	} else {
		if s.Source == nil {
			return updatecheck.Status{}, errors.New("updates: release source is not configured")
		}
		var releases []updatecheck.Release
		releases, err = s.Source.Releases(ctx)
		if err == nil {
			status, err = updatecheck.Select(s.Version, s.Channel, releases)
		}
	}
	if err != nil {
		return updatecheck.Status{}, err
	}
	status.ApplySupported = false
	status.ApplyError = updater.RemoteApplyDisabledReason

	err = tx.Write(ctx, s.DB, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpUpdateStatusRead, domain.Scope{}, now())
		if err != nil {
			return err
		}
		event, err := domainEvent(ctx, audit.EventUpdateStatusRead, caller.Principal,
			audit.Object{Type: "update_status", ID: "release_channel"}, audit.Payload{
				"channel": string(s.Channel), "current_version": s.Version,
			})
		if err != nil {
			return err
		}
		return repos.Audit().InsertInstance(ctx, proof, event)
	})
	return status, err
}

// Request preserves authenticated refusal evidence but cannot contact the
// retired deployment helper. Release discovery remains independently available.
func (s *Updates) Request(ctx context.Context, actor Actor, version string) (updater.Job, error) {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	id, err := newID("upd")
	if err != nil {
		return updater.Job{}, err
	}
	err = tx.Write(ctx, s.DB, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpUpdateRequest, domain.Scope{}, now())
		if err != nil {
			return err
		}
		age := now().Sub(caller.Assurance.AuthenticatedAt)
		if caller.Assurance.AuthenticatedAt.IsZero() || age < 0 || age > updateRecentAuthentication {
			return fmt.Errorf("fresh authentication required: %w", domain.ErrUnauthorized)
		}
		version = audit.SanitizeFreeText(version)
		if version == "" || len(version) > 128 {
			return domain.ErrInvalid
		}
		intent, err := newAuditEvent(ctx, audit.EventUpdateRequested, caller.Principal,
			audit.Object{Type: "instance_update", ID: id}, audit.OutcomeIntent, id,
			audit.Payload{"version": version, "backend": "disabled"})
		if err != nil {
			return err
		}
		if err := repos.Audit().InsertInstance(ctx, proof, intent); err != nil {
			return err
		}
		outcome, err := updateOutcomeEvent(ctx, updater.Job{
			ID: id, Backend: "disabled", Version: version, RequestedBy: string(caller.Principal),
			State: updater.StateFailed, FailureCode: "remote-apply-disabled",
		})
		if err != nil {
			return err
		}
		return repos.Audit().InsertInstance(ctx, proof, outcome)
	})
	if err != nil {
		return updater.Job{}, err
	}
	return updater.Job{}, fmt.Errorf("%w: %w", domain.ErrConflict, updater.ErrRemoteApplyDisabled)
}

// GetJob reads helper-owned durable state. The first terminal observation
// appends the correlated outcome before acknowledging it to the helper.
func (s *Updates) GetJob(ctx context.Context, actor Actor, id string) (updater.Job, error) {
	if s.Control == nil {
		return updater.Job{}, fmt.Errorf("%w: remote update helper is not configured", domain.ErrConflict)
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	if err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, now())
		if err != nil {
			return err
		}
		_, err = az.Authorize(ctx, caller, authz.OpUpdateJobRead, domain.Scope{})
		return err
	}); err != nil {
		return updater.Job{}, err
	}
	job, err := s.Control.Job(ctx, id)
	if errors.Is(err, updater.ErrJobNotFound) {
		return updater.Job{}, domain.ErrNotFound
	}
	if err != nil {
		return updater.Job{}, err
	}
	if !job.OutcomeReported && job.State.Terminal() {
		s.outcomeMu.Lock()
		defer s.outcomeMu.Unlock()
		job, err = s.Control.Job(ctx, id)
		if err != nil {
			return updater.Job{}, err
		}
		if job.OutcomeReported || !job.State.Terminal() {
			return job, nil
		}
		if err := s.recordOutcome(ctx, actor, job); err != nil {
			return updater.Job{}, err
		}
		if err := s.Control.AcknowledgeOutcome(ctx, id); err != nil {
			return updater.Job{}, err
		}
		job.OutcomeReported = true
	}
	return job, nil
}

func (s *Updates) recordSystemOutcome(ctx context.Context, job updater.Job) error {
	s.outcomeMu.Lock()
	defer s.outcomeMu.Unlock()
	current, err := s.Control.Job(ctx, job.ID)
	if err != nil {
		return err
	}
	if current.OutcomeReported || !current.State.Terminal() {
		return nil
	}
	if err := tx.Write(ctx, s.DB, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
		proof, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
		if err != nil {
			return err
		}
		event, err := updateOutcomeEvent(ctx, current)
		if err != nil {
			return err
		}
		return repos.Audit().InsertInstance(ctx, proof, event)
	}); err != nil && !errors.Is(err, store.ErrConflict) {
		return err
	}
	return s.Control.AcknowledgeOutcome(ctx, current.ID)
}

func (s *Updates) recordOutcome(ctx context.Context, actor Actor, job updater.Job) error {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	err := tx.Write(ctx, s.DB, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpUpdateJobRead, domain.Scope{}, now())
		if err != nil {
			return err
		}
		event, err := updateOutcomeEvent(ctx, job)
		if err != nil {
			return err
		}
		if event.Actor.ID != string(caller.Principal) {
			event.AuthorityID = string(caller.Principal)
		}
		return repos.Audit().InsertInstance(ctx, proof, event)
	})
	if errors.Is(err, store.ErrConflict) {
		return nil
	}
	return err
}

func updateOutcomeEvent(ctx context.Context, job updater.Job) (audit.Event, error) {
	outcome := audit.OutcomeFailure
	if job.State == updater.StateSucceeded {
		outcome = audit.OutcomeSuccess
	}
	payload := audit.Payload{
		"version": job.Version, "backend": string(job.Backend), "state": string(job.State),
	}
	if job.FailureCode != "" {
		payload["failure_code"] = job.FailureCode
	}
	event, err := newAuditEvent(ctx, audit.EventUpdateOutcome, domain.PrincipalID(job.RequestedBy),
		audit.Object{Type: "instance_update", ID: job.ID}, outcome, job.ID, payload)
	if err != nil {
		return audit.Event{}, err
	}
	// One deterministic event id makes the journal-to-audit handoff
	// idempotent across a crash after commit but before helper acknowledgement.
	event.ID = "evt_" + strings.TrimPrefix(job.ID, "upd_")
	return event, nil
}
