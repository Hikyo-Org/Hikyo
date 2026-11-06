package tlspolicy_test

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

const canary = "Bearer canary-credential-1127"

// call performs one real HTTPS request through net/http with a fresh
// connection, carrying the canary credential, and returns the client-side TLS
// state of the response.
func call(t *testing.T, cfg *tls.Config, url string) (*tls.ConnectionState, error) {
	t.Helper()
	transport := &http.Transport{TLSClientConfig: cfg, DisableKeepAlives: true}
	t.Cleanup(transport.CloseIdleConnections)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", canary)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return resp.TLS, nil
}

func client(pool *x509.CertPool, profile tlspolicy.Profile) *tls.Config {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
	profile.Apply(cfg)
	return cfg
}

func TestParseProfileIsClosedAndExact(t *testing.T) {
	for _, ok := range []string{"compat", "strict"} {
		if p, err := tlspolicy.ParseProfile(ok); err != nil || string(p) != ok {
			t.Fatalf("ParseProfile(%q) = %q, %v", ok, p, err)
		}
	}
	for _, bad := range []string{"", "Strict", " strict", "pq", "hybrid", "strict "} {
		if _, err := tlspolicy.ParseProfile(bad); err == nil {
			t.Fatalf("ParseProfile(%q) accepted an unknown profile", bad)
		}
	}
}

func TestCompatApplyChangesNothing(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	tlspolicy.ProfileCompat.Apply(cfg)
	if cfg.MinVersion != tls.VersionTLS12 || cfg.MaxVersion != 0 || cfg.CurvePreferences != nil || cfg.SessionTicketsDisabled {
		t.Fatalf("compat must be today's behavior, got %+v", cfg)
	}
	zero := &tls.Config{}
	tlspolicy.Profile("").Apply(zero)
	if zero.MinVersion != 0 || zero.MaxVersion != 0 || zero.CurvePreferences != nil {
		t.Fatalf("the zero profile is compat and must not change a config, got %+v", zero)
	}
}

func TestStrictApplyOverwritesWeakerCallerSettings(t *testing.T) {
	cache := tls.NewLRUClientSessionCache(4)
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12,
		CurvePreferences:       []tls.CurveID{tls.X25519, tls.CurveP256},
		ClientSessionCache:     cache,
		SessionTicketsDisabled: false,
		ServerName:             "keep.example",
		InsecureSkipVerify:     true, // a pin-based hop replaces chain checks; Apply must not touch it
	}
	tlspolicy.ProfileStrict.Apply(cfg)
	if cfg.MinVersion != tls.VersionTLS13 || cfg.MaxVersion != tls.VersionTLS13 {
		t.Fatalf("strict must pin TLS 1.3, got %x-%x", cfg.MinVersion, cfg.MaxVersion)
	}
	if len(cfg.CurvePreferences) != 1 || cfg.CurvePreferences[0] != tls.X25519MLKEM768 {
		t.Fatalf("strict groups = %v, want exactly [X25519MLKEM768]", cfg.CurvePreferences)
	}
	if !cfg.SessionTicketsDisabled || cfg.ClientSessionCache != nil {
		t.Fatal("strict must disable session tickets and drop any client session cache")
	}
	if cfg.ServerName != "keep.example" || !cfg.InsecureSkipVerify {
		t.Fatal("Apply must not touch trust fields; pin, CA and hostname checks belong to the hop")
	}
}

func TestUnknownProfileFailsClosedToStrict(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	tlspolicy.Profile("future").Apply(cfg)
	if cfg.MinVersion != tls.VersionTLS13 || len(cfg.CurvePreferences) != 1 {
		t.Fatal("an unrecognized profile value must never weaken to compat")
	}
}

func TestHandshakeMatrix(t *testing.T) {
	cases := []struct {
		name       string
		peer       tlstest.PeerOptions
		compatOK   bool
		compatCurv tls.CurveID
		strictOK   bool
	}{
		{"hybrid peer", tlstest.PeerOptions{}, true, tls.X25519MLKEM768, true},
		{"TLS 1.3 only classical peer", tlstest.PeerOptions{MinVersion: tls.VersionTLS13, Curves: []tls.CurveID{tls.X25519, tls.CurveP256}}, true, tls.X25519, false},
		{"TLS 1.2 only peer", tlstest.PeerOptions{MaxVersion: tls.VersionTLS12}, true, tls.X25519, false},
		// The client's first key share is hybrid, so the server's own preference order does not downgrade it.
		{"peer prefers classical but offers hybrid", tlstest.PeerOptions{Curves: []tls.CurveID{tls.X25519, tls.X25519MLKEM768}}, true, tls.X25519MLKEM768, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compat := tlstest.StartPeer(t, tc.peer)
			state, err := call(t, client(compat.Pool, tlspolicy.ProfileCompat), compat.URL)
			if (err == nil) != tc.compatOK {
				t.Fatalf("compat ok=%v, err=%v", tc.compatOK, err)
			}
			if err == nil && state.CurveID != tc.compatCurv {
				t.Fatalf("compat negotiated %v, want %v", state.CurveID, tc.compatCurv)
			}

			strict := tlstest.StartPeer(t, tc.peer)
			state, err = call(t, client(strict.Pool, tlspolicy.ProfileStrict), strict.URL)
			if (err == nil) != tc.strictOK {
				t.Fatalf("strict ok=%v, err=%v", tc.strictOK, err)
			}
			if err == nil {
				if state.Version != tls.VersionTLS13 || state.CurveID != tls.X25519MLKEM768 {
					t.Fatalf("strict negotiated version %x group %v", state.Version, state.CurveID)
				}
				return
			}
			// A refused strict handshake must not have delivered the credential:
			// the server-side handler never ran and the bytes never reached it.
			if strict.Hits() != 0 || len(strict.Authorizations()) != 0 || strict.RawContains(canary) {
				t.Fatalf("credential reached a peer that strict must have refused: hits=%d", strict.Hits())
			}
		})
	}
}

func TestStrictKeepsCertificateAndHostnameVerification(t *testing.T) {
	good := tlstest.StartPeer(t, tlstest.PeerOptions{})
	if _, err := call(t, client(x509.NewCertPool(), tlspolicy.ProfileStrict), good.URL); err == nil {
		t.Fatal("strict must still refuse an untrusted certificate")
	}
	if good.Hits() != 0 {
		t.Fatal("canary reached a peer whose certificate the client does not trust")
	}
	wrongName := tlstest.StartPeer(t, tlstest.PeerOptions{Hosts: []string{"other.example"}})
	if _, err := call(t, client(wrongName.Pool, tlspolicy.ProfileStrict), wrongName.URL); err == nil {
		t.Fatal("strict must still refuse a hostname mismatch")
	}
	if wrongName.Hits() != 0 {
		t.Fatal("canary reached a peer with the wrong hostname")
	}
}

func TestResumptionFollowsProfile(t *testing.T) {
	dial := func(profile tlspolicy.Profile) []tls.ConnectionState {
		peer := tlstest.StartPeer(t, tlstest.PeerOptions{Tickets: true})
		cfg := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: peer.Pool, ClientSessionCache: tls.NewLRUClientSessionCache(8)}
		profile.Apply(cfg)
		for range 2 {
			if _, err := call(t, cfg, peer.URL); err != nil {
				t.Fatal(err)
			}
		}
		return peer.States()
	}
	compat := dial(tlspolicy.ProfileCompat)
	if len(compat) != 2 || !compat[1].DidResume {
		t.Fatalf("control: compat with a session cache should resume, states=%d", len(compat))
	}
	strict := dial(tlspolicy.ProfileStrict)
	if len(strict) != 2 {
		t.Fatalf("strict states = %d", len(strict))
	}
	for i, s := range strict {
		if s.DidResume {
			t.Fatalf("strict connection %d resumed a session; tickets must be off", i)
		}
		if s.CurveID != tls.X25519MLKEM768 {
			t.Fatalf("strict reconnect %d negotiated %v", i, s.CurveID)
		}
	}
}

func TestNamesAreClosedAndUnknownIsNeverInferred(t *testing.T) {
	if got := tlspolicy.GroupName(tls.X25519MLKEM768); got != "x25519mlkem768" {
		t.Fatalf("hybrid group name = %q", got)
	}
	if got := tlspolicy.GroupName(tls.X25519); got != "x25519" {
		t.Fatalf("classical group name = %q", got)
	}
	for _, id := range []tls.CurveID{0, 0x9999, tls.SecP256r1MLKEM768} {
		name := tlspolicy.GroupName(id)
		if name == "x25519mlkem768" || name == "x25519" {
			t.Fatalf("group %v must not be inferred as %q", id, name)
		}
	}
	if tlspolicy.GroupName(0) != "unknown" || tlspolicy.GroupName(0x9999) != "unknown" {
		t.Fatal("a missing or unmapped group must be reported unknown, not classical or hybrid")
	}
	if tlspolicy.VersionName(tls.VersionTLS13) != "tls1.3" || tlspolicy.VersionName(0x7f) != "unknown" {
		t.Fatal("version names must be a closed set")
	}
}

func TestObserveRecordsTheServerHandshake(t *testing.T) {
	certPEM, keyPEM, _ := tlstest.MintServerCert(t, "localhost", "127.0.0.1")
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	var seen []tlspolicy.Negotiation
	var mu sync.Mutex
	server := &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	tlspolicy.ProfileStrict.Apply(server)
	tlspolicy.Observe(server, tlspolicy.HopNativeListener, tlspolicy.ProfileStrict, func(n tlspolicy.Negotiation) {
		mu.Lock()
		seen = append(seen, n)
		mu.Unlock()
	})
	ln, err := tls.Listen("tcp", "127.0.0.1:0", server)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = conn.Read(make([]byte, 1)); conn.Close() }()
		}
	}()
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	for _, c := range []*tls.Config{client(pool, tlspolicy.ProfileStrict), client(pool, tlspolicy.ProfileCompat)} {
		conn, err := tls.Dial("tcp", ln.Addr().String(), c)
		if err == nil {
			conn.Close()
		}
	}
	mu.Lock()
	defer mu.Unlock()
	// The compat client offers hybrid by default, but this listener is strict,
	// so both clients meet the same policy: one observed negotiation each.
	for _, n := range seen {
		if n.Hop != tlspolicy.HopNativeListener || n.Profile != tlspolicy.ProfileStrict || n.Version != "tls1.3" || n.Group != "x25519mlkem768" {
			t.Fatalf("unexpected observation %+v", n)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no negotiation observed")
	}
}

// The child runs with GODEBUG removing the default hybrid group. Strict must
// either negotiate the hybrid group or refuse; it must never fall back.
func TestStrictUnderGODEBUGChild(t *testing.T) {
	if os.Getenv("HIKYO_TLSPOLICY_CHILD") != "1" {
		t.Skip("subprocess body")
	}
	peer := tlstest.StartPeer(t, tlstest.PeerOptions{Curves: []tls.CurveID{tls.X25519, tls.X25519MLKEM768}})
	report := func(label string, profile tlspolicy.Profile) {
		state, err := call(t, client(peer.Pool, profile), peer.URL)
		if err != nil {
			println("RESULT", label, "error")
			return
		}
		println("RESULT", label, tlspolicy.GroupName(state.CurveID))
	}
	report("compat", tlspolicy.ProfileCompat)
	report("strict", tlspolicy.ProfileStrict)
}

func TestStrictIgnoresGODEBUG(t *testing.T) {
	for _, godebug := range []string{"tlsmlkem=0", "tlsmlkem=0,tlssecpmlkem=0", "tls13=0"} {
		t.Run(godebug, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestStrictUnderGODEBUGChild$", "-test.v")
			cmd.Env = append(os.Environ(), "HIKYO_TLSPOLICY_CHILD=1", "GODEBUG="+godebug)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("child failed: %v\n%s", err, out)
			}
			results := map[string]string{}
			for _, line := range strings.Split(string(out), "\n") {
				if f := strings.Fields(line); len(f) == 3 && f[0] == "RESULT" {
					results[f[1]] = f[2]
				}
			}
			t.Logf("GODEBUG=%s: %v", godebug, results)
			if results["strict"] != "x25519mlkem768" && results["strict"] != "error" {
				t.Fatalf("strict weakened under GODEBUG=%s: %v\n%s", godebug, results, out)
			}
			if results["strict"] == "" {
				t.Fatalf("child produced no strict result:\n%s", out)
			}
		})
	}
}

// DEC-06 allows exactly one group. Another Go hybrid, a group this build does
// not know, and a peer that offers only those must all be refused by strict
// before the credential leaves, without panicking.
func TestStrictRefusesOtherAndUnknownGroups(t *testing.T) {
	for name, curves := range map[string][]tls.CurveID{
		"other Go hybrid":     {tls.SecP256r1MLKEM768},
		"unmapped group":      {tls.CurveID(0x6399)},
		"classical and other": {tls.CurveP256, tls.SecP384r1MLKEM1024},
	} {
		t.Run(name, func(t *testing.T) {
			peer := tlstest.StartPeer(t, tlstest.PeerOptions{Curves: curves})
			if _, err := call(t, client(peer.Pool, tlspolicy.ProfileStrict), peer.URL); err == nil {
				t.Fatalf("strict negotiated with a peer offering only %v", curves)
			}
			if peer.Hits() != 0 || peer.RawContains(canary) {
				t.Fatal("credential reached a peer strict must have refused")
			}
		})
	}
}

// A caller that pre-set weaker groups, versions and a session cache gets none of
// them back: the real handshake, not only the config, shows strict.
func TestOverriddenClientSettingsDoNotSurviveAHandshake(t *testing.T) {
	peer := tlstest.StartPeer(t, tlstest.PeerOptions{Tickets: true})
	cfg := &tls.Config{
		RootCAs: peer.Pool, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12,
		CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256}, ClientSessionCache: tls.NewLRUClientSessionCache(4),
	}
	tlspolicy.ProfileStrict.Apply(cfg)
	for range 2 {
		state, err := call(t, cfg, peer.URL)
		if err != nil {
			t.Fatal(err)
		}
		if state.Version != tls.VersionTLS13 || state.CurveID != tls.X25519MLKEM768 || state.DidResume {
			t.Fatalf("overridden settings leaked into the handshake: %x %v resumed=%v", state.Version, state.CurveID, state.DidResume)
		}
	}
}

// The strict listener side under GODEBUG: the server's explicit group list does
// not depend on the process default that tlsmlkem=0 removes.
func TestStrictListenerUnderGODEBUGChild(t *testing.T) {
	if os.Getenv("HIKYO_TLSPOLICY_CHILD") != "1" {
		t.Skip("subprocess body")
	}
	certPEM, keyPEM, _ := tlstest.MintServerCert(t, "localhost", "127.0.0.1")
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	server := &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	tlspolicy.ProfileStrict.Apply(server)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", server)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = conn.Read(make([]byte, 1)); conn.Close() }()
		}
	}()
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	for label, cfg := range map[string]*tls.Config{
		"strict-client":    client(roots, tlspolicy.ProfileStrict),
		"classical-client": {RootCAs: roots, MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}},
		"default-client":   {RootCAs: roots},
	} {
		conn, err := tls.Dial("tcp", ln.Addr().String(), cfg)
		if err != nil {
			println("RESULT", label, "error")
			continue
		}
		println("RESULT", label, tlspolicy.GroupName(conn.ConnectionState().CurveID))
		conn.Close()
	}
}

func TestStrictListenerIgnoresGODEBUG(t *testing.T) {
	for _, godebug := range []string{"tlsmlkem=0", "tlsmlkem=0,tlssecpmlkem=0"} {
		t.Run(godebug, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestStrictListenerUnderGODEBUGChild$", "-test.v")
			cmd.Env = append(os.Environ(), "HIKYO_TLSPOLICY_CHILD=1", "GODEBUG="+godebug)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("child failed: %v\n%s", err, out)
			}
			results := map[string]string{}
			for _, line := range strings.Split(string(out), "\n") {
				if f := strings.Fields(line); len(f) == 3 && f[0] == "RESULT" {
					results[f[1]] = f[2]
				}
			}
			if results["strict-client"] != "x25519mlkem768" {
				t.Fatalf("strict client against a strict listener under %s: %v\n%s", godebug, results, out)
			}
			for _, weak := range []string{"classical-client", "default-client"} {
				if results[weak] != "error" {
					t.Fatalf("%s was served by a strict listener under %s: %v", weak, godebug, results)
				}
			}
		})
	}
}
