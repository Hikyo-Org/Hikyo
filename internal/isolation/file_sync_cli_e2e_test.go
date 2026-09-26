package isolation

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"gopkg.in/yaml.v3"
)

// In-process e2e for generic file destinations (#164): a real app.Boot
// server, the real CLI, a real workload credential bound to a file target.
// It proves the client renders exactly the target's selection byte-exact with
// the configured mode, reports what it applied, answers "current" through its
// cursor, follows a publish, serves the encrypted snapshot during an outage
// (and refuses it past expiry), and never writes plaintext outside the
// destination.

type fileSyncRig struct {
	*composeRig
	stop func()
}

func bootFileSyncRig(t *testing.T, engine store.Engine) *fileSyncRig {
	t.Helper()
	cfg := retentionAppConfig(t, engine)
	srv, err := app.Boot(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	origin := "http://" + srv.Addr
	stop := serveRetentionApp(t, srv)
	waitHTTP(t, "http://"+srv.OperationalAddr+"/healthz")
	db, err := openBootedIsolationFixture(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	rootKey, err := crypto.ReadRootKey(cfg.RootKeyFile, "")
	if err != nil {
		t.Fatal(err)
	}
	loadAndRegisterKeyring(t, db, rootKey)
	seedComposeCatalogue(t, db)
	stateDir := t.TempDir()
	writeTrustStore(t, stateDir, origin)
	return &fileSyncRig{composeRig: &composeRig{origin: origin, db: db, stateDir: stateDir}, stop: stop}
}

func (r *fileSyncRig) run(t *testing.T, now time.Time, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	ios := cli.IO{
		Stdout: &stdout, Stderr: &stderr, Workdir: t.TempDir(),
		Env: cli.Env{Getenv: func(k string) string {
			if k == "HIKYO_STATE_DIR" {
				return r.stateDir
			}
			return ""
		}},
	}
	if !now.IsZero() {
		ios.Now = func() time.Time { return now }
	}
	code := cli.Run(t.Context(), ios, args)
	return code, stdout.String(), stderr.String()
}

func TestFileSyncCLISQLite(t *testing.T) { runFileSyncCLI(t, store.EngineSQLite) }

func TestFileSyncCLIPostgres(t *testing.T) { runFileSyncCLI(t, store.EnginePostgres) }

func runFileSyncCLI(t *testing.T, engine store.Engine) {
	rig := bootFileSyncRig(t, engine)
	principal, tokenFile := rig.mintWorkload(t)
	rig.grantReveal(t, principal)
	execRaw(t, rig.db, fmt.Sprintf(
		`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_fs_report', '%s', 'report-delivery-status', '%s', '%s', '%s', %s)`,
		principal, cOrg, cPrj, cEnv, ts))
	execRaw(t, rig.db, fmt.Sprintf(
		`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_fs_adapters', '%s', 'manage-adapters', '%s', '%s', NULL, %s)`,
		cAdmin, cOrg, cPrj, ts))
	seedOrigins(t, rig.db)
	saID := queryString(t, rig.db, `SELECT id FROM service_accounts WHERE principal_id = '`+string(principal)+`'`)
	admin := service.LocalPrincipal(domain.PrincipalID(cAdmin))
	project := domain.Scope{Org: cOrg, Project: cPrj}
	targets := &service.FileTargets{DB: rig.db}
	target, err := targets.Create(t.Context(), admin, project, service.FileTargetInput{
		EnvironmentID: cEnv, Name: "web-host", ServiceAccountID: saID,
		KeySelection: service.AdapterKeySelection{Names: []string{"DATABASE_URL", "DATABASE_PASSWORD"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "secrets")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{
		"version": 1, "instance": rig.origin, "org": cOrg, "project": cPrj, "environment": cEnv,
		"target":      target.ID,
		"snapshot":    map[string]any{"offline_serve": true},
		"destination": map[string]any{"directory": dest, "mode": "0640", "on_removed": "prune"},
		"files": []map[string]any{
			{"name": "app.env", "format": "dotenv", "keys": []string{"DATABASE_URL", "DATABASE_PASSWORD"}},
			{"name": "app.json", "format": "json", "keys": []string{"DATABASE_URL", "DATABASE_PASSWORD"}},
			{"name": "db-password", "format": "raw", "keys": []string{"DATABASE_PASSWORD"}},
		},
	}
	cfgPath := filepath.Join(t.TempDir(), "hikyo-file-sync.yaml")
	writeYAML(t, cfgPath, config)
	render := []string{"file-sync", "render", "--config", cfgPath, "--token-file", tokenFile}

	code, _, stderr := rig.run(t, time.Time{}, render...)
	if code != cli.ExitOK {
		t.Fatalf("first render exit=%d; stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "rendered revision") {
		t.Fatalf("first render did not report a generation: %s", stderr)
	}
	assertFile(t, filepath.Join(dest, "app.env"), "DATABASE_URL=postgres://dev\nDATABASE_PASSWORD=dev-secret\n", 0o640)
	assertFile(t, filepath.Join(dest, "app.json"), "{\n  \"DATABASE_PASSWORD\": \"dev-secret\",\n  \"DATABASE_URL\": \"postgres://dev\"\n}\n", 0o640)
	assertFile(t, filepath.Join(dest, "db-password"), "dev-secret", 0o640)

	shown, err := targets.Get(t.Context(), admin, project, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if shown.Report == nil || shown.Report.State != service.FileTargetApplied || shown.Report.Generation != 1 || shown.Report.Stamp == "" {
		t.Fatalf("server-side report after render = %+v", shown.Report)
	}

	// The cursor answers "current"; nothing is rewritten.
	code, _, stderr = rig.run(t, time.Time{}, render...)
	if code != cli.ExitOK || !strings.Contains(stderr, "up to date") {
		t.Fatalf("second render exit=%d; stderr=%s", code, stderr)
	}

	// A publish moves the delivery; the next render follows it.
	publishComposeValues(t, rig.db, map[string]string{"DATABASE_PASSWORD": "rotated $ \"quoted\""})
	code, _, stderr = rig.run(t, time.Time{}, render...)
	if code != cli.ExitOK {
		t.Fatalf("render after publish exit=%d; stderr=%s", code, stderr)
	}
	assertFile(t, filepath.Join(dest, "app.env"), "DATABASE_URL=postgres://dev\nDATABASE_PASSWORD=\"rotated $ \\\"quoted\\\"\"\n", 0o640)
	assertFile(t, filepath.Join(dest, "db-password"), "rotated $ \"quoted\"", 0o640)

	code, stdout, stderr := rig.run(t, time.Time{}, "file-sync", "doctor", "--config", cfgPath, "-o", "json")
	if code != cli.ExitOK || !strings.Contains(stdout, "is intact") {
		t.Fatalf("doctor exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}

	// A key outside the target's selection is refused by name before any
	// write: the server never delivered it.
	config["files"] = append(config["files"].([]map[string]any), map[string]any{"name": "worker", "format": "raw", "keys": []string{"WORKER_URL"}})
	writeYAML(t, cfgPath, config)
	code, _, stderr = rig.run(t, time.Time{}, render...)
	if code != cli.ExitRefused || !strings.Contains(stderr, "worker: WORKER_URL: not delivered") {
		t.Fatalf("out-of-selection render exit=%d; stderr=%s", code, stderr)
	}
	if _, err := os.Lstat(filepath.Join(dest, "worker")); !os.IsNotExist(err) {
		t.Fatal("a refused render wrote a file")
	}
	config["files"] = config["files"].([]map[string]any)[:3]
	writeYAML(t, cfgPath, config)

	// Poll mode renders the change, waits one interval, and exits once the
	// cursor reads current.
	config["refresh"] = map[string]any{"mode": "poll", "interval": "5s", "timeout": "1m"}
	writeYAML(t, cfgPath, config)
	publishComposeValues(t, rig.db, map[string]string{"DATABASE_URL": "postgres://polled"})
	code, _, stderr = rig.run(t, time.Time{}, render...)
	if code != cli.ExitOK || !strings.Contains(stderr, "rendered revision") || !strings.Contains(stderr, "up to date") {
		t.Fatalf("poll render exit=%d; stderr=%s", code, stderr)
	}
	assertFile(t, filepath.Join(dest, "app.env"), "DATABASE_URL=postgres://polled\nDATABASE_PASSWORD=\"rotated $ \\\"quoted\\\"\"\n", 0o640)
	delete(config, "refresh")
	writeYAML(t, cfgPath, config)

	// Outage: the encrypted snapshot serves within its age, never past it.
	rig.stop()
	code, _, stderr = rig.run(t, time.Time{}, render...)
	if code != cli.ExitOK || !strings.Contains(stderr, "serving stale from") {
		t.Fatalf("offline render exit=%d; stderr=%s", code, stderr)
	}
	assertFile(t, filepath.Join(dest, "db-password"), "rotated $ \"quoted\"", 0o640)
	code, _, stderr = rig.run(t, time.Now().Add(8*24*time.Hour), render...)
	if code != cli.ExitRefused || !strings.Contains(stderr, "offline serve refused") {
		t.Fatalf("expired offline render exit=%d; stderr=%s", code, stderr)
	}
	// The last valid generation survives the refusal.
	assertFile(t, filepath.Join(dest, "db-password"), "rotated $ \"quoted\"", 0o640)

	// Zero plaintext outside the destination: the state dir holds only the
	// sealed snapshot, the local key and bookkeeping.
	err = filepath.WalkDir(rig.stateDir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "rotated $") || strings.Contains(string(b), "dev-secret") {
			t.Errorf("plaintext in client state %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeYAML(t *testing.T, path string, v any) {
	t.Helper()
	// Round-trip through JSON so the map keys serialize deterministically.
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var generic any
	if err := yaml.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	out, err := yaml.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, want string, mode os.FileMode) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != mode {
		t.Fatalf("%s mode = %v, want %v (%v)", path, fi.Mode().Perm(), mode, err)
	}
}
