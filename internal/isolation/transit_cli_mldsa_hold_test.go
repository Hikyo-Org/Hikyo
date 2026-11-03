package isolation

import (
	"encoding/json"
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
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
)

// The public Transit surface after the ML-DSA mechanism landed (#1129): a real
// app.Boot server and the real CLI. The API contract is frozen at v1.0.0 and
// TransitAlgorithm is a closed enum, so the public surface must keep refusing
// mldsa65 (HOLD, registered in the handoff) while the existing ed25519 flow is
// unchanged end to end. This pins both halves through the production path, not
// through a hand-set configuration.
func TestTransitPublicSurfaceHoldsMLDSA65SQLite(t *testing.T) {
	runTransitPublicSurfaceHold(t, store.EngineSQLite)
}

func TestTransitPublicSurfaceHoldsMLDSA65Postgres(t *testing.T) {
	runTransitPublicSurfaceHold(t, store.EnginePostgres)
}

func runTransitPublicSurfaceHold(t *testing.T, engine store.Engine) {
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

	// The frozen API keeps mldsa65 out: refused through the real server, with no
	// row created and the key set unchanged.
	for _, algorithm := range []string{"mldsa65", "mldsa44", "mldsa87", "MLDSA65"} {
		code, stdout, stderr := run(with("transit", "key", "create", "pq-"+strings.ToLower(algorithm), "--algorithm", algorithm)...)
		if code != cli.ExitRefused || !strings.Contains(stderr, "does not satisfy the API contract (algorithm)") {
			t.Fatalf("%s: exit %d, want the contract refusal (%d)\nstdout: %s\nstderr: %s", algorithm, code, cli.ExitRefused, stdout, stderr)
		}
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM transit_keys WHERE algorithm <> 'ed25519'`); n != 0 {
		t.Fatalf("the public surface created %d non-ed25519 keys", n)
	}
	var list apigen.TransitKeyList
	if err := json.Unmarshal([]byte(runCLI(cli.ExitOK, with("transit", "key", "list", "-o", "json")...)), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != "classic" || list.Items[0].Algorithm != apigen.TransitAlgorithm("ed25519") {
		t.Fatalf("key list = %+v", list.Items)
	}
}
