package isolation

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"net/http"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
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
		var body map[string]jsontext.Value
		if err := json.UnmarshalRead(r.Body, &body); err != nil {
			panic(err)
		}
		var params map[string]jsontext.Value
		if err := json.Unmarshal(body["params"], &params); err != nil {
			panic(err)
		}
		delete(params, "_meta")
		body["params"], _ = json.Marshal(params, jsonwire.Stable)
		raw, _ := json.Marshal(body, jsonwire.Stable)
		r = r.Clone(r.Context())
		r.URL.Path = mcpserver.CodexPath
		r.Header.Del("Mcp-Method")
		r.Header.Del("Mcp-Name")
		r.Header.Set("Mcp-Protocol-Version", mcpserver.CodexProtocolVersion)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		h.ServeHTTP(w, r)
	})
}
