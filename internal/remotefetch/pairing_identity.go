package remotefetch

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
)

// PairingSPKIPin observes the configured serving origin's TLS identity using
// normal CA and hostname verification. A currently loaded native certificate
// can be an additional trust anchor; the pin always comes from the observed
// public peer, which can present a different certificate at a TLS proxy.
// It sends no HTTP request to the origin.
// Call only after administrator authorization; construction performs no I/O.
func (c *Client) PairingSPKIPin(ctx context.Context, origin string, trustedNativeLeaf *x509.Certificate) (string, error) {
	if trustedNativeLeaf == nil {
		return c.pairingSPKIPin(ctx, origin, nil)
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		// A minimal host can still trust its explicitly configured certificate.
		roots = x509.NewCertPool()
	}
	roots.AddCert(trustedNativeLeaf)
	return c.pairingSPKIPin(ctx, origin, roots)
}

func (c *Client) pairingSPKIPin(ctx context.Context, origin string, roots *x509.CertPool) (string, error) {
	canonical, err := CanonicalRemoteURL(origin)
	if err != nil || canonical != origin {
		return "", errors.New("remotefetch: pairing identity requires a canonical HTTPS origin")
	}
	u, err := url.Parse(origin)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Deadline)
	defer cancel()
	port := u.Port()
	if port == "" {
		port = "443"
	}
	address := net.JoinHostPort(u.Hostname(), port)
	var conn net.Conn
	if c.cfg.Proxy != nil {
		conn, err = c.dialThroughProxy(ctx, address)
	} else {
		dials.Add(1)
		conn, err = (&net.Dialer{Timeout: c.cfg.Deadline}).DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return "", err
	}
	defer conn.Close()
	secure := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname(), RootCAs: roots})
	if err := secure.HandshakeContext(ctx); err != nil {
		return "", err
	}
	state := secure.ConnectionState()
	if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
		return "", errors.New("remotefetch: pairing TLS identity was not verified")
	}
	return SPKIFingerprint(state.PeerCertificates[0]), nil
}
