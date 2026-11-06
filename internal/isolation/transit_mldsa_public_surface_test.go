package isolation

import (
	"bytes"
	"crypto/mldsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/transit"
)

// The public Transit surface for ML-DSA-65 (#1129): a real app.Boot server and
// the real CLI. The contract (api-cli-surface amendment of 2026-11-04) names
// mldsa65, so create, sign, verify, rotate and list work end to end, signatures
// verify against the published SPKI key with the standard library alone, the
// existing ed25519 flow is unchanged, and any other ML-DSA name is still refused
// through the production path.
func TestTransitPublicSurfaceMLDSA65SQLite(t *testing.T) {
	runTransitPublicSurface(t, store.EngineSQLite)
}

func TestTransitPublicSurfaceMLDSA65Postgres(t *testing.T) {
	runTransitPublicSurface(t, store.EnginePostgres)
}

func runTransitPublicSurface(t *testing.T, engine store.Engine) {
	t.Helper()
	cfg := retentionAppConfig(t, engine)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	first, err := app.Boot(t.Context(), cfg, log)
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	origin := "http://" + first.Addr
	cfg.Listen = first.Addr
	stop := serveRetentionApp(t, first)
	defer stop()
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
	boot, err := auth.BootstrapAdmin(t.Context(), "transit-admin", "Transit Admin", "terminal")
	if err != nil {
		t.Fatal(err)
	}
	seedRetentionCLITenant(t, db)
	execRaw(t, db, fmt.Sprintf(`INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order)
        VALUES ('%s', '%s', '%s', 'transit-e2e', '', %s, 11)`, retentionCLIEnv, retentionCLIOrg, retentionCLIProject, ts))
	seedOrigins(t, db)
	for id, g := range map[string][2]string{
		"g_tr_cli_use":    {"crypto-use", retentionCLIEnv},
		"g_tr_cli_read":   {"read", retentionCLIEnv},
		"g_tr_cli_manage": {"crypto-manage", ""},
	} {
		envID := "NULL"
		if g[1] != "" {
			envID = "'" + g[1] + "'"
		}
		execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at)
            VALUES ('%s', '%s', '%s', '%s', '%s', %s, %s)`, id, boot.PrincipalID, g[0], retentionCLIOrg, retentionCLIProject, envID, ts))
	}

	const password = "transit e2e ordinary passphrase"
	stateDir, workDir := t.TempDir(), t.TempDir()
	prompts := map[string]string{"authority": boot.Authority, "New password": password, "Repeat": password}
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var terminal fakeTerminal
		terminalSession, err := disclose.NewTerminalSession(&terminal)
		if err != nil {
			t.Fatal(err)
		}
		stdout, stderr := &strings.Builder{}, &strings.Builder{}
		invocation := cli.IO{
			Stdout: stdout, Stderr: stderr, Workdir: workDir,
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
		code := cli.Run(t.Context(), invocation, args)
		return code, stdout.String(), stderr.String()
	}
	runCLI := func(want int, args ...string) string {
		t.Helper()
		got, stdout, stderr := run(args...)
		if got != want {
			t.Fatalf("hikyo %s exited %d, want %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), got, want, stdout, stderr)
		}
		return stdout
	}

	runCLI(cli.ExitOK, "account", "establish-credential", "--instance", origin, "--socket", cfg.CLISocket, "--as", "transit-admin")
	clear(prompts)
	prompts["Password for transit-admin"] = password
	runCLI(cli.ExitOK, "login", origin, "--local", "--as", "transit-admin")
	totpFile := filepath.Join(workDir, "transit-totp.uri")
	prompts["to authorize enrolment"] = password
	runCLI(cli.ExitOK, "account", "factor", "enrol-totp", "--output-file", totpFile)
	uri, err := os.ReadFile(totpFile)
	if err != nil {
		t.Fatal(err)
	}
	step := crypto.TOTPStep(time.Now().UTC())
	prompts["to confirm"] = totpCode(t, strings.TrimSpace(string(uri)), time.Now().UTC().Add(30*time.Second))
	runCLI(cli.ExitOK, "account", "factor", "confirm-totp")
	waitForTOTPStep(t, step+1)
	prompts["authenticator:"] = totpCode(t, strings.TrimSpace(string(uri)), time.Now().UTC().Add(30*time.Second))
	runCLI(cli.ExitOK, "account", "factor", "step-up")

	scope := []string{"--org", retentionCLIOrg, "--project", retentionCLIProject, "--env", retentionCLIEnv}
	with := func(args ...string) []string { return append(args, scope...) }

	// The existing flow is unchanged: create, sign, verify, tamper.
	runCLI(cli.ExitOK, with("transit", "key", "create", "classic", "--algorithm", "ed25519")...)
	message := filepath.Join(workDir, "message.bin")
	if err := os.WriteFile(message, []byte("public surface"), 0o600); err != nil {
		t.Fatal(err)
	}
	signature := strings.TrimSpace(runCLI(cli.ExitOK, with("transit", "sign", "classic", "--input-file", message)...))
	if !strings.HasPrefix(signature, "hikyo:v1:") {
		t.Fatalf("signature output = %.40q", signature)
	}
	runCLI(cli.ExitOK, with("transit", "verify", "classic", "--input-file", message, "--signature", signature)...)
	if err := os.WriteFile(message, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	runCLI(cli.ExitRefused, with("transit", "verify", "classic", "--input-file", message, "--signature", signature)...)

	// ML-DSA-65 through the real server: create, sign, verify, tamper, rotate, list.
	runCLI(cli.ExitOK, with("transit", "key", "create", "pq", "--algorithm", "mldsa65")...)
	if err := os.WriteFile(message, []byte("post-quantum public surface"), 0o600); err != nil {
		t.Fatal(err)
	}
	pqOne := strings.TrimSpace(runCLI(cli.ExitOK, with("transit", "sign", "pq", "--input-file", message)...))
	if !strings.HasPrefix(pqOne, "hikyo:v1:") || len(pqOne) != 4424 {
		t.Fatalf("mldsa65 signature output has %d characters, want the 4424-character wire value: %.40q", len(pqOne), pqOne)
	}
	runCLI(cli.ExitOK, with("transit", "verify", "pq", "--input-file", message, "--signature", pqOne)...)
	// A signature of the other algorithm is not accepted for this key.
	runCLI(cli.ExitRefused, with("transit", "verify", "pq", "--input-file", message, "--signature", signature)...)
	if err := os.WriteFile(message, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	runCLI(cli.ExitRefused, with("transit", "verify", "pq", "--input-file", message, "--signature", pqOne)...)
	if err := os.WriteFile(message, []byte("post-quantum public surface"), 0o600); err != nil {
		t.Fatal(err)
	}
	runCLI(cli.ExitOK, with("transit", "key", "rotate", "pq")...)
	pqTwo := strings.TrimSpace(runCLI(cli.ExitOK, with("transit", "sign", "pq", "--input-file", message)...))
	if !strings.HasPrefix(pqTwo, "hikyo:v2:") {
		t.Fatalf("signature after rotation = %.40q", pqTwo)
	}
	for _, value := range []string{pqOne, pqTwo} {
		runCLI(cli.ExitOK, with("transit", "verify", "pq", "--input-file", message, "--signature", value)...)
	}

	// `show -o json` prints the one key as a list document, as it does for every algorithm.
	var shownList apigen.TransitKeyList
	if err := json.Unmarshal([]byte(runCLI(cli.ExitOK, with("transit", "key", "show", "pq", "-o", "json")...)), &shownList); err != nil || len(shownList.Items) != 1 {
		t.Fatalf("show output: %d items, err %v", len(shownList.Items), err)
	}
	shown := shownList.Items[0]
	if shown.Algorithm != apigen.TransitAlgorithm("mldsa65") || shown.LatestVersion != 2 || len(shown.Versions) != 2 {
		t.Fatalf("shown key = %+v, want mldsa65 at version 2 with two versions", shown)
	}
	// Both signatures verify against the published public keys with the standard
	// library, the way an outside verifier would use them.
	for i, value := range []string{pqOne, pqTwo} {
		if shown.Versions[i].PublicKey == nil {
			t.Fatalf("version %d publishes no public key", i+1)
		}
		if !spkiVerifies(t, *shown.Versions[i].PublicKey, []byte("post-quantum public surface"), value) {
			t.Errorf("signature %d does not verify under the published version %d key", i+1, i+1)
		}
	}
	if *shown.Versions[0].PublicKey == *shown.Versions[1].PublicKey {
		t.Error("rotation published the same public key for both versions")
	}
	if spkiVerifies(t, *shown.Versions[1].PublicKey, []byte("post-quantum public surface"), pqOne) {
		t.Error("a version 1 signature verified under the version 2 key")
	}

	var list apigen.TransitKeyList
	if err := json.Unmarshal([]byte(runCLI(cli.ExitOK, with("transit", "key", "list", "-o", "json")...)), &list); err != nil {
		t.Fatal(err)
	}
	algorithms := map[string]apigen.TransitAlgorithm{}
	for _, item := range list.Items {
		algorithms[item.Name] = item.Algorithm
	}
	if len(list.Items) != 2 || algorithms["classic"] != apigen.TransitAlgorithm("ed25519") || algorithms["pq"] != apigen.TransitAlgorithm("mldsa65") {
		t.Fatalf("key list = %+v", algorithms)
	}
	if table := runCLI(cli.ExitOK, with("transit", "key", "list")...); !strings.Contains(table, "mldsa65") {
		t.Fatalf("the table list does not show mldsa65:\n%s", table)
	}

	// Any other ML-DSA name is refused by the contract, and nothing is created.
	for _, algorithm := range []string{"mldsa44", "mldsa87", "MLDSA65"} {
		code, stdout, stderr := run(with("transit", "key", "create", "pq-"+strings.ToLower(algorithm), "--algorithm", algorithm)...)
		if code != cli.ExitRefused || !strings.Contains(stderr, "does not satisfy the API contract (algorithm)") {
			t.Fatalf("%s: exit %d, want the contract refusal (%d)\nstdout: %s\nstderr: %s", algorithm, code, cli.ExitRefused, stdout, stderr)
		}
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM transit_keys`); n != 2 {
		t.Fatalf("the public surface holds %d keys, want the two created", n)
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM transit_key_versions WHERE key_id IN (SELECT id FROM transit_keys WHERE algorithm='mldsa65')`); n != 2 {
		t.Fatalf("the mldsa65 key has %d versions, want 2", n)
	}
}

// spkiVerifies checks a transit signature value with the standard library alone,
// from the public key exactly as the API publishes it (standard base64 of the
// DER SubjectPublicKeyInfo) and the documented signature layout.
func spkiVerifies(t *testing.T, publishedKey string, message []byte, value string) bool {
	t.Helper()
	der, err := base64.StdEncoding.DecodeString(publishedKey)
	if err != nil {
		t.Fatalf("published public key is not standard base64: %v", err)
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		t.Fatalf("published public key is not an SPKI: %v", err)
	}
	pk, ok := parsed.(*mldsa.PublicKey)
	if !ok || pk.Parameters() != mldsa.MLDSA65() {
		t.Fatalf("published public key parsed as %T, want an ML-DSA-65 key", parsed)
	}
	_, enc, ok := strings.Cut(strings.TrimPrefix(value, "hikyo:v"), ":")
	if !ok {
		t.Fatalf("not a transit value: %.20q", value)
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(enc)
	if err != nil || len(payload) != 3311 || payload[0] != 0x09 || payload[1] != 0x05 {
		t.Fatalf("payload shape: len %d err %v", len(payload), err)
	}
	return mldsa.Verify(pk, message, payload[2:], &mldsa.Options{Context: "hikyo/transit/sign/v1"}) == nil
}

// The same lifecycle over the raw HTTP API, with the wire shapes a third-party
// client sees: JSON bodies, status codes, the published SPKI key and the
// verification outcome classes. Both engines run (PostgreSQL when
// HIKYO_TEST_POSTGRES_DSN is set).
func TestTransitMLDSA65APILifecycle(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := bootstrapAdmin(t, db, adminOpts{username: "factor-admin", displayName: "A", password: "correct horse battery staple transit api"})
		operator := service.LocalPrincipal(root)
		keyring := probeKeyring(t, db)
		org, err := (&service.Orgs{DB: db}).Create(t.Context(), operator, "api-org", true, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		project, err := (&service.Projects{DB: db}).Create(t.Context(), operator, domain.OrgID(org.ID), "api-project")
		if err != nil {
			t.Fatal(err)
		}
		env, err := (&service.Environments{DB: db, Keyring: keyring}).Create(t.Context(), operator, domain.Scope{Org: domain.OrgID(org.ID), Project: domain.ProjectID(project.ID)}, "api-env", nil)
		if err != nil {
			t.Fatal(err)
		}
		principal := string(admin.boot.PrincipalID)
		for i, grant := range [][2]string{{"read", "'" + env.ID + "'"}, {"crypto-use", "'" + env.ID + "'"}, {"crypto-manage", "NULL"}} {
			execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_transit_api_%d','%s','%s','%s','%s',%s,%s)`, i, principal, grant[0], org.ID, project.ID, grant[1], ts))
		}
		clk := time.Now().UTC()
		admin.auth.Now = func() time.Time { return clk }
		initial, err := admin.auth.LocalLogin(t.Context(), "factor-admin", admin.password, service.ArtifactCLI)
		if err != nil {
			t.Fatal(err)
		}
		uri, err := admin.auth.EnrolTOTPStart(t.Context(), initial.SessionToken, admin.password)
		if err != nil {
			t.Fatal(err)
		}
		confirmed, err := admin.auth.EnrolTOTPConfirm(t.Context(), initial.SessionToken, totpCode(t, uri, clk))
		if err != nil {
			t.Fatal(err)
		}
		clk = clk.Add(30 * time.Second)
		elevated, err := admin.auth.StepUpTOTP(t.Context(), confirmed.SessionToken, totpCode(t, uri, clk))
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(server.New(&server.API{
			Auth: admin.auth, Orgs: &service.Orgs{DB: db}, Version: "transit-mldsa65-api",
			Transit: &service.Transit{DB: db, Keyring: keyring, Custody: transit.NewRegistry(&transit.Software{Keyring: keyring})},
		}, nil))
		defer srv.Close()
		admin.auth.ExternalOrigin = srv.URL

		base := "/api/v1/orgs/" + org.ID + "/projects/" + project.ID + "/environments/" + env.ID + "/transit-keys"
		call := func(method, path string, body any, want int, out any) {
			t.Helper()
			var reader io.Reader
			if body != nil {
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				reader = bytes.NewReader(raw)
			}
			req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, reader)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+elevated.SessionToken)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", srv.URL)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			raw, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != want {
				t.Fatalf("%s %s = %d, want %d: %.300s", method, path, res.StatusCode, want, raw)
			}
			if out != nil {
				if err := json.Unmarshal(raw, out); err != nil {
					t.Fatalf("%s %s: %v: %.300s", method, path, err, raw)
				}
			}
		}
		b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

		var created apigen.TransitKey
		call(http.MethodPost, base, map[string]any{"name": "pq", "algorithm": "mldsa65", "allowed_operations": []string{"sign", "verify"}}, http.StatusCreated, &created)
		if created.Algorithm != apigen.TransitAlgorithm("mldsa65") || created.LatestVersion != 1 || len(created.Versions) != 1 || created.Versions[0].PublicKey == nil {
			t.Fatalf("created key = %+v", created)
		}
		if der, err := base64.StdEncoding.DecodeString(*created.Versions[0].PublicKey); err != nil || len(der) != 1974 {
			t.Fatalf("published public key: %d bytes, err %v, want the 1974-byte SPKI", len(der), err)
		}

		message := b64("api lifecycle message")
		var one, two apigen.TransitSignatureResult
		call(http.MethodPost, base+"/pq/sign", map[string]any{"message": message}, http.StatusOK, &one)
		var verified apigen.TransitVerifyResult
		call(http.MethodPost, base+"/pq/verify", map[string]any{"message": message, "signature": one.Signature}, http.StatusOK, &verified)
		if !verified.Valid || verified.KeyVersion != 1 {
			t.Fatalf("verify = %+v, want valid at version 1", verified)
		}
		call(http.MethodPost, base+"/pq/verify", map[string]any{"message": b64("another message"), "signature": one.Signature}, http.StatusOK, &verified)
		if verified.Valid {
			t.Fatal("a signature verified for a different message")
		}
		// Operations the key does not allow are refused, not misread.
		call(http.MethodPost, base+"/pq/encrypt", map[string]any{"plaintext": message}, http.StatusForbidden, nil)

		var rotated apigen.TransitKey
		call(http.MethodPost, base+"/pq/rotate", nil, http.StatusOK, &rotated)
		if rotated.LatestVersion != 2 || len(rotated.Versions) != 2 || *rotated.Versions[0].PublicKey == *rotated.Versions[1].PublicKey {
			t.Fatalf("rotated key = %+v", rotated)
		}
		call(http.MethodPost, base+"/pq/sign", map[string]any{"message": message}, http.StatusOK, &two)
		var pinned apigen.TransitSignatureResult
		call(http.MethodPost, base+"/pq/sign", map[string]any{"message": message, "key_version": 1}, http.StatusOK, &pinned)
		if !strings.HasPrefix(two.Signature, "hikyo:v2:") || !strings.HasPrefix(pinned.Signature, "hikyo:v1:") {
			t.Fatalf("signature versions: latest %.12q, pinned %.12q", two.Signature, pinned.Signature)
		}
		for i, value := range []string{one.Signature, two.Signature, pinned.Signature} {
			version := []int{0, 1, 0}[i]
			if !spkiVerifies(t, *rotated.Versions[version].PublicKey, []byte("api lifecycle message"), value) {
				t.Errorf("signature %d does not verify under the published version %d key", i, version+1)
			}
			call(http.MethodPost, base+"/pq/verify", map[string]any{"message": message, "signature": value}, http.StatusOK, &verified)
			if !verified.Valid {
				t.Errorf("service verify of signature %d = %+v", i, verified)
			}
		}
		// A well-formed value of the wrong length answers valid=false; a value that
		// is not a transit wire value at all is a request error.
		call(http.MethodPost, base+"/pq/verify", map[string]any{"message": message, "signature": "not-a-transit-value"}, http.StatusBadRequest, nil)
		call(http.MethodPost, base+"/pq/verify", map[string]any{"message": message, "signature": "hikyo:v1:AAAA"}, http.StatusOK, &verified)
		if verified.Valid {
			t.Fatal("a truncated signature verified")
		}

		var list apigen.TransitKeyList
		call(http.MethodGet, base, nil, http.StatusOK, &list)
		if len(list.Items) != 1 || list.Items[0].Algorithm != apigen.TransitAlgorithm("mldsa65") || list.Items[0].LatestVersion != 2 {
			t.Fatalf("list = %+v", list.Items)
		}
		var shown apigen.TransitKey
		call(http.MethodGet, base+"/pq", nil, http.StatusOK, &shown)
		if shown.Algorithm != apigen.TransitAlgorithm("mldsa65") || len(shown.Versions) != 2 {
			t.Fatalf("shown = %+v", shown)
		}
		// The contract still refuses every other ML-DSA name.
		for _, other := range []string{"mldsa44", "mldsa87", "MLDSA65"} {
			call(http.MethodPost, base, map[string]any{"name": "other", "algorithm": other}, http.StatusBadRequest, nil)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM transit_keys`); n != 1 {
			t.Fatalf("%d keys after the refused requests, want 1", n)
		}
	})
}
