package shell

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/clienttrust"
)

// Trust is the TLS verification choice stored with a connection (ADR D5).
type Trust struct {
	Mode clienttrust.Mode `json:"mode"`
	// Pin is the leaf SPKI fingerprint for spki mode, empty for system-ca.
	Pin string `json:"pin,omitzero"`
}

// Connection is the non-secret record of one instance. The session token and
// session id live together in one keychain item, keyed by Host (ADR D4).
type Connection struct {
	// Host is the connection's own origin host (ADR D2): random, stored with
	// the connection and never reused, so deleted browser storage can never
	// be inherited by another instance.
	Host    string    `json:"host"`
	Origin  string    `json:"origin"`
	Name    string    `json:"name"`
	Trust   Trust     `json:"trust"`
	Created time.Time `json:"created"`
	// PrincipalID and PrincipalName identify the account the connection signed
	// in as. A sign-in that returns another principal needs confirmation (D3).
	PrincipalID   string `json:"principal_id,omitzero"`
	PrincipalName string `json:"principal_name,omitzero"`
	// WipePending marks a connection whose removal has begun and is not
	// finished. It is stored before anything is destroyed. It can't be opened or
	// signed in; removing it again retries.
	WipePending bool `json:"wipe_pending,omitzero"`
}

const (
	hostPrefix   = "c-"
	storeVersion = 1
	maxStoreSize = 1 << 20
)

type storeFile struct {
	Version     int          `json:"version"`
	Connections []Connection `json:"connections"`
}

// Store persists connection records as one JSON file with mode 0600.
type Store struct {
	path string
	mu   sync.Mutex
	list []Connection
}

// OpenStore loads path, treating a missing file as an empty store. A
// corrupt file is an error: the shell never guesses at connection data.
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	// Bounded before it is read: a huge file must not exhaust memory.
	data, err := io.ReadAll(io.LimitReader(f, maxStoreSize+1))
	_ = f.Close()
	if err != nil {
		return nil, err
	}
	if len(data) > maxStoreSize {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxStoreSize)
	}
	var file storeFile
	if err := decodeStrict(data, &file); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if file.Version != storeVersion {
		return nil, fmt.Errorf("%s has unsupported version %d", path, file.Version)
	}
	seen := map[string]bool{}
	for _, c := range file.Connections {
		if !validHost(c.Host) || c.Origin == "" || seen[c.Host] {
			return nil, fmt.Errorf("%s holds an invalid or duplicate connection", path)
		}
		seen[c.Host] = true
	}
	s.list = file.Connections
	return s, nil
}

func validHost(h string) bool {
	if len(h) != len(hostPrefix)+32 || h[:len(hostPrefix)] != hostPrefix {
		return false
	}
	// Lowercase only: the router lowercases a request host before the lookup.
	suffix := h[len(hostPrefix):]
	_, err := hex.DecodeString(suffix)
	return err == nil && suffix == strings.ToLower(suffix)
}

// NewHost draws a fresh random connection host that no stored connection uses.
func (s *Store) NewHost() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for range 8 {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", err
		}
		host := hostPrefix + hex.EncodeToString(raw[:])
		if !slices.ContainsFunc(s.list, func(c Connection) bool { return c.Host == host }) {
			return host, nil
		}
	}
	return "", errors.New("could not draw an unused connection host")
}

// List returns a copy of the stored connections.
func (s *Store) List() []Connection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.list)
}

// Put adds or replaces a connection by host.
func (s *Store) Put(c Connection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := slices.Clone(s.list)
	if i := slices.IndexFunc(next, func(x Connection) bool { return x.Host == c.Host }); i >= 0 {
		next[i] = c
	} else {
		next = append(next, c)
	}
	if err := s.write(next); err != nil {
		return err
	}
	s.list = next
	return nil
}

// Remove deletes a connection record.
func (s *Store) Remove(host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := slices.DeleteFunc(slices.Clone(s.list), func(c Connection) bool { return c.Host == host })
	if err := s.write(next); err != nil {
		return err
	}
	s.list = next
	return nil
}

func (s *Store) write(list []Connection) error {
	data, err := json.Marshal(storeFile{Version: storeVersion, Connections: list})
	if err != nil {
		return err
	}
	// OpenStore refuses a larger file, so writing one would lock the app out.
	if len(data) > maxStoreSize {
		return fmt.Errorf("the connection list would exceed %d bytes", maxStoreSize)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".connections-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
