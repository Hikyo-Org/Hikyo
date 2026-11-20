package shell

import (
	"errors"
	"sync"
)

// Secret is what the keychain holds for one connection: the bearer and the id
// of its session, in one item so a crash cannot leave them out of step (D4).
type Secret struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
}

// ErrNoSecret means the keychain has no item for the connection.
var ErrNoSecret = errors.New("no stored session for this connection")

// SecretStore is the OS keychain. Implementations restrict access to the
// signed app (ADR D3) and must make Put atomic.
type SecretStore interface {
	Get(host string) (Secret, error)
	Put(host string, s Secret) error
	Delete(host string) error
}

// MemorySecrets is an in-memory SecretStore for tests and for platforms
// without a keychain. FailPut makes Put fail, to exercise keychain failures.
type MemorySecrets struct {
	mu      sync.Mutex
	items   map[string]Secret
	FailPut error
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
