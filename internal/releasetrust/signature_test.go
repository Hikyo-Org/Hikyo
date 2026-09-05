package releasetrust_test

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/sigstore/sigstore/pkg/signature"
)

func TestMaintainedKeyVerifierPreservesSupportedAlgorithms(t *testing.T) {
	for _, algorithm := range []string{"p256", "p384", "p521", "rsa", "ed25519"} {
		t.Run(algorithm, func(t *testing.T) {
			var private crypto.PrivateKey
			var err error
			switch algorithm {
			case "p256":
				private, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			case "p384":
				private, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
			case "p521":
				private, err = ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
			case "rsa":
				private, err = rsa.GenerateKey(rand.Reader, 2048)
			case "ed25519":
				_, private, err = ed25519.GenerateKey(rand.Reader)
			}
			if err != nil {
				t.Fatal(err)
			}
			signer, err := signature.LoadSigner(private, crypto.SHA256)
			if err != nil {
				t.Fatal(err)
			}
			public, err := signer.PublicKey()
			if err != nil {
				t.Fatal(err)
			}
			der, err := x509.MarshalPKIXPublicKey(public)
			if err != nil {
				t.Fatal(err)
			}
			key := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
			payload := []byte(`{"format":"upgrade-attestation/v1","fixture":"exact bytes"}`)
			sig, err := signer.SignMessage(bytes.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := json.Marshal(releasetrust.LegacyBundle{Base64Signature: base64.StdEncoding.EncodeToString(sig)})
			if err != nil {
				t.Fatal(err)
			}
			if err := releasetrust.VerifyOperatorSignature(key, bundle, payload); err != nil {
				t.Fatal(err)
			}
			if err := releasetrust.VerifyOperatorSignature(key, bundle, append(payload, ' ')); err == nil {
				t.Fatal("changed exact payload accepted")
			}
			ambiguous := append([]byte(`{"base64Signature":"forged",`), bundle[1:]...)
			if err := releasetrust.VerifyOperatorSignature(key, ambiguous, payload); err == nil {
				t.Fatal("duplicate signature member accepted")
			}
		})
	}
}
