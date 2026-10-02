package vaultkv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func assertForgotten(t *testing.T, client *Client) {
	t.Helper()
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.token != "" || client.loggedIn || client.credential.Token != "" || client.credential.RoleID != "" || client.credential.SecretID != "" {
		t.Fatal("cleanup retained secret material")
	}
}

func cleanupServer(t *testing.T, revoke func(http.ResponseWriter, *http.Request)) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/approle/login":
			_, _ = io.WriteString(w, `{"auth":{"client_token":"hvs.cleanup","lease_duration":3600,"renewable":false}}`)
		case "/v1/auth/token/revoke-self":
			revoke(w, r)
		default:
			_, _ = io.WriteString(w, `{"data":{"current_version":0,"custom_metadata":null,"versions":{}}}`)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestForgetContextNeverRevivesCanceledRevokeOrRevokesStaticToken(t *testing.T) {
	for _, tc := range []struct {
		name, descriptor string
		canceled         bool
	}{
		{"minted canceled", `{"method":"approle","role_id":"role","secret_id":"secret-id"}`, true},
		{"static live", `{"method":"token","token":"static-token"}`, false},
		{"static canceled", `{"method":"token","token":"static-token"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var revocations atomic.Int32
			server := cleanupServer(t, func(w http.ResponseWriter, _ *http.Request) {
				revocations.Add(1)
				w.WriteHeader(http.StatusNoContent)
			})
			client := pinnedClient(t, server, "", tc.descriptor)
			if _, err := client.ReadMetadata(t.Context(), "secret", "p"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			client.ForgetContext(ctx)
			assertForgotten(t, client)
			if revocations.Load() != 0 {
				t.Fatal("cleanup started forbidden revocation")
			}
		})
	}
}

func TestForgetContextCancelsActualSlowRevokeAtOriginatingDeadline(t *testing.T) {
	started := make(chan struct{}, 1)
	canceled := make(chan struct{}, 1)
	server := cleanupServer(t, func(_ http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
		canceled <- struct{}{}
	})
	client := pinnedClient(t, server, "", `{"method":"approle","role_id":"role","secret_id":"secret-id"}`)
	if _, err := client.ReadMetadata(t.Context(), "secret", "p"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	begin := time.Now()
	client.ForgetContext(ctx)
	if elapsed := time.Since(begin); elapsed > time.Second {
		t.Fatalf("cleanup escaped originating deadline: %v", elapsed)
	}
	assertForgotten(t, client)
	select {
	case <-started:
	default:
		t.Fatal("live cleanup did not attempt best-effort revoke")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("egress request did not cancel with cleanup context")
	}
}
