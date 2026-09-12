package federationhttp

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
)

// MaxCABundleBytes bounds administrator-supplied trust anchors.
const MaxCABundleBytes = 64 << 10

// ParseCABundle accepts only PEM CA certificates. Supplied roots replace the
// system root store; private keys, junk and partial bundles fail closed.
func ParseCABundle(bundle string) (*x509.CertPool, error) {
	invalid := errors.New("federation: CA bundle must contain only PEM CA certificates (maximum 64 KiB)")
	if len(bundle) == 0 || len(bundle) > MaxCABundleBytes {
		return nil, invalid
	}
	rest := bytes.TrimSpace([]byte(bundle))
	roots := x509.NewCertPool()
	count := 0
	for len(rest) > 0 {
		if !bytes.HasPrefix(rest, []byte("-----BEGIN CERTIFICATE-----")) {
			return nil, invalid
		}
		endMarker := []byte("-----END CERTIFICATE-----")
		end := bytes.Index(rest, endMarker)
		if end < 0 {
			return nil, invalid
		}
		end += len(endMarker)
		block, trailing := pem.Decode(rest[:end])
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(bytes.TrimSpace(trailing)) != 0 {
			return nil, invalid
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !cert.IsCA || !cert.BasicConstraintsValid {
			return nil, invalid
		}
		roots.AddCert(cert)
		count++
		rest = bytes.TrimSpace(rest[end:])
	}
	if count == 0 {
		return nil, invalid
	}
	return roots, nil
}

// CloneWithRootCAs retains every egress and response bound while replacing only
// this copy's roots. The original transport and caller's pool stay untouched.
func (t *transport) CloneWithRootCAs(roots *x509.CertPool) http.RoundTripper {
	out := *t
	out.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots.Clone()}
	return &out
}
