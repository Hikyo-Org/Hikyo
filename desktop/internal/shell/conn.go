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
	// item. Such a connection is never used or signed in again. Removal
	// revokes only through the item's own binding, and only when the item
	// passes the access check.
	// It also marks a record that can never be used (an invalid transport, a
	// removal whose storage wipe is still owed).
	tampered bool

	// rotMu serializes token rotation: one at a time (ADR D4).
	rotMu sync.Mutex
	// sessGate admits one session change at a time from before the bearer is
	// chosen until the new session is committed or the change fails (ADR D4).
	// It is separate from rotMu, which applyRotation takes inside the gate.
	sessGate gate
	removed  bool // set under rotMu by Remove: no new session may be stored
	flights  flights
	proxy    *httputil.ReverseProxy

	reauthSlot, signInSlot, downloadSlot slot
	reauthLimit, signInLimit             *limiter
	openLimit, downloadLimit             *limiter
	linkLimit, clipLimit, alertLimit     *limiter
}

func (m *Manager) newConn(info Connection) (*Conn, error) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Conn{
		ctx: ctx, cancel: cancel,
		m: m, host: info.Host, info: info,
		sessGate:      newGate(),
		reauthLimit:   newLimiter(6, time.Minute, m.now),
		signInLimit:   newLimiter(6, time.Minute, m.now),
		openLimit:     newLimiter(10, time.Minute, m.now),
		downloadLimit: newLimiter(20, time.Minute, m.now),
		linkLimit:     newLimiter(10, time.Minute, m.now),
		clipLimit:     newLimiter(30, time.Minute, m.now),
		alertLimit:    newLimiter(4, time.Minute, m.now),
	}
	// A record the transport refuses (a plain-http origin without the
	// development switch, an edited trust mode) must not stop the app: it is
	// kept stopped and removable, with no instance to send anything to.
	inst, err := newInstance(info.Origin, info.Trust, m.userAgent(), m.opts.AllowLoopbackHTTP)
	if err != nil {
		c.tampered = true
		c.stopped = "This connection can't be opened: " + err.Error() + ". Remove it, or fix the setting it needs and restart the app."
		log.Printf("connection %s: unusable record: %v", info.Host, err)
		return c, nil
	}
	c.inst = inst
	if c.url, err = url.Parse(inst.Origin); err != nil {
		return nil, err
	}
	if info.WipePending {
		c.tampered = true
		c.stopped = "This connection was removed, but its browser data could not be deleted. Remove it again to finish."
		c.proxy = c.newProxy()
		return c, nil
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
	case errors.Is(err, ErrItemUntrusted):
		// Not tampered: the item is only unusable, never used or revoked
		// through. A different build or a moved unsigned app lands here too, so
		// signing in again (which replaces the item) must stay possible; the
		// sign-in confirmation names the origin because no item vouches for it.
		c.stopped = "The saved session was created by a different build of this app, or can be read by other applications, so it was not used. Sign in again to replace it."
		log.Printf("connection %s: keychain item is not restricted to this app", info.Host)
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
// session becomes active only after the keychain item and the connection record
// are both written; if the record write fails the previous keychain state is
// put back, so memory, keychain and disk never disagree.
func (c *Conn) setSession(token, sessionID, principalID, principalName string) error {
	c.mu.RLock()
	tampered := c.tampered
	info := c.info
	prev := c.secret
	c.mu.RUnlock()
	if tampered {
		return errors.New("this connection's saved settings were changed outside the app; remove it and add it again")
	}
	info.PrincipalID, info.PrincipalName = principalID, principalName
	s := Secret{Token: token, SessionID: sessionID, Origin: info.Origin,
		TrustMode: info.Trust.Mode, Pin: info.Trust.Pin, PrincipalID: principalID}
	if err := c.m.secrets.Put(c.host, s); err != nil {
		return c.putFailed(prev, err)
	}
	if err := c.m.store.Put(info); err != nil {
		return c.rollbackKeychain(prev, err)
	}
	c.mu.Lock()
	c.secret = s
	c.stopped = ""
	c.info = info
	c.mu.Unlock()
	c.flights.cancelStreams()
	return nil
}

// putFailed handles a failed keychain replacement. The store deletes the old
// item before it adds the new one, so the old session survives only if it can
// still be read back; otherwise the connection stops and asks for a sign-in.
func (c *Conn) putFailed(prev Secret, err error) error {
	if prev.Token == "" {
		return err
	}
	if got, gerr := c.m.secrets.Get(c.host); gerr == nil && got.Token == prev.Token {
		return err
	}
	c.stop("The saved session was lost while it was being replaced. Sign in again.")
	return err
}

// rollbackKeychain undoes a keychain write whose record write failed: the
// previous item comes back, or the new one is deleted when there was none. A
// failure to do so is reported with the original error, and a connection that
// had a session stops because its keychain state is unknown.
func (c *Conn) rollbackKeychain(prev Secret, cause error) error {
	var err error
	if prev.Token != "" {
		err = c.m.secrets.Put(c.host, prev)
	} else {
		err = c.m.secrets.Delete(c.host)
	}
	if err == nil {
		return cause
	}
	if prev.Token != "" {
		c.stop("The previous session could not be restored after a failed save. Sign in again.")
	}
	return fmt.Errorf("%w; the keychain could not be put back: %w", cause, err)
}

// revoke asks the instance to delete the session behind token. It is best
// effort and bounded.
func (c *Conn) revoke(ctx context.Context, token string) error {
	return revokeOn(ctx, c.inst, token)
}

func revokeOn(ctx context.Context, inst *Instance, token string) error {
	if token == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	status, _, _, err := inst.call(ctx, http.MethodPost, "/api/v1/auth/logout", token, nil, "")
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

// errSuperseded means a sign-in replaced the session that sent the request
// while it was in flight. Its response is obsolete, not an attack.
var errSuperseded = errors.New("the session was replaced while this request was running")

// applyRotation runs the ADR D4 checks on a token returned by a listed route
// and, if they pass, stores it in the keychain, swaps it in, and only then
// lets the caller release the response. On any failure the connection stops
// and the new session is revoked. sent is the bearer the request carried: a
// response for a session that has since been replaced is discarded without
// stopping the current one.
func (c *Conn) applyRotation(kind Rotation, view tokenView, sent string) error {
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
	if stored.Token != sent {
		c.revokeAsync(view.Token)
		return errSuperseded
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

// endSession stops the connection quietly because its session is gone
// (revoked or deleted) while a removal is still unfinished. The record stays so
// the removal can be retried, and the list offers a sign-in.
func (c *Conn) endSession(reason string) {
	c.mu.Lock()
	if c.stopped == "" {
		c.stopped = reason
	}
	c.mu.Unlock()
	c.flights.cancelAll()
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

// markWipePending turns the connection into one that can't be used while its
// browser data is still owed a deletion.
func (c *Conn) markWipePending(info Connection) {
	c.mu.Lock()
	c.info = info
	c.tampered = true
	c.stopped = "This connection was removed, but its browser data could not be deleted. Remove it again to finish."
	c.mu.Unlock()
}

// closeInstance releases the idle connections of a connection that has an
// instance; one built from an unusable record has none.
func (c *Conn) closeInstance() {
	if c.inst != nil {
		c.inst.close()
	}
}
