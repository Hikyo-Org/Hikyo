package clientauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/clienterr"
)

const reauthCallbackPath = "/callback"

// ReauthRequest is the intent a CLI reauthentication handoff covers, as
// CLIReauthStartRequest defines it in api/openapi.yaml.
type ReauthRequest struct {
	Purpose        string
	Operation      string
	EnvironmentIDs []string
	KeyIDs         []string
	// SelfConfig and DeveloperCredential carry the purpose-specific intent.
	SelfConfig          *apigen.SelfConfigReauthIntent
	DeveloperCredential *apigen.DeveloperCredentialReauthIntent
}

// RotatedSession is the replacement bearer a redeemed handoff returns. The
// caller stores it; it must not reach any rendering or logging path.
type RotatedSession struct {
	Token     string
	SessionID string
}

// Reauth binds the callback before opening the browser, then returns the
// rotated session after an exact state + PKCE exchange.
func Reauth(ctx context.Context, tr Transport, req ReauthRequest, openURL func(string) error) (RotatedSession, error) {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return RotatedSession{}, clienterr.Failf(clienterr.ExitUnavailable, "binding the CLI reauthentication callback: %v", err)
	}
	defer func() { _ = listener.Close() }()

	rawVerifier := make([]byte, 32)
	if _, err := rand.Read(rawVerifier); err != nil {
		return RotatedSession{}, clienterr.Failf(clienterr.ExitInternal, "generating the CLI reauthentication proof: %v", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(rawVerifier)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	redirectURI := "http://" + listener.Addr().String() + reauthCallbackPath
	environments := make([]apigen.ID, len(req.EnvironmentIDs))
	for i, environmentID := range req.EnvironmentIDs {
		environments[i] = apigen.ID(environmentID)
	}
	request := apigen.CLIReauthStartRequest{
		SelfConfig:          req.SelfConfig,
		DeveloperCredential: req.DeveloperCredential,
		Purpose:             apigen.CLIReauthStartRequestPurpose(req.Purpose),
		Operation:           apigen.CLIReauthStartRequestOperation(req.Operation),
		EnvironmentIds:      environments,
		PkceChallenge:       challenge,
		RedirectUri:         redirectURI,
	}
	if len(req.KeyIDs) > 0 {
		keys := make([]apigen.ID, len(req.KeyIDs))
		for i, keyID := range req.KeyIDs {
			keys[i] = apigen.ID(keyID)
		}
		request.KeyIds = &keys
	}
	var started apigen.CLIReauthStart
	if err := tr.Do(ctx, http.MethodPost, api.PathPrefix+"/auth/cli-reauth/start", request, &started); err != nil {
		return RotatedSession{}, err
	}

	target, err := url.Parse(tr.Origin())
	if err != nil {
		return RotatedSession{}, clienterr.Failf(clienterr.ExitInternal, "the trusted instance origin is invalid: %v", err)
	}
	target.Path = "/reauth/cli"
	target.RawPath = ""
	target.RawQuery = url.Values{"transaction": []string{started.State}}.Encode()
	target.Fragment = ""

	code := make(chan string, 1)
	var callbackOnce sync.Once
	callback := http.NewServeMux()
	callback.HandleFunc(reauthCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Host != listener.Addr().String() || !exactCallbackQuery(r.URL.Query(), started.State) {
			http.Error(w, "This CLI authorization callback did not match the request.", http.StatusBadRequest)
			return
		}
		accepted := false
		callbackOnce.Do(func() {
			accepted = true
			code <- r.URL.Query().Get("code")
		})
		if accepted {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("CLI authorization received. Return to the Hikyo app or terminal.\n"))
			return
		}
		http.Error(w, "This CLI authorization callback was already used.", http.StatusConflict)
	})
	server := &http.Server{Handler: callback, ReadHeaderTimeout: 5 * time.Second}
	defer func() { _ = server.Close() }()
	go func() { _ = server.Serve(listener) }()

	if openURL == nil {
		openURL = OpenBrowser
	}
	if err := openURL(target.String()); err != nil {
		return RotatedSession{}, clienterr.Failf(clienterr.ExitUnavailable, "opening the browser for CLI authorization: %v", err)
	}

	wait := time.Until(started.ExpiresAt)
	if wait <= 0 || wait > 5*time.Minute {
		wait = 5 * time.Minute
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	select {
	case value := <-code:
		return RedeemReauth(ctx, tr, value, []byte(verifier))
	case <-ctx.Done():
		return RotatedSession{}, ctx.Err()
	case <-deadline.C:
		return RotatedSession{}, clienterr.Failf(clienterr.ExitAuth, "CLI authorization expired; start the command again")
	}
}

// RedeemReauth completes a browser-approved handoff and returns the rotated
// bearer without logging it.
func RedeemReauth(ctx context.Context, tr Transport, code string, verifier []byte) (RotatedSession, error) {
	var result apigen.CLIReauthRedeemed
	request := apigen.CLIReauthRedeemRequest{Code: code, PkceVerifier: string(verifier)}
	if err := tr.Do(ctx, http.MethodPost, api.PathPrefix+"/auth/cli-reauth/redeem", request, &result); err != nil {
		return RotatedSession{}, err
	}
	if result.SessionToken == "" {
		return RotatedSession{}, clienterr.Failf(clienterr.ExitInternal, "server returned no rotated CLI session token")
	}
	return RotatedSession{Token: result.SessionToken, SessionID: string(result.SessionId)}, nil
}
