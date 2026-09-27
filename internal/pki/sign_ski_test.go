package pki

import (
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"strings"
	"testing"
)

func TestVerifyCARejectsMissingSubjectKeyIdentifier(t *testing.T) {
	root := newRoot(t)
	template := *root.Certificate
	// Override x509.CreateCertificate's automatic CA SKI with an empty one.
	template.ExtraExtensions = []pkix.Extension{{Id: asn1.ObjectIdentifier{2, 5, 29, 14}, Value: []byte{4, 0}}}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, root.Signer.Public(), root.Signer)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.SubjectKeyId) != 0 {
		t.Fatal("fixture unexpectedly has SKI")
	}
	if err := parsed.CheckSignatureFrom(parsed); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCA(der, root.Signer.Public(), nil, epoch); !errors.Is(err, ErrInvalidCA) || !strings.Contains(err.Error(), "subject key identifier") {
		t.Fatalf("missing SKI: %v", err)
	}
}
