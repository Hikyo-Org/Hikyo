package upgradecustody

import (
	"bytes"
	"errors"
	"os"
)

const enrollmentInstance = "ins_00000000000000000000000000000000"

// PrepareLocalEnrollment creates encrypted keys before the fresh gate assigns
// its installation identity. The caller must hold enrollment exclusion and
// prove the datastore is fresh before the first call. A crash may resume the
// same unbound record; existing bound custody is never replaced or adopted.
func PrepareLocalEnrollment(directory string, rootKey []byte) ([]byte, error) {
	vault, err := OpenLocal(directory, rootKey, enrollmentInstance)
	if err != nil {
		vault, err = CreateLocal(directory, rootKey, enrollmentInstance)
	}
	if err != nil {
		return nil, err
	}
	defer vault.Close()
	return vault.PublicKey(), nil
}

// BindLocalEnrollment replaces only an unbound fresh enrollment record. The
// instance and public key must come from the completed fresh gate and its
// durable operator pin, never an archive. Repeat calls for that exact pair are
// safe after a crash; another instance cannot reuse the encrypted identity.
func BindLocalEnrollment(directory string, rootKey []byte, instance string, public []byte) (*Vault, error) {
	if instance == enrollmentInstance {
		return nil, errors.New("fresh enrollment requires an assigned installation identity")
	}
	if existing, err := OpenLocal(directory, rootKey, instance); err == nil {
		if bytes.Equal(existing.PublicKey(), public) {
			return existing, nil
		}
		existing.Close()
		return nil, errors.New("fresh enrollment public key differs from installed pin")
	}
	secret, err := RootKeySecret(rootKey)
	if err != nil {
		return nil, err
	}
	defer clear(secret)
	r, err := unseal(directory, secret, enrollmentInstance, os.Geteuid())
	if err != nil {
		return nil, err
	}
	defer r.clear()
	r.Instance = instance
	vault, err := decodeRecord(r, instance)
	if err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		if !complete {
			vault.Close()
		}
	}()
	if !bytes.Equal(vault.PublicKey(), public) {
		return nil, errors.New("fresh enrollment public key differs from installed pin")
	}
	ciphertext, err := seal(r, secret)
	if err != nil {
		return nil, err
	}
	dir, err := custodyDirectory(directory, false, os.Geteuid())
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	if err := publish(dir, ciphertext, true); err != nil {
		return nil, err
	}
	complete = true
	return vault, nil
}

// CreateLocal is reserved for explicitly enrolled unattended installations.
// It stores the same encrypted record as host custody, but requires directories
// and files owned by the current runtime UID. Root-only Create is unchanged.
// The supplied instance must come from authoritative installation inspection.
func CreateLocal(directory string, rootKey []byte, instance string) (*Vault, error) {
	secret, err := RootKeySecret(rootKey)
	if err != nil {
		return nil, err
	}
	defer clear(secret)
	return create(directory, secret, rootKey, instance, os.Geteuid())
}

// OpenLocal unlocks an enrolled installation using its existing root key.
// It never prompts, falls back to plaintext or changes the pinned instance.
func OpenLocal(directory string, rootKey []byte, instance string) (*Vault, error) {
	secret, err := RootKeySecret(rootKey)
	if err != nil {
		return nil, err
	}
	defer clear(secret)
	return open(directory, secret, instance, os.Geteuid())
}
