package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

const cliLoginCallbackPath = "/callback"

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
	// Session timestamps share the server's clock. Validate their ordering and
	// retain the server expiry verbatim; the server enforces credential expiry.
	if result.Session.Id == "" || result.Principal.Id == "" || result.Principal.Kind != "human" || result.Session.Artifact != "cli" || result.Session.CreatedAt.IsZero() || !result.Session.AbsoluteExpiresAt.After(result.Session.CreatedAt) || !result.Session.IdleExpiresAt.After(result.Session.CreatedAt) {
		return failf(ExitInternal, "server returned an invalid CLI session")
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
	if hint == "" {
		return "", nil
	}
	var methods apigen.AuthMethods
	if err := client.Do(ctx, http.MethodGet, api.PathPrefix+"/auth/methods", nil, &methods); err != nil {
		return "", err
	}
	var matches []string
	for _, provider := range methods.Providers {
		qualified := string(provider.Kind) + ":" + provider.Slug
		if hint == qualified || (!strings.Contains(hint, ":") && hint == provider.Slug) {
			matches = append(matches, qualified)
		}
	}
	if len(matches) != 1 {
		return "", failf(ExitRefused, "--provider is unknown or ambiguous among enabled providers; use kind:slug from the instance's sign-in page")
	}
	return matches[0], nil
}

func loginProof() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func loginWithLoopback(ctx context.Context, client *Client, provider string, openURL func(string) error) (apigen.LoginResult, error) {
	listener, err := listenLoginLoopback(ctx)
	if err != nil {
		return apigen.LoginResult{}, failf(ExitUnavailable, "binding the CLI login callback: %v", err)
	}
	defer listener.Close()
	state, err := loginProof()
	if err != nil {
		return apigen.LoginResult{}, err
	}
	verifier, err := loginProof()
	if err != nil {
		return apigen.LoginResult{}, err
	}
	digest := sha256.Sum256([]byte(verifier))
	redirectURI := "http://" + listener.Addr().String() + cliLoginCallbackPath
	var started apigen.CLILoginStart
	if err := client.Do(ctx, http.MethodPost, api.PathPrefix+"/auth/cli-login/start", apigen.CLILoginStartRequest{
		Transport: "loopback", State: state, PkceChallenge: base64.RawURLEncoding.EncodeToString(digest[:]), RedirectUri: redirectURI,
	}, &started); err != nil {
		return apigen.LoginResult{}, err
	}
	if started.State != state || started.ExpiresAt.IsZero() {
		return apigen.LoginResult{}, failf(ExitInternal, "server returned an invalid CLI login transaction")
	}
	// The server owns absolute expiry. Bound the local wait with a monotonic
	// timer using the loopback protocol's ten-minute lifetime, even with skew.
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	target, err := url.Parse(client.Entry.Origin)
	if err != nil {
		return apigen.LoginResult{}, err
	}
	target.Path, target.RawPath, target.Fragment = "/login/cli", "", ""
	query := url.Values{"state": {state}}
	if provider != "" {
		query.Set("provider", provider)
	}
	target.RawQuery = query.Encode()
	code := make(chan string, 1)
	var once sync.Once
	callback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		values, queryErr := url.ParseQuery(r.URL.RawQuery)
		if r.Method != http.MethodGet || r.Host != listener.Addr().String() || r.URL.Path != cliLoginCallbackPath || r.URL.RawPath != "" || queryErr != nil || !exactCallbackQuery(values, state) {
			http.Error(w, "This CLI login callback did not match the request.", http.StatusBadRequest)
			return
		}
		accepted := false
		once.Do(func() {
			accepted = true
			// Close the listening socket before publishing a redeemable code.
			// Existing accepted connections are closed by server.Close below.
			_ = listener.Close()
			code <- values.Get("code")
		})
		if !accepted {
			http.Error(w, "This CLI login callback was already used.", http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "CLI login received. Return to the terminal.\n")
	})
	server := &http.Server{Handler: callback, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	if openURL == nil {
		openURL = OpenBrowser
	}
	if err := openURL(target.String()); err != nil {
		return apigen.LoginResult{}, failf(ExitUnavailable, "opening the browser for CLI login failed; use --device on a headless terminal")
	}
	var authorizationCode string
	for authorizationCode == "" {
		select {
		case authorizationCode = <-code:
		case err := <-serveErr:
			// Closing the listener at acceptance makes Serve return first on
			// some schedulers; the accepted callback still owns the code channel.
			if !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
				return apigen.LoginResult{}, failf(ExitUnavailable, "serving the CLI login callback failed")
			}
			serveErr = nil
		case <-waitCtx.Done():
			return apigen.LoginResult{}, failf(ExitAuth, "CLI login expired or was cancelled; start login again")
		}
	}
	if waitCtx.Err() != nil {
		return apigen.LoginResult{}, failf(ExitAuth, "CLI login expired or was cancelled; start login again")
	}
	// Wait for the accepted handler to finish its plain-text response while the
	// socket is already closed, then tear down its connections before redemption.
	_ = server.Shutdown(waitCtx)
	_ = server.Close()
	var result apigen.LoginResult
	err = client.Do(waitCtx, http.MethodPost, api.PathPrefix+"/auth/cli-login/redeem", apigen.CLILoginRedeemRequest{Code: authorizationCode, PkceVerifier: verifier, RedirectUri: redirectURI}, &result)
	return result, err
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

func deviceSeconds(seconds int) (time.Duration, bool) {
	if seconds <= 0 || int64(seconds) > math.MaxInt64/int64(time.Second) {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

func validDeviceUserCode(code string) bool {
	if len(code) != 8 {
		return false
	}
	for _, character := range code {
		if !strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", character) {
			return false
		}
	}
	return true
}

func deviceTransportError(ctx context.Context, err error) error {
	var cliError *Error
	if errors.As(err, &cliError) {
		return cliError
	}
	if ctx.Err() != nil {
		return failf(ExitAuth, "device login expired or was cancelled; start login again")
	}
	return failf(ExitUnavailable, "device login transport failed")
}

func loginWithDevice(ctx context.Context, ios IO, client *Client, provider string) (apigen.LoginResult, error) {
	status, header, raw, err := requestCLIDevice(ctx, client, api.PathPrefix+"/auth/cli-login/device/start", url.Values{"client_id": {"hikyo-cli"}})
	if err != nil {
		return apigen.LoginResult{}, deviceTransportError(ctx, err)
	}
	if status != http.StatusOK {
		return apigen.LoginResult{}, errorFromResponse(status, header, raw)
	}
	var started apigen.CLIDeviceStart
	if err := json.Unmarshal(raw, &started); err != nil {
		return apigen.LoginResult{}, failf(ExitInternal, "server returned an invalid device authorization response")
	}
	interval, validInterval := deviceSeconds(started.Interval)
	lifetime, validLifetime := deviceSeconds(started.ExpiresIn)
	verification, parseErr := url.Parse(started.VerificationUri)
	if parseErr != nil {
		return apigen.LoginResult{}, failf(ExitInternal, "server returned an invalid device authorization response")
	}
	verificationQuery, queryErr := url.ParseQuery(verification.RawQuery)
	if verification.Scheme+"://"+verification.Host != client.Entry.Origin || verification.User != nil || verification.Path != "/login/cli" || verification.RawPath != "" || verification.Fragment != "" || queryErr != nil || len(verificationQuery) != 1 || len(verificationQuery["device"]) != 1 || verificationQuery.Get("device") != "1" || started.DeviceCode == "" || len(started.DeviceCode) > 512 || !validInterval || interval < 5*time.Second || !validLifetime || lifetime > 15*time.Minute || !validDeviceUserCode(started.UserCode) || started.ExpiresAt.IsZero() {
		return apigen.LoginResult{}, failf(ExitInternal, "server returned an invalid device authorization response")
	}
	// RFC 8628 expires_in supplies the local monotonic budget. The server's
	// expires_at uses a different clock and cannot shorten or extend it.
	pollCtx, cancel := context.WithTimeout(ctx, lifetime)
	defer cancel()
	if provider != "" {
		query := verification.Query()
		query.Set("provider", provider)
		verification.RawQuery = query.Encode()
	}
	fmt.Fprintf(ios.Stderr, "On another device, visit %s\nEnter code: %s\nApprove a CLI login for %s.\n", verification.String(), started.UserCode, client.Entry.Origin)
	return pollCLIDevice(pollCtx, ios.Stderr, client, started.DeviceCode, interval)
}

func pollCLIDevice(ctx context.Context, stderr io.Writer, client *Client, deviceCode string, interval time.Duration) (apigen.LoginResult, error) {
	wait := interval
	for {
		if err := client.wait(ctx, wait); err != nil {
			return apigen.LoginResult{}, failf(ExitAuth, "device login expired or was cancelled; start login again")
		}
		if ctx.Err() != nil {
			return apigen.LoginResult{}, failf(ExitAuth, "device login expired or was cancelled; start login again")
		}
		status, header, raw, err := requestCLIDevice(ctx, client, api.PathPrefix+"/auth/cli-login/device/token", url.Values{
			"client_id": {"hikyo-cli"}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {deviceCode},
		})
		if err != nil {
			var networkError net.Error
			if ctx.Err() == nil && errors.As(err, &networkError) && networkError.Timeout() {
				if interval > time.Duration(math.MaxInt64)/2 {
					return apigen.LoginResult{}, failf(ExitAuth, "device login polling interval exceeds the remaining login lifetime")
				}
				interval *= 2
				wait = interval
				continue
			}
			return apigen.LoginResult{}, deviceTransportError(ctx, err)
		}
		wait = interval
		if status == http.StatusTooManyRequests {
			retrySeconds, parseErr := strconv.ParseUint(strings.TrimSpace(header.Get("Retry-After")), 10, 64)
			if parseErr != nil || retrySeconds == 0 || retrySeconds > uint64(math.MaxInt64/int64(time.Second)) {
				return apigen.LoginResult{}, failf(ExitRateLimited, "device login throttled without a usable Retry-After")
			}
			var limited *RateLimitedError
			throttle := errorFromResponse(status, header, raw)
			if !errors.As(throttle, &limited) || limited.RetryAfter <= 0 {
				return apigen.LoginResult{}, throttle
			}
			wait = max(interval, limited.RetryAfter)
			fmt.Fprintf(stderr, "hikyo: throttled by the server; retrying in %d s (Retry-After)\n", int(wait/time.Second))
			continue
		}
		if status == http.StatusOK {
			var token apigen.CLIDeviceToken
			if err := json.Unmarshal(raw, &token); err != nil || token.AccessToken == "" || token.TokenType != "Bearer" || token.ExpiresIn <= 0 {
				return apigen.LoginResult{}, failf(ExitInternal, "server returned an invalid device token response")
			}
			return apigen.LoginResult{SessionToken: &token.AccessToken, Session: token.Session, Principal: token.Principal}, nil
		}
		if status != http.StatusBadRequest {
			return apigen.LoginResult{}, errorFromResponse(status, header, raw)
		}
		var protocol apigen.CLIDeviceError
		if err := json.Unmarshal(raw, &protocol); err != nil {
			return apigen.LoginResult{}, failf(ExitInternal, "server returned an invalid device polling response")
		}
		switch protocol.Error {
		case "slow_down":
			if interval > time.Duration(math.MaxInt64)-5*time.Second {
				return apigen.LoginResult{}, failf(ExitInternal, "device polling interval overflow")
			}
			interval += 5 * time.Second
			fallthrough
		case "authorization_pending":
			if serverInterval, valid := deviceSeconds(protocol.Interval); valid {
				interval = max(interval, serverInterval)
			}
			wait = interval
		case "expired_token", "access_denied":
			return apigen.LoginResult{}, failf(ExitAuth, "device login expired or was denied; start login again")
		default:
			return apigen.LoginResult{}, failf(ExitRefused, "device login was refused; polling stopped")
		}
	}
}
