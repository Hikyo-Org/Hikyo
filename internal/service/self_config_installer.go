package service

import (
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

type selfConfigPrepared struct {
	snapshotID, incarnation string
	activation              runtimeconfig.PreparedActivation
}

// ResolveRuntimeBundle resolves the authoritative startup configuration without
// acknowledging a node or installing application resources. The application
// calls it before constructing its service graph. A suspended binding still
// supplies its saved policy for the recovery UI; Capture remains fenced.
func (s *SelfConfig) ResolveRuntimeBundle(ctx context.Context) (*runtimeconfig.Bundle, error) {
	if operation.IsNetwork(ctx) {
		return nil, ErrSelfConfigUnavailable
	}
	metadata, err := s.DB.Coordination().CurrentSelfConfigGeneration(ctx)
	if err != nil {
		return nil, err
	}
	if !metadata.Managed {
		seed, err := s.prepareSeed()
		if err != nil {
			return nil, err
		}
		return runtimeconfig.Prepare(seed.values)
	}
	var binding store.SelfConfigBinding
	err = tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		binding, err = r.SelfConfig().Binding(ctx, p)
		return err
	})
	if err != nil {
		return nil, err
	}
	owner, _, err := s.DB.RecoveryIdentity()
	if err != nil {
		return nil, err
	}
	if owner != binding.OwnerInstanceID {
		return nil, ErrSelfConfigUnavailable
	}
	return s.prepareRuntimeSnapshot(ctx, binding, binding.DesiredSnapshotID, binding.DesiredRevision)
}

// CloseRuntime disposes candidate resources after the app-owned worker has
// stopped. The currently installed application remains the supervisor's owner.
func (s *SelfConfig) CloseRuntime() error {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	return s.closePrepared()
}

func (s *SelfConfig) closePrepared() error {
	prepared := s.prepared
	s.prepared = nil
	if prepared == nil {
		return nil
	}
	return prepared.activation.Close()
}

// prepareInstallation is called with runtimeMu held. A prepared acknowledgement
// represents real application resources, retained until commitment or disposal.
func (s *SelfConfig) prepareInstallation(ctx context.Context, snapshotID, incarnation string, bundle *runtimeconfig.Bundle) error {
	if s.prepared != nil && s.prepared.snapshotID == snapshotID && s.prepared.incarnation == incarnation {
		return nil
	}
	if err := s.closePrepared(); err != nil {
		return err
	}
	if s.Installer == nil {
		if len(bundle.OwnerValues()) != 0 || bundle.HasNodeValues() {
			return errors.New("application configuration requires an installed runtime consumer")
		}
		return nil
	}
	activation, err := s.Installer.Prepare(ctx, bundle)
	if err != nil {
		return err
	}
	if activation == nil {
		return errors.New("application configuration preparation returned no activation")
	}
	s.prepared = &selfConfigPrepared{snapshotID: snapshotID, incarnation: incarnation, activation: activation}
	return nil
}

func (s *SelfConfig) activateInstallation(ctx context.Context, snapshotID, incarnation string, bundle *runtimeconfig.Bundle) error {
	if err := s.prepareInstallation(ctx, snapshotID, incarnation, bundle); err != nil {
		return err
	}
	if s.prepared == nil {
		return nil
	}
	if err := s.prepared.activation.Activate(ctx); err != nil {
		return errors.Join(err, s.closePrepared())
	}
	// Ownership transferred to the app. Close only releases any residual
	// preparation resources; the interface forbids closing its active graph.
	return s.closePrepared()
}

// ResolveRepairRuntimeBundle supplies only the retained previous completed
// policy for a fenced boot repair graph. It grants no active-generation ack.
func (s *SelfConfig) ResolveRepairRuntimeBundle(ctx context.Context) (*runtimeconfig.Bundle, error) {
	if operation.IsNetwork(ctx) {
		return nil, ErrSelfConfigUnavailable
	}
	var b store.SelfConfigBinding
	var revision int64
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		b, err = r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		revision, err = r.SelfConfig().PreviousRevision(ctx, p)
		return err
	})
	if err != nil {
		return nil, err
	}
	owner, inc, err := s.DB.RecoveryIdentity()
	if err != nil {
		return nil, err
	}
	if owner != b.OwnerInstanceID || inc != b.Incarnation || b.PreviousSnapshotID == "" {
		return nil, ErrSelfConfigUnavailable
	}
	return s.prepareRuntimeSnapshot(ctx, b, b.PreviousSnapshotID, revision)
}
