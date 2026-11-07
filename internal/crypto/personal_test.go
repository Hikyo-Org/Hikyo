package crypto

import (
	"crypto/rand"
	"errors"
	"testing"
)

// A personal value opens only under the exact owner, key and environment it
// was sealed for: a ciphertext moved onto another person's row, or another
// key's row, must not open (personal-values ADR § Encryption).
func TestPersonalValueAADBindsOwner(t *testing.T) {
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i)
	}
	sealedFor := PersonalValueAAD{OrgID: "org_1", ProjectID: "prj_1", EnvID: "env_1", KeyID: "key_1", OwnerPrincipalID: "usr_alice"}
	record, err := seal(rand.Reader, key, []byte("dek_test"), 1, sealedFor, []byte("personal"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := open(key, []byte("dek_test"), 1, sealedFor, record); err != nil {
		t.Fatalf("the owner's own row does not open: %v", err)
	}
	moved := []PersonalValueAAD{
		{OrgID: "org_1", ProjectID: "prj_1", EnvID: "env_1", KeyID: "key_1", OwnerPrincipalID: "usr_mallory"},
		{OrgID: "org_1", ProjectID: "prj_1", EnvID: "env_1", KeyID: "key_2", OwnerPrincipalID: "usr_alice"},
		{OrgID: "org_1", ProjectID: "prj_1", EnvID: "env_2", KeyID: "key_1", OwnerPrincipalID: "usr_alice"},
	}
	for _, aad := range moved {
		if _, err := open(key, []byte("dek_test"), 1, aad, record); !errors.Is(err, ErrDecrypt) {
			t.Errorf("a ciphertext moved to %+v opened (err %v)", aad, err)
		}
	}
	// A value-kind AAD with the same coordinates is a different context.
	if _, err := open(key, []byte("dek_test"), 1, ValueAAD{OrgID: "org_1", ProjectID: "prj_1", EnvID: "env_1", KeyID: "key_1", RowID: "usr_alice"}, record); err == nil {
		t.Error("a personal value opened as an ordinary value")
	}
}
