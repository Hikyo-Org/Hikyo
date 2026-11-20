package clientauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/clienterr"
)

const loginCallbackPath = "/callback"

// ValidateSession refuses a login result that is not a well-formed human CLI
// session: ids present, kind human, artifact cli, timestamps ordered. Session
// timestamps share the server's clock; the server enforces credential expiry.
func ValidateSession(result apigen.LoginResult) error {
	if result.Session.Id == "" || result.Principal.Id == "" || result.Principal.Kind != "human" || result.Session.Artifact != "cli" || result.Session.CreatedAt.IsZero() || !result.Session.AbsoluteExpiresAt.After(result.Session.CreatedAt) || !result.Session.IdleExpiresAt.After(result.Session.CreatedAt) {
		return clienterr.Failf(clienterr.ExitInternal, "server returned an invalid CLI session")
	}
	return nil
}

// ResolveProvider maps a --provider hint to the one enabled kind:slug it names,
// or "" for no hint.
func ResolveProvider(ctx context.Context, tr Transport, hint string) (string, error) {
	if hint == "" {
		return "", nil
	}
	var methods apigen.AuthMethods
	if err := tr.Do(ctx, http.MethodGet, api.PathPrefix+"/auth/methods", nil, &methods); err != nil {
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
		return "", clienterr.Failf(clienterr.ExitRefused, "--provider is unknown or ambiguous among enabled providers; use kind:slug from the instance's sign-in page")
	}
	return matches[0], nil
}

// LoginLoopback runs the system-browser sign-in with PKCE: it binds an
// ephemeral loopback callback, opens the instance's /login/cli page through
// openURL (OpenBrowser when nil), and redeems the returned code.
func LoginLoopback(ctx context.Context, tr Transport, provider string, openURL func(string) error) (apigen.LoginResult, error) {
	listener, err := ListenLoopback(ctx)
	if err != nil {
		return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitUnavailable, "binding the CLI login callback: %v", err)
	}
	defer func() { _ = listener.Close() }()
	state, err := proof()
	if err != nil {
		return apigen.LoginResult{}, err
	}
	verifier, err := proof()
	if err != nil {
		return apigen.LoginResult{}, err
	}
	digest := sha256.Sum256([]byte(verifier))
	redirectURI := "http://" + listener.Addr().String() + loginCallbackPath
	var started apigen.CLILoginStart
	if err := tr.Do(ctx, http.MethodPost, api.PathPrefix+"/auth/cli-login/start", apigen.CLILoginStartRequest{
		Transport: "loopback", State: state, PkceChallenge: base64.RawURLEncoding.EncodeToString(digest[:]), RedirectUri: redirectURI,
	}, &started); err != nil {
		return apigen.LoginResult{}, err
	}
	if started.State != state || started.ExpiresAt.IsZero() {
		return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitInternal, "server returned an invalid CLI login transaction")
	}
	// The server owns absolute expiry. Bound the local wait with a monotonic
	// timer using the loopback protocol's ten-minute lifetime, even with skew.
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	target, err := url.Parse(tr.Origin())
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
		if r.Method != http.MethodGet || r.Host != listener.Addr().String() || r.URL.Path != loginCallbackPath || r.URL.RawPath != "" || queryErr != nil || !exactCallbackQuery(values, state) {
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
		_, _ = io.WriteString(w, "CLI login received. Return to the Hikyo app or terminal.\n")
	})
	server := &http.Server{Handler: callback, ReadHeaderTimeout: 5 * time.Second}
	defer func() { _ = server.Close() }()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	if openURL == nil {
		openURL = OpenBrowser
	}
	if err := openURL(target.String()); err != nil {
		return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitUnavailable, "opening the browser for CLI login failed; use --device on a headless terminal")
	}
	var authorizationCode string
	for authorizationCode == "" {
		select {
		case authorizationCode = <-code:
		case err := <-serveErr:
			// Closing the listener at acceptance makes Serve return first on
			// some schedulers; the accepted callback still owns the code channel.
			if !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
				return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitUnavailable, "serving the CLI login callback failed")
			}
			serveErr = nil
		case <-waitCtx.Done():
			return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitAuth, "CLI login expired or was cancelled; start login again")
		}
	}
	if waitCtx.Err() != nil {
		return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitAuth, "CLI login expired or was cancelled; start login again")
	}
	// Wait for the accepted handler to finish its plain-text response while the
	// socket is already closed, then tear down its connections before redemption.
	_ = server.Shutdown(waitCtx)
	_ = server.Close()
	var result apigen.LoginResult
	err = tr.Do(waitCtx, http.MethodPost, api.PathPrefix+"/auth/cli-login/redeem", apigen.CLILoginRedeemRequest{Code: authorizationCode, PkceVerifier: verifier, RedirectUri: redirectURI}, &result)
	return result, err
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
	var cliError *clienterr.Error
	if errors.As(err, &cliError) {
		return cliError
	}
	if ctx.Err() != nil {
		return clienterr.Failf(clienterr.ExitAuth, "device login expired or was cancelled; start login again")
	}
	return clienterr.Failf(clienterr.ExitUnavailable, "device login transport failed")
}

// DevicePrompt is what a human needs to approve a device login elsewhere.
type DevicePrompt struct {
	VerificationURL string
	UserCode        string
	ExpiresIn       time.Duration
}

// LoginDevice runs the RFC 8628 device flow. The verification URL and user
// code are written to prompt for the human to act on another device.
func LoginDevice(ctx context.Context, tr Transport, prompt io.Writer, provider string) (apigen.LoginResult, error) {
	return LoginDeviceNotify(ctx, tr, prompt, provider, nil)
}

// LoginDeviceNotify is LoginDevice plus a structured callback, called once
// before polling, so a caller need not parse the prompt text. notify may be nil.
func LoginDeviceNotify(ctx context.Context, tr Transport, prompt io.Writer, provider string, notify func(DevicePrompt)) (apigen.LoginResult, error) {
	status, header, raw, err := tr.PostForm(ctx, api.PathPrefix+"/auth/cli-login/device/start", url.Values{"client_id": {"hikyo-cli"}})
	if err != nil {
		return apigen.LoginResult{}, deviceTransportError(ctx, err)
	}
	if status != http.StatusOK {
		return apigen.LoginResult{}, tr.ResponseError(status, header, raw)
	}
	var started apigen.CLIDeviceStart
	if err := json.Unmarshal(raw, &started); err != nil {
		return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitInternal, "server returned an invalid device authorization response")
	}
	interval, validInterval := deviceSeconds(started.Interval)
	lifetime, validLifetime := deviceSeconds(started.ExpiresIn)
	verification, parseErr := url.Parse(started.VerificationUri)
	if parseErr != nil {
		return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitInternal, "server returned an invalid device authorization response")
	}
	verificationQuery, queryErr := url.ParseQuery(verification.RawQuery)
	if verification.Scheme+"://"+verification.Host != tr.Origin() || verification.User != nil || verification.Path != "/login/cli" || verification.RawPath != "" || verification.Fragment != "" || queryErr != nil || len(verificationQuery) != 1 || len(verificationQuery["device"]) != 1 || verificationQuery.Get("device") != "1" || started.DeviceCode == "" || len(started.DeviceCode) > 512 || !validInterval || interval < 5*time.Second || !validLifetime || lifetime > 15*time.Minute || !validDeviceUserCode(started.UserCode) || started.ExpiresAt.IsZero() {
		return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitInternal, "server returned an invalid device authorization response")
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
	_, _ = fmt.Fprintf(prompt, "On another device, visit %s\nEnter code: %s\nApprove a CLI login for %s.\n", verification.String(), started.UserCode, tr.Origin())
	if notify != nil {
		notify(DevicePrompt{VerificationURL: verification.String(), UserCode: started.UserCode, ExpiresIn: lifetime})
	}
	return PollDevice(pollCtx, tr, prompt, started.DeviceCode, interval)
}

// PollDevice polls the device token endpoint until approval, denial or expiry.
func PollDevice(ctx context.Context, tr Transport, stderr io.Writer, deviceCode string, interval time.Duration) (apigen.LoginResult, error) {
	wait := interval
	for {
		if err := tr.Wait(ctx, wait); err != nil {
			return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitAuth, "device login expired or was cancelled; start login again")
		}
		if ctx.Err() != nil {
			return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitAuth, "device login expired or was cancelled; start login again")
		}
		status, header, raw, err := tr.PostForm(ctx, api.PathPrefix+"/auth/cli-login/device/token", url.Values{
			"client_id": {"hikyo-cli"}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {deviceCode},
		})
		if err != nil {
			var networkError net.Error
			if ctx.Err() == nil && errors.As(err, &networkError) && networkError.Timeout() {
				if interval > time.Duration(math.MaxInt64)/2 {
					return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitAuth, "device login polling interval exceeds the remaining login lifetime")
				}
				interval *= 2
				wait = interval
				continue
			}
			return apigen.LoginResult{}, deviceTransportError(ctx, err)
		}
		if status == http.StatusTooManyRequests {
			retrySeconds, parseErr := strconv.ParseUint(strings.TrimSpace(header.Get("Retry-After")), 10, 64)
			if parseErr != nil || retrySeconds == 0 || retrySeconds > uint64(math.MaxInt64/int64(time.Second)) {
				return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitRateLimited, "device login throttled without a usable Retry-After")
			}
			// The validated header decides the delay, whatever error type the transport maps a 429 to.
			wait = max(interval, time.Duration(retrySeconds)*time.Second)
			_, _ = fmt.Fprintf(stderr, "hikyo: throttled by the server; retrying in %d s (Retry-After)\n", int(wait/time.Second))
			continue
		}
		if status == http.StatusOK {
			var token apigen.CLIDeviceToken
			if err := json.Unmarshal(raw, &token); err != nil || token.AccessToken == "" || token.TokenType != "Bearer" || token.ExpiresIn <= 0 {
				return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitInternal, "server returned an invalid device token response")
			}
			return apigen.LoginResult{SessionToken: &token.AccessToken, Session: token.Session, Principal: token.Principal}, nil
		}
		if status != http.StatusBadRequest {
			return apigen.LoginResult{}, tr.ResponseError(status, header, raw)
		}
		var protocol apigen.CLIDeviceError
		if err := json.Unmarshal(raw, &protocol); err != nil {
			return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitInternal, "server returned an invalid device polling response")
		}
		switch protocol.Error {
		case "slow_down":
			if interval > time.Duration(math.MaxInt64)-5*time.Second {
				return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitInternal, "device polling interval overflow")
			}
			interval += 5 * time.Second
			fallthrough
		case "authorization_pending":
			if serverInterval, valid := deviceSeconds(protocol.Interval); valid {
				interval = max(interval, serverInterval)
			}
			wait = interval
		case "expired_token", "access_denied":
			return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitAuth, "device login expired or was denied; start login again")
		default:
			return apigen.LoginResult{}, clienterr.Failf(clienterr.ExitRefused, "device login was refused; polling stopped")
		}
	}
}
