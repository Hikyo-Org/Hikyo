package pki

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"
)

// Clock skew allowance: every certificate's notBefore is backdated by this.
const backdate = time.Minute

// Bounds on CA lifetimes.
const (
	MaxRootTTL         = 20 * 365 * 24 * time.Hour
	MaxIntermediateTTL = 10 * 365 * 24 * time.Hour
	MinCATTL           = 24 * time.Hour
)

var (
	// ErrInvalidCA refuses a certificate that cannot act as a Hikyo issuer.
	ErrInvalidCA = errors.New("pki: certificate is not a usable CA")
	// ErrInvalidCSR refuses a malformed or unsigned CSR.
	ErrInvalidCSR = errors.New("pki: invalid certificate signing request")
	// ErrIssuerExpiry refuses a leaf that would outlive its issuer.
	ErrIssuerExpiry = errors.New("pki: the certificate would outlive its issuer")
)

// Subject is the CA subject a caller may set: a common name and an optional
// organization. Nothing else is caller-controlled.
type Subject struct {
	CommonName   string
	Organization string
}

func (s Subject) name() (pkix.Name, error) {
	cn := strings.TrimSpace(s.CommonName)
	if cn == "" || len(cn) > 64 || strings.ContainsFunc(cn, isControl) {
		return pkix.Name{}, errors.New("pki: common name must be 1-64 printable characters")
	}
	name := pkix.Name{CommonName: cn}
	if o := strings.TrimSpace(s.Organization); o != "" {
		if len(o) > 64 || strings.ContainsFunc(o, isControl) {
			return pkix.Name{}, errors.New("pki: organization must be at most 64 printable characters")
		}
		name.Organization = []string{o}
	}
	return name, nil
}

// CheckCAAlgorithm refuses a CA key algorithm outside CAKeyAlgorithms.
func CheckCAAlgorithm(algorithm KeyAlgorithm) error {
	if !slices.Contains(CAKeyAlgorithms, algorithm) {
		return fmt.Errorf("%w %q for a CA key", ErrUnknownAlgorithm, algorithm)
	}
	return nil
}

func caTemplate(subject pkix.Name, serial *big.Int, now time.Time, ttl time.Duration, maxPathLen int) *x509.Certificate {
	return &x509.Certificate{
		SerialNumber:          serial,
		Subject:               subject,
		NotBefore:             now.Add(-backdate),
		NotAfter:              now.Add(ttl),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            maxPathLen,
		MaxPathLenZero:        maxPathLen == 0,
	}
}

// CreateRoot self-signs a root CA certificate for key. The root may sign one
// level of intermediates (MaxPathLen 1) and leaves directly.
func CreateRoot(key crypto.Signer, subject Subject, now time.Time, ttl time.Duration) ([]byte, error) {
	if ttl < MinCATTL || ttl > MaxRootTTL {
		return nil, fmt.Errorf("pki: root ttl must be between %s and %s", MinCATTL, MaxRootTTL)
	}
	name, err := subject.name()
	if err != nil {
		return nil, err
	}
	serial, err := NewSerial()
	if err != nil {
		return nil, err
	}
	template := caTemplate(name, serial, now, ttl, 1)
	return x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
}

// CreateCSR builds a CSR for a pending intermediate, for signing by an offline
// root.
func CreateCSR(key crypto.Signer, subject Subject) ([]byte, error) {
	name, err := subject.name()
	if err != nil {
		return nil, err
	}
	return x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: name}, key)
}

// Parent is an issuing CA: its certificate and a signer for its key.
type Parent struct {
	Certificate *x509.Certificate
	Signer      crypto.Signer
}

// SignIntermediate signs an intermediate CA certificate (MaxPathLen 0: it may
// sign only leaves). The intermediate may not outlive its parent.
func SignIntermediate(parent Parent, public crypto.PublicKey, subject Subject, now time.Time, ttl time.Duration) ([]byte, error) {
	if ttl < MinCATTL || ttl > MaxIntermediateTTL {
		return nil, fmt.Errorf("pki: intermediate ttl must be between %s and %s", MinCATTL, MaxIntermediateTTL)
	}
	if !parent.Certificate.IsCA || parent.Certificate.MaxPathLenZero {
		return nil, fmt.Errorf("%w: the parent may not sign intermediates", ErrInvalidCA)
	}
	name, err := subject.name()
	if err != nil {
		return nil, err
	}
	serial, err := NewSerial()
	if err != nil {
		return nil, err
	}
	template := caTemplate(name, serial, now, ttl, 0)
	if template.NotAfter.After(parent.Certificate.NotAfter) {
		return nil, ErrIssuerExpiry
	}
	return x509.CreateCertificate(rand.Reader, template, parent.Certificate, public, parent.Signer)
}

// VerifyCA checks that certDER is a usable issuer for key: a CA certificate
// with certificate and CRL signing, currently valid, whose public key is the
// sealed key's, and which chains to the last supplied chain certificate,
// using earlier entries as intermediates. With no chain, the certificate
// must be self-signed. A self-issued trust anchor must verify its own
// signature; a cross-signed rollover is verified against the supplied parent.
// Every rejection wraps ErrInvalidCA.
func VerifyCA(certDER []byte, public crypto.PublicKey, chain []*x509.Certificate, now time.Time) (*x509.Certificate, error) {
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCA, err)
	}
	switch {
	case !cert.BasicConstraintsValid || !cert.IsCA:
		return nil, fmt.Errorf("%w: basic constraints do not mark it as a CA", ErrInvalidCA)
	case cert.KeyUsage&x509.KeyUsageCertSign == 0 || cert.KeyUsage&x509.KeyUsageCRLSign == 0:
		return nil, fmt.Errorf("%w: key usage must include certificate and CRL signing", ErrInvalidCA)
	case now.Before(cert.NotBefore) || !now.Before(cert.NotAfter):
		return nil, fmt.Errorf("%w: it is not currently valid", ErrInvalidCA)
	case !PublicKeysEqual(public, cert.PublicKey):
		return nil, fmt.Errorf("%w: its public key does not match the issuer key", ErrInvalidCA)
	}
	if _, err := AlgorithmOf(cert.PublicKey); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCA, err)
	}
	roots := x509.NewCertPool()
	intermediates := x509.NewCertPool()
	if bytes.Equal(cert.RawIssuer, cert.RawSubject) && (len(chain) == 0 || bytes.Equal(cert.Raw, chain[len(chain)-1].Raw)) {
		// Verify skips trust-anchor signatures. Check a self-signed import,
		// but let Verify validate a self-issued rollover against its parent.
		if err := cert.CheckSignatureFrom(cert); err != nil {
			return nil, fmt.Errorf("%w: self-signature does not verify: %v", ErrInvalidCA, err)
		}
		if len(chain) == 0 {
			roots.AddCert(cert)
		}
	}
	for i, c := range chain {
		if i == len(chain)-1 {
			roots.AddCert(c)
		} else {
			intermediates.AddCert(c)
		}
	}
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots: roots, Intermediates: intermediates, CurrentTime: now,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return nil, fmt.Errorf("%w: it does not chain to the supplied chain: %v", ErrInvalidCA, err)
	}
	return cert, nil
}

// ParseCSR parses a PEM or DER CSR and checks its self-signature (proof of
// possession). Only the public key is used: names come from the explicit
// request fields (ADR D6).
func ParseCSR(input []byte) (*x509.CertificateRequest, error) {
	der := input
	if block, _ := pem.Decode(input); block != nil {
		if block.Type != "CERTIFICATE REQUEST" && block.Type != "NEW CERTIFICATE REQUEST" {
			return nil, fmt.Errorf("%w: PEM block is %q", ErrInvalidCSR, block.Type)
		}
		der = block.Bytes
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCSR, err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("%w: signature does not verify", ErrInvalidCSR)
	}
	return csr, nil
}

// Leaf is everything SignLeaf needs beyond the resolved request.
type Leaf struct {
	Serial                *big.Int
	PublicKey             crypto.PublicKey
	NotBefore             time.Time
	NotAfter              time.Time
	Organization          string
	KeyUsages             []KeyUsage
	ExtKeyUsages          []ExtKeyUsage
	CRLDistributionPoints []string
}

// LeafWindow returns [now - 1 minute, now + ttl], allowing clock skew. It
// returns ErrIssuerExpiry if the end exceeds the issuer's NotAfter; equality
// is allowed. The caller must validate ttl.
func LeafWindow(issuer *x509.Certificate, now time.Time, ttl time.Duration) (time.Time, time.Time, error) {
	notBefore, notAfter := now.Add(-backdate), now.Add(ttl)
	if notAfter.After(issuer.NotAfter) {
		return time.Time{}, time.Time{}, ErrIssuerExpiry
	}
	return notBefore, notAfter, nil
}

// SignLeaf signs an end-entity certificate. Leaves are never CAs. Key usages
// that do not apply to the key type are dropped (key encipherment is RSA-only,
// key agreement is ECDSA-only).
func SignLeaf(parent Parent, resolved Resolved, leaf Leaf) ([]byte, error) {
	if leaf.NotAfter.After(parent.Certificate.NotAfter) {
		return nil, ErrIssuerExpiry
	}
	subject := pkix.Name{CommonName: resolved.CommonName}
	if leaf.Organization != "" {
		subject.Organization = []string{leaf.Organization}
	}
	usage := applicableKeyUsage(leaf.PublicKey, leaf.KeyUsages)
	if usage == 0 {
		return nil, fmt.Errorf("%w: no configured key usage applies to the public key", ErrInvalidPolicy)
	}
	var extended []x509.ExtKeyUsage
	for _, u := range leaf.ExtKeyUsages {
		switch u {
		case ExtUsageServerAuth:
			extended = append(extended, x509.ExtKeyUsageServerAuth)
		case ExtUsageClientAuth:
			extended = append(extended, x509.ExtKeyUsageClientAuth)
		}
	}
	template := &x509.Certificate{
		SerialNumber:          leaf.Serial,
		Subject:               subject,
		NotBefore:             leaf.NotBefore,
		NotAfter:              leaf.NotAfter,
		KeyUsage:              usage,
		ExtKeyUsage:           extended,
		BasicConstraintsValid: true,
		IsCA:                  false,
		DNSNames:              resolved.DNSNames,
		IPAddresses:           resolved.IPAddresses,
		URIs:                  resolved.URIs,
		CRLDistributionPoints: leaf.CRLDistributionPoints,
	}
	return x509.CreateCertificate(rand.Reader, template, parent.Certificate, leaf.PublicKey, parent.Signer)
}

// applicableKeyUsage intersects configured usages with the key's capabilities.
// It never grants a usage absent from the policy.
func applicableKeyUsage(public crypto.PublicKey, usages []KeyUsage) x509.KeyUsage {
	var usage x509.KeyUsage
	for _, u := range usages {
		switch u {
		case UsageDigitalSignature:
			usage |= x509.KeyUsageDigitalSignature
		case UsageKeyEncipherment:
			if _, ok := public.(*rsa.PublicKey); ok {
				usage |= x509.KeyUsageKeyEncipherment
			}
		case UsageKeyAgreement:
			if _, ok := public.(*ecdsa.PublicKey); ok {
				usage |= x509.KeyUsageKeyAgreement
			}
		}
	}
	return usage
}

// RevocationReason is the closed RFC 5280 reason enum Hikyo records.
type RevocationReason string

const (
	ReasonUnspecified          RevocationReason = "unspecified"
	ReasonKeyCompromise        RevocationReason = "key-compromise"
	ReasonCACompromise         RevocationReason = "ca-compromise"
	ReasonAffiliationChanged   RevocationReason = "affiliation-changed"
	ReasonSuperseded           RevocationReason = "superseded"
	ReasonCessationOfOperation RevocationReason = "cessation-of-operation"
	ReasonPrivilegeWithdrawn   RevocationReason = "privilege-withdrawn"
)

var reasonCodes = map[RevocationReason]int{
	ReasonUnspecified: 0, ReasonKeyCompromise: 1, ReasonCACompromise: 2,
	ReasonAffiliationChanged: 3, ReasonSuperseded: 4, ReasonCessationOfOperation: 5,
	ReasonPrivilegeWithdrawn: 9,
}

// ParseRequestedReason accepts the reasons a caller may request. ca-compromise
// is system-only: it is recorded when an issuer is revoked. An empty string
// selects unspecified; unknown or system-only reasons return an error.
func ParseRequestedReason(s string) (RevocationReason, error) {
	if s == "" {
		return ReasonUnspecified, nil
	}
	reason := RevocationReason(s)
	if _, ok := reasonCodes[reason]; !ok || reason == ReasonCACompromise {
		return "", fmt.Errorf("pki: unknown revocation reason %q", s)
	}
	return reason, nil
}

// RevokedEntry is one CRL entry.
type RevokedEntry struct {
	Serial    *big.Int
	RevokedAt time.Time
	Reason    RevocationReason
}

// CRLValidity is how long a published CRL is valid; the worker republishes at
// half of it.
const CRLValidity = 24 * time.Hour

// CreateCRL signs a CRL for the issuer.
func CreateCRL(parent Parent, entries []RevokedEntry, number *big.Int, now time.Time) ([]byte, error) {
	list := make([]x509.RevocationListEntry, 0, len(entries))
	for _, entry := range entries {
		list = append(list, x509.RevocationListEntry{
			SerialNumber: entry.Serial, RevocationTime: entry.RevokedAt.UTC(), ReasonCode: reasonCodes[entry.Reason],
		})
	}
	return x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		Number:                    number,
		ThisUpdate:                now.UTC(),
		NextUpdate:                now.Add(CRLValidity).UTC(),
		RevokedCertificateEntries: list,
	}, parent.Certificate, parent.Signer)
}

// NextCRLNumber is max(previous+1, now in seconds): monotonic even after a
// restore from a backup that carried an older number (ADR D7).
func NextCRLNumber(previous int64, now time.Time) int64 {
	return max(previous+1, now.Unix())
}

func bigInt(v int64) *big.Int { return big.NewInt(v) }

// CRLNumber is NextCRLNumber as the *big.Int CreateCRL takes.
func CRLNumber(v int64) *big.Int { return bigInt(v) }
