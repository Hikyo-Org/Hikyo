package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/clientauth"
)

func runInteractiveLogin(ctx context.Context, ios IO, state *State, client *Client, meta apigen.Meta, device bool, hint string) error {
	capability := "loopback-handoff"
	if device {
		capability = "device-code"
	}
	if !slices.Contains(meta.ProtocolCapabilities, capability) {
		return failf(ExitRefused, "%s does not serve %s login (it advertises: %s)", client.Entry.Origin, capability, strings.Join(meta.ProtocolCapabilities, ", "))
	}
	provider, err := resolveLoginProvider(ctx, client, hint)
	if err != nil {
		return err
	}
	var result apigen.LoginResult
	if device {
		result, err = loginWithDevice(ctx, ios, client, provider)
	} else {
		result, err = loginWithLoopback(ctx, client, provider, ios.OpenURL)
	}
	if err != nil {
		return err
	}
	token, err := cliSessionToken(result.SessionToken)
	if err != nil {
		return err
	}
	if err := clientauth.ValidateSession(result); err != nil {
		return err
	}
	if err := state.PutSession(SessionArtifact{
		Instance: client.Entry.Name, Origin: client.Entry.Origin, Token: token,
		SessionID: result.Session.Id, Principal: result.Principal.Id,
		ExpiresAt: result.Session.AbsoluteExpiresAt.Format(time.RFC3339),
	}); err != nil {
		return err
	}
	displayName := result.Principal.Id
	if result.Principal.DisplayName != nil {
		displayName = *result.Principal.DisplayName
	}
	fmt.Fprintf(ios.Stderr, "logged in to %s as %s (session %s, idle expiry %s)\n", client.Entry.Origin, displayName, result.Session.Id, result.Session.IdleExpiresAt.Format("2006-01-02 15:04 MST"))
	return nil
}

// The terminal selects only a page hint. Provider transactions stay in the
// browser with their ordinary cookie binding.
func resolveLoginProvider(ctx context.Context, client *Client, hint string) (string, error) {
	return clientauth.ResolveProvider(ctx, authTransport{client}, hint)
}

func loginWithLoopback(ctx context.Context, client *Client, provider string, openURL func(string) error) (apigen.LoginResult, error) {
	return clientauth.LoginLoopback(ctx, authTransport{client}, provider, openURL)
}

// Device requests use the RFC form encoding through the same pinned,
// origin-bound HTTP client as all other CLI requests.
func requestCLIDevice(ctx context.Context, client *Client, path string, form url.Values) (int, http.Header, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.Entry.Origin+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, nil, err
	}
	if err := client.requireRevision(ctx, req); err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", client.UserAgent)
	response, err := client.HTTP.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer response.Body.Close()
	const maxDeviceResponse = 64 << 10
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxDeviceResponse+1))
	if err == nil && len(raw) > maxDeviceResponse {
		err = failf(ExitInternal, "device response exceeds the protocol size limit")
	}
	return response.StatusCode, response.Header, raw, err
}

func loginWithDevice(ctx context.Context, ios IO, client *Client, provider string) (apigen.LoginResult, error) {
	return clientauth.LoginDevice(ctx, authTransport{client}, ios.Stderr, provider)
}

func pollCLIDevice(ctx context.Context, stderr io.Writer, client *Client, deviceCode string, interval time.Duration) (apigen.LoginResult, error) {
	return clientauth.PollDevice(ctx, authTransport{client}, stderr, deviceCode, interval)
}

// authTransport lends the origin-bound Client to internal/clientauth.
type authTransport struct{ client *Client }

func (t authTransport) Origin() string { return t.client.Entry.Origin }

func (t authTransport) Do(ctx context.Context, method, path string, body, out any) error {
	return t.client.Do(ctx, method, path, body, out)
}

func (t authTransport) PostForm(ctx context.Context, path string, form url.Values) (int, http.Header, []byte, error) {
	return requestCLIDevice(ctx, t.client, path, form)
}

func (t authTransport) ResponseError(status int, header http.Header, body []byte) error {
	return errorFromResponse(status, header, body)
}

func (t authTransport) Wait(ctx context.Context, d time.Duration) error { return t.client.wait(ctx, d) }
