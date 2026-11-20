package shell

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	maxScannedResponse = 32 << 20
	maxGuardedRequest  = 64 << 10
)

// flight is one request in progress. An event stream is cancelled when the
// bearer changes, so the page reconnects with the new one (ADR D4).
type flight struct {
	cancel context.CancelFunc
	stream atomic.Bool
	// sent is the bearer the latest attempt of this request carried.
	sent atomic.Pointer[string]
}

func (fl *flight) sentBearer() string {
	if p := fl.sent.Load(); p != nil {
		return *p
	}
	return ""
}

type flights struct {
	mu sync.Mutex
	m  map[*flight]struct{}
}

type flightKey struct{}

func (f *flights) add(fl *flight) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[*flight]struct{}{}
	}
	f.m[fl] = struct{}{}
}

func (f *flights) remove(fl *flight) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.m, fl)
}

func (f *flights) cancelStreams() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for fl := range f.m {
		if fl.stream.Load() {
			fl.cancel()
		}
	}
}

func (f *flights) cancelAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for fl := range f.m {
		fl.cancel()
	}
}

// errBlocked is a response the proxy refuses to release.
type errBlocked struct{ msg string }

func (e errBlocked) Error() string { return e.msg }

func (c *Conn) newProxy() *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite:        c.rewrite,
		Transport:      bearerTransport{c},
		ModifyResponse: c.modifyResponse,
		// Streams each chunk as it arrives, which event streams need.
		FlushInterval: -1,
		ErrorHandler:  c.proxyError,
	}
}

// rewrite binds the request to this connection's instance. The page never
// chooses the credential: Cookie, Origin, Referer and its own Authorization
// are dropped, and the bearer is added at send time by bearerTransport.
func (c *Conn) rewrite(pr *httputil.ProxyRequest) {
	pr.SetURL(c.url)
	h := pr.Out.Header
	for name := range h {
		if strings.HasPrefix(strings.ToLower(name), "x-wails-") {
			delete(h, name)
		}
	}
	for _, name := range []string{"Cookie", "Origin", "Referer", "Authorization", "X-Http-Method-Override", "X-Method-Override", "X-Forwarded-For", "Forwarded"} {
		h.Del(name)
	}
	// The proxy can't remove a token from a body it can't read (ADR D4).
	h.Set("Accept-Encoding", "identity")
	h.Set("User-Agent", c.inst.userAgent)
}

// bearerTransport adds the current bearer when the request is sent, and
// retries an idempotent request once if a rotation made its bearer stale.
type bearerTransport struct{ c *Conn }

func (b bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	fl, _ := req.Context().Value(flightKey{}).(*flight)
	sent := b.c.bearer()
	if fl != nil {
		fl.sent.Store(&sent)
	}
	resp, err := b.c.inst.rt.RoundTrip(withBearer(req, sent))
	if err != nil || resp.StatusCode != http.StatusUnauthorized || !idempotentWithoutBody(req) {
		return resp, err
	}
	if now := b.c.bearer(); now != "" && now != sent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		_ = resp.Body.Close()
		if fl != nil {
			fl.sent.Store(&now)
		}
		return b.c.inst.rt.RoundTrip(withBearer(req, now))
	}
	return resp, nil
}

func withBearer(req *http.Request, token string) *http.Request {
	out := req.Clone(req.Context())
	out.Header = req.Header.Clone()
	if token != "" {
		out.Header.Set("Authorization", "Bearer "+token)
	}
	return out
}

func idempotentWithoutBody(req *http.Request) bool {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return req.Body == nil || req.Body == http.NoBody
	}
	return false
}

func jsonError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	body, _ := marshalError(code, message)
	_, _ = w.Write(body)
}

// serveProxy answers a page request: the D4 refusals first, then the proxy.
func (c *Conn) serveProxy(w http.ResponseWriter, r *http.Request) {
	switch c.m.policy.Classify(r.Method, r.URL.EscapedPath()) {
	case Malformed:
		jsonError(w, http.StatusBadRequest, "invalid", "non-canonical API path")
		return
	case Refused:
		jsonError(w, http.StatusForbidden, "refused", "this operation isn't available in the desktop app")
		return
	}
	if IsAPIPath(r.URL.Path) {
		// One session change at a time, from before the bearer is chosen until
		// the new session is committed or the change fails (ADR D4). Reads stay
		// outside the gate.
		if c.m.policy.RotationFor(r.Method, r.URL.EscapedPath()) != NotRotating {
			release, err := c.sessGate.acquire(r.Context())
			if err != nil {
				return
			}
			defer release()
		}
		if reason := c.Stopped(); reason != "" {
			jsonError(w, http.StatusUnauthorized, "unauthenticated", reason)
			return
		}
		if c.isTOTPReauth(r) && !c.guardTOTPReauth(w, r) {
			return
		}
	}
	ctx, cancel := context.WithCancel(r.Context())
	fl := &flight{cancel: cancel}
	c.flights.add(fl)
	defer func() { c.flights.remove(fl); cancel() }()
	c.proxy.ServeHTTP(w, r.WithContext(context.WithValue(ctx, flightKey{}, fl)))
}

func (c *Conn) isTOTPReauth(r *http.Request) bool {
	return r.Method == http.MethodPost && c.m.policy.RotationFor(r.Method, r.URL.EscapedPath()) == Rotate &&
		strings.EqualFold(strings.TrimRight(r.URL.Path, "/"), "/api/v1/auth/reauth/totp")
}

// guardTOTPReauth refuses a TOTP reauth whose purpose is developer-credential
// (ADR D4). The body is decoded as JSON and any parse error or duplicate name
// fails closed. It reports whether the request may proceed.
func (c *Conn) guardTOTPReauth(w http.ResponseWriter, r *http.Request) bool {
	body, err := readLimited(r.Body, maxGuardedRequest)
	_ = r.Body.Close()
	if err != nil {
		jsonError(w, http.StatusForbidden, "refused", "the reauthentication request could not be read")
		return false
	}
	members, err := object(body)
	if err != nil {
		jsonError(w, http.StatusForbidden, "refused", "the reauthentication request could not be read")
		return false
	}
	// encoding/json v1 on the instance matches member names case-insensitively
	// (and folds a few non-ASCII letters), so every spelling of "purpose" is
	// the purpose, and two spellings are ambiguous: refuse both.
	var purposeKey string
	for key := range members {
		if !strings.EqualFold(key, "purpose") {
			continue
		}
		if purposeKey != "" {
			jsonError(w, http.StatusForbidden, "refused", "the reauthentication request could not be read")
			return false
		}
		purposeKey = key
	}
	if purposeKey != "" {
		purpose, isString := stringMember(members, purposeKey)
		switch {
		case isString && strings.EqualFold(strings.TrimSpace(purpose), "developer-credential"):
			jsonError(w, http.StatusForbidden, "refused", "developer credentials are minted from a terminal")
			return false
		case !isString && !isNull(members[purposeKey]):
			jsonError(w, http.StatusForbidden, "refused", "the reauthentication request could not be read")
			return false
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	return true
}

func isJSONType(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return mt == "application/json" || strings.HasSuffix(mt, "+json")
}

func (c *Conn) modifyResponse(resp *http.Response) error {
	h := resp.Header
	h.Del("Set-Cookie")
	c.rewriteLocation(resp)
	fl, _ := resp.Request.Context().Value(flightKey{}).(*flight)
	// A response that fails here stops the connection only while the session the
	// request carried is still the current one (stopSent).
	sent := c.bearer()
	if fl != nil {
		sent = fl.sentBearer()
	}
	stop := func(reason string) { c.stopSent(sent, reason) }
	rot := c.m.policy.RotationFor(resp.Request.Method, resp.Request.URL.EscapedPath())
	ok2xx := resp.StatusCode/100 == 2
	// sessionChange: a success on a route that carries a new session. Anything
	// the shell can't read there has already changed the session on the
	// instance, so the connection stops.
	sessionChange := ok2xx && rot != NotRotating
	if mt, _, _ := mime.ParseMediaType(h.Get("Content-Type")); mt == "text/event-stream" {
		// A stream is passed through unscanned, so it can't carry a session change.
		if rot != NotRotating {
			if sessionChange {
				stop("The instance answered a session change with a stream the app can't read. Sign in again.")
			}
			return errBlocked{"unreadable session response"}
		}
		if fl != nil {
			// Marked first, then compared: a rotation between the send and
			// this point is seen here, and one after it by cancelStreams.
			fl.stream.Store(true)
			if fl.sentBearer() != c.bearer() {
				fl.cancel()
			}
		}
		return nil
	}
	switch enc := strings.ToLower(strings.TrimSpace(h.Get("Content-Encoding"))); enc {
	case "", "identity":
	case "gzip":
		// A scheme handler's bytes are not decoded by WebKit, and the shell
		// can't scan what it can't read.
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			if sessionChange {
				stop("The response to a session change could not be read. Sign in again.")
			}
			return errBlocked{"the instance sent an unreadable response"}
		}
		resp.Body = struct {
			io.Reader
			io.Closer
		}{zr, resp.Body}
		h.Del("Content-Encoding")
		h.Del("Content-Length")
		resp.ContentLength = -1
	default:
		if sessionChange {
			stop("The response to a session change could not be read. Sign in again.")
		}
		return errBlocked{"the instance used an encoding the app can't read"}
	}
	if resp.Request.Method == http.MethodHead || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusResetContent {
		if sessionChange {
			stop("The instance answered a session change without a new session. Sign in again.")
		}
		return nil
	}
	if !isJSONType(h.Get("Content-Type")) {
		if sessionChange {
			stop("The instance answered a session change with something the app can't read. Sign in again.")
			return errBlocked{"unreadable session response"}
		}
		return nil
	}
	// A JSON body is scanned whatever its status: an error response carries no
	// session either (ADR D4). Only a success may install one.
	raw, err := readLimited(resp.Body, maxScannedResponse)
	_ = resp.Body.Close()
	if err != nil {
		if sessionChange {
			stop("The response to a session change could not be read. Sign in again.")
		}
		return errBlocked{"the response could not be read"}
	}
	view, err := splitToken(raw)
	switch {
	case errors.Is(err, errNotObject) && !sessionChange:
		setBody(resp, raw)
		return nil
	case err != nil:
		if sessionChange {
			stop("The instance sent a malformed session response. Sign in again.")
		}
		return errBlocked{"the instance sent a malformed JSON response"}
	case view.Token != "" && (rot == NotRotating || !ok2xx):
		// Unexpected tokens never reach the page, and are treated as an error.
		if view.Token != c.bearer() {
			c.revokeAsync(view.Token)
		}
		log.Printf("connection %s: removed an unexpected session token from %s", c.host, logSafe(resp.Request.URL.Path))
		return errBlocked{"the instance returned a session token where none is allowed"}
	case !ok2xx:
		setBody(resp, raw)
		return nil
	case view.Token == "" && rot != NotRotating:
		// Both kinds of session change return the new session; a success without
		// one leaves the old bearer in use for a session the instance replaced.
		stop("The instance answered a session change without a new session. Sign in again.")
	case view.Token != "":
		// A failed check has already stopped the connection and revoked the new
		// session. The body is still released without the token, because
		// recovery codes in it can't be fetched again (ADR D4).
		_ = c.applyRotation(rot, view, sent)
	}
	setBody(resp, view.Body)
	return nil
}

func setBody(resp *http.Response, body []byte) {
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
}

// rewriteLocation keeps a same-origin redirect inside the window.
func (c *Conn) rewriteLocation(resp *http.Response) {
	loc := resp.Header.Get("Location")
	if loc == "" {
		return
	}
	u, err := url.Parse(loc)
	if err != nil || !u.IsAbs() {
		return
	}
	if c.sameSite(u) && u.User == nil {
		rel := url.URL{Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery, Fragment: u.Fragment}
		if rel.Path == "" {
			rel.Path = "/"
		}
		if strings.HasPrefix(rel.Path, "//") {
			// A bare "//x" would read as a reference to another host, possibly
			// another connection's: write the scheme and host out.
			rel.Scheme, rel.Host = PageScheme, c.host
		}
		resp.Header.Set("Location", rel.String())
	}
}

func (c *Conn) proxyError(w http.ResponseWriter, r *http.Request, err error) {
	var blocked errBlocked
	if errors.As(err, &blocked) {
		log.Printf("connection %s: blocked %s %s: %s", c.host, r.Method, logSafe(r.URL.Path), blocked.msg)
		jsonError(w, http.StatusBadGateway, "failed", blocked.msg)
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	log.Printf("connection %s: %s %s: instance unreachable: %v", c.host, r.Method, logSafe(r.URL.Path), logSafe(err.Error()))
	jsonError(w, http.StatusBadGateway, "failed", "the instance could not be reached")
}
