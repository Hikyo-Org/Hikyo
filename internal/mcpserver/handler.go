package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Hikyo-Org/hikyo/internal/audit"
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
}

type handler struct {
	sdk            http.Handler
	externalScheme string
	externalHost   string
	allowedOrigins []string
	trustedProxies []*net.IPNet
	admission      discoveryAdmission
	slots          chan struct{}
}

type bearerContextKey struct{}
type callStateContextKey struct{}

type callState struct {
	rateLimited atomic.Bool
}

func markRateLimited(ctx context.Context) {
	if state, ok := ctx.Value(callStateContextKey{}).(*callState); ok {
		state.rateLimited.Store(true)
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
	server := mcp.NewServer(&mcp.Implementation{
		Name: "hikyo", Title: "Hikyo", Description: "Read-only Hikyo configuration tools.", Version: options.Version,
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
		sdk: sdk, externalScheme: origin.Scheme, externalHost: origin.Host,
		allowedOrigins: slices.Clone(options.AllowedOrigins),
		trustedProxies: slices.Clone(options.TrustedProxies),
		admission:      options.Admission, slots: make(chan struct{}, concurrency),
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
	if len(headerVersions) != 1 || requestedVersion == "" || headerVersions[0] != requestedVersion {
		writeRPCError(w, http.StatusBadRequest, envelope.ID, -32020, "protocol mirror headers do not match request", nil)
		return
	}
	methodHeaders := r.Header.Values("Mcp-Method")
	nameHeaders := r.Header.Values("Mcp-Name")
	if len(methodHeaders) != 1 || methodHeaders[0] != envelope.Method ||
		(envelope.Method == "tools/call" && (len(nameHeaders) != 1 || nameHeaders[0] != requestedName)) ||
		(envelope.Method != "tools/call" && len(nameHeaders) != 0) {
		writeRPCError(w, http.StatusBadRequest, envelope.ID, -32020, "protocol mirror headers do not match request", nil)
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
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, ToolExecutionTimeout)
	defer cancel()
	state := &callState{}
	ctx = context.WithValue(ctx, callStateContextKey{}, state)
	ctx = context.WithValue(ctx, bearerContextKey{}, newBearer(bearer))
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
	if state != nil && state.rateLimited.Load() {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
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
