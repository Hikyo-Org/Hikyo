// Package transittest holds the in-memory mock external custody provider
// (transit ADR D3). It stands in for an HSM, KMIP server or cloud KMS: the
// material lives only inside the mock, Hikyo stores an opaque reference, and the
// provider can be switched unavailable to prove the fail-closed path. It is
// never wired into a production binary.
package transittest

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/transit"
)

// External is the mock external custody provider.
type External struct {
	down     atomic.Bool
	mu       sync.Mutex
	material map[string][]byte
	// Calls counts material-bearing operations that reached the provider while
	// it was available; a test reads it to prove an unavailable provider did
	// no work.
	Calls atomic.Int64
}

var _ transit.Custody = (*External)(nil)

// NewExternal returns an available mock provider with no material.
func NewExternal() *External {
	return &External{material: map[string][]byte{}}
}

// SetAvailable switches the provider on or off.
func (e *External) SetAvailable(on bool) { e.down.Store(!on) }

// Holds reports whether the provider still holds material for ref.
func (e *External) Holds(ref string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.material[ref]
	return ok
}

func (e *External) Kind() transit.CustodyKind { return transit.CustodyExternal }

func (e *External) Available(context.Context) error {
	if e.down.Load() {
		return transit.ErrUnavailable
	}
	return nil
}

func (e *External) Create(ctx context.Context, t transit.Target) (transit.Version, error) {
	if err := e.Available(ctx); err != nil {
		return transit.Version{}, err
	}
	e.Calls.Add(1)
	material, err := crypto.NewTransitMaterial()
	if err != nil {
		return transit.Version{}, err
	}
	public, err := transit.PublicKeyFor(material, t)
	if err != nil {
		crypto.Zero(material)
		return transit.Version{}, err
	}
	ref := "mock-" + uuid.NewString()
	e.mu.Lock()
	e.material[ref] = material
	e.mu.Unlock()
	return transit.Version{ExternalRef: ref, PublicKey: public}, nil
}

func (e *External) with(ctx context.Context, t transit.Target, v transit.Version, fn func(*crypto.TransitKey) ([]byte, error)) ([]byte, error) {
	if err := e.Available(ctx); err != nil {
		return nil, err
	}
	e.mu.Lock()
	material, ok := e.material[v.ExternalRef]
	var copied []byte
	if ok {
		copied = append([]byte(nil), material...)
	}
	e.mu.Unlock()
	if !ok || v.ExternalRef == "" {
		return nil, transit.ErrMaterialMissing
	}
	defer crypto.Zero(copied)
	e.Calls.Add(1)
	return transit.Operate(copied, t, fn)
}

func (e *External) Encrypt(ctx context.Context, t transit.Target, v transit.Version, plaintext, aad []byte) ([]byte, error) {
	return e.with(ctx, t, v, func(k *crypto.TransitKey) ([]byte, error) { return k.Encrypt(plaintext, aad) })
}

func (e *External) Decrypt(ctx context.Context, t transit.Target, v transit.Version, record, aad []byte) ([]byte, error) {
	return e.with(ctx, t, v, func(k *crypto.TransitKey) ([]byte, error) { return k.Decrypt(record, aad) })
}

func (e *External) Sign(ctx context.Context, t transit.Target, v transit.Version, message []byte) ([]byte, error) {
	return e.with(ctx, t, v, func(k *crypto.TransitKey) ([]byte, error) { return k.Sign(message) })
}

func (e *External) MAC(ctx context.Context, t transit.Target, v transit.Version, message []byte) ([]byte, error) {
	return e.with(ctx, t, v, func(k *crypto.TransitKey) ([]byte, error) { return k.MAC(message) })
}

func (e *External) Destroy(ctx context.Context, _ transit.Target, v transit.Version) error {
	if err := e.Available(ctx); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if m, ok := e.material[v.ExternalRef]; ok {
		crypto.Zero(m)
		delete(e.material, v.ExternalRef)
	}
	return nil
}
