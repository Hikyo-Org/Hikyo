package isolation

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
)

// Run the same domain/security proofs through both wire profiles. Only wire
// framing changes; bearer, scope, cancellation and response assertions remain.
func mcpProfile(h http.Handler, codex bool) http.Handler {
	if !codex {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			r = r.Clone(r.Context())
			r.URL.Path = mcpserver.CodexPath
			h.ServeHTTP(w, r)
			return
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			panic(err)
		}
		var params map[string]json.RawMessage
		if err := json.Unmarshal(body["params"], &params); err != nil {
			panic(err)
		}
		delete(params, "_meta")
		body["params"], _ = json.Marshal(params)
		raw, _ := json.Marshal(body)
		r = r.Clone(r.Context())
		r.URL.Path = mcpserver.CodexPath
		r.Header.Del("Mcp-Method")
		r.Header.Del("Mcp-Name")
		r.Header.Set("Mcp-Protocol-Version", mcpserver.CodexProtocolVersion)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		h.ServeHTTP(w, r)
	})
}
