// Package transit is the custody seam of the transit surface (#156, transit
// ADR D3). A custody provider decides where one key version's material lives
// and performs the operations that need it; it never decides whether an
// operation is permitted (the service does, inside its transaction) and it never
// touches a cryptographic primitive (internal/crypto does).
//
// Two providers exist. Software custody seals material under the owning project
// DEK, so the key hierarchy's rotation, reencrypt and crypto-shred cover it.
// External custody (an HSM, KMIP or a cloud KMS) holds the material itself and
// Hikyo stores an opaque reference; this release ships only the in-memory mock
// in transittest, used to prove both providers keep identical semantics.
//
// The seam is operation-level on purpose: a hardware provider must never have
// to release material to Hikyo, so "give me the key bytes" is not a method.
package transit

import (
	"context"
	"errors"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

// CustodyKind is the closed custody set, fixed per key at creation.
type CustodyKind string

const (
	CustodySoftware CustodyKind = "software"
	CustodyExternal CustodyKind = "external"
)

// ParseCustodyKind refuses anything outside the closed set.
func ParseCustodyKind(s string) (CustodyKind, error) {
	switch CustodyKind(s) {
	case CustodySoftware, CustodyExternal:
		return CustodyKind(s), nil
	}
	return "", fmt.Errorf("transit: unknown custody kind %q", s)
}

var (
	// ErrUnavailable is the fail-closed answer of a custody provider that is not
	// registered, not reachable, or reports itself down. It is retryable and it
	// never triggers a fallback to another provider.
	ErrUnavailable = errors.New("transit: key custody provider unavailable")
	// ErrMaterialMissing reports a version whose material is gone at the
	// provider (destroyed, trimmed, or never created there), for example after a
	// restore resurrected a reference the provider no longer holds.
	ErrMaterialMissing = errors.New("transit: key version material is not held by its custody provider")
)

// Target names one key version: the binding every derived key is bound to,
// plus the version row id the software provider's sealing AAD is anchored to.
type Target struct {
	Binding      crypto.TransitBinding
	VersionRowID string
}

// Version is what the datastore keeps for one key version. Exactly one of
// Sealed (software) or ExternalRef (external) is set. PublicKey is public
// metadata for signing keys.
type Version struct {
	Sealed []byte
	// SealedDEKVersion is the project DEK version Sealed was written under; the
	// caller fences on it in the transaction that stores the row.
	SealedDEKVersion uint32
	ExternalRef      string
	PublicKey        []byte
}

// Custody performs the material-bearing half of every transit operation.
// Implementations must be safe for concurrent use and must honour ctx.
type Custody interface {
	Kind() CustodyKind
	// Available reports ErrUnavailable when the provider cannot serve.
	Available(ctx context.Context) error
	// Create makes new material for t and returns what the datastore stores.
	Create(ctx context.Context, t Target) (Version, error)
	Encrypt(ctx context.Context, t Target, v Version, plaintext, aad []byte) ([]byte, error)
	Decrypt(ctx context.Context, t Target, v Version, record, aad []byte) ([]byte, error)
	Sign(ctx context.Context, t Target, v Version, message []byte) ([]byte, error)
	MAC(ctx context.Context, t Target, v Version, message []byte) ([]byte, error)
	// Destroy erases the material at the provider. Software custody has nothing
	// to erase outside the row the caller deletes.
	Destroy(ctx context.Context, t Target, v Version) error
}

// Registry resolves a key's recorded custody kind to its provider. A kind with
// no registered provider is ErrUnavailable: the key row's custody decides, and
// nothing substitutes another provider.
type Registry struct {
	providers map[CustodyKind]Custody
}

// NewRegistry registers providers by their own Kind. A duplicate kind is a
// wiring bug and panics.
func NewRegistry(providers ...Custody) *Registry {
	r := &Registry{providers: map[CustodyKind]Custody{}}
	for _, p := range providers {
		if _, dup := r.providers[p.Kind()]; dup {
			panic("transit: custody kind registered twice: " + string(p.Kind()))
		}
		r.providers[p.Kind()] = p
	}
	return r
}

// Resolve returns the provider for kind, or ErrUnavailable.
func (r *Registry) Resolve(kind CustodyKind) (Custody, error) {
	if r == nil {
		return nil, ErrUnavailable
	}
	p, ok := r.providers[kind]
	if !ok {
		return nil, ErrUnavailable
	}
	return p, nil
}

// Registered reports whether a provider is registered for kind, so key
// creation can refuse a custody the instance cannot serve.
func (r *Registry) Registered(kind CustodyKind) bool {
	if r == nil {
		return false
	}
	_, ok := r.providers[kind]
	return ok
}

// Operate derives the operation key for one version from material and runs fn
// with it, destroying the derived key afterwards. It is the shared tail of every
// provider that holds raw material (software, and the test mock).
func Operate(material []byte, t Target, fn func(*crypto.TransitKey) ([]byte, error)) ([]byte, error) {
	k, err := crypto.OpenTransitKey(material, t.Binding)
	if err != nil {
		return nil, err
	}
	defer k.Destroy()
	return fn(k)
}

// PublicKeyFor returns the Ed25519 public key for signing material, or nil for
// any other algorithm.
func PublicKeyFor(material []byte, t Target) ([]byte, error) {
	if t.Binding.Algorithm != crypto.TransitEd25519 {
		return nil, nil
	}
	return Operate(material, t, func(k *crypto.TransitKey) ([]byte, error) { return k.PublicKey() })
}
