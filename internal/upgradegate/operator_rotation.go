package upgradegate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

type OperatorRotationRequest struct {
	// Package-private failure injection exercises actual durable journal boundaries.
	afterBoundary                      func(string) error
	Store                              upgrade.Config
	StateDirectory                     string
	NewPublicKey, Statement, Signature []byte
	// Only an explicit local CLI loads this escrow material. HTTP must never
	// construct an operator rotation request or read root-key custody files.
	LocalRecoveryRoot []byte
}

func RotateOperator(ctx context.Context, request OperatorRotationRequest) (upgrade.State, error) {
	root := bytes.Clone(request.LocalRecoveryRoot)
	defer crypto.Zero(root)
	f, err := openOperatorFile(ctx, request.StateDirectory, nil, false)
	if err != nil {
		return upgrade.State{}, err
	}
	defer f.close()
	var result upgrade.State
	err = upgrade.WithLock(ctx, request.Store, func(session *upgrade.Session) error {
		current, err := session.Read(ctx)
		if err != nil {
			return err
		}
		before := current
		committed := false
		if f.value.Journal != nil {
			before = f.value.Journal.Before
			committed = sameOperatorState(current, f.value.Journal.After)
			if !committed && !sameOperatorState(current, before) {
				return errors.New("operator rotation journal differs from database; admission remains blocked")
			}
		}
		if before.InstanceID != f.value.InstanceID {
			return errors.New("operator rotation installation authority mismatch")
		}
		pin, err := backupreceipt.PinOperator(before.InstanceID, f.value.PublicKey)
		if err != nil {
			return err
		}
		statement, err := backupreceipt.ParseRotation(request.Statement)
		if err != nil {
			return err
		}
		if before.RestoreEpoch < f.value.EpochFloor && statement.Mode != backupreceipt.LocalBreakGlass {
			return errors.New("historical restore requires explicit local root-escrow break-glass")
		}
		strongest := statement.MaxKnownCredentialEpoch
		if !committed {
			strongest, err = session.OperatorCredentialEpoch(ctx, before)
			if err != nil {
				return err
			}
		}
		incarnation, _ := before.RecoveryIncarnation.MarshalText()
		if !committed {
			strongest = max(strongest, f.value.EpochFloor)
		}
		live := backupreceipt.LiveSource{InstanceID: before.InstanceID, Engine: request.Store.Engine, Source: before.Applied, SourceSchemaSHA256: before.SchemaDigest, MigrationSHA256: before.MigrationDigest, RestoreEpoch: before.RestoreEpoch, RecoveryIncarnation: backupreceipt.Nonce(incarnation), Generation: before.Generation}
		transition, err := backupreceipt.VerifyKeyTransition(pin, request.NewPublicKey, request.Statement, request.Signature, backupreceipt.RotationSource{Live: live, MaxKnownCredentialEpoch: strongest}, time.Now().UTC())
		if err != nil {
			return err
		}
		if transition.RequiresLocalRecovery() {
			if len(root) != crypto.KeySize {
				return errors.New("local break-glass requires explicit root escrow custody")
			}
			if err := session.VerifyOperatorRoot(ctx, current, root); err != nil {
				return err
			}
		} else if len(root) != 0 {
			return errors.New("root escrow is only accepted for explicit local break-glass")
		}
		// A fresh authenticated request may replace only an uncommitted journal
		// whose complete DB authority still equals Before. This lets operators
		// reissue a stale statement after ordinary credentials advanced during a
		// crash, without deleting custody or reviving an obsolete signer.
		replace := f.value.Journal != nil && f.value.Journal.Digest != transition.Digest()
		if replace && committed {
			return errors.New("a committed operator rotation must resume its exact signed request")
		}
		if f.value.Journal == nil || replace {
			if f.value.Journal == nil && current.RestoreEpoch >= f.value.EpochFloor {
				if err := f.value.check(current); err != nil {
					return err
				}
			}
			after, err := session.PlanOperatorRotation(ctx, before, strongest)
			if err != nil {
				return err
			}
			err = session.ApplyJournaledOperatorRotation(ctx, before, after, strongest, func() error {
				f.value.Journal = &operatorJournal{Digest: transition.Digest(), Before: before, After: after, PublicKey: request.NewPublicKey}
				if err := f.save(); err != nil {
					return err
				}
				if request.afterBoundary != nil {
					return request.afterBoundary("journal-durable")
				}
				return nil
			})
			if err != nil {
				return err
			}
			current = after
		}
		j := f.value.Journal
		if sameOperatorState(current, j.Before) {
			if err := session.ApplyOperatorRotation(ctx, j.Before, j.After, strongest); err != nil {
				return err
			}
		} else if !sameOperatorState(current, j.After) {
			return errors.New("operator rotation journal differs from database; admission remains blocked")
		}

		if request.afterBoundary != nil {
			if err := request.afterBoundary("database-committed"); err != nil {
				return err
			}
		}
		result = j.After
		f.value.PublicKey = j.PublicKey
		f.value.EpochFloor = j.After.RestoreEpoch
		f.value.Journal = nil
		return f.save()
	})
	if err != nil {
		return upgrade.State{}, err
	}
	return result, nil
}
func sameOperatorState(a, b upgrade.State) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}

// InstalledOperator reads public installation custody for local export/drill.
// It never bootstraps a missing pin from an archive or from supplied evidence.
func InstalledOperator(ctx context.Context, directory, instance string, configured []byte) (backupreceipt.PinnedOperator, error) {
	f, err := openOperatorFile(ctx, directory, configured, false)
	if err != nil {
		return backupreceipt.PinnedOperator{}, err
	}
	defer f.close()
	if f.value.InstanceID == "" {
		return backupreceipt.PinnedOperator{}, errors.New("operator pin is not bound to an installation yet")
	}
	return f.value.pin(instance)
}

// InstallLegacyOperator establishes the initial public pin only after local
// tooling authenticates a pre-ledger source against the signed genesis catalog.
// It cannot replace existing custody or authorize a key transition.
func InstallLegacyOperator(ctx context.Context, directory, instance string, public []byte, epoch int64) (backupreceipt.PinnedOperator, error) {
	if epoch < 0 {
		return backupreceipt.PinnedOperator{}, errors.New("invalid legacy installation epoch")
	}
	if _, err := backupreceipt.PinOperator(instance, public); err != nil {
		return backupreceipt.PinnedOperator{}, err
	}
	f, err := openOperatorFile(ctx, directory, public, true)
	if err != nil {
		return backupreceipt.PinnedOperator{}, err
	}
	defer f.close()
	pin, err := f.value.pin(instance)
	if err != nil {
		return backupreceipt.PinnedOperator{}, err
	}
	if f.value.InstanceID == "" {
		f.value.InstanceID = instance
		f.value.EpochFloor = epoch
		if err := f.save(); err != nil {
			return backupreceipt.PinnedOperator{}, err
		}
	}
	if epoch < f.value.EpochFloor {
		return backupreceipt.PinnedOperator{}, errors.New("legacy source predates installed operator epoch")
	}
	return pin, nil
}

// OperatorCustody is valid only inside WithInstalledOperator. It contains only
// public authority; callers must authenticate the actual source independently.
type OperatorCustody struct {
	file   *operatorFile
	active *bool
}

func (c OperatorCustody) CheckSource(instance string, epoch int64) error {
	if c.file == nil || c.active == nil || !*c.active {
		return errors.New("operator custody operation expired")
	}
	if _, err := c.file.value.pin(instance); err != nil {
		return err
	}
	if epoch < c.file.value.EpochFloor {
		return errors.New("source predates durable operator epoch floor")
	}
	return nil
}
func (c OperatorCustody) Check(state upgrade.State) error {
	if err := state.Validate(); err != nil {
		return err
	}
	if err := c.CheckSource(state.InstanceID, state.RestoreEpoch); err != nil {
		return err
	}
	return c.file.value.check(state)
}

// WithInstalledOperator holds public installation custody before the caller
// acquires datastore exclusion. Backup and reconciliation need no root escrow,
// but cannot bypass a pending journal, configured pin or external epoch floor.
func WithInstalledOperator(ctx context.Context, directory string, configured []byte, fn func(OperatorCustody) error) error {
	if fn == nil {
		return errors.New("missing operator custody operation")
	}
	f, err := openOperatorFile(ctx, directory, configured, false)
	if err != nil {
		return err
	}
	defer f.close()
	if f.value.InstanceID == "" {
		return errors.New("operator pin is not bound to an installation yet")
	}
	if _, err := f.value.pin(f.value.InstanceID); err != nil {
		return err
	}
	active := true
	defer func() { active = false }()
	return fn(OperatorCustody{file: f, active: &active})
}
