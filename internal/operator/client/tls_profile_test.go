package client

import (
	"crypto/tls"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

const operatorCanary = "canary-bearer-1127-operator"

func TestOperatorClientProfilesAgainstRealPeers(t *testing.T) {
	cases := []struct {
		name     string
		peer     tlstest.PeerOptions
		strictOK bool
	}{
		{"compatible hybrid peer", tlstest.PeerOptions{}, true},
		{"TLS 1.3 only classical peer", tlstest.PeerOptions{MinVersion: tls.VersionTLS13, Curves: []tls.CurveID{tls.X25519}}, false},
		{"TLS 1.2 only peer", tlstest.PeerOptions{MaxVersion: tls.VersionTLS12}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, profile := range []tlspolicy.Profile{"", tlspolicy.ProfileCompat, tlspolicy.ProfileStrict} {
				peer := tlstest.StartPeer(t, tc.peer)
				c, err := NewClient(peer.URL, peer.CertPEM, "hikyo-operator/test", WithTLSProfile(profile))
				if err != nil {
					t.Fatal(err)
				}
				_, _, err = c.Fetch(t.Context(), FetchRequest{Org: "o", Project: "p", Environment: "e", Bearer: operatorCanary})
				delivered := peer.Hits() == 1
				want := profile != tlspolicy.ProfileStrict || tc.strictOK
				if delivered != want {
					t.Fatalf("profile %q: request delivered=%v, want %v (err=%v)", profile, delivered, want, err)
				}
				if !want {
					if err == nil || !strings.Contains(err.Error(), "tls:") {
						t.Fatalf("a refused handshake must surface as a TLS error, got %v", err)
					}
					if len(peer.Authorizations()) != 0 || peer.RawContains(operatorCanary) {
						t.Fatal("bearer reached a peer that strict must have refused")
					}
					continue
				}
				if profile == tlspolicy.ProfileStrict {
					s := peer.States()[0]
					if s.Version != tls.VersionTLS13 || s.CurveID != tls.X25519MLKEM768 {
						t.Fatalf("strict negotiated %x %v", s.Version, s.CurveID)
					}
				}
			}
		})
	}
}

func TestOperatorStrictKeepsCABundleVerification(t *testing.T) {
	peer := tlstest.StartPeer(t, tlstest.PeerOptions{})
	other, _, _ := tlstest.MintServerCert(t, "127.0.0.1")
	c, err := NewClient(peer.URL, other, "hikyo-operator/test", WithTLSProfile(tlspolicy.ProfileStrict))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Fetch(t.Context(), FetchRequest{Org: "o", Project: "p", Environment: "e", Bearer: operatorCanary}); err == nil {
		t.Fatal("strict must still refuse a certificate outside the instance CA bundle")
	}
	if peer.Hits() != 0 || peer.RawContains(operatorCanary) {
		t.Fatal("bearer delivered to an untrusted peer")
	}
	wrongName := tlstest.StartPeer(t, tlstest.PeerOptions{Hosts: []string{"other.example"}})
	c, err = NewClient(wrongName.URL, wrongName.CertPEM, "hikyo-operator/test", WithTLSProfile(tlspolicy.ProfileStrict))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Fetch(t.Context(), FetchRequest{Org: "o", Project: "p", Environment: "e", Bearer: operatorCanary}); err == nil || wrongName.Hits() != 0 {
		t.Fatalf("strict must still refuse a hostname mismatch: %v", err)
	}
}
