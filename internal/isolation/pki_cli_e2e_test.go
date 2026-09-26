package isolation

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pki"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
)

// The private-PKI CLI seam (#154): a real app.Boot server, the real CLI, a
// stepped-up operator session and a real workload credential, on both
// engines. The operator builds a CA hierarchy and a profile; a human issues
// through a CSR; a workload issues with a server-generated key through the
// print triad; the worker publishes the CRL; and the state survives restart.
func TestPKICLISQLite(t *testing.T)   { runPKICLI(t, store.EngineSQLite) }
func TestPKICLIPostgres(t *testing.T) { runPKICLI(t, store.EnginePostgres) }

func runPKICLI(t *testing.T, engine store.Engine) {
	t.Helper()
	cfg := retentionAppConfig(t, engine)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	first, err := app.Boot(t.Context(), cfg, log)
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	origin := "http://" + first.Addr
	cfg.Listen = first.Addr
	stopFirst := serveRetentionApp(t, first)
	waitHTTP(t, "http://"+first.OperationalAddr+"/healthz")

	db, err := openBootedIsolationFixture(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rootKey, err := crypto.ReadRootKey(cfg.RootKeyFile, "")
	if err != nil {
		t.Fatal(err)
	}
	kr, err := crypto.LoadKeyring(t.Context(), &keyring.Store{DB: db}, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	kdf, limiter, err := app.AuthComponents(cfg)
	if err != nil {
		t.Fatal(err)
	}
	auth := &service.Auth{DB: db, Keyring: kr, KDF: kdf, Admission: limiter, ExternalOrigin: cfg.ExternalOrigin}
	boot, err := auth.BootstrapAdmin(t.Context(), "pki-admin", "PKI Admin", "terminal")
	if err != nil {
		t.Fatal(err)
	}
	seedRetentionCLITenant(t, db)
	execRaw(t, db, fmt.Sprintf(`INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order)
        VALUES ('%s', '%s', '%s', 'pki-e2e', '', %s, 10)`, retentionCLIEnv, retentionCLIOrg, retentionCLIProject, ts))
	seedOrigins(t, db)
	for id, capability := range map[string]string{"g_pki_cli_issue": "issue-certificate", "g_pki_cli_read": "read"} {
		execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at)
            VALUES ('%s', '%s', '%s', '%s', '%s', '%s', %s)`, id, boot.PrincipalID, capability, retentionCLIOrg, retentionCLIProject, retentionCLIEnv, ts))
	}
	execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at)
        VALUES ('g_pki_cli_mi', '%s', 'manage-identities', '%s', '%s', NULL, %s)`, boot.PrincipalID, retentionCLIOrg, retentionCLIProject, ts))

	const password = "pki e2e ordinary passphrase"
	stateDir, workDir := t.TempDir(), t.TempDir()
	// The workload runs from its own state directory, provisioned with the
	// instance's trust entry the way a deployment would ship it.
	humanState, machineState := stateDir, t.TempDir()
	writeTrustStore(t, machineState, origin)
	prompts := map[string]string{"authority": boot.Authority, "New password": password, "Repeat": password}
	ios := func() cli.IO {
		var terminal fakeTerminal
		terminalSession, err := disclose.NewTerminalSession(&terminal)
		if err != nil {
			t.Fatal(err)
		}
		return cli.IO{
			Stdout: io.Discard, Stderr: io.Discard, Workdir: workDir,
			Env: cli.Env{Getenv: func(key string) string {
				if key == "HIKYO_STATE_DIR" {
					return stateDir
				}
				return ""
			}},
			ReadPassword: func(prompt string) (string, error) {
				for match, answer := range prompts {
					if strings.Contains(prompt, match) {
						return answer, nil
					}
				}
				t.Fatalf("unexpected prompt: %q", prompt)
				return "", nil
			},
			TerminalSession: terminalSession,
		}
	}
	runCLI := func(want int, args ...string) string {
		t.Helper()
		stdout, stderr := &strings.Builder{}, &strings.Builder{}
		invocation := ios()
		invocation.Stdout, invocation.Stderr = stdout, stderr
		if got := cli.Run(t.Context(), invocation, args); got != want {
			t.Fatalf("hikyo %s exited %d, want %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), got, want, stdout, stderr)
		}
		return stdout.String()
	}

	runCLI(cli.ExitOK, "account", "establish-credential", "--instance", origin, "--as", "pki-admin")
	clear(prompts)
	prompts["Password for pki-admin"] = password
	runCLI(cli.ExitOK, "login", origin, "--local", "--as", "pki-admin")
	totpFile := filepath.Join(workDir, "pki-totp.uri")
	prompts["to authorize enrolment"] = password
	runCLI(cli.ExitOK, "account", "factor", "enrol-totp", "--output-file", totpFile)
	uri, err := os.ReadFile(totpFile)
	if err != nil {
		t.Fatal(err)
	}
	initialStep := crypto.TOTPStep(time.Now().UTC())
	prompts["to confirm"] = totpCode(t, strings.TrimSpace(string(uri)), time.Now().UTC().Add(30*time.Second))
	runCLI(cli.ExitOK, "account", "factor", "confirm-totp")
	waitForTOTPStep(t, initialStep+1)
	prompts["authenticator:"] = totpCode(t, strings.TrimSpace(string(uri)), time.Now().UTC().Add(30*time.Second))
	runCLI(cli.ExitOK, "account", "factor", "step-up")

	// --- Operator: CA hierarchy and profile -------------------------------
	runCLI(cli.ExitOK, "pki", "issuer", "create-root", "e2e-root", "--common-name", "E2E Root", "--ttl", "8760h")
	var issuing apigen.PkiIssuer
	if err := json.Unmarshal([]byte(runCLI(cli.ExitOK, "pki", "issuer", "create-intermediate", "e2e-issuing",
		"--parent", "e2e-root", "--common-name", "E2E Issuing", "--ttl", "2160h", "-o", "json")), &issuing); err != nil {
		t.Fatalf("intermediate JSON: %v", err)
	}
	if issuing.State != "active" || issuing.CertificatePem == nil {
		t.Fatalf("intermediate: %+v", issuing)
	}
	csrOut := filepath.Join(workDir, "offline.csr")
	runCLI(cli.ExitOK, "pki", "issuer", "create-intermediate", "e2e-offline", "--common-name", "Offline Signed", "--csr-out", csrOut)
	if raw, err := os.ReadFile(csrOut); err != nil || !strings.Contains(string(raw), "CERTIFICATE REQUEST") {
		t.Fatalf("pending intermediate CSR was not written: %v", err)
	}
	// A CA key on argv is refused by construction: there is no such flag.
	runCLI(cli.ExitUsage, "pki", "issuer", "import", "x", "--private-key", "-----BEGIN PRIVATE KEY-----")

	policy := `{"allowed_issuers":["e2e-issuing"],"dns_patterns":["*.svc.e2e.test"],"ip_ranges":[],"uri_patterns":["spiffe://e2e.test/ns/*"],
"allow_wildcard_names":false,"key_algorithms":["ecdsa-p256","ed25519"],"key_usages":["digital-signature"],"ext_key_usages":["server-auth","client-auth"],
"max_ttl_seconds":259200,"default_ttl_seconds":86400,"renew_window_seconds":28800,"allow_csr":true,"allow_generated_key":true,"machine_issuance":true,"organization":""}`
	policyFile := filepath.Join(workDir, "policy.json")
	if err := os.WriteFile(policyFile, []byte(policy), 0o600); err != nil {
		t.Fatal(err)
	}
	runCLI(cli.ExitOK, "pki", "profile", "create", "e2e-web", "--policy-file", policyFile)
	widened := filepath.Join(workDir, "widened.json")
	if err := os.WriteFile(widened, []byte(strings.Replace(policy, `"*.svc.e2e.test"`, `"*.svc.e2e.test","*.other.test"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	runCLI(cli.ExitRefused, "pki", "profile", "update", "e2e-web", "--policy-file", widened)
	runCLI(cli.ExitOK, "pki", "profile", "bind", "e2e-web", "--org", retentionCLIOrg, "--project", retentionCLIProject)

	// --- Human CSR issuance ----------------------------------------------------
	env := []string{"--org", retentionCLIOrg, "--project", retentionCLIProject, "--env", retentionCLIEnv}
	key, err := pki.GenerateKey(pki.ECDSAP256)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := pki.CreateCSR(key, pki.Subject{CommonName: "api"})
	if err != nil {
		t.Fatal(err)
	}
	csrFile := filepath.Join(workDir, "api.csr")
	if err := os.WriteFile(csrFile, []byte(pki.CSRPEM(csrDER)), 0o600); err != nil {
		t.Fatal(err)
	}
	certOut := filepath.Join(workDir, "api.pem")
	var issued apigen.Certificate
	if err := json.Unmarshal([]byte(runCLI(cli.ExitOK, append([]string{"cert", "issue", "--profile", "e2e-web", "--csr-file", csrFile,
		"--dns", "api.svc.e2e.test", "--cert-out", certOut, "-o", "json"}, env...)...)), &issued); err != nil {
		t.Fatalf("issue JSON: %v", err)
	}
	chainPEM, err := os.ReadFile(certOut)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(chainPEM)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !pki.PublicKeysEqual(leaf.PublicKey, key.Public()) || leaf.DNSNames[0] != "api.svc.e2e.test" {
		t.Fatalf("issued leaf does not carry the CSR key and requested name: %v", err)
	}
	runCLI(cli.ExitRefused, append([]string{"cert", "issue", "--profile", "e2e-web", "--csr-file", csrFile, "--dns", "api.evil.test"}, env...)...)

	// --- Workload generated-key issuance ------------------------------------------
	ident := &service.Identities{DB: db, Auth: &service.Auth{DB: db, ReauthWindow: 5 * time.Minute, ReauthHardCap: time.Hour}}
	project := domain.Scope{Org: retentionCLIOrg, Project: retentionCLIProject}
	sa, err := ident.CreateServiceAccount(t.Context(), service.LocalPrincipal(boot.PrincipalID), project, "pki-e2e-workload", domain.ClassWorkload)
	if err != nil {
		t.Fatal(err)
	}
	minted, err := ident.MintCredential(t.Context(), service.LocalPrincipal(boot.PrincipalID), project, sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for id, capability := range map[string]string{"g_pki_cli_sa_issue": "issue-certificate", "g_pki_cli_sa_read": "read"} {
		execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at)
            VALUES ('%s', '%s', '%s', '%s', '%s', '%s', %s)`, id, sa.Principal, capability, retentionCLIOrg, retentionCLIProject, retentionCLIEnv, ts))
	}
	tokenFile := filepath.Join(workDir, "workload.token")
	if err := os.WriteFile(tokenFile, []byte(minted.Value), 0o600); err != nil {
		t.Fatal(err)
	}
	keyOut := filepath.Join(workDir, "worker.key")
	workload := append([]string{"--instance", "local", "--token-file", tokenFile, "--auth", "machine"}, env...)
	stateDir = machineState
	var workerCert apigen.Certificate
	if err := json.Unmarshal([]byte(runCLI(cli.ExitOK, append([]string{"cert", "issue", "--profile", "e2e-web", "--generate-key",
		"--key-algorithm", "ed25519", "--uri", "spiffe://e2e.test/ns/prod/sa/worker", "--output-file", keyOut, "-o", "json"}, workload...)...)), &workerCert); err != nil {
		t.Fatalf("workload issue JSON: %v", err)
	}
	keyPEM, err := os.ReadFile(keyOut)
	if err != nil {
		t.Fatalf("the generated key was not written to its display-once file: %v", err)
	}
	info, err := os.Stat(keyOut)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("generated key file mode = %v, want 0600", info.Mode().Perm())
	}
	generated, err := pki.ParsePrivateKey(keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if workerCert.CertificatePem == nil {
		t.Fatal("workload certificate missing")
	}
	workerBlock, _ := pem.Decode([]byte(*workerCert.CertificatePem))
	workerLeaf, err := x509.ParseCertificate(workerBlock.Bytes)
	if err != nil || !pki.PublicKeysEqual(workerLeaf.PublicKey, generated.Public()) {
		t.Fatal("the generated key does not match the issued certificate")
	}
	// The key is display-once: a second read of the certificate carries none.
	if strings.Contains(runCLI(cli.ExitOK, append([]string{"cert", "show", workerCert.Id, "-o", "json"}, workload...)...), "PRIVATE KEY") {
		t.Fatal("a certificate read returned private key material")
	}

	// --- Revocation and the worker-published CRL ----------------------------------
	stateDir = humanState
	runCLI(cli.ExitOK, append([]string{"cert", "revoke", issued.Id, "--reason", "key-compromise"}, env...)...)
	waitForCount(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM pki_issuers WHERE id='%s' AND crl_der IS NOT NULL AND crl_this_update >= (SELECT revoked_at FROM pki_certificates WHERE id='%s')`, issuing.Id, issued.Id), 1)
	stateDir = machineState
	crlPEM := runCLI(cli.ExitOK, append([]string{"cert", "crl", workerCert.Id}, workload...)...)
	stateDir = humanState
	crlBlock, _ := pem.Decode([]byte(crlPEM))
	crl, err := x509.ParseRevocationList(crlBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, entry := range crl.RevokedCertificateEntries {
		listed = listed || pki.SerialHex(entry.SerialNumber) == issued.Serial
	}
	if !listed {
		t.Fatalf("the worker-published CRL does not list the revoked serial %s", issued.Serial)
	}
	stopFirst()

	// --- Restart: inventory and custody survive ------------------------------------
	second, err := app.Boot(t.Context(), cfg, log)
	if err != nil {
		t.Fatalf("restart boot: %v", err)
	}
	stopSecond := serveRetentionApp(t, second)
	defer stopSecond()
	waitHTTP(t, "http://"+second.OperationalAddr+"/healthz")
	var afterRestart apigen.CertificateList
	if err := json.Unmarshal([]byte(runCLI(cli.ExitOK, append([]string{"cert", "list", "-o", "json"}, env...)...)), &afterRestart); err != nil {
		t.Fatal(err)
	}
	if len(afterRestart.Certificates) != 2 {
		t.Fatalf("certificates after restart = %d, want 2", len(afterRestart.Certificates))
	}
	// The sealed CA key survived the restart: the intermediate still signs.
	runCLI(cli.ExitOK, append([]string{"cert", "issue", "--profile", "e2e-web", "--csr-file", csrFile, "--dns", "again.svc.e2e.test"}, env...)...)
	var issuers apigen.PkiIssuerList
	if err := json.Unmarshal([]byte(runCLI(cli.ExitOK, "pki", "issuer", "list", "-o", "json")), &issuers); err != nil {
		t.Fatal(err)
	}
	if len(issuers.Issuers) != 3 {
		t.Fatalf("issuers after restart = %d, want 3", len(issuers.Issuers))
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='pki.certificate_key_disclosed' AND actor_class='machine'"); got != 1 {
		t.Fatalf("machine key disclosure events = %d, want 1", got)
	}
}
