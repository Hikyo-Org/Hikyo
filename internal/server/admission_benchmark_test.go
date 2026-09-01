package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// BenchmarkAuthenticatedAPIRequests measures representative reads and writes
// through the same public router and middleware stack used by the server. It
// keeps OpenAPI request validation in the measured path because that runtime
// enforcement is the performance boundary this benchmark protects.
func BenchmarkAuthenticatedAPIRequests(b *testing.B) {
	org := service.Org{
		ID: testOrgID, Name: "acme", Active: true,
		Metadata: []byte(`{}`), CreatedAt: liveIdentity.CreatedAt,
	}
	handler := server.New(stubReady{}, &server.API{
		Auth: stubAuth{identity: liveIdentityFn},
		Orgs: stubOrgs{
			list: func(context.Context, service.Actor) ([]service.Org, error) {
				return []service.Org{org}, nil
			},
			create: func(context.Context, service.Actor, string, bool, json.RawMessage) (service.Org, error) {
				return org, nil
			},
		},
		Providers: stubProviders{}, Version: "benchmark",
		Projects: stubHierarchy{}, Environments: stubEnvs{}, Values: stubValues{}, Folders: stubFolders{},
	}, nil)

	for _, benchmark := range []struct {
		name       string
		method     string
		path       string
		body       []byte
		statusCode int
	}{
		{name: "GET", method: http.MethodGet, path: api.PathPrefix + "/orgs", statusCode: http.StatusOK},
		{name: "POST", method: http.MethodPost, path: api.PathPrefix + "/orgs", body: []byte(`{"name":"acme"}`), statusCode: http.StatusCreated},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				req := httptest.NewRequest(benchmark.method, benchmark.path, bytes.NewReader(benchmark.body))
				req.Header.Set("Authorization", "Bearer hik_1_cli_benchmark")
				if len(benchmark.body) > 0 {
					req.Header.Set("Content-Type", "application/json")
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				if response.Code != benchmark.statusCode {
					b.Fatalf("status = %d, want %d: %s", response.Code, benchmark.statusCode, response.Body)
				}
			}
		})
	}
}
