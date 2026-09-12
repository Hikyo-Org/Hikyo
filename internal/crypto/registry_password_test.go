package crypto

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestRegistryPasswordHashInteroperatesWithBcrypt(t *testing.T) {
	password := []byte("disposable-registry-fixture-password")
	hash, err := RegistryPasswordHash(password)
	if err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword(hash, password); err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte("wrong")); err == nil {
		t.Fatal("accepted wrong registry password")
	}
}
