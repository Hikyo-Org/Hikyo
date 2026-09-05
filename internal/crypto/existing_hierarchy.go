package crypto

import (
	"context"
	"errors"
)

// ExistingHierarchyStore exposes only persisted wrappers, with no creation
// capability. Escrow verification must not initialize an absent hierarchy.
type ExistingHierarchyStore interface {
	ActiveMasterWrappers(context.Context) ([]WrappedKey, error)
	AllOpenableTier3(context.Context) ([]WrappedKey, error)
}

// VerifyExistingHierarchy authenticates the existing master and every openable
// tier-3 wrapper using separately escrowed root material. It consumes root and
// zeroes every temporary unwrapped key on success and failure. A hierarchy from
// before scanning keys existed is valid; missing instance/token keys are not.
func VerifyExistingHierarchy(ctx context.Context, ks ExistingHierarchyStore, root []byte) error {
	defer Zero(root)
	if len(root) != KeySize {
		return ErrRootKeyFormat
	}
	if ks == nil {
		return errors.New("crypto: existing hierarchy store unavailable")
	}
	wrappers, err := ks.ActiveMasterWrappers(ctx)
	if err != nil {
		return errors.New("crypto: escrow master inventory unavailable")
	}
	if len(wrappers) == 0 {
		return ErrNoKey
	}
	k := &Keyring{}
	master, err := k.unwrapMaster(root, wrappers)
	if err != nil {
		return err
	}
	defer Zero(master.key)
	if len(master.key) != KeySize {
		return errors.New("crypto: escrow master key has invalid length")
	}
	k.master.Store(singleMaster(master.version, master.key))
	rows, err := ks.AllOpenableTier3(ctx)
	if err != nil {
		return errors.New("crypto: escrow tier-3 inventory unavailable")
	}
	var instance, token bool
	for _, row := range rows {
		switch row.Purpose {
		case PurposeProject:
			if row.OrgID == "" || row.ProjectID == "" {
				return errors.New("crypto: invalid escrow project key scope")
			}
		case PurposeInstance, PurposeToken, PurposeScanning:
			if row.OrgID != "" || row.ProjectID != "" {
				return errors.New("crypto: invalid escrow instance key scope")
			}
		default:
			return errors.New("crypto: unsupported escrow key purpose")
		}
		key, err := k.unwrapTier3(row)
		if err != nil {
			return errors.New("crypto: an existing tier-3 wrapper did not authenticate")
		}
		validLength := len(key.key) == KeySize
		Zero(key.key)
		if !validLength {
			return errors.New("crypto: escrow tier-3 key has invalid length")
		}
		instance = instance || row.Purpose == PurposeInstance
		token = token || row.Purpose == PurposeToken
	}
	if !instance || !token {
		return ErrNoKey
	}
	return nil
}
