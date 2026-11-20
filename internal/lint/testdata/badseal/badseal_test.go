package badseal

import "github.com/Hikyo-Org/hikyo/internal/crypto"

// seedSealed is a test helper that seeds a fixture database.
//
// fence:fixture seeds an isolated fixture database no rotation can race.
func seedSealed(s *crypto.ProjectSealer) []byte {
	b, _ := s.SealField(aad(), nil)
	return b
}

// seedNoReason names the marker without a reason.
//
// fence:fixture
func seedNoReason(s *crypto.ProjectSealer) []byte { // want "seedNoReason carries fence:fixture without a reason" "seedNoReason seals ciphertext but never calls the writer fence"
	b, _ := s.SealField(aad(), nil)
	return b
}

// unmarkedSeed has no marker, so the analyzer still judges it.
func unmarkedSeed(s *crypto.ProjectSealer) []byte { // want "unmarkedSeed seals ciphertext but never calls the writer fence"
	b, _ := s.SealField(aad(), nil)
	return b
}
