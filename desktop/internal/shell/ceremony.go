package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/clientauth"
	"github.com/Hikyo-Org/hikyo/internal/clienterr"
)

// openHandoff opens only the shell's own sign-in and ceremony pages, on the
// connection's origin. clientauth builds those URLs; this is the second check.
func (m *Manager) openHandoff(origin, target string) error {
	u, err := url.Parse(target)
	if err != nil || !sameOrigin(u, origin) || u.User != nil || (u.Path != "/reauth/cli" && u.Path != "/login/cli") {
		return errors.New("refusing to open an unexpected handoff URL")
	}
	return m.opts.OpenURL(target)
}

// sameOrigin reports whether u is on the canonical origin, under any spelling.
func sameOrigin(u *url.URL, origin string) bool {
	got, err := originOf(u)
	return err == nil && got == origin
}

func (c *Conn) transport() authTransport { return authTransport{in: c.inst, bearer: c.bearer} }

// runReauth runs the browser ceremony for a page's request (ADR D7, D9) and
// stores the rotated session. The page gets no token and no window result.
func (c *Conn) runReauth(ctx context.Context, in reauthIn) *shellErr {
	rotated, err := clientauth.Reauth(ctx, c.transport(), clientauth.ReauthRequest{
		Purpose: in.Purpose, Operation: in.Operation,
		EnvironmentIDs: in.EnvironmentIDs, KeyIDs: in.KeyIDs, SelfConfig: in.SelfConfig,
	}, func(target string) error { return c.m.openHandoff(c.inst.Origin, target) })
	if err != nil {
		return failureOf(err)
	}
	// A redeemed handoff rotates the session and keeps its id (ADR D4).
	if err := c.applyRotation(Rotate, tokenView{Token: rotated.Token, SessionID: rotated.SessionID}); err != nil {
		return &shellErr{codeFailed, "the approved session could not be kept; sign in again"}
	}
	return nil
}

// sessionAlive asks the instance whether the stored session still works.
func (c *Conn) sessionAlive(ctx context.Context) (bool, error) {
	token := c.bearer()
	if token == "" {
		return false, nil
	}
	status, _, _, err := c.inst.call(ctx, http.MethodGet, "/api/v1/auth/whoami", token, nil, "")
	switch {
	case err != nil:
		return false, err
	case status/100 == 2:
		return true, nil
	case status == http.StatusUnauthorized:
		return false, nil
	default:
		return false, fmt.Errorf("the instance answered %d", status)
	}
}

// runSignIn confirms the session is dead, then signs in again through the
// system browser (ADR D6, D9).
func (c *Conn) runSignIn(ctx context.Context) *shellErr {
	if c.isTampered() {
		return &shellErr{codeRefused, "this connection's saved settings were changed outside the app; remove it and add it again"}
	}
	alive, err := c.sessionAlive(ctx)
	if err != nil {
		return &shellErr{codeFailed, "the instance could not be reached to check the session"}
	}
	if alive {
		return nil
	}
	result, err := c.m.login(ctx, c.inst, "")
	if err != nil {
		return failureOf(err)
	}
	if err := c.m.acceptLogin(ctx, c, result); err != nil {
		return failureOf(err)
	}
	return nil
}

// login runs the sign-in handoff: loopback with PKCE, then the device flow
// when the loopback can't run. method "device" skips the loopback.
func (m *Manager) login(ctx context.Context, in *Instance, method string) (apigen.LoginResult, error) {
	tr := authTransport{in: in}
	if method != "device" {
		result, err := clientauth.LoginLoopback(ctx, tr, "", func(target string) error { return m.openHandoff(in.Origin, target) })
		var coded *clienterr.Error
		if err == nil || ctx.Err() != nil || !errors.As(err, &coded) || coded.Code != clienterr.ExitUnavailable {
			return result, err
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var dismiss func()
	result, err := clientauth.LoginDeviceNotify(ctx, tr, io.Discard, "", func(p clientauth.DevicePrompt) {
		open := func() {
			if err := m.openHandoff(in.Origin, p.VerificationURL); err != nil {
				log.Printf("sign-in: opening the verification page failed: %v", err)
			}
		}
		dismiss = m.native.ShowDeviceCode(p.VerificationURL, p.UserCode, cancel, open)
	})
	if dismiss != nil {
		dismiss()
	}
	return result, err
}

// acceptLogin validates a sign-in result and makes it the connection's
// session. A different principal than the connection had needs a native
// confirmation first (ADR D3). c may be a connection that has no session yet.
func (m *Manager) acceptLogin(ctx context.Context, c *Conn, result apigen.LoginResult) error {
	if err := clientauth.ValidateSession(result); err != nil {
		return err
	}
	if result.SessionToken == nil || !parsesAsCLIToken(*result.SessionToken) {
		return clienterr.Failf(clienterr.ExitInternal, "the instance returned no usable session token")
	}
	token := *result.SessionToken
	principalID := string(result.Principal.Id)
	name := principalID
	if result.Principal.DisplayName != nil && *result.Principal.DisplayName != "" {
		name = *result.Principal.DisplayName
	}
	if before := c.Info(); before.PrincipalID != "" && before.PrincipalID != principalID {
		if !m.native.Confirm("Sign in as a different account?",
			fmt.Sprintf("%s was signed in as %s. This sign-in is for %s. Use the new account on this connection?", before.Name, labelOf(before), name),
			"Use new account") {
			c.revokeAsync(token)
			return context.Canceled
		}
	}
	c.rotMu.Lock()
	defer c.rotMu.Unlock()
	if c.removed {
		c.revokeAsync(token)
		return errRemoved
	}
	if err := c.setSession(token, string(result.Session.Id), principalID, name); err != nil {
		c.revokeAsync(token)
		return clienterr.Failf(clienterr.ExitInternal, "the session could not be saved to the keychain: %v", err)
	}
	return nil
}

func labelOf(c Connection) string {
	if c.PrincipalName != "" {
		return c.PrincipalName
	}
	return c.PrincipalID
}
