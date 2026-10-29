package vaultkv

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

func TestNamespaceSupportIsProvedBeforeCredentialTransmission(t *testing.T) {
	var probeNames sync.Map
	for _, edition := range []string{"vault-oss", "vault-enterprise", "openbao", "unknown", "namespace-header-stripped"} {
		for _, method := range []string{"token", "approle"} {
			t.Run(edition+"/"+method, func(t *testing.T) {
				var credentials, writes, probes int
				stub := &vaultStub{t: t, handle: func(w http.ResponseWriter, r recordedRequest) {
					if r.Token != "" || r.Body["secret_id"] != nil {
						credentials++
					}
					switch {
					case r.Path == operationRegistry["namespace-probe"].Path:
						probes++
						if _, repeated := probeNames.LoadOrStore(r.Namespace, struct{}{}); repeated {
							t.Error("namespace probe reused a child namespace")
						}
						if r.Token != "" || r.Body != nil || !strings.HasPrefix(r.Namespace, "team/hikyo-probe-") {
							t.Error("namespace probe leaked credential or used configured namespace")
							w.WriteHeader(http.StatusInternalServerError)
							return
						}
						if edition == "openbao" || edition == "vault-enterprise" {
							w.WriteHeader(http.StatusNotFound)
							_, _ = io.WriteString(w, `{"errors":["namespace not found"]}`)
						} else {
							w.WriteHeader(http.StatusForbidden)
						}
					case r.Path == "/v1/auth/approle/login":
						_, _ = io.WriteString(w, `{"auth":{"client_token":"synthetic-minted","lease_duration":600,"renewable":true}}`)
					case r.Path == "/v1/auth/token/revoke-self":
						w.WriteHeader(http.StatusNoContent)
					default:
						if r.Namespace != "team" {
							t.Error("delivery left the configured namespace")
							w.WriteHeader(http.StatusInternalServerError)
							return
						}
						writes++
						_, _ = io.WriteString(w, `{"data":{"version":1}}`)
					}
				}}
				server := httptest.NewTLSServer(stub)
				t.Cleanup(server.Close)
				credential := "synthetic-private-token"
				if method == "approle" {
					credential = `{"method":"approle","role_id":"synthetic-role","secret_id":"synthetic-private-id"}`
				}
				client := pinnedClient(t, server, "team", credential)
				t.Cleanup(client.Forget)
				_, err := client.WriteCAS(t.Context(), "secret", "TOKEN", "synthetic-value", 0)
				supported := edition == "vault-enterprise" || edition == "openbao"
				if supported {
					if err != nil || writes != 1 || credentials == 0 {
						t.Fatalf("supported namespace delivery: err=%v writes=%d credentials=%d", err, writes, credentials)
					}
				} else if !errors.Is(err, adapter.ErrDestinationID) || writes != 0 || credentials != 0 || client.loggedIn {
					t.Fatalf("unsupported namespace escaped refusal: err=%v writes=%d credentials=%d", err, writes, credentials)
				}
				if probes != 1 {
					t.Fatalf("unauthenticated namespace proof count = %d", probes)
				}
				if err != nil && (strings.Contains(err.Error(), "synthetic-private") || strings.Contains(err.Error(), "synthetic-value")) {
					t.Fatal("namespace refusal exposed credential or value")
				}
			})
		}
	}
}
