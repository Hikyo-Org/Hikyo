package service

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

// Updates owns the authenticated release-notification read. No database
// transaction remains open across the bounded public release lookup.
type Updates struct {
	DB      *store.DB
	Source  updatecheck.Source
	Version string
	Channel updatecheck.Channel
	Now     func() time.Time
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

	err = tx.Write(ctx, s.DB, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, now())
		if err != nil {
			return err
		}
		proof, err := az.Authorize(ctx, caller, authz.OpUpdateStatusRead, domain.Scope{})
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
