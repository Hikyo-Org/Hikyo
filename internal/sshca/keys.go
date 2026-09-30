// Package sshca is the OpenSSH user-certificate core (#155): the closed key
// algorithm set, CA key custody encoding, certificate construction under a
// profile's constraints, and the Key Revocation List encoder. It holds no
// state and performs no I/O; the service layer owns authorization, sealing,
// and persistence.
//
// Every cryptographic primitive comes from the standard library or
// golang.org/x/crypto/ssh. The KRL encoder (krl.go) writes only the OpenSSH
// container format (big-endian integers and length-prefixed strings); it
// implements no primitive.
package sshca

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/subtle"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// PublicKey is the OpenSSH public key type. Callers name it through this
// package so golang.org/x/crypto/ssh stays confined here (boundary test).
type PublicKey = ssh.PublicKey

// Algorithm is the closed key algorithm enum shared by CA keys and user keys.
// Anything else is refused by every parser and switch (fail-closed).
type Algorithm string

const (
	AlgorithmEd25519   Algorithm = "ed25519"
	AlgorithmECDSAP256 Algorithm = "ecdsa-p256"
	AlgorithmRSA3072   Algorithm = "rsa-3072"
)

// rsaMinBits is the smallest RSA modulus accepted for any key, CA or user.
const rsaMinBits = 3072

// Algorithms returns the closed set in its canonical order.
func Algorithms() []Algorithm {
	return []Algorithm{AlgorithmEd25519, AlgorithmECDSAP256, AlgorithmRSA3072}
}

// ErrUnsupportedKey refuses a key outside the closed algorithm set: DSA, other
// curves, short RSA, security-key types, or a certificate offered as a key.
var ErrUnsupportedKey = errors.New("sshca: unsupported key type")

// ErrEncryptedKey refuses a passphrase-protected import. The operator decrypts
// the key locally first; Hikyo never handles a CA passphrase.
var ErrEncryptedKey = errors.New("sshca: passphrase-protected keys are not accepted")

// ParseAlgorithm rejects an unknown algorithm name rather than defaulting one.
func ParseAlgorithm(s string) (Algorithm, error) {
	for _, a := range Algorithms() {
		if string(a) == s {
			return a, nil
		}
	}
	return "", fmt.Errorf("%w: algorithm %q", ErrUnsupportedKey, s)
}

// GenerateKey creates a fresh private key of the given algorithm.
func GenerateKey(alg Algorithm) (crypto.Signer, error) {
	switch alg {
	case AlgorithmEd25519:
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("sshca: generate ed25519: %w", err)
		}
		return priv, nil
	case AlgorithmECDSAP256:
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("sshca: generate ecdsa: %w", err)
		}
		return priv, nil
	case AlgorithmRSA3072:
		priv, err := rsa.GenerateKey(rand.Reader, rsaMinBits)
		if err != nil {
			return nil, fmt.Errorf("sshca: generate rsa: %w", err)
		}
		return priv, nil
	default:
		return nil, fmt.Errorf("%w: algorithm %q", ErrUnsupportedKey, alg)
	}
}

// algorithmOfPrivate classifies a parsed private key into the closed set.
func algorithmOfPrivate(key any) (crypto.Signer, Algorithm, error) {
	switch k := key.(type) {
	case ed25519.PrivateKey:
		return canonicalEd25519(k)
	case *ed25519.PrivateKey:
		if k == nil {
			return nil, "", ErrUnsupportedKey
		}
		return canonicalEd25519(*k)
	case *ecdsa.PrivateKey:
		if k.Curve != elliptic.P256() {
			return nil, "", fmt.Errorf("%w: only the P-256 curve is accepted", ErrUnsupportedKey)
		}
		return k, AlgorithmECDSAP256, nil
	case *rsa.PrivateKey:
		if k.N.BitLen() < rsaMinBits {
			return nil, "", fmt.Errorf("%w: RSA keys must be at least %d bits", ErrUnsupportedKey, rsaMinBits)
		}
		return k, AlgorithmRSA3072, nil
	default:
		return nil, "", ErrUnsupportedKey
	}
}

func canonicalEd25519(key ed25519.PrivateKey) (crypto.Signer, Algorithm, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, "", ErrUnsupportedKey
	}
	canonical := ed25519.NewKeyFromSeed(key.Seed())
	if subtle.ConstantTimeCompare(key, canonical) != 1 {
		return nil, "", fmt.Errorf("%w: Ed25519 public key does not match its private seed", ErrUnsupportedKey)
	}
	return canonical, AlgorithmEd25519, nil
}

// AlgorithmOf classifies an SSH public key into the closed set. Certificates,
// security-key types, DSA, other curves, and RSA under 3072 bits are refused.
func AlgorithmOf(pub ssh.PublicKey) (Algorithm, error) {
	if _, isCert := pub.(*ssh.Certificate); isCert {
		return "", fmt.Errorf("%w: a certificate is not a public key", ErrUnsupportedKey)
	}
	switch pub.Type() {
	case ssh.KeyAlgoED25519:
		return AlgorithmEd25519, nil
	case ssh.KeyAlgoECDSA256:
		return AlgorithmECDSAP256, nil
	case ssh.KeyAlgoRSA:
		cp, ok := pub.(ssh.CryptoPublicKey)
		if !ok {
			return "", ErrUnsupportedKey
		}
		rk, ok := cp.CryptoPublicKey().(*rsa.PublicKey)
		if !ok || rk.N.BitLen() < rsaMinBits {
			return "", fmt.Errorf("%w: RSA keys must be at least %d bits", ErrUnsupportedKey, rsaMinBits)
		}
		return AlgorithmRSA3072, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedKey, pub.Type())
	}
}

// maxPublicKeyLine bounds a supplied authorized_keys line. A 16k-bit RSA key
// is ~2.8 KiB base64; anything larger is not a key this service accepts.
const maxPublicKeyLine = 8 << 10

// ParsePublicKey parses one authorized_keys-format line (options and comment
// tolerated, ignored) and classifies it. Exactly one key is accepted.
func ParsePublicKey(line string) (ssh.PublicKey, Algorithm, error) {
	if len(line) == 0 || len(line) > maxPublicKeyLine {
		return nil, "", fmt.Errorf("%w: public key must be one authorized_keys line", ErrUnsupportedKey)
	}
	pub, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrUnsupportedKey, err)
	}
	if strings.TrimSpace(string(rest)) != "" {
		return nil, "", fmt.Errorf("%w: exactly one public key is accepted", ErrUnsupportedKey)
	}
	alg, err := AlgorithmOf(pub)
	if err != nil {
		return nil, "", err
	}
	return pub, alg, nil
}

// maxImportPEM bounds an imported CA private key.
const maxImportPEM = 16 << 10

// ParseImportedKey parses an operator-supplied CA private key (OpenSSH or
// PKCS#8/PKCS#1/SEC1 PEM). Passphrase-protected keys are refused.
func ParseImportedKey(pemBytes []byte) (crypto.Signer, Algorithm, error) {
	if len(pemBytes) == 0 || len(pemBytes) > maxImportPEM {
		return nil, "", fmt.Errorf("%w: private key must be a single PEM block", ErrUnsupportedKey)
	}
	raw, err := ssh.ParseRawPrivateKey(pemBytes)
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, "", ErrEncryptedKey
		}
		return nil, "", fmt.Errorf("%w: %v", ErrUnsupportedKey, err)
	}
	return algorithmOfPrivate(raw)
}

// MarshalCAKey encodes a CA private key as PKCS#8 DER, the single canonical
// sealed form. The caller seals the result and zeroes it.
func MarshalCAKey(key crypto.Signer) ([]byte, error) {
	if _, _, err := algorithmOfPrivate(key); err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("sshca: marshal CA key: %w", err)
	}
	return der, nil
}

// SignerFromCAKey opens a PKCS#8 DER CA key into an SSH signer. RSA CA keys
// sign with rsa-sha2-512 only (never SHA-1 ssh-rsa).
func SignerFromCAKey(der []byte) (ssh.Signer, Algorithm, error) {
	raw, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, "", fmt.Errorf("sshca: parse CA key: %w", err)
	}
	key, alg, err := algorithmOfPrivate(raw)
	if err != nil {
		return nil, "", err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, "", fmt.Errorf("sshca: CA signer: %w", err)
	}
	if alg == AlgorithmRSA3072 {
		as, ok := signer.(ssh.AlgorithmSigner)
		if !ok {
			return nil, "", ErrUnsupportedKey
		}
		signer, err = ssh.NewSignerWithAlgorithms(as, []string{ssh.KeyAlgoRSASHA512})
		if err != nil {
			return nil, "", fmt.Errorf("sshca: CA signer: %w", err)
		}
	}
	return signer, alg, nil
}

// PublicKeyOf returns the SSH public key of a private key.
func PublicKeyOf(key crypto.Signer) (ssh.PublicKey, error) {
	pub, err := ssh.NewPublicKey(key.Public())
	if err != nil {
		return nil, fmt.Errorf("sshca: public key: %w", err)
	}
	return pub, nil
}

// AuthorizedKey renders a public key as one authorized_keys line (no trailing
// newline) with an optional comment.
func AuthorizedKey(pub ssh.PublicKey, comment string) string {
	line := strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(pub)), "\n")
	if comment != "" {
		line += " " + comment
	}
	return line
}

// Fingerprint is the OpenSSH SHA256 fingerprint (`SHA256:...`).
func Fingerprint(pub ssh.PublicKey) string { return ssh.FingerprintSHA256(pub) }

// MarshalUserPrivateKey renders a generated user key in the OpenSSH private
// key format, unencrypted: it is disclosed once to the requester, who owns its
// protection from then on. Hikyo never stores it.
func MarshalUserPrivateKey(key crypto.Signer, comment string) (string, error) {
	block, err := ssh.MarshalPrivateKey(key, comment)
	if err != nil {
		return "", fmt.Errorf("sshca: marshal user key: %w", err)
	}
	return string(pem.EncodeToMemory(block)), nil
}
