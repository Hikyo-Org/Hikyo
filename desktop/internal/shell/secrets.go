package shell

import (
	"errors"
	"sync"

	"github.com/Hikyo-Org/hikyo/internal/clienttrust"
)

// Secret is what the keychain holds for one connection: the bearer and the id
// of its session, in one item so a crash cannot leave them out of step (D4).
// It also records where the bearer may be spent (origin, trust mode, pin) and
// whom it belongs to. connections.json is editable by any process of the same
// user, the keychain item is not, so a record that disagrees with the item is
// refused instead of trusted (see bindingMismatch).
type Secret struct {
	Token       string           `json:"token"`
	SessionID   string           `json:"session_id"`
	Origin      string           `json:"origin"`
	TrustMode   clienttrust.Mode `json:"trust_mode"`
	Pin         string           `json:"pin,omitzero"`
	PrincipalID string           `json:"principal_id,omitzero"`
}

// bindingMismatch names the first way the connection record departs from what
// the keychain item recorded, or "" when they agree.
func (s Secret) bindingMismatch(info Connection) string {
	switch {
	case s.Origin != info.Origin:
		return "origin"
	case s.TrustMode != info.Trust.Mode:
		return "trust mode"
	case s.Pin != info.Trust.Pin:
		return "certificate pin"
	case s.PrincipalID != info.PrincipalID:
		return "account"
	}
	return ""
}

// ErrNoSecret means the keychain has no item for the connection.
var ErrNoSecret = errors.New("no stored session for this connection")

// ErrItemUntrusted means the keychain item exists but is not restricted to this
// application, so another process can read it. This is an access check, not proof of who wrote the item. Get returns
// it instead of the secret; the item is never used or revoked through.
var ErrItemUntrusted = errors.New("the keychain item is not restricted to this application")

// SecretStore is the OS keychain. Implementations restrict access to the
// signed app (ADR D3), and Get refuses (ErrItemUntrusted) an item whose access
// list is anything else. Put replaces the item as a whole: it deletes any
// existing item first, so a pre-planted item's access list is never kept, and
// never leaves a half-written one. A failed Put leaves either the previous item
// intact (it failed before the delete) or no item (it failed after), so a caller
// that needs the old session must read it back (Conn.putFailed).
type SecretStore interface {
	Get(host string) (Secret, error)
	Put(host string, s Secret) error
	Delete(host string) error
}

// MemorySecrets is an in-memory SecretStore for tests and for platforms
// without a keychain. FailPut makes Put fail before it changes anything;
// FailPutAfterDelete makes it fail after it removed the old item, as a keychain
// replacement that deletes first and then cannot add does.
type MemorySecrets struct {
	mu                 sync.Mutex
	items              map[string]Secret
	FailPut            error
	FailPutAfterDelete error
}

// Get implements SecretStore.
func (m *MemorySecrets) Get(host string) (Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.items[host]
	if !ok {
		return Secret{}, ErrNoSecret
	}
	return s, nil
}

// Put implements SecretStore.
func (m *MemorySecrets) Put(host string, s Secret) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailPut != nil {
		return m.FailPut
	}
	if m.FailPutAfterDelete != nil {
		delete(m.items, host)
		return m.FailPutAfterDelete
	}
	if m.items == nil {
		m.items = map[string]Secret{}
	}
	m.items[host] = s
	return nil
}

// Delete implements SecretStore.
func (m *MemorySecrets) Delete(host string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, host)
	return nil
}
