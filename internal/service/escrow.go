package service

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Escrow is a CLI-only local custody operation. It does not authenticate a
// network caller and must never be wired to an HTTP handler.
type Escrow struct {
	DB  *store.DB
	Now func() time.Time
}

type escrowHierarchy struct {
	keys  store.KeyReader
	proof authz.Proof
}

func (h escrowHierarchy) ActiveMasterWrappers(ctx context.Context) ([]crypto.WrappedKey, error) {
	return h.keys.ActiveMasterWrappers(ctx, h.proof)
}
func (h escrowHierarchy) AllOpenableTier3(ctx context.Context) ([]crypto.WrappedKey, error) {
	return h.keys.AllOpenableTier3(ctx, h.proof)
}

// Verify consumes the supplied key. The caller checks distinct file custody;
// the assertion records the operator's claim, not physical offline proof.
func (s *Escrow) Verify(ctx context.Context, root []byte, separateCustodyAsserted bool) error {
	defer crypto.Zero(root)
	if !separateCustodyAsserted {
		return errors.New("explicit separate escrow custody assertion is required")
	}
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := authz.SystemAuthority(authz.SiteEscrow, az.Token())
		if err != nil {
			return err
		}
		if err := r.Keys().AcquireHierarchyGeneration(ctx, p); err != nil {
			return err
		}
		instance, incarnation, err := s.DB.RecoveryIdentity()
		if err != nil {
			return err
		}
		wrappers, err := r.Keys().ActiveMasterWrappers(ctx, p)
		if err != nil {
			return err
		}
		if len(wrappers) != 1 {
			return errors.New("escrow verification requires one finalized root epoch; complete root rotation first")
		}
		if err := crypto.VerifyExistingHierarchy(ctx, escrowHierarchy{r.Keys(), p}, append([]byte(nil), root...)); err != nil {
			return err
		}
		record := store.EscrowRecord{At: nowOr(s.Now), InstanceID: instance, Incarnation: incarnation, RootEpoch: int64(wrappers[0].RootKeyEpoch)}
		if err := r.Retention().RecordEscrow(ctx, p, record); err != nil {
			return err
		}
		ev, err := newAuditEvent(ctx, audit.EventRootEscrowVerified, "", audit.Object{Type: "instance", ID: "instance"}, audit.OutcomeSuccess, "", audit.Payload{"root_key_epoch": record.RootEpoch, "separate_custody_asserted": true})
		if err != nil {
			return err
		}
		return r.Audit().InsertInstance(ctx, p, ev)
	})
}
