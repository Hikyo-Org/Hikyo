package isolation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestPersonalOffboardingReportCLISQLite(t *testing.T) {
	runPersonalOffboardingReportCLI(t, store.EngineSQLite)
}

func TestPersonalOffboardingReportCLIPostgres(t *testing.T) {
	runPersonalOffboardingReportCLI(t, store.EnginePostgres)
}

// runPersonalOffboardingReportCLI drives `hikyo personal offboarding-report`
// against a booted server: the table, the stderr evidence window and limits,
// the JSON shape, and the nonexistent answer.
func runPersonalOffboardingReportCLI(t *testing.T, engine store.Engine) {
	rig := bootComposeRig(t, engine)
	auth := authService(t, rig.db)
	adm := bootstrapAdmin(t, rig.db, adminOpts{username: "pv-report-cli", displayName: "PV Report", password: ceremonyPassword, auth: auth, login: true})
	principal := string(adm.boot.PrincipalID)
	for _, cap := range []string{"manage-members", "audit-read"} {
		execRaw(t, rig.db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_pv_report_`+cap+`', '`+principal+`', '`+cap+`', '`+cOrg+`', '`+cPrj+`', NULL, `+ts+`)`)
	}
	seedOrigins(t, rig.db)
	execRaw(t, rig.db, `INSERT INTO principals (id, kind, class, created_at) VALUES ('mch_pv_report', 'machine', 'workload', `+ts+`)`)
	execRaw(t, rig.db, `INSERT INTO service_accounts (id, principal_id, org_id, project_id, name, kind, created_at, created_by) `+
		`VALUES ('sa_pv_report', 'mch_pv_report', '`+cOrg+`', '`+cPrj+`', 'report-bot', 'workload', `+ts+`, '`+principal+`')`)
	token := sessionWithWindows(t, rig.db, adm.boot.PrincipalID)

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
		Instance: "local", Origin: rig.origin, Token: token, SessionID: "ses_" + principal,
		Principal: principal, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	report := func(args ...string) (int, string, string) {
		t.Helper()
		return rig.runCLI(t, t.TempDir(), nil, append([]string{"personal", "offboarding-report"}, append(args, "--instance", "local", "--org", cOrg, "--project", cPrj)...)...)
	}

	code, stdout, stderr := report("pv-report-cli")
	if code != cli.ExitOK || !strings.Contains(stdout, "service account") || !strings.Contains(stdout, "report-bot") {
		t.Fatalf("report table: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "evidence from ") || !strings.Contains(stderr, "limit: Hikyo cannot undo a disclosure") ||
		!strings.Contains(stderr, "persist in backups") {
		t.Fatalf("report stderr lacks the window or limits: %s", stderr)
	}

	code, stdout, stderr = report("pv-report-cli", "-o", "json")
	var out apigen.PersonalOffboardingReport
	if code != cli.ExitOK || json.Unmarshal([]byte(stdout), &out) != nil {
		t.Fatalf("report json: exit=%d stderr=%s", code, stderr)
	}
	if out.PrincipalId != principal || out.Username != "pv-report-cli" || len(out.ServiceAccounts) != 1 || len(out.Limits) == 0 || out.EvidenceFrom.After(out.GeneratedAt) {
		t.Fatalf("report json = %+v", out)
	}

	if code, _, stderr := report("nobody-at-all"); code != cli.ExitNotFound {
		t.Fatalf("nonexistent member: exit=%d stderr=%s", code, stderr)
	}
}
