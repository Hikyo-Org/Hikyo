package crypto

import (
	"context"
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
	defer Zero(root)
	if len(root) != KeySize || orgID == "" || projectID == "" || ks == nil {
		return nil, ErrDecrypt
	}
	masters, err := ks.ActiveMasterWrappers(ctx)
	if err != nil {
		return nil, ErrDecrypt
	}
	k := &Keyring{}
	master, err := k.unwrapMaster(root, masters)
	if err != nil {
		return nil, ErrDecrypt
	}
	defer Zero(master.key)
	k.master.Store(singleMaster(master.version, master.key))
	rows, err := ks.AllOpenableTier3(ctx)
	if err != nil {
		return nil, ErrDecrypt
	}
	versions := &versionSet{byVer: make(map[uint32]keyHandle)}
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
			return nil, ErrDecrypt
		}
		if _, duplicate := versions.byVer[key.version]; duplicate {
			Zero(key.key)
			return nil, ErrDecrypt
		}
		versions.byVer[key.version] = key
	}
	if len(versions.byVer) == 0 {
		return nil, ErrDecrypt
	}
	sealer := &ProjectSealer{kr: k, orgID: orgID, projectID: projectID, deks: versions}
	values := make(map[string]string, len(fields))
	for _, field := range fields {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, duplicate := values[field.Name]; duplicate || field.Name == "" {
			return nil, errors.New("crypto: duplicate configuration field")
		}
		plain, err := sealer.OpenField(field.AAD, field.Ciphertext)
		if err != nil {
			return nil, ErrDecrypt
		}
		values[field.Name] = string(plain)
		Zero(plain)
	}
	return values, nil
}
