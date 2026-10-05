package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type emptyOIDCDirectory struct {
	stubProviders
	rows []service.ProviderView
}

func (s emptyOIDCDirectory) List(context.Context, service.Actor) ([]service.ProviderView, error) {
	return s.rows, nil
}

func TestEmptyOIDCProviderDirectoryEncodesArray(t *testing.T) {
	for _, rows := range [][]service.ProviderView{nil, {}} {
		srv := httptest.NewServer(server.New(&server.API{
			Auth: stubAuth{}, Providers: emptyOIDCDirectory{rows: rows},
		}, nil))
		response, body := call(t, srv, http.MethodGet, api.PathPrefix+"/instance/oidc-providers", "live", nil)
		srv.Close()
		if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"providers":[]`) {
			t.Fatalf("empty OIDC directory status=%d body=%s, want 200 with providers:[]", response.StatusCode, body)
		}
	}
}
