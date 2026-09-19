package mcpserver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
)

// CodexPath is an explicit stateless compatibility boundary. The modern /mcp
// endpoint retains its independently validated 2026-07-28 wire contract.
const CodexPath = "/mcp/codex"
const CodexProtocolVersion = "2025-06-18"

func encodeCodexName(name string) string {
	return headerSentinelPrefix + base64.StdEncoding.EncodeToString([]byte(name)) + headerSentinelSuffix
}

func (h *handler) serveCodex(w http.ResponseWriter, r *http.Request) {
	if !h.validHost(r) || !h.validOrigin(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if baseMediaType(r.Header.Get("Content-Type")) != "application/json" {
		http.Error(w, "Unsupported Media Type", http.StatusUnsupportedMediaType)
		return
	}
	if !acceptsMCP(r.Header.Values("Accept")) {
		http.Error(w, "Not Acceptable", http.StatusNotAcceptable)
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
	var envelope rpcEnvelope
	if err := decodeOne(raw, &envelope); err != nil {
		writeRPCError(w, http.StatusBadRequest, nil, -32700, "expected one JSON-RPC object", nil)
		return
	}
	if envelope.JSONRPC != "2.0" || envelope.Method == "" || !codexRequestID(envelope.ID) {
		writeRPCError(w, http.StatusBadRequest, nil, -32600, "invalid JSON-RPC request", nil)
		return
	}
	if len(r.Header.Values("Mcp-Session-Id")) != 0 {
		writeRPCError(w, http.StatusBadRequest, envelope.ID, -32020, "stateless transport does not accept a session id", nil)
		return
	}
	versions := r.Header.Values("Mcp-Protocol-Version")
	if len(versions) > 1 || (len(versions) == 1 && versions[0] != CodexProtocolVersion) {
		codexVersionError(w, envelope.ID)
		return
	}
	params := map[string]json.RawMessage{}
	if len(envelope.Params) != 0 {
		if err := json.Unmarshal(envelope.Params, &params); err != nil || params == nil {
			writeRPCError(w, http.StatusBadRequest, envelope.ID, -32602, "params must be an object", nil)
			return
		}
	}
	switch envelope.Method {
	case "tools/list", "tools/call":
		// No authority comes from legacy client metadata. Every call traverses the
		// same bounded modern transport, operation contract, and service admission.
		if len(envelope.ID) == 0 {
			writeRPCError(w, http.StatusBadRequest, nil, -32600, "tool methods require a request id", nil)
			return
		}
		params["_meta"], _ = json.Marshal(map[string]any{
			"io.modelcontextprotocol/protocolVersion":    ProtocolVersion,
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
			"io.modelcontextprotocol/clientInfo":         map[string]string{"name": "hikyo-codex-profile", "version": "1"},
		})
		envelope.Params, _ = json.Marshal(params)
		translated, _ := json.Marshal(envelope)
		request := r.Clone(r.Context())
		request.URL.Path = Path
		request.Header.Set("Mcp-Protocol-Version", ProtocolVersion)
		request.Header.Set("Mcp-Method", envelope.Method)
		request.Header.Del("Mcp-Name")
		if envelope.Method == "tools/call" {
			var name string
			if err := json.Unmarshal(params["name"], &name); err != nil || name == "" {
				writeRPCError(w, http.StatusBadRequest, envelope.ID, -32602, "tool name is required", nil)
				return
			}
			// Encode even ordinary names so sentinel-like names cannot change meaning.
			request.Header.Set("Mcp-Name", encodeCodexName(name))
		}
		request.Body = io.NopCloser(bytes.NewReader(translated))
		request.ContentLength = int64(len(translated))
		h.ServeHTTP(w, request)
	case "initialize", "ping", "notifications/initialized", "notifications/cancelled":
		h.serveCodexLifecycle(w, r, envelope, params)
	default:
		writeRPCError(w, http.StatusNotFound, envelope.ID, -32601, "method not supported by /mcp/codex; use initialize, tools/list or tools/call", nil)
	}
}

func codexRequestID(id json.RawMessage) bool {
	if len(id) == 0 {
		return true
	}
	var value any
	if json.Unmarshal(id, &value) != nil {
		return false
	}
	switch value.(type) {
	case string, float64:
		return true
	}
	return false
}

func codexVersionError(w http.ResponseWriter, id json.RawMessage) {
	writeRPCError(w, http.StatusBadRequest, id, -32022,
		"unsupported protocol; /mcp/codex supports 2025-06-18; use /mcp for 2026-07-28",
		map[string]any{"supported": []string{CodexProtocolVersion}})
}

func (h *handler) serveCodexLifecycle(w http.ResponseWriter, r *http.Request, envelope rpcEnvelope, params map[string]json.RawMessage) {
	if h.admission != nil && !h.admission.AllowDiscovery(h.sourceIP(r)) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	notification := envelope.Method == "notifications/initialized" || envelope.Method == "notifications/cancelled"
	if notification != (len(envelope.ID) == 0) {
		writeRPCError(w, http.StatusBadRequest, envelope.ID, -32600, "invalid lifecycle request id", nil)
		return
	}
	if notification {
		// Stateless, synchronous calls have no session or detached work to cancel.
		// HTTP request cancellation still propagates to service transactions.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	result := map[string]any{}
	if envelope.Method == "initialize" {
		var version string
		var info struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		var capabilities map[string]json.RawMessage
		if json.Unmarshal(params["protocolVersion"], &version) != nil || version == "" ||
			json.Unmarshal(params["clientInfo"], &info) != nil || info.Name == "" || info.Version == "" ||
			json.Unmarshal(params["capabilities"], &capabilities) != nil || capabilities == nil {
			writeRPCError(w, http.StatusBadRequest, envelope.ID, -32602, "initialize requires protocolVersion, clientInfo and capabilities", nil)
			return
		}
		if version != CodexProtocolVersion {
			codexVersionError(w, envelope.ID)
			return
		}
		result = map[string]any{
			"protocolVersion": CodexProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]string{"name": "hikyo", "version": h.version},
		}
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": envelope.ID, "result": result})
	if len(body) > MaxStaticResponseBytes {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}
