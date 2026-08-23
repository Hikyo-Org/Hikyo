// Package tlspolicy owns Hikyo's inbound certificate loading policy.
package tlspolicy

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"os"
	"time"
)

// LoadCertificate reads and validates a server certificate pair. The same
// function is used at config load and at runtime reload so replacement files
// cannot enter service under weaker rules than the initial pair.
func LoadCertificate(certPath, keyPath string, now time.Time) (*tls.Certificate, *x509.Certificate, error) {
	info, err := os.Stat(keyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("TLS key file: %w", err)
	}
	if perm := info.Mode().Perm(); perm != 0o400 && perm != 0o600 {
		return nil, nil, fmt.Errorf("TLS key file %s is mode %04o; want 0400 or 0600", keyPath, perm)
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, fmt.Errorf("TLS certificate file: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("TLS key file: %w", err)
	}
	return ParseCertificatePEM(certPEM, keyPEM, now)
}

// ParseCertificatePEM validates an in-memory pair. File permission policy is
// intentionally owned by LoadCertificate, after a caller chooses the file.
func ParseCertificatePEM(certPEM, keyPEM []byte, now time.Time) (*tls.Certificate, *x509.Certificate, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("TLS certificate pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, nil, fmt.Errorf("TLS certificate pair contains no leaf certificate")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, nil, fmt.Errorf("TLS leaf certificate: %w", err)
	}
	if now.Before(leaf.NotBefore) {
		return nil, nil, fmt.Errorf("TLS leaf certificate is not valid before %s", leaf.NotBefore.UTC().Format(time.RFC3339))
	}
	if !leaf.NotAfter.After(now) {
		return nil, nil, fmt.Errorf("TLS leaf certificate expired at %s", leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	pair.Leaf = leaf
	return &pair, leaf, nil
}

// SPKIFingerprint is base64(sha256(SubjectPublicKeyInfo)).
func SPKIFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(sum[:])
}
