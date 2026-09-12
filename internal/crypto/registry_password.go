package crypto

import "golang.org/x/crypto/bcrypt"

// RegistryPasswordHash produces the bcrypt format required by a distribution
// registry's htpasswd backend. The disposable native-consumer fixture uses it;
// Hikyo account passwords continue to use the Argon2id policy in password.go.
func RegistryPasswordHash(password []byte) ([]byte, error) {
	return bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
}
