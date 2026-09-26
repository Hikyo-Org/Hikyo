package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
)

const (
	Path                       = "/mcp"
	ProtocolVersion            = "2026-07-28"
	MaxRequestBytes            = 256 << 10
	MaxStaticResponseBytes     = 64 << 10
	MaxBearerBytes             = 4096
	ToolExecutionTimeout       = 30 * time.Second
	DefaultInstanceConcurrency = 64
)

type discoveryAdmission interface {
	AllowDiscovery(string) bool
}

// Options fixes the transport policy at process boot.
type Options struct {
	Registry       *Registry
	ExternalOrigin string
	AllowedOrigins []string
	TrustedProxies []*net.IPNet
	Admission      discoveryAdmission
	Version        string
	MaxConcurrent  int
	// CursorSealer encrypts and authenticates pagination cursors. Its live key
	// comes from shared authoritative configuration, so cursors are portable
	// across replicas and token-key rotation cannot leave a stale cached key.
	// It is required whenever the registry advertises a paginated tool. New
	// refuses a non-empty registry without it. The crypto chokepoint confines
	// the AEAD primitive to internal/crypto.
	CursorSealer CursorSealer
	// OnReleaseFailure is told when an admission slot could not be released
	// after the call itself succeeded (the call keeps its committed outcome).
	// It receives the authorization operation and the coordinator's error,
	// never a bearer or request material. The package owns no telemetry sink
	// (MCP secret boundary, internal/boundary); the app routes this to its
	// logger. Nil drops the notice, and the lease still expires on its TTL.
	OnReleaseFailure func(operation string, err error)
}

type handler struct {
	version        string
	sdk            http.Handler
	externalScheme string
	externalHost   string
	allowedOrigins []string
	trustedProxies []*net.IPNet
	admission      discoveryAdmission
	slots          chan struct{}
	cursorSealer   CursorSealer
	onReleaseFail  func(operation string, err error)
}

type bearerContextKey struct{}
type callStateContextKey struct{}

type callState struct {
	rateLimited     atomic.Bool
	unauthenticated atomic.Bool
	mu              sync.Mutex
	releaseOp       authz.Operation
	releaseErr      error
}

// markReleaseFailed records that the call's admission slot could not be
// released after the call itself succeeded. The call keeps its committed
// outcome; the transport hands the cleanup failure to Options.OnReleaseFailure
// so it is observable without telling the caller to retry a write that
// already landed.
func markReleaseFailed(ctx context.Context, op authz.Operation, err error) {
	if state, ok := ctx.Value(callStateContextKey{}).(*callState); ok {
		state.mu.Lock()
		state.releaseOp, state.releaseErr = op, err
		state.mu.Unlock()
	}
}

func markRateLimited(ctx context.Context) {
	if state, ok := ctx.Value(callStateContextKey{}).(*callState); ok {
		state.rateLimited.Store(true)
	}
}

// markUnauthenticated records that the presented bearer is not a live
// artifact. The transport then answers with the same uniform 401 a missing
// bearer receives, which is the REST disposition for domain.ErrUnauthenticated
// and lets an MCP client surface an authentication problem instead of handing
// the model a retryable tool error. Authorization denial for a live artifact
// is not routed here; it stays the indistinguishable safe tool error.
func markUnauthenticated(ctx context.Context) {
	if state, ok := ctx.Value(callStateContextKey{}).(*callState); ok {
		state.unauthenticated.Store(true)
	}
}

// New constructs the feature-gated endpoint handler around the pinned SDK.
func New(options Options) (http.Handler, error) {
	concurrency := options.MaxConcurrent
	if concurrency == 0 {
		concurrency = DefaultInstanceConcurrency
	}
	if concurrency < 1 || concurrency > DefaultInstanceConcurrency {
		return nil, errors.New("mcpserver: instance concurrency must be between 1 and 64")
	}
	origin, err := url.Parse(options.ExternalOrigin)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") ||
		origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" ||
		options.ExternalOrigin != origin.Scheme+"://"+origin.Host {
		return nil, errors.New("mcpserver: external origin must be canonical")
	}
	for _, candidate := range options.AllowedOrigins {
		allowed, parseErr := url.Parse(candidate)
		if parseErr != nil || (allowed.Scheme != "http" && allowed.Scheme != "https") ||
			allowed.Host == "" || allowed.User != nil || allowed.Path != "" || allowed.RawQuery != "" || allowed.Fragment != "" ||
			candidate != allowed.Scheme+"://"+allowed.Host || candidate == "null" || candidate == "*" {
			return nil, errors.New("mcpserver: allowed origins must be canonical")
		}
	}
	registrations := options.Registry.freeze()
	if len(registrations) > 0 && options.CursorSealer == nil {
		return nil, errors.New("mcpserver: a paginated tool registry requires a cursor sealer")
	}
	description := "Read-only Hikyo configuration tools."
	for _, item := range registrations {
		if item.row.Class == ToolClassWriteSurface {
			description = "Hikyo configuration tools."
			break
		}
	}
	server := mcp.NewServer(&mcp.Implementation{
		Name: "hikyo", Title: "Hikyo", Description: description, Version: options.Version,
	}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
	})
	for _, item := range registrations {
		item.install(server)
	}
	sdk := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: MaxRequestBytes,
		PropagateRequestCancellation: true,
	})
	return &handler{
		version: options.Version,
		sdk:     sdk, externalScheme: origin.Scheme, externalHost: origin.Host,
		allowedOrigins: slices.Clone(options.AllowedOrigins),
		trustedProxies: slices.Clone(options.TrustedProxies),
		admission:      options.Admission, slots: make(chan struct{}, concurrency),
		cursorSealer: options.CursorSealer, onReleaseFail: options.OnReleaseFailure,
	}, nil
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type requestMeta struct {
	Meta map[string]any `json:"_meta"`
	Name string         `json:"name"`
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == CodexPath {
		h.serveCodex(w, r)
		return
	}
	if !h.validHost(r) || !h.validOrigin(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		h.sdk.ServeHTTP(w, r)
		return
	}
	if baseMediaType(r.Header.Get("Content-Type")) != "application/json" || !acceptsMCP(r.Header.Values("Accept")) {
		h.sdk.ServeHTTP(w, r)
		return
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, MaxRequestBytes+1))
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if len(raw) > MaxRequestBytes {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var envelope rpcEnvelope
	if err := decodeOne(raw, &envelope); err != nil {
		h.sdk.ServeHTTP(w, r)
		return
	}
	if envelope.JSONRPC != "2.0" {
		h.serveSDK(w, r, envelope.Method, envelope.ID, MaxStaticResponseBytes, nil)
		return
	}

	requestedVersion, requestedName := metadataFrom(envelope.Params)
	headerVersions := r.Header.Values("Mcp-Protocol-Version")
	if len(headerVersions) != 1 || (requestedVersion != "" && headerVersions[0] != requestedVersion) {
		message := "protocol mirror headers do not match request"
		if envelope.Method == "initialize" {
			message += "; native Codex clients must use /mcp/codex"
		}
		writeRPCError(w, http.StatusBadRequest, envelope.ID, -32020, message, nil)
		return
	}
	methodHeaders := r.Header.Values("Mcp-Method")
	nameHeaders := r.Header.Values("Mcp-Name")
	if len(methodHeaders) != 1 || methodHeaders[0] != envelope.Method ||
		(envelope.Method == "tools/call" && len(nameHeaders) != 1) ||
		(envelope.Method != "tools/call" && len(nameHeaders) != 0) {
		writeRPCError(w, http.StatusBadRequest, envelope.ID, -32020, "protocol mirror headers do not match request", nil)
		return
	}
	if envelope.Method == "tools/call" {
		decodedName, ok := decodeHeaderSentinel(nameHeaders[0])
		if !ok || decodedName != requestedName {
			writeRPCError(w, http.StatusBadRequest, envelope.ID, -32020, "protocol mirror headers do not match request", nil)
			return
		}
		// The pinned SDK v1.7.0 validator compares Mcp-Name without decoding
		// (fixed upstream in go-sdk #1242, unreleased as of v1.8.0-pre.2), so
		// it must see the decoded value this check accepted. The write-back
		// stays correct once the SDK decodes too: a decoded tool name never
		// carries the sentinel prefix.
		r.Header.Set("Mcp-Name", decodedName)
	}
	if requestedVersion == "" {
		// Missing modern request metadata is an invalid-params error. The SDK
		// owns that schema check; a present mirror header cannot turn malformed
		// body metadata into a header-mismatch error.
		h.serveSDK(w, r, envelope.Method, envelope.ID, MaxStaticResponseBytes, nil)
		return
	}
	if requestedVersion != "" && requestedVersion != ProtocolVersion {
		writeRPCError(w, http.StatusBadRequest, envelope.ID, -32022, "unsupported protocol version; upgrade the MCP client", map[string]any{
			"supported": []string{ProtocolVersion}, "requested": requestedVersion,
		})
		return
	}
	if len(headerVersions) == 1 && headerVersions[0] != ProtocolVersion {
		writeRPCError(w, http.StatusBadRequest, envelope.ID, -32022, "unsupported protocol version; upgrade the MCP client", map[string]any{
			"supported": []string{ProtocolVersion}, "requested": headerVersions[0],
		})
		return
	}
	if envelope.Method != "server/discover" && envelope.Method != "tools/list" && envelope.Method != "tools/call" {
		writeRPCError(w, http.StatusNotFound, envelope.ID, -32601, "method not registered", nil)
		return
	}
	if len(r.Header.Values("Mcp-Session-Id")) != 0 {
		writeRPCError(w, http.StatusBadRequest, envelope.ID, -32020, "stateless transport does not accept a session id", nil)
		return
	}

	sourceIP := h.sourceIP(r)
	ctx := audit.WithContext(r.Context(), audit.Context{
		SourceIP: sourceIP, UserAgent: r.UserAgent(), Origin: audit.OriginMCP, RequestOrigin: r.Header.Get("Origin"),
	})
	if envelope.Method == "server/discover" || envelope.Method == "tools/list" {
		if h.admission != nil && !h.admission.AllowDiscovery(sourceIP) {
			// Every MCP refusal advertises 60 s, an upper bound on each windowed
			// limiter behind it: the one-minute discovery window, the 1 s token
			// refill, and the 35 s lease TTL. It can overshoot but never names an
			// instant the same window still refuses absent new traffic (#806).
			// The per-node in-flight cap has no window; 60 s is a hint there.
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		r = r.WithContext(ctx)
		if len(envelope.ID) == 0 {
			h.serveValidatedStaticNotification(w, r, raw)
			return
		}
		h.serveStatic(w, r, envelope.Method, envelope.ID)
		return
	}

	if len(envelope.ID) == 0 {
		// tools/call is a request. As a notification it is malformed, and it
		// is refused here, before the slot and the bearer, so a missing and a
		// presented bearer receive the same response and no tool ever runs.
		writeRPCError(w, http.StatusBadRequest, nil, -32600, "tools/call requires a request id", nil)
		return
	}

	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	bearer, ok := parseBearer(r.Header.Values("Authorization"))
	if !ok {
		writeUnauthorized(w)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, ToolExecutionTimeout)
	defer cancel()
	state := &callState{}
	ctx = context.WithValue(ctx, callStateContextKey{}, state)
	ctx = context.WithValue(ctx, bearerContextKey{}, newBearer(bearer))
	ctx = withCursorSealer(ctx, h.cursorSealer)
	h.serveSDK(w, r.WithContext(ctx), envelope.Method, envelope.ID, 0, state)
}

func (h *handler) serveValidatedStaticNotification(w http.ResponseWriter, r *http.Request, raw []byte) {
	objectStart := bytes.IndexByte(raw, '{')
	if objectStart < 0 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	withID := make([]byte, 0, len(raw)+7)
	withID = append(withID, raw[:objectStart+1]...)
	withID = append(withID, `"id":0,`...)
	withID = append(withID, raw[objectStart+1:]...)
	r.Body = io.NopCloser(bytes.NewReader(withID))

	capture := newCapturedResponse()
	h.sdk.ServeHTTP(capture, r)
	if capture.status >= http.StatusOK && capture.status < http.StatusMultipleChoices &&
		capture.body.Len() <= MaxStaticResponseBytes {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if capture.body.Len() > MaxStaticResponseBytes {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	body := capture.body.Bytes()
	if baseMediaType(capture.header.Get("Content-Type")) == "application/json" {
		var response map[string]json.RawMessage
		if json.Unmarshal(body, &response) == nil {
			if _, hasID := response["id"]; hasID {
				response["id"] = json.RawMessage("null")
				body, _ = json.Marshal(response)
			}
		}
	}
	copyHeaders(w.Header(), capture.header)
	w.Header().Del("Content-Length")
	w.WriteHeader(capture.status)
	_, _ = w.Write(body)
}

func (h *handler) serveStatic(w http.ResponseWriter, r *http.Request, method string, requestID json.RawMessage) {
	h.serveSDK(w, r, method, requestID, MaxStaticResponseBytes, nil)
}

func (h *handler) serveSDK(w http.ResponseWriter, r *http.Request, method string, requestID json.RawMessage, maxBytes int, state *callState) {
	capture := newCapturedResponse()
	h.sdk.ServeHTTP(capture, r)
	if state != nil {
		state.mu.Lock()
		releaseOp, releaseErr := state.releaseOp, state.releaseErr
		state.mu.Unlock()
		if releaseErr != nil && h.onReleaseFail != nil {
			// The bearer never crosses; the operation and the error are enough
			// to find a stuck lease, which expires on its own TTL regardless.
			h.onReleaseFail(string(releaseOp), releaseErr)
		}
	}
	if state != nil && state.rateLimited.Load() {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	if state != nil && state.unauthenticated.Load() {
		writeUnauthorized(w)
		return
	}
	body := capture.body.Bytes()
	if len(body) > 0 && baseMediaType(capture.header.Get("Content-Type")) == "application/json" {
		var response map[string]json.RawMessage
		if json.Unmarshal(body, &response) == nil {
			if _, hasID := response["id"]; hasID && len(requestID) > 0 {
				response["id"] = slices.Clone(requestID)
			}
			if method == "server/discover" {
				var result map[string]json.RawMessage
				if json.Unmarshal(response["result"], &result) == nil {
					result["supportedVersions"], _ = json.Marshal([]string{ProtocolVersion})
					response["result"], _ = json.Marshal(result)
				}
			}
			body, _ = json.Marshal(response)
		}
	}
	if maxBytes > 0 && len(body) > maxBytes {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	copyHeaders(w.Header(), capture.header)
	w.Header().Del("Content-Length")
	w.WriteHeader(capture.status)
	_, _ = w.Write(body)
}

// writeUnauthorized is the one authentication-failure response: a missing
// bearer and a presented bearer that is not live are byte-identical.
func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

const (
	headerSentinelPrefix = "=?base64?"
	headerSentinelSuffix = "?="
)

// decodeHeaderSentinel applies the pinned protocol's Base64 sentinel rule to
// one mirror header value: a value wrapped in =?base64?...?= is decoded with
// strict standard Base64, and any other value is used as presented. A wrapped
// value that does not decode is a mirror-header failure, never a match.
func decodeHeaderSentinel(value string) (string, bool) {
	encoded, ok := strings.CutPrefix(value, headerSentinelPrefix)
	if !ok {
		return value, true
	}
	encoded, ok = strings.CutSuffix(encoded, headerSentinelSuffix)
	if !ok {
		return "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", false
	}
	return string(decoded), true
}

func parseBearer(values []string) (string, bool) {
	if len(values) != 1 || len(values[0]) > len("Bearer ")+MaxBearerBytes {
		return "", false
	}
	value, ok := strings.CutPrefix(values[0], "Bearer ")
	if !ok || value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, " \t\r\n,") {
		return "", false
	}
	return value, true
}

func metadataFrom(params json.RawMessage) (version, name string) {
	var decoded requestMeta
	if json.Unmarshal(params, &decoded) != nil || decoded.Meta == nil {
		return "", ""
	}
	version, _ = decoded.Meta["io.modelcontextprotocol/protocolVersion"].(string)
	return version, decoded.Name
}

func decodeOne(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}

func writeRPCError(w http.ResponseWriter, status int, id json.RawMessage, code int, message string, data any) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	payload := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    any    `json:"data,omitempty"`
		} `json:"error"`
	}{JSONRPC: "2.0", ID: id}
	payload.Error.Code = code
	payload.Error.Message = message
	payload.Error.Data = data
	body, err := json.Marshal(payload)
	if err != nil || len(body) > MaxStaticResponseBytes {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (h *handler) validHost(r *http.Request) bool {
	host := r.Host
	if h.trusted(r.RemoteAddr) {
		forwarded := r.Header.Values("X-Forwarded-Host")
		if len(forwarded) != 1 || forwarded[0] == "" || strings.Contains(forwarded[0], ",") {
			return false
		}
		host = forwarded[0]
		forwardedProto := r.Header.Values("X-Forwarded-Proto")
		if len(forwardedProto) != 1 || forwardedProto[0] == "" || strings.Contains(forwardedProto[0], ",") || forwardedProto[0] != h.externalScheme {
			return false
		}
	}
	return host != "" && !strings.Contains(host, ",") && strings.EqualFold(host, h.externalHost)
}

func (h *handler) validOrigin(r *http.Request) bool {
	values := r.Header.Values("Origin")
	if len(values) == 0 {
		return true
	}
	return len(values) == 1 && values[0] != "null" && !strings.Contains(values[0], ",") && slices.Contains(h.allowedOrigins, values[0])
}

func (h *handler) trusted(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	for _, network := range h.trustedProxies {
		if ip != nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func (h *handler) sourceIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !h.trusted(r.RemoteAddr) {
		return host
	}
	values := r.Header.Values("X-Forwarded-For")
	if len(values) != 1 {
		return host
	}
	entries := strings.Split(values[0], ",")
	for i := len(entries) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(entries[i])
		if net.ParseIP(candidate) == nil {
			return host
		}
		if !h.trusted(candidate) {
			return candidate
		}
	}
	return host
}

func baseMediaType(value string) string {
	if before, _, ok := strings.Cut(value, ";"); ok {
		value = before
	}
	return strings.TrimSpace(value)
}

func acceptsMCP(values []string) bool {
	jsonOK, streamOK := false, false
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			switch baseMediaType(part) {
			case "application/json", "*/*":
				jsonOK = true
			case "text/event-stream":
				streamOK = true
			}
		}
	}
	return jsonOK && streamOK
}

type capturedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
	wrote  bool
}

func newCapturedResponse() *capturedResponse {
	return &capturedResponse{header: make(http.Header), status: http.StatusOK}
}
func (r *capturedResponse) Header() http.Header { return r.header }
func (r *capturedResponse) WriteHeader(status int) {
	if !r.wrote {
		r.status = status
		r.wrote = true
	}
}
func (r *capturedResponse) Write(p []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	return r.body.Write(p)
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		dst[key] = slices.Clone(values)
	}
}
