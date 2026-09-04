// mcp-conformance-server exposes Hikyo's production MCP transport with an
// empty closed registry. The upstream suite can verify protocol behavior
// without needing a tenant, bearer, or secret fixture.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
)

func main() {
	addr := os.Getenv("HIKYO_MCP_CONFORMANCE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:18080"
	}
	registry := mcpserver.NewRegistry()
	if err := mcpserver.RegisterConformanceDiagnostics(registry); err != nil {
		log.Fatal(err)
	}
	handler, err := mcpserver.New(mcpserver.Options{
		Registry:       registry,
		ExternalOrigin: "http://" + addr,
		Admission:      allowDiscovery{},
		Version:        "conformance",
		CursorSealer:   conformanceSealer{},
	})
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: addr, Handler: authorizeDiagnostics(handler), ReadHeaderTimeout: mcpserver.ToolExecutionTimeout}
	log.Printf("MCP conformance fixture listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

type allowDiscovery struct{}

func (allowDiscovery) AllowDiscovery(string) bool { return true }

// The upstream diagnostic calls carry no deployment credential. Add a fixture-
// only bearer to tool calls; the registered diagnostics do not resolve it or
// touch a tenant service.
func authorizeDiagnostics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mcp-Method") == "tools/call" {
			r.Header.Set("Authorization", "Bearer conformance-fixture")
		}
		next.ServeHTTP(w, r)
	})
}

type conformanceSealer struct{}

func (conformanceSealer) Seal(context.Context, []byte) (string, error) {
	return "", errors.New("conformance diagnostics do not paginate")
}

func (conformanceSealer) Open(context.Context, string) ([]byte, error) {
	return nil, errors.New("conformance diagnostics do not paginate")
}
