// Package releasetrust authenticates release and operator evidence using
// maintained Sigstore verifiers. It performs no network or datastore access.
package releasetrust

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/sigstore/sigstore/pkg/signature"
)

const MaxDocumentBytes = 4 << 20

// LegacyBundle is the closed key-based Cosign --new-bundle-format=false
// envelope. Cert/RekorBundle are packaging only: this profile always verifies
// against the caller's separately pinned public key, never envelope key data.
type LegacyBundle struct {
	Base64Signature string          `json:"base64Signature"`
	Cert            string          `json:"cert,omitempty"`
	RekorBundle     json.RawMessage `json:"rekorBundle,omitempty"`
}

func decodeDocument(raw []byte, value any) error {
	if len(raw) == 0 || len(raw) > MaxDocumentBytes {
		return errors.New("trust document exceeds its byte bound or is empty")
	}
	return definitions.DecodeStrict(raw, value)
}

// VerifyKeySignature authenticates exact payload bytes under an independently
// pinned public key. This does not authorize a release, route or migration;
// the corresponding closed schema and trust-policy checks remain mandatory.
func VerifyKeySignature(publicKeyPEM, bundleRaw, payload []byte) error {
	var bundle LegacyBundle
	if err := decodeDocument(bundleRaw, &bundle); err != nil || bundle.Base64Signature == "" {
		return errors.New("invalid key-based Cosign bundle")
	}
	sig, err := base64.StdEncoding.Strict().DecodeString(bundle.Base64Signature)
	if err != nil || len(sig) == 0 {
		return errors.New("invalid base64 signature")
	}
	key, err := parsePinnedKey(publicKeyPEM)
	if err != nil {
		return err
	}
	verifier, err := signature.LoadVerifier(key, crypto.SHA256)
	if err != nil {
		return fmt.Errorf("load maintained signature verifier: %w", err)
	}
	if err := verifier.VerifySignature(bytes.NewReader(sig), bytes.NewReader(payload)); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	return nil
}

func parsePinnedKey(publicKeyPEM []byte) (crypto.PublicKey, error) {
	if len(publicKeyPEM) == 0 || len(publicKeyPEM) > MaxDocumentBytes {
		return nil, errors.New("public key exceeds its byte bound or is empty")
	}
	block, rest := pem.Decode(publicKeyPEM)
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 || block.Type != "PUBLIC KEY" || len(block.Headers) != 0 {
		return nil, errors.New("invalid PEM public key")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	// Retain the existing installer key policy. The library owns all
	// signature algorithms; this switch selects permitted key parameters.
	switch public := key.(type) {
	case *ecdsa.PublicKey:
		bits := public.Curve.Params().BitSize
		if bits != 256 && bits != 384 && bits != 521 {
			return nil, errors.New("unsupported ECDSA curve")
		}
	case *rsa.PublicKey:
		if public.N.BitLen() < 2048 {
			return nil, errors.New("RSA public key is smaller than 2048 bits")
		}
	case ed25519.PublicKey:
	default:
		return nil, fmt.Errorf("unsupported public key type %T", key)
	}
	return key, nil
}

// OperatorKeyID fingerprints the canonical PKIX DER public key after applying
// the same key policy as signature verification. PEM whitespace is not identity.
func OperatorKeyID(publicKeyPEM []byte) (releaseidentity.Digest, error) {
	key, err := parsePinnedKey(publicKeyPEM)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", err
	}
	return releaseidentity.Hash(der), nil
}

// VerifyOperatorSignature is the explicit key-based operator profile. Its
// caller must pin the current installation key and validate an operator
// statement before granting authority. It cannot create a VerifiedRelease.
// Server/helper verification requires no external Cosign executable.
func VerifyOperatorSignature(publicKeyPEM, bundleRaw, payload []byte) error {
	return VerifyKeySignature(publicKeyPEM, bundleRaw, payload)
}
