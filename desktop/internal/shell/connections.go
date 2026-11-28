package shell

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/clienterr"
	"github.com/Hikyo-Org/hikyo/internal/clienttrust"
)

// Errors the connection list reports beyond the shell route codes.
const (
	codeCapabilityMissing = "capability_missing"
	codeTrustDeclined     = "cancelled"
)

// AddRequest is a new connection as the user entered it.
type AddRequest struct {
	Origin string
	Name   string
	// Mode is clienttrust.ModeSystemCA or clienttrust.ModeSPKI.
	Mode clienttrust.Mode
	// Method is "browser" (default) or "device".
	Method string
}

// trustPrompt is the native first-contact confirmation (ADR D5): origin,
// trust mode and the SPKI fingerprint, the rotation tradeoff, and the ask to
// verify the fingerprint independently.
func trustPrompt(origin string, mode clienttrust.Mode, fingerprint string) string {
	text := strings.TrimSuffix(clienttrust.FirstContactPrompt(origin, mode, fingerprint, ""), "\n\nRecord it")
	text += "\n    fingerprint:  " + fingerprint
	if mode == clienttrust.ModeSPKI {
		text += "\n\nThe app will accept only this key. If the server's key is ever replaced, it refuses to connect until you remove and add the connection again."
	} else {
		text += "\n\nThe app will accept any certificate your Mac trusts for this host, so a key change is not noticed."
	}
	return text + "\n\nCheck the fingerprint with the instance's operator through another channel before you continue."
}

// capabilityError is the refusal for an instance without desktop-app/1.
type capabilityError struct{ serverVersion string }

func (e capabilityError) Error() string {
	return fmt.Sprintf("This instance (server version %s) doesn't support the Hikyo desktop app. It needs Hikyo %s or newer, which advertises %s. You can open it in your browser instead.",
		e.serverVersion, RequiredServerVersion, capabilityDesktop)
}

// checkCapability reads /api/v1/meta and refuses an instance that does not
// advertise desktop-app/1 (ADR D8). There is no degraded mode: an instance that
// answers the meta request with a client error (one that predates the endpoint
// answers 404) is refused like one that omits the capability. A server error,
// rate limit or timeout says nothing about the capability, so it is returned as
// a failure and the window opens to show the instance's own state.
func checkCapability(ctx context.Context, in *Instance) error {
	status, _, data, err := in.call(ctx, http.MethodGet, api.PathPrefix+"/meta", "", nil, "")
	if err != nil {
		return clienterr.Failf(clienterr.ExitUnavailable, "cannot reach %s: %v", in.Origin, err)
	}
	if status/100 == 4 && status != http.StatusRequestTimeout && status != http.StatusTooManyRequests {
		return capabilityError{serverVersion: "unknown"}
	}
	if status/100 != 2 {
		return authTransport{in: in}.ResponseError(status, nil, nil)
	}
	var meta apigen.Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		// A 200 that is not the meta document (a login page, a proxy's error
		// page) does not advertise the capability.
		return capabilityError{serverVersion: "unknown"}
	}
	if !slices.Contains(meta.ProtocolCapabilities, capabilityDesktop) {
		return capabilityError{serverVersion: meta.ServerVersion}
	}
	return nil
}

// Add trusts, checks and signs in a new instance, then stores the connection.
// Order matters (ADR D5, D8): nothing but the TLS identity probe is sent
// before the user confirms the fingerprint, and the capability check comes
// before sign-in.
func (m *Manager) Add(ctx context.Context, req AddRequest) (string, error) {
	origin, err := canonicalOrigin(req.Origin)
	if err != nil {
		return "", &shellErr{codeInvalid, "that is not a valid instance origin"}
	}
	if req.Mode != clienttrust.ModeSystemCA && req.Mode != clienttrust.ModeSPKI {
		return "", &shellErr{codeInvalid, "choose a trust mode first"}
	}
	for _, c := range m.store.List() {
		if existing, err := canonicalOrigin(c.Origin); c.Origin == origin || (err == nil && existing == origin) {
			return "", &shellErr{codeInvalid, "this instance is already connected"}
		}
	}
	trust := Trust{Mode: req.Mode}
	loopback := !strings.HasPrefix(origin, "https://")
	if loopback && (!m.opts.AllowLoopbackHTTP || !clienttrust.IsLoopbackOrigin(origin)) {
		return "", &shellErr{codeInvalid, "instances must use https"}
	}
	if !loopback {
		fingerprint, err := clienttrust.InspectIdentity(ctx, origin, 15*time.Second, req.Mode)
		if err != nil {
			return "", &shellErr{codeFailed, "the instance could not be reached or its certificate was refused"}
		}
		if !m.native.Confirm("Trust this instance?", trustPrompt(origin, req.Mode, fingerprint), "Trust and continue") {
			return "", &shellErr{codeTrustDeclined, "the connection was not trusted"}
		}
		if req.Mode == clienttrust.ModeSPKI {
			trust.Pin = fingerprint
		}
	} else if !m.native.Confirm("Connect without TLS?", "This is a development connection to "+origin+" over plain HTTP on this Mac.", "Connect") {
		return "", &shellErr{codeTrustDeclined, "the connection was not trusted"}
	}
	inst, err := newInstance(origin, trust, m.userAgent(), m.opts.AllowLoopbackHTTP)
	if err != nil {
		return "", &shellErr{codeInvalid, err.Error()}
	}
	defer inst.close()
	if err := checkCapability(ctx, inst); err != nil {
		var missing capabilityError
		if errors.As(err, &missing) {
			return "", &shellErr{codeCapabilityMissing, missing.Error()}
		}
		return "", failureOf(err)
	}
	result, err := m.login(ctx, inst, req.Method)
	if err != nil {
		return "", failureOf(err)
	}
	host, err := m.store.NewHost()
	if err != nil {
		return "", &shellErr{codeFailed, "could not allocate a connection"}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	}
	info := Connection{Host: host, Origin: origin, Name: name, Trust: trust, Created: m.now().UTC()}
	conn, err := m.newConn(info)
	if err != nil {
		return "", &shellErr{codeFailed, err.Error()}
	}
	if err := m.acceptLogin(ctx, conn, result); err != nil {
		return "", failureOf(err)
	}
	m.mu.Lock()
	m.conns[host] = conn
	m.mu.Unlock()
	return host, nil
}

// RemoveResult reports what removal did. Revoked is false when the instance
// could not be told to end the session, which then stays live until it expires.
// StorageKept is true when the connection's browser data could not be deleted:
// the connection stays in the list, unusable, so the removal can be retried.
// Message is shown whatever the other fields say.
type RemoveResult struct {
	Revoked     bool   `json:"revoked"`
	StorageKept bool   `json:"storage_kept,omitzero"`
	Message     string `json:"message,omitzero"`
}

// Remove revokes the connection's session on the instance first, then deletes
// its window, browser storage, keychain item and record (ADR D2). The retry
// marker is stored before anything is destroyed (beginRemoval); from then on the
// connection is unusable and never restored, so a step that fails is reported
// and Remove can be run again from the list. A failed revocation is reported
// plainly and does not keep the data.
func (m *Manager) Remove(ctx context.Context, host string) (RemoveResult, error) {
	c := m.conn(host)
	if c == nil {
		return RemoveResult{}, &shellErr{codeInvalid, "unknown connection"}
	}
	c.removeMu.Lock()
	defer c.removeMu.Unlock()
	token, retry, err := c.beginRemoval()
	if errors.Is(err, errRemoved) {
		return RemoveResult{}, &shellErr{codeInvalid, "unknown connection"}
	}
	if err != nil {
		return RemoveResult{}, &shellErr{codeFailed, "the removal could not be recorded, so nothing was changed: " + err.Error()}
	}
	const notRevoked = "The session could not be revoked on the instance, so it stays valid until it expires. Revoke it from another signed-in session."
	var revokeErr error
	if token != "" {
		revokeErr = c.revoke(ctx, token)
	} else {
		revokeErr = m.revokeStored(ctx, host)
	}
	if revokeErr == nil {
		c.revoked = true
	}
	result := RemoveResult{Revoked: revokeErr == nil}
	switch {
	case errors.Is(revokeErr, ErrNoSecret) && c.revoked:
		// An earlier attempt of this run revoked it and deleted the item.
		result.Revoked = true
	case errors.Is(revokeErr, ErrNoSecret) && retry:
		result.Message = "An earlier removal already deleted the stored session; whether it was revoked on the instance can't be confirmed here."
	case errors.Is(revokeErr, ErrNoSecret):
		result.Message = "There was no stored session to revoke."
	case revokeErr != nil:
		result.Message = notRevoked
	}
	// The renderer must be gone before its data is wiped, or it could write the
	// data back.
	if err := m.native.CloseWindow(host); err != nil {
		return result, &shellErr{codeFailed, "the window did not close, so the browser data was not deleted: " + err.Error()}
	}
	wipeErr := m.native.WipeStorage(host)
	if err := m.secrets.Delete(host); err != nil {
		return result, &shellErr{codeFailed, "the keychain item could not be deleted: " + err.Error()}
	}
	if wipeErr != nil {
		result.StorageKept = true
		result.Message = strings.TrimSpace(result.Message + " The connection's browser data could not be deleted (" + wipeErr.Error() + "). It stays in the list; remove it again to retry.")
		return result, nil
	}
	if err := m.store.Remove(host); err != nil {
		return result, &shellErr{codeFailed, "the connection record could not be deleted: " + err.Error()}
	}
	m.mu.Lock()
	delete(m.conns, host)
	m.mu.Unlock()
	c.finishRemoval()
	return result, nil
}

// revokeStored revokes the keychain item's session through an instance built
// from the item's own origin, trust mode and pin. connections.json is editable
// by any process of the same user, so no instance built from it may carry a
// bearer. The store refuses items that another application could read. The
// bearer goes only to the origin the item names, whatever the record says, so a
// record that startup could not compare with its item (the keychain was
// unreadable then) can't redirect it.
func (m *Manager) revokeStored(ctx context.Context, host string) error {
	secret, err := m.secrets.Get(host)
	if err != nil {
		return err
	}
	inst, err := newInstance(secret.Origin, Trust{Mode: secret.TrustMode, Pin: secret.Pin}, m.userAgent(), m.opts.AllowLoopbackHTTP)
	if err != nil {
		return err
	}
	defer inst.close()
	return revokeOn(ctx, inst, secret.Token)
}

// SignIn signs an existing connection in again from the list.
func (m *Manager) SignIn(ctx context.Context, host, method string) error {
	c := m.conn(host)
	if c == nil {
		return &shellErr{codeInvalid, "unknown connection"}
	}
	if c.isTampered() {
		return &shellErr{codeRefused, c.Stopped()}
	}
	if !c.confirmSignIn() {
		return &shellErr{codeTrustDeclined, "the sign-in was not confirmed"}
	}
	// Removing the connection ends its sign-in.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(c.life(), cancel)()
	result, err := m.login(ctx, c.inst, method)
	if err != nil {
		return failureOf(err)
	}
	if err := m.acceptLogin(ctx, c, result); err != nil {
		return failureOf(err)
	}
	return nil
}
