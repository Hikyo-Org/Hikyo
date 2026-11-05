package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
)

// registry is every connection the shell serves and the window that belongs to each. Request goroutines read it
// while connections are added and removed, so everything goes through the lock.
type registry struct {
	mu      sync.RWMutex
	conns   map[string]*conn  // host -> connection
	windows map[string]string // window id -> host
	order   []string          // hosts in the order they were added
}

func newRegistry() *registry {
	return &registry{conns: map[string]*conn{}, windows: map[string]string{}}
}

func (r *registry) add(host string, c *conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.conns[host]; !dup {
		r.order = append(r.order, host)
	}
	r.conns[host] = c
}

// bind ties a window to a host. It must happen before the window's first load, or that load is refused.
func (r *registry) bind(id, host string) {
	r.mu.Lock()
	r.windows[id] = host
	r.mu.Unlock()
}

// remove forgets a connection and its windows, so its host is refused as unknown from then on.
func (r *registry) remove(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.conns, host)
	for id, h := range r.windows {
		if h == host {
			delete(r.windows, id)
		}
	}
	for i, h := range r.order {
		if h == host {
			r.order = append(r.order[:i:i], r.order[i+1:]...)
			break
		}
	}
}

func (r *registry) conn(host string) *conn {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.conns[host]
}

func (r *registry) hosts() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

// owns reports whether the window id belongs to the host. An empty or unknown id owns nothing.
func (r *registry) owns(id, host string) bool {
	if id == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.windows[id]
	return ok && h == host
}

func (r *registry) idOf(host string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for id, h := range r.windows {
		if h == host {
			return id
		}
	}
	return ""
}

// bindOwner names the connection a request came from by its window id. The native side knows the renderer that sent
// the request and passes its host as owner. A page can send the header itself, so its value goes first. An owner with
// no window leaves no id, and the shell refuses a request without one.
func bindOwner(req *http.Request, owner string, reg *registry) {
	req.Header.Del("x-wails-window-id")
	if id := reg.idOf(owner); owner != "" && id != "" {
		req.Header.Set("x-wails-window-id", id)
	}
}

// randomLabel is an unguessable host label (D2): lowercase hex, because WebKit and Chromium lowercase hosts and a
// mixed-case label would never match. It is 33 characters, inside the 63 a label may have.
func randomLabel() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // no entropy: refuse to invent a guessable host
	}
	return "c" + hex.EncodeToString(b)
}
