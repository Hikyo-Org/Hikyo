package crypto

import (
	"context"
	"crypto/rand"
	"errors"
)

// ExistingProjectField is one authenticated persisted field, with no write or
// key-creation capability. Callers must restrict its source to the fixed owner.
type ExistingProjectField struct {
	redactor
	Name       string
	AAD        ProjectFieldAAD
	Ciphertext []byte
}

// OpenExistingProjectFields consumes root and opens a fixed project projection.
// No keyring or plaintext key escapes; temporary key material is always cleared.
func OpenExistingProjectFields(ctx context.Context, ks ExistingHierarchyStore, root []byte, orgID, projectID string, fields []ExistingProjectField) (map[string]string, error) {
	values := make(map[string]string, len(fields))
	err := withExistingProjectSealer(ctx, ks, root, orgID, projectID, 0, func(sealer *ProjectSealer) error {
		for _, field := range fields {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, duplicate := values[field.Name]; duplicate || field.Name == "" {
				return errors.New("crypto: duplicate configuration field")
			}
			plain, err := sealer.OpenField(field.AAD, field.Ciphertext)
			if err != nil {
				return ErrDecrypt
			}
			values[field.Name] = string(plain)
			Zero(plain)
		}
		return nil
	})
	if err != nil {
		clear(values)
		return nil, err
	}
	return values, nil
}

// ExistingProjectSealingStore additionally identifies the active write version.
type ExistingProjectSealingStore interface {
	ExistingHierarchyStore
	ActiveProjectVersion(context.Context, string, string) (uint32, error)
}

// WithExistingProjectSealer authenticates only existing project wrappers. It consumes
// root and clears all temporary keys after fn; fn must not retain the sealer.
// It never creates or persists key material.
func WithExistingProjectSealer(ctx context.Context, ks ExistingProjectSealingStore, root []byte, orgID, projectID string, fn func(*ProjectSealer) error) error {
	defer Zero(root)
	if ks == nil {
		return ErrDecrypt
	}
	active, err := ks.ActiveProjectVersion(ctx, orgID, projectID)
	if err != nil || active == 0 {
		return ErrDecrypt
	}
	return withExistingProjectSealer(ctx, ks, root, orgID, projectID, active, fn)
}

func withExistingProjectSealer(ctx context.Context, ks ExistingHierarchyStore, root []byte, orgID, projectID string, active uint32, fn func(*ProjectSealer) error) error {
	defer Zero(root)
	if len(root) != KeySize || orgID == "" || projectID == "" || ks == nil {
		return ErrDecrypt
	}
	masters, err := ks.ActiveMasterWrappers(ctx)
	if err != nil {
		return ErrDecrypt
	}
	k := &Keyring{rnd: rand.Reader}
	master, err := k.unwrapMaster(root, masters)
	if err != nil {
		return ErrDecrypt
	}
	defer Zero(master.key)
	k.master.Store(singleMaster(master.version, master.key))
	rows, err := ks.AllOpenableTier3(ctx)
	if err != nil {
		return ErrDecrypt
	}
	versions := &versionSet{byVer: make(map[uint32]keyHandle), active: active}
	defer func() {
		for _, key := range versions.byVer {
			Zero(key.key)
		}
	}()
	for _, row := range rows {
		if row.Purpose != PurposeProject || row.OrgID != orgID || row.ProjectID != projectID {
			continue
		}
		key, err := k.unwrapTier3(row)
		if err != nil {
			return ErrDecrypt
		}
		if _, duplicate := versions.byVer[key.version]; duplicate {
			Zero(key.key)
			return ErrDecrypt
		}
		versions.byVer[key.version] = key
	}
	if len(versions.byVer) == 0 {
		return ErrDecrypt
	}
	if active != 0 {
		if _, ok := versions.byVer[active]; !ok {
			return ErrDecrypt
		}
	}
	sealer := &ProjectSealer{kr: k, orgID: orgID, projectID: projectID, deks: versions}
	return fn(sealer)
}
