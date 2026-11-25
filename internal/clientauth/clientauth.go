// Package clientauth is the browser-handoff half of Hikyo client sign-in: the
// loopback PKCE login and its device-flow fallback, and the CLI reauthentication
// handoff that runs a passkey ceremony in the system browser. Both the CLI and
// the desktop shell use it.
//
// It owns the protocol (start, open the browser, an ephemeral loopback
// callback, redeem) and nothing else. Where the session token is kept is the
// caller's decision, and every request goes through a Transport the caller
// supplies, so this package never imports internal/cli or the server.
package clientauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"time"
)

// Transport is the origin-bound, trust-checked HTTP channel to one instance.
// The CLI's Client implements it through a small adapter, and the shell
// supplies its own over its pinned connection.
type Transport interface {
	// Origin is the canonical origin every request is bound to.
	Origin() string
	// Do sends a JSON request and decodes a 2xx body into out. A non-2xx
	// response comes back as the error the caller's taxonomy maps it to.
	Do(ctx context.Context, method, path string, body, out any) error
	// PostForm sends an application/x-www-form-urlencoded request and returns
	// the raw status, headers and body, bounded by the protocol size limit.
	PostForm(ctx context.Context, path string, form url.Values) (status int, header http.Header, body []byte, err error)
	// ResponseError maps a non-2xx response to the caller's error type.
	ResponseError(status int, header http.Header, body []byte) error
	// Wait sleeps for d or until ctx ends.
	Wait(ctx context.Context, d time.Duration) error
}

// proof returns 256 bits of CSPRNG output, base64url without padding.
func proof() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
