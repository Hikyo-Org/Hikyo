package oidcrp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/federationhttp"
	"github.com/Hikyo-Org/hikyo/internal/oidctest"
	"github.com/coreos/go-oidc/v3/oidc"
)

func TestDiscoveryRefusesImplicitLoopbackHTTP(t *testing.T) {
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer": issuer, "authorization_endpoint": issuer + "/authorize",
			"token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys",
		})
	}))
	defer server.Close()
	issuer = server.URL
	if _, err := Discover(context.Background(), issuer); !errors.Is(err, ErrDiscovery) {
		t.Fatalf("implicit loopback HTTP discovery must fail closed; got %v", err)
	}
}

func TestDiscoveryRejectsUnsafeDiscoveredEndpoints(t *testing.T) {
	for _, field := range []string{"authorization_endpoint", "token_endpoint", "jwks_uri"} {
		t.Run(field, func(t *testing.T) {
			var issuer string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				document := map[string]string{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys"}
				document[field] = "http://169.254.169.254/private"
				_ = json.NewEncoder(w).Encode(document)
			}))
			defer server.Close()
			issuer = server.URL
			if _, err := DiscoverWithPolicy(context.Background(), issuer, federationhttp.Policy{Development: true}); !errors.Is(err, ErrDiscovery) {
				t.Fatal("discovery accepted an endpoint outside explicit development policy")
			}
		})
	}
}

type forbiddenClient struct{ calls atomic.Int32 }

func (c *forbiddenClient) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return nil, errors.New("ambient client must never run")
}

func TestAllOIDCLegsUseBoundedClient(t *testing.T) {
	for _, mode := range []string{"healthy", "discovery-large", "token-large", "jwks-large", "discovery-error", "token-error", "jwks-stall"} {
		t.Run(mode, func(t *testing.T) {
			idp, err := oidctest.New()
			if err != nil {
				t.Fatal(err)
			}
			defer idp.Close()
			keysResponse, err := idp.Client().Get(idp.Issuer() + "/jwks")
			if err != nil {
				t.Fatal(err)
			}
			keys, err := io.ReadAll(keysResponse.Body)
			keysResponse.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			var issuer, rawToken string
			var discoveryHits, tokenHits, keyHits atomic.Int32
			released := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					discoveryHits.Add(1)
					if mode == "discovery-error" {
						w.WriteHeader(500)
						_, _ = io.WriteString(w, "synthetic-response-secret")
						return
					}
					if mode == "discovery-large" {
						_, _ = io.WriteString(w, strings.Repeat(" ", federationhttp.DocumentBytes+1))
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
				case "/token":
					tokenHits.Add(1)
					if mode == "token-error" {
						w.WriteHeader(400)
						_, _ = io.WriteString(w, `{"error":"synthetic-response-secret"}`)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "synthetic-access", "token_type": "Bearer", "id_token": rawToken})
					if mode == "token-large" {
						_, _ = io.WriteString(w, strings.Repeat(" ", federationhttp.TokenBytes))
					}
				case "/jwks":
					keyHits.Add(1)
					if mode == "jwks-stall" {
						w.WriteHeader(200)
						w.(http.Flusher).Flush()
						<-r.Context().Done()
						close(released)
						return
					}
					_, _ = w.Write(keys)
					if mode == "jwks-large" {
						_, _ = io.WriteString(w, strings.Repeat(" ", federationhttp.DocumentBytes))
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			issuer = server.URL
			now := time.Now()
			rawToken, err = idp.MintIDToken(map[string]any{"iss": issuer, "sub": "synthetic-user", "aud": "client", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
			if err != nil {
				t.Fatal(err)
			}
			ambient := &forbiddenClient{}
			ctx := oidc.ClientContext(context.Background(), &http.Client{Transport: ambient})
			provider, err := DiscoverWithPolicy(ctx, issuer, federationhttp.Policy{Development: true})
			if strings.HasPrefix(mode, "discovery-") {
				if !errors.Is(err, ErrDiscovery) || strings.Contains(err.Error(), "synthetic-response-secret") {
					t.Fatalf("discovery refusal was not closed: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			token, err := provider.Exchange(ctx, "client", []byte("synthetic-client-secret"), "https://hikyo.test/callback", "openid", "code", "verifier")
			if strings.HasPrefix(mode, "token-") {
				if !errors.Is(err, ErrExchange) || strings.Contains(err.Error(), "synthetic-response-secret") {
					t.Fatalf("token refusal was not closed: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "jwks-stall" {
				provider.client.Timeout = 100 * time.Millisecond
			}
			claims, err := provider.Verify(ctx, "client", token, func() time.Time { return now })
			if strings.HasPrefix(mode, "jwks-") {
				if !errors.Is(err, ErrTokenInvalid) {
					t.Fatalf("JWKS was not bounded: %v", err)
				}
				if mode == "jwks-stall" {
					select {
					case <-released:
					case <-time.After(time.Second):
						t.Fatal("JWKS retained request after independent client deadline")
					}
				}
			} else if err != nil || claims.Subject != "synthetic-user" {
				t.Fatalf("wire validation failed: %v", err)
			}
			if ambient.calls.Load() != 0 || discoveryHits.Load() != 1 || tokenHits.Load() != 1 || keyHits.Load() != 1 {
				t.Fatal("OIDC network legs did not use the pinned client")
			}
		})
	}
}
