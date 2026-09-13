package service

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

// RuntimeStatus is a closed public projection with no instance identifiers.
type RuntimeStatus struct {
	State string
	Phase string
}

func (s *System) RuntimeStatus(ctx context.Context) (RuntimeStatus, error) {
	state, err := s.DB.RuntimeUpgradeState(ctx)
	if err != nil {
		return RuntimeStatus{}, err
	}
	if state.Pending.Invalidated || state.Pending.Phase == upgrade.RestoreRequired {
		return RuntimeStatus{State: "recovery-required"}, nil
	}
	if !state.Maintenance {
		// A superseded process may observe a healthy newer release, but cannot
		// advertise itself as ready until its own admission is still valid.
		if err := s.Ready(ctx); err != nil {
			return RuntimeStatus{}, err
		}
		return RuntimeStatus{State: "ready"}, nil
	}
	phase := "preparing"
	switch state.Pending.Phase {
	case upgrade.BackupPreparing:
		phase = "backup"
	case upgrade.SchemaWriteStarted:
		phase = "migration"
	case upgrade.SchemaApplied, upgrade.Healthy:
		phase = "health-check"
	}
	return RuntimeStatus{State: "maintenance", Phase: phase}, nil
}
