package shell

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

// Conn is one live connection: its instance channel, its session and the
// proxy that spends it. The bearer never leaves this type except in a request
// to the connection's own instance.
type Conn struct {
	m    *Manager
	host string
	inst *Instance
	url  *url.URL

	// ctx ends when the connection is removed, which ends its ceremonies. It
	// is replaced if a removal fails part way. Guarded by mu; use life().
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.RWMutex
	info    Connection
	secret  Secret
	stopped string
	// tampered is set when the connection record disagrees with the keychain
	// item. Such a connection is never used, signed in again or revoked: the
	// record can't be trusted to say where the bearer goes.
	tampered bool

	// rotMu serializes token rotation: one at a time (ADR D4).
	rotMu   sync.Mutex
	removed bool // set under rotMu by Remove: no new session may be stored
	flights flights
	proxy   *httputil.ReverseProxy

	reauthSlot, signInSlot, downloadSlot slot
	reauthLimit, signInLimit             *limiter
	openLimit, downloadLimit             *limiter
	linkLimit, clipLimit, alertLimit     *limiter
}

func (m *Manager) newConn(info Connection) (*Conn, error) {
	inst, err := newInstance(info.Origin, info.Trust, m.userAgent(), m.opts.AllowLoopbackHTTP)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(inst.Origin)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Conn{
		ctx: ctx, cancel: cancel,
		m: m, host: info.Host, inst: inst, url: u, info: info,
		reauthLimit:   newLimiter(6, time.Minute, m.now),
		signInLimit:   newLimiter(6, time.Minute, m.now),
		openLimit:     newLimiter(10, time.Minute, m.now),
		downloadLimit: newLimiter(20, time.Minute, m.now),
		linkLimit:     newLimiter(10, time.Minute, m.now),
		clipLimit:     newLimiter(30, time.Minute, m.now),
		alertLimit:    newLimiter(4, time.Minute, m.now),
	}
	switch secret, err := m.secrets.Get(info.Host); {
	case err == nil:
		if field := secret.bindingMismatch(info); field != "" {
			c.tampered = true
			c.stopped = "The saved connection settings (" + field + ") do not match the keychain. " +
				"Something changed this connection outside the app, so it was not used. Remove the connection and add it again."
			log.Printf("connection %s: record disagrees with the keychain (%s)", info.Host, field)
			break
		}
		c.secret = secret
	case errors.Is(err, ErrNoSecret):
		c.stopped = "This connection has no stored session. Sign in again."
	default:
		c.stopped = "The keychain could not be read: " + err.Error()
	}
	c.proxy = c.newProxy()
	return c, nil
}

// life is the context that ends when the connection is removed.
func (c *Conn) life() context.Context {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ctx
}

// beginRemoval stops ceremonies and refuses new sessions; undo reverses it
// when the removal fails and the connection stays in the list.
func (c *Conn) beginRemoval() (undo func()) {
	c.rotMu.Lock()
	c.removed = true
	c.rotMu.Unlock()
	c.mu.Lock()
	c.cancel()
	c.mu.Unlock()
	return func() {
		c.rotMu.Lock()
		c.removed = false
		c.rotMu.Unlock()
		c.mu.Lock()
		c.ctx, c.cancel = context.WithCancel(context.Background())
		c.mu.Unlock()
	}
}

// errRemoved means the connection was removed while a ceremony was running.
var errRemoved = errors.New("the connection was removed")

func (c *Conn) bearer() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.stopped != "" {
		return ""
	}
	return c.secret.Token
}

func (c *Conn) sessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.secret.SessionID
}

// Stopped returns why the connection can't be used, or "" when it can.
func (c *Conn) Stopped() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stopped
}

// Info returns the connection record.
func (c *Conn) Info() Connection {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.info
}

// stop ends use of the session until the user signs in again, and tells the
// user why (ADR D4: a visible error).
func (c *Conn) stop(reason string) {
	c.mu.Lock()
	c.stopped = reason
	c.mu.Unlock()
	c.flights.cancelAll()
	log.Printf("connection %s stopped: %s", c.host, reason)
	c.m.native.Alert("Hikyo connection stopped", fmt.Sprintf("%s\n\n%s", c.Info().Name, reason))
}

// setSession stores a fresh session after a sign-in and clears a stop. The
// keychain write comes first: if it fails nothing in memory changes.
func (c *Conn) setSession(token, sessionID, principalID, principalName string) error {
	c.mu.RLock()
	tampered := c.tampered
	info := c.info
	c.mu.RUnlock()
	if tampered {
		return errors.New("this connection's saved settings were changed outside the app; remove it and add it again")
	}
	info.PrincipalID, info.PrincipalName = principalID, principalName
	s := Secret{Token: token, SessionID: sessionID, Origin: info.Origin,
		TrustMode: info.Trust.Mode, Pin: info.Trust.Pin, PrincipalID: principalID}
	if err := c.m.secrets.Put(c.host, s); err != nil {
		return err
	}
	c.mu.Lock()
	c.secret = s
	c.stopped = ""
	c.info = info
	c.mu.Unlock()
	c.flights.cancelStreams()
	return c.m.store.Put(info)
}

// revoke asks the instance to delete the session behind token. It is best
// effort and bounded.
func (c *Conn) revoke(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	status, _, _, err := c.inst.call(ctx, http.MethodPost, "/api/v1/auth/logout", token, nil, "")
	if err != nil {
		return err
	}
	// 401 means the session was already gone: nothing is left to revoke.
	if status/100 != 2 && status != http.StatusUnauthorized {
		return fmt.Errorf("the instance answered %d", status)
	}
	return nil
}

// revokeAsync revokes a token that must not be left live, off the response path.
func (c *Conn) revokeAsync(token string) {
	if !parsesAsCLIToken(token) {
		return
	}
	c.m.background(func() {
		if err := c.revoke(context.Background(), token); err != nil {
			log.Printf("connection %s: revoking an unwanted session failed: %v", c.host, err)
		}
	})
}

// applyRotation runs the ADR D4 checks on a token returned by a listed route
// and, if they pass, stores it in the keychain, swaps it in, and only then
// lets the caller release the response. On any failure the connection stops
// and the new session is revoked.
func (c *Conn) applyRotation(kind Rotation, view tokenView) error {
	c.rotMu.Lock()
	defer c.rotMu.Unlock()
	if c.removed {
		c.revokeAsync(view.Token)
		return errRemoved
	}
	c.mu.RLock()
	stored, principal := c.secret, c.info.PrincipalID
	tampered := c.tampered
	c.mu.RUnlock()
	if tampered {
		return errors.New("the connection record was changed outside the app")
	}
	newID, err := checkRotation(kind, view, stored, principal)
	if err != nil {
		c.revokeAsync(view.Token)
		c.stop("The instance returned a session the app can't accept (" + err.Error() + "). Sign in again.")
		return err
	}
	rotated := stored
	rotated.Token, rotated.SessionID = view.Token, newID
	if err := c.m.secrets.Put(c.host, rotated); err != nil {
		c.revokeAsync(view.Token)
		c.stop("The new session could not be saved to the keychain (" + err.Error() + "). Sign in again.")
		return err
	}
	c.mu.Lock()
	c.secret = rotated
	c.mu.Unlock()
	c.flights.cancelStreams()
	return nil
}

func (c *Conn) isTampered() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tampered
}

// confirmSignIn names the origin before a sign-in that has no stored session to
// vouch for it. Without a session the keychain can't say the record is the one
// the person added, so the person says so.
func (c *Conn) confirmSignIn() bool {
	if c.bearer() != "" {
		return true
	}
	info := c.Info()
	return c.m.native.Confirm("Sign in to this instance?",
		fmt.Sprintf("origin: %s\nname: %s\n\nSign in opens your browser at this address. Continue only if it is the instance you added.", info.Origin, logSafe(info.Name)), "Sign in")
}
