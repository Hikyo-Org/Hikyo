// Package badline is a fixture whose production file claims, through a //line
// directive, to be a _test.go file. Every diagnostic below must still be
// judged as a production-file diagnostic.
//
//line claimed_test.go:1
package badline

import (
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
)

var _ = pggen.New

func aad() crypto.ProjectFieldAAD { return crypto.ProjectFieldAAD{} }

func take(p authz.Proof) { _ = p }

func markedNil() {
	take(nil) /* forgeguard:nil-proof claimed to be a test file */
}

// fence:fixture claimed to be a test file.
func seed(s *crypto.ProjectSealer) []byte {
	b, _ := s.SealField(aad(), nil)
	return b
}
