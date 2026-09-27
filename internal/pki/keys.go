// Package pki is the private-PKI core (#154, docs/adr/pki.md): closed key
// algorithms, decidable certificate-profile policy, and the X.509 signing and
// CRL primitives. It uses only the standard library's crypto/x509 (no
// hand-rolled primitive, human-auth ADR) and holds no state: custody, storage
// and authorization live in the service layer, which hands this package a
// crypto.Signer for exactly one signature at a time.
package pki

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"slices"
)

// KeyAlgorithm is the closed key-algorithm enum. A row or request naming
// anything else is refused by ParseKeyAlgorithm (fail-closed).
type KeyAlgorithm string

const (
	ECDSAP256 KeyAlgorithm = "ecdsa-p256"
	ECDSAP384 KeyAlgorithm = "ecdsa-p384"
	Ed25519   KeyAlgorithm = "ed25519"
	RSA2048   KeyAlgorithm = "rsa-2048"
	RSA3072   KeyAlgorithm = "rsa-3072"
	RSA4096   KeyAlgorithm = "rsa-4096"
)

// LeafKeyAlgorithms is the closed set a profile may admit for leaves.
var LeafKeyAlgorithms = []KeyAlgorithm{ECDSAP256, ECDSAP384, Ed25519, RSA2048, RSA3072, RSA4096}

// CAKeyAlgorithms is the narrower set a CA key may use: Ed25519 CA
// certificates have poor relying-party support and RSA-2048 is below the
// strength a multi-year CA key needs.
var CAKeyAlgorithms = []KeyAlgorithm{ECDSAP256, ECDSAP384, RSA3072, RSA4096}

var (
	// ErrUnknownAlgorithm refuses a key algorithm outside the closed set.
	ErrUnknownAlgorithm = errors.New("pki: unknown key algorithm")
	// ErrUnsupportedKey refuses a public key whose algorithm or size is not
	// one of the closed set (for example RSA-1024 or P-521).
	ErrUnsupportedKey = errors.New("pki: unsupported public key")
)

// ParseKeyAlgorithm rejects an unknown algorithm rather than defaulting one.
func ParseKeyAlgorithm(s string) (KeyAlgorithm, error) {
	algorithm := KeyAlgorithm(s)
	if slices.Contains(LeafKeyAlgorithms, algorithm) {
		return algorithm, nil
	}
	return "", fmt.Errorf("%w %q", ErrUnknownAlgorithm, s)
}

// GenerateKey creates a fresh private key of the given algorithm. Unknown
// algorithms wrap ErrUnknownAlgorithm; key-generation errors are propagated.
func GenerateKey(algorithm KeyAlgorithm) (crypto.Signer, error) {
	switch algorithm {
	case ECDSAP256:
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case ECDSAP384:
		return ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	case Ed25519:
		_, key, err := ed25519.GenerateKey(rand.Reader)
		return key, err
	case RSA2048:
		return rsa.GenerateKey(rand.Reader, 2048)
	case RSA3072:
		return rsa.GenerateKey(rand.Reader, 3072)
	case RSA4096:
		return rsa.GenerateKey(rand.Reader, 4096)
	}
	return nil, fmt.Errorf("%w %q", ErrUnknownAlgorithm, algorithm)
}

// AlgorithmOf classifies a public key into the closed enum, refusing any key
// outside it.
func AlgorithmOf(public crypto.PublicKey) (KeyAlgorithm, error) {
	switch key := public.(type) {
	case *ecdsa.PublicKey:
		switch key.Curve {
		case elliptic.P256():
			return ECDSAP256, nil
		case elliptic.P384():
			return ECDSAP384, nil
		}
	case ed25519.PublicKey:
		if len(key) == ed25519.PublicKeySize {
			return Ed25519, nil
		}
	case *rsa.PublicKey:
		switch key.N.BitLen() {
		case 2048:
			return RSA2048, nil
		case 3072:
			return RSA3072, nil
		case 4096:
			return RSA4096, nil
		}
	}
	return "", ErrUnsupportedKey
}

// KeyFingerprint is the product's one SPKI fingerprint spelling for
// lifecycle views (the SAML SP key precedent):
// "sha256:" + base64url(sha256(SubjectPublicKeyInfo)).
func KeyFingerprint(public crypto.PublicKey) (string, error) {
	spki, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(spki)
	return "sha256:" + base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// CertificateFingerprint fingerprints a whole certificate (DER), in the same
// spelling as KeyFingerprint.
func CertificateFingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return "sha256:" + base64.RawURLEncoding.EncodeToString(sum[:])
}

// PublicKeysEqual reports whether two public keys are the same key.
func PublicKeysEqual(a, b crypto.PublicKey) bool {
	type equaler interface{ Equal(crypto.PublicKey) bool }
	left, ok := a.(equaler)
	return ok && left.Equal(b)
}

// NewSerial returns a random serial number in [1, 2^127), propagating errors
// from the random source (RFC 5280 §4.1.2.2 allows up to 20 octets; CA/B
// guidance asks for at least 64 bits of entropy).
func NewSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 127)
	for {
		serial, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return nil, err
		}
		if serial.Sign() > 0 {
			return serial, nil
		}
	}
}

// SerialHex is the canonical lowercase hex spelling of a serial number.
func SerialHex(serial *big.Int) string { return fmt.Sprintf("%x", serial) }

// ParseSerialHex parses SerialHex output.
func ParseSerialHex(s string) (*big.Int, error) {
	serial, ok := new(big.Int).SetString(s, 16)
	if !ok || serial.Sign() <= 0 {
		return nil, fmt.Errorf("pki: invalid serial %q", s)
	}
	return serial, nil
}
