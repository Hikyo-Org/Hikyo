package federationhttp

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

type dialFunc func(context.Context, string, string) (net.Conn, error)

func (f dialFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}

func clientForServer(t *testing.T, server *httptest.Server, policy Policy, cap int64) (*http.Client, *transport) {
	t.Helper()
	c, err := NewClient(policy, cap)
	if err != nil {
		t.Fatal(err)
	}
	rt := c.Transport.(*transport)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	rt.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	return c, rt
}

func TestCanonicalURLsAndExplicitDevelopment(t *testing.T) {
	for _, raw := range []string{"http://example.com", "http://127.0.0.1", "https://user:secret@example.com", "https://EXAMPLE.com", "https://example.com.", "https://example.com:", "https://example.com:0443", "https://example.com:65536", "https://example.com/#fragment", "https://[fe80::1%25eth0]", "https://bad_host"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ValidateURL(raw, false); err == nil {
				t.Fatal("accepted noncanonical URL")
			}
		})
	}
	for _, raw := range []string{"http://localhost", "http://127.0.0.1.example.com", "http://10.0.0.1"} {
		if _, err := ValidateURL(raw, true); err == nil {
			t.Fatal("development allowed nonliteral/nonloopback HTTP")
		}
	}
	for _, raw := range []string{"https://example.com/tenant", "http://127.0.0.1:1234", "http://[::1]:1234"} {
		if _, err := ValidateURL(raw, true); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPrivateNetworkRequiresExactOriginPolicy(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer server.Close()
	for _, tc := range []struct {
		name, origin string
		allowed      bool
	}{
		{"default", "", false}, {"different origin", "https://other.example", false}, {"exact origin", server.URL, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := Policy{AllowedCIDRs: map[string][]netip.Prefix{}}
			if tc.origin != "" {
				policy.AllowedCIDRs[tc.origin] = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
			}
			client, _ := clientForServer(t, server, policy, 100)
			response, err := client.Get(server.URL)
			if response != nil {
				response.Body.Close()
			}
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
		})
	}
}

func TestDNSPinningRebindingAndMixedAnswers(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer server.Close()
	client, rt := clientForServer(t, server, Policy{}, 100)
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	var queries, dials atomic.Int32
	rt.resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		if queries.Add(1) == 1 {
			return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	rt.dialer = dialFunc(func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		if address != net.JoinHostPort("1.1.1.1", port) {
			t.Errorf("dial was not pinned: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	})
	url := "https://example.com:" + port
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, err := client.Get(url); err == nil {
		t.Fatal("DNS rebinding was accepted")
	}
	if queries.Load() != 2 || dials.Load() != 1 {
		t.Fatal("request reused connection or dialed unapproved address")
	}
	rt.resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("169.254.169.254")}, nil
	})
	if _, err := client.Get(url); err == nil || dials.Load() != 1 {
		t.Fatal("mixed public/private DNS answer was dialed")
	}
}

func TestRedirectAndEnvironmentProxyAreNeverFollowed(t *testing.T) {
	var pivotCalls atomic.Int32
	pivot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { pivotCalls.Add(1) }))
	defer pivot.Close()
	t.Setenv("HTTPS_PROXY", pivot.URL)
	t.Setenv("HTTP_PROXY", pivot.URL)
	t.Setenv("ALL_PROXY", pivot.URL)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, pivot.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, _ := clientForServer(t, server, Policy{AllowedCIDRs: map[string][]netip.Prefix{server.URL: {netip.MustParsePrefix("127.0.0.1/32")}}}, 100)
	response, err := client.Post(server.URL, "application/x-www-form-urlencoded", strings.NewReader("client_secret=synthetic-secret"))
	if response != nil {
		response.Body.Close()
	}
	if err == nil || pivotCalls.Load() != 0 {
		t.Fatal("redirect or proxy received the token request")
	}
}

func TestResponseCeilingsIncludeTrailingAndCompressedBytes(t *testing.T) {
	for _, mode := range []string{"content-length", "chunked", "gzip"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				payload := "{}" + strings.Repeat(" ", 127)
				if mode == "gzip" {
					w.Header().Set("Content-Encoding", "gzip")
					g := gzip.NewWriter(w)
					_, _ = io.WriteString(g, payload)
					_ = g.Close()
					return
				}
				if mode == "chunked" {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				_, _ = io.WriteString(w, payload)
			}))
			defer server.Close()
			client, _ := clientForServer(t, server, Policy{Development: true}, 128)
			if _, err := client.Get(server.URL); err == nil {
				t.Fatal("oversized body accepted")
			}
		})
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, strings.Repeat("a", 128)) }))
	defer server.Close()
	client, _ := clientForServer(t, server, Policy{Development: true}, 128)
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil || len(payload) != 128 {
		t.Fatal("exact cap was not accepted")
	}
}

func TestDeadlineClosesSlowHeaderAndBodyRequests(t *testing.T) {
	for _, body := range []bool{false, true} {
		t.Run(map[bool]string{false: "headers", true: "body"}[body], func(t *testing.T) {
			released := make(chan struct{})
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if body {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
				close(released)
			}))
			defer server.Close()
			client, rt := clientForServer(t, server, Policy{Development: true}, 128)
			rt.deadline = 100 * time.Millisecond
			start := time.Now()
			if _, err := client.Get(server.URL); err == nil {
				t.Fatal("stalled request succeeded")
			}
			if time.Since(start) > time.Second {
				t.Fatal("deadline was not bounded")
			}
			select {
			case <-released:
			case <-time.After(time.Second):
				t.Fatal("timeout retained upstream request")
			}
		})
	}
}

// A close-delimited response reaches EOF only when the upstream connection
// closes. Cancel synchronously before returning that EOF to the real HTTP body
// reader, so cancellation is ordered before the completed body is published.
type cancelOnEOFConn struct {
	net.Conn
	cancel context.CancelFunc
}

func (c *cancelOnEOFConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if errors.Is(err, io.EOF) {
		c.cancel()
	}
	return n, err
}

func TestCancellationAtBodyEOFIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, err = rw.WriteString("HTTP/1.0 200 OK\r\nConnection: close\r\n\r\n{}")
		if err != nil {
			t.Error(err)
			return
		}
		if err := rw.Flush(); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client, err := NewClient(Policy{Development: true}, 128)
	if err != nil {
		t.Fatal(err)
	}
	rt := client.Transport.(*transport)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rt.dialer = dialFunc(func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := new(net.Dialer).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &cancelOnEOFConn{Conn: conn, cancel: cancel}, nil
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := rt.RoundTrip(request)
	if response != nil {
		response.Body.Close()
	}
	if ctx.Err() == nil {
		t.Fatal("body EOF boundary did not cancel request")
	}
	if !errors.Is(err, ErrTransport) || response != nil {
		t.Fatalf("canceled request with clean body EOF: response=%v err=%v, want nil response and ErrTransport", response, err)
	}
}
