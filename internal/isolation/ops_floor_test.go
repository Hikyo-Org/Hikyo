//go:build opsfloor

package isolation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

type opsDoctorReport struct {
	Status   string `json:"status"`
	Findings []struct {
		Code     string `json:"code"`
		Severity string `json:"severity"`
		Message  string `json:"message"`
	} `json:"findings"`
}

// TestOpsFloorDoctor exercises the real CLI and application with synthetic
// persisted health states. Resource claims belong exclusively to the native
// cgroup runner; invoking this test locally is functional validation only.
func TestOpsFloorDoctor(t *testing.T) {
	binary := os.Getenv("HIKYO_OPS_FLOOR_CLI")
	if !filepath.IsAbs(binary) {
		t.Fatal("HIKYO_OPS_FLOOR_CLI must name the source-built absolute CLI path")
	}
	c := newCustody(t)
	target := sqliteTarget(t, t.TempDir(), c.recipient(t))
	db, artifacts := buildInstance(t, target, c)
	defer db.Close()
	auth := authWithRoot(t, db, c.rootKey(t))
	token := stepUpPasskey(t, auth, t.Context(), artifacts.session, artifacts.passkey)
	configureSAMLProvider(t, auth, artifacts.adminPrin)
	target.cfg.Dev = true
	target.cfg.Listen, target.cfg.OperationalListen = "127.0.0.1:0", "127.0.0.1:0"
	target.cfg.ExternalOrigin = waOrigin
	target.cfg.Argon2MemoryKiB, target.cfg.Argon2Time, target.cfg.Argon2Parallelism = crypto.PasswordFloor.MemoryKiB, crypto.PasswordFloor.Time, crypto.PasswordFloor.Parallelism
	target.cfg.AdmissionBudgetMiB = 272
	target.cfg.BackupRPO = config.DefaultBackupRPO
	target.cfg.BackupInterval = config.DefaultBackupInterval
	target.cfg.BackupRetainCount = config.DefaultBackupRetainCount
	target.cfg.BackupRetainDays = config.DefaultBackupRetainDays
	target.cfg.BackupRTOTarget = config.DefaultBackupRTOTarget
	now := time.Now().UTC().Format(time.RFC3339Nano)
	execRealAdoption(t, db, `UPDATE backup_state SET last_success_at=$1,last_artifact_name='synthetic.age',last_artifact_bytes=1,last_drill_at=$1,last_drill_ok=1 WHERE id=1`, now)
	execRealAdoption(t, db, `UPDATE retention_runtime SET last_prune_success=$1 WHERE id=1`, now)
	srv, err := app.Boot(t.Context(), target.cfg, drillLogger())
	if err != nil {
		t.Fatal(err)
	}
	stop := serveRetentionApp(t, srv)
	defer stop()
	waitHTTP(t, "http://"+srv.OperationalAddr+"/healthz")
	stateDir := t.TempDir()
	state, err := cli.NewState(cli.Env{Getenv: func(name string) string {
		if name == "HIKYO_STATE_DIR" {
			return stateDir
		}
		return ""
	}})
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + srv.Addr
	if err := state.Trust().Put(cli.TrustEntry{Name: "floor", Origin: origin}); err != nil {
		t.Fatal(err)
	}
	identity := authenticate(t, db, token)
	if identity.SessionID == "" {
		t.Fatal("floor human session missing")
	}
	if err := state.PutSession(cli.SessionArtifact{Instance: "floor", Origin: origin, Token: token, SessionID: identity.SessionID, Principal: string(identity.Principal), ExpiresAt: identity.AbsoluteExpiresAt.Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	results := map[string]opsDoctorReport{}
	check := func(name, status string, exit int, code, severity string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), binary, "doctor", "--instance", "floor", "-o", "json")
		cmd.Env = []string{"HIKYO_STATE_DIR=" + stateDir}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, runErr := cmd.Output()
		gotExit := 0
		if runErr != nil {
			if failure, ok := runErr.(*exec.ExitError); ok {
				gotExit = failure.ExitCode()
			} else {
				t.Fatalf("doctor invocation: %v", runErr)
			}
		}
		if gotExit != exit {
			var diagnostic struct {
				Status   string `json:"status"`
				Findings []struct {
					Code     string `json:"code"`
					Severity string `json:"severity"`
				} `json:"findings"`
			}
			_ = json.Unmarshal(output, &diagnostic)
			safe := strings.NewReplacer(token, "[REDACTED]", artifacts.password, "[REDACTED]")
			t.Fatalf("%s: doctor exit %d, want %d; checklist=%+v; stderr=%s", name, gotExit, exit, diagnostic, safe.Replace(stderr.String()))
		}
		var report opsDoctorReport
		if err := json.Unmarshal(output, &report); err != nil {
			t.Fatalf("%s: invalid doctor JSON: %v", name, err)
		}
		if report.Status != status {
			t.Fatalf("%s: status %q, want %q", name, report.Status, status)
		}
		codes := map[string]string{}
		for _, finding := range report.Findings {
			codes[finding.Code] = finding.Severity
		}
		for _, required := range []string{"retention-prune", "project-storage", "backup-rpo", "restore-drill", "adapter-targets"} {
			if codes[required] == "" {
				t.Fatalf("%s: doctor omitted %s", name, required)
			}
		}
		if code != "" && codes[code] != severity {
			t.Fatalf("%s: %s severity %q, want %q", name, code, codes[code], severity)
		}
		if status == "ok" {
			for name, severity := range codes {
				if severity != "ok" {
					t.Fatalf("healthy checklist retained %s=%s", name, severity)
				}
			}
		}
		results[name] = report
		t.Logf("doctor state %s: status=%s exit=%d", name, report.Status, gotExit)
	}
	check("healthy", "ok", cli.ExitOK, "", "")
	execRealAdoption(t, db, `UPDATE retention_runtime SET last_prune_success=$1 WHERE id=1`, time.Now().UTC().Add(-25*time.Hour).Format(time.RFC3339Nano))
	check("prune-warning", "warning", cli.ExitOK, "retention-prune", "warn")
	execRealAdoption(t, db, `UPDATE retention_runtime SET last_prune_success=$1 WHERE id=1`, now)
	execRealAdoption(t, db, `UPDATE backup_state SET last_success_at=$1 WHERE id=1`, time.Now().UTC().Add(-48*time.Hour).Format(time.RFC3339Nano))
	check("backup-error", "error", cli.ExitRefused, "backup-rpo", "error")
	execRealAdoption(t, db, `UPDATE backup_state SET last_success_at=$1,last_drill_ok=0 WHERE id=1`, now)
	check("drill-warning", "warning", cli.ExitOK, "restore-drill", "warn")
	execRealAdoption(t, db, `UPDATE backup_state SET last_drill_ok=1 WHERE id=1`)
	execRaw(t, db, `INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_floor','org_a','prj_a1','forgejo','https://fixture.invalid','usr_alice','active',`+ts+`)`)
	execRaw(t, db, `INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_floor','org_a','prj_a1','env_a1','adp_floor','repository','fixture','fixture',1,'FLOOR_',1,'active','failed',`+ts+`)`)
	check("adapter-warning", "warning", cli.ExitOK, "adapter-targets", "warn")
	execRaw(t, db, `UPDATE adapter_targets SET sync_status='converged' WHERE id='tgt_floor'`)
	execRealAdoption(t, db, `UPDATE saml_providers SET metadata_valid_until=$1 WHERE slug='saml-idp'`, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano))
	check("provider-error", "error", cli.ExitRefused, "metadata_expired", "error")
	execRealAdoption(t, db, `UPDATE saml_providers SET metadata_valid_until=$1 WHERE slug='saml-idp'`, time.Now().UTC().Add(180*24*time.Hour).Format(time.RFC3339Nano))
	// Allocate exactly the real 1 GiB warning floor as 64 KiB synthetic payload
	// rows. These are accounting fixtures, never decrypted or represented as
	// real published ciphertext. No production threshold is lowered.
	var snapshot string
	if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT id FROM snapshots WHERE org_id='org_a' AND project_id='prj_a1' LIMIT 1`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	// Bound setup memory independently of the persisted accounting threshold.
	for batch := 0; batch < 64; batch++ {
		execRealAdoption(t, db, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<256)
  INSERT INTO snapshot_entries(id,org_id,project_id,environment_id,snapshot_id,key_id,key_name,classification,ciphertext,value_entry_id)
  SELECT 'floor_bytes_'||($2+i),'org_a','prj_a1','env_a1',$1,'floor_key_'||($2+i),'FLOOR_'||($2+i),'config',zeroblob(65536),'floor_value_'||($2+i) FROM n`, snapshot, batch*256)
	}

	check("storage-warning", "warning", cli.ExitOK, "project-storage", "warn")
	execRaw(t, db, `DELETE FROM snapshot_entries WHERE id LIKE 'floor_bytes_%'`)
	check("recovered", "ok", cli.ExitOK, "", "")
	for _, report := range results {
		raw, _ := json.Marshal(report)
		if strings.Contains(string(raw), token) || strings.Contains(string(raw), artifacts.password) {
			t.Fatal("doctor output contained synthetic credential material")
		}
	}
	if output := os.Getenv("HIKYO_OPS_FLOOR_OUTPUT"); output != "" {
		raw, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "doctor.json"), append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log(fmt.Sprintf("doctor checklist: %d states, authenticated real CLI and server, synthetic SQLite health", len(results)))
}

func TestOpsFloorStorageRefusal(t *testing.T) { runProjectStorageHighWater(t, seededDB(t, openSQLite)) }
