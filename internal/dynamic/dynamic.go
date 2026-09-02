// Package dynamic defines the compiled-in dynamic-secret provider seam and the
// credential-material generator. A Provider mints, extends, probes, and drops a
// short-lived credential at an external engine; it never reads a secret back
// out. The first (and only v1) implementation is internal/dynamic/postgres.
package dynamic

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

// Kind is the closed provider enum. A row or request naming anything else is
// refused by the default arm of every switch (fail-closed).
type Kind string

const KindPostgres Kind = "postgres"

// ParseKind rejects an unknown provider kind rather than defaulting one.
func ParseKind(s string) (Kind, error) {
	if s == string(KindPostgres) {
		return KindPostgres, nil
	}
	return "", fmt.Errorf("dynamic: unknown provider kind %q", s)
}

// Sentinel errors classify a provider outcome so the caller can decide state.
// A definite failure means nothing durable changed at the provider (a connect
// refusal before any statement); an ambiguous outcome means a statement may or
// may not have taken effect (a deadline or network error after send) and MUST
// NOT be reported as success: the lease enters `unknown` and reconcile settles
// it by re-probing.
var (
	// ErrUnreachable is a definite failure: the provider could not be reached
	// or authenticated, so no statement ran.
	ErrUnreachable = errors.New("dynamic: provider unreachable")
	// ErrAmbiguous is an uncertain outcome: a statement was sent but its result
	// is unknown. Never treated as success.
	ErrAmbiguous = errors.New("dynamic: provider outcome ambiguous")
	// ErrRefused is a DEFINITE failure the engine reported: a statement reached
	// the server and it answered with an error (a missing grant role, a
	// privilege refusal, a syntax error). The role state is known — the DDL did
	// not take effect — so the lease settles to a terminal failure, not unknown.
	ErrRefused = errors.New("dynamic: provider refused the request")
)

// CreateRoleRequest is one lease mint at the provider. GrantRole is the parent
// role the minted role inherits (IN ROLE), so a lease role is not privilege-
// less; ValidUntil is enforced by the engine itself, so a lease expires even if
// Hikyo is down.
type CreateRoleRequest struct {
	Name       string
	Password   string
	GrantRole  string
	ValidUntil time.Time
}

// RoleStatus is what a re-probe learns: whether the role exists and, if so,
// until when it is valid. Reconcile uses ValidUntil to settle a renew, not just
// a mint or revoke.
type RoleStatus struct {
	Exists     bool
	ValidUntil time.Time
}

// Factory builds a Provider for one operation from the sealed-then-opened
// admin credential and the provider's stored origin/tls_mode. The app supplies
// it, closing over the egress policy and connection deadline, so the service
// and worker never name a concrete engine package. The caller Closes the
// Provider when the operation ends.
type Factory func(kind Kind, origin, tlsMode, credential string) (Provider, error)

// Provider is the closed four-operation seam. No method reads a credential
// value back; there is no arbitrary-SQL escape hatch. A reflection test pins
// this interface so it cannot grow one silently.
type Provider interface {
	CreateRole(ctx context.Context, req CreateRoleRequest) error
	ExtendRole(ctx context.Context, name string, validUntil time.Time) error
	DropRole(ctx context.Context, name string) error
	RoleStatus(ctx context.Context, name string) (RoleStatus, error)
	// Close releases pooled connections and forgets the admin credential.
	Close()
}

// passwordAlphabet is the credential charset: unambiguous alphanumerics only.
// A restricted charset is the fuzz contract and keeps the value safe to embed
// in a quoted DDL literal after the engine's own escaping.
const passwordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

const passwordLength = 32

// GeneratePassword returns a high-entropy alphanumeric credential. Rejection
// sampling keeps the distribution uniform over the alphabet.
func GeneratePassword() (string, error) {
	out := make([]byte, passwordLength)
	buf := make([]byte, 1)
	for i := 0; i < passwordLength; {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("dynamic: generate password: %w", err)
		}
		// 256 is not a multiple of len(alphabet); discard the biased tail so
		// every character is equally likely.
		max := byte(256 - (256 % len(passwordAlphabet)))
		if buf[0] >= max {
			continue
		}
		out[i] = passwordAlphabet[int(buf[0])%len(passwordAlphabet)]
		i++
	}
	return string(out), nil
}

// ValidPassword reports whether s is exactly the generated shape: the fixed
// length over the fixed alphabet. The provider refuses anything else before
// building SQL, so a value that somehow arrived off-charset never reaches the
// engine.
func ValidPassword(s string) bool {
	if len(s) != passwordLength {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !inAlphabet(s[i]) {
			return false
		}
	}
	return true
}

func inAlphabet(b byte) bool {
	for i := 0; i < len(passwordAlphabet); i++ {
		if passwordAlphabet[i] == b {
			return true
		}
	}
	return false
}

// roleNamePrefix is the fixed marker every Hikyo-minted role carries, so an
// operator scanning pg_roles can tell which roles Hikyo owns.
const roleNamePrefix = "hikyo_"

// roleNameAlphabet is the lowercase-alphanumeric charset a role name is reduced
// to. PostgreSQL folds unquoted identifiers to lowercase; restricting to this
// set means the stored handle and the engine's own name never disagree.
const roleNameAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// roleNameMaxBody is how many normalized lease-id characters the role name
// keeps. A lease id is `dls_<uuidv7>` (~35 alphanumerics after normalization);
// 56 keeps the WHOLE id — UUIDv7's timestamp is common to same-millisecond
// mints, so truncating would keep too few RANDOM bits and risk a collision. The
// result stays under PostgreSQL's 63-byte identifier limit (6 + 56 = 62).
const roleNameMaxBody = 56

// RoleName derives a stable, engine-safe role name from a lease id. The lease
// id is already unique, so the derived name is too.
func RoleName(leaseID string) string {
	out := make([]byte, 0, len(roleNamePrefix)+roleNameMaxBody)
	out = append(out, roleNamePrefix...)
	for i := 0; i < len(leaseID) && len(out) < len(roleNamePrefix)+roleNameMaxBody; i++ {
		c := leaseID[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+('a'-'A'))
		case inRoleAlphabet(c):
			out = append(out, c)
		}
	}
	// A lease id with no usable characters is impossible (UUIDv7 hex), but keep
	// the name non-empty rather than emit a bare prefix.
	if len(out) == len(roleNamePrefix) {
		out = append(out, "role"...)
	}
	return string(out)
}

func inRoleAlphabet(b byte) bool {
	for i := 0; i < len(roleNameAlphabet); i++ {
		if roleNameAlphabet[i] == b {
			return true
		}
	}
	return false
}

// ValidRoleName reports whether s is a well-formed Hikyo role name: the fixed
// prefix followed by lowercase alphanumerics, within the identifier limit.
func ValidRoleName(s string) bool {
	if len(s) <= len(roleNamePrefix) || len(s) > 63 {
		return false
	}
	if s[:len(roleNamePrefix)] != roleNamePrefix {
		return false
	}
	for i := len(roleNamePrefix); i < len(s); i++ {
		if !inRoleAlphabet(s[i]) {
			return false
		}
	}
	return true
}
