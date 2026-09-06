package crypto

import "errors"

// PrepareNewProject creates the encryption material for an atomic project
// provision. It performs no datastore write and does not populate the keyring
// cache. The caller persists the returned wrapper in the same transaction as
// the new project and its initial encrypted values. Existing projects must use
// ForProject; the datastore's unique active-key constraint refuses replacement.
func (k *Keyring) PrepareNewProject(orgID, projectID string) (WrappedKey, *ProjectSealer, error) {
	if orgID == "" || projectID == "" {
		return WrappedKey{}, nil, errors.New("crypto: project scope requires org and project ids")
	}
	handle, row, err := k.mintTier3(PurposeProject, orgID, projectID)
	if err != nil {
		return WrappedKey{}, nil, err
	}
	set := &versionSet{active: handle.version, byVer: map[uint32]keyHandle{handle.version: handle}}
	return row, &ProjectSealer{kr: k, orgID: orgID, projectID: projectID, deks: set}, nil
}
