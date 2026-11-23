package badseal

import "github.com/Hikyo-Org/hikyo/internal/crypto"

// A test-file seeder is judged like any other function: no exemption exists.
func unmarkedSeed(s *crypto.ProjectSealer) []byte { // want "unmarkedSeed seals ciphertext but never calls the writer fence"
	b, _ := s.SealField(aad(), nil)
	return b
}
