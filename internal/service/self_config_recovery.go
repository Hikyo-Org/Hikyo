package service

import (
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// SelfConfigRecoveryStatus contains only host-local operational metadata.
type SelfConfigRecoveryStatus struct {
	OwnerInstanceID string `json:"owner_instance_id"`
	Managed         bool   `json:"managed"`
	Generation      int64  `json:"generation"`
	DesiredRevision int64  `json:"desired_revision"`
	Suspended       bool   `json:"suspended"`
}

func (s *SelfConfig) LocalStatus(ctx context.Context) (SelfConfigRecoveryStatus, error) {
	if operation.IsNetwork(ctx) {
		return SelfConfigRecoveryStatus{}, domain.ErrUnauthorized
	}
	var out SelfConfigRecoveryStatus
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		owner, err := az.InstanceIdentity(ctx)
		if err != nil {
			return err
		}
		out.OwnerInstanceID = owner
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		b, err := r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		out = SelfConfigRecoveryStatus{OwnerInstanceID: b.OwnerInstanceID, Managed: true, Generation: b.Generation, DesiredRevision: b.DesiredRevision, Suspended: b.Suspended}
		return nil
	})
	return out, err
}

// Recover selects an exact uncollected published revision under host authority.
// The store refuses active writers and unresolved jobs. It preserves restored
// credential suspension, so choosing a revision cannot resume outbound mail.
func (s *SelfConfig) Recover(ctx context.Context, revision int64) (SelfConfigRecoveryStatus, error) {
	if operation.IsNetwork(ctx) {
		return SelfConfigRecoveryStatus{}, domain.ErrUnauthorized
	}
	if revision < 1 {
		return SelfConfigRecoveryStatus{}, domain.ErrInvalid
	}
	var binding store.SelfConfigBinding
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRecoveryAuthority(ctx, revision)
		if err != nil {
			return err
		}
		binding, err = r.SelfConfig().Binding(ctx, p)
		return err
	})
	if err != nil {
		return SelfConfigRecoveryStatus{}, err
	}
	sealer, err := s.Keyring.ForProject(ctx, binding.OrgID, binding.ProjectID)
	if err != nil {
		return SelfConfigRecoveryStatus{}, err
	}
	at, err := s.runtimeTimestamp(ctx)
	if err != nil {
		return SelfConfigRecoveryStatus{}, err
	}
	err = tx.WriteSerialized(ctx, s.DB, "hikyo:self-config-recovery", func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRecoveryAuthority(ctx, revision)
		if err != nil {
			return err
		}
		if _, err := prepareSelfConfigSnapshot(ctx, r.Snapshots(), r.Catalogue(), p, sealer, revision); err != nil {
			return err
		}
		snapshot, err := r.Snapshots().AtRevision(ctx, p, revision)
		if err != nil {
			return err
		}
		binding, err = r.SelfConfig().RecoverTarget(ctx, p, binding.Generation, revision, snapshot.ID, at)
		if err != nil {
			return err
		}
		event, err := newAuditEvent(ctx, audit.EventSelfConfigRecovered, "", audit.Object{Type: "environment", ID: binding.EnvironmentID}, audit.OutcomeSuccess, "", audit.Payload{"owner_instance_id": binding.OwnerInstanceID, "revision": revision, "generation": binding.Generation})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, event)
	})
	if err != nil {
		return SelfConfigRecoveryStatus{}, err
	}
	return SelfConfigRecoveryStatus{OwnerInstanceID: binding.OwnerInstanceID, Managed: true, Generation: binding.Generation, DesiredRevision: binding.DesiredRevision, Suspended: binding.Suspended}, nil
}
