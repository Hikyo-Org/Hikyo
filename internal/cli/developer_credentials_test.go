package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/crypto"

	"github.com/Hikyo-Org/hikyo/internal/cli"
)

func TestDeveloperSessionMintRefusesWithoutTerminal(t *testing.T) {
	ios, _, stderr := testIO(t, nil)
	code := cli.Run(t.Context(), ios, []string{"dev", "session", "--env", "env_70"})
	if code != cli.ExitRefused || !strings.Contains(stderr.String(), "controlling terminal") {
		t.Fatalf("code=%d stderr=%s; want a controlling-terminal refusal", code, stderr.String())
	}
}

const (
	developerOrg     = "org_01989abc-def0-7123-8123-000000000001"
	developerProject = "prj_01989abc-def0-7123-8123-000000000002"
	developerEnv     = "env_01989abc-def0-7123-8123-000000000003"
	developerID      = "dev_01989abc-def0-7123-8123-000000000004"
)

func developerRunArgs() []string {
	return []string{"run", "--instance", "local", "--org", developerOrg, "--project", developerProject, "--env", developerEnv, "--", "true"}
}

func putDeveloperCredential(t *testing.T, ios cli.IO, id string, expires time.Time) (cli.DeveloperCredentialArtifact, *cli.State) {
	t.Helper()
	st, err := cli.NewState(ios.Env)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := st.Trust().Load()
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := crypto.NewArtifact(crypto.ArtifactDeveloperCredential)
	if err != nil {
		t.Fatal(err)
	}
	c := cli.DeveloperCredentialArtifact{ID: id, Origin: entries["local"].Origin, Org: developerOrg, Project: developerProject, Environment: developerEnv, Token: token, ExpiresAt: expires}
	if err := st.PutDeveloperCredential(c); err != nil {
		t.Fatal(err)
	}
	return c, st
}

func TestDeveloperRunIsNonTTYOnlineOnlyAndStripsCredentialEnvironment(t *testing.T) {
	var calls []string
	var selected string
	value := "dev-secret"
	ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if !strings.HasSuffix(r.URL.Path, "/delivery") {
			t.Errorf("unexpected non-delivery call %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+selected {
			t.Errorf("wrong selected credential")
		}
		if r.URL.Query().Get("cursor") != "" {
			t.Errorf("developer fetch uses cached cursor")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apigen.DeliveryResponse{CredentialId: developerID, Revision: 1, SchemaRevision: 1, Keys: []apigen.DeliveredKey{{KeyId: "key_one", Name: "DEV_807_SECRET", Classification: apigen.KeyClassificationSecret, Presence: apigen.DeliveredKeyPresenceSet, Value: &value}}})
	}))
	credential, st := putDeveloperCredential(t, ios, developerID, time.Now().Add(time.Hour))
	selected = credential.Token
	t.Setenv("HIKYO_TOKEN", "parent-machine-value")
	t.Setenv("HIKYO_SESSION_TOKEN", "parent-human-value")
	t.Setenv("HIKYO_OIDC_TOKEN", "parent-id-token")
	executed := false
	ios.Exec = func(_ string, _ []string, env []string) error {
		executed = true
		if !slices.Contains(env, "DEV_807_SECRET=dev-secret") {
			t.Error("secret not injected")
		}
		for _, item := range env {
			if strings.HasPrefix(item, "HIKYO_TOKEN=") || strings.HasPrefix(item, "HIKYO_SESSION_TOKEN=") || strings.HasPrefix(item, "HIKYO_OIDC_TOKEN=") {
				t.Errorf("child inherited credential %s", strings.SplitN(item, "=", 2)[0])
			}
		}
		return nil
	}
	if code := cli.Run(t.Context(), ios, developerRunArgs()); code != cli.ExitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !executed || len(calls) != 1 {
		t.Fatalf("executed=%t calls=%v", executed, calls)
	}
	if strings.Contains(stdout.String()+stderr.String(), selected) || strings.Contains(stdout.String()+stderr.String(), value) {
		t.Fatal("secret printed")
	}
	if !strings.Contains(stderr.String(), "developer-credential") || !strings.Contains(stderr.String(), "expires at") {
		t.Errorf("selected credential diagnostic missing: %s", stderr.String())
	}
	files, err := os.ReadDir(st.Dir())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.Contains(file.Name(), "snapshot") || file.Name() == "compose" || strings.Contains(file.Name(), "offline") {
			t.Errorf("developer delivery wrote cache %s", file.Name())
		}
	}
}

func TestDeveloperRunRefusesExpiredAmbiguousAndUnsafeCustodyBeforeNetwork(t *testing.T) {
	for _, kind := range []string{"expired", "ambiguous", "mode", "symlink", "different-environment", "different-origin"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.NotFound(w, r) }))
			expiry := time.Now().Add(time.Hour)
			if kind == "expired" {
				expiry = time.Now().Add(-time.Second)
			}
			c, st := putDeveloperCredential(t, ios, developerID, expiry)
			path := filepath.Join(st.Dir(), "developer-credentials.json")
			switch kind {
			case "ambiguous":
				_, _ = putDeveloperCredential(t, ios, "dev_01989abc-def0-7123-8123-000000000005", expiry)
			case "mode":
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if runtime.GOOS == "windows" {
					t.Skip("symlink test requires Unix")
				}
				original := path + ".original"
				if err := os.Rename(path, original); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(original, path); err != nil {
					t.Fatal(err)
				}
			case "different-environment":
				c.Environment = "env_other"
				if err := st.PutDeveloperCredential(c); err != nil {
					t.Fatal(err)
				}
			case "different-origin":
				c.Origin = "https://different.example"
				if err := st.PutDeveloperCredential(c); err != nil {
					t.Fatal(err)
				}
			}
			ios.Exec = func(string, []string, []string) error { t.Fatal("unsafe custody reached child"); return nil }
			if code := cli.Run(t.Context(), ios, developerRunArgs()); code == cli.ExitOK {
				t.Fatalf("unsafe custody accepted: %s", stderr.String())
			}
			if calls != 0 {
				t.Fatalf("refusal leaked credentials over %d requests", calls)
			}
		})
	}
}

func TestDeveloperRunNeverUsesSnapshotOrHumanFallbackAfterServerRefusal(t *testing.T) {
	calls := 0
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"not found"}}`))
	}))
	_, st := putDeveloperCredential(t, ios, developerID, time.Now().Add(time.Hour))
	old := []byte("old machine snapshot must not be read or overwritten")
	path := filepath.Join(st.Dir(), "snapshot.bin")
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	ios.Exec = func(string, []string, []string) error { t.Fatal("refused online fetch used cached values"); return nil }
	if code := cli.Run(t.Context(), ios, developerRunArgs()); code == cli.ExitOK {
		t.Fatalf("refused fetch succeeded: %s", stderr.String())
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, old) {
		t.Fatalf("snapshot changed err=%v", err)
	}
	if calls != 1 {
		t.Fatalf("unexpected retry/fallback calls=%d", calls)
	}
}

func TestDeveloperRunRefusesBearerThroughUnboundMachineChannels(t *testing.T) {
	calls := 0
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.NotFound(w, r) }))
	token, _, err := crypto.NewArtifact(crypto.ArtifactDeveloperCredential)
	if err != nil {
		t.Fatal(err)
	}
	before := ios.Env.Getenv
	ios.Env = cli.Env{Getenv: func(key string) string {
		if key == "HIKYO_TOKEN" {
			return token
		}
		return before(key)
	}}
	if code := cli.Run(t.Context(), ios, developerRunArgs()); code != cli.ExitRefused {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if calls != 0 {
		t.Fatalf("unbound artifact sent over network")
	}
}

func TestDeveloperMintAlwaysUsesFreshBoundCeremonyAndPersistsMetadataOnlyOutput(t *testing.T) {
	testDeveloperMint(t, false, false, false)
}
func TestDeveloperMintRevokesAfterPrivatePersistenceFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("failure seam uses Unix symlink")
	}
	testDeveloperMint(t, true, false, false)
}
func TestDeveloperMintReportsFailedServerCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("failure seam uses Unix symlink")
	}
	testDeveloperMint(t, true, true, false)
}
func TestDeveloperMintRefusalExplainsLoginAssuranceStepUp(t *testing.T) {
	testDeveloperMint(t, false, false, true)
}

func testDeveloperMint(t *testing.T, failPersistence, failCleanup, refuseMint bool) {
	t.Helper()
	token, _, err := crypto.NewArtifact(crypto.ArtifactDeveloperCredential)
	if err != nil {
		t.Fatal(err)
	}
	rotated := "rotated-cli-session"
	var stateDir string
	var order []string
	var fresh bool
	ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/revisions/latest"):
			_ = json.NewEncoder(w).Encode(apigen.RevisionDetail{Keys: []apigen.SnapshotKey{{KeyId: "key_01989abc-def0-7123-8123-000000000006", Name: "DEV_807_SECRET"}}})
		case strings.HasSuffix(r.URL.Path, "/reveal-window"):
			// A live generic window must not authorize mint without another ceremony.
			_ = json.NewEncoder(w).Encode(apigen.RevealWindow{Live: true, CanReveal: true, EffectiveWindowSeconds: 300, TotpOffered: true})
		case r.URL.Path == "/api/v1/auth/totp":
			_ = json.NewEncoder(w).Encode(apigen.TotpStatus{Confirmed: true})
		case r.URL.Path == "/api/v1/auth/reauth/totp":
			order = append(order, "fresh-proof")
			var body apigen.TotpDeveloperCredentialReauthRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Purpose != "developer-credential" || body.EnvironmentId != developerEnv || body.DeveloperCredential.LifetimeSeconds != 3600 || !body.DeveloperCredential.ConsentCurrentAndFuture || body.Code != "123456" || len(body.KeyIds) != 1 {
				t.Errorf("proof not bound to exact confirmed delegation")
			}
			fresh = true
			_ = json.NewEncoder(w).Encode(apigen.ReauthResult{SessionId: "ses_rotated", SessionToken: &rotated})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/developer-credentials"):
			order = append(order, "mint")
			if !fresh || r.Header.Get("Authorization") != "Bearer "+rotated {
				t.Error("mint bypassed fresh rotated proof")
			}
			var body apigen.MintDeveloperCredentialRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.LifetimeSeconds == nil || *body.LifetimeSeconds != 3600 || !body.ConsentCurrentAndFuture || len(body.KeyIds) != 1 {
				t.Error("mint changed consent unit")
			}
			if refuseMint {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"code":"not-found","message":"not found"}}`))
				return
			}
			if failPersistence {
				target := filepath.Join(stateDir, "unsafe-target")
				if err := os.WriteFile(target, []byte("private destination"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(stateDir, "developer-credentials.json")); err != nil {
					t.Fatal(err)
				}
			}
			_ = json.NewEncoder(w).Encode(apigen.MintDeveloperCredentialResult{Value: token, Credential: apigen.DeveloperCredential{Id: developerID, OrgId: developerOrg, ProjectId: developerProject, EnvironmentId: developerEnv, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), AuthorityPrincipalId: "usr_01989abc-def0-7123-8123-000000000008"}})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/auth/developer-credentials/"+developerID:
			order = append(order, "revoke")
			if failCleanup {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":{"code":"unavailable","message":"temporarily unavailable"}}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	stateDir = ios.Env.Getenv("HIKYO_STATE_DIR")
	ios.TerminalSession, _ = terminalSession(t, "y\n")
	prompts := 0
	ios.ReadPassword = func(string) (string, error) { prompts++; return "123456", nil }
	args := []string{"dev", "session", "--instance", "local", "--org", developerOrg, "--project", developerProject, "--env", developerEnv, "--ttl", "1h"}
	code := cli.Run(t.Context(), ios, args)
	if refuseMint {
		if code != cli.ExitNotFound || !strings.Contains(stderr.String(), "hikyo account factor step-up") || !strings.Contains(stderr.String(), "does not upgrade login assurance") || strings.Join(order, ",") != "fresh-proof,mint" {
			t.Fatalf("mint refusal lost assurance guidance or status: code=%d order=%v stderr=%s", code, order, stderr.String())
		}
	} else if failPersistence {
		if code == cli.ExitOK || strings.Join(order, ",") != "fresh-proof,mint,revoke" {
			t.Fatalf("cleanup code=%d order=%v stderr=%s", code, order, stderr.String())
		}
		expected := "server credential revoked"
		if failCleanup {
			expected = "server revocation failed"
		}
		if !strings.Contains(stderr.String(), expected) {
			t.Errorf("cleanup result missing %q: %s", expected, stderr.String())
		}
	} else {
		if code != cli.ExitOK {
			t.Fatalf("mint code=%d stderr=%s", code, stderr.String())
		}
		st, err := cli.NewState(ios.Env)
		if err != nil {
			t.Fatal(err)
		}
		all, err := st.DeveloperCredentials()
		if err != nil {
			t.Fatal(err)
		}
		if all[developerID].Token != token {
			t.Error("minted token not in private custody")
		}
		info, err := os.Stat(filepath.Join(stateDir, "developer-credentials.json"))
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Errorf("custody mode=%o", info.Mode().Perm())
		}
	}
	if prompts != 1 || strings.Contains(stdout.String()+stderr.String(), token) {
		t.Error("fresh proof count or bearer output violated")
	}
}

func TestDeveloperRunPreservesExplicitMachinePrecedence(t *testing.T) {
	machine, _, err := crypto.NewArtifact(crypto.ArtifactWorkload)
	if err != nil {
		t.Fatal(err)
	}
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/delivery") || r.Header.Get("Authorization") != "Bearer "+machine {
			t.Error("explicit machine did not retain precedence")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apigen.DeliveryResponse{Keys: []apigen.DeliveredKey{}})
	}))
	_, st := putDeveloperCredential(t, ios, developerID, time.Now().Add(time.Hour))
	if err := os.Chmod(filepath.Join(st.Dir(), "developer-credentials.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := ios.Env.Getenv
	ios.Env = cli.Env{Getenv: func(k string) string {
		if k == "HIKYO_TOKEN" {
			return machine
		}
		return before(k)
	}}
	execs := 0
	ios.Exec = func(string, []string, []string) error { execs++; return nil }
	if code := cli.Run(t.Context(), ios, developerRunArgs()); code != cli.ExitOK || execs != 1 {
		t.Fatalf("code=%d execs=%d stderr=%s", code, execs, stderr.String())
	}
}

func TestDeveloperMintRefusesMutableTargetNamesBeforeNetwork(t *testing.T) {
	calls := 0
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.NotFound(w, r) }))
	ios.TerminalSession, _ = terminalSession(t, "y\n")
	args := []string{"dev", "session", "--instance", "local", "--org", developerOrg, "--project", developerProject, "--env", "development"}
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitUsage || !strings.Contains(stderr.String(), "immutable env ID") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if calls != 0 {
		t.Fatalf("mutable target reached network: %d", calls)
	}
}

func TestDeveloperRunRefusesRepositoryLocalCustody(t *testing.T) {
	calls := 0
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.NotFound(w, r) }))
	_, st := putDeveloperCredential(t, ios, developerID, time.Now().Add(time.Hour))
	if err := os.WriteFile(filepath.Join(st.Dir(), ".git"), []byte("gitdir: harmless-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	ios.Workdir = st.Dir()
	if code := cli.Run(t.Context(), ios, developerRunArgs()); code != cli.ExitRefused || !strings.Contains(stderr.String(), "outside the repository") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if calls != 0 {
		t.Fatalf("repository custody leaked credential")
	}
}

func TestDeveloperRunRefusesRepositoryCustodyFromUnrelatedWorkdir(t *testing.T) {
	calls := 0
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.NotFound(w, r) }))
	_, st := putDeveloperCredential(t, ios, developerID, time.Now().Add(time.Hour))
	if err := os.WriteFile(filepath.Join(st.Dir(), ".git"), []byte("gitdir: harmless-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	ios.Workdir = t.TempDir()
	if code := cli.Run(t.Context(), ios, developerRunArgs()); code != cli.ExitRefused || !strings.Contains(stderr.String(), "outside the repository") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if calls != 0 {
		t.Fatalf("repository custody reached network: %d", calls)
	}
}

func TestDeveloperRunRefusesCustodyInEnclosingRepository(t *testing.T) {
	calls := 0
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.NotFound(w, r) }))
	_, st := putDeveloperCredential(t, ios, developerID, time.Now().Add(time.Hour))
	if err := os.WriteFile(filepath.Join(st.Dir(), ".git"), []byte("gitdir: outer-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(st.Dir(), "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, ".git"), []byte("gitdir: nested-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	ios.Workdir = nested
	if code := cli.Run(t.Context(), ios, developerRunArgs()); code != cli.ExitRefused || !strings.Contains(stderr.String(), "outside the repository") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if calls != 0 {
		t.Fatalf("enclosing repository custody reached network: %d", calls)
	}
}
