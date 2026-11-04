package cli

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/diagnostics"
	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

const cliCanary = "canary-bearer-1127-cli"

func leafOf(t *testing.T, chainPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(chainPEM)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return leaf
}

// pinnedEntry trusts the peer by leaf SPKI pin, the mode that needs no system
// roots. A pin gives confidentiality below strict and no authenticity claim.
func pinnedEntry(t *testing.T, peer *tlstest.Peer, profile string) TrustEntry {
	t.Helper()
	return TrustEntry{Name: "peer", Origin: peer.URL, TLSMode: TLSModeSPKI, SPKIPin: SPKIFingerprint(leafOf(t, peer.CertPEM)), TLSProfile: profile}
}

func cliGet(t *testing.T, entry TrustEntry) (*http.Response, error) {
	t.Helper()
	client, err := NewClient(entry, cliCanary)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.HTTP.CloseIdleConnections)
	req, err := http.NewRequest(http.MethodGet, entry.Origin+"/api/v1/meta", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+client.Bearer)
	return client.HTTP.Do(req)
}

func TestCLIStrictProfileAgainstRealPeers(t *testing.T) {
	cases := []struct {
		name     string
		peer     tlstest.PeerOptions
		compatOK bool
		strictOK bool
	}{
		{"compatible hybrid peer", tlstest.PeerOptions{}, true, true},
		{"TLS 1.3 only classical peer", tlstest.PeerOptions{MinVersion: tls.VersionTLS13, Curves: []tls.CurveID{tls.X25519}}, true, false},
		{"TLS 1.2 only peer", tlstest.PeerOptions{MaxVersion: tls.VersionTLS12}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, profile := range []string{"", "compat", "strict"} {
				peer := tlstest.StartPeer(t, tc.peer)
				resp, err := cliGet(t, pinnedEntry(t, peer, profile))
				want := tc.compatOK
				if profile == "strict" {
					want = tc.strictOK
				}
				if (err == nil) != want {
					t.Fatalf("profile %q: served=%v want %v (%v)", profile, err == nil, want, err)
				}
				if err != nil {
					// Server-side proof that the bearer never left the process.
					if peer.Hits() != 0 || len(peer.Authorizations()) != 0 || peer.RawContains(cliCanary) {
						t.Fatalf("profile %q: credential reached a refused peer", profile)
					}
					if !strings.Contains(err.Error(), "tls:") {
						t.Fatalf("refusal should name the TLS failure, got %v", err)
					}
					continue
				}
				resp.Body.Close()
				if profile == "strict" && (resp.TLS.Version != tls.VersionTLS13 || resp.TLS.CurveID != tls.X25519MLKEM768) {
					t.Fatalf("strict negotiated %x %v", resp.TLS.Version, resp.TLS.CurveID)
				}
			}
		})
	}
}

func TestCLIStrictKeepsThePinCheckAndReconnectsFreshly(t *testing.T) {
	peer := tlstest.StartPeer(t, tlstest.PeerOptions{Tickets: true})
	wrong := pinnedEntry(t, peer, "strict")
	wrong.SPKIPin = SPKIFingerprint(leafOf(t, func() []byte { c, _, _ := tlstest.MintServerCert(t, "127.0.0.1"); return c }()))
	if _, err := cliGet(t, wrong); err == nil || !strings.Contains(err.Error(), "does not match the pin") {
		t.Fatalf("strict must keep the SPKI pin check: %v", err)
	}
	if peer.Hits() != 0 {
		t.Fatal("bearer delivered to a peer whose pin did not match")
	}
	client, err := NewClient(pinnedEntry(t, peer, "strict"), cliCanary)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		resp, err := client.HTTP.Get(peer.URL + "/api/v1/meta")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		client.HTTP.CloseIdleConnections() // force a real reconnect
	}
	states := peer.States()
	if len(states) != 2 {
		t.Fatalf("expected two fresh connections, got %d", len(states))
	}
	for _, s := range states {
		if s.DidResume || s.CurveID != tls.X25519MLKEM768 {
			t.Fatalf("reconnect resumed or weakened: %+v", s)
		}
	}
}

func TestCLITrustEntryProfileIsValidatedAndSticky(t *testing.T) {
	https := TrustEntry{Name: "a", Origin: "https://hikyo.example.test", TLSMode: TLSModeSystemCA}
	for _, bad := range []string{"Strict", "pq", " strict"} {
		e := https
		e.TLSProfile = bad
		if err := validateTLSMode(e); err == nil {
			t.Fatalf("profile %q accepted", bad)
		}
	}
	local := TrustEntry{Name: "b", Origin: "http://127.0.0.1:8080", CLISocket: "/tmp/x.sock", TLSProfile: "strict"}
	if err := validateTLSMode(local); err == nil {
		t.Fatal("strict must refuse a local socket entry: it is not a TLS connection")
	}
	store := &TrustStore{dir: t.TempDir()}
	strict := https
	strict.TLSProfile = "strict"
	if err := store.Put(strict); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(https); err == nil || !strings.Contains(err.Error(), "TLS profile") {
		t.Fatalf("relaxing an established strict entry must need deliberate re-establishment: %v", err)
	}
	entries, err := store.Load()
	if err != nil || entries["a"].TLSProfile != "strict" {
		t.Fatalf("round trip lost the profile: %v %+v", err, entries["a"])
	}
	if effectiveTLSProfile(TrustEntry{}) != string(tlspolicy.ProfileCompat) {
		t.Fatal("omitted profile is compat")
	}
}

func TestCLIProvisionedBundleCarriesTheProfile(t *testing.T) {
	var bundle TrustBundle
	if err := decodeTrustJSON([]byte(`{"name":"a","origin":"https://hikyo.example.test","tls_mode":"system-ca","tls_profile":"strict"}`), &bundle); err != nil || bundle.TLSProfile != "strict" {
		t.Fatalf("bundle did not decode the profile: %v", err)
	}
}

func TestCLIReportsObservedNegotiationUnderDiagnostics(t *testing.T) {
	peer := tlstest.StartPeer(t, tlstest.PeerOptions{})
	var out strings.Builder
	ctx := diagnostics.With(t.Context(), 2, &out)
	client, err := NewClient(pinnedEntry(t, peer, "strict"), cliCanary)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, peer.URL+"/api/v1/meta?secret=1", nil)
	req.Header.Set("Authorization", "Bearer "+cliCanary)
	resp, err := client.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	want := "TLS hop=cli-client profile=strict version=tls1.3 group=x25519mlkem768 resumed=false evidence=observed"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("diagnostics = %q, want %q", out.String(), want)
	}
	for _, leak := range []string{cliCanary, "secret=1", "Authorization"} {
		if strings.Contains(out.String(), leak) {
			t.Fatalf("diagnostics leaked %q", leak)
		}
	}
}

// Narrow by design: this pins the CURRENT decoder (unknown fields disallowed),
// using the pre-#1127 entry shape. It proves nothing about older binaries, whose
// decoder (before 7dc30f973) used json.Unmarshal and silently drops tls_profile.
func TestCurrentTrustDecoderRefusesAFieldOutsideItsEntryShape(t *testing.T) {
	type legacyEntry struct {
		Name      string  `json:"name"`
		Origin    string  `json:"origin"`
		SPKIPin   string  `json:"spki_pin,omitempty"`
		TLSMode   TLSMode `json:"tls_mode,omitempty"`
		CLISocket string  `json:"cli_socket,omitempty"`
	}
	var legacy map[string]legacyEntry
	raw := []byte(`{"a":{"name":"a","origin":"https://hikyo.example.test","tls_mode":"system-ca","tls_profile":"strict"}}`)
	if err := decodeTrustJSON(raw, &legacy); err == nil {
		t.Fatal("a reader that does not know tls_profile must refuse the entry, not ignore the field")
	}
}

// The profile binds to the entry name, so two names for one origin with different
// profiles would let a lookup by origin pick either one.
func TestCLIRefusesSecondNameForAnOriginWithADifferentProfile(t *testing.T) {
	store := &TrustStore{dir: t.TempDir()}
	strict := TrustEntry{Name: "prod", Origin: "https://hikyo.example.test", TLSMode: TLSModeSystemCA, TLSProfile: "strict"}
	if err := store.Put(strict); err != nil {
		t.Fatal(err)
	}
	compat := strict
	compat.Name, compat.TLSProfile = "prod-alias", ""
	if err := store.Put(compat); err == nil || !strings.Contains(err.Error(), "different TLS profile") {
		t.Fatalf("a compat alias beside a strict entry must be refused: %v", err)
	}
	// The same profile under another name stays allowed.
	same := strict
	same.Name = "prod-alias"
	if err := store.Put(same); err != nil {
		t.Fatalf("an identical-policy alias must stay valid: %v", err)
	}
}

func TestLookupByOriginRefusesAmbiguousPoliciesDeterministically(t *testing.T) {
	state := &State{dir: t.TempDir()}
	store := state.Trust()
	// Profiles are refused at load; modes and pins can still differ for one origin.
	pin := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	entries := map[string]TrustEntry{
		"a": {Name: "a", Origin: "https://hikyo.example.test", TLSMode: TLSModeSystemCA, TLSProfile: "strict"},
		"b": {Name: "b", Origin: "https://hikyo.example.test", TLSMode: TLSModeSPKI, SPKIPin: pin, TLSProfile: "strict"},
	}
	if err := store.write(entries); err != nil {
		t.Fatal(err)
	}
	for range 200 { // map order is random; the refusal must not depend on it
		if _, err := lookupByOrigin(state, "https://hikyo.example.test", "hikyo.yaml"); err == nil || !strings.Contains(err.Error(), "different verification policies") {
			t.Fatalf("ambiguous origin must be refused every time: %v", err)
		}
	}
	entries["b"] = TrustEntry{Name: "b", Origin: "https://hikyo.example.test", TLSMode: TLSModeSystemCA, TLSProfile: "strict"}
	if err := store.write(entries); err != nil {
		t.Fatal(err)
	}
	got, err := lookupByOrigin(state, "https://hikyo.example.test", "hikyo.yaml")
	if err != nil || got.TLSProfile != "strict" {
		t.Fatalf("identical policies resolve: %v %+v", err, got)
	}
}

// The default port and a trailing dot name the same server, so they cannot be
// used to keep a compat entry beside a strict one.
func TestEquivalentOriginSpellingsShareTheProfileRules(t *testing.T) {
	variants := []string{"https://h.example.test:443", "https://h.example.test.", "https://H.EXAMPLE.TEST.:443"}
	for _, variant := range variants {
		store := &TrustStore{dir: t.TempDir()}
		strict := TrustEntry{Name: "prod", Origin: "https://h.example.test", TLSMode: TLSModeSystemCA, TLSProfile: "strict"}
		if err := store.Put(strict); err != nil {
			t.Fatal(err)
		}
		canonical, err := CanonicalOrigin(variant)
		if err != nil {
			t.Fatal(err)
		}
		alias := TrustEntry{Name: "alias", Origin: canonical, TLSMode: TLSModeSystemCA}
		if err := store.Put(alias); err == nil || !strings.Contains(err.Error(), "different TLS profile") {
			t.Fatalf("Put accepted a compat entry for %s beside a strict one: %v", variant, err)
		}
		// A pre-existing store holding both is refused at load, not resolved by name.
		both := map[string]TrustEntry{"prod": strict, "alias": alias}
		if err := store.write(both); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "different TLS profiles") || !strings.Contains(err.Error(), "hikyo context delete") {
			t.Fatalf("Load must refuse equivalent origins with different profiles (%s): %v", variant, err)
		}
		if _, err := store.Lookup("alias"); err == nil {
			t.Fatalf("Lookup by name must not return the compat alias (%s)", variant)
		}
	}
}

func TestLookupByOriginMatchesEquivalentSpellings(t *testing.T) {
	state := &State{dir: t.TempDir()}
	if err := state.Trust().write(map[string]TrustEntry{"prod": {Name: "prod", Origin: "https://h.example.test:443", TLSMode: TLSModeSystemCA, TLSProfile: "strict"}}); err != nil {
		t.Fatal(err)
	}
	for _, asked := range []string{"https://h.example.test", "https://h.example.test.", "https://h.example.test:443"} {
		got, err := lookupByOrigin(state, asked, "hikyo.yaml")
		if err != nil || got.TLSProfile != "strict" {
			t.Fatalf("lookup %s: %v %+v", asked, err, got)
		}
	}
}

// Stores without any strict entry keep loading as they did.
func TestStoresWithoutAStrictEntryStillLoadWithEquivalentOrigins(t *testing.T) {
	store := &TrustStore{dir: t.TempDir()}
	entries := map[string]TrustEntry{
		"a": {Name: "a", Origin: "https://h.example.test", TLSMode: TLSModeSystemCA},
		"b": {Name: "b", Origin: "https://h.example.test:443", TLSMode: TLSModeSystemCA, TLSProfile: "compat"},
	}
	if err := store.write(entries); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err != nil {
		t.Fatalf("a compat-only store must load unchanged: %v", err)
	}
}

func TestOriginSpellingsCompareByNumericPortAndParsedIPLiteral(t *testing.T) {
	same := [][2]string{
		{"https://h.example.test", "https://h.example.test:0443"},
		{"https://h.example.test:443", "https://H.EXAMPLE.TEST.:00443"},
		{"http://h.example.test", "http://h.example.test:0080"},
		{"https://h.example.test:8443", "https://h.example.test:08443"},
		{"https://[::1]:8443", "https://[0:0:0:0:0:0:0:1]:8443"},
		{"https://[2001:db8::1]", "https://[2001:0DB8:0:0:0:0:0:1]:443"},
		{"https://[::ffff:192.0.2.1]", "https://192.0.2.1"},
	}
	for _, p := range same {
		if !sameOrigin(p[0], p[1]) {
			t.Errorf("%s and %s name one server", p[0], p[1])
		}
	}
	different := [][2]string{
		{"https://h.example.test", "https://h.example.test:8443"},
		{"https://h.example.test", "http://h.example.test"},
		{"https://h.example.test:443", "https://h.example.test:4430"},
		{"https://[::1]:8443", "https://[::2]:8443"},
		{"https://h.example.test", "https://h2.example.test"},
	}
	for _, p := range different {
		if sameOrigin(p[0], p[1]) {
			t.Errorf("%s and %s are different servers", p[0], p[1])
		}
	}
}

func TestPutRefusesLeadingZeroPortAndExpandedIPv6AliasesWithADifferentProfile(t *testing.T) {
	for _, pair := range [][2]string{
		{"https://h.example.test", "https://h.example.test:0443"},
		{"https://[::1]:8443", "https://[0:0:0:0:0:0:0:1]:8443"},
	} {
		store := &TrustStore{dir: t.TempDir()}
		if err := store.Put(TrustEntry{Name: "prod", Origin: pair[0], TLSMode: TLSModeSystemCA, TLSProfile: "strict"}); err != nil {
			t.Fatal(err)
		}
		if err := store.Put(TrustEntry{Name: "alias", Origin: pair[1], TLSMode: TLSModeSystemCA}); err == nil || !strings.Contains(err.Error(), "different TLS profile") {
			t.Fatalf("compat alias %s accepted beside strict %s: %v", pair[1], pair[0], err)
		}
	}
}

// The refusal tells the user to run `hikyo context delete --instance <name>`.
// That exact command must work on the store the refusal describes.
func TestContextDeleteRepairsAStoreRefusedForConflictingProfiles(t *testing.T) {
	dir := t.TempDir()
	store := &TrustStore{dir: dir}
	strict := TrustEntry{Name: "prod", Origin: "https://h.example.test", TLSMode: TLSModeSystemCA, TLSProfile: "strict"}
	alias := TrustEntry{Name: "alias", Origin: "https://h.example.test:443", TLSMode: TLSModeSystemCA}
	if err := store.write(map[string]TrustEntry{"prod": strict, "alias": alias}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("fixture must be refused at load")
	}
	var stdout, stderr strings.Builder
	ios := IO{Stdout: &stdout, Stderr: &stderr, Env: Env{Getenv: func(k string) string {
		if k == "HIKYO_STATE_DIR" {
			return dir
		}
		return ""
	}}, Workdir: t.TempDir()}
	if code := Run(t.Context(), ios, []string{"context", "delete", "--instance", "alias"}); code != 0 {
		t.Fatalf("the named repair step failed with %d: %s", code, stderr.String())
	}
	entries, err := store.Load()
	if err != nil || len(entries) != 1 || entries["prod"].TLSProfile != "strict" {
		t.Fatalf("repaired store = %v, %+v", err, entries)
	}
}

// Skipping the conflict check must not skip any other validation.
func TestDeleteStillRefusesAnInvalidStore(t *testing.T) {
	store := &TrustStore{dir: t.TempDir()}
	bad := map[string]TrustEntry{"prod": {Name: "wrong-name", Origin: "https://h.example.test", TLSMode: TLSModeSystemCA}}
	if err := store.write(bad); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("prod"); err == nil || !strings.Contains(err.Error(), "invalid entry") {
		t.Fatalf("Delete must keep validating entries: %v", err)
	}
}

func TestLookupByOriginIsDeterministicAcrossSamePolicySpellings(t *testing.T) {
	state := &State{dir: t.TempDir()}
	entries := map[string]TrustEntry{
		"z": {Name: "z", Origin: "https://h.example.test", TLSMode: TLSModeSystemCA},
		"a": {Name: "a", Origin: "https://h.example.test:443", TLSMode: TLSModeSystemCA},
		"b": {Name: "b", Origin: "https://h.example.test:0443", TLSMode: TLSModeSystemCA},
	}
	if err := state.Trust().write(entries); err != nil {
		t.Fatal(err)
	}
	for range 200 {
		if got, err := lookupByOrigin(state, "https://h.example.test", "c"); err != nil || got.Name != "z" {
			t.Fatalf("an exact spelling must win: %v %+v", err, got)
		}
		if got, err := lookupByOrigin(state, "https://h.example.test.", "c"); err != nil || got.Name != "a" {
			t.Fatalf("with no exact spelling the lowest name must win: %v %+v", err, got)
		}
	}
}

func TestDeveloperCredentialDeleteByOriginMatchesEquivalentSpellings(t *testing.T) {
	st := &State{dir: t.TempDir()}
	token, _, err := crypto.NewArtifact(crypto.ArtifactDeveloperCredential)
	if err != nil {
		t.Fatal(err)
	}
	c := DeveloperCredentialArtifact{ID: "dev_01989abc-def0-7123-8123-000000000004", Origin: "https://h.example.test:443", Org: "o", Project: "p", Environment: "e", Token: token, ExpiresAt: time.Now().Add(time.Hour)}
	if err := st.PutDeveloperCredential(c); err != nil {
		t.Fatal(err)
	}
	if err := st.deleteDeveloperCredentials("https://h.example.test", ""); err != nil {
		t.Fatal(err)
	}
	if all, err := st.DeveloperCredentials(); err != nil || len(all) != 0 {
		t.Fatalf("an equivalent origin spelling must delete the credential: %v %v", err, all)
	}
}
