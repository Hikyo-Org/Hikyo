// Package sealfixture seals ciphertext for tests that seed an isolated fixture
// database. The fence analyzer judges internal/service, whose production write
// paths must call the writer fence before sealing; a fixture seeder is not a
// service write path, so its sealing lives here, outside that package.
package sealfixture

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

// Field seals a project field value under the project sealer.
func Field(t *testing.T, sealer *crypto.ProjectSealer, aad crypto.ProjectFieldAAD, plaintext []byte) []byte {
	t.Helper()
	sealed, err := sealer.SealField(aad, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

// Value seals a value cell under the project sealer.
func Value(t *testing.T, sealer *crypto.ProjectSealer, aad crypto.ValueAAD, plaintext []byte) []byte {
	t.Helper()
	sealed, err := sealer.SealValue(aad, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}
