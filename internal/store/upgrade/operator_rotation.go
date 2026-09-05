package upgrade

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/stdlib"
)

// OperatorCredentialEpoch uses the canonical complete credential-stamp inventory.
// It is read again inside the writer transaction before changing any authority.
func (s *Session) OperatorCredentialEpoch(ctx context.Context, expected State) (int64, error) {
	if err := s.check(); err != nil {
		return 0, err
	}
	current, err := s.Read(ctx)
	if err != nil {
		return 0, err
	}
	if !equalRecord(current, expected) {
		return 0, ErrConflict
	}
	// This generated SELECT contains no engine-specific syntax or parameters.
	raw, err := sqlitegen.New(s.conn).MaxKnownCredentialEpoch(ctx)
	if err != nil {
		return 0, err
	}
	epoch, ok := raw.(int64)
	if !ok || epoch < expected.RestoreEpoch || epoch > 1<<32 {
		return 0, errors.New("invalid strongest operator credential epoch")
	}
	return epoch, nil
}

// PlanOperatorRotation is persisted in external installation custody BEFORE
// ApplyOperatorRotation. Random incarnation is journaled, so crash retry cannot
// accidentally mint a second transition.
func (s *Session) PlanOperatorRotation(ctx context.Context, expected State, strongest int64) (State, error) {
	actual, err := s.OperatorCredentialEpoch(ctx, expected)
	if err != nil {
		return State{}, err
	}
	if actual > strongest || strongest > 1<<32 {
		return State{}, ErrConflict
	}
	next := expected
	next.Generation, err = nextGeneration(expected.Generation)
	if err != nil {
		return State{}, err
	}
	if _, err := rand.Read(next.RecoveryIncarnation[:]); err != nil {
		return State{}, err
	}
	next.RestoreEpoch = strongest + 1
	next.Maintenance = true
	pending := *expected.Pending
	pending.Invalidated = true
	pending.Phase = RestoreRequired
	next.Pending = &pending
	return next, next.Validate()
}
func (s *Session) ApplyOperatorRotation(ctx context.Context, before, after State, strongest int64) error {
	return s.applyOperatorRotation(ctx, before, after, strongest, nil)
}

// ApplyJournaledOperatorRotation takes the runtime control-row writer lock,
// rechecks all authority, then fsyncs the external journal before mutation. A
// concurrent credential rotation cannot leave a newly created stale journal.
func (s *Session) ApplyJournaledOperatorRotation(ctx context.Context, before, after State, strongest int64, journal func() error) error {
	if journal == nil {
		return ErrConflict
	}
	return s.applyOperatorRotation(ctx, before, after, strongest, journal)
}
func (s *Session) applyOperatorRotation(ctx context.Context, before, after State, strongest int64, journal func() error) error {
	if before.Validate() != nil || after.Validate() != nil || after.InstanceID != before.InstanceID || after.RestoreEpoch != strongest+1 || after.Generation != before.Generation+1 || !after.Pending.Invalidated {
		return ErrConflict
	}
	expected := before
	pending := *before.Pending
	pending.Invalidated = true
	pending.Phase = RestoreRequired
	expected.Pending = &pending
	expected.Maintenance = true
	expected.Generation = after.Generation
	expected.RestoreEpoch = after.RestoreEpoch
	expected.RecoveryIncarnation = after.RecoveryIncarnation
	if !equalRecord(expected, after) {
		return ErrConflict
	}
	return s.transaction(ctx, func() error {
		if err := s.compare(ctx, before); err != nil {
			return err
		}
		actual, err := s.OperatorCredentialEpoch(ctx, before)
		if err != nil {
			return err
		}
		if actual > strongest || strongest > 1<<32 {
			return ErrConflict
		}
		if journal != nil {
			if err := journal(); err != nil {
				return err
			}
		}
		// A restored historical database may predate the external installation
		// floor. Raise the input stamp inside this same excluded transaction, then
		// use the canonical full-inventory invalidator to advance beyond it.
		if actual < strongest {
			changed, err := s.conn.ExecContext(ctx, `UPDATE auth_instance_state SET credential_epoch=$1 WHERE id=1`, strongest)
			if err != nil {
				return err
			}
			if count, err := changed.RowsAffected(); err != nil || count != 1 {
				return ErrConflict
			}
		}
		now := time.Now().UTC()
		if s.engine == releaseidentity.SQLite {
			err = authn.NewSQLite(s.conn).AdvanceRestoreEpoch(ctx, now)
		} else {
			err = s.conn.Raw(func(value any) error {
				conn, ok := value.(*stdlib.Conn)
				if !ok {
					return ErrCorrupt
				}
				return authn.NewPG(conn.Conn()).AdvanceRestoreEpoch(ctx, now)
			})
		}
		if err != nil {
			return err
		}
		return s.persist(ctx, after, false)
	})
}

// VerifyOperatorRoot proves local root escrow against existing encrypted key
// wrappers under the same migration exclusion. It never initializes keys.
func (s *Session) VerifyOperatorRoot(ctx context.Context, expected State, root []byte) error {
	if expected.Validate() != nil {
		return ErrConflict
	}
	reader := &candidateKeys{session: s, expected: expected, phase: expected.Pending.Phase, operatorRecovery: true}
	return crypto.VerifyExistingHierarchy(ctx, reader, root)
}
