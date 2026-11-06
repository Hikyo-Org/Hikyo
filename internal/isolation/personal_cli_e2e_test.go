package isolation

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

const personalCLIValue = "personal-cli-synthetic-value"

func TestPersonalValueCLIAgainstARealServerSQLite(t *testing.T) {
	runPersonalValueCLI(t, store.EngineSQLite)
}

func TestPersonalValueCLIAgainstARealServerPostgres(t *testing.T) {
	runPersonalValueCLI(t, store.EnginePostgres)
}

// runPersonalValueCLI drives the personal-value CLI against a booted server:
// set, list, reveal, members and clear through `hikyo personal`, then the
// three `hikyo run` paths (consented developer credential, --config-only, human
// session). Values are compared, never printed.
func runPersonalValueCLI(t *testing.T, engine store.Engine) {
	rig := bootComposeRig(t, engine)
	ctx := t.Context()
	scope := domain.Scope{Org: cOrg, Project: cPrj, Env: cEnv}

	// The developer: a local account with a confirmed authenticator app (the
	// qualification), read, reveal, manage-members and definitions-edit on the
	// project, and a
	// session holding a live reauthentication window over the environment.
	auth := authService(t, rig.db)
	dev := bootstrapAdmin(t, rig.db, adminOpts{username: "pv-cli", displayName: "PV CLI", password: ceremonyPassword, auth: auth, login: true})
	principal := dev.boot.PrincipalID
	for _, cap := range []string{"read", "reveal", "manage-members", "definitions-edit"} {
		execRaw(t, rig.db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_pv_cli_`+cap+`', '`+string(principal)+`', '`+cap+`', '`+cOrg+`', '`+cPrj+`', NULL, `+ts+`)`)
	}
	seedOrigins(t, rig.db)
	execRaw(t, rig.db, `UPDATE environments SET reauth_window_seconds = 300 WHERE id = '`+cEnv+`'`)
	uri, err := auth.EnrolTOTPStart(ctx, dev.token, dev.password)
	if err != nil {
		t.Fatalf("totp enrol start: %v", err)
	}
	if _, err := auth.EnrolTOTPConfirm(ctx, dev.token, totpCode(t, uri, time.Now())); err != nil {
		t.Fatalf("totp enrol confirm: %v", err)
	}
	token := sessionWithWindows(t, rig.db, principal, cEnv)

	state, err := cli.NewState(cli.Env{Getenv: func(k string) string {
		if k == "HIKYO_STATE_DIR" {
			return rig.stateDir
		}
		return ""
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.PutSession(cli.SessionArtifact{
		Instance: "local", Origin: rig.origin, Token: token, SessionID: "ses_" + string(principal),
		Principal: string(principal), ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	target := []string{"--instance", "local", "--org", cOrg, "--project", cPrj, "--env", cEnv}
	personal := func(args ...string) (int, string, string) {
		t.Helper()
		return rig.runCLI(t, t.TempDir(), nil, append(append([]string{"personal"}, args...), target...)...)
	}

	valueFile := filepath.Join(t.TempDir(), "value")
	if err := os.WriteFile(valueFile, []byte(personalCLIValue), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := personal("set", "DATABASE_PASSWORD", "--value-file", valueFile); code != cli.ExitOK {
		t.Fatalf("personal set: exit=%d stderr=%s", code, stderr)
	}
	if code, stdout, stderr := personal("list"); code != cli.ExitOK || !strings.Contains(stdout, "DATABASE_PASSWORD") || strings.Contains(stdout, personalCLIValue) {
		t.Fatalf("personal list: exit=%d holds-key=%t stderr=%s", code, strings.Contains(stdout, "DATABASE_PASSWORD"), stderr)
	}
	revealed := filepath.Join(t.TempDir(), "revealed")
	if code, _, stderr := personal("get", "DATABASE_PASSWORD", "--reveal", "--output-file", revealed); code != cli.ExitOK {
		t.Fatalf("personal get --reveal: exit=%d stderr=%s", code, stderr)
	}
	if got, err := os.ReadFile(revealed); err != nil || string(got) != personalCLIValue+"\n" { // the file sink ends the value with one newline
		t.Fatalf("revealed value differs from the one set (read err=%v)", err)
	}
	if code, stdout, stderr := personal("members", "DATABASE_PASSWORD"); code != cli.ExitOK || !strings.Contains(stdout, "pv-cli") {
		t.Fatalf("personal members: exit=%d stderr=%s", code, stderr)
	}

	if code, stdout, stderr := personal("counts"); code != cli.ExitOK || !strings.Contains(stdout, "DATABASE_PASSWORD") || !strings.Contains(stderr, "eligible here (read and reveal): 1") {
		t.Fatalf("personal counts: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}

	// The human-session path never delivers a personal value: it refuses the
	// key before the confirmation, so nothing is disclosed.
	code, stderr, injected := runPersonalHumanSession(t, rig, target)
	if code != cli.ExitRefused || injected != nil || !strings.Contains(stderr, "refusing key(s) DATABASE_PASSWORD") {
		t.Fatalf("human session: exit=%d started=%t stderr=%s", code, injected != nil, stderr)
	}

	// A consented developer credential delivers the personal value to the child.
	minted, rotated := mintCLIDeveloperCredential(t, rig, auth, principal, token, uri, scope)
	if err := state.PutSession(cli.SessionArtifact{
		Instance: "local", Origin: rig.origin, Token: rotated, SessionID: "ses_" + string(principal),
		Principal: string(principal), ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.PutDeveloperCredential(cli.DeveloperCredentialArtifact{
		ID: minted.Credential.ID, Origin: rig.origin, Org: cOrg, Project: cPrj, Environment: cEnv,
		Token: minted.Value, ExpiresAt: minted.Credential.ExpiresAt,
	}); err != nil {
		t.Fatal(err)
	}
	var env []string
	code, _, stderr = rig.runCLI(t, t.TempDir(), &env, append(append([]string{"run"}, target...), "--", "true")...)
	if code != cli.ExitOK || !strings.Contains(stderr, "using your personal value for DATABASE_PASSWORD") {
		t.Fatalf("developer run: exit=%d stderr=%s", code, stderr)
	}
	if !slices.Contains(env, "DATABASE_PASSWORD="+personalCLIValue) {
		t.Fatal("developer run did not inject the personal value")
	}
	if slices.Contains(env, "DATABASE_PASSWORD=dev-secret") {
		t.Fatal("developer run injected the shared value over the personal one")
	}

	// --config-only names the key and refuses it rather than serve the shared value.
	env = nil
	code, _, stderr = rig.runCLI(t, t.TempDir(), &env, append(append([]string{"run", "--config-only"}, target...), "--", "true")...)
	if code != cli.ExitRefused || env != nil || !strings.Contains(stderr, "DATABASE_PASSWORD") {
		t.Fatalf("config-only developer run: exit=%d started=%t stderr=%s", code, env != nil, stderr)
	}

	// Clearing restores the shared value on the next run.
	if code, _, stderr := personal("set", "DATABASE_PASSWORD", "--clear"); code != cli.ExitOK {
		t.Fatalf("personal set --clear: exit=%d stderr=%s", code, stderr)
	}
	env = nil
	if code, _, stderr := rig.runCLI(t, t.TempDir(), &env, append(append([]string{"run"}, target...), "--", "true")...); code != cli.ExitOK {
		t.Fatalf("run after clear: exit=%d stderr=%s", code, stderr)
	}
	if !slices.Contains(env, "DATABASE_PASSWORD=dev-secret") {
		t.Fatal("run after clear did not inject the shared value")
	}
	if n := queryInt(t, rig.db, `SELECT COUNT(*) FROM personal_values`); n != 0 {
		t.Fatalf("clear left %d personal values", n)
	}

	// personal_required_in through `hikyo key declare`: the owner's checklist
	// shows the key, and a declare without the flag keeps it.
	project := []string{"--instance", "local", "--org", cOrg, "--project", cPrj}
	declare := func(extra ...string) {
		t.Helper()
		args := append(append([]string{"key", "declare", cKeyWork, "--declaration", `{"rule":{"type":"string"}}`}, extra...), project...)
		if code, _, stderr := rig.runCLI(t, t.TempDir(), nil, args...); code != cli.ExitOK {
			t.Fatalf("key declare %v: exit=%d stderr=%s", extra, code, stderr)
		}
	}
	declare("--personal-required-in", cEnv)
	declare()
	if code, stdout, stderr := personal("list"); code != cli.ExitOK || !strings.Contains(stdout, "WORKER_URL") || !strings.Contains(stdout, "required") {
		t.Fatalf("checklist after a declare without the flag: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	declare("--personal-required-in", "none")
	if n := queryInt(t, rig.db, `SELECT COUNT(*) FROM key_personal_required_environments`); n != 0 {
		t.Fatalf("--personal-required-in none left %d rows", n)
	}
}

// mintCLIDeveloperCredential mints a personal-consent developer credential
// through the authenticator-app ceremony, one TOTP step after enrolment.
func mintCLIDeveloperCredential(t *testing.T, rig *composeRig, auth *service.Auth, principal domain.PrincipalID, token, uri string, scope domain.Scope) (service.MintedDeveloperCredential, string) {
	t.Helper()
	ctx := t.Context()
	preview, err := deliverySvc(t, rig.db).FetchAs(ctx, service.LocalPrincipal(principal), scope, "", service.FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, key := range preview.Keys {
		ids = append(ids, key.KeyID)
	}
	intent, err := service.NewPersonalDeveloperCredentialReauthIntent(string(scope.Env), ids, time.Hour, true, true)
	if err != nil {
		t.Fatal(err)
	}
	reauth, err := auth.ReauthTOTP(ctx, token, intent, totpCode(t, uri, time.Now().Add(30*time.Second)))
	if err != nil {
		t.Fatalf("developer credential reauth: %v", err)
	}
	minted, err := (&service.DeveloperCredentials{DB: rig.db, Auth: auth}).Mint(ctx, service.Bearer(reauth.SessionToken), scope,
		service.MintDeveloperCredentialRequest{Lifetime: time.Hour, ConsentCurrentAndFuture: true, KeyIDs: ids, PersonalConsent: true})
	if err != nil {
		t.Fatalf("developer credential mint: %v", err)
	}
	return minted, reauth.SessionToken
}

// runPersonalHumanSession runs `hikyo run --use-human-session` behind a
// terminal that declines, so a refusal placed after the confirmation would
// report "declined" instead of naming the key.
func runPersonalHumanSession(t *testing.T, rig *composeRig, target []string) (int, string, []string) {
	t.Helper()
	var stdout, stderr strings.Builder
	var terminal bytes.Buffer
	session, err := disclose.NewTerminalSession(socialAcceptanceTerminal{struct {
		io.Reader
		io.Writer
	}{strings.NewReader("n\n"), &terminal}})
	if err != nil {
		t.Fatal(err)
	}
	var injected []string
	ios := cli.IO{
		Stdout: &stdout, Stderr: &stderr, Workdir: t.TempDir(),
		Env: cli.Env{Getenv: func(k string) string {
			if k == "HIKYO_STATE_DIR" {
				return rig.stateDir
			}
			return ""
		}},
		TerminalSession:  session,
		StderrIsTerminal: func() bool { return true },
		Exec:             func(_ string, _, env []string) error { injected = env; return nil },
	}
	args := append(append([]string{"run", "--use-human-session"}, target...), "--", "true")
	return cli.Run(t.Context(), ios, args), stderr.String(), injected
}
