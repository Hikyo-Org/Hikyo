package oidcfed

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/federationhttp"
	"github.com/Hikyo-Org/hikyo/internal/jwkssource"
	"github.com/Hikyo-Org/hikyo/internal/oidctest"
)

func issuerCA(t *testing.T) (*oidctest.IdP, Issuer) {
	t.Helper()
	p, err := oidctest.NewTLS()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	bundle := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: p.Server.Certificate().Raw}))
	return p, Issuer{Issuer: p.Server.URL, KeySource: jwkssource.RemoteDiscovery(), CABundlePEM: bundle}
}

func otherCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	raw, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}))
}

func TestIssuerCABundleTrustAndCacheIsolation(t *testing.T) {
	_, iss := issuerCA(t)
	cache := &Cache{}
	untrusted := iss
	untrusted.CABundlePEM = ""
	if _, err := cache.fetch(t.Context(), untrusted, time.Now()); err == nil {
		t.Fatal("private CA trusted without bundle")
	}
	if _, _, err := cache.keysFor(t.Context(), iss, "test-key-1", time.Now()); err != nil {
		t.Fatalf("supplied CA: %v", err)
	}
	if _, _, err := cache.keysFor(t.Context(), untrusted, "test-key-1", time.Now()); err == nil {
		t.Fatal("cleared bundle reused previously trusted cache")
	}
	untrusted.CABundlePEM = otherCA(t)
	if _, _, err := cache.keysFor(t.Context(), untrusted, "test-key-1", time.Now()); err == nil {
		t.Fatal("changed bundle reused previously trusted cache")
	}
}

func TestIssuerCABundleReplacesClientRootsWithoutMutation(t *testing.T) {
	p, iss := issuerCA(t)
	cache := &Cache{HTTP: p.Client()}
	iss.CABundlePEM = otherCA(t)
	if _, err := cache.fetch(t.Context(), iss, time.Now()); err == nil {
		t.Fatal("custom bundle augmented client roots")
	}
	response, err := p.Client().Get(p.Server.URL + "/jwks")
	if err != nil {
		t.Fatalf("original client roots mutated: %v", err)
	}
	response.Body.Close()
}

func TestIssuerCABundleRedirectGuardsAndEgress(t *testing.T) {
	p, iss := issuerCA(t)
	policy := federationhttp.Policy{AllowedCIDRs: map[string][]netip.Prefix{p.Server.URL: {netip.MustParsePrefix("127.0.0.1/32")}}}
	client, err := federationhttp.NewClient(policy, federationhttp.DocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	cache := &Cache{HTTP: client}
	p.JWKSURIOverride = p.Server.URL + "/jwks-redirect"
	p.RedirectJWKSTo = p.Server.URL + "/jwks"
	if _, err := cache.fetch(t.Context(), iss, time.Now()); err != nil {
		t.Fatalf("HTTPS redirect with same CA: %v", err)
	}
	p.RedirectJWKSTo = "http://127.0.0.1/jwks"
	if _, err := cache.fetch(t.Context(), iss, time.Now()); !errors.Is(err, ErrInsecureTransport) {
		t.Fatalf("HTTP redirect = %v", err)
	}
	p.RedirectJWKSTo = "https://127.0.0.1:1/jwks"
	if _, err := cache.fetch(t.Context(), iss, time.Now()); err == nil {
		t.Fatal("CA widened egress policy")
	}
	denied, err := federationhttp.NewClient(federationhttp.Policy{}, federationhttp.DocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Cache{HTTP: denied}).fetch(t.Context(), iss, time.Now()); err == nil {
		t.Fatal("CA allowed private endpoint without egress approval")
	}
}

func TestIssuerCABundleStrictPEM(t *testing.T) {
	_, iss := issuerCA(t)
	for _, raw := range []string{"", "garbage", "garbage\n" + iss.CABundlePEM, iss.CABundlePEM + "garbage", "-----BEGIN PRIVATE KEY-----\nAA==\n-----END PRIVATE KEY-----", iss.CABundlePEM + "-----BEGIN CERTIFICATE-----", "-----BEGIN CERTIFICATE-----\ninvalid\n-----END CERTIFICATE-----\n" + iss.CABundlePEM} {
		if _, err := federationhttp.ParseCABundle(raw); err == nil {
			t.Fatal("accepted invalid bundle")
		}
	}
	if _, err := federationhttp.ParseCABundle(iss.CABundlePEM + iss.CABundlePEM); err != nil {
		t.Fatalf("multiple CAs: %v", err)
	}
}

func TestIssuerCABundleStillVerifiesHostname(t *testing.T) {
	_, iss := issuerCA(t)
	iss.Issuer = strings.Replace(iss.Issuer, "127.0.0.1", "localhost", 1)
	if _, err := (&Cache{}).fetch(t.Context(), iss, time.Now()); err == nil {
		t.Fatal("CA bundle bypassed hostname verification")
	}
}
