// Package federationhttp owns bounded, direct federation HTTP transport. It
// shares the DNS/address policy used by SAML metadata and authenticated adapters.
package federationhttp

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/netpolicy"
)

const (
	Deadline      = 15 * time.Second
	DocumentBytes = 1 << 20
	TokenBytes    = 256 << 10
)

var ErrTransport = errors.New("federation: outbound request refused or failed")

// Policy comes from local operator configuration, never provider discovery or
// an HTTP request. CIDRs are scoped to each exact endpoint origin. Development
// permits HTTP only for literal loopback addresses, not DNS names.
type Policy struct {
	AllowedCIDRs map[string][]netip.Prefix
	Development  bool
}

// ValidateURL rejects noncanonical authorities and credentials. Paths remain
// byte-exact because OIDC issuer identity must never be normalized.
func ValidateURL(raw string, development bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Host == "" || u.Opaque != "" || u.User != nil ||
		u.Fragment != "" || u.RawFragment != "" || u.String() != raw {
		return nil, ErrTransport
	}
	host := u.Hostname()
	if host == "" || host != strings.ToLower(host) || strings.HasSuffix(host, ".") || strings.Contains(host, "%") {
		return nil, ErrTransport
	}
	ip, ipErr := netip.ParseAddr(host)
	if u.Scheme != "https" && !(development && u.Scheme == "http" && ipErr == nil && ip.IsLoopback()) {
		return nil, ErrTransport
	}
	if ipErr != nil {
		for _, label := range strings.Split(host, ".") {
			if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return nil, ErrTransport
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return nil, ErrTransport
				}
			}
		}
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
			return nil, ErrTransport
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return nil, ErrTransport
	}
	return u, nil
}

// NewClient copies policy so later caller mutation cannot widen the client.
// Environment proxies are intentionally disabled: their remote DNS behavior
// cannot be treated as the local, approved address dialed by PublicDialer.
func NewClient(policy Policy, maxBytes int64) (*http.Client, error) {
	if maxBytes <= 0 || maxBytes > DocumentBytes {
		return nil, ErrTransport
	}
	frozen := Policy{Development: policy.Development, AllowedCIDRs: make(map[string][]netip.Prefix)}
	for origin, cidrs := range policy.AllowedCIDRs {
		u, err := ValidateURL(origin, false)
		if err != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery {
			return nil, ErrTransport
		}
		for _, cidr := range cidrs {
			if !cidr.IsValid() {
				return nil, ErrTransport
			}
		}
		frozen.AllowedCIDRs[origin] = append([]netip.Prefix(nil), cidrs...)
	}
	transport := &transport{
		policy: frozen, maxBytes: maxBytes, resolver: net.DefaultResolver,
		dialer:   &net.Dialer{Timeout: 5 * time.Second, KeepAlive: -1},
		deadline: Deadline,
	}
	return &http.Client{
		Transport: transport, Timeout: Deadline,
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrTransport },
	}, nil
}

type transport struct {
	policy    Policy
	maxBytes  int64
	resolver  netpolicy.Resolver
	dialer    netpolicy.Dialer
	deadline  time.Duration
	tlsConfig *tls.Config
}

func (t *transport) RoundTrip(request *http.Request) (*http.Response, error) {
	u, err := ValidateURL(request.URL.String(), t.policy.Development)
	if err != nil {
		return nil, ErrTransport
	}
	allowed := append([]netip.Prefix(nil), t.policy.AllowedCIDRs[u.Scheme+"://"+u.Host]...)
	if t.policy.Development {
		if ip, err := netip.ParseAddr(u.Hostname()); err == nil && ip.IsLoopback() {
			allowed = append(allowed, netip.PrefixFrom(ip, ip.BitLen()))
		}
	}
	dialer, err := netpolicy.NewPublicDialer(allowed, t.resolver, t.dialer)
	if err != nil {
		return nil, ErrTransport
	}
	ctx, cancel := context.WithTimeout(request.Context(), t.deadline)
	defer cancel()
	base := &http.Transport{
		Proxy: nil, DialContext: dialer.DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
		MaxResponseHeaderBytes: 64 << 10, DisableKeepAlives: true,
	}
	if t.tlsConfig != nil {
		base.TLSClientConfig = t.tlsConfig.Clone()
	}
	defer base.CloseIdleConnections()
	// A fresh transport resolves every request and dials only the validated IP;
	// TLS still authenticates the original hostname. Redirects never issue a hop.
	response, err := base.RoundTrip(request.Clone(ctx))
	if err != nil {
		return nil, ErrTransport
	}
	defer response.Body.Close()
	if response.ContentLength > t.maxBytes {
		return nil, ErrTransport
	}
	// Read before handing the response to protocol libraries. A LimitReader
	// alone can hide trailing bytes after a valid JSON value; cap+1 rejects them.
	payload, err := io.ReadAll(io.LimitReader(response.Body, t.maxBytes+1))
	// Cancellation can race a clean EOF (for example when the peer finishes
	// its response after observing cancellation). A successful body read does
	// not prove the request is still within its deadline.
	if err != nil || ctx.Err() != nil || int64(len(payload)) > t.maxBytes {
		return nil, ErrTransport
	}
	response.Body = io.NopCloser(bytes.NewReader(payload))
	response.ContentLength = int64(len(payload))
	return response, nil
}
