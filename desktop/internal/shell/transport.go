package shell

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/clientauth"
	"github.com/Hikyo-Org/hikyo/internal/clienterr"
	"github.com/Hikyo-Org/hikyo/internal/clienttrust"
)

const maxAPIResponse = 4 << 20

// Instance is the TLS-trusted HTTP channel to one instance. Every request
// the shell makes itself, and every request it forwards for a page, uses its
// transport, so the connection's trust choice (ADR D5) applies to all of them.
type Instance struct {
	Origin    string
	userAgent string
	rt        *http.Transport
	client    *http.Client
}

// newInstance builds the channel for a canonical origin. It refuses plain
// HTTP unless the origin is loopback and the caller allows it (development).
func newInstance(origin string, trust Trust, userAgent string, allowLoopbackHTTP bool) (*Instance, error) {
	canonical, err := clienttrust.CanonicalOrigin(origin)
	if err != nil {
		return nil, err
	}
	rt := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 90 * time.Second,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       60 * time.Second,
		// The proxy asks for identity encoding itself (ADR D4).
		DisableCompression: true,
	}
	switch {
	case strings.HasPrefix(canonical, "https://"):
		if trust.Mode != clienttrust.ModeSPKI && trust.Mode != clienttrust.ModeSystemCA {
			return nil, errors.New("a connection needs an explicit trust mode")
		}
		if trust.Mode == clienttrust.ModeSPKI && trust.Pin == "" {
			return nil, errors.New("an SPKI connection needs its pin")
		}
		rt.TLSClientConfig = clienttrust.TLSConfig(canonical, trust.Mode, trust.Pin, "")
		rt.ForceAttemptHTTP2 = true
	case allowLoopbackHTTP && clienttrust.IsLoopbackOrigin(canonical):
		// Development only: a local hikyo server without TLS.
	default:
		return nil, errors.New("the desktop app connects to https instances only")
	}
	return &Instance{
		Origin:    canonical,
		userAgent: userAgent,
		rt:        rt,
		client: &http.Client{
			Transport: rt,
			// A redirect is never followed with the bearer attached (ADR D4).
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (in *Instance) close() { in.rt.CloseIdleConnections() }

func (in *Instance) newRequest(ctx context.Context, method, path, bearer string, body []byte, contentType string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, in.Origin+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", in.userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return req, nil
}

// call sends one request and returns status, headers and the bounded body.
func (in *Instance) call(ctx context.Context, method, path, bearer string, body []byte, contentType string) (int, http.Header, []byte, error) {
	req, err := in.newRequest(ctx, method, path, bearer, body, contentType)
	if err != nil {
		return 0, nil, nil, err
	}
	resp, err := in.client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := readLimited(resp.Body, maxAPIResponse)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return resp.StatusCode, resp.Header, data, nil
}

// authTransport adapts an Instance to clientauth.Transport. bearer may be nil
// for the unauthenticated login handoffs.
type authTransport struct {
	in     *Instance
	bearer func() string
}

var _ clientauth.Transport = authTransport{}

func (t authTransport) Origin() string { return t.in.Origin }

func (t authTransport) token() string {
	if t.bearer == nil {
		return ""
	}
	return t.bearer()
}

func (t authTransport) Do(ctx context.Context, method, path string, body, out any) error {
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return err
		}
	}
	status, header, data, err := t.in.call(ctx, method, path, t.token(), raw, "application/json")
	if err != nil {
		return clienterr.Failf(clienterr.ExitUnavailable, "cannot reach %s: %v", t.in.Origin, err)
	}
	if status/100 != 2 {
		return t.ResponseError(status, header, data)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return clienterr.Failf(clienterr.ExitInternal, "the instance sent an unreadable response")
	}
	return nil
}

func (t authTransport) PostForm(ctx context.Context, path string, form url.Values) (int, http.Header, []byte, error) {
	return t.in.call(ctx, http.MethodPost, path, t.token(), []byte(form.Encode()), "application/x-www-form-urlencoded")
}

// ResponseError maps a non-2xx status to the shared error taxonomy. The body
// is not echoed: it is instance-controlled text.
func (t authTransport) ResponseError(status int, header http.Header, _ []byte) error {
	switch {
	case status == http.StatusTooManyRequests:
		seconds, _ := strconv.Atoi(strings.TrimSpace(header.Get("Retry-After")))
		return &clienterr.RateLimitedError{Message: "the instance is rate limiting this app", RetryAfter: time.Duration(max(seconds, 0)) * time.Second}
	case status == http.StatusUnauthorized:
		return clienterr.Failf(clienterr.ExitAuth, "the instance did not accept the session (%d)", status)
	case status == http.StatusNotFound:
		return clienterr.Failf(clienterr.ExitNotFound, "the instance answered %d", status)
	case status >= 500:
		return clienterr.Failf(clienterr.ExitUnavailable, "the instance answered %d", status)
	default:
		return clienterr.Failf(clienterr.ExitRefused, "the instance answered %d", status)
	}
}

func (t authTransport) Wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
