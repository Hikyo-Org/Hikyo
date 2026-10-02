package samlsp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

func TestParseMetadataSelectsExactEntityAndSigningKeys(t *testing.T) {
	t.Parallel()

	_, signingCertificate := requestSigningFixture(t)
	_, encryptionCertificate := requestSigningFixture(t)
	validUntil := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	raw := []byte(`<md:EntitiesDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" xmlns:ds="http://www.w3.org/2000/09/xmldsig#" validUntil="` + validUntil.Format(time.RFC3339Nano) + `">` +
		`<md:EntityDescriptor entityID="https://other.example"><md:IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol"/></md:EntityDescriptor>` +
		`<md:EntityDescriptor entityID="https://idp.example/metadata"><md:IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol" WantAuthnRequestsSigned="true">` +
		metadataKeyDescriptor("signing", signingCertificate.Raw) +
		metadataKeyDescriptor("encryption", encryptionCertificate.Raw) +
		`<md:SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example/sso"/>` +
		`</md:IDPSSODescriptor></md:EntityDescriptor></md:EntitiesDescriptor>`)

	metadata, err := ParseMetadata(raw, "https://idp.example/metadata")
	if err != nil {
		t.Fatalf("ParseMetadata() error = %v", err)
	}
	if metadata.EntityID != "https://idp.example/metadata" || metadata.SSOURL != "https://idp.example/sso" {
		t.Fatalf("metadata identity = (%q, %q)", metadata.EntityID, metadata.SSOURL)
	}
	if !metadata.WantAuthnRequestsSigned {
		t.Fatal("WantAuthnRequestsSigned = false")
	}
	if len(metadata.SigningCertificates) != 1 || !metadata.SigningCertificates[0].Equal(signingCertificate) {
		t.Fatalf("SigningCertificates = %d, want selected signing cert", len(metadata.SigningCertificates))
	}
	if metadata.ValidUntil == nil || !metadata.ValidUntil.Equal(validUntil) {
		t.Fatalf("ValidUntil = %v", metadata.ValidUntil)
	}
}

func TestParseMetadataRejectsAmbiguousEntityAndMissingRedirectEndpoint(t *testing.T) {
	t.Parallel()

	duplicate := []byte(`<md:EntitiesDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata"><md:EntityDescriptor entityID="x"/><md:EntityDescriptor entityID="x"/></md:EntitiesDescriptor>`)
	if _, err := ParseMetadata(duplicate, "x"); !errors.Is(err, ErrMetadataEntityCardinality) {
		t.Fatalf("duplicate ParseMetadata() error = %v, want ErrMetadataEntityCardinality", err)
	}

	missingRedirect := []byte(`<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" entityID="x"><md:IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol"><md:SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="https://idp.example/sso"/></md:IDPSSODescriptor></md:EntityDescriptor>`)
	if _, err := ParseMetadata(missingRedirect, "x"); !errors.Is(err, ErrMetadataSSOEndpoint) {
		t.Fatalf("POST-only ParseMetadata() error = %v, want ErrMetadataSSOEndpoint", err)
	}
}

func TestValidateMetadataCertificateKeyRejectsWeakAndUnsupportedKeys(t *testing.T) {
	t.Parallel()

	tests := map[string]*x509.Certificate{
		"rsa-1024": {
			PublicKey: &rsa.PublicKey{N: new(big.Int).Lsh(big.NewInt(1), 1023), E: 65537},
		},
		"ecdsa-p224": {
			PublicKey: &ecdsa.PublicKey{Curve: elliptic.P224()},
		},
	}
	for name, certificate := range tests {
		certificate := certificate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := validateMetadataCertificateKey(certificate); !errors.Is(err, ErrMetadataCertificateKey) {
				t.Fatalf("validateMetadataCertificateKey() error = %v, want ErrMetadataCertificateKey", err)
			}
		})
	}
}

func TestValidateMetadataCertificateKeyAcceptsSupportedKeys(t *testing.T) {
	t.Parallel()

	_, rsaCertificate := requestSigningFixture(t)
	if err := validateMetadataCertificateKey(rsaCertificate); err != nil {
		t.Fatalf("validateMetadataCertificateKey(RSA-2048) error = %v", err)
	}
	if err := validateMetadataCertificateKey(&x509.Certificate{
		PublicKey: &ecdsa.PublicKey{Curve: elliptic.P256()},
	}); err != nil {
		t.Fatalf("validateMetadataCertificateKey(ECDSA-P256) error = %v", err)
	}
}

func TestParseMetadataVerifiesSignedDescriptorBeforeExtraction(t *testing.T) {
	t.Parallel()

	key, certificate := requestSigningFixture(t)
	xml := `<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" xmlns:ds="http://www.w3.org/2000/09/xmldsig#" ID="_metadata" entityID="https://idp.example/metadata">` +
		`<md:IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">` + metadataKeyDescriptor("signing", certificate.Raw) +
		`<md:SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example/sso"/>` +
		`</md:IDPSSODescriptor></md:EntityDescriptor>`
	document := etree.NewDocument()
	if err := document.ReadFromString(xml); err != nil {
		t.Fatal(err)
	}
	signing, err := dsig.NewSigningContext(key, [][]byte{certificate.Raw})
	if err != nil {
		t.Fatal(err)
	}
	signing.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	if err := signing.SetSignatureMethod(SignatureRSASHA256); err != nil {
		t.Fatal(err)
	}
	signed, err := signing.SignEnveloped(document.Root())
	if err != nil {
		t.Fatal(err)
	}
	document.SetRoot(signed)
	raw, err := document.WriteToBytes()
	if err != nil {
		t.Fatal(err)
	}

	metadata, err := ParseMetadata(raw, "https://idp.example/metadata")
	if err != nil {
		t.Fatalf("ParseMetadata() error = %v", err)
	}
	if !metadata.Signed || metadata.SignatureCertificate == nil || !metadata.SignatureCertificate.Equal(certificate) {
		t.Fatalf("signature state = (%v, %v)", metadata.Signed, metadata.SignatureCertificate)
	}

	tampered := []byte(strings.Replace(string(raw), "https://idp.example/sso", "https://attacker.example/sso", 1))
	if _, err := ParseMetadata(tampered, "https://idp.example/metadata"); !errors.Is(err, ErrMetadataSignature) {
		t.Fatalf("tampered ParseMetadata() error = %v, want ErrMetadataSignature", err)
	}

	// The child remains signed over the same namespace-qualified values when
	// its namespace declarations come only from the aggregate parent.
	signed.RemoveAttr("xmlns:md")
	signed.RemoveAttr("xmlns:ds")
	aggregate := etree.NewElement("md:EntitiesDescriptor")
	aggregate.CreateAttr("xmlns:md", SAMLMetadataNamespace)
	aggregate.CreateAttr("xmlns:ds", XMLDSIGNamespace)
	aggregate.AddChild(signed)
	document.SetRoot(aggregate)
	raw, err = document.WriteToBytes()
	if err != nil {
		t.Fatal(err)
	}
	metadata, err = ParseMetadata(raw, "https://idp.example/metadata")
	if err != nil || !metadata.Signed || metadata.SSOURL != "https://idp.example/sso" {
		t.Fatalf("inherited namespaces = %+v, error %v", metadata, err)
	}
	tampered = []byte(strings.Replace(string(raw), "https://idp.example/sso", "https://attacker.example/sso", 1))
	if _, err := ParseMetadata(tampered, "https://idp.example/metadata"); !errors.Is(err, ErrMetadataSignature) {
		t.Fatalf("tampered aggregate error = %v", err)
	}
}

func TestMetadataValidityIncludesSelectedRole(t *testing.T) {
	_, certificate := requestSigningFixture(t)
	for _, roleValidity := range []string{"2026-08-01T00:00:00Z", "invalid"} {
		for _, ancestorValidity := range []string{"", ` validUntil="2026-09-01T00:00:00Z"`} {
			raw := []byte(`<md:EntityDescriptor xmlns:md="` + SAMLMetadataNamespace + `" xmlns:ds="` + XMLDSIGNamespace + `" entityID="x"` + ancestorValidity + `><md:IDPSSODescriptor protocolSupportEnumeration="` + SAMLProtocolNamespace + `" validUntil="` + roleValidity + `">` + metadataKeyDescriptor("signing", certificate.Raw) + `<md:SingleSignOnService Binding="` + BindingHTTPRedirect + `" Location="https://idp.example/sso"/></md:IDPSSODescriptor></md:EntityDescriptor>`)
			metadata, err := ParseMetadata(raw, "x")
			if roleValidity == "invalid" {
				if !errors.Is(err, ErrMetadataValidUntil) {
					t.Fatalf("malformed role validity error = %v", err)
				}
			} else if err != nil || metadata.ValidUntil == nil || metadata.ValidUntil.Format(time.RFC3339) != roleValidity {
				t.Fatalf("role validity = %v, error %v", metadata.ValidUntil, err)
			}
		}
	}
}

func metadataKeyDescriptor(use string, certificate []byte) string {
	return `<md:KeyDescriptor use="` + use + `"><ds:KeyInfo><ds:X509Data><ds:X509Certificate>` +
		strings.TrimSpace(base64.StdEncoding.EncodeToString(certificate)) +
		`</ds:X509Certificate></ds:X509Data></ds:KeyInfo></md:KeyDescriptor>`
}
