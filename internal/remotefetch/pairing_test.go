package remotefetch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/pairing"
)

func TestPairedDirectoryUsesFreshKeyProof(t *testing.T) {
	private, err := pairing.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	public, err := pairing.PublicJWK(private)
	if err != nil {
		t.Fatal(err)
	}
	jkt, err := pairing.PublicKeyThumbprint(public)
	if err != nil {
		t.Fatal(err)
	}
	var origin string
	seen := map[string]bool{}
	var hits atomic.Int64
	peer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "DPoP test-keybound-token" {
			t.Errorf("paired fetch used wrong authentication scheme")
			w.WriteHeader(401)
			return
		}
		proof, err := pairing.VerifyDPoP(r.Header.Get("DPoP"), pairing.DPoPExpectation{Method: "GET", URL: origin + directoryPath, AccessToken: "test-keybound-token", Thumbprint: jkt, Now: time.Now().UTC()})
		if err != nil {
			t.Errorf("directory proof did not verify: %v", err)
			w.WriteHeader(401)
			return
		}
		if seen[proof.JTI] {
			t.Error("successive fetches reused the same proof")
		}
		seen[proof.JTI] = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Listing{Identity: "peer", Version: "test", Orgs: []OrgEntry{}, OrgCount: 0, ProjectCount: 0})
	}))
	defer peer.Close()
	origin = peer.URL
	stored, err := EncodePairedCredential("test-keybound-token", private)
	if err != nil {
		t.Fatal(err)
	}
	client := testClient(t)
	for range 2 {
		if _, outcome, err := client.Directory(t.Context(), peer.URL, SPKIFingerprint(peer.Certificate()), stored); err != nil || outcome != OutcomeOK {
			t.Fatalf("paired directory: outcome=%s error=%v", outcome, err)
		}
	}
	if hits.Load() != 2 {
		t.Fatalf("wire hits=%d", hits.Load())
	}
}
func TestMalformedPairedDirectoryNeverFallsBackToBearer(t *testing.T) {
	var hits atomic.Int64
	peer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(401) }))
	defer peer.Close()
	client := testClient(t)
	for _, credential := range []string{"hikyo-paired-v1:garbage", "hikyo-paired-v2:garbage", "hikyo-paired-v1:" + strings.Repeat("x", 9000)} {
		if _, outcome, err := client.Directory(t.Context(), peer.URL, SPKIFingerprint(peer.Certificate()), credential); err == nil || outcome != OutcomeCredentialRejected {
			t.Fatalf("malformed paired payload accepted: outcome=%s error=%v", outcome, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("invalid paired state reached a peer as bearer material")
	}
}
