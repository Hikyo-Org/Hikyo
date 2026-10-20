package app

import (
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/remotefetch"
	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

func TestPairingIdentityRequiresExplicitHTTPSOriginWithoutIO(t *testing.T) {
	client, err := remotefetch.New(remotefetch.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []*config.Config{
		{ExternalOrigin: "https://localhost"},
		{ExternalOrigin: "http://localhost", ExternalOriginExplicit: true},
		{ExternalOrigin: "https://localhost/", ExternalOriginExplicit: true},
		{ExternalOrigin: "https://user:do-not-disclose@localhost", ExternalOriginExplicit: true},
	} {
		cfg.PairingSPKIPin = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
		before := remotefetch.Dials()
		problem, resolve := newPairingIdentity(cfg, nil, client)
		if problem != pairingOriginConfigurationError || remotefetch.Dials() != before {
			t.Fatal("invalid origin not diagnosed without network I/O")
		}
		pin, err := resolve(t.Context())
		if pin != "" || !errors.Is(err, domain.ErrInvalid) || strings.Contains(err.Error(), "do-not-disclose") || remotefetch.Dials() != before {
			t.Fatal("invalid origin bypassed by override or leaked input")
		}
	}
}

func TestPairingIdentityOverridePrecedesNativeCertificateAndCapturesGeneration(t *testing.T) {
	certPEM, keyPEM, _ := tlstest.MintServerCert(t, "pairing.example")
	certificate, err := newManagedCertificate(string(certPEM), string(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	const override = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	cfg := &config.Config{Listen: "0.0.0.0:443", ExternalOrigin: "https://pairing.example", ExternalOriginExplicit: true, PairingSPKIPin: override}
	problem, resolve := newPairingIdentity(cfg, certificate, nil)
	cfg.PairingSPKIPin, cfg.ExternalOrigin = "invalid", "http://changed.invalid"
	before := remotefetch.Dials()
	pin, err := resolve(t.Context())
	if problem != "" || err != nil || pin != override || remotefetch.Dials() != before {
		t.Fatal("override lost priority or immutable generation authority")
	}
}

func TestPairingIdentityObservesNativeTLSAndNeverInfersPublicKey(t *testing.T) {
	certPEM, keyPEM, leaf := tlstest.MintServerCert(t, "127.0.0.1")
	certificate, err := newManagedCertificate(string(certPEM), string(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	peer := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	peer.TLS = &tls.Config{Certificates: []tls.Certificate{*certificate.pair}, MinVersion: tls.VersionTLS12}
	peer.Config.ErrorLog = log.New(io.Discard, "", 0)
	peer.StartTLS()
	defer peer.Close()
	client, err := remotefetch.New(remotefetch.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Listen: "127.0.0.1:443", ExternalOrigin: peer.URL, ExternalOriginExplicit: true, TrustedProxyCIDRs: []string{"10.0.0.0/8"}}
	before := remotefetch.Dials()
	problem, resolve := newPairingIdentity(cfg, certificate, client)
	if problem != "" || remotefetch.Dials() != before {
		t.Fatal("constructing resolver performed discovery")
	}
	pin, err := resolve(t.Context())
	if err != nil || pin != remotefetch.SPKIFingerprint(leaf) || remotefetch.Dials() != before+1 || requests.Load() != 0 {
		t.Fatalf("native certificate did not authenticate actual TLS-only probe: %v", err)
	}
	// A loaded certificate alone is never taken as evidence of the public key.
	_, noTransport := newPairingIdentity(cfg, certificate, nil)
	if pin, err := noTransport(t.Context()); pin != "" || !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("native certificate bypassed public TLS observation")
	}
	certificate.pair.Leaf.NotAfter = time.Now().Add(-time.Second)
	_, expired := newPairingIdentity(cfg, certificate, client)
	if pin, err := expired(t.Context()); pin != "" || !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("expired native certificate accepted as trust anchor")
	}
}
