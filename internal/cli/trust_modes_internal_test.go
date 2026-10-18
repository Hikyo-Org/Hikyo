package cli

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
)

func TestTrustModesValidateAndRequireDeliberateReestablishment(t *testing.T) {
	pin := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	valid := TrustEntry{Name: "ci", Origin: "https://secrets.example", TLSMode: TLSModeSystemCA}
	for _, tc := range []struct {
		name  string
		entry TrustEntry
		valid bool
	}{
		{"CA", valid, true},
		{"legacy SPKI", TrustEntry{Name: "ci", Origin: valid.Origin, SPKIPin: pin}, true},
		{"explicit SPKI", TrustEntry{Name: "ci", Origin: valid.Origin, TLSMode: TLSModeSPKI, SPKIPin: pin}, true},
		{"missing legacy pin", TrustEntry{Name: "ci", Origin: valid.Origin}, false},
		{"missing explicit pin", TrustEntry{Name: "ci", Origin: valid.Origin, TLSMode: TLSModeSPKI}, false},
		{"CA pin conflict", TrustEntry{Name: "ci", Origin: valid.Origin, TLSMode: TLSModeSystemCA, SPKIPin: pin}, false},
		{"CA socket conflict", TrustEntry{Name: "ci", Origin: valid.Origin, TLSMode: TLSModeSystemCA, CLISocket: "/tmp/hikyo.sock"}, false},
		{"unknown", TrustEntry{Name: "ci", Origin: valid.Origin, TLSMode: "ca"}, false},
		{"HTTP mode", TrustEntry{Name: "ci", Origin: "http://127.0.0.1:8080", TLSMode: TLSModeSystemCA}, false},
		{"HTTP SPKI mode", TrustEntry{Name: "ci", Origin: "http://127.0.0.1:8080", TLSMode: TLSModeSPKI}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTrustEntry(tc.entry.Name, tc.entry)
			if (err == nil) != tc.valid {
				t.Fatalf("validation=%v, valid=%v", err, tc.valid)
			}
			_, clientErr := NewClient(tc.entry, "credential")
			if !tc.valid && clientErr == nil {
				t.Fatal("invalid trust accepted by client")
			}
		})
	}
	store := (&State{dir: t.TempDir()}).Trust()
	legacy := TrustEntry{Name: "ci", Origin: valid.Origin, SPKIPin: pin}
	if err := store.Put(legacy); err != nil {
		t.Fatal(err)
	}
	explicit := legacy
	explicit.TLSMode = TLSModeSPKI
	if err := store.Put(explicit); err != nil {
		t.Fatalf("equivalent explicit mode: %v", err)
	}
	for _, changed := range []TrustEntry{valid, {Name: "ci", Origin: "https://other.example", SPKIPin: pin}, {Name: "ci", Origin: valid.Origin, SPKIPin: "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBA="}} {
		if err := store.Put(changed); err == nil {
			t.Fatal("identity change overwrote trust")
		}
	}
	if err := store.Delete("ci"); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(valid); err != nil {
		t.Fatalf("deliberate mode reestablishment: %v", err)
	}
	if err := store.Put(legacy); err == nil {
		t.Fatal("CA to SPKI change overwrote trust")
	}
}

func TestProvisionedTrustModeAndRepositoryRefusals(t *testing.T) {
	state := &State{dir: t.TempDir()}
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "bundle.json")
	ios := IO{Stderr: io.Discard, Env: Env{Getenv: func(string) string { return "" }}}
	write := func(raw string) {
		t.Helper()
		if err := os.WriteFile(bundlePath, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"name":"ci","origin":"https://secrets.invalid","tls_mode":"system-ca"}`)
	entry, err := establishWithTLSMode(ios, state, "https://secrets.invalid", "", bundlePath, "", "")
	if err != nil || entry.TLSMode != TLSModeSystemCA {
		t.Fatalf("provisioned CA=%+v %v", entry, err)
	}
	before, err := os.ReadFile(state.Trust().path())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, target, mode, raw string }{
		{"origin substitution", "https://attacker.invalid", "", `{"name":"ci","origin":"https://secrets.invalid","tls_mode":"system-ca"}`},
		{"reference substitution", "other", "", `{"name":"ci","origin":"https://secrets.invalid","tls_mode":"system-ca"}`},
		{"flag mode substitution", "ci", TLSModeSPKI, `{"name":"ci","origin":"https://secrets.invalid","tls_mode":"system-ca"}`},
		{"empty mode", "ci", "", `{"name":"ci","origin":"https://secrets.invalid","tls_mode":"","spki_pin":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`},
		{"null mode", "ci", "", `{"name":"ci","origin":"https://secrets.invalid","tls_mode":null,"spki_pin":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`},
		{"unknown mode", "ci", "", `{"name":"ci","origin":"https://secrets.invalid","tls_mode":"insecure"}`},
		{"pin conflict", "ci", "", `{"name":"ci","origin":"https://secrets.invalid","tls_mode":"system-ca","spki_pin":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`},
		{"socket conflict", "ci", "", `{"name":"ci","origin":"https://secrets.invalid","tls_mode":"system-ca","cli_socket":"/tmp/sock"}`},
		{"unknown field", "ci", "", `{"name":"ci","origin":"https://secrets.invalid","tls_mode":"system-ca","tls_mod":"system-ca"}`},
		{"missing pin", "ci", "", `{"name":"ci","origin":"https://secrets.invalid"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			write(tc.raw)
			fresh := &State{dir: t.TempDir()}
			if _, err := establishWithTLSMode(ios, fresh, tc.target, "", bundlePath, "", tc.mode); err == nil {
				t.Fatal("unsafe bundle accepted into a fresh store")
			}
			entries, err := fresh.Trust().Load()
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed establishment recorded trust: entries=%v err=%v", entries, err)
			}
			if _, err := establishWithTLSMode(ios, state, tc.target, "", bundlePath, "", tc.mode); err == nil {
				t.Fatal("unsafe bundle accepted into an existing store")
			}
			after, err := os.ReadFile(state.Trust().path())
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("refusal changed existing trust store")
			}
		})
	}
	for _, raw := range []string{
		`{"instance":"ci","tls_mode":"spki"}`,
		`{"instance":"ci","origin":"https://attacker.invalid"}`,
		`{"instance":"ci","spki_pin":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, PinFileName), []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Resolve(state, ios.Env, Flags{}, dir); err == nil {
			t.Fatalf("repository trust introduction accepted: %s", raw)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, PinFileName), []byte(`{"instance":"https://attacker.invalid"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve(state, ios.Env, Flags{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.Trust().Lookup(resolved.Get(DimInstance)); err == nil {
		t.Fatal("repository origin became trusted")
	}
	for _, target := range []string{"ci", "https://secrets.invalid"} {
		if _, err := establishWithTLSMode(ios, state, target, "ci", "", "", TLSModeSPKI); err == nil {
			t.Fatal("flag changed established mode")
		}
	}
}

func TestEstablishmentTLSModeFlagRejectsInvalidValues(t *testing.T) {
	for _, mode := range []string{"", "insecure"} {
		for _, args := range [][]string{
			{"login", "https://secrets.invalid", "--local", "--as", "user", "--tls-mode=" + mode},
			{"context", "create", "ci", "--instance", "https://secrets.invalid", "--tls-mode=" + mode},
		} {
			ios, _, stderr := composeIO(t.TempDir(), t.TempDir(), "", nil)
			if code := Run(t.Context(), ios, args); code != ExitRefused || !strings.Contains(stderr.String(), "TLS mode must be") {
				t.Fatalf("args=%v exit=%d stderr=%s", args, code, stderr)
			}
		}
	}
}

// Keep the process-global fallback root pool isolated from every other test.
// This exercises the production client with its normal nil RootCAs and Go's
// normal verification, using Go's supported replacement system roots for tests.
func TestSystemCATrustCertificateLifecycle(t *testing.T) {
	if os.Getenv("HIKYO_TEST_CA_CHILD") != "1" {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSystemCATrustCertificateLifecycle$", "-test.v")
		cmd.Env = append(os.Environ(), "HIKYO_TEST_CA_CHILD=1", "GODEBUG="+os.Getenv("GODEBUG")+",x509usefallbackroots=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("CA lifecycle subprocess: %v\n%s", err, out)
		}
		return
	}
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "CLI test root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	x509.SetFallbackRoots(roots)
	serial := int64(1)
	leaf := func(expired, wrongHost bool) *tls.Certificate {
		t.Helper()
		serial++
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		template := &x509.Certificate{SerialNumber: big.NewInt(serial), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
		if expired {
			template.NotBefore = now.Add(-2 * time.Hour)
			template.NotAfter = now.Add(-time.Hour)
		}
		if wrongHost {
			template.IPAddresses = nil
			template.DNSNames = []string{"other.invalid"}
		}
		der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: parsed}
	}
	first, renewed := leaf(false, false), leaf(false, false)
	if SPKIFingerprint(first.Leaf) == SPKIFingerprint(renewed.Leaf) {
		t.Fatal("rotation did not change public key")
	}
	var current atomic.Pointer[tls.Certificate]
	current.Store(first)
	var requests atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(100); w.WriteHeader(http.StatusNoContent) }))
	defer sink.Close()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-credential" {
			t.Error("bound credential missing")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, sink.URL, http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/meta" {
			_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "test", ApiRevision: 999})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/environments") {
			_, _ = io.WriteString(w, `{"items":[],"count":0}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
		return &tls.Config{Certificates: []tls.Certificate{*current.Load()}, MinVersion: tls.VersionTLS12}, nil
	}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()
	entry := TrustEntry{Name: "ci", Origin: server.URL, TLSMode: TLSModeSystemCA}
	request := func(entry TrustEntry, path string) error {
		t.Helper()
		client, err := NewClient(entry, "test-credential")
		if err != nil {
			return err
		}
		defer client.HTTP.CloseIdleConnections()
		return client.Do(t.Context(), http.MethodGet, path, nil, nil)
	}
	for _, cert := range []*tls.Certificate{first, renewed} {
		current.Store(cert)
		if err := request(entry, "/probe"); err != nil {
			t.Fatalf("valid CA certificate rejected: %v", err)
		}
	}
	if requests.Load() != 2 {
		t.Fatalf("valid requests=%d", requests.Load())
	}
	untrusted := server.TLS.Certificates[0]
	for name, cert := range map[string]*tls.Certificate{"expired": leaf(true, false), "wrong hostname": leaf(false, true), "untrusted": &untrusted} {
		t.Run(name, func(t *testing.T) {
			current.Store(cert)
			if err := request(entry, "/probe"); err == nil {
				t.Fatal("invalid certificate accepted")
			}
			if requests.Load() != 2 {
				t.Fatal("credentials sent before certificate validation")
			}
		})
	}
	current.Store(renewed)
	pinned := TrustEntry{Name: "ci", Origin: server.URL, SPKIPin: SPKIFingerprint(first.Leaf)}
	if err := request(pinned, "/probe"); err == nil {
		t.Fatal("changed SPKI accepted")
	}
	if requests.Load() != 2 {
		t.Fatal("credentials sent after pin mismatch")
	}
	current.Store(first)
	if err := request(pinned, "/probe"); err != nil {
		t.Fatalf("legacy SPKI rejected: %v", err)
	}
	if err := request(entry, "/redirect"); err == nil {
		t.Fatal("redirect accepted")
	}
	if requests.Load() != 4 {
		t.Fatalf("redirect forwarded credentials or request: count=%d", requests.Load())
	}

	t.Run("interactive establishment", func(t *testing.T) {
		state := &State{dir: t.TempDir()}
		tty := &answeringTTY{in: strings.NewReader("y\n")}
		session, err := disclose.NewTerminalSession(tty)
		if err != nil {
			t.Fatal(err)
		}
		ios := IO{TerminalSession: session, Stderr: io.Discard, Env: Env{Getenv: func(string) string { return "" }}}
		// An invalid certificate must fail before recording trust or displaying a confirmation.
		current.Store(leaf(false, true))
		if _, err := establishWithTLSMode(ios, state, server.URL, "ci", "", "", TLSModeSystemCA); err == nil {
			t.Fatal("unverified CA origin recorded")
		}
		entries, err := state.Trust().Load()
		if err != nil || len(entries) != 0 {
			t.Fatal("failed establishment wrote trust")
		}
		if tty.out.Len() != 0 {
			t.Fatal("invalid CA certificate reached confirmation")
		}
		current.Store(renewed)
		established, err := establishWithTLSMode(ios, state, server.URL, "ci", "", "", TLSModeSystemCA)
		if err != nil {
			t.Fatal(err)
		}
		if established.TLSMode != TLSModeSystemCA || established.SPKIPin != "" {
			t.Fatalf("CA establishment=%+v", established)
		}
		for _, text := range []string{server.URL, "system-ca", "CA chain", "hostname", "keys may rotate"} {
			if !strings.Contains(tty.out.String(), text) {
				t.Errorf("confirmation omitted %q: %s", text, tty.out.String())
			}
		}
		if requests.Load() != 4 {
			t.Fatal("establishment sent an application request or credentials")
		}
	})
	t.Run("provisioned machine command", func(t *testing.T) {
		current.Store(renewed)
		stateDir, workdir := t.TempDir(), t.TempDir()
		bundlePath := filepath.Join(t.TempDir(), "trust.json")
		raw, err := json.Marshal(TrustBundle{Name: "ci", Origin: server.URL, TLSMode: TLSModeSystemCA})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bundlePath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		ios, stdout, stderr := composeIO(stateDir, workdir, "test-credential", map[string]string{"HIKYO_TRUST_BUNDLE": bundlePath})
		if code := Run(t.Context(), ios, []string{"context", "create", "ci-context", "--instance", "ci", "--tls-mode", "system-ca", "--org", "org_1", "--project", "project_1"}); code != 0 {
			t.Fatalf("provision context: exit=%d stderr=%s", code, stderr)
		}
		before := requests.Load()
		args := []string{"env", "list", "--context", "ci-context", "--auth", "machine", "-o", "json"}
		if code := Run(t.Context(), ios, args); code != 0 {
			t.Fatalf("machine read: exit=%d stderr=%s", code, stderr)
		}
		if !strings.Contains(stdout.String(), `"items"`) || requests.Load() != before+2 {
			t.Fatalf("machine read did not fetch meta and environments: output=%s requests=%d", stdout, requests.Load()-before)
		}
		if err := os.WriteFile(filepath.Join(workdir, PinFileName), []byte(`{"instance":"ci","tls_mode":"spki"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		before = requests.Load()
		if code := Run(t.Context(), ios, args); code != ExitRefused {
			t.Fatalf("repository mode introduction: exit=%d stderr=%s", code, stderr)
		}
		if requests.Load() != before {
			t.Fatal("repository mode introduction sent a credential")
		}
	})

}
