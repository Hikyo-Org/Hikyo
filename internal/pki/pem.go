package pki

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// CertificatePEM encodes one DER certificate.
func CertificatePEM(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// CertificatesPEM encodes a DER chain, in order.
func CertificatesPEM(ders [][]byte) string {
	var b strings.Builder
	for _, der := range ders {
		b.WriteString(CertificatePEM(der))
	}
	return b.String()
}

// CSRPEM encodes a DER CSR.
func CSRPEM(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

// CRLPEM encodes a DER CRL.
func CRLPEM(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: der}))
}

// PrivateKeyPEM encodes a PKCS#8 private key. The caller owns zeroing the
// returned buffer; it is the display-once material of a generated-key
// issuance and never persisted.
func PrivateKeyPEM(pkcs8 []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
}

// ParseCertificates reads every CERTIFICATE block from PEM input, in order,
// refusing any other block type (a stray private key in a chain file is an
// operator mistake to surface, not to ignore).
func ParseCertificates(input []byte) ([]*x509.Certificate, [][]byte, error) {
	var certs []*x509.Certificate
	var ders [][]byte
	rest := input
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			return nil, nil, fmt.Errorf("pki: unexpected PEM block %q in a certificate list", block.Type)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("pki: %w", err)
		}
		certs = append(certs, cert)
		ders = append(ders, block.Bytes)
	}
	if strings.TrimSpace(string(rest)) != "" {
		return nil, nil, errors.New("pki: trailing data after the last PEM block")
	}
	return certs, ders, nil
}

// ParsePrivateKey reads one PKCS#8, PKCS#1 (RSA) or SEC 1 (EC) private key
// from PEM protected input. The returned key is the caller's to seal and drop.
func ParsePrivateKey(input []byte) (crypto.Signer, error) {
	block, rest := pem.Decode(input)
	if block == nil {
		return nil, errors.New("pki: no PEM private key found")
	}
	if strings.TrimSpace(string(rest)) != "" {
		return nil, errors.New("pki: the key input must hold exactly one PEM block")
	}
	var key any
	var err error
	switch block.Type {
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("pki: unsupported private key block %q (encrypted keys are not accepted)", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("pki: %w", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, errors.New("pki: the key cannot sign")
	}
	if _, err := AlgorithmOf(signer.Public()); err != nil {
		return nil, err
	}
	return signer, nil
}

// MarshalPrivateKey is PKCS#8 marshaling; the caller zeroes the result.
func MarshalPrivateKey(key crypto.Signer) ([]byte, error) {
	return x509.MarshalPKCS8PrivateKey(key)
}

// UnmarshalPrivateKey opens a sealed-then-unsealed PKCS#8 key.
func UnmarshalPrivateKey(pkcs8 []byte) (crypto.Signer, error) {
	key, err := x509.ParsePKCS8PrivateKey(pkcs8)
	if err != nil {
		return nil, err
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, errors.New("pki: stored key cannot sign")
	}
	return signer, nil
}
