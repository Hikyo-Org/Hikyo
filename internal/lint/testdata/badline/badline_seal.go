//line reencrypt.go:1
package badline

import "github.com/Hikyo-Org/hikyo/internal/crypto"

func aad() crypto.ProjectFieldAAD { return crypto.ProjectFieldAAD{} }

func seal(s *crypto.ProjectSealer) []byte {
	b, _ := s.SealField(aad(), nil)
	return b
}
