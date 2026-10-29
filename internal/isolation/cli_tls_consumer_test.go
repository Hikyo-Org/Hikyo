package isolation

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
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
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// Unlike a canned HTTP response, this consumer uses the real credential mint,
// bearer resolver, grants, API handler and SQLite store. Its only authority is
// a newly created disposable fixture. It proves no operational CI update.
// Set HIKYO_TLS_CONSUMER_BINARY to exercise an independently verified released
// CLI instead of cli.Run. Linux loads the fixture CA through SSL_CERT_FILE;
// fallback roots isolate the in-process source CLI from host trust on all OSes.
func TestCLISystemCARealAuthorizationConsumer(t *testing.T) {
	if os.Getenv("HIKYO_TLS_CONSUMER_CHILD") != "1" {
		command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestCLISystemCARealAuthorizationConsumer$", "-test.v")
		command.Env = append(os.Environ(), "HIKYO_TLS_CONSUMER_CHILD=1", "GODEBUG=x509usefallbackroots=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("isolated consumer: %v\n%s", err, output)
		}
		t.Log(string(output))
		return
	}

	db := openSQLite(t)
	wire := newAccessWireEnv(t, db)
	auth := wire.auth
	orgID, projectID, environmentID := domain.OrgID(wire.org), domain.ProjectID(wire.project), wire.env
	scope := domain.Scope{Org: orgID, Project: projectID}
	const siblingID = domain.ProjectID("prj_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f99")
	execRaw(t, db, fmt.Sprintf(`INSERT INTO projects (id, org_id, name, created_at) VALUES ('%s', '%s', 'tls-sibling', %s)`, siblingID, orgID, ts))
	for i, capability := range []string{"read", "manage-identities", "manage-members"} {
		execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_tls_admin_%d', '%s', '%s', '%s', '%s', NULL, %s)`, i, wire.admin, capability, orgID, projectID, ts))
	}
	seedOrigins(t, db)
	identities := &service.Identities{DB: db, Auth: auth}
	operator := service.LocalPrincipal(wire.admin)
	sa, err := identities.CreateServiceAccount(t.Context(), operator, scope, "tls-consumer", domain.ClassAutomation)
	if err != nil {
		t.Fatal(err)
	}
	minted, err := identities.MintCredential(t.Context(), operator, scope, sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := grantSvcWithAuth(db).Create(t.Context(), operator, service.GrantSpec{Target: sa.Principal, Capability: domain.CapRead, Scope: scope}); err != nil {
		t.Fatal(err)
	}

	root, rootKey := consumerCA(t)
	rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw})
	roots := x509.NewCertPool()
	roots.AddCert(root)
	x509.SetFallbackRoots(roots)
	rootFile := filepath.Join(t.TempDir(), "root.pem")
	if err := os.WriteFile(rootFile, rootPEM, 0600); err != nil {
		t.Fatal(err)
	}
	first := consumerLeaf(t, root, rootKey, "valid")
	renewed := consumerLeaf(t, root, rootKey, "valid")
	if cli.SPKIFingerprint(first.Leaf) == cli.SPKIFingerprint(renewed.Leaf) {
		t.Fatal("renewal retained the leaf key")
	}
	var certificate atomic.Pointer[tls.Certificate]
	certificate.Store(first)
	var requests, sinkRequests atomic.Int32
	var redirect atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { sinkRequests.Add(1) }))
	t.Cleanup(sink.Close)
	handler := server.New(&server.API{Auth: auth, Environments: &service.Environments{DB: db, Keyring: auth.Keyring}, Version: "tls-consumer-fixture"}, nil)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if mode := redirect.Load(); mode != 0 {
			target := sink.URL
			if mode == 2 {
				target = "/same-origin-redirect"
			}
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12, GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
		return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{*certificate.Load()}}, nil
	}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	stateDir, workDir := t.TempDir(), t.TempDir()
	bundleFile := filepath.Join(t.TempDir(), "bundle.json")
	writeBundle := func(entry cli.TrustBundle) {
		t.Helper()
		raw, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bundleFile, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	entry := cli.TrustBundle{Name: "ci", Origin: srv.URL, TLSMode: cli.TLSModeSystemCA}
	writeBundle(entry)
	binary := os.Getenv("HIKYO_TLS_CONSUMER_BINARY")
	if binary != "" {
		if !filepath.IsAbs(binary) {
			t.Fatal("released consumer binary must be absolute")
		}
		raw, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("external CLI sha256=%x; signed provenance must be verified separately", sha256.Sum256(raw))
	}
	token := minted.Value
	run := func(want int, args ...string) []byte {
		t.Helper()
		var stdout, stderr bytes.Buffer
		values := map[string]string{"HIKYO_STATE_DIR": stateDir, "HIKYO_TOKEN": token, "HIKYO_TRUST_BUNDLE": bundleFile}
		code := 0
		if binary == "" {
			code = cli.Run(t.Context(), cli.IO{Stdout: &stdout, Stderr: &stderr, Workdir: workDir, Env: cli.Env{Getenv: func(key string) string { return values[key] }}}, args)
		} else {
			command := exec.CommandContext(t.Context(), binary, args...)
			command.Dir, command.Stdout, command.Stderr = workDir, &stdout, &stderr
			// No operator credentials, proxies, contexts or configuration are inherited.
			command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + workDir, "SSL_CERT_FILE=" + rootFile, "SSL_CERT_DIR=" + workDir}
			for key, value := range values {
				command.Env = append(command.Env, key+"="+value)
			}
			if err := command.Run(); err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					code = exit.ExitCode()
				} else {
					t.Fatal("could not execute consumer CLI", err)
				}
			}
		}
		if strings.Contains(stdout.String()+stderr.String(), token) {
			t.Fatal("CLI output disclosed the fixture bearer")
		}
		if code != want {
			t.Fatalf("%v: exit=%d want=%d stderr=%s", args, code, want, &stderr)
		}
		if want != cli.ExitOK && stdout.Len() != 0 {
			t.Fatal("refused consumer printed success output")
		}
		return bytes.Clone(stdout.Bytes())
	}
	read := func(want int, project domain.ProjectID) []byte {
		return run(want, "env", "list", "--auth", "machine", "--instance", "ci", "--org", string(orgID), "--project", string(project), "-o", "json")
	}
	contextArgs := []string{"context", "create", "ci", "--instance", "ci", "--trust-file", bundleFile}
	run(cli.ExitOK, contextArgs...)
	if requests.Load() != 0 {
		t.Fatal("bundle import made a request")
	}
	for _, cert := range []*tls.Certificate{first, renewed} {
		certificate.Store(cert)
		if helper := os.Getenv("HIKYO_TLS_CONSUMER_HELPER"); helper != "" {
			if binary == "" || !filepath.IsAbs(helper) {
				t.Fatal("consumer helper requires an absolute helper path and external CLI")
			}
			helperDir := t.TempDir()
			bundle, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(t.Context(), "/bin/bash", "-c", `set -euo pipefail
source "$1"
hikyo_machine_setup "$2"
hikyo_cli env list -o json`, "tls-consumer", helper, helperDir)
			command.Dir = workDir
			command.Env = []string{"PATH=" + filepath.Dir(binary) + ":/usr/bin:/bin", "HOME=" + workDir,
				"SSL_CERT_FILE=" + rootFile, "SSL_CERT_DIR=" + workDir,
				"HIKYO_PREVIEW_TOKEN=" + token, "HIKYO_TRUST_BUNDLE=" + string(bundle),
				"HIKYO_RUNTIME_TARGETS=" + fmt.Sprintf(`{"org":%q,"project":%q}`, orgID, projectID)}
			var output, diagnostics bytes.Buffer
			command.Stdout, command.Stderr = &output, &diagnostics
			err = command.Run()
			if strings.Contains(output.String()+diagnostics.String(), token) {
				t.Fatal("consumer helper disclosed the fixture bearer")
			}
			if err != nil {
				t.Fatalf("downstream helper: %v\n%s", err, &diagnostics)
			}
			var environments apigen.EnvironmentList
			if err := json.Unmarshal(output.Bytes(), &environments); err != nil || len(environments.Items) == 0 {
				t.Fatal("downstream helper did not return the real environment list", err)
			}
			t.Log("isolated downstream helper: protected JSON bundle and real authenticated read passed")
		}
		var environments apigen.EnvironmentList
		if err := json.Unmarshal(read(cli.ExitOK, projectID), &environments); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, env := range environments.Items {
			found = found || env.Id == environmentID
		}
		if !found {
			t.Fatal("authorized consumer did not read the real fixture environment")
		}
	}
	t.Log("valid and renewed leaf: authenticated environment reads passed")
	for _, kind := range []string{"expired", "future", "wrong-host", "untrusted"} {
		certificate.Store(consumerLeaf(t, root, rootKey, kind))
		before := requests.Load()
		read(cli.ExitUnavailable, projectID)
		if requests.Load() != before {
			t.Fatal(kind + " certificate received an HTTP credential")
		}
	}
	certificate.Store(renewed)
	t.Log("expired, future, wrong-host and untrusted leaves: zero HTTP requests")
	for _, mode := range []int32{1, 2} {
		before := requests.Load()
		redirect.Store(mode)
		read(cli.ExitRefused, projectID)
		redirect.Store(0)
		if requests.Load() != before+1 || sinkRequests.Load() != 0 {
			t.Fatal("redirect forwarded a request or credential")
		}
	}
	t.Log("cross-origin and same-origin redirects: one original request each, zero forwarded requests")
	before := requests.Load()
	for _, changed := range []cli.TrustBundle{
		{Name: "ci", Origin: "https://attacker.invalid", TLSMode: cli.TLSModeSystemCA},
		{Name: "ci", Origin: srv.URL, SPKIPin: cli.SPKIFingerprint(first.Leaf)},
		{Name: "ci", Origin: srv.URL, TLSMode: cli.TLSModeSystemCA, SPKIPin: cli.SPKIFingerprint(first.Leaf)},
	} {
		writeBundle(changed)
		run(cli.ExitRefused, contextArgs...)
	}
	writeBundle(entry)
	for _, field := range []string{`"origin":"https://attacker.invalid"`, `"tls_mode":"spki"`, `"spki_pin":"untrusted"`} {
		path := filepath.Join(workDir, cli.PinFileName)
		if err := os.WriteFile(path, []byte(`{"instance":"ci",`+field+`}`), 0600); err != nil {
			t.Fatal(err)
		}
		read(cli.ExitRefused, projectID)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != before || sinkRequests.Load() != 0 {
		t.Fatal("protected identity conflict sent credentials")
	}
	t.Log("protected origin/mode/pin conflicts and repository injection: zero HTTP requests")
	// An actual consumer must preserve legacy pins and require deliberate mode
	// migration. Neither an omitted mode nor a changed key can enable CA trust.
	run(cli.ExitOK, "context", "delete", "--instance", "ci")
	legacy := cli.TrustBundle{Name: "ci", Origin: srv.URL, SPKIPin: cli.SPKIFingerprint(first.Leaf)}
	writeBundle(legacy)
	run(cli.ExitOK, contextArgs...)
	certificate.Store(first)
	read(cli.ExitOK, projectID)
	legacy.TLSMode = cli.TLSModeSPKI
	writeBundle(legacy)
	run(cli.ExitOK, contextArgs...)
	certificate.Store(renewed)
	before = requests.Load()
	read(cli.ExitUnavailable, projectID)
	if requests.Load() != before {
		t.Fatal("changed SPKI sent an HTTP credential")
	}
	run(cli.ExitOK, "context", "delete", "--instance", "ci")
	writeBundle(cli.TrustBundle{Name: "ci", Origin: srv.URL})
	run(cli.ExitRefused, contextArgs...)
	writeBundle(entry)
	run(cli.ExitOK, contextArgs...)
	read(cli.ExitOK, projectID)
	t.Log("legacy and explicit SPKI accepted; changed key and missing pin refused; deliberate CA migration passed")
	// These refusals must come from the live server, after valid TLS. They
	// distinguish transport-only fixtures from real authorization acceptance.
	read(cli.ExitNotFound, siblingID)
	if err := identities.RevokeCredential(t.Context(), operator, scope, sa.ID, minted.Credential.ID); err != nil {
		t.Fatal(err)
	}
	read(cli.ExitAuth, projectID)
	t.Log("real authorization: sibling project refused; revoked credential refused")
}

func consumerCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "disposable TLS consumer root"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func consumerLeaf(t *testing.T, root *x509.Certificate, rootKey *ecdsa.PrivateKey, kind string) *tls.Certificate {
	t.Helper()
	if kind == "untrusted" {
		root, rootKey = consumerCA(t)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(now.UnixNano()), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	switch kind {
	case "valid", "untrusted":
	case "expired":
		template.NotBefore, template.NotAfter = now.Add(-2*time.Hour), now.Add(-time.Hour)
	case "future":
		template.NotBefore, template.NotAfter = now.Add(time.Minute), now.Add(time.Hour)
	case "wrong-host":
		template.IPAddresses, template.DNSNames = nil, []string{"other.invalid"}
	default:
		t.Fatal(fmt.Errorf("unknown certificate fixture %q", kind))
	}
	der, err := x509.CreateCertificate(rand.Reader, template, root, &key.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: cert}
}
